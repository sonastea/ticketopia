package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/models"
)

type discussionStub struct {
	discussions.Repository
	post        discussions.Post
	input       discussions.Create
	unavailable bool
}

func (r *discussionStub) Snapshot(context.Context) (time.Time, error) { return time.Now().UTC(), nil }

func (r *discussionStub) Retry(_ context.Context, who, key string, hash [32]byte) (discussions.Post, error) {
	if r.unavailable {
		return discussions.Post{}, discussions.ErrUnavailable
	}
	if r.post.ID == "" || r.input.Owner != who || r.input.Key != key {
		return discussions.Post{}, discussions.ErrNotFound
	}
	if r.input.Hash != hash {
		return discussions.Post{}, discussions.ErrConflict
	}
	return r.post, nil
}
func (r *discussionStub) Create(_ context.Context, c discussions.Create, d *models.EventDetail) (discussions.Post, bool, error) {
	r.input = c
	now := time.Now().UTC()
	r.post = discussions.Post{ID: accounts.ID(), EventID: c.EventID, Body: c.Body, CreatedAt: now, UpdatedAt: now, Profile: &accounts.PublicProfile{ID: c.Owner, DisplayName: "Contributor"}}
	r.post.ThreadID = r.post.ID
	if d != nil {
		r.post.Event = d.Item
		r.post.Meta = d.Meta
	}
	return r.post, true, nil
}
func (r *discussionStub) Get(_ context.Context, viewer, id string) (discussions.Post, error) {
	if id != r.post.ID {
		return discussions.Post{}, discussions.ErrNotFound
	}
	return r.post, nil
}
func (r *discussionStub) List(context.Context, string, string, string, discussions.Query) ([]discussions.Post, int, error) {
	if r.unavailable {
		return nil, 0, discussions.ErrUnavailable
	}
	if r.post.ID == "" {
		return []discussions.Post{}, 0, nil
	}
	return []discussions.Post{r.post}, 1, nil
}
func (r *discussionStub) Edit(_ context.Context, who, id, body string) (discussions.Post, error) {
	if id != r.post.ID || who != r.input.Owner {
		return discussions.Post{}, discussions.ErrNotFound
	}
	r.post.Body = body
	return r.post, nil
}
func (r *discussionStub) Remove(_ context.Context, who, id string) error {
	if id != r.post.ID || who != r.input.Owner {
		return discussions.ErrNotFound
	}
	r.post.Removed = true
	r.post.Body = ""
	r.post.Profile = nil
	return nil
}
func (r *discussionStub) React(_ context.Context, who, id string, set bool) (discussions.Post, error) {
	if id != r.post.ID {
		return discussions.Post{}, discussions.ErrNotFound
	}
	r.post.Helpful = set
	r.post.HelpfulCount = 0
	if set {
		r.post.HelpfulCount = 1
	}
	return r.post, nil
}
func discussionAPI(t *testing.T) (*api, *discussionStub, string, string, string, string) {
	a, _, _, browser, bearer, csrf, id := recommendationAPI(t)
	r := &discussionStub{}
	a.discussions = discussions.New(r, a.saved)
	return a, r, browser, bearer, csrf, id
}
func discussionRequest(a *api, method, path, body, token, key string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if key != "" {
		r.Header.Set("Idempotency-Key", key)
	}
	w := httptest.NewRecorder()
	a.Routes().ServeHTTP(w, r)
	return w
}
func TestDiscussionAPIValidationCredentialsAndPublicProjection(t *testing.T) {
	a, repo, browser, bearer, csrf, event := discussionAPI(t)
	path := "/api/v1/events/" + event + "/discussions"
	key := accounts.ID()
	for _, tc := range []struct{ body, key string }{{`{}`, key}, {`{"body":null}`, key}, {`{"body":"a","body":"b"}`, key}, {`{"body":"a","account_id":"other"}`, key}, {`{"body":"a","parent_id":"` + accounts.ID() + `"}`, key}, {`{"body":"a"}`, ""}, {`{"body":"a"}`, "short"}} {
		w := discussionRequest(a, "POST", path, tc.body, bearer, tc.key)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct {
		cookie, csrf, origin string
		status               int
	}{{"", "", "", 401}, {browser, "", "", 403}, {browser, csrf, "https://evil.example", 403}} {
		w := accountRequest(a, "POST", path, `{"body":"Hello"}`, tc.cookie, "", tc.csrf, tc.origin, "")
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := discussionRequest(a, "POST", path, `{"body":"<script>question</script>"}`, bearer, key)
	if w.Code != 201 || w.Header().Get("Location") == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	var p discussions.Post
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	w = discussionRequest(a, "POST", path, `{"body":"<script>question</script>"}`, bearer, key)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = discussionRequest(a, "POST", path, `{"body":"Changed"}`, bearer, key)
	if w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, path := range []string{path, "/api/v1/community/discussions", path + "/" + p.ID} {
		w = discussionRequest(a, "GET", path, "", "", "")
		if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, private := range []string{"private@example.com", "Secret city", "csrf_token", "idempotency_key", "request_hash"} {
			if strings.Contains(w.Body.String(), private) {
				t.Fatal("private field leaked", private)
			}
		}
	}
	postPath := "/api/v1/posts/" + p.ID
	for _, tc := range []struct{ method, path, body string }{{"PATCH", postPath, `{"body":"new","parent_id":"x"}`}, {"PATCH", postPath + "?owner=x", `{"body":"new"}`}, {"DELETE", postPath, `{}`}, {"PUT", postPath + "/reactions/helpful", `{}`}} {
		w = discussionRequest(a, tc.method, tc.path, tc.body, bearer, "")
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w = discussionRequest(a, "PATCH", postPath, `{"body":"new"}`, bearer, "")
	if w.Code != 200 || repo.post.Body != "new" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = discussionRequest(a, "PUT", postPath+"/reactions/helpful", "", bearer, "")
	if w.Code != 200 || !repo.post.Helpful {
		t.Fatal(w.Code, w.Body.String())
	}
	for range 2 {
		w = discussionRequest(a, "DELETE", postPath, "", bearer, "")
		if w.Code != 204 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	repo.unavailable = true
	w = discussionRequest(a, "GET", path, "", "", "")
	if w.Code != 503 || strings.Contains(w.Body.String(), `"count":0`) {
		t.Fatal("failure fabricated empty activity", w.Body.String())
	}
}
func TestDiscussionNativeDraftRecoveryAndSafeReturns(t *testing.T) {
	a, repo, browser, _, csrf, event := discussionAPI(t)
	thread := "/events/" + event + "/discussions/" + accounts.ID() + "?return_to=" + url.QueryEscape("/community/discussions?city=Berlin")
	for _, tc := range []struct {
		cookie, csrf string
		status       int
	}{{"", "", 401}, {browser, "bad", 403}, {browser, csrf, 400}} {
		body := url.Values{"action": {"create"}, "body": {"<script>draft</script>"}, "event_id": {event}, "idempotency_key": {"bad"}, "return_to": {thread}, "csrf_token": {tc.csrf}}.Encode()
		w := accountRequest(a, "POST", "/discussion-actions", body, tc.cookie, "", "", "https://events.example", "application/x-www-form-urlencoded")
		if w.Code != tc.status || !strings.Contains(w.Body.String(), "&lt;script&gt;draft&lt;/script&gt;") || repo.post.ID != "" {
			t.Fatal("failed write lost draft or wrote", w.Code, w.Body.String())
		}
	}
	if got := safeAuthReturn(thread); got != thread {
		t.Fatal("sign-in lost thread", got)
	}
	selected := thread + "&post_id=" + accounts.ID()
	if got := safeAuthReturn(selected); got != selected {
		t.Fatal("lost selected contribution", got)
	}
	for _, bad := range []string{"https://evil.example/", "//evil.example/", "/foo/discussions", "/events/ticketmaster:show/discussions/bad", "/community/discussions/evil", "/events/ticketmaster:show/discussions?owner=x"} {
		if got := safeDiscussionReturn(bad); got != "/" {
			t.Fatal("unsafe return", bad, got)
		}
	}
}

func TestContextThreadUsesRetainedEventAndIsolatesReplyFailure(t *testing.T) {
	a, repo, browser, bearer, _, event := discussionAPI(t)
	w := discussionRequest(a, "POST", "/api/v1/events/"+event+"/discussions", `{"body":"A retained question"}`, bearer, accounts.ID())
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	thread := repo.post.ID
	origin := "/?city=Berlin&selected_event=" + event + "&section=discussion&selected_thread=" + thread
	if safeReturnURL(origin) != origin || safeAuthReturn(origin) != origin || safeDiscussionReturn(origin) != origin {
		t.Fatal("selected thread lost its validated browsing/auth return")
	}
	path := "/events/" + event + "/discussions/" + thread + "?return_to=" + url.QueryEscape("/?city=Berlin")
	for _, unavailable := range []bool{false, true} {
		repo.unavailable = unavailable
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Cookie", "ticketopia_session="+browser)
		r.Header.Set("X-Ticketopia-Panel", "true")
		w := httptest.NewRecorder()
		a.Routes().ServeHTTP(w, r)
		if w.Code != 200 || strings.Contains(w.Body.String(), "<html") || !strings.Contains(w.Body.String(), `data-context-thread="`+thread+`"`) || !strings.Contains(w.Body.String(), "A retained question") || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("Vary") != "X-Ticketopia-Panel" {
			t.Fatal("thread fragment lost event context or private response boundary", w.Code, w.Body.String())
		}
		if unavailable && (!strings.Contains(w.Body.String(), "Retry replies") || !strings.Contains(w.Body.String(), "Open full conversation")) {
			t.Fatal("reply failure removed usable root/expansion/recovery")
		}
	}
	if w := accountRequest(a, "GET", strings.Replace(path, event, "ticketmaster:wrong", 1), "", browser, "", "", "", ""); w.Code != 404 {
		t.Fatal("thread/event mismatch accepted", w.Code)
	}
}

func TestContextThreadParametersDoNotBecomeDiscoveryFilters(t *testing.T) {
	thread, parent := accounts.ID(), accounts.ID()
	values := url.Values{"city": {"Berlin"}, "selected_event": {"ticketmaster:show"}, "section": {"discussion"}, "selected_thread": {thread}, "reply_to": {parent}, "discussion_cursor": {"opaque"}}
	filters, event, section, err := webSearch(values)
	if err != nil || event != "ticketmaster:show" || section != "discussion" || filters.Encode() != "city=Berlin" {
		t.Fatal(filters, event, section, err)
	}
	for _, change := range []url.Values{
		{"selected_thread": {"bad"}}, {"selected_thread": {thread, thread}}, {"section": {"overview"}},
		{"selected_event": {""}}, {"reply_to": {"bad"}}, {"discussion_cursor": {strings.Repeat("x", 1025)}},
	} {
		copy := url.Values{}
		for key, value := range values {
			copy[key] = value
		}
		for key, value := range change {
			copy[key] = value
		}
		if _, _, _, err := webSearch(copy); err == nil {
			t.Fatal("invalid contextual state accepted", copy)
		}
	}
}
