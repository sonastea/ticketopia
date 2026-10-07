package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

type savedStub struct {
	saved.Repository
	items       map[string]map[string]saved.Item
	detail      models.EventDetail
	unavailable bool
}

func (r *savedStub) Get(_ context.Context, owner, id string) (saved.Item, error) {
	if r.unavailable {
		return saved.Item{}, saved.ErrUnavailable
	}
	if item, ok := r.items[owner][id]; ok {
		return item, nil
	}
	return saved.Item{}, saved.ErrNotFound
}
func (r *savedStub) Snapshot(_ context.Context, id string) (models.EventDetail, error) {
	if id == r.detail.Item.ID {
		return r.detail, nil
	}
	return models.EventDetail{}, saved.ErrNotFound
}
func (r *savedStub) Save(_ context.Context, owner string, d models.EventDetail) (saved.Item, bool, error) {
	if r.items[owner] == nil {
		r.items[owner] = map[string]saved.Item{}
	}
	item := saved.Item{Event: d.Item, SavedAt: time.Now().UTC(), Meta: d.Meta}
	r.items[owner][d.Item.ID] = item
	return item, true, nil
}
func (r *savedStub) Remove(_ context.Context, owner, id string) error {
	delete(r.items[owner], id)
	return nil
}
func (r *savedStub) List(_ context.Context, owner string, _ saved.Query) ([]saved.Item, error) {
	if r.unavailable {
		return nil, saved.ErrUnavailable
	}
	items := []saved.Item{}
	for _, item := range r.items[owner] {
		items = append(items, item)
	}
	return items, nil
}
func (r *savedStub) States(_ context.Context, owner string, ids []string) (map[string]bool, error) {
	result := map[string]bool{}
	for _, id := range ids {
		_, result[id] = r.items[owner][id]
	}
	return result, nil
}
func TestSavedAPIPrivacyCSRFAndRetries(t *testing.T) {
	a, accountRepo, browser, bearer, csrf := accountAPI(t)
	event := models.Event{ID: "ticketmaster:Private", Name: "Private collection show", Source: models.Source{Provider: "ticketmaster", ID: "Private"}}
	r := &savedStub{items: map[string]map[string]saved.Item{}, detail: models.EventDetail{Item: event, Meta: models.Freshness{DataAsOf: time.Now()}}}
	a.saved = saved.New(r, nil)
	path := "/api/v1/me/saved-events/" + event.ID
	for _, tc := range []struct {
		method, path, body, cookie, token, csrf, origin string
		status                                          int
	}{
		{"GET", "/api/v1/me/saved-events", "", "", "", "", "", 401},
		{"PUT", path, "", browser, "", "", "", 403},
		{"PUT", path, "", browser, "", csrf, "https://evil.example", 403},
		{"PUT", path, `{"account_id":"other"}`, "", bearer, "", "", 400},
		{"PUT", path + "?owner=other", "", "", bearer, "", "", 400},
		{"PUT", "/api/v1/me/saved-events/invalid", "", "", bearer, "", "", 400},
		{"PUT", path, "", browser, "", csrf, "https://events.example", 201},
		{"PUT", path, "", "", bearer, "", "", 200},
		{"GET", path, "", "", bearer, "", "", 200},
		{"GET", "/api/v1/me/saved-events?limit=101", "", "", bearer, "", "", 400},
		{"GET", "/api/v1/me/saved-events?account_id=other", "", "", bearer, "", "", 400},
		{"GET", "/api/v1/me/saved-events/states?event_id=" + event.ID, "", browser, "", "", "", 200},
	} {
		w := accountRequest(a, tc.method, tc.path, tc.body, tc.cookie, tc.token, tc.csrf, tc.origin, "")
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
		}
		if tc.status == 201 && w.Header().Get("Location") != path {
			t.Fatal("created save missing resolvable location")
		}
	}
	owner := accountRepo.account.ID
	public := accountRequest(a, "GET", "/api/v1/events/"+event.ID, "", "", "", "", "", "")
	if public.Code != 200 || strings.Contains(public.Body.String(), "saved_at") || strings.Contains(public.Body.String(), owner) {
		t.Fatal("public fallback leaked bookmark ownership", public.Code, public.Body.String())
	}
	invalid := accountRequest(a, "GET", "/api/v1/events/invalid", "", "", "", "", "", "")
	if invalid.Code != 400 || !strings.Contains(invalid.Body.String(), "invalid_query") {
		t.Fatal("public event validation contract changed", invalid.Code, invalid.Body.String())
	}
	item := r.items[owner][event.ID]
	w := accountRequest(a, "GET", "/api/v1/me/saved-events", "", "", bearer, "", "", "")
	var list saved.List
	if json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Items) != 1 || !list.Items[0].SavedAt.Equal(item.SavedAt) {
		t.Fatal("retry changed saved timestamp")
	}
	accountRepo.account.ID = accounts.ID()
	w = accountRequest(a, "GET", "/api/v1/me/saved-events", "", "", bearer, "", "", "")
	if strings.Contains(w.Body.String(), event.Name) {
		t.Fatal("another owner read private save")
	}
	w = accountRequest(a, "GET", path, "", "", bearer, "", "", "")
	if w.Code != 404 {
		t.Fatal("foreign save exists publicly")
	}
	w = accountRequest(a, "DELETE", path, "", "", bearer, "", "", "")
	if w.Code != 204 || len(r.items[owner]) != 1 {
		t.Fatal("another owner removed bookmark")
	}
	accountRepo.account.ID = owner
	for range 2 {
		if w := accountRequest(a, "DELETE", path, "", "", bearer, "", "", ""); w.Code != 204 {
			t.Fatal("delete not idempotent")
		}
	}
	if _, err := r.Get(t.Context(), owner, event.ID); !errors.Is(err, saved.ErrNotFound) {
		t.Fatal("bookmark remained")
	}
	r.unavailable = true
	w = accountRequest(a, "GET", "/api/v1/me/saved-events", "", browser, "", "", "", "")
	if w.Code != 503 {
		t.Fatal("unavailable collection failed open")
	}
}
func TestSavedOrdinaryFormsAndReturnContext(t *testing.T) {
	a, _, browser, _, csrf := accountAPI(t)
	id := "ticketmaster:Form"
	r := &savedStub{items: map[string]map[string]saved.Item{}, detail: models.EventDetail{Item: models.Event{ID: id, Name: "Form show"}, Meta: models.Freshness{DataAsOf: time.Now()}}}
	a.saved = saved.New(r, nil)
	w := accountRequest(a, "GET", "/saved", "", "", "", "", "", "")
	if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "return_to=%2Fsaved") {
		t.Fatal("guest task lost")
	}
	for _, action := range []string{"save", "remove"} {
		form := url.Values{"csrf_token": {csrf}, "action": {action}, "return_to": {"/?city=Berlin&selected_event=" + id}}
		w = accountRequest(a, "POST", "/saved/"+id, form.Encode(), browser, "", "", "https://events.example", "application/x-www-form-urlencoded")
		if w.Code != 303 || w.Header().Get("Location") != form.Get("return_to") {
			t.Fatal("ordinary save form/context failed", w.Code, w.Body.String())
		}
	}
	for _, raw := range []string{"//evil.example/", "https://evil.example/saved", "/saved#outside", "/saved%2Fother", "/saved\\evil"} {
		if got := safeSaveReturn(raw); got != "/me" {
			t.Fatal("unsafe redirect", raw, got)
		}
	}
	if got := safeReturnURL("/saved?limit=1&cursor=abc"); got != "/saved?limit=1&cursor=abc" {
		t.Fatal("lost Saved return", got)
	}
}
