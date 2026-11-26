package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
)

type discussionStub struct {
	discussions.Repository
	post        discussions.Post
	input       discussions.Create
	unavailable bool
	listCalls   int
	lastQuery   discussions.Query
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
	r.post = discussions.Post{ID: accounts.ID(), EventID: c.EventID, Body: c.Body, CreatedAt: now, UpdatedAt: now, Profile: &accounts.PublicProfile{ID: c.Owner, DisplayName: "Contributor"}, Via: c.Via, ViaHost: c.ViaHost}
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
func (r *discussionStub) List(_ context.Context, _, _, _ string, q discussions.Query) ([]discussions.Post, int, error) {
	r.listCalls++
	r.lastQuery = q
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
func (r *discussionStub) SameQuestion(_ context.Context, who, id string, set bool) (discussions.Post, error) {
	if id != r.post.ID || r.post.ID != r.post.ThreadID {
		return discussions.Post{}, discussions.ErrNotFound
	}
	if set && r.post.Profile != nil && r.post.Profile.ID == who {
		return discussions.Post{}, discussions.ErrOwnQuestion
	}
	r.post.SameQuestion = set
	r.post.SameQuestionCount = 0
	if set {
		r.post.SameQuestionCount = 1
	}
	return r.post, nil
}
func (r *discussionStub) NewReplies(_ context.Context, viewer, event, thread string, since time.Time) (discussions.NewReplies, error) {
	if thread != r.post.ID || event != r.post.EventID {
		return discussions.NewReplies{}, discussions.ErrNotFound
	}
	return discussions.NewReplies{Replies: []discussions.NewReply{{ID: r.post.ID, CreatedAt: since}}, AsOf: since.Add(time.Second)}, nil
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
	// A browser session's token is sent as its cookie with its CSRF token, so
	// tests can act publicly without a publishing token.
	if p, err := a.accounts.Authenticate(r.Context(), token, "session"); err == nil {
		r.AddCookie(&http.Cookie{Name: a.authConfig.CookieName("session"), Value: token})
		r.Header.Set("X-CSRF-Token", p.CSRF)
	} else if token != "" {
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
		w := discussionRequest(a, "POST", path, tc.body, browser, tc.key)
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
	w := discussionRequest(a, "POST", path, `{"body":"<script>question</script>"}`, browser, key)
	if w.Code != 201 || w.Header().Get("Location") == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	var p discussions.Post
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	w = discussionRequest(a, "POST", path, `{"body":"<script>question</script>"}`, browser, key)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = discussionRequest(a, "POST", path, `{"body":"Changed"}`, browser, key)
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
	for _, tc := range []struct{ method, path, body string }{{"PATCH", postPath, `{"body":"new","parent_id":"x"}`}, {"DELETE", postPath, `{}`}, {"PUT", postPath + "/reactions/helpful", `{}`}} {
		w = discussionRequest(a, tc.method, tc.path, tc.body, browser, "")
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	// A token without publish never posts or reacts, and no token edits or
	// removes the person's posts.
	for _, tc := range []struct{ method, path, body, code string }{
		{"POST", path, `{"body":"Token question"}`, "token_cannot_publish"}, {"POST", path + "/" + p.ID + "/replies", `{"body":"Token reply"}`, "token_cannot_publish"},
		{"PATCH", postPath, `{"body":"new"}`, "browser_session_required"}, {"DELETE", postPath, "", "browser_session_required"},
		{"PUT", postPath + "/reactions/helpful", "", "token_cannot_publish"}, {"PUT", postPath + "/reactions/same-question", "", "token_cannot_publish"},
	} {
		w = discussionRequest(a, tc.method, tc.path, tc.body, bearer, accounts.ID())
		if w.Code != 403 || !strings.Contains(w.Body.String(), tc.code) || repo.post.Body != "<script>question</script>" {
			t.Fatal(tc.method, tc.path, w.Code, w.Body.String())
		}
	}
	w = discussionRequest(a, "PATCH", postPath, `{"body":"new"}`, browser, "")
	if w.Code != 200 || repo.post.Body != "new" {
		t.Fatal(w.Code, w.Body.String())
	}
	w = discussionRequest(a, "PUT", postPath+"/reactions/helpful", "", browser, "")
	if w.Code != 200 || !repo.post.Helpful {
		t.Fatal(w.Code, w.Body.String())
	}
	for range 2 {
		w = discussionRequest(a, "DELETE", postPath, "", browser, "")
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

func TestDiscussionNativeRetryDoesNotTurnConflictIntoNewPost(t *testing.T) {
	a, repo, browser, _, csrf, event := discussionAPI(t)
	key := accounts.ID()
	form := url.Values{"action": {"create"}, "body": {"Original question"}, "event_id": {event}, "idempotency_key": {key}, "return_to": {"/community/discussions?city=Berlin"}, "csrf_token": {csrf}}
	post := func() *httptest.ResponseRecorder {
		return accountRequest(a, "POST", "/discussion-actions", form.Encode(), browser, "", "", "https://events.example", "application/x-www-form-urlencoded")
	}
	w := post()
	if w.Code != http.StatusSeeOther {
		t.Fatal(w.Code, w.Body.String())
	}
	original := repo.post.ID
	location, err := url.Parse(w.Header().Get("Location"))
	if err != nil || location.Fragment != "post-"+original || location.Query().Get("return_to") != "/community/discussions?city=Berlin" {
		t.Fatal("confirmed post lost selection or origin", location, err)
	}
	w = post()
	if w.Code != http.StatusSeeOther || repo.post.ID != original || w.Header().Get("Location") != location.String() {
		t.Fatal("unchanged retry created a new contribution", w.Code, w.Body.String())
	}
	form.Set("body", "<changed draft>")
	w = post()
	if w.Code != http.StatusConflict || repo.post.ID != original || repo.post.Body != "Original question" {
		t.Fatal("conflicting retry changed a published contribution", w.Code, w.Body.String())
	}
	markup := w.Body.String()
	if !strings.Contains(markup, `name="idempotency_key" value="`+key+`"`) || !strings.Contains(markup, "&lt;changed draft&gt;") || strings.Contains(markup, "Retry contribution") {
		t.Fatal("conflict must retain the draft/key without offering a new publication", markup)
	}
}

func TestContextThreadUsesRetainedEventAndIsolatesReplyFailure(t *testing.T) {
	a, repo, browser, _, _, event := discussionAPI(t)
	w := discussionRequest(a, "POST", "/api/v1/events/"+event+"/discussions", `{"body":"A retained question"}`, browser, accounts.ID())
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
		if w.Code != 200 || strings.Contains(w.Body.String(), "<html") || !strings.Contains(w.Body.String(), `data-context-thread="`+thread+`"`) || !strings.Contains(w.Body.String(), "A retained question") || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("Vary") != "X-Ticketopia-Panel, X-Ticketopia-Section" {
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

func TestEventContextShowsDiscussionAndCommunityAcrossEntryPoints(t *testing.T) {
	a, posts, browser, _, _, event := discussionAPI(t)
	if w := discussionRequest(a, "POST", "/api/v1/events/"+event+"/discussions", `{"body":"Is there a coat check?"}`, browser, accounts.ID()); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := discussionRequest(a, "PUT", "/api/v1/me/event-recommendations/"+event, `{"reason":"A great neighborhood venue"}`, browser, ""); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	public, private := accounts.ID(), accounts.ID()
	a.interests = interests.New(&interestStub{
		items: map[string]map[string]interests.Item{
			public: {event: {Visibility: "public"}}, private: {event: {Visibility: "private"}},
		},
		profiles: map[string]accounts.PublicProfile{
			public: {ID: public, DisplayName: "Public event explorer"}, private: {ID: private, DisplayName: "Private participant must stay hidden"},
		},
	}, a.saved)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "classifications") {
			_, _ = w.Write([]byte(`{"_embedded":{"classifications":[]}}`))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/events.json") {
			_, _ = w.Write([]byte(`{"page":{"number":0,"size":20,"totalElements":0,"totalPages":0}}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"Interest","name":"Interest fixture"}`))
	}))
	defer upstream.Close()
	cache := kv.NewMemory()
	defer cache.Close()
	a.events = discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL, RequestInterval: time.Millisecond})

	for _, cookie := range []string{"", browser} {
		for _, section := range []string{"overview", "discussion", "community"} {
			for _, panel := range []bool{false, true} {
				path := "/?city=Berlin&selected_event=" + event + "&section=" + section
				if panel {
					path = "/events/" + event + "?section=" + section
				}
				t.Run(section+"/"+path+"/"+cookie, func(t *testing.T) {
					r := httptest.NewRequest("GET", path, nil)
					if cookie != "" {
						r.Header.Set("Cookie", "ticketopia_session="+cookie)
					}
					if panel {
						r.Header.Set("X-Ticketopia-Panel", "true")
					}
					w := httptest.NewRecorder()
					a.Routes().ServeHTTP(w, r)
					if w.Code != 200 {
						t.Fatal(w.Code, w.Body.String())
					}
					body := w.Body.String()
					for _, want := range []string{`aria-label="Event sections"`, `data-context-section="` + section + `"`} {
						if !strings.Contains(body, want) {
							t.Errorf("missing inline activity %q", want)
						}
					}
					if strings.Contains(body, "Is there a coat check?") != (section == "discussion") || strings.Contains(body, "A great neighborhood venue") != (section == "community") {
						t.Fatal("preview did not select the requested community section")
					}
					if strings.Contains(body, "Private participant must stay hidden") {
						t.Fatal("preview leaked private interest")
					}
				})
			}
		}
	}
	// Retained threads load the same community without losing their conversation.
	r := httptest.NewRequest("GET", "/events/"+event+"/discussions/"+posts.post.ID, nil)
	r.Header.Set("X-Ticketopia-Panel", "true")
	w := httptest.NewRecorder()
	a.Routes().ServeHTTP(w, r)
	for _, want := range []string{"Is there a coat check?", "A great neighborhood venue", "Public event explorer"} {
		if w.Code != 200 || !strings.Contains(w.Body.String(), want) {
			t.Fatal("thread preview lost inline activity", w.Code, want)
		}
	}
	// Community outages leave the question, facts, and recovery visible.
	a.recommendations = recommendations.New(&recommendationStub{unavailable: true}, a.saved)
	w = httptest.NewRecorder()
	a.Routes().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Is there a coat check?") || !strings.Contains(w.Body.String(), "Public recommendations couldn&#39;t load") {
		t.Fatal("community failure hid conversation or fabricated empty activity", w.Code, w.Body.String())
	}
}

// Open questions are public, "mine" needs sign-in and reaches the query as
// the viewer's own connections, and Same question refuses the author's own
// post and returns native forms to their card.
func TestOpenQuestionsRoutesAndSameQuestion(t *testing.T) {
	a, repo, browser, bearer, csrf, event := discussionAPI(t)
	w := discussionRequest(a, "POST", "/api/v1/events/"+event+"/discussions", `{"body":"Is there a cloakroom?"}`, browser, accounts.ID())
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	post := repo.post
	if w = discussionRequest(a, "GET", "/api/v1/community/questions?city=Berlin", "", "", ""); w.Code != 200 || !repo.lastQuery.Open || repo.lastQuery.Connected != "" || repo.lastQuery.City != "Berlin" {
		t.Fatal("public open questions", w.Code, repo.lastQuery, w.Body.String())
	}
	if w = discussionRequest(a, "GET", "/api/v1/community/questions?connections=mine", "", "", ""); w.Code != 401 {
		t.Fatal("anonymous mine", w.Code, w.Body.String())
	}
	if w = discussionRequest(a, "GET", "/api/v1/community/questions?connections=mine", "", bearer, ""); w.Code != 200 || repo.lastQuery.Connected != post.Profile.ID {
		t.Fatal("signed-in mine", w.Code, repo.lastQuery)
	}
	if w = discussionRequest(a, "GET", "/api/v1/community/questions?connections=friends", "", bearer, ""); w.Code != 400 {
		t.Fatal("unknown connections", w.Code)
	}
	if w = discussionRequest(a, "PUT", "/api/v1/posts/"+post.ID+"/reactions/same-question", "", browser, ""); w.Code != 400 || !strings.Contains(w.Body.String(), "someone else") {
		t.Fatal("own question marked", w.Code, w.Body.String())
	}
	if w = discussionRequest(a, "DELETE", "/api/v1/posts/"+post.ID+"/reactions/same-question", "", browser, ""); w.Code != 200 {
		t.Fatal("clearing a mark", w.Code, w.Body.String())
	}

	page := accountRequest(a, "GET", "/community/questions", "", browser, "", "", "", "")
	body := page.Body.String()
	if page.Code != 200 || !strings.Contains(body, "Can you help?") || !strings.Contains(body, "#discussion-composer") || !strings.Contains(body, `value="mine"`) || strings.Contains(body, "same-question-button") {
		t.Fatal("open questions page: answer link, Your events choice, and no Same question on your own post", page.Code)
	}
	if w = accountRequest(a, "GET", "/community/questions?connections=mine", "", "", "", "", "", ""); w.Code != http.StatusSeeOther || !strings.Contains(w.Header().Get("Location"), "/auth/sign-in") {
		t.Fatal("signed-out Your events should ask to sign in", w.Code)
	}
	repo.post.Profile = &accounts.PublicProfile{ID: accounts.ID(), DisplayName: "Someone else"}
	if body = accountRequest(a, "GET", "/community/questions?city=Berlin", "", browser, "", "", "", "").Body.String(); !strings.Contains(body, "same-question-button") || !strings.Contains(body, `name="return_to" value="/community/questions?city=Berlin"`) {
		t.Fatal("someone else's question has no Same question control returning to this list")
	}
	form := url.Values{"action": {"same_question"}, "event_id": {event}, "thread_id": {post.ID}, "post_id": {post.ID}, "return_to": {"/community/questions?city=Berlin"}, "csrf_token": {csrf}}
	w = accountRequest(a, "POST", "/discussion-actions", form.Encode(), browser, "", "", "https://events.example", "application/x-www-form-urlencoded")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/community/questions?city=Berlin#question-"+post.ID || !repo.post.SameQuestion {
		t.Fatal("native Same question should return to its card", w.Code, w.Header().Get("Location"))
	}
	for raw, want := range map[string]string{"/community/questions?connections=mine&city=Berlin": "/community/questions?connections=mine&city=Berlin", "/community/questions?owner=x": "/community/questions"} {
		if got := safeDiscussionReturn(raw); got != want {
			t.Fatal("open questions return", raw, got)
		}
	}
}

// The live check is a signed-in browser read with a strict since, limited
// per account, and conversations offer it only when it is on.
func TestLiveReplyChecks(t *testing.T) {
	a, repo, browser, _, _, event := discussionAPI(t)
	WithLiveReplyChecks(1)(a)
	if w := discussionRequest(a, "POST", "/api/v1/events/"+event+"/discussions", `{"body":"Doors?"}`, browser, accounts.ID()); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	thread := "/events/" + event + "/discussions/" + repo.post.ID
	since := url.QueryEscape(time.Now().UTC().Format(time.RFC3339Nano))
	if w := accountRequest(a, "GET", thread+"/new-replies?since="+since, "", "", "", "", "", ""); w.Code != 401 {
		t.Fatal("anonymous live check", w.Code)
	}
	for _, bad := range []string{"", "?since=yesterday", "?since=" + since + "&since=" + since, "?since=" + since + "&extra=1"} {
		if w := accountRequest(a, "GET", thread+"/new-replies"+bad, "", browser, "", "", "", ""); w.Code != 400 {
			t.Fatal("malformed live check", bad, w.Code)
		}
	}
	w := accountRequest(a, "GET", thread+"/new-replies?since="+since, "", browser, "", "", "", "")
	var out discussions.NewReplies
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Replies) != 1 || out.Replies[0].ID != repo.post.ID || w.Header().Get("Cache-Control") == "" {
		t.Fatal("live check", w.Code, w.Body.String())
	}
	if w = accountRequest(a, "GET", thread+"/new-replies?since="+since, "", browser, "", "", "", ""); w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") == "" {
		t.Fatal("over the per-account limit", w.Code)
	}
	page := accountRequest(a, "GET", thread, "", browser, "", "", "", "").Body.String()
	if !strings.Contains(page, `data-live-replies="`+thread+`/new-replies"`) || !strings.Contains(page, "data-live-engaged") {
		t.Fatal("the thread's author should get live checks from the start")
	}
	WithLiveReplyChecks(0)(a)
	if page = accountRequest(a, "GET", thread, "", browser, "", "", "", "").Body.String(); strings.Contains(page, "data-live-replies") {
		t.Fatal("live checks offered while off")
	}
	if w = accountRequest(a, "GET", thread+"/new-replies?since="+since, "", browser, "", "", "", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatal("live check while off", w.Code)
	}
}
