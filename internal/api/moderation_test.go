package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/moderation"
)

type moderationStub struct {
	moderation.Repository
	allowed   bool
	report    moderation.Report
	who       string
	writes    int
	err       error
	decisions int
	review    moderation.Review
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

func (r *moderationStub) Decide(_ context.Context, who, post string, review moderation.Review) (moderation.Outcome, error) {
	r.decisions++
	r.who, r.review = who, review
	return moderation.Outcome{PostID: post, Action: review.Action, Reason: review.Reason}, nil
}

func TestModerationDecisionsRequireModeratorCredentials(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		moderator, browser, csrf bool
		roleError                error
		want                     int
		code                     string
	}{
		{"ordinary API token", false, false, false, nil, 403, "moderator_access_required"},
		{"ordinary browser session", false, true, true, nil, 403, "moderator_access_required"},
		{"moderator browser without CSRF", true, true, false, nil, 403, "csrf_failed"},
		{"role lookup unavailable", true, false, false, moderation.ErrUnavailable, 503, "moderation_unavailable"},
		{"moderator API token", true, false, false, nil, 201, ""},
		{"moderator browser session", true, true, true, nil, 201, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, _, browser, bearer, csrf, _ := discussionAPI(t)
			principal, err := a.accounts.Authenticate(t.Context(), bearer, "api")
			if err != nil {
				t.Fatal(err)
			}
			r := &moderationStub{allowed: tc.moderator, err: tc.roleError}
			a.moderation = moderation.New(r)
			version := int64(3)
			review := moderation.Review{Action: "hide", Reason: "Reviewed concern", ExpectedVersion: &version, ExpectedUpdatedAt: time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)}
			body, err := json.Marshal(review)
			if err != nil {
				t.Fatal(err)
			}
			post := accounts.ID()
			cookie, token, proof := "", bearer, ""
			if tc.browser {
				cookie, token = browser, ""
			}
			if tc.csrf {
				proof = csrf
			}
			w := accountRequest(a, "POST", "/api/v1/moderation/posts/"+post+"/decisions", string(body), cookie, token, proof, "", "")
			if w.Code != tc.want || w.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatal(w.Code, w.Body.String())
			}
			if tc.want != http.StatusCreated {
				if r.decisions != 0 {
					t.Fatal("unauthorized request reached decision storage")
				}
				if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/problem+json") || !strings.Contains(w.Body.String(), `"code":"`+tc.code+`"`) {
					t.Fatal("missing authorization error response")
				}
			} else if r.decisions != 1 || r.who != principal.Account.ID || r.review.ExpectedVersion == nil || *r.review.ExpectedVersion != version || !r.review.ExpectedUpdatedAt.Equal(review.ExpectedUpdatedAt) {
				t.Fatal("authorized decision lost its reviewer or race guards")
			}
		})
	}
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
