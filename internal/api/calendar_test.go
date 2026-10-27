package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

type calendarCatalog struct {
	publicCatalog
	detail models.EventDetail
}

func (s *calendarCatalog) Snapshot(_ context.Context, id string) (models.EventDetail, error) {
	if id == s.detail.Item.ID {
		return s.detail, nil
	}
	return models.EventDetail{}, discovery.ErrNotFound
}

func TestEventCalendarIsAPublicEventRepresentation(t *testing.T) {
	cache := kv.NewMemory()
	t.Cleanup(func() { cache.Close() })
	upstream := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(upstream.Close)
	at := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	store := &calendarCatalog{detail: models.EventDetail{
		Item: models.Event{ID: "ticketmaster:dated", Name: "Night Market: Live!", Start: models.EventStart{DateTime: &at}},
		Meta: models.Freshness{DataAsOf: time.Now().UTC()},
	}}
	s := discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{Catalog: store, APIKey: "test", BaseURL: upstream.URL})
	routes := (&api{events: s, authConfig: accounts.Config{BaseURL: "https://ticketopia.example"}}).Routes()
	path := "/api/v1/events/ticketmaster:dated/calendar.ics"

	first := publicRequest(routes, path, "")
	body := first.Body.String()
	if first.Code != 200 || first.Header().Get("Content-Type") != calendarMIME || first.Header().Get("Cache-Control") != "public, no-cache" ||
		first.Header().Get("Content-Disposition") != `attachment; filename="night-market-live.ics"` ||
		!strings.Contains(body, "DTSTART:"+at.Format("20060102T150405Z")+"\r\n") ||
		!strings.Contains(body, "URL:https://ticketopia.example/events/ticketmaster:dated\r\n") {
		t.Fatalf("calendar: %d %v\n%s", first.Code, first.Header(), body)
	}
	if again := publicRequest(routes, path, first.Header().Get("ETag")); again.Code != 304 || again.Body.Len() != 0 {
		t.Fatalf("revalidation: %d", again.Code)
	}

	for _, tc := range []struct {
		path, code string
		status     int
	}{
		{"/api/v1/events/ticketmaster:missing/calendar.ics", "event_not_found", 404},
		{path + "?pretty", "invalid_query", 400},
	} {
		w := publicRequest(routes, tc.path, "*")
		if w.Code != tc.status || w.Header().Get("Cache-Control") != "no-store" || !strings.Contains(w.Body.String(), tc.code) {
			t.Errorf("%s: %d %v %s", tc.path, w.Code, w.Header(), w.Body)
		}
	}
	store.detail.Item.Start = models.EventStart{DateTBA: true}
	if w := publicRequest(routes, path, ""); w.Code != 404 || !strings.Contains(w.Body.String(), "date_not_announced") {
		t.Fatalf("undated event: %d %s", w.Code, w.Body)
	}
	store.detail.Item.Start = models.EventStart{DateTime: &at}
	store.detail.Meta.Stale = true
	if w := publicRequest(routes, path, "*"); w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("ETag") != "" {
		t.Fatalf("stale calendar cached: %v", w.Header())
	}
}
