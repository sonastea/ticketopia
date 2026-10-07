package api

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

type interestStub struct {
	items       map[string]map[string]interests.Item
	profiles    map[string]accounts.PublicProfile
	unavailable bool
}

func (r *interestStub) Get(_ context.Context, owner, id string) (interests.Item, error) {
	if r.unavailable {
		return interests.Item{}, interests.ErrUnavailable
	}
	if item, ok := r.items[owner][id]; ok {
		return item, nil
	}
	return interests.Item{}, interests.ErrNotFound
}
func (r *interestStub) Set(_ context.Context, owner string, detail models.EventDetail, visibility string) (interests.Item, bool, error) {
	if r.items[owner] == nil {
		r.items[owner] = map[string]interests.Item{}
	}
	item, exists := r.items[owner][detail.Item.ID]
	if !exists {
		item = interests.Item{Event: detail.Item, Meta: detail.Meta, InterestedAt: time.Now().UTC()}
	}
	item.Visibility = visibility
	r.items[owner][detail.Item.ID] = item
	return item, !exists, nil
}
func (r *interestStub) Remove(_ context.Context, owner, id string) error {
	delete(r.items[owner], id)
	return nil
}
func (r *interestStub) States(_ context.Context, owner string, ids []string) (map[string]interests.State, error) {
	if r.unavailable {
		return nil, interests.ErrUnavailable
	}
	result := map[string]interests.State{}
	for _, id := range ids {
		var state interests.State
		for account, items := range r.items {
			if item, ok := items[id]; ok {
				state.Count++
				if account == owner {
					state.Interested = true
					state.Visibility = item.Visibility
				}
			}
		}
		result[id] = state
	}
	return result, nil
}
func (r *interestStub) List(_ context.Context, owner string, public bool, _ interests.Query) ([]interests.Item, error) {
	if r.unavailable {
		return nil, interests.ErrUnavailable
	}
	items := []interests.Item{}
	for _, item := range r.items[owner] {
		if !public || item.Visibility == "public" {
			items = append(items, item)
		}
	}
	return items, nil
}
func (r *interestStub) Participants(_ context.Context, id string, _ interests.Query) ([]interests.Participant, error) {
	if r.unavailable {
		return nil, interests.ErrUnavailable
	}
	items := []interests.Participant{}
	for owner, events := range r.items {
		if item, ok := events[id]; ok && item.Visibility == "public" {
			items = append(items, interests.Participant{Profile: r.profiles[owner], InterestedAt: item.InterestedAt})
		}
	}
	return items, nil
}
func interestAPI(t *testing.T) (*api, *accountStub, *interestStub, string, string, string, string) {
	a, account, browser, bearer, csrf := accountAPI(t)
	id := "ticketmaster:Interest"
	sr := &savedStub{items: map[string]map[string]saved.Item{}, detail: models.EventDetail{Item: models.Event{ID: id, Name: "Interest fixture"}, Meta: models.Freshness{DataAsOf: time.Now().UTC()}}}
	a.saved = saved.New(sr, nil)
	r := &interestStub{items: map[string]map[string]interests.Item{}, profiles: map[string]accounts.PublicProfile{account.account.ID: account.account.Public()}}
	a.interests = interests.New(r, a.saved)
	return a, account, r, browser, bearer, csrf, id
}
func TestInterestAPIPrivacyDefaultsOwnershipAndValidation(t *testing.T) {
	a, account, repo, browser, bearer, csrf, id := interestAPI(t)
	path := "/api/v1/me/event-interests/" + id
	for _, tc := range []struct {
		method, path, body, cookie, token, csrf, origin string
		status                                          int
	}{
		{"GET", "/api/v1/me/event-interests", "", "", "", "", "", 401},
		{"PUT", path, `{}`, browser, "", "", "", 403},
		{"PUT", path, `{}`, browser, "", csrf, "https://evil.example", 403},
		{"PUT", path + "?owner=other", `{}`, "", bearer, "", "", 400},
		{"PUT", path, `{"account_id":"other"}`, "", bearer, "", "", 400},
		{"PUT", path, `{"visibility":null}`, "", bearer, "", "", 400},
		{"PUT", path, `{"visibility":"public","visibility":"private"}`, "", bearer, "", "", 400},
		{"PUT", path, `{"visibility":"friends"}`, "", bearer, "", "", 400},
		{"PUT", path, `[]`, "", bearer, "", "", 400},
		{"PUT", "/api/v1/me/event-interests/invalid", `{}`, "", bearer, "", "", 400},
		{"PUT", path, `{}`, browser, "", csrf, "https://events.example", 201},
		{"PUT", path, `{}`, "", bearer, "", "", 200},
		{"GET", path, "", "", bearer, "", "", 200},
		{"GET", path + "?owner=other", "", "", bearer, "", "", 400},
		{"GET", "/api/v1/me/event-interests?limit=101", "", "", bearer, "", "", 400},
		{"GET", "/api/v1/me/event-interests/states?owner=other", "", "", bearer, "", "", 400},
		{"GET", "/api/v1/me/event-interests/states?event_id=" + id, "", browser, "", "", "", 200},
		{"DELETE", path, `{}`, "", bearer, "", "", 400},
		{"DELETE", path, "", browser, "", "", "", 403},
	} {
		w := accountRequest(a, tc.method, tc.path, tc.body, tc.cookie, tc.token, tc.csrf, tc.origin, "")
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if tc.status == 201 && w.Header().Get("Location") != path {
			t.Fatal("missing interest location")
		}
	}
	owner := account.account.ID
	first := repo.items[owner][id]
	if first.Visibility != "private" {
		t.Fatal("new interest not private by default")
	}
	account.account.InterestVisibility = "public"
	w := accountRequest(a, "PUT", path, `{}`, "", bearer, "", "", "")
	if w.Code != 200 || repo.items[owner][id].Visibility != "private" || !repo.items[owner][id].InterestedAt.Equal(first.InterestedAt) {
		t.Fatal("new default published an existing private choice")
	}
	for _, route := range []string{"/api/v1/users/" + owner + "/event-interests", "/api/v1/events/" + id + "/interested-users", "/users/" + owner} {
		w := accountRequest(a, "GET", route, "", "", "", "", "", "")
		if w.Code != 200 || strings.Contains(w.Body.String(), first.Event.Name) || strings.Contains(w.Body.String(), "private@example.com") {
			t.Fatal("private interest/profile data leaked", route, w.Body.String())
		}
	}
	w = accountRequest(a, "GET", "/api/v1/events/"+id+"/interest", "", "", "", "", "", "")
	var count map[string]int
	if json.Unmarshal(w.Body.Bytes(), &count) != nil || count["count"] != 1 || len(count) != 1 {
		t.Fatal("public count leaked viewer fields or omitted private interest", w.Body.String())
	}
	w = accountRequest(a, "PUT", path, `{"visibility":"public"}`, "", bearer, "", "", "")
	if w.Code != 200 || !repo.items[owner][id].InterestedAt.Equal(first.InterestedAt) {
		t.Fatal("visibility update reset choice")
	}
	w = accountRequest(a, "GET", "/api/v1/events/"+id+"/interested-users", "", "", "", "", "", "")
	if !strings.Contains(w.Body.String(), owner) || strings.Contains(w.Body.String(), account.account.Email) {
		t.Fatal("public participant projection wrong")
	}
	w = accountRequest(a, "GET", "/users/"+owner, "", "", "", "", "", "")
	if !strings.Contains(w.Body.String(), first.Event.Name) {
		t.Fatal("public activity absent from profile")
	}
	account.account.ID = accounts.ID()
	w = accountRequest(a, "GET", path, "", "", bearer, "", "", "")
	if w.Code != 404 {
		t.Fatal("owner read another choice")
	}
	w = accountRequest(a, "DELETE", path, "", "", bearer, "", "", "")
	if w.Code != 204 || len(repo.items[owner]) != 1 {
		t.Fatal("foreign delete changed choice")
	}
	account.account.ID = owner
	for range 2 {
		if w := accountRequest(a, "DELETE", path, "", "", bearer, "", "", ""); w.Code != 204 {
			t.Fatal("remove not idempotent")
		}
	}
	if w := accountRequest(a, "GET", "/api/v1/events/"+id+"/interested-users", "", "", "", "", "", ""); strings.Contains(w.Body.String(), owner) {
		t.Fatal("removed identity remained public")
	}
	repo.unavailable = true
	if w := accountRequest(a, "GET", "/api/v1/events/"+id+"/interest", "", "", "", "", "", ""); w.Code != 503 {
		t.Fatal("failed count fabricated zero")
	}
}
func TestInterestOrdinaryFormsRetainEventAndCollectionContext(t *testing.T) {
	a, _, repo, browser, _, csrf, id := interestAPI(t)
	for _, target := range []string{"/?city=Berlin&selected_event=" + id, "/me/interests?limit=1&cursor=abc", "/saved?limit=1", "/events/" + id + "?section=community"} {
		for _, action := range []string{"set", "remove"} {
			form := url.Values{"csrf_token": {csrf}, "action": {action}, "visibility": {"private"}, "return_to": {target}}
			w := accountRequest(a, "POST", "/interests/"+id, form.Encode(), browser, "", "", "https://events.example", "application/x-www-form-urlencoded")
			if w.Code != 303 || w.Header().Get("Location") != target {
				t.Fatal("native interest lost task", w.Code, w.Body.String())
			}
		}
	}
	if len(repo.items) != 1 {
		t.Fatal("unexpected ownership")
	}
	for _, raw := range []string{"//evil.example/", "https://evil.example/me/interests", "/me/interests#outside", "/me/interests%2Fother", "/me/interests\\evil"} {
		if got := safeInterestReturn(raw); got != "/me" {
			t.Fatal("unsafe interest return", raw, got)
		}
	}
	if got := safeAuthReturn("/me/interests?limit=1&cursor=abc"); got != "/me/interests?limit=1&cursor=abc" {
		t.Fatal("sign-in lost interest pagination", got)
	}
	if got := safeAuthReturn("/me/interests?cursor=%zz"); got != "/me/interests" {
		t.Fatal("malformed collection return was not bounded", got)
	}
	a.authConfig.Enabled = false
	if w := accountRequest(a, "GET", "/api/v1/events/"+id+"/interest", "", "", "", "", "", ""); w.Code != 503 {
		t.Fatal("disabled interest fabricated availability")
	}
}

func TestParticipantPagesPreserveOnlySafeReturnContext(t *testing.T) {
	a, account, repo, _, _, _, id := interestAPI(t)
	// Two public identities force participant pagination in this handler test.
	item := interests.Item{Event: models.Event{ID: id}, Visibility: "public", InterestedAt: time.Now().UTC()}
	repo.items[account.account.ID] = map[string]interests.Item{id: item}
	other := accounts.ID()
	repo.items[other] = map[string]interests.Item{id: item}
	repo.profiles[other] = accounts.PublicProfile{ID: other, DisplayName: "Other explorer"}
	for _, origin := range []string{"/?city=Berlin&limit=2", "/me/interests?limit=1&cursor=abc", "/saved?limit=1"} {
		path := "/events/" + id + "/interested-users?limit=1&return_to=" + url.QueryEscape(origin)
		w := accountRequest(a, "GET", path, "", "", "", "", "", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), "return_to="+url.QueryEscape(origin)) {
			t.Fatal("participants lost event return context", w.Code, w.Body.String())
		}
	}
	w := accountRequest(a, "GET", "/events/"+id+"/interested-users?return_to="+url.QueryEscape("https://evil.example/"), "", "", "", "", "", "")
	if w.Code != 200 || strings.Contains(w.Body.String(), "evil.example") {
		t.Fatal("unsafe participant return reflected", w.Body.String())
	}
	w = accountRequest(a, "GET", "/events/"+id+"/interested-users?return_to=%2F&return_to=%2Fsaved", "", "", "", "", "", "")
	if w.Code != 400 {
		t.Fatal("repeated participant return accepted", w.Code)
	}
}
