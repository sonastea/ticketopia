package api

import (
	"encoding/json"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

func categoryFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("../discovery/testdata/categories.json")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestCategoryWebFiltersPaginationAndDetails(t *testing.T) {
	fixture := categoryFixture(t)
	var eventCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/classifications.json" {
			_, _ = w.Write(fixture)
			return
		}
		eventCalls.Add(1)
		q := r.URL.Query()
		category, genre, page := q.Get("segmentId"), q.Get("genreId"), q.Get("page")
		if genre == "rock" && category != "KZFzniwnSyZfZ7v7nJ" {
			t.Error("category switch retained the old music genre")
		}
		if q.Get("keyword") == "empty" {
			_, _ = fmt.Fprint(w, `{"page":{"number":0,"totalElements":0,"totalPages":0}}`)
			return
		}
		name := map[string]string{"KZFzniwnSyZfZ7v7nJ": "Music", "KZFzniwnSyZfZ7v7nE": "Sports", "KZFzniwnSyZfZ7v7na": "Arts & Theatre"}[category]
		_, _ = fmt.Fprintf(w, `{"page":{"number":%s,"size":1,"totalElements":2,"totalPages":2},"_embedded":{"events":[{"id":"event-%s-%s","name":"Local event","url":"https://www.ticketmaster.com/event/example","classifications":[{"primary":true,"segment":{"id":%q,"name":%q}}]}]}}`, page, category, page, category, name)
	}))
	defer upstream.Close()
	cache := kv.NewMemory()
	defer cache.Close()
	a := &api{events: discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL})}
	routes := a.Routes()
	request := func(path string, partial bool) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if partial {
			req.Header.Set("HX-Request", "true")
		}
		routes.ServeHTTP(rec, req)
		return rec
	}
	base := "city=Chicago&country=US&start_date=2026-10-03&end_date=2026-12-31&limit=1"
	for _, tc := range []struct{ name, query, selected, genre, absent string }{
		{"all categories", "category_id=all", "all", "", "rock"},
		{"legacy music genre", "genre_id=rock", "KZFzniwnSyZfZ7v7nJ", "rock", "basketball"},
		{"switch music to sports without JS", "category_id=KZFzniwnSyZfZ7v7nE&genre_id=rock&genre_category_id=KZFzniwnSyZfZ7v7nJ", "KZFzniwnSyZfZ7v7nE", "", "rock"},
		{"sports genre without JS", "category_id=KZFzniwnSyZfZ7v7nE&genre_id=basketball&genre_category_id=KZFzniwnSyZfZ7v7nE", "KZFzniwnSyZfZ7v7nE", "basketball", "rock"},
		{"switch sports to all without JS", "category_id=all&genre_id=basketball&genre_category_id=KZFzniwnSyZfZ7v7nE", "all", "", "basketball"},
		{"arts category", "category_id=KZFzniwnSyZfZ7v7na", "KZFzniwnSyZfZ7v7na", "", "rock"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := request("/?"+base+"&"+tc.query, false)
			body := rec.Body.String()
			if rec.Code != 200 || !strings.Contains(body, `value="`+tc.selected+`" selected`) {
				t.Fatalf("category selection failed: %d %s", rec.Code, body)
			}
			selects := regexp.MustCompile(`<select id="genre"[^>]*>(.*?)</select>`).FindStringSubmatch(body)
			if len(selects) != 2 || strings.Contains(selects[1], tc.absent) || (tc.genre != "" && !strings.Contains(selects[1], `value="`+tc.genre+`" selected`)) {
				t.Fatalf("incompatible genre choices: %v", selects)
			}
			if (tc.selected == "all") != strings.Contains(selects[0], "disabled") {
				t.Fatal("all-category genre control has the wrong availability")
			}
			for _, text := range []string{`value="Chicago"`, `value="US"`, `value="2026-10-03"`, "Event date to be announced", "Price not listed", "Venue to be announced"} {
				if !strings.Contains(body, text) {
					t.Errorf("lost search context or sparse metadata: %q", text)
				}
			}
		})
	}

	query := base + "&category_id=KZFzniwnSyZfZ7v7nE&genre_id=basketball"
	first := request("/?"+query, false)
	before := eventCalls.Load()
	apiResult := request("/api/v1/events?"+query, false)
	var list models.EventList
	if apiResult.Code != 200 || json.Unmarshal(apiResult.Body.Bytes(), &list) != nil || list.NextCursor == nil || eventCalls.Load() != before {
		t.Fatal("category-filtered HTML/API reads did not share their cache")
	}
	link := regexp.MustCompile(`href="([^"]*cursor[^"]*)"`).FindStringSubmatch(first.Body.String())
	if len(link) != 2 {
		t.Fatal("missing category pagination link")
	}
	next := html.UnescapeString(link[1])
	for _, partial := range []bool{false, true} {
		rec := request(next, partial)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `ticketmaster:event-KZFzniwnSyZfZ7v7nE-1`) || strings.Contains(rec.Body.String(), "Load more events") {
			t.Fatalf("category pagination failed (partial=%v): %d %s", partial, rec.Code, rec.Body.String())
		}
	}
	if eventCalls.Load() != before+1 {
		t.Fatal("full and partial category pagination did not reuse results")
	}
	for _, path := range []string{
		"/events/ticketmaster:event-KZFzniwnSyZfZ7v7nE-0?return_to=" + url.QueryEscape("/?"+query),
		"/?" + query + "&selected_event=ticketmaster:event-KZFzniwnSyZfZ7v7nE-0",
	} {
		rec := request(path, false)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), `<p class="genre-label">Sports</p>`) || strings.Contains(rec.Body.String(), "<h3>Featuring</h3>") || !strings.Contains(rec.Body.String(), "category_id=KZFzniwnSyZfZ7v7nE") {
			t.Fatalf("non-music event/context failed: %d %s", rec.Code, rec.Body.String())
		}
	}
	empty := request("/?"+query+"&keyword=empty", false)
	if empty.Code != 200 || !strings.Contains(empty.Body.String(), "No events found for these filters") || !strings.Contains(empty.Body.String(), `value="basketball" selected`) {
		t.Fatal("empty category search lost its filters or recovery state")
	}
	for _, path := range []string{
		"/api/v1/events?" + query + "&cursor=" + url.QueryEscape(*list.NextCursor) + "&genre_category_id=anything",
		"/?" + base + "&category_id=KZFzniwnSyZfZ7v7na&genre_id=basketball&cursor=" + url.QueryEscape(*list.NextCursor),
		"/?" + base + "&genre_category_id=a&genre_category_id=b",
	} {
		if rec := request(path, false); rec.Code != 400 {
			t.Fatalf("invalid UI state or changed category cursor accepted: %s", path)
		}
	}
}

func TestCategoryCatalogFailureKeepsWebResultsAndFilters(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/classifications.json" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = fmt.Fprint(w, `{"page":{"number":0,"totalElements":1,"totalPages":1},"_embedded":{"events":[{"id":"sparse","name":"Sparse event"}]}}`)
	}))
	defer upstream.Close()
	cache := kv.NewMemory()
	defer cache.Close()
	a := &api{events: discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL})}
	for range 2 {
		rec := httptest.NewRecorder()
		a.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?city=Chicago&category_id=KZFzniwnSyZfZ7v7nE&genre_id=basketball", nil))
		for _, text := range []string{"Sparse event", "Category not listed", "Category and genre options are temporarily unavailable", `value="KZFzniwnSyZfZ7v7nE" selected`, `value="basketball" selected`} {
			if rec.Code != 200 || !strings.Contains(rec.Body.String(), text) {
				t.Fatalf("catalog failure replaced results or lost filters: %d, missing %q", rec.Code, text)
			}
		}
	}
}

func TestCategoryAPIAndMusicCompatibility(t *testing.T) {
	fixture := categoryFixture(t)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/classifications.json":
			_, _ = w.Write(fixture)
		case "/classifications/segments/KZFzniwnSyZfZ7v7nJ.json":
			_, _ = fmt.Fprint(w, `{"id":"KZFzniwnSyZfZ7v7nJ","_embedded":{"genres":[{"id":"rock","name":"Rock"}]}}`)
		case "/events.json":
			if r.URL.Query().Get("segmentId") != "KZFzniwnSyZfZ7v7nJ" || r.URL.Query().Get("genreId") != "rock" {
				t.Error("legacy genre-only API request lost its Music scope")
			}
			_, _ = fmt.Fprint(w, `{"page":{"number":0,"totalElements":0,"totalPages":0}}`)
		default:
			t.Errorf("unexpected provider path %s", r.URL.Path)
		}
	}))
	defer upstream.Close()
	cache := kv.NewMemory()
	defer cache.Close()
	a := &api{events: discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL})}
	routes := a.Routes()
	request := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	for range 2 {
		rec := request("/api/v1/categories")
		var catalog models.CategoryList
		if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &catalog) != nil || len(catalog.Items) != 5 || catalog.Items[0].Name != "Arts & Theatre" || catalog.Meta.DataAsOf.IsZero() {
			t.Fatalf("category API failed: %d %s", rec.Code, rec.Body.String())
		}
	}
	if calls.Load() != 1 {
		t.Fatal("category API did not reuse cached metadata")
	}
	for _, path := range []string{"/api/v1/genres", "/api/v1/events?genre_id=rock", "/api/v1/events?category_id=KZFzniwnSyZfZ7v7nJ&genre_id=rock"} {
		if rec := request(path); rec.Code != 200 {
			t.Fatalf("legacy music API failed: %s %s", path, rec.Body.String())
		}
	}
	before := calls.Load()
	for _, path := range []string{"/api/v1/categories?category_id=all", "/api/v1/events?category_id=../bad", "/api/v1/events?category_id=all&genre_id=rock", "/api/v1/events?category_id=a&category_id=b"} {
		rec := request(path)
		if rec.Code != 400 || rec.Header().Get("Content-Type") != "application/problem+json" || !strings.Contains(rec.Body.String(), "invalid_query") || calls.Load() != before {
			t.Fatalf("invalid category request reached provider: %s %s", path, rec.Body.String())
		}
	}
}
