package reviewfixture

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/sonastea/ticketopia/internal/drafts"
	"github.com/sonastea/ticketopia/internal/persistence/persistencetest"
)

// browser sends requests as a fixture account, without following redirects.
type browser struct {
	t      *testing.T
	m      Manifest
	cookie *http.Cookie
}

func signedIn(t *testing.T, m Manifest, who string) browser {
	t.Helper()
	var state struct {
		Cookies []struct{ Name, Value string }
	}
	data, err := os.ReadFile(m.Accounts[who].StorageState)
	if err != nil || json.Unmarshal(data, &state) != nil || len(state.Cookies) != 1 {
		t.Fatalf("%s storage state: %v", who, err)
	}
	return browser{t, m, &http.Cookie{Name: state.Cookies[0].Name, Value: state.Cookies[0].Value}}
}

func (b browser) do(method, path string, form url.Values) (int, http.Header, string) {
	b.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	request, _ := http.NewRequestWithContext(b.t.Context(), method, b.m.BaseURL+path, body)
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", b.m.BaseURL)
	}
	if b.cookie != nil {
		request.AddCookie(b.cookie)
	}
	response, err := (&http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}).Do(request)
	if err != nil {
		b.t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	return response.StatusCode, response.Header, string(data)
}

var formInput = regexp.MustCompile(`<input type="hidden" name="([a-z_]+)" value="([^"]*)"`)

// connectWithPublish runs the owner through the consent page with the
// publish box ticked, and exchanges the code as the review agent would.
func connectWithPublish(t *testing.T, m Manifest, b browser) string {
	t.Helper()
	status, _, page := b.do(http.MethodGet, m.Paths["oauth_authorize_publish"], nil)
	if status != http.StatusOK || !strings.Contains(page, `name="publish"`) {
		t.Fatalf("consent: %d", status)
	}
	form := url.Values{"decision": {"allow"}, "write": {"on"}, "publish": {"on"}}
	for _, match := range formInput.FindAllStringSubmatch(page, -1) {
		form.Set(match[1], html.UnescapeString(match[2]))
	}
	status, header, _ := b.do(http.MethodPost, "/oauth/authorize", form)
	redirect, _ := url.Parse(header.Get("Location"))
	if status != http.StatusSeeOther || redirect.Query().Get("code") == "" {
		t.Fatalf("decision: %d %s", status, header.Get("Location"))
	}
	token := url.Values{"grant_type": {"authorization_code"}, "client_id": {ReviewClientID}, "code": {redirect.Query().Get("code")},
		"code_verifier": {ReviewVerifier}, "redirect_uri": {"http://127.0.0.1:53682/callback"}}
	response, err := http.PostForm(m.BaseURL+"/oauth/token", token)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var issued struct {
		AccessToken string `json:"access_token"`
		Scope       string
		Error       string `json:"error_description"`
	}
	if err := json.NewDecoder(response.Body).Decode(&issued); err != nil || issued.Scope != "read write publish" {
		t.Fatalf("token: %d %v %+v", response.StatusCode, err, issued)
	}
	return issued.AccessToken
}

func publicJSON(t *testing.T, base, path string) string {
	t.Helper()
	response, err := http.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("%s: %d %s", path, response.StatusCode, data)
	}
	return string(data)
}

// An agent the owner allowed to post publicly makes a choice public, then
// recommends and withdraws; a reason it drafts stays invisible until the
// owner publishes it on /me/drafts, which then shows "via" the app. Drafts
// are per account and capped. Expiry is covered by the persistence tests.
func TestMariaDBAgentPublishing(t *testing.T) {
	database, ok := persistencetest.OptionsFromEnv(filepath.Join("..", "..", ".local", "mariadb", "certs", "ca.crt"))
	if !ok {
		t.Skip("set MARIADB_TEST_ADDR to run real MariaDB tests")
	}
	s, err := Start(t.Context(), Options{Scenario: "discover-last-known", Addr: "127.0.0.1:0", StateDir: t.TempDir(), Database: database})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	m := s.Manifest()
	base, owner := m.BaseURL, m.Accounts["owner"]
	b := signedIn(t, m, "owner")
	access := connectWithPublish(t, m, b)
	// Events without seeded interest or recommendations from the owner.
	interestEvent, recommendEvent, draftEvent := m.EventIDs[3], m.EventIDs[4], m.EventIDs[5]

	// API tokens never publish.
	if r := mcpTool(t, base, owner.APIToken, "recommend_event", map[string]any{"event_id": recommendEvent}); r.Status != http.StatusForbidden {
		t.Fatalf("API token published: %+v", r)
	}

	// A public Interested choice appears on the public profile.
	profileInterests := "/api/v1/users/" + owner.ID + "/event-interests"
	if strings.Contains(publicJSON(t, base, profileInterests), interestEvent) {
		t.Fatal("event already public on the profile")
	}
	if r := mcpTool(t, base, access, "make_interest_public", map[string]any{"event_id": interestEvent}); r.IsError || !strings.Contains(string(r.Content), `"visibility":"public"`) {
		t.Fatalf("make_interest_public: %+v", r)
	}
	if !strings.Contains(publicJSON(t, base, profileInterests), interestEvent) {
		t.Fatal("public choice missing from the profile")
	}
	if r := mcpTool(t, base, access, "make_interest_private", map[string]any{"event_id": interestEvent}); r.IsError || strings.Contains(publicJSON(t, base, profileInterests), interestEvent) {
		t.Fatalf("make_interest_private: %+v", r)
	}

	// Recommend without a reason, attributed; then withdraw.
	eventRecommendations := "/api/v1/events/" + url.PathEscape(recommendEvent) + "/recommendations"
	if r := mcpTool(t, base, access, "recommend_event", map[string]any{"event_id": recommendEvent}); r.IsError || !strings.Contains(string(r.Content), `"via":"Review Agent"`) {
		t.Fatalf("recommend_event: %+v", r)
	}
	if body := publicJSON(t, base, eventRecommendations); !strings.Contains(body, `"via":"Review Agent"`) || !strings.Contains(body, owner.ID) {
		t.Fatalf("public recommendation: %s", body)
	}
	if _, _, body := (browser{t, m, nil}).do(http.MethodGet, "/events/"+url.PathEscape(recommendEvent)+"?section=community", nil); !strings.Contains(body, `<span class="recommendation-via">via Review Agent (review-agent.example)</span>`) {
		t.Fatal("event page lacks the via label")
	}
	if r := mcpTool(t, base, access, "withdraw_recommendation", map[string]any{"event_id": recommendEvent}); r.IsError || strings.Contains(publicJSON(t, base, eventRecommendations), owner.ID) {
		t.Fatalf("withdraw_recommendation: %+v", r)
	}
	// The owner's seeded recommendation keeps its reason.
	if r := mcpTool(t, base, access, "recommend_event", map[string]any{"event_id": m.EventIDs[0]}); r.IsError ||
		!strings.Contains(string(r.Content), "Arrive early; the opener is worth it.") || strings.Contains(string(r.Content), `"via"`) {
		t.Fatalf("existing reason: %+v", r)
	}

	// The agent cannot withdraw the owner's own recommendation.
	if r := mcpTool(t, base, access, "withdraw_recommendation", map[string]any{"event_id": m.EventIDs[0]}); !r.IsError ||
		!strings.Contains(publicJSON(t, base, "/api/v1/events/"+url.PathEscape(m.EventIDs[0])+"/recommendations"), "Arrive early; the opener is worth it.") {
		t.Fatalf("withdrew the owner's recommendation: %+v", r)
	}

	// A drafted reason is invisible publicly until the owner publishes it.
	const words = "Drafted by the agent: <go> early & stay late"
	reply := mcpTool(t, base, access, "draft_recommendation", map[string]any{"event_id": draftEvent, "reason": words})
	var drafted struct {
		Draft     struct{ ID string }
		ReviewURL string `json:"review_url"`
		Published bool
	}
	if reply.IsError || json.Unmarshal(reply.Content, &drafted) != nil || drafted.Published || drafted.ReviewURL != base+"/me/drafts" {
		t.Fatalf("draft: %+v", reply)
	}
	draftRecommendations := "/api/v1/events/" + url.PathEscape(draftEvent) + "/recommendations"
	for _, path := range []string{draftRecommendations, "/api/v1/community/recommendations", "/api/v1/users/" + owner.ID + "/event-recommendations"} {
		if strings.Contains(publicJSON(t, base, path), "Drafted by the agent") {
			t.Fatalf("draft text public at %s", path)
		}
	}
	if strings.Contains(get(t, m, "other", "/me/drafts"), "Drafted by the agent") ||
		strings.Contains(string(mcpTool(t, base, m.Accounts["other"].APIToken, "my_drafts", map[string]any{}).Content), drafted.Draft.ID) {
		t.Fatal("another account sees the draft")
	}
	page := get(t, m, "owner", "/me/drafts")
	if !strings.Contains(page, html.EscapeString(words)) || !strings.Contains(page, "Review Agent") {
		t.Fatal("drafts page lacks the draft")
	}
	// The form for the drafted event carries the CSRF token and the
	// (absent) recommendation the owner reviewed.
	fields := url.Values{}
	for _, match := range formInput.FindAllStringSubmatch(page[strings.Index(page, "/me/drafts/"+drafted.Draft.ID):], -1) {
		if fields.Get(match[1]) == "" {
			fields.Set(match[1], html.UnescapeString(match[2]))
		}
	}
	if fields.Get("csrf_token") == "" || fields.Get("has_current") != "0" {
		t.Fatalf("drafts page form: %v", fields)
	}
	// Another account cannot publish it, even with its own CSRF token.
	var me struct {
		CSRF string `json:"csrf_token"`
	}
	if err := json.Unmarshal([]byte(get(t, m, "other", "/api/v1/me")), &me); err != nil || me.CSRF == "" {
		t.Fatalf("other CSRF: %v", err)
	}
	for _, action := range []string{"publish", "discard"} {
		if status, _, _ := signedIn(t, m, "other").do(http.MethodPost, "/me/drafts/"+drafted.Draft.ID, url.Values{"action": {action}, "csrf_token": {me.CSRF}, "has_current": {"0"}}); action == "publish" && status != http.StatusNotFound {
			t.Fatalf("other published the draft: %d", status)
		}
	}
	if !strings.Contains(get(t, m, "owner", "/me/drafts"), html.EscapeString(words)) {
		t.Fatal("another account discarded the draft")
	}
	fields.Set("action", "publish")
	status, header, _ := b.do(http.MethodPost, "/me/drafts/"+drafted.Draft.ID, fields)
	if status != http.StatusSeeOther || !strings.HasPrefix(header.Get("Location"), "/me/drafts?saved=published") {
		t.Fatalf("publish: %d %v", status, header)
	}
	var published struct {
		Items []struct {
			Reason, Via string
			ViaHost     string `json:"via_host"`
		}
	}
	if err := json.Unmarshal([]byte(publicJSON(t, base, draftRecommendations)), &published); err != nil || len(published.Items) != 1 ||
		published.Items[0].Reason != words || published.Items[0].Via != "Review Agent" || published.Items[0].ViaHost != "review-agent.example" {
		t.Fatalf("published draft: %+v %v", published, err)
	}
	if strings.Contains(get(t, m, "owner", "/me/drafts"), "Drafted by the agent") {
		t.Fatal("published draft still pending")
	}

	// Disconnecting the app deletes its pending drafts.
	reply = mcpTool(t, base, access, "draft_recommendation", map[string]any{"event_id": draftEvent, "reason": "Pending when disconnected"})
	if reply.IsError || !strings.Contains(get(t, m, "owner", "/me/drafts"), "Pending when disconnected") {
		t.Fatalf("draft before disconnect: %+v", reply)
	}
	me.CSRF = ""
	if err := json.Unmarshal([]byte(get(t, m, "owner", "/api/v1/me")), &me); err != nil {
		t.Fatal(err)
	}
	for _, id := range regexp.MustCompile(`/me/connections/([0-9a-f]+)/revoke`).FindAllStringSubmatch(get(t, m, "owner", "/me"), -1) {
		b.do(http.MethodPost, "/me/connections/"+id[1]+"/revoke", url.Values{"csrf_token": {me.CSRF}})
	}
	if page := get(t, m, "owner", "/me/drafts"); strings.Contains(page, "Pending when disconnected") || strings.Contains(page, "Small room, big sound") {
		t.Fatal("disconnected apps' drafts remain")
	}
	access = connectWithPublish(t, m, b)

	// Drafts are capped per account.
	pending := len(regexp.MustCompile(`"id":"[0-9a-f]{32}"`).FindAllString(string(mcpTool(t, base, access, "my_drafts", map[string]any{}).Content), -1))
	for i := pending; i < drafts.MaxPending; i++ {
		if r := mcpTool(t, base, access, "draft_recommendation", map[string]any{"event_id": draftEvent, "reason": fmt.Sprintf("Reason %d", i)}); r.IsError {
			t.Fatalf("draft %d: %+v", i, r)
		}
	}
	if r := mcpTool(t, base, access, "draft_recommendation", map[string]any{"event_id": draftEvent, "reason": "One too many"}); !r.IsError || !strings.Contains(r.Text, "20 drafts") {
		t.Fatalf("over the cap: %+v", r)
	}
	if r := mcpTool(t, base, m.Accounts["other"].APIToken, "draft_recommendation", map[string]any{"event_id": draftEvent, "reason": "Mine"}); r.IsError {
		t.Fatalf("cap is not per account: %+v", r)
	}
}
