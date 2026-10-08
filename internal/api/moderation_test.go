package api

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/moderation"
)

type moderationStub struct {
	moderation.Repository
	allowed bool
	report  moderation.Report
	who     string
	writes  int
	err     error
}

func (r *moderationStub) IsModerator(context.Context, string) (bool, error) { return r.allowed, r.err }
func (r *moderationStub) Report(_ context.Context, who, post, reason, text string) (moderation.Report, bool, error) {
	if r.err != nil {
		return moderation.Report{}, false, r.err
	}
	r.who = who
	r.writes++
	if r.report.ID != "" {
		return r.report, false, nil
	}
	r.report = moderation.Report{ID: accounts.ID(), PostID: post, EventID: "ticketmaster:fixture", ThreadID: post, Reason: reason, Context: text, CreatedAt: time.Now().UTC()}
	return r.report, true, nil
}
func (r *moderationStub) Reports(_ context.Context, who string, _ moderation.Query) ([]moderation.Report, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.who == who {
		return []moderation.Report{r.report}, nil
	}
	return nil, nil
}
func TestPrivateReportsHTTPValidationAndDraftRecovery(t *testing.T) {
	a, posts, browser, bearer, csrf, event := discussionAPI(t)
	root := discussionRequest(a, "POST", "/api/v1/events/"+event+"/discussions", `{"body":"Fixture post"}`, bearer, accounts.ID())
	if root.Code != 201 {
		t.Fatal(root.Body.String())
	}
	r := &moderationStub{}
	a.moderation = moderation.New(r)
	path := "/api/v1/posts/" + posts.post.ID + "/reports"
	for _, body := range []string{`{}`, `{"reason":null}`, `{"reason":"spam","reason":"abuse"}`, `{"reason":"spam","reporter_id":"other"}`, `{"reason":"spam","context":null}`, `{"reason":"downvote"}`, `{"reason":"spam","context":"` + strings.Repeat("x", 2001) + `"}`} {
		w := discussionRequest(a, "POST", path, body, bearer, "")
		if w.Code != 400 || r.writes != 0 {
			t.Fatal("invalid report", w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct {
		cookie, csrf, origin string
		status               int
	}{{"", "", "", 401}, {browser, "", "", 403}, {browser, csrf, "https://evil.example", 403}} {
		w := accountRequest(a, "POST", path, `{"reason":"spam"}`, tc.cookie, "", tc.csrf, tc.origin, "")
		if w.Code != tc.status || r.writes != 0 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for i, status := range []int{201, 200} {
		w := discussionRequest(a, "POST", path, `{"reason":"spam","context":"Private concern"}`, bearer, "")
		if w.Code != status || w.Header().Get("Cache-Control") != "private, no-store" || r.who != posts.input.Owner || r.writes != i+1 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, route := range []string{"/api/v1/moderation", "/api/v1/moderation/posts/" + posts.post.ID, "/moderation", "/moderation/posts/" + posts.post.ID} {
		w := accountRequest(a, "GET", route, "", browser, "", "", "", "")
		if w.Code != 403 {
			t.Fatal("role bypass", route, w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct {
		cookie, csrf, reason string
		status               int
	}{{"", "", "spam", 401}, {browser, "wrong", "spam", 403}, {browser, csrf, "invalid", 400}} {
		form := url.Values{"csrf_token": {tc.csrf}, "reason": {tc.reason}, "context": {"<script>private draft</script>"}}.Encode()
		w := accountRequest(a, "POST", "/posts/"+posts.post.ID+"/report", form, tc.cookie, "", "", "https://events.example", "application/x-www-form-urlencoded")
		if w.Code != tc.status || !strings.Contains(w.Body.String(), "&lt;script&gt;private draft&lt;/script&gt;") {
			t.Fatal("lost report draft", w.Code, w.Body.String())
		}
	}
	for _, tc := range []struct {
		cookie, csrf string
		status       int
	}{{"", "", 401}, {browser, "wrong", 403}, {browser, csrf, 403}} {
		form := url.Values{"csrf_token": {tc.csrf}, "action": {"hide"}, "reason": {"<script>Reason draft</script>"}, "notes": {"Private notes draft"}, "expected_version": {"0"}, "expected_updated_at": {time.Now().UTC().Format(time.RFC3339Nano)}}.Encode()
		w := accountRequest(a, "POST", "/moderation/posts/"+posts.post.ID, form, tc.cookie, "", "", "https://events.example", "application/x-www-form-urlencoded")
		if w.Code != tc.status || !strings.Contains(w.Body.String(), "&lt;script&gt;Reason draft&lt;/script&gt;") || !strings.Contains(w.Body.String(), "Private notes draft") {
			t.Fatal("lost moderator draft", w.Code, w.Body.String())
		}
	}
	r.err = moderation.ErrUnavailable
	w := discussionRequest(a, "GET", "/api/v1/me/reports", "", bearer, "")
	if w.Code != 503 || strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal("fabricated empty reports", w.Code, w.Body.String())
	}
	form := url.Values{"csrf_token": {csrf}, "reason": {"spam"}, "context": {"Storage failure draft"}}.Encode()
	w = accountRequest(a, "POST", "/posts/"+posts.post.ID+"/report", form, browser, "", "", "https://events.example", "application/x-www-form-urlencoded")
	if w.Code != 503 || !strings.Contains(w.Body.String(), "Storage failure draft") {
		t.Fatal("unavailable draft", w.Code, w.Body.String())
	}
}
func TestModerationSafeSignInReturns(t *testing.T) {
	post := accounts.ID()
	for _, raw := range []string{"/me/reports", "/me/moderation?limit=1", "/moderation?state=all", "/moderation/posts/" + post + "?limit=1", "/posts/" + post + "/report"} {
		if got := safeAuthReturn(raw); got != raw {
			t.Fatal("lost return", raw, got)
		}
	}
	for _, raw := range []string{"https://evil.example/", "//evil.example/", "/moderation?owner=x", "/moderation?state=bad", "/moderation/posts/bad", "/moderation/posts/" + post + "?owner=x", "/posts/" + post + "/report?return_to=https://evil.example", "/me/reports#x", "/me/reports?cursor=x&cursor=y"} {
		if got := safeModerationReturn(raw); got != "/me" {
			t.Fatal("unsafe return", raw, got)
		}
	}
}
