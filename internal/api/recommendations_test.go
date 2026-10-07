package api

import (
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
)

type recommendationStub struct {
	items       map[string]map[string]recommendations.Item
	profiles    map[string]accounts.PublicProfile
	unavailable bool
}

func (r *recommendationStub) Get(_ context.Context, owner, id string) (recommendations.Item, error) {
	if r.unavailable {
		return recommendations.Item{}, recommendations.ErrUnavailable
	}
	if item, ok := r.items[owner][id]; ok {
		return item, nil
	}
	return recommendations.Item{}, recommendations.ErrNotFound
}
func (r *recommendationStub) Set(_ context.Context, owner string, d models.EventDetail, reason string) (recommendations.Item, bool, error) {
	if r.items[owner] == nil {
		r.items[owner] = map[string]recommendations.Item{}
	}
	item, exists := r.items[owner][d.Item.ID]
	if !exists {
		at := time.Now().UTC()
		item = recommendations.Item{Event: d.Item, Meta: d.Meta, Profile: r.profiles[owner], RecommendedAt: at, UpdatedAt: at}
	}
	if item.Reason != reason {
		item.UpdatedAt = time.Now().UTC()
	}
	item.Reason = reason
	r.items[owner][d.Item.ID] = item
	return item, !exists, nil
}
func (r *recommendationStub) Remove(_ context.Context, owner, id string) error {
	delete(r.items[owner], id)
	return nil
}
func (r *recommendationStub) Count(_ context.Context, id string) (int, error) {
	count := 0
	for _, items := range r.items {
		if _, ok := items[id]; ok {
			count++
		}
	}
	return count, nil
}
func (r *recommendationStub) List(_ context.Context, owner, event string, q recommendations.Query) ([]recommendations.Item, error) {
	if r.unavailable {
		return nil, recommendations.ErrUnavailable
	}
	items := []recommendations.Item{}
	for id, entries := range r.items {
		if owner != "" && owner != id {
			continue
		}
		for key, item := range entries {
			if event != "" && key != event {
				continue
			}
			items = append(items, item)
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].RecommendedAt.Equal(items[j].RecommendedAt) {
			if items[i].Event.ID == items[j].Event.ID {
				return items[i].Profile.ID > items[j].Profile.ID
			}
			return items[i].Event.ID > items[j].Event.ID
		}
		return items[i].RecommendedAt.After(items[j].RecommendedAt)
	})
	if len(items) > q.Limit+1 {
		items = items[:q.Limit+1]
	}
	return items, nil
}
func recommendationAPI(t *testing.T) (*api, *accountStub, *recommendationStub, string, string, string, string) {
	a, account, _, browser, bearer, csrf, id := interestAPI(t)
	r := &recommendationStub{items: map[string]map[string]recommendations.Item{}, profiles: map[string]accounts.PublicProfile{account.account.ID: account.account.Public()}}
	a.recommendations = recommendations.New(r, a.saved)
	return a, account, r, browser, bearer, csrf, id
}
func TestRecommendationAPICredentialsStrictInputAndPublicPrivacy(t *testing.T) {
	a, account, repo, browser, bearer, csrf, id := recommendationAPI(t)
	path := "/api/v1/me/event-recommendations/" + id
	for _, tc := range []struct {
		method, path, body, cookie, token, csrf, origin string
		status                                          int
	}{
		{"GET", "/api/v1/me/event-recommendations", "", "", "", "", "", 401},
		{"PUT", path, `{}`, browser, "", "", "", 403},
		{"PUT", path, `{}`, browser, "", csrf, "https://evil.example", 403},
		{"PUT", path + "?owner=other", `{}`, "", bearer, "", "", 400},
		{"PUT", path, `{"account_id":"other"}`, "", bearer, "", "", 400},
		{"PUT", path, `{"visibility":"private"}`, "", bearer, "", "", 400},
		{"PUT", path, `{"reason":null}`, "", bearer, "", "", 400},
		{"PUT", path, `{"reason":"a","reason":"b"}`, "", bearer, "", "", 400},
		{"PUT", path, `{"reason":2}`, "", bearer, "", "", 400},
		{"PUT", path, `{"reason":"` + strings.Repeat("a", 501) + `"}`, "", bearer, "", "", 400},
		{"PUT", path, `[]`, "", bearer, "", "", 400},
		{"GET", path + "?owner=other", "", "", bearer, "", "", 400},
		{"DELETE", path, `{}`, "", bearer, "", "", 400},
	} {
		response := accountRequest(a, tc.method, tc.path, tc.body, tc.cookie, tc.token, tc.csrf, tc.origin, "")
		if response.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, response.Code, response.Body.String())
		}
	}
	response := accountRequest(a, "PUT", path, `{"reason":"<script>alert(1)</script>"}`, "", bearer, "", "", "")
	if response.Code != 201 || response.Header().Get("Location") != path {
		t.Fatal(response.Code, response.Body.String())
	}
	var first recommendations.Item
	if err := json.Unmarshal(response.Body.Bytes(), &first); err != nil {
		t.Fatal(err)
	}
	response = accountRequest(a, "PUT", path, `{"reason":"Changed"}`, browser, "", csrf, "https://events.example", "")
	var edited recommendations.Item
	_ = json.Unmarshal(response.Body.Bytes(), &edited)
	if response.Code != 200 || !edited.RecommendedAt.Equal(first.RecommendedAt) || edited.Reason != "Changed" {
		t.Fatal("edit changed publication", response.Body.String())
	}
	for _, path := range []string{"/api/v1/recommendations", "/api/v1/events/" + id + "/recommendations", "/api/v1/users/" + account.account.ID + "/event-recommendations"} {
		response = accountRequest(a, "GET", path, "", "", "", "", "", "")
		if response.Code != 200 || !strings.Contains(response.Body.String(), "Changed") || response.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(path, response.Code, response.Body.String())
		}
		for _, private := range []string{"private@example.com", "Secret city", "email_reminders", "interest_visibility", "csrf_token"} {
			if strings.Contains(response.Body.String(), private) {
				t.Fatal("public recommendation leaked", private)
			}
		}
	}
	other := accounts.ID()
	repo.items[other] = map[string]recommendations.Item{id: first}
	for range 2 {
		if response := accountRequest(a, "DELETE", path, "", "", bearer, "", "", ""); response.Code != 204 {
			t.Fatal(response.Code)
		}
	}
	if len(repo.items[other]) != 1 || len(repo.items[account.account.ID]) != 0 {
		t.Fatal("withdrawal crossed ownership")
	}
	response = accountRequest(a, "GET", path, "", "", bearer, "", "", "")
	if response.Code != 404 {
		t.Fatal("missing own recommendation", response.Code)
	}
	repo.unavailable = true
	if response := accountRequest(a, "GET", "/api/v1/recommendations", "", "", "", "", "", ""); response.Code != 503 {
		t.Fatal("storage outage invented empty collection")
	}
	if response := accountRequest(&api{}, "GET", "/api/v1/recommendations", "", "", "", "", "", ""); response.Code != 503 {
		t.Fatal("disabled recommendations fabricated public data")
	}
}
func TestRecommendationNativeFormsPreserveDraftsEscapeContentAndReturnContext(t *testing.T) {
	a, account, repo, browser, _, csrf, id := recommendationAPI(t)
	asset := accountRequest(a, "GET", "/assets/recommendations.js", "", "", "", "", "", "")
	if asset.Code != 200 || !strings.Contains(asset.Body.String(), "data-recommendation-form") {
		t.Fatal("recommendation enhancement is not shipped", asset.Code)
	}
	origin := "/community?city=Berlin&category_id=sports"
	target := "/events/" + id + "?section=community&return_to=" + url.QueryEscape(origin)
	form := url.Values{"csrf_token": {csrf}, "return_to": {target}, "action": {"set"}, "reason": {"<script>bad()</script>"}}
	response := accountRequest(a, "POST", "/recommendations/"+id, form.Encode(), browser, "", "", "", "application/x-www-form-urlencoded")
	if response.Code != 303 || response.Header().Get("Location") != target {
		t.Fatal("native write lost event return", response.Code, response.Body.String())
	}
	response = accountRequest(a, "GET", target, "", browser, "", "", "", "")
	if response.Code != 200 || strings.Contains(response.Body.String(), "<script>bad()") || !strings.Contains(response.Body.String(), "&lt;script&gt;bad()") || !strings.Contains(response.Body.String(), "Back to Community") || !strings.Contains(response.Body.String(), "Edit your recommendation") {
		t.Fatal("event attribution not escaped or return lost", response.Code, response.Body.String())
	}
	response = accountRequest(a, "GET", "/users/"+account.account.ID, "", "", "", "", "", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "&lt;script&gt;bad()") || strings.Contains(response.Body.String(), account.account.Email) {
		t.Fatal("public profile recommendation leaked or missing", response.Body.String())
	}
	form.Set("reason", strings.Repeat("京", 501))
	response = accountRequest(a, "POST", "/recommendations/"+id, form.Encode(), browser, "", "", "", "application/x-www-form-urlencoded")
	if response.Code != 400 || !strings.Contains(response.Body.String(), form.Get("reason")) || !strings.Contains(response.Body.String(), "Retry recommendation") || repo.items[account.account.ID][id].Reason != "<script>bad()</script>" {
		t.Fatal("failed native write lost draft or changed confirmed state", response.Code)
	}
	for _, raw := range []string{"https://evil.example/community", "//evil.example/community", "/community/evil", "/community?owner=other", "/community?city=a&city=b", "/users/unknown", "/community#bad", "/community?city=%zz"} {
		if got := safeRecommendationReturn(raw); got == raw {
			t.Fatal("unsafe redirect accepted", raw)
		}
	}
	for _, raw := range []string{origin, "/users/" + account.account.ID + "?limit=1", target} {
		if got := safeRecommendationReturn(raw); got != raw {
			t.Fatal("valid return lost", raw, got)
		}
	}
}

func TestRecommendationComposerIntentAndNativeCredentialRecovery(t *testing.T) {
	a, account, repo, browser, _, csrf, id := recommendationAPI(t)
	target := "/events/" + id + "?section=community&recommend=true&return_to=" + url.QueryEscape("/community?city=Berlin")
	response := accountRequest(a, "GET", target, "", browser, "", "", "", "")
	if response.Code != 200 || !strings.Contains(response.Body.String(), "data-recommendation-editor open") || !strings.Contains(response.Body.String(), "autofocus") {
		t.Fatal("Recommend did not open native editor", response.Code, response.Body.String())
	}
	for _, raw := range []string{"/events/" + id + "?recommend=true", "/events/" + id + "?section=community&recommend=false", "/events/" + id + "?section=community&recommend=true&recommend=true"} {
		if response := accountRequest(a, "GET", raw, "", browser, "", "", "", ""); response.Code != 400 {
			t.Fatal("invalid editor intent", raw, response.Code)
		}
	}
	if _, _, _, err := webSearch(url.Values{"selected_event": {id}, "section": {"community"}, "recommend": {"true"}}); err != nil {
		t.Fatal("selected composer URL rejected", err)
	}
	if _, _, _, err := webSearch(url.Values{"recommend": {"true"}}); err == nil {
		t.Fatal("composer intent without selection accepted")
	}
	form := url.Values{"action": {"set"}, "reason": {"<script>keep this draft</script>"}, "return_to": {target}, "csrf_token": {"expired"}}
	for _, tc := range []struct {
		cookie, origin string
		status         int
		signIn         bool
	}{{browser, "", 403, false}, {browser, "https://evil.example", 403, false}, {"", "", 401, true}} {
		response := accountRequest(a, "POST", "/recommendations/"+id, form.Encode(), tc.cookie, "", "", tc.origin, "application/x-www-form-urlencoded")
		if response.Code != tc.status || !strings.Contains(response.Body.String(), "&lt;script&gt;keep this draft&lt;/script&gt;") || !strings.Contains(response.Body.String(), "Back to event") || strings.Contains(response.Body.String(), "<script>keep this") {
			t.Fatal("native credential rejection lost/failed to escape draft", response.Code, response.Body.String())
		}
		if strings.Contains(response.Body.String(), "Sign in to continue") != tc.signIn {
			t.Fatal("incorrect native auth recovery")
		}
		if len(repo.items[account.account.ID]) != 0 {
			t.Fatal("credential failure published a draft")
		}
	}
	form.Set("csrf_token", csrf)
	response = accountRequest(a, "POST", "/recommendations/"+id, form.Encode(), browser, "", "", "", "application/x-www-form-urlencoded")
	if response.Code != 303 || strings.Contains(response.Header().Get("Location"), "recommend=") || repo.items[account.account.ID][id].Reason != form.Get("reason") {
		t.Fatal("verified retry failed or left editor-intent flag", response.Code, response.Body.String())
	}
}
