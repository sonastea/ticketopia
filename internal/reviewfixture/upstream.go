package reviewfixture

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

// upstream is a loopback stand-in for the Ticketmaster Discovery API that
// serves the fixture's own events, so stored Discover collects them into
// MariaDB exactly as in production. Omitted events disappear from searches
// and their detail reads return 404, as when an event is removed upstream.
type upstream struct {
	server  *httptest.Server
	mu      sync.Mutex
	events  []models.Event
	omitted map[string]bool
}

func newUpstream() *upstream {
	u := &upstream{omitted: map[string]bool{}}
	u.server = httptest.NewServer(http.HandlerFunc(u.serve))
	return u
}

func (u *upstream) set(events []models.Event) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.events = events
}

func (u *upstream) omit(id string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.omitted[id] = true
}

func (u *upstream) serve(w http.ResponseWriter, r *http.Request) {
	u.mu.Lock()
	defer u.mu.Unlock()
	var listed []map[string]any
	for _, e := range u.events {
		if !u.omitted[e.ID] {
			listed = append(listed, providerJSON(e))
		}
	}
	if raw, ok := strings.CutPrefix(r.URL.Path, "/events/"); ok {
		id := "ticketmaster:" + strings.TrimSuffix(raw, ".json")
		for _, e := range listed {
			if "ticketmaster:"+e["id"].(string) == id {
				_ = json.NewEncoder(w).Encode(e)
				return
			}
		}
		http.NotFound(w, r)
		return
	}
	if r.URL.Path != "/events.json" {
		http.NotFound(w, r)
		return
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size <= 0 {
		size = 100
	}
	items := []map[string]any{}
	for i := page * size; i < min((page+1)*size, len(listed)); i++ {
		items = append(items, listed[i])
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"_embedded": map[string]any{"events": items},
		"page": map[string]int{"number": page, "size": size, "totalElements": len(listed), "totalPages": max(1, (len(listed)+size-1)/size)}})
}

// providerJSON renders an event in the provider's embedded-resource layout.
func providerJSON(e models.Event) map[string]any {
	when := func(t *time.Time) string {
		if t == nil {
			return ""
		}
		return t.UTC().Format(time.RFC3339)
	}
	text := func(s *string) string {
		if s == nil {
			return ""
		}
		return *s
	}
	prices := []map[string]any{}
	for _, p := range e.PriceRanges {
		prices = append(prices, map[string]any{"type": p.Type, "currency": p.Currency, "min": json.Number(text(p.Min)), "max": json.Number(text(p.Max))})
	}
	presales := []map[string]any{}
	for _, p := range e.Presales {
		presales = append(presales, map[string]any{"name": p.Name, "description": p.Description, "url": p.URL})
	}
	venues := []map[string]any{}
	for _, v := range e.Venues {
		venues = append(venues, map[string]any{"id": strings.TrimPrefix(v.ID, "ticketmaster:"), "name": v.Name,
			"address": map[string]string{"line1": v.Address}, "city": map[string]string{"name": v.City},
			"state": map[string]string{"name": v.State}, "country": map[string]string{"name": v.Country, "countryCode": v.CountryCode}})
	}
	return map[string]any{"id": e.Source.ID, "name": e.Name, "url": e.Source.URL, "info": e.Info, "pleaseNote": e.PleaseNote,
		"images": e.Images, "classifications": e.Classifications, "priceRanges": prices,
		"dates": map[string]any{"start": map[string]string{"dateTime": when(e.Start.DateTime), "localDate": text(e.Start.LocalDate), "localTime": text(e.Start.LocalTime)},
			"timezone": text(e.Start.Timezone), "status": map[string]string{"code": e.Status}},
		"sales":     map[string]any{"public": map[string]string{"startDateTime": when(e.PublicSale.Start)}, "presales": presales},
		"_embedded": map[string]any{"venues": venues}}
}
