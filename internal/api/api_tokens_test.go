package api

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/drafts"
)

// readOnlyToken stores a read-only API token for the fixture's account.
func readOnlyToken(f *publishFixture) string { return scopedToken(f, 3, accounts.ScopeRead) }

// scopedToken stores an API token with scopes for the fixture's account.
func scopedToken(f *publishFixture, seed byte, scopes ...string) string {
	raw := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{seed}, 32))
	hash := accounts.Hash(raw)
	f.accountStorage.credentials[hash] = accounts.Credential{ID: accounts.ID(), AccountID: f.accountStorage.account.ID, Hash: hash, Kind: "api", Name: "scoped",
		CreatedAt: time.Now().UTC(), ExpiresAt: time.Now().Add(time.Hour), Scopes: scopes}
	return raw
}

func problemCode(t *testing.T, body []byte) string {
	t.Helper()
	var problem struct{ Code string }
	if err := json.Unmarshal(body, &problem); err != nil {
		t.Fatal(err, string(body))
	}
	return problem.Code
}

// A read-only token reads and revokes itself but changes nothing, on the
// JSON API and on /mcp.
func TestReadOnlyAPITokenChangesNothing(t *testing.T) {
	f := newPublishFixture(t, true)
	token := readOnlyToken(f)
	if w := accountRequest(f.app, "GET", "/api/v1/me", "", "", token, "", "", ""); w.Code != 200 {
		t.Fatal("read", w.Code, w.Body.String())
	}
	for _, tc := range []struct{ method, path, body string }{
		{"PUT", "/api/v1/me/saved-events/" + f.savedEvent, ""},
		{"PATCH", "/api/v1/me/preferences", `{"city":"Berlin"}`},
		{"PUT", "/api/v1/me/event-interests/" + f.savedEvent, `{}`},
		{"POST", "/api/v1/me/drafts", `{"kind":"recommendation","event_id":"` + f.savedEvent + `","text":"Great"}`},
	} {
		w := accountRequest(f.app, tc.method, tc.path, tc.body, "", token, "", "", "")
		if w.Code != 403 || problemCode(t, w.Body.Bytes()) != "insufficient_scope" {
			t.Fatal(tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	if len(f.store.items) != 0 || len(f.interests.items) != 0 || f.accountStorage.account.Preferences.City != "Secret city" {
		t.Fatal("read-only token changed state")
	}
	r := mcpPost(t, f.routes, toolCall("save_event", `{"event_id":"`+f.savedEvent+`"}`), map[string]string{"Authorization": "Bearer " + token})
	if r.Code != http.StatusForbidden || !strings.Contains(r.Body.String(), "this API token can only read") {
		t.Fatal("MCP write with a read-only token", r.Code, r.Body.String())
	}
	if r := mcpPost(t, f.routes, toolCall("my_saved_events", `{}`), map[string]string{"Authorization": "Bearer " + token}); r.Code != http.StatusOK || strings.Contains(r.Body.String(), `"isError":true`) {
		t.Fatal("MCP read with a read-only token", r.Code, r.Body.String())
	}
	if w := accountRequest(f.app, "DELETE", "/api/v1/me/session", "", "", token, "", "", ""); w.Code != 204 {
		t.Fatal("read-only token couldn't revoke itself", w.Code, w.Body.String())
	}
	if w := accountRequest(f.app, "GET", "/api/v1/me", "", "", token, "", "", ""); w.Code != 401 {
		t.Fatal("revoked token still works", w.Code)
	}
}

// A token allowed changes but not posting never acts publicly: its profile
// writes are refused and its new Interested choices stay private whatever
// the default.
func TestWriteAPITokenStaysPrivate(t *testing.T) {
	f := newPublishFixture(t, true)
	f.accountStorage.account.InterestVisibility = "public"
	if w := accountRequest(f.app, "PATCH", "/api/v1/me/profile", `{"bio":"Buy followers"}`, "", f.apiToken, "", "", ""); w.Code != 403 || problemCode(t, w.Body.Bytes()) != "browser_session_required" || f.accountStorage.account.Bio != "" {
		t.Fatal("token changed public profile", w.Code, w.Body.String())
	}
	w := accountRequest(f.app, "PUT", "/api/v1/me/event-interests/"+f.savedEvent, `{}`, "", f.apiToken, "", "", "")
	if w.Code != 201 || f.interests.items[f.accountStorage.account.ID][f.savedEvent].Visibility != "private" {
		t.Fatal("token interest followed the public default", w.Code, w.Body.String())
	}
}

// API tokens propose public text as drafts the person publishes on the web.
func TestAPITokenDrafts(t *testing.T) {
	f := newPublishFixture(t, true)
	f.app.discussions = discussions.New(&discussionStub{}, f.app.saved)
	path := "/api/v1/me/drafts"
	w := accountRequest(f.app, "POST", path, `{"kind":"recommendation","event_id":"`+f.savedEvent+`","text":"Great room"}`, "", f.apiToken, "", "", "")
	var created struct {
		Item      drafts.Draft `json:"item"`
		Published bool         `json:"published"`
		ReviewURL string       `json:"review_url"`
	}
	if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &created) != nil || created.Published || created.ReviewURL != "https://events.example/me/drafts" ||
		created.Item.ClientName != drafts.APIClientName || created.Item.Kind != drafts.KindRecommendation {
		t.Fatal("recommendation draft", w.Code, w.Body.String())
	}
	if len(f.recommendations.items) != 0 {
		t.Fatal("a draft was published")
	}
	if w = accountRequest(f.app, "POST", path, `{"kind":"question","event_id":"`+f.savedEvent+`","text":"Doors at 7?"}`, "", f.apiToken, "", "", ""); w.Code != 201 {
		t.Fatal("question draft", w.Code, w.Body.String())
	}
	for body, field := range map[string]string{
		`{"kind":"post","event_id":"` + f.savedEvent + `","text":"x"}`:                                               "kind",
		`{"kind":"recommendation","event_id":"` + f.savedEvent + `","text":"x","thread_id":"` + accounts.ID() + `"}`: "thread_id",
		`{"kind":"question","event_id":"` + f.savedEvent + `","text":"x","parent_id":"` + accounts.ID() + `"}`:       "parent_id",
		`{"kind":"recommendation","event_id":"` + f.savedEvent + `","text":""}`:                                      "text",
		`{"kind":"question","event_id":"` + f.savedEvent + `","text":""}`:                                            "text",
		`{"kind":"recommendation","event_id":"` + f.savedEvent + `","text":"x","published":true}`:                    "body",
	} {
		w := accountRequest(f.app, "POST", path, body, "", f.apiToken, "", "", "")
		if w.Code != 400 || !strings.Contains(w.Body.String(), `"fields":{"`+field+`"`) || strings.Contains(w.Body.String(), "recommend_event") {
			t.Fatal("invalid draft", body, w.Code, w.Body.String())
		}
	}
	// The website publishes directly; a browser's draft would be mislabelled.
	for _, csrf := range []string{f.csrf, ""} {
		if w := accountRequest(f.app, "POST", path, `{"kind":"question","event_id":"`+f.savedEvent+`","text":"Hi"}`, f.browser, "", csrf, "https://events.example", ""); w.Code != 400 {
			t.Fatal("browser drafted", w.Code, w.Body.String())
		}
	}
	var list draftList
	w = accountRequest(f.app, "GET", path, "", "", f.apiToken, "", "", "")
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Items) != 2 || list.ReviewURL != "https://events.example/me/drafts" {
		t.Fatal("draft list", w.Code, w.Body.String())
	}
	for range 2 {
		if w := accountRequest(f.app, "DELETE", path+"/"+created.Item.ID, "", "", f.apiToken, "", "", ""); w.Code != 204 {
			t.Fatal("discard", w.Code, w.Body.String())
		}
	}
	if w = accountRequest(f.app, "GET", path, "", "", f.apiToken, "", "", ""); json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Items) != 1 {
		t.Fatal("discarded draft remains", w.Body.String())
	}
}

// Tokens are read-only unless the person allows private changes, and post
// only when also allowed to, on the JSON API and the Security page alike.
func TestCreateAPITokenScopes(t *testing.T) {
	a, repo, browser, _, csrf := accountAPI(t)
	for body, want := range map[string][]string{
		`{"name":"Reader"}`:                           {"read"},
		`{"name":"Reader","scopes":["read"]}`:         {"read"},
		`{"name":"Writer","scopes":["write"]}`:        {"read", "write"},
		`{"name":"Writer","scopes":["read","write"]}`: {"read", "write"},
		`{"name":"Poster","scopes":["publish"]}`:      {"read", "write", "publish"},
	} {
		w := accountRequest(a, "POST", "/api/v1/me/tokens", body, browser, "", csrf, "https://events.example", "")
		var created struct{ Item accounts.Credential }
		if w.Code != 201 || json.Unmarshal(w.Body.Bytes(), &created) != nil || !slices.Equal(created.Item.Scopes, want) {
			t.Fatal(body, w.Code, w.Body.String())
		}
	}
	if w := accountRequest(a, "POST", "/api/v1/me/tokens", `{"name":"Admin","scopes":["read","admin"]}`, browser, "", csrf, "https://events.example", ""); w.Code != 400 {
		t.Fatal("unknown scope accepted", w.Code, w.Body.String())
	}
	for _, tc := range []struct{ write, publish bool }{{false, false}, {true, false}, {false, true}} {
		form := url.Values{"name": {"Form token"}, "csrf_token": {csrf}}
		if tc.write {
			form.Set("write", "on")
		}
		if tc.publish {
			form.Set("publish", "on")
		}
		before := len(repo.credentials)
		if w := accountRequest(a, "POST", "/me/tokens", form.Encode(), browser, "", "", "https://events.example", "application/x-www-form-urlencoded"); w.Code != 303 || len(repo.credentials) != before+1 {
			t.Fatal("form token", w.Code, w.Body.String())
		}
		newest := accounts.Credential{}
		for _, c := range repo.credentials {
			if c.Name == "Form token" && c.CreatedAt.After(newest.CreatedAt) {
				newest = c
			}
		}
		if newest.Can(accounts.ScopeWrite) != (tc.write || tc.publish) || newest.Can(accounts.ScopePublish) != tc.publish || !newest.Can(accounts.ScopeRead) {
			t.Fatal("form scopes", tc, newest.Scopes)
		}
		for _, c := range repo.credentials {
			if c.Name == "Form token" {
				delete(repo.credentials, c.Hash)
			}
		}
	}
}

// A token allowed to post acts publicly as the person on /api/v1: posts and
// recommendations carry "an API client", a recommendation the person wrote
// stays theirs, drafts publish, edits and the profile stay on the web, and
// public actions share the hourly allowance with /mcp.
func TestPublishingAPIToken(t *testing.T) {
	f := newPublishFixture(t, true)
	f.app.discussions = discussions.New(&discussionStub{}, f.app.saved)
	f.routes = f.app.Routes()
	token := scopedToken(f, 4, accounts.ScopeRead, accounts.ScopeWrite, accounts.ScopePublish)
	owner := f.accountStorage.account.ID
	event := f.savedEvent

	w := discussionRequest(f.app, "POST", "/api/v1/events/"+event+"/discussions", `{"body":"Doors at seven?"}`, token, accounts.ID())
	if w.Code != 201 || !strings.Contains(w.Body.String(), `"via":"an API client"`) {
		t.Fatal("token question", w.Code, w.Body.String())
	}
	var post discussions.Post
	_ = json.Unmarshal(w.Body.Bytes(), &post)
	for _, tc := range []struct{ method, path, body string }{{"PATCH", "/api/v1/posts/" + post.ID, `{"body":"changed"}`}, {"DELETE", "/api/v1/posts/" + post.ID, ""}} {
		if w := discussionRequest(f.app, tc.method, tc.path, tc.body, token, ""); w.Code != 403 || problemCode(t, w.Body.Bytes()) != "browser_session_required" {
			t.Fatal(tc.method, "by a publishing token", w.Code, w.Body.String())
		}
	}

	path := "/api/v1/me/event-recommendations/" + event
	if w := accountRequest(f.app, "PUT", path, `{"reason":"Great room"}`, "", token, "", "", ""); w.Code != 201 || !strings.Contains(w.Body.String(), `"via":"an API client"`) || !strings.Contains(w.Body.String(), "Great room") {
		t.Fatal("token recommendation", w.Code, w.Body.String())
	}
	// The person's own words stay: a token's PUT returns them unchanged and
	// its DELETE refuses.
	item := f.recommendations.items[owner][event]
	item.Reason, item.Via = "My own words", ""
	f.recommendations.items[owner][event] = item
	if w := accountRequest(f.app, "PUT", path, `{"reason":"Replace"}`, "", token, "", "", ""); w.Code != 200 || !strings.Contains(w.Body.String(), "My own words") {
		t.Fatal("token replaced the person's reason", w.Code, w.Body.String())
	}
	if w := accountRequest(f.app, "DELETE", path, "", "", token, "", "", ""); w.Code != 409 || problemCode(t, w.Body.Bytes()) != "recommendation_is_personal" {
		t.Fatal("token withdrew the person's recommendation", w.Code, w.Body.String())
	}

	if w := accountRequest(f.app, "PUT", "/api/v1/me/event-interests/"+event, `{"visibility":"public"}`, "", token, "", "", ""); w.Code != 201 || f.interests.items[owner][event].Visibility != "public" {
		t.Fatal("token public interest", w.Code, w.Body.String())
	}

	delete(f.recommendations.items[owner], event)
	w = accountRequest(f.app, "POST", "/api/v1/me/drafts", `{"kind":"recommendation","event_id":"`+event+`","text":"Drafted reason"}`, "", token, "", "", "")
	var drafted struct{ Item drafts.Draft }
	_ = json.Unmarshal(w.Body.Bytes(), &drafted)
	w = accountRequest(f.app, "POST", "/api/v1/me/drafts/"+drafted.Item.ID+"/publish", "", "", token, "", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"published":true`) || f.recommendations.items[owner][event].Reason != "Drafted reason" {
		t.Fatal("publish draft", w.Code, w.Body.String())
	}
	if w := accountRequest(f.app, "POST", "/api/v1/me/drafts/"+drafted.Item.ID+"/publish", "", "", token, "", "", ""); w.Code != 404 {
		t.Fatal("published a draft twice", w.Code, w.Body.String())
	}
	// A token without publish can't publish its drafts either.
	if w := accountRequest(f.app, "POST", "/api/v1/me/drafts/"+drafted.Item.ID+"/publish", "", "", f.apiToken, "", "", ""); w.Code != 403 || problemCode(t, w.Body.Bytes()) != "token_cannot_publish" {
		t.Fatal("publish without publish", w.Code, w.Body.String())
	}

	// The allowance is shared with /mcp: spend the rest of the hour there,
	// through real tool calls, until the agent is told to wait.
	limited := false
	for range 25 {
		r := mcpPost(t, f.routes, toolCall("make_interest_public", `{"event_id":"`+event+`"}`), map[string]string{"Authorization": "Bearer " + token})
		if strings.Contains(r.Body.String(), "public actions an hour") {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("agents were never limited")
	}
	w = accountRequest(f.app, "PUT", "/api/v1/me/event-interests/"+event, `{"visibility":"public"}`, "", token, "", "", "")
	if w.Code != 429 || problemCode(t, w.Body.Bytes()) != "publish_limited" || w.Header().Get("Retry-After") == "" || w.Header().Get("Retry-After") == "600" || !strings.Contains(w.Body.String(), "limit of 20 public actions") {
		t.Fatal("hourly allowance", w.Code, w.Header(), w.Body.String())
	}
}

// Only public actions that happened count: refused or failed requests give
// the allowance back.
func TestFailedPublicActionsKeepTheAllowance(t *testing.T) {
	f := newPublishFixture(t, true)
	f.app.discussions = discussions.New(&discussionStub{}, f.app.saved)
	token := scopedToken(f, 6, accounts.ScopeRead, accounts.ScopeWrite, accounts.ScopePublish)
	path := "/api/v1/events/" + f.savedEvent + "/discussions"
	for range 25 {
		if w := discussionRequest(f.app, "POST", path, `{"body":"   "}`, token, accounts.ID()); w.Code != 400 {
			t.Fatal("blank post", w.Code, w.Body.String())
		}
	}
	if w := discussionRequest(f.app, "POST", path, `{"body":"Doors?"}`, token, accounts.ID()); w.Code != 201 {
		t.Fatal("allowance spent by failed posts", w.Code, w.Body.String())
	}
}
