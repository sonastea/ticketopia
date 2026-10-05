package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/events"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

func TestHTMLAndAPIShareEventsAndPagination(t *testing.T) {
	fixture := categoryFixture(t)
	var eventCalls, genreCalls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "classifications") {
			genreCalls.Add(1)
			_, _ = w.Write(fixture)
			return
		}
		eventCalls.Add(1)
		page := r.URL.Query().Get("page")
		_, _ = fmt.Fprintf(w, `{"page":{"number":%s,"size":1,"totalElements":2,"totalPages":2},"_embedded":{"events":[{"id":"show-%s","name":"A show","url":"https://www.ticketmaster.com/event/show-%s","dates":{"start":{"localDate":"2026-10-12","localTime":"19:30:00"}},"sales":{"public":{"startDateTime":"2026-09-01T14:00:00Z"}}}]}}`, page, page, page)
	}))
	defer upstream.Close()
	cache := kv.NewMemory()
	defer cache.Close()
	a := &api{events: discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "secret", BaseURL: upstream.URL})}
	// If a discovery route touches durable storage, this disabled service fails.
	// Reads must keep their existing cache/provider behavior in persistence mode.
	WithPersistence(func(context.Context) error { return nil }, events.New(failOnDurableRead{t}))(a)
	routes := a.Routes()
	query := "city=Boston&start_date=2026-10-02&end_date=2026-10-31&limit=1&category_id=KZFzniwnSyZfZ7v7nJ"
	request := func(path string, partial bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if partial {
			req.Header.Set("HX-Request", "true")
		}
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, req)
		return rec
	}
	html := request("/?"+query, false)
	if html.Code != 200 || !strings.HasPrefix(html.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("HTML response failed: %d %s", html.Code, html.Body.String())
	}
	for _, text := range []string{"Mon, Oct 12, 2026", "Public sale: Sep 1, 2026", "Venue to be announced", "Price not listed", `https://www.ticketmaster.com/event/show-0`, `data-event-id="ticketmaster:show-0"`, "Load more events", `<option value="rock">Rock</option>`} {
		if !strings.Contains(html.Body.String(), text) {
			t.Errorf("HTML omitted %q", text)
		}
	}
	response := request("/api/v1/events?"+query, false)
	var list models.EventList
	if response.Code != 200 || json.Unmarshal(response.Body.Bytes(), &list) != nil || list.NextCursor == nil {
		t.Fatalf("API response failed: %d %s", response.Code, response.Body.String())
	}
	if eventCalls.Load() != 1 || genreCalls.Load() != 1 {
		t.Fatalf("web/API cache not shared: events=%d genres=%d", eventCalls.Load(), genreCalls.Load())
	}
	for _, id := range []string{"ticketmaster:show-0", "ticketmaster%3Ashow-0"} {
		detail := request("/api/v1/events/"+id, false)
		if detail.Code != 200 || !strings.Contains(detail.Body.String(), `"id":"ticketmaster:show-0"`) || eventCalls.Load() != 1 {
			t.Fatalf("encoded detail ID or shared detail cache failed: %d %s", detail.Code, detail.Body.String())
		}
		web := request("/events/"+id+"?section=discussion&return_to="+url.QueryEscape("/?"+query), false)
		for _, text := range []string{"A show", "A place to talk about this event", "Posting and replies are not available yet.", "Back to results", `aria-current="page"`} {
			if web.Code != 200 || !strings.Contains(web.Body.String(), text) || eventCalls.Load() != 1 {
				t.Fatalf("web event route/cache failed for %s: %d, missing %q", id, web.Code, text)
			}
		}
	}
	selected := request("/?"+query+"&selected_event=ticketmaster:show-0&section=community", false)
	if selected.Code != 200 || !strings.Contains(selected.Body.String(), `data-context-id="ticketmaster:show-0"`) || !strings.Contains(selected.Body.String(), "Good events are worth sharing") || eventCalls.Load() != 1 {
		t.Fatalf("selected event did not reconstruct from URL/cache: %d %s", selected.Code, selected.Body.String())
	}
	panelReq := httptest.NewRequest(http.MethodGet, "/events/ticketmaster:show-0", nil)
	panelReq.Header.Set("X-Ticketopia-Panel", "true")
	panel := httptest.NewRecorder()
	routes.ServeHTTP(panel, panelReq)
	if panel.Code != 200 || strings.Contains(panel.Body.String(), "<html") || !strings.Contains(panel.Body.String(), `data-context-id="ticketmaster:show-0"`) || eventCalls.Load() != 1 {
		t.Fatal("event panel did not reuse cached detail or returned a full document")
	}
	partial := request("/?"+query+"&cursor="+url.QueryEscape(*list.NextCursor), true)
	if partial.Code != 200 || strings.Contains(partial.Body.String(), "Load more events") || strings.Contains(partial.Body.String(), "<html") || !strings.Contains(partial.Body.String(), `hx-swap-oob="beforeend:#event-list"`) || !strings.Contains(partial.Body.String(), "ticketmaster:show-1") {
		t.Fatalf("last-page fragment failed: %d %s", partial.Code, partial.Body.String())
	}
	if eventCalls.Load() != 2 || genreCalls.Load() != 1 {
		t.Fatal("pagination fetched unrelated metadata")
	}
	invalid := request("/api/v1/events?limit=-1", false)
	if invalid.Code != 400 || invalid.Header().Get("Content-Type") != "application/problem+json" || !strings.Contains(invalid.Body.String(), `"code":"invalid_query"`) || eventCalls.Load() != 2 {
		t.Fatalf("invalid request handling failed: %d %s", invalid.Code, invalid.Body.String())
	}
	spec := request("/api/v1/openapi.yaml", false)
	if spec.Code != 200 || !strings.Contains(spec.Body.String(), "openapi: 3.1.0") {
		t.Fatal("OpenAPI contract unavailable")
	}
}

type failOnDurableRead struct{ t *testing.T }

func (f failOnDurableRead) Upsert(context.Context, models.Event) (models.Event, error) {
	f.t.Error("discovery read attempted an ingestion write")
	return models.Event{}, events.ErrDisabled
}

func (f failOnDurableRead) Get(context.Context, string) (models.Event, error) {
	f.t.Error("discovery read changed to durable metadata")
	return models.Event{}, events.ErrDisabled
}

func TestUnavailableResponsesAreVisibleAndRedacted(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "90")
		w.WriteHeader(429)
		_, _ = fmt.Fprint(w, `{"fault":"private-api-key"}`)
	}))
	defer upstream.Close()
	cache := kv.NewMemory()
	defer cache.Close()
	a := &api{events: discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "private-api-key", BaseURL: upstream.URL})}
	for _, path := range []string{"/api/v1/events", "/?city=Boston", "/api/v1/genres", "/api/v1/categories"} {
		rec := httptest.NewRecorder()
		a.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != 503 || rec.Header().Get("Retry-After") == "" || strings.Contains(rec.Body.String(), "private-api-key") {
			t.Fatalf("bad outage response for %s: %d %s", path, rec.Code, rec.Body.String())
		}
		if strings.HasPrefix(path, "/?") && !strings.Contains(rec.Body.String(), `role="alert"`) {
			t.Fatal("HTML error not visible")
		}
	}
}
