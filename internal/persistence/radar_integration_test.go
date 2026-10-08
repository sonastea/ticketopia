package persistence

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/radar"
)

func TestMariaDBRadarIdentityLocationPreferencesPaginationAndRestart(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "radar-owner")
	other, _ := loginAccount(t, auth, "radar-other")
	service := radar.New(p.Radar(), nil, 6*time.Hour)
	list, err := service.List(t.Context(), owner.ID, nil)
	if err != nil || list.Mode != "needs_location" {
		t.Fatal(list, err)
	}
	_, err = auth.UpdatePreferences(t.Context(), owner.ID, accounts.PreferencesPatch{City: pointer("Berlin"), Country: pointer("DE"), Timezone: pointer("Europe/Berlin"), CategoryIDs: pointer([]string{"music"})})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = p.Follows().Follow(t.Context(), owner.ID, followDetail("artist", "Artist"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = p.Follows().Follow(t.Context(), owner.ID, followDetail("venue", "Venue"))
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = p.Follows().Follow(t.Context(), other.ID, followDetail("artist", "other"))
	if err != nil {
		t.Fatal(err)
	}
	date := time.Now().AddDate(0, 0, 2).Format(time.DateOnly)
	for i := range 8 {
		e := sampleEvent(fmt.Sprintf("radar-%02d", i))
		e.Start.LocalDate = &date
		e.Start.DateTime = nil
		e.Venues[0].City = "Berlin"
		e.Venues[0].CountryCode = "DE"
		e.Classifications = nil
		e.Artists = nil
		switch i {
		case 0, 1:
			e.Artists = []models.Artist{{ID: "ticketmaster:Artist", Name: "Artist"}}
			e.Venues = append(e.Venues, models.Venue{ID: "ticketmaster:Venue", Place: models.Place{Name: "Venue", City: "Berlin", CountryCode: "DE"}})
		case 2:
			e.Venues[0].ID = "ticketmaster:Venue"
		case 3:
			e.Classifications = []models.Classification{{Segment: &models.NamedID{ID: "music"}}}
		case 4:
			e.Artists = []models.Artist{{ID: "ticketmaster:artist", Name: "Artist"}}
		case 5:
			e.Artists = []models.Artist{{ID: "ticketmaster:Artist", Name: "Artist"}}
			e.Venues[0].CountryCode = "US"
		case 6:
			e.Artists = []models.Artist{{ID: "ticketmaster:other", Name: "Other"}}
		case 7:
			e.Artists = []models.Artist{{ID: "ticketmaster:Artist", Name: "Artist"}}
			e.Status = "cancelled"
		}
		if err := p.Discovery().Observe(t.Context(), models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: time.Now().Add(-24 * time.Hour)}}); err != nil {
			t.Fatal(err)
		}
	}
	list, err = service.List(t.Context(), owner.ID, url.Values{"limit": {"2"}})
	if err != nil || len(list.Items) != 2 || list.NextCursor == nil || !list.Meta.Stale || list.Items[0].Event.ID != "ticketmaster:radar-00" || len(list.Items[0].Reasons) != 2 {
		t.Fatal(list, err)
	}
	// Restarted/independent pools have the same versioned ordering and reasons.
	restarted := radar.New(f.open(t).Radar(), nil, 6*time.Hour)
	next, err := restarted.List(t.Context(), owner.ID, url.Values{"limit": {"2"}, "cursor": {*list.NextCursor}})
	if err != nil || len(next.Items) != 2 || next.Items[0].Event.ID != "ticketmaster:radar-02" || next.Items[1].Event.ID != "ticketmaster:radar-03" || next.NextCursor != nil {
		t.Fatal(next, err)
	}
	foreign, err := service.List(t.Context(), other.ID, nil)
	if err != nil || len(foreign.Items) != 0 {
		t.Fatal("foreign account", foreign, err)
	}
	if err := p.Follows().Remove(t.Context(), owner.ID, "venue", "ticketmaster:Venue"); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.List(t.Context(), owner.ID, url.Values{"limit": {"2"}, "cursor": {*list.NextCursor}}); !errors.Is(err, radar.ErrChanged) {
		t.Fatal("cursor didn't detect follow change", err)
	}
	_, err = auth.UpdatePreferences(t.Context(), owner.ID, accounts.PreferencesPatch{CategoryIDs: pointer([]string{"unavailable"})})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Follows().Remove(t.Context(), owner.ID, "artist", "ticketmaster:Artist"); err != nil {
		t.Fatal(err)
	}
	list, err = service.List(t.Context(), owner.ID, nil)
	if err != nil || list.Mode != "personalized" || len(list.Items) != 0 || list.Meta.Coverage.Status != "not_collected" {
		t.Fatal("empty unknown taxonomy", list, err)
	}
	_, err = auth.UpdatePreferences(t.Context(), owner.ID, accounts.PreferencesPatch{CategoryIDs: pointer([]string{})})
	if err != nil {
		t.Fatal(err)
	}
	list, err = service.List(t.Context(), owner.ID, nil)
	if err != nil || list.Mode != "nearby" || len(list.Items) != 6 || list.Items[0].Reasons[0].Code != "nearby" {
		t.Fatal("cold start suggestions", list, err)
	}
}

func TestMariaDBRadarBroadRefreshReuseAndOutage(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "radar-refresh")
	other, _ := loginAccount(t, auth, "radar-refresh-other")
	for _, id := range []string{owner.ID, other.ID} {
		if _, err := auth.UpdatePreferences(t.Context(), id, accounts.PreferencesPatch{City: pointer("Berlin"), Country: pointer("DE")}); err != nil {
			t.Fatal(err)
		}
	}
	var calls atomic.Int32
	var failed atomic.Bool
	date := time.Now().AddDate(0, 0, 2).Format(time.DateOnly)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/events.json" || failed.Load() {
			w.WriteHeader(503)
			return
		}
		for _, key := range []string{"classificationId", "classificationName", "genreId", "attractionId", "venueId", "keyword"} {
			if r.URL.Query().Get(key) != "" {
				t.Errorf("radar collection retained private/narrow filter %s", key)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"_embedded": map[string]any{"events": []any{map[string]any{
				"id": "fresh-radar", "name": "Fresh radar event",
				"dates":     map[string]any{"start": map[string]string{"localDate": date}, "status": map[string]string{"code": "onsale"}},
				"_embedded": map[string]any{"venues": []any{map[string]any{"id": "arena", "name": "Arena", "city": map[string]string{"name": "Berlin"}, "country": map[string]string{"countryCode": "DE"}}}},
			}}}, "page": map[string]int{"number": 0, "totalElements": 1, "totalPages": 1},
		})
	}))
	defer provider.Close()
	discover := catalogService(t, p, provider.URL, 100)
	service := radar.New(p.Radar(), discover, 6*time.Hour)
	list, err := service.List(t.Context(), owner.ID, nil)
	if err != nil || len(list.Items) != 1 || calls.Load() != 1 || list.Meta.Coverage.Status != "complete" || list.Items[0].Meta.Stale {
		t.Fatal("initial collection", list, err, calls.Load())
	}
	list, err = radar.New(f.open(t).Radar(), discover, 6*time.Hour).List(t.Context(), other.ID, nil)
	if err != nil || len(list.Items) != 1 || calls.Load() != 1 {
		t.Fatal("coverage not reused across owners/pools", list, err, calls.Load())
	}
	// Force due receipts with the fixture's admin connection, not runtime grants.
	execSQL(t, f.admin, `UPDATE `+f.runtime.Database+`.discovery_scopes SET last_success=TIMESTAMPADD(DAY,-1,UTC_TIMESTAMP(6)),next_refresh=TIMESTAMPADD(DAY,-1,UTC_TIMESTAMP(6))`)
	failed.Store(true)
	list, err = service.List(t.Context(), owner.ID, nil)
	if err != nil || len(list.Items) != 1 || list.Meta.Coverage.Status != "stale" || !list.Meta.Stale || calls.Load() != 2 {
		t.Fatal("outage erased records or fabricated fresh coverage", list, err, calls.Load())
	}
	_, err = service.List(t.Context(), other.ID, nil)
	if err != nil || calls.Load() != 2 {
		t.Fatal("durable outage cooldown not reused", err, calls.Load())
	}
}
