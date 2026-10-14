package persistence

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/radar"
)

func TestMariaDBRadarEligibilityOrderingAndRevisions(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "radar-revisions")
	if _, err := auth.UpdatePreferences(t.Context(), owner.ID, accounts.PreferencesPatch{City: pointer("Berlin"), Country: pointer("DE")}); err != nil {
		t.Fatal(err)
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	date := at.AddDate(0, 0, 2).Format(time.DateOnly)
	base := sampleEvent("revision-a")
	base.Start.LocalDate, base.Start.DateTime, base.Start.LocalTime = &date, nil, pointer("20:00:00")
	base.Venues[0].City, base.Venues[0].CountryCode = "Berlin", "DE"
	observe := func(e models.Event, when time.Time) {
		t.Helper()
		if err := p.Discovery().Observe(t.Context(), models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: when}}); err != nil {
			t.Fatal(err)
		}
	}
	var candidates []models.Event
	for i, mutate := range []func(*models.Event){
		func(e *models.Event) {},
		func(e *models.Event) { e.Start.LocalTime = nil },
		func(e *models.Event) { e.Start.LocalTime = pointer("01:00:00"); e.Start.TimeTBA = true },
		func(e *models.Event) { e.Start.NoSpecificTime = true },
		func(e *models.Event) { e.Status = "cancelled" },
		func(e *models.Event) { e.Status = "canceled" },
		func(e *models.Event) { e.Status = "postponed" },
		func(e *models.Event) { e.Start.DateTBA = true },
		func(e *models.Event) { e.Start.DateTBD = true },
		func(e *models.Event) { e.Start.LocalDate = nil },
		func(e *models.Event) { e.Start.DateTime = pointer(at.Add(-time.Hour)) },
		func(e *models.Event) { e.Start.LocalDate = pointer(at.AddDate(0, 0, 90).Format(time.DateOnly)) },
	} {
		e := base
		e.Source.ID = fmt.Sprintf("revision-%02d", i)
		e.ID = "ticketmaster:" + e.Source.ID
		mutate(&e)
		observe(e, at.Add(-7*time.Hour))
		candidates = append(candidates, e)
	}
	s := radar.New(p.Radar(), nil, 6*time.Hour)
	first := func() radar.List {
		t.Helper()
		page, err := s.List(t.Context(), owner.ID, url.Values{"limit": {"1"}})
		if err != nil || len(page.Items) != 1 || page.NextCursor == nil {
			t.Fatal(page, err)
		}
		return page
	}
	page := first()
	var ids []string
	for {
		ids = append(ids, page.Items[0].Event.ID)
		if page.NextCursor == nil {
			break
		}
		var err error
		page, err = s.List(t.Context(), owner.ID, url.Values{"limit": {"1"}, "cursor": {*page.NextCursor}})
		if err != nil || len(page.Items) != 1 {
			t.Fatal(page, err)
		}
	}
	if !slices.Equal(ids, []string{candidates[0].ID, candidates[1].ID, candidates[2].ID, candidates[3].ID}) {
		t.Fatal(ids)
	}
	check := func(cursor string, changed bool) {
		t.Helper()
		_, err := s.List(t.Context(), owner.ID, url.Values{"limit": {"1"}, "cursor": {cursor}})
		if changed && !errors.Is(err, radar.ErrChanged) || !changed && err != nil {
			t.Fatalf("changed=%v: %v", changed, err)
		}
	}
	page = first()
	// A newer observation of a later page changes the revision even if metadata is identical.
	observe(candidates[3], at)
	check(*page.NextCursor, true)
	page = first()
	older := candidates[3]
	older.Name = "Backdated must not win"
	observe(older, at.Add(-time.Hour))
	observe(older, at)
	check(*page.NextCursor, false)
	// An excluded event does not invalidate the eligible set until it becomes eligible.
	excluded := candidates[4]
	observe(excluded, at)
	check(*page.NextCursor, false)
	excluded.Status = "onsale"
	observe(excluded, at.Add(time.Second))
	check(*page.NextCursor, true)
	page = first()
	execSQL(t, f.admin, `INSERT INTO `+f.runtime.Database+`.event_detail_tasks(event_id) VALUES (?)`, candidates[3].ID)
	check(*page.NextCursor, true)
	page = first()
	execSQL(t, f.admin, `DELETE FROM `+f.runtime.Database+`.event_detail_tasks WHERE event_id=?`, candidates[3].ID)
	check(*page.NextCursor, true)
	page = first()
	// Coverage evolves independently of the result revision, including while
	// browsing an existing cursor.
	scope := radar.DateScope(accounts.Preferences{City: "Berlin", Country: "DE"}, at)
	execSQL(t, f.admin, `INSERT INTO `+f.runtime.Database+`.discovery_scopes(scope_id,city,country,start_date,end_date,artist_id,venue_id,last_success,data_as_of,status) VALUES (?,'Berlin','DE',?,?,'','',?,?,'complete')`, strings.Repeat("a", 64), scope.StartDate, scope.EndDate, at, at)
	check(*page.NextCursor, false)
	page = first()
	data, _ := base64.RawURLEncoding.DecodeString(*page.NextCursor)
	var cursor radar.Cursor
	if err := json.Unmarshal(data, &cursor); err != nil {
		t.Fatal(err)
	}
	cursor.After = "ticketmaster:absent"
	data, _ = json.Marshal(cursor)
	_, err := s.List(t.Context(), owner.ID, url.Values{"limit": {"1"}, "cursor": {base64.RawURLEncoding.EncodeToString(data)}})
	var validation *accounts.ValidationError
	if !errors.As(err, &validation) {
		t.Fatal("forged anchor", err)
	}
	// Corrupt the lookahead DTO without changing its projection. The first page
	// must still load: it must not decode snapshots outside the requested page.
	execSQL(t, f.admin, `UPDATE `+f.runtime.Database+`.event_snapshots SET snapshot=JSON_SET(snapshot,'$.start','invalid') WHERE event_id=?`, candidates[4].ID)
	first()
	if _, err := s.List(t.Context(), owner.ID, nil); !errors.Is(err, radar.ErrUnavailable) {
		t.Fatal("selected corrupt snapshot", err)
	}
}

func TestMariaDBRadarSignalScoresAndOffPageFreshness(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "radar-scores")
	if _, err := auth.UpdatePreferences(t.Context(), owner.ID, accounts.PreferencesPatch{City: pointer("Berlin"), Country: pointer("DE"), CategoryIDs: pointer([]string{"music"})}); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"artist", "venue"} {
		if _, _, err := p.Follows().Follow(t.Context(), owner.ID, followDetail(kind, "Matched")); err != nil {
			t.Fatal(err)
		}
	}
	at := time.Now().UTC().Truncate(time.Microsecond)
	date := at.AddDate(0, 0, 2).Format(time.DateOnly)
	for score := range 8 {
		e := sampleEvent(fmt.Sprintf("score-%d", score))
		e.Start.LocalDate, e.Start.DateTime = &date, nil
		e.Artists = nil
		e.Classifications = nil
		e.Venues[0].City, e.Venues[0].CountryCode = "Berlin", "DE"
		if score&4 != 0 {
			e.Artists = []models.Artist{{ID: "ticketmaster:Matched"}, {ID: "ticketmaster:Matched"}}
		}
		if score&2 != 0 {
			e.Venues[0].ID = "ticketmaster:Matched"
			e.Venues = append(e.Venues, e.Venues[0])
		}
		if score&1 != 0 {
			e.Classifications = []models.Classification{{Segment: &models.NamedID{ID: "music"}}, {Segment: &models.NamedID{ID: "music"}}}
		}
		asOf := at
		if score == 1 {
			asOf = at.Add(-7 * time.Hour)
		}
		if err := p.Discovery().Observe(t.Context(), models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: asOf}}); err != nil {
			t.Fatal(err)
		}
	}
	s := radar.New(p.Radar(), nil, 6*time.Hour)
	q := url.Values{"limit": {"1"}}
	for score := 7; score > 0; score-- {
		page, err := s.List(t.Context(), owner.ID, q)
		if err != nil || len(page.Items) != 1 || page.Items[0].Event.ID != fmt.Sprintf("ticketmaster:score-%d", score) {
			t.Fatal(score, page, err)
		}
		if !page.Meta.Stale || !page.Meta.DataAsOf.Equal(at.Add(-7*time.Hour)) || page.Items[0].Meta.Stale != (score == 1) {
			t.Fatal("off-page freshness", page)
		}
		if score > 1 {
			if page.NextCursor == nil {
				t.Fatal("missing next page")
			}
			q.Set("cursor", *page.NextCursor)
		} else if page.NextCursor != nil {
			t.Fatal("extra page")
		}
	}
}

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
	// Retained results return before the background outage attempt finishes;
	// drain it before asserting provider accounting and durable retry receipts.
	discover.WaitRefreshes()
	if err != nil || len(list.Items) != 1 || list.Meta.Coverage.Status != "stale" || !list.Meta.Stale || calls.Load() != 2 {
		t.Fatal("outage erased records or fabricated fresh coverage", list, err, calls.Load())
	}
	_, err = service.List(t.Context(), other.ID, nil)
	if err != nil || calls.Load() != 2 {
		t.Fatal("durable outage cooldown not reused", err, calls.Load())
	}
}
