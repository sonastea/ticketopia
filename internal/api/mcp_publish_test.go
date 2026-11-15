package api

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/appmcp"
	"github.com/sonastea/ticketopia/internal/drafts"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/oauth"
	"github.com/sonastea/ticketopia/internal/oauth/oauthtest"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
)

// draftStub is an in-memory drafts.Repository.
type draftStub struct {
	mu     sync.Mutex
	drafts map[string]map[string]drafts.Draft
}

func newDraftStub() *draftStub { return &draftStub{drafts: map[string]map[string]drafts.Draft{}} }

func (r *draftStub) Create(_ context.Context, _ *models.EventDetail, d drafts.Draft, account string, now time.Time) (drafts.Draft, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.drafts[account] == nil {
		r.drafts[account] = map[string]drafts.Draft{}
	}
	pending := 0
	for id, existing := range r.drafts[account] {
		if !existing.ExpiresAt.After(now) {
			delete(r.drafts[account], id)
			continue
		}
		pending++
	}
	if pending >= drafts.MaxPending {
		return drafts.Draft{}, drafts.ErrLimit
	}
	r.drafts[account][d.ID] = d
	return d, nil
}
func (r *draftStub) List(_ context.Context, account string, now time.Time) ([]drafts.Draft, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := []drafts.Draft{}
	for _, d := range r.drafts[account] {
		if d.ExpiresAt.After(now) {
			items = append(items, d)
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}
func (r *draftStub) Get(_ context.Context, account, id string, now time.Time) (drafts.Draft, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.drafts[account][id]
	if !ok || !d.ExpiresAt.After(now) {
		return drafts.Draft{}, drafts.ErrNotFound
	}
	return d, nil
}
func (r *draftStub) Delete(_ context.Context, account, id string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.drafts[account][id]
	delete(r.drafts[account], id)
	return ok, nil
}
func (r *draftStub) Count(ctx context.Context, account string, now time.Time) (int, error) {
	items, _ := r.List(ctx, account, now)
	return len(items), nil
}

type publishFixture struct {
	*oauthFixture
	recommendations *recommendationStub
	interests       *interestStub
	drafts          *draftStub
	connections     int
}

// newPublishFixture is the OAuth fixture with Interested, recommendation and
// draft services, wired before routes are built.
func newPublishFixture(t *testing.T, oauthOn bool) *publishFixture {
	t.Helper()
	base, repo, browser, bearer, csrf := accountAPI(t)
	event := models.Event{ID: "ticketmaster:Agent", Name: "Agent show", Source: models.Source{Provider: "ticketmaster", ID: "Agent"}}
	store := &savedStub{items: map[string]map[string]saved.Item{}, detail: models.EventDetail{Item: event, Meta: models.Freshness{DataAsOf: time.Now()}}}
	grants := oauthtest.NewRepository()
	clients := oauthtest.Clients{agentClientID: {ID: agentClientID, Name: "Test Agent", RedirectURIs: []string{"http://127.0.0.1/callback"}, Refresh: true}}
	app := mcpAPI(t, WithAccounts(base.authConfig, base.accounts), WithSavedEvents(store),
		WithMCP(appmcp.Config{Enabled: true, OAuth: oauthOn, RatePerMinute: 600, MaxInFlight: 8}),
		WithMCPOAuth(oauth.New(grants, clients, oauthBase)))
	f := &publishFixture{
		recommendations: &recommendationStub{items: map[string]map[string]recommendations.Item{}, profiles: map[string]accounts.PublicProfile{repo.account.ID: {ID: repo.account.ID, DisplayName: repo.account.DisplayName}}},
		interests:       &interestStub{items: map[string]map[string]interests.Item{}, profiles: map[string]accounts.PublicProfile{}},
		drafts:          newDraftStub(),
	}
	f.recommendations.consume = func(owner, id string) bool {
		f.drafts.mu.Lock()
		defer f.drafts.mu.Unlock()
		if _, ok := f.drafts.drafts[owner][id]; !ok {
			return false
		}
		delete(f.drafts.drafts[owner], id)
		return true
	}
	app.recommendations = recommendations.New(f.recommendations, app.saved)
	app.interests = interests.New(f.interests, app.saved)
	app.drafts = drafts.New(f.drafts, app.saved, draftThreads{app})
	f.oauthFixture = &oauthFixture{app: app, routes: app.Routes(), grants: grants, browser: browser, csrf: csrf, apiToken: bearer, savedEvent: event.ID, accountStorage: repo, store: store}
	return f
}

// connect stores a connection for account with scopes and returns its access token.
func (f *publishFixture) connect(account string, scopes ...string) string {
	f.connections++
	access := fmt.Sprintf("%sfixture-%d", oauth.AccessPrefix, f.connections)
	f.grants.CreateGrant(oauth.Grant{ID: fmt.Sprintf("%032x", f.connections), AccountID: account, ClientID: agentClientID, ClientName: "Test Agent",
		Scopes: scopes, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), AccessHash: oauth.Hash(access), AccessUntil: time.Now().Add(time.Hour)}, time.Now())
	return access
}

func toolCall(name, args string) string {
	return `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"` + name + `","arguments":` + args + `}}`
}

// API tokens carry read and write only, never publish, whether or not OAuth
// is on: publishing tools get a 403 and reach no service.
func TestMCPAPITokensCannotPublish(t *testing.T) {
	if slices.Contains(apiTokenScopes, oauth.ScopePublish) {
		t.Fatal("API tokens hold publish")
	}
	for _, oauthOn := range []bool{true, false} {
		f := newPublishFixture(t, oauthOn)
		bearer := map[string]string{"Authorization": "Bearer " + f.apiToken}
		for _, name := range []string{"make_interest_public", "recommend_event", "withdraw_recommendation"} {
			r := mcpPost(t, f.routes, toolCall(name, `{"event_id":"`+f.savedEvent+`"}`), bearer)
			if r.Code != http.StatusForbidden || !strings.Contains(r.Header().Get("WWW-Authenticate"), `error="insufficient_scope"`) ||
				!strings.Contains(r.Body.String(), "API tokens can't post publicly") {
				t.Fatalf("oauth=%v %s: %d %v %s", oauthOn, name, r.Code, r.Header(), r.Body.String())
			}
		}
		if len(f.recommendations.items) != 0 || len(f.interests.items) != 0 {
			t.Fatalf("API token publish reached services: %+v %+v", f.recommendations.items, f.interests.items)
		}
		// Drafting needs only write, which API tokens hold.
		r := mcpPost(t, f.routes, toolCall("draft_recommendation", `{"event_id":"`+f.savedEvent+`","reason":"Great room"}`), bearer)
		if r.Code != http.StatusOK || strings.Contains(r.Body.String(), `"isError":true`) || !strings.Contains(r.Body.String(), "https://events.example/me/drafts") {
			t.Fatalf("oauth=%v API token draft: %d %s", oauthOn, r.Code, r.Body.String())
		}
	}
}

// The boundary asks for publish only for publishing tools: anonymous calls
// get a 401 naming it, and connections without it a 403 insufficient_scope.
func TestMCPPublishChallengesNamePublish(t *testing.T) {
	f := newPublishFixture(t, true)
	call := toolCall("recommend_event", `{"event_id":"`+f.savedEvent+`"}`)
	r := mcpPost(t, f.routes, call, nil)
	if r.Code != http.StatusUnauthorized || !strings.Contains(r.Header().Get("WWW-Authenticate"), `scope="read write publish"`) {
		t.Fatalf("anonymous: %d %v", r.Code, r.Header())
	}
	r = mcpPost(t, f.routes, toolCall("my_saved_events", `{}`), nil)
	if !strings.Contains(r.Header().Get("WWW-Authenticate"), `scope="read write"`) {
		t.Fatalf("ordinary sign-in offered publish: %v", r.Header())
	}
	for _, scopes := range [][]string{{oauth.ScopeRead}, {oauth.ScopeRead, oauth.ScopeWrite}} {
		access := f.connect(f.accountStorage.account.ID, scopes...)
		r := mcpPost(t, f.routes, call, map[string]string{"Authorization": "Bearer " + access})
		challenge := r.Header().Get("WWW-Authenticate")
		if r.Code != http.StatusForbidden || !strings.Contains(challenge, `error="insufficient_scope"`) || !strings.Contains(challenge, `scope="read write publish"`) {
			t.Fatalf("%v: %d %q", scopes, r.Code, challenge)
		}
	}
	if len(f.recommendations.items) != 0 {
		t.Fatal("refused publish reached recommendations")
	}
	// With publish, the recommendation is published and attributed.
	access := f.connect(f.accountStorage.account.ID, oauth.ScopeRead, oauth.ScopePublish)
	r = mcpPost(t, f.routes, call, map[string]string{"Authorization": "Bearer " + access})
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"via":"Test Agent"`) {
		t.Fatalf("publish: %d %s", r.Code, r.Body.String())
	}
	if got := f.recommendations.items[f.accountStorage.account.ID][f.savedEvent]; got.Via != "Test Agent" || got.Reason != "" {
		t.Fatalf("stored %+v", got)
	}
}

// At most PublishesPerHour publishing calls per account per replica; other
// accounts keep their own allowance.
// The public host is the canonical ASCII form, so lookalike Unicode shows.
func TestAttributionHostIsASCII(t *testing.T) {
	if got := attributionHost("https://BÜCHER.example/client.json"); got != "xn--bcher-kva.example" {
		t.Fatalf("host %q", got)
	}
	if got := attributionHost("https://agent.example/client.json"); got != "agent.example" {
		t.Fatalf("host %q", got)
	}
}

func TestMCPPublishLimitPerAccount(t *testing.T) {
	f := newPublishFixture(t, true)
	first := f.connect(f.accountStorage.account.ID, oauth.ScopeRead, oauth.ScopePublish)
	second := f.connect(accounts.ID(), oauth.ScopeRead, oauth.ScopeWrite, oauth.ScopePublish)
	call := toolCall("withdraw_recommendation", `{"event_id":"`+f.savedEvent+`"}`)
	for i := range appmcp.PublishesPerHour {
		if r := mcpPost(t, f.routes, call, map[string]string{"Authorization": "Bearer " + first}); strings.Contains(r.Body.String(), `"isError":true`) {
			t.Fatalf("call %d: %s", i, r.Body.String())
		}
	}
	r := mcpPost(t, f.routes, call, map[string]string{"Authorization": "Bearer " + first})
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"isError":true`) || !strings.Contains(r.Body.String(), "Retry after") {
		t.Fatalf("over the limit: %d %s", r.Code, r.Body.String())
	}
	if r := mcpPost(t, f.routes, call, map[string]string{"Authorization": "Bearer " + second}); strings.Contains(r.Body.String(), `"isError":true`) {
		t.Fatalf("another account limited: %s", r.Body.String())
	}
}

// The consent page offers posting publicly only when the app asks, unchecked;
// publish is granted only with the box ticked, and the account page labels
// such connections.
func TestOAuthConsentPublishCheckbox(t *testing.T) {
	f := newPublishFixture(t, true)
	request := func(scope string) string {
		v := url.Values{"response_type": {"code"}, "client_id": {agentClientID}, "redirect_uri": {agentRedirect}, "state": {"s1"},
			"code_challenge": {strings.Repeat("c", 43)}, "code_challenge_method": {"S256"}, "scope": {scope}, "resource": {oauthBase + "/mcp"}}
		return oauth.AuthorizePath + "?" + v.Encode()
	}
	page := f.browserRequest(t, http.MethodGet, request("read write"), nil).Body.String()
	if strings.Contains(page, `name="publish"`) || !strings.Contains(page, "or post anything publicly") || strings.Contains(page, "connect-public") {
		t.Fatalf("consent without publish:\n%s", page)
	}
	page = f.browserRequest(t, http.MethodGet, request("read write publish"), nil).Body.String()
	checkbox := regexp.MustCompile(`<input type="checkbox" name="publish"[^>]*>`).FindString(page)
	if checkbox == "" || strings.Contains(checkbox, "checked") || !strings.Contains(page, "Also allow posting publicly as Event explorer") ||
		!strings.Contains(page, "connect-public-off") || !strings.Contains(page, "connect-public-on") || !strings.Contains(page, "connect-public-either") {
		t.Fatalf("consent with publish:\n%s", page)
	}
	decide := func(publish bool) []string {
		t.Helper()
		form := url.Values{"decision": {"allow"}, "write": {"on"}}
		for _, m := range hiddenInput.FindAllStringSubmatch(page, -1) {
			form.Set(m[1], html.UnescapeString(m[2]))
		}
		if publish {
			form.Set("publish", "on")
		}
		before := map[[32]byte]bool{}
		for hash := range f.grants.Codes {
			before[hash] = true
		}
		if r := f.browserRequest(t, http.MethodPost, oauth.AuthorizePath, form); r.Code != http.StatusSeeOther || len(f.grants.Codes) != len(before)+1 {
			t.Fatalf("decision: %d", r.Code)
		}
		for hash, code := range f.grants.Codes {
			if !before[hash] {
				return code.Scopes
			}
		}
		t.Fatal("no new code")
		return nil
	}
	if scopes := decide(false); slices.Contains(scopes, oauth.ScopePublish) {
		t.Fatalf("publish without the checkbox: %v", scopes)
	}
	if scopes := decide(true); !slices.Equal(scopes, []string{"read", "write", "publish"}) {
		t.Fatalf("publish with the checkbox: %v", scopes)
	}
	// The connected apps list labels a connection that can post publicly.
	f.connect(f.accountStorage.account.ID, oauth.ScopeRead, oauth.ScopePublish)
	if account := f.browserRequest(t, http.MethodGet, "/me", nil).Body.String(); !strings.Contains(account, "Can post publicly") || strings.Contains(account, "Read-only") {
		t.Fatalf("account page publish label:\n%s", account)
	}
}

// A drafted reason is private until the person publishes it on /me/drafts
// with a CSRF-protected POST; it then carries the app's name and host. The
// page shows the reason a draft would replace, and publishing refuses if that
// changed after the page was read. Reading never publishes, drafts are per
// account, and a draft publishes at most once.
func TestDraftsPagePublishAndDiscard(t *testing.T) {
	f := newPublishFixture(t, true)
	owner := f.accountStorage.account.ID
	bearer := map[string]string{"Authorization": "Bearer " + f.connect(owner, oauth.ScopeRead, oauth.ScopeWrite)}
	for _, reason := range []string{"<b>Loud</b> & worth it", "Second thoughts"} {
		r := mcpPost(t, f.routes, toolCall("draft_recommendation", `{"event_id":"`+f.savedEvent+`","reason":"`+strings.ReplaceAll(reason, `"`, `\"`)+`"}`), bearer)
		if r.Code != http.StatusOK || strings.Contains(r.Body.String(), `"isError":true`) {
			t.Fatalf("draft: %d %s", r.Code, r.Body.String())
		}
	}
	// API-token drafts identify no app.
	r := mcpPost(t, f.routes, toolCall("draft_recommendation", `{"event_id":"`+f.savedEvent+`","reason":"From a script"}`), map[string]string{"Authorization": "Bearer " + f.apiToken})
	if strings.Contains(r.Body.String(), `"isError":true`) || !strings.Contains(r.Body.String(), `"client_name":"an API client"`) || strings.Contains(r.Body.String(), `"client_host"`) {
		t.Fatalf("API token draft: %s", r.Body.String())
	}
	if len(f.recommendations.items) != 0 {
		t.Fatal("drafting published")
	}
	// The person's own reason for the event, which a draft would replace.
	f.recommendations.items[owner] = map[string]recommendations.Item{f.savedEvent: {Event: models.Event{ID: f.savedEvent}, Reason: "My <own>\nwords"}}
	page := f.browserRequest(t, http.MethodGet, "/me/drafts", nil)
	body := page.Body.String()
	for _, want := range []string{"&lt;b&gt;Loud&lt;/b&gt; &amp; worth it", "Test Agent", "(<span class=\"account-connect-host\">agent.example</span>)",
		"App unidentified; created using an API token", "Publishing replaces your current reason:", "My &lt;own&gt;\nwords",
		`<input type="hidden" name="current_hash" value="` + recommendations.ReasonHash("My <own>\nwords") + `">`,
		`<input type="hidden" name="has_current" value="1">`, `class="account-nav-count">3`} {
		if !strings.Contains(body, want) {
			t.Errorf("drafts page lacks %s", want)
		}
	}
	if page.Code != http.StatusOK || strings.Contains(body, "<b>Loud</b>") {
		t.Fatalf("drafts page: %d\n%s", page.Code, body)
	}
	var loud, second, script string
	for id, d := range f.drafts.drafts[owner] {
		switch {
		case strings.HasPrefix(d.Text, "<b>"):
			loud = id
		case d.Text == "Second thoughts":
			second = id
		default:
			script = id
		}
	}
	publish := func(id string, has, current string) *httptest.ResponseRecorder {
		t.Helper()
		return f.browserRequest(t, http.MethodPost, "/me/drafts/"+id, url.Values{"action": {"publish"}, "csrf_token": {f.csrf}, "has_current": {has}, "current_hash": {recommendations.ReasonHash(current)}})
	}
	// Without CSRF, or as another account, nothing happens.
	if r := f.browserRequest(t, http.MethodPost, "/me/drafts/"+loud, url.Values{"action": {"publish"}, "has_current": {"1"}, "current_hash": {recommendations.ReasonHash("My <own> words")}}); r.Code != http.StatusForbidden {
		t.Fatalf("publish without CSRF: %d", r.Code)
	}
	f.drafts.drafts["someone-else"] = map[string]drafts.Draft{strings.Repeat("e", 32): {ID: strings.Repeat("e", 32), EventID: f.savedEvent, Text: "Not yours", ExpiresAt: time.Now().Add(time.Hour)}}
	if r := publish(strings.Repeat("e", 32), "1", "My <own>\nwords"); r.Code != http.StatusNotFound {
		t.Fatalf("publish another account's draft: %d", r.Code)
	}
	// A review that no longer matches publishes nothing and keeps the draft.
	if r := publish(loud, "0", ""); r.Code != http.StatusSeeOther || r.Header().Get("Location") != "/me/drafts?saved=changed" {
		t.Fatalf("stale review: %d %v", r.Code, r.Header())
	}
	if r := publish(loud, "1", "Something else"); r.Header().Get("Location") != "/me/drafts?saved=changed" ||
		f.recommendations.items[owner][f.savedEvent].Reason != "My <own>\nwords" || f.drafts.drafts[owner][loud].ID == "" {
		t.Fatalf("stale reason: %v %+v", r.Header(), f.recommendations.items[owner])
	}
	if !strings.Contains(f.browserRequest(t, http.MethodGet, "/me/drafts?saved=changed", nil).Body.String(), "changed after you opened this page") {
		t.Fatal("changed notice missing")
	}
	// The hash survives the form whatever its line endings: a multiline
	// reason publishes without a false "changed".
	r = publish(loud, "1", "My <own>\nwords")
	if r.Code != http.StatusSeeOther || r.Header().Get("Location") != "/me/drafts?saved=published&event_id="+url.QueryEscape(f.savedEvent) {
		t.Fatalf("publish: %d %v %s", r.Code, r.Header(), r.Body.String())
	}
	got := f.recommendations.items[owner][f.savedEvent]
	if got.Reason != "<b>Loud</b> & worth it" || got.Via != "Test Agent" || got.ViaHost != "agent.example" {
		t.Fatalf("published %+v", got)
	}
	if _, ok := f.drafts.drafts[owner][loud]; ok {
		t.Fatal("published draft kept")
	}
	// A repeated submission finds the draft gone and cannot republish it.
	if r := publish(loud, "1", got.Reason); r.Code != http.StatusNotFound {
		t.Fatalf("replayed publish: %d", r.Code)
	}
	// A failed publication leaves the draft for a retry.
	f.recommendations.unavailable = true
	if r := publish(second, "1", got.Reason); r.Code != http.StatusServiceUnavailable {
		t.Fatalf("failed publish: %d", r.Code)
	}
	f.recommendations.unavailable = false
	if _, ok := f.drafts.drafts[owner][second]; !ok {
		t.Fatal("failed publish lost the draft")
	}
	// An API-token draft publishes as "an API client", without a host.
	if r := publish(script, "1", got.Reason); r.Code != http.StatusSeeOther {
		t.Fatalf("publish API token draft: %d", r.Code)
	}
	if got := f.recommendations.items[owner][f.savedEvent]; got.Via != "an API client" || got.ViaHost != "" {
		t.Fatalf("API token attribution: %+v", got)
	}
	if r := f.browserRequest(t, http.MethodPost, "/me/drafts/"+second, url.Values{"action": {"discard"}, "csrf_token": {f.csrf}}); r.Code != http.StatusSeeOther {
		t.Fatalf("discard: %d", r.Code)
	}
	if len(f.drafts.drafts[owner]) != 0 || f.recommendations.items[owner][f.savedEvent].Reason != "From a script" {
		t.Fatalf("discard changed more than the draft: %+v", f.recommendations.items)
	}
	if body := f.browserRequest(t, http.MethodGet, "/me/drafts?saved=published&event_id="+url.QueryEscape(f.savedEvent), nil).Body.String(); !strings.Contains(body, "No drafts waiting") ||
		!strings.Contains(body, "Your recommendation was published.") || strings.Contains(body, "account-nav-count") {
		t.Fatalf("after decisions:\n%s", body)
	}
	// Signed out, the page asks for sign-in.
	if r := f.do(t, http.MethodGet, "/me/drafts", "", nil); r.Code != http.StatusSeeOther || !strings.HasPrefix(r.Header().Get("Location"), "/auth/sign-in") {
		t.Fatalf("signed out: %d %v", r.Code, r.Header())
	}
}
