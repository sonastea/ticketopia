package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

func TestIfNoneMatchGrammar(t *testing.T) {
	for _, tc := range []struct {
		fields []string
		match  bool
	}{
		{nil, false}, {[]string{`W/"abc"`}, true}, {[]string{`"abc"`}, true},
		{[]string{`"other", W/"abc"`}, true}, {[]string{`"other"`, `"abc"`}, true},
		{[]string{`"a,b", "abc"`}, true}, {[]string{" \t* \t"}, true},
		{[]string{`"a,b"`}, false}, {[]string{`w/"abc"`}, false},
		{[]string{`"abc", garbage`}, false}, {[]string{`"abc",`}, false},
		{[]string{`"abc"`, `*`}, false}, {[]string{`W/ "abc"`}, false},
		{[]string{`"abc" "other"`}, false}, {[]string{`"abc`}, false},
		{[]string{"\"abc\", \"bad\x7f\""}, false}, {[]string{`"a b", "abc"`}, false},
	} {
		if got := matchesIfNoneMatch(tc.fields, `W/"abc"`); got != tc.match {
			t.Errorf("%q: got %v want %v", tc.fields, got, tc.match)
		}
	}
	if !matchesIfNoneMatch([]string{`"a,b"`}, `W/"a,b"`) {
		t.Fatal("opaque comma did not match")
	}
}

func publicRequest(h http.Handler, path, tag string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, path, nil)
	if tag != "" {
		r.Header.Set("If-None-Match", tag)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestPublicRepresentationAndEligibility(t *testing.T) {
	a := &api{authConfig: accounts.Config{BaseURL: "https://example.com"}}
	e := echo.New()
	data := models.EventList{Items: []models.Event{}, Meta: models.Freshness{DataAsOf: time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)}}
	e.GET("/", func(c echo.Context) error {
		c.Response().Header().Add("Vary", "Origin")
		return a.publicJSON(c, data, eventCachePolicy(data.Meta.Stale))
	}, a.publicCacheBoundary)
	first := publicRequest(e, "/", "")
	tag := first.Header().Get("ETag")
	if first.Code != 200 || first.Body.Len() == 0 || !strings.HasPrefix(tag, `W/"v1-`) || first.Header().Get("Cache-Control") != "public, no-cache" {
		t.Fatalf("initial: %v", first)
	}
	matched := publicRequest(e, "/", tag)
	if matched.Code != 304 || matched.Body.Len() != 0 || matched.Header().Get("ETag") != tag || matched.Header().Get("Vary") != "Origin" || matched.Header().Get("Cache-Control") != "public, no-cache" {
		t.Fatalf("conditional: %v", matched)
	}
	for _, tc := range []struct {
		fields []string
		status int
	}{
		{[]string{strings.TrimPrefix(tag, "W/")}, 304},
		{[]string{`"comma,in,tag"`, tag}, 304},
		{[]string{"*"}, 304},
		{[]string{tag + ", malformed"}, 200},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		for _, field := range tc.fields {
			r.Header.Add("If-None-Match", field)
		}
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		if w.Code != tc.status || (w.Code == 304 && w.Body.Len() != 0) {
			t.Fatalf("headers %v: %v", tc.fields, w)
		}
	}
	// Compare with Echo's existing encoding, including newline/HTML escaping/pretty.
	for _, path := range []string{"/", "/?pretty"} {
		w := httptest.NewRecorder()
		c := e.NewContext(httptest.NewRequest("GET", path, nil), w)
		if err := c.JSON(200, data); err != nil {
			t.Fatal(err)
		}
		if got := publicRequest(e, path, ""); got.Body.String() != w.Body.String() || got.Header().Get("Content-Type") != w.Header().Get("Content-Type") {
			t.Fatal("JSON encoding changed")
		}
	}
	for _, mutate := range []func(){
		func() { data.Meta.DataAsOf = data.Meta.DataAsOf.Add(time.Second) },
		func() { data.Meta.Coverage = &models.Coverage{Status: "complete", TotalDays: 1, CollectedDays: 1} },
		func() { data.Total++ }, func() { cursor := "next"; data.NextCursor = &cursor },
		func() { data.Limited = true }, func() { data.Items = []models.Event{{ID: "ticketmaster:changed"}} },
	} {
		mutate()
		w := publicRequest(e, "/", tag)
		if w.Code != 200 || w.Header().Get("ETag") == tag {
			t.Fatal("changed representation matched")
		}
		tag = w.Header().Get("ETag")
	}
	for _, header := range []http.Header{
		{"Authorization": {""}}, {"Authorization": {"Bearer invalid"}},
		{"Cookie": {a.authConfig.CookieName("session") + "=invalid"}},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header = header
		r.Header.Set("If-None-Match", "*")
		w := httptest.NewRecorder()
		e.ServeHTTP(w, r)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "private, no-store" || w.Header().Get("ETag") != "" {
			t.Fatalf("private: %v", w)
		}
	}
	data.Meta.Stale = true
	w := publicRequest(e, "/", "*")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" {
		t.Fatal("stale response cached")
	}
}

func TestTaxonomyLifetime(t *testing.T) {
	now := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		remaining time.Duration
		want      string
	}{
		{24 * time.Hour, "public, max-age=600, must-revalidate"},
		{125*time.Second + 900*time.Millisecond, "public, max-age=125, must-revalidate"},
		{900 * time.Millisecond, "public, max-age=0, must-revalidate"},
		{0, "no-store"}, {-time.Second, "no-store"},
	} {
		meta := models.Freshness{DataAsOf: now.Add(-time.Hour), FreshUntil: now.Add(tc.remaining)}
		if got := taxonomyCachePolicy(meta, now); got != tc.want {
			t.Errorf("%v: %s", tc.remaining, got)
		}
		meta.Stale = true
		if taxonomyCachePolicy(meta, now) != "no-store" {
			t.Fatal("stale taxonomy cached")
		}
	}
	if taxonomyCachePolicy(models.Freshness{}, now) != "no-store" {
		t.Fatal("unknown freshness cached")
	}
}

type publicCatalog struct {
	discovery.Catalog
	list    models.EventList
	err     error
	calls   int
	date    string
	history models.EventHistory
}

func (s *publicCatalog) Search(_ context.Context, q discovery.Query, _ time.Duration) (discovery.CatalogRead, error) {
	s.calls++
	list := s.list
	if s.date != "" {
		list.Items = []models.Event{{Name: q.StartDate}}
	}
	return discovery.CatalogRead{List: list}, s.err
}
func (s *publicCatalog) Snapshot(_ context.Context, id string) (models.EventDetail, error) {
	if id == "ticketmaster:example" {
		return models.EventDetail{Item: models.Event{ID: id}, Meta: s.list.Meta}, s.err
	}
	return models.EventDetail{}, discovery.ErrNotFound
}
func (s *publicCatalog) History(context.Context, string, time.Time, int) (*models.EventHistory, error) {
	s.calls++
	return &s.history, s.err
}

func TestPublicServicePrecedenceAndUTCBoundary(t *testing.T) {
	cache := kv.NewMemory()
	t.Cleanup(func() { cache.Close() })
	store := &publicCatalog{list: models.EventList{Items: []models.Event{}, Meta: models.Freshness{DataAsOf: time.Now().UTC()}}}
	upstream := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(upstream.Close)
	s := discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{Catalog: store, APIKey: "test", BaseURL: upstream.URL})
	now := time.Date(2026, 10, 15, 23, 59, 59, 0, time.UTC)
	a := &api{events: s, now: func() time.Time { return now }}
	routes := a.Routes()
	first := publicRequest(routes, "/api/v1/events", "")
	second := publicRequest(routes, "/api/v1/events", first.Header().Get("ETag"))
	if first.Code != 200 || second.Code != 304 || store.calls != 2 {
		t.Fatalf("service bypass: %d %d calls=%d", first.Code, second.Code, store.calls)
	}
	for _, tc := range []struct {
		path   string
		status int
	}{
		{"/api/v1/events?limit=bad", 400}, {"/api/v1/events/ticketmaster:missing", 404},
		{"/api/v1/events/ticketmaster:missing/history?limit=bad", 400},
		{"/api/v1/categories?bad=1", 400}, {"/api/v1/genres?bad=1", 400}, {"/api/v1/openapi.yaml?bad=1", 400},
	} {
		w := publicRequest(routes, tc.path, "*")
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d %v", tc.path, w.Code, w.Header())
		}
	}
	store.err = &discovery.UnavailableError{RetryAfter: 42 * time.Second}
	w := publicRequest(routes, "/api/v1/events", "*")
	if w.Code != 503 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Retry-After") != "42" {
		t.Fatalf("error: %v", w)
	}
	store.err = nil
	store.date = "default"
	first = publicRequest(routes, "/api/v1/events", "")
	now = now.Add(2 * time.Second)
	second = publicRequest(routes, "/api/v1/events", first.Header().Get("ETag"))
	if second.Code != 200 || second.Body.String() == first.Body.String() {
		t.Fatal("UTC date defaults not reevaluated")
	}
	for _, path := range []string{"/api/v1/events", "/api/v1/events/ticketmaster:example", "/api/v1/events/ticketmaster:example/history", "/api/v1/openapi.yaml"} {
		first = publicRequest(routes, path, "")
		other := (&api{events: s, now: a.now}).Routes()
		second = publicRequest(other, path, first.Header().Get("ETag"))
		if first.Code != 200 || second.Code != 304 || second.Body.Len() != 0 {
			t.Fatalf("replica mismatch %s: %d %d", path, first.Code, second.Code)
		}
	}
	for _, path := range []string{"/api/v1/events/ticketmaster:example", "/api/v1/events/ticketmaster:example/history"} {
		first = publicRequest(routes, path, "")
		store.history.LastSeen = store.history.LastSeen.Add(time.Second)
		second = publicRequest(routes, path, first.Header().Get("ETag"))
		if second.Code != 200 || second.Header().Get("ETag") == first.Header().Get("ETag") {
			t.Fatal("history change ignored")
		}
	}
	store.list.Meta.Stale = true
	for _, path := range []string{"/api/v1/events", "/api/v1/events/ticketmaster:example"} {
		w := publicRequest(routes, path, "*")
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("stale %s: %v", path, w)
		}
	}
}

func TestTaxonomyDiscoveryDeadline(t *testing.T) {
	now := time.Now().UTC()
	cache := kv.NewMemory()
	t.Cleanup(func() { cache.Close() })
	a := &api{events: discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{}), now: func() time.Time { return now }}
	for _, tc := range []struct {
		path, key string
		data      any
	}{
		{"/api/v1/categories", "ticketmaster:v1:categories:en", []models.Category{}},
		{"/api/v1/genres", "ticketmaster:v1:genres:music:en", []models.Genre{}},
	} {
		// Seed the discovery record with a deliberately shorter deadline. The API
		// must use that deadline, not derive a new 24-hour lifetime from data_as_of.
		body, err := json.Marshal(map[string]any{"data": tc.data, "fetched_at": now.Add(-time.Hour), "fresh_until": now.Add(90 * time.Second), "stale_until": now.Add(48 * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if err := cache.Set(t.Context(), tc.key, body, time.Hour); err != nil {
			t.Fatal(err)
		}
		w := publicRequest(a.Routes(), tc.path, "")
		if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=90, must-revalidate" {
			t.Fatalf("deadline: %d %s %v", w.Code, w.Body, w.Header())
		}
		if got := publicRequest(a.Routes(), tc.path, w.Header().Get("ETag")); got.Code != 304 {
			t.Fatal("taxonomy validator ignored")
		}
		now = now.Add(91 * time.Second)
		if got := publicRequest(a.Routes(), tc.path, "*"); got.Code != 200 || got.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("expired HTTP lifetime cached")
		}
		now = now.Add(-91 * time.Second)
		body, err = json.Marshal(map[string]any{"data": tc.data, "fetched_at": now.Add(-25 * time.Hour), "fresh_until": now.Add(-time.Hour), "stale_until": now.Add(48 * time.Hour)})
		if err != nil {
			t.Fatal(err)
		}
		if err := cache.Set(t.Context(), tc.key, body, time.Hour); err != nil {
			t.Fatal(err)
		}
		fallback := publicRequest(a.Routes(), tc.path, "*")
		if fallback.Code != 200 || fallback.Header().Get("Cache-Control") != "no-store" || !strings.Contains(fallback.Body.String(), `"stale":true`) {
			t.Fatalf("stale fallback: %v", fallback)
		}
	}
}

func TestPublicSerializationError(t *testing.T) {
	a := &api{}
	e := echo.New()
	e.GET("/", func(c echo.Context) error { return a.publicJSON(c, make(chan int), "public, no-cache") }, a.publicCacheBoundary)
	w := publicRequest(e, "/", "*")
	if w.Code != 500 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" {
		t.Fatal("serialization error cached")
	}
}
