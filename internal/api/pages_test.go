package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/kv"
)

func TestSelectedEventFailureDoesNotReplaceResults(t *testing.T) {
	fixture := categoryFixture(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/events/missing") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.Contains(r.URL.Path, "classifications") {
			_, _ = w.Write(fixture)
			return
		}
		_, _ = fmt.Fprint(w, `{"page":{"number":0,"size":20,"totalElements":1,"totalPages":1},"_embedded":{"events":[{"id":"available","name":"Available show","dates":{"status":{"code":"canceled"}},"priceRanges":[{"currency":"USD","min":20,"max":40}]}]}}`)
	}))
	defer upstream.Close()
	cache := kv.NewMemory()
	defer cache.Close()
	a := &api{events: discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL})}
	for _, tc := range []struct {
		path   string
		status int
		text   string
	}{
		{"/?city=Boston&selected_event=ticketmaster:missing", 200, "Available show"},
		{"/events/ticketmaster:missing", 404, "Event unavailable"},
		{"/events/ticketmaster:available", 200, "Canceled"},
		{"/events/ticketmaster:available?section=unsupported", 400, "choose overview, discussion, or community"},
		{"/api/v1/events?city=Boston&selected_event=ticketmaster:available", 400, "invalid_query"},
		{"/?city=Boston&selected_event=a&selected_event=b", 400, "use one value"},
	} {
		rec := httptest.NewRecorder()
		a.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.text) {
			t.Fatalf("%s: got %d, missing %q", tc.path, rec.Code, tc.text)
		}
		if tc.path == "/events/ticketmaster:available" {
			body := rec.Body.String()
			actions := strings.Index(body, `class="participation-actions"`)
			if price := strings.Index(body, "USD 20"); price < 0 || price >= actions || strings.Index(body, "Canceled") >= actions {
				t.Fatal("decision-critical price/cancellation must precede participation controls")
			}
		}
	}
}

func TestReturnContextCannotNavigateOutsideDiscovery(t *testing.T) {
	for _, raw := range []string{"https://evil.example/", "//evil.example/", "/events/ticketmaster:other", "/?selected_event=other", "/?city=A&city=B", "/?city=%zz", "/#external", "/?cursor=invalid"} {
		if got := safeReturnURL(raw); got != "/" {
			t.Errorf("accepted unsafe/invalid return context %q: %q", raw, got)
		}
	}
	valid := "/?city=Boston&country=US&keyword=jazz"
	if got := safeReturnURL(valid); got != valid {
		t.Errorf("lost valid discovery filters: %q", got)
	}
}

func TestPlannedDestinationsRemainTruthful(t *testing.T) {
	a := &api{}
	for _, path := range []string{"/saved", "/community", "/me", "/me/interests"} {
		rec := httptest.NewRecorder()
		a.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), "In development") || !strings.Contains(rec.Body.String(), "Discover events") {
			t.Errorf("planned destination %s is missing its availability/recovery state", path)
		}
	}
}
