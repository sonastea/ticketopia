package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/follows"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

type followStub struct {
	follows.Repository
	items       map[string]map[string]follows.Item
	unavailable bool
}

func (r *followStub) Get(_ context.Context, owner, kind, id string) (follows.Item, error) {
	if r.unavailable {
		return follows.Item{}, follows.ErrUnavailable
	}
	if item, ok := r.items[owner][kind+":"+id]; ok {
		return item, nil
	}
	return follows.Item{}, follows.ErrNotFound
}
func (r *followStub) Snapshot(_ context.Context, kind, id string) (models.CatalogDetail, error) {
	if id != "ticketmaster:One" {
		return models.CatalogDetail{}, follows.ErrNotFound
	}
	return models.CatalogDetail{Item: models.FollowTarget{Kind: kind, ID: id, Name: "Private artist <script>"}, Meta: models.Freshness{DataAsOf: time.Now(), Stale: true}}, nil
}
func (r *followStub) Follow(_ context.Context, owner string, detail models.CatalogDetail) (follows.Item, bool, error) {
	if r.items[owner] == nil {
		r.items[owner] = map[string]follows.Item{}
	}
	item := follows.Item{Target: detail.Item, Meta: detail.Meta, FollowedAt: time.Now().UTC()}
	r.items[owner][detail.Item.Kind+":"+detail.Item.ID] = item
	return item, true, nil
}
func (r *followStub) Remove(_ context.Context, owner, kind, id string) error {
	delete(r.items[owner], kind+":"+id)
	return nil
}
func (r *followStub) List(_ context.Context, owner string, q follows.Query) ([]follows.Item, error) {
	if r.unavailable {
		return nil, follows.ErrUnavailable
	}
	items := []follows.Item{}
	for _, item := range r.items[owner] {
		if q.Kind == "" || item.Target.Kind == q.Kind {
			items = append(items, item)
		}
	}
	return items, nil
}
func (r *followStub) States(_ context.Context, owner, kind string, ids []string) (map[string]bool, error) {
	result := map[string]bool{}
	for _, id := range ids {
		_, result[id] = r.items[owner][kind+":"+id]
	}
	return result, nil
}
func TestFollowAPIAuthenticationOwnershipCSRFAndIdempotency(t *testing.T) {
	a, auth, browser, bearer, csrf := accountAPI(t)
	r := &followStub{items: map[string]map[string]follows.Item{}}
	a.follows = follows.New(r, nil)
	path := "/api/v1/me/follows/artist/ticketmaster:One"
	for _, tc := range []struct {
		method, path, body, cookie, token, csrf, origin string
		status                                          int
	}{
		{"GET", "/api/v1/me/follows", "", "", "", "", "", 401},
		{"GET", "/api/v1/artists?keyword=a", "", "", "", "", "", 401},
		{"PUT", path, "", browser, "", "", "", 403},
		{"PUT", path, "", browser, "", csrf, "https://evil.example", 403},
		{"PUT", path, `{"account_id":"other"}`, "", bearer, "", "", 400},
		{"PUT", path + "?visibility=public", "", "", bearer, "", "", 400},
		{"PUT", "/api/v1/me/follows/user/ticketmaster:One", "", "", bearer, "", "", 400},
		{"PUT", "/api/v1/me/follows/artist/bad", "", "", bearer, "", "", 400},
		{"PUT", path, "", browser, "", csrf, "https://events.example", 201},
		{"PUT", path, "", "", bearer, "", "", 200},
		{"GET", path, "", "", bearer, "", "", 200},
		{"GET", "/api/v1/me/follows?account_id=other", "", "", bearer, "", "", 400},
		{"GET", "/api/v1/me/follows?limit=101", "", "", bearer, "", "", 400},
		{"PUT", "/api/v1/me/follows/artist/ticketmaster:Missing", "", "", bearer, "", "", 404},
	} {
		w := accountRequest(a, tc.method, tc.path, tc.body, tc.cookie, tc.token, tc.csrf, tc.origin, "")
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "private, no-store" {
			t.Fatal(tc.method, tc.path, w.Code, w.Body.String())
		}
		if tc.status == 201 && w.Header().Get("Location") != path {
			t.Fatal("missing created location")
		}
	}
	owner := auth.account.ID
	first := r.items[owner]["artist:ticketmaster:One"]
	w := accountRequest(a, "GET", "/api/v1/me/follows", "", "", bearer, "", "", "")
	if !strings.Contains(w.Body.String(), first.FollowedAt.Format(time.RFC3339Nano)) {
		t.Fatal("follow time missing")
	}
	auth.account.ID = accounts.ID()
	w = accountRequest(a, "GET", path, "", "", bearer, "", "", "")
	if w.Code != 404 {
		t.Fatal("foreign follow disclosed")
	}
	w = accountRequest(a, "GET", "/api/v1/me/follows", "", "", bearer, "", "", "")
	if strings.Contains(w.Body.String(), "Private artist") {
		t.Fatal("foreign collection leaked")
	}
	w = accountRequest(a, "DELETE", path, "", "", bearer, "", "", "")
	if w.Code != 204 || len(r.items[owner]) != 1 {
		t.Fatal("foreign removal changed follow")
	}
	w = accountRequest(a, "GET", "/api/v1/users/"+auth.account.ID, "", "", "", "", "", "")
	if strings.Contains(w.Body.String(), "followed_at") {
		t.Fatal("public profile leaked follows")
	}
	auth.account.ID = owner
	for range 2 {
		if w := accountRequest(a, "DELETE", path, "", "", bearer, "", "", ""); w.Code != 204 {
			t.Fatal("removal not idempotent")
		}
	}
	r.unavailable = true
	if w := accountRequest(a, "GET", "/api/v1/me/follows", "", browser, "", "", "", ""); w.Code != 503 {
		t.Fatal("database failure fabricated empty collection")
	}
}
func TestFollowOrdinaryFormsSearchAndSafeReturns(t *testing.T) {
	a, _, browser, bearer, csrf := accountAPI(t)
	r := &followStub{items: map[string]map[string]follows.Item{}}
	a.follows = follows.New(r, nil)
	cache := kv.NewMemory()
	defer cache.Close()
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/venues.json" {
			fmt.Fprint(w, `{"_embedded":{"venues":[{"id":"One","name":"Private venue","city":{"name":"Boston"}}]},"page":{"number":0,"totalElements":1,"totalPages":1}}`)
		} else {
			fmt.Fprint(w, `{"_embedded":{"attractions":[{"id":"One","name":"Private artist"}]},"page":{"number":0,"totalElements":1,"totalPages":1}}`)
		}
	}))
	defer provider.Close()
	a.events = discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "fixture", BaseURL: provider.URL})
	w := accountRequest(a, "GET", "/follows?kind=venue&keyword=Private", "", "", "", "", "", "")
	if w.Code != 303 || !strings.Contains(w.Header().Get("Location"), "return_to=") {
		t.Fatal("guest search return lost")
	}
	for _, kind := range []string{"artist", "venue"} {
		w = accountRequest(a, "GET", "/api/v1/"+kind+"s?keyword=Private", "", "", bearer, "", "", "")
		if w.Code != 200 || !strings.Contains(w.Body.String(), `"kind":"`+kind+`"`) {
			t.Fatal("authenticated search failed", w.Code, w.Body.String())
		}
		for _, verb := range []string{"follow", "unfollow"} {
			form := url.Values{"csrf_token": {csrf}, "action": {verb}, "return_to": {"/follows?kind=" + kind + "&keyword=Private"}}
			w = accountRequest(a, "POST", "/follows/"+kind+"/ticketmaster:One", form.Encode(), browser, "", "", "https://events.example", "application/x-www-form-urlencoded")
			if w.Code != 303 || w.Header().Get("Location") != form.Get("return_to") {
				t.Fatal("ordinary follow form failed", w.Code, w.Body.String())
			}
			w = accountRequest(a, "GET", form.Get("return_to"), "", browser, "", "", "", "")
			if w.Code != 200 || !strings.Contains(w.Body.String(), "Your follows") {
				t.Fatal("collection rendering failed", w.Code, w.Body.String())
			}
		}
	}
	if w := accountRequest(a, "GET", "/follows?kind=artist&keyword=Private&city=Boston&country=US", "", browser, "", "", "", ""); w.Code != 200 {
		t.Fatal("switching from venue to artist failed")
	}
	for _, raw := range []string{"https://evil.example/follows", "//evil.example/follows", "/follows%2Fevil", "/follows#x", "/follows?owner=other", "/follows\\evil"} {
		if safeFollowReturn(raw) != "/follows" {
			t.Fatal("unsafe follow return", raw)
		}
	}
	if safeAuthReturn("/follows?filter=venue") != "/follows?filter=venue" {
		t.Fatal("sign-in lost follows task")
	}
	a.authConfig.Enabled = false
	w = accountRequest(a, "GET", "/follows", "", "", "", "", "", "")
	if w.Code != 200 || !strings.Contains(w.Body.String(), "Following requires accounts") {
		t.Fatal("disabled accounts not explained", w.Code)
	}
}
