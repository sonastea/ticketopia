package api

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/internal/discussions"
)

type discussionFollowStub struct {
	discussionfollows.Repository
	who         string
	follow      discussionfollows.Follow
	writes      int
	unavailable bool
}

func (r *discussionFollowStub) Get(_ context.Context, who, event, thread string) (discussionfollows.Follow, error) {
	r.who = who
	if r.follow.ThreadID == "" {
		return discussionfollows.Follow{}, discussions.ErrNotFound
	}
	return r.follow, nil
}
func (r *discussionFollowStub) Set(_ context.Context, who, event, thread, frequency string) (discussionfollows.Follow, error) {
	r.who = who
	r.writes++
	r.follow = discussionfollows.Follow{EventID: event, ThreadID: thread, Frequency: frequency}
	return r.follow, nil
}
func (r *discussionFollowStub) Remove(_ context.Context, who, event, thread string) error {
	r.who = who
	r.writes++
	r.follow = discussionfollows.Follow{}
	return nil
}
func (r *discussionFollowStub) List(_ context.Context, who string, q discussionfollows.Query) ([]discussionfollows.Follow, error) {
	r.who = who
	if r.unavailable {
		return nil, discussionfollows.ErrUnavailable
	}
	return []discussionfollows.Follow{}, nil
}
func (r *discussionFollowStub) Notifications(_ context.Context, who string, q discussionfollows.Query) ([]discussionfollows.Notification, error) {
	r.who = who
	return []discussionfollows.Notification{{ID: accounts.ID(), EventID: "ticketmaster:Event", ThreadID: accounts.ID(), PostID: accounts.ID(), EventName: "A public event", ReplyCount: 1, AvailableAt: time.Now()}}, nil
}
func (r *discussionFollowStub) Read(_ context.Context, who, id string) error {
	r.who = who
	r.writes++
	return nil
}

func TestDiscussionFollowHTTPPrivacyValidationAndCSRF(t *testing.T) {
	a, _, browser, bearer, csrf, event := discussionAPI(t)
	repo := &discussionFollowStub{}
	a.discussionFollows = discussionfollows.New(repo)
	thread := accounts.ID()
	path := "/api/v1/events/" + event + "/discussions/" + thread + "/follow"
	for _, tc := range []struct{ body, path string }{{`{}`, path}, {`{"frequency":null}`, path}, {`{"frequency":"daily","frequency":"weekly"}`, path}, {`{"frequency":"daily","account_id":"someone"}`, path}, {`{"frequency":"hourly"}`, path}, {`{"frequency":"daily"}`, path + "?owner=other"}} {
		w := discussionRequest(a, "PUT", tc.path, tc.body, bearer, "")
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct {
		cookie, csrf, origin string
		status               int
	}{{"", "", "", 401}, {browser, "", "", 403}, {browser, csrf, "https://evil.example", 403}} {
		w := accountRequest(a, "PUT", path, `{"frequency":"daily"}`, tc.cookie, "", tc.csrf, tc.origin, "")
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if repo.writes != 0 {
		t.Fatal("invalid writes reached repository")
	}
	w := discussionRequest(a, "PUT", path, `{"frequency":"daily"}`, bearer, "")
	if w.Code != 200 || repo.follow.Frequency != "daily" || repo.who == "" {
		t.Fatal(w.Code, w.Body.String())
	}
	who := repo.who
	for _, path := range []string{"/api/v1/me/discussions?limit=1", "/api/v1/me/notifications?limit=1"} {
		w = discussionRequest(a, "GET", path, "", bearer, "")
		if w.Code != 200 || repo.who != who || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(w.Code, w.Body.String())
		}
		for _, secret := range []string{"private@example.com", "csrf_token", "report_id", "reported_body", "notes"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("private field", secret)
			}
		}
		w = discussionRequest(a, "GET", path, "", "", "")
		if w.Code != 401 {
			t.Fatal("guest inbox", w.Code)
		}
	}
	w = discussionRequest(a, "DELETE", path, `{}`, bearer, "")
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = discussionRequest(a, "DELETE", path, "", bearer, "")
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	read := "/api/v1/me/notifications/" + accounts.ID() + "/read"
	w = accountRequest(a, "PUT", read, "", browser, "", "", "", "")
	if w.Code != 403 {
		t.Fatal("read CSRF", w.Code)
	}
	w = discussionRequest(a, "PUT", read, "", bearer, "")
	if w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	form := url.Values{"action": {"follow"}, "event_id": {event}, "thread_id": {thread}, "frequency": {"weekly"}, "csrf_token": {csrf}, "return_to": {"https://evil.example"}}
	r := httptest.NewRequest("POST", "/discussion-follow-actions", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("Cookie", a.authConfig.CookieName("session")+"="+browser)
	w = httptest.NewRecorder()
	a.Routes().ServeHTTP(w, r)
	if w.Code != 303 || w.Header().Get("Location") != "/me/discussions" || repo.follow.Frequency != "weekly" {
		t.Fatal(w.Code, w.Header(), w.Body.String())
	}
	repo.unavailable = true
	w = discussionRequest(a, "GET", "/api/v1/me/discussions", "", bearer, "")
	if w.Code != 503 || strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal("fabricated empty follows", w.Code, w.Body.String())
	}
}
func TestDiscussionFollowSafeReturnContext(t *testing.T) {
	for _, path := range []string{"/me/discussions?limit=1", "/me/notifications"} {
		if safeAuthReturn(path) != path || safeReturnURL(path) != path {
			t.Fatal("lost return context", path)
		}
	}
	for _, path := range []string{"//evil.example/me/discussions", "/me/notifications#bad", "/me/discussions?owner=other", "/me/discussions?limit=0"} {
		if got := safeDiscussionFollowReturn(path); strings.Contains(got, "evil") || strings.Contains(got, "#") || strings.Contains(got, "owner") || strings.Contains(got, "limit=0") {
			t.Fatal("unsafe return", got)
		}
	}
}
