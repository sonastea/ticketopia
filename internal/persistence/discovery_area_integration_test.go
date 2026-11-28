package persistence

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

// areaEvent is a dated listing at a venue in a US city.
func areaEvent(id, name, venue, city, date, clock string) models.Event {
	e := sampleEvent(id)
	e.Name = name
	e.Start.LocalDate, e.Start.LocalTime = &date, &clock
	e.Venues = []models.Venue{{ID: "ticketmaster:" + venue, Place: models.Place{Name: venue, City: city, CountryCode: "US"}}}
	return e
}

func observeAll(t *testing.T, r *DiscoveryRepository, events ...models.Event) {
	t.Helper()
	for _, e := range events {
		if err := r.Observe(t.Context(), models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
}

// Metro searches read every member city once, folded lists show each run once
// with a truthful summary across pages, and ticket add-ons never list.
func TestMariaDBDiscoverMetroRunsAndAddOns(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	r := p.Discovery()
	cancelled := areaEvent("run-4", "Long Show", "theatre", "Los Angeles", "2026-10-10", "20:00:00")
	cancelled.Status = "cancelled"
	twoPlaces := areaEvent("two-places", "Rooftop", "roof", "Los Angeles", "2026-10-09", "21:00:00")
	twoPlaces.Place = &models.Place{City: "Hollywood", CountryCode: "US"}
	upsell := areaEvent("upgrade", "Upgrade: Viewing Deck", "theatre", "Los Angeles", "2026-10-08", "19:00:00")
	upsell.Classifications = append(upsell.Classifications, models.Classification{Type: &models.NamedID{ID: models.UpsellType, Name: "Upsell"}})
	observeAll(t, r,
		areaEvent("run-1", "Long Show", "theatre", "Los Angeles", "2026-10-08", "14:00:00"),
		areaEvent("run-2", "Long Show", "theatre", "Los Angeles", "2026-10-08", "20:00:00"),
		areaEvent("run-3", "Long Show", "theatre", "Los Angeles", "2026-10-09", "20:00:00"),
		cancelled,
		// Same name at another venue is a different run.
		areaEvent("tour-stop", "Long Show", "arena", "Inglewood", "2026-10-09", "19:00:00"),
		areaEvent("slot-1", "Bar Slots", "bar", "West Hollywood", "2026-10-08", "18:00:00"),
		areaEvent("slot-2", "Bar Slots", "bar", "West Hollywood", "2026-10-08", "18:15:00"),
		areaEvent("slot-3", "Bar Slots", "bar", "West Hollywood", "2026-10-08", "21:30:00"),
		twoPlaces, upsell,
		areaEvent("boston", "Elsewhere", "hall", "Boston", "2026-10-08", "20:00:00"),
	)
	search := func(raw string) []models.Event {
		t.Helper()
		if !strings.Contains(raw, "start_date") {
			raw += "&start_date=2026-10-08&end_date=2026-10-10"
		}
		q := catalogQuery(t, "limit=2&"+raw)
		var items []models.Event
		total := -1
		for {
			read, err := r.Search(t.Context(), q, 6*time.Hour)
			if err != nil {
				t.Fatalf("%s: %v", raw, err)
			}
			if total == -1 {
				total = read.List.Total
			} else if read.List.Total != total {
				t.Fatalf("%s: total changed across pages: %d then %d", raw, total, read.List.Total)
			}
			items = append(items, read.List.Items...)
			if read.List.NextCursor == nil {
				break
			}
			v := q.Values()
			v.Set("cursor", *read.List.NextCursor)
			if q, err = discovery.ParseQuery(v, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		if len(items) != total {
			t.Fatalf("%s: paged %d of %d", raw, len(items), total)
		}
		return items
	}
	ids := func(items []models.Event) []string {
		out := []string{}
		for _, e := range items {
			out = append(out, strings.TrimPrefix(e.ID, "ticketmaster:"))
		}
		return out
	}
	// The exact city omits other member cities, and the add-on in it.
	if got := ids(search("city=Los+Angeles&country=US")); !slices.Equal(got, []string{"run-1", "run-2", "run-3", "two-places", "run-4"}) {
		t.Fatalf("exact city: %v", got)
	}
	metro := search("city=los+angeles&country=US&area=metro")
	if got := ids(metro); !slices.Equal(got, []string{"run-1", "slot-1", "slot-2", "run-2", "slot-3", "tour-stop", "run-3", "two-places", "run-4"}) {
		t.Fatalf("metro listings: %v", got)
	}
	// A member city spelled as typed, without a country, reaches the same metro.
	if got := ids(search("city=inglewood&area=metro")); !slices.Equal(got, ids(metro)) {
		t.Fatalf("member city metro: %v", got)
	}
	folded := search("city=Los+Angeles&country=US&area=metro&fold=run")
	if got := ids(folded); !slices.Equal(got, []string{"run-1", "slot-1", "tour-stop", "two-places"}) {
		t.Fatalf("folded runs: %v", got)
	}
	runs := map[string]*models.Run{}
	for _, e := range folded {
		runs[strings.TrimPrefix(e.ID, "ticketmaster:")] = e.Run
	}
	if run := runs["run-1"]; run == nil || run.Count != 4 || run.Days != 3 || run.LastDate != "2026-10-10" || run.Changed != 1 || run.FirstTime != nil {
		t.Fatalf("multi-day run: %+v", run)
	}
	if run := runs["slot-1"]; run == nil || run.Count != 3 || run.Days != 1 || run.FirstTime == nil || *run.FirstTime != "18:00:00" || *run.LastTime != "21:30:00" {
		t.Fatalf("one-day run: %+v", run)
	}
	if runs["tour-stop"] != nil || runs["two-places"] != nil {
		t.Fatal("a single listing has no run summary")
	}
	// Narrower dates still find a run, at its first date in the range.
	if got := ids(search("city=Los+Angeles&country=US&area=metro&fold=run&start_date=2026-10-10&end_date=2026-10-10")); !slices.Equal(got, []string{"run-4"}) {
		t.Fatalf("narrow run: %v", got)
	}
	for _, sort := range []string{"date_desc", "name_asc"} {
		if got := search("city=Los+Angeles&country=US&area=metro&fold=run&sort=" + sort); len(got) != 4 {
			t.Fatalf("%s folded: %v", sort, ids(got))
		}
	}
	dates, err := r.Dates(t.Context(), areaEvent("run-2", "Long Show", "theatre", "Los Angeles", "2026-10-08", "20:00:00"), "2026-10-01", "2026-12-31", time.Time{}, 10)
	if err != nil {
		t.Fatal(err)
	}
	var dateIDs []string
	for _, d := range dates {
		dateIDs = append(dateIDs, strings.TrimPrefix(d.ID, "ticketmaster:"))
	}
	if !slices.Equal(dateIDs, []string{"run-1", "run-2", "run-3", "run-4"}) || dates[3].Status != "cancelled" || *dates[0].LocalTime != "14:00:00" {
		t.Fatalf("event dates: %v %+v", dateIDs, dates)
	}
	// Until each member is collected, a metro is not covered and needs refresh.
	read, err := r.Search(t.Context(), catalogQuery(t, "city=Los+Angeles&country=US&area=metro"), 6*time.Hour)
	if err != nil || !read.Refresh || read.List.Meta.Coverage == nil || read.List.Meta.Coverage.Status != "not_collected" {
		t.Fatalf("metro coverage: %+v %v", read.List.Meta, err)
	}
}

// Runs compare titles exactly, keep their order in every sort, and never
// present a placeholder time as a showtime.
func TestMariaDBDiscoverRunIdentityAndTimes(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	r := p.Discovery()
	tba := areaEvent("tba-1", "Late Set", "club", "Los Angeles", "2026-10-08", "19:00:00")
	tba.Start.TimeTBA = true
	observeAll(t, r,
		areaEvent("cafe-1", "Cafe", "club", "Los Angeles", "2026-10-08", "20:00:00"),
		areaEvent("cafe-2", "Cafe", "club", "Los Angeles", "2026-10-09", "20:00:00"),
		areaEvent("cafe-accent", "Café", "club", "Los Angeles", "2026-10-08", "21:00:00"),
		tba,
		areaEvent("late-2", "Late Set", "club", "Los Angeles", "2026-10-08", "22:00:00"),
	)
	for sort, want := range map[string][]string{
		"date_asc":  {"ticketmaster:tba-1", "ticketmaster:cafe-1", "ticketmaster:cafe-accent"},
		"date_desc": {"ticketmaster:cafe-accent", "ticketmaster:cafe-1", "ticketmaster:tba-1"},
		"name_asc":  {"ticketmaster:cafe-1", "ticketmaster:cafe-accent", "ticketmaster:tba-1"},
	} {
		q := catalogQuery(t, "city=Los+Angeles&country=US&fold=run&limit=1&start_date=2026-10-08&end_date=2026-10-09&sort="+sort)
		var got []string
		runs := map[string]*models.Run{}
		for {
			read, err := r.Search(t.Context(), q, 6*time.Hour)
			if err != nil || read.List.Total != 3 {
				t.Fatalf("%s: total %d %v", sort, read.List.Total, err)
			}
			for _, e := range read.List.Items {
				got = append(got, e.ID)
				runs[e.ID] = e.Run
			}
			if read.List.NextCursor == nil {
				break
			}
			v := q.Values()
			v.Set("cursor", *read.List.NextCursor)
			if q, err = discovery.ParseQuery(v, time.Now()); err != nil {
				t.Fatal(err)
			}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s order: %v", sort, got)
		}
		if run := runs["ticketmaster:cafe-1"]; run == nil || run.Count != 2 || runs["ticketmaster:cafe-accent"] != nil {
			t.Fatalf("%s: differently accented titles shared a run: %+v", sort, runs)
		}
		if run := runs["ticketmaster:tba-1"]; run == nil || run.FirstTime == nil || *run.FirstTime != "22:00:00" || *run.LastTime != "22:00:00" {
			t.Fatalf("%s: placeholder time in run summary: %+v", sort, run)
		}
	}
	dates, err := r.Dates(t.Context(), tba, "2026-10-01", "2026-12-31", time.Time{}, 10)
	if err != nil || len(dates) != 2 || dates[0].LocalTime != nil || *dates[1].LocalTime != "22:00:00" {
		t.Fatalf("dates with a placeholder time: %+v %v", dates, err)
	}
	if dates, err := r.Dates(t.Context(), areaEvent("cafe-1", "Cafe", "club", "Los Angeles", "2026-10-08", "20:00:00"), "2026-10-01", "2026-12-31", time.Time{}, 10); err != nil || len(dates) != 2 {
		t.Fatalf("exact-title dates: %+v %v", dates, err)
	}
	// Listings that have started are no longer upcoming.
	if dates, err := r.Dates(t.Context(), tba, "2026-10-01", "2026-12-31", time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC), 10); err != nil || len(dates) != 0 {
		t.Fatalf("started dates listed: %+v %v", dates, err)
	}
}

// Scheduled collection of a metro city collects the city day by day, then
// keeps the rest of its area fresh with one broad read per member, never the
// city's own broad range. Once everything is fresh, nothing is collected.
func TestMariaDBScheduledMetroKeepsAreaFresh(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	var mu sync.Mutex
	daily, broad := map[string]int{}, map[string]int{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		q := req.URL.Query()
		city, dates := q.Get("city"), strings.Split(q.Get("localStartDateTime"), ",")
		mu.Lock()
		if len(dates) == 2 && dates[0][:10] == dates[1][:10] {
			daily[city]++
		} else {
			broad[city]++
		}
		mu.Unlock()
		events := []any{}
		if city == "Inglewood" {
			events = append(events, map[string]any{"id": "kept-fresh", "name": "Stadium Show", "dates": map[string]any{"start": map[string]string{"localDate": time.Now().UTC().Format(time.DateOnly), "localTime": "19:00:00"}, "status": map[string]string{"code": "onsale"}},
				"_embedded": map[string]any{"venues": []any{map[string]any{"id": "stadium", "name": "Stadium", "city": map[string]string{"name": "Inglewood"}, "country": map[string]string{"countryCode": "US"}}}}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"_embedded": map[string]any{"events": events}, "page": map[string]int{"number": 0, "totalElements": len(events), "totalPages": 1}})
	}))
	t.Cleanup(upstream.Close)
	cache := kv.NewMemory()
	t.Cleanup(func() { _ = cache.Close() })
	s := discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "metro-schedule", BaseURL: upstream.URL, Catalog: p.Discovery(),
		Coordinator: p.ProviderBudget("metro-schedule", 1000), DailyBudget: 1000, RequestInterval: time.Millisecond})
	t.Cleanup(s.WaitRefreshes)
	c := history.New(p.History(), s, history.Config{Cities: []history.Scope{{City: "Los Angeles", Country: "US", Area: discovery.AreaMetro}}, Days: 1, Interval: 6 * time.Hour}, zerolog.Nop())
	for range 10 {
		worked, err := c.Once(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		if !worked {
			break
		}
	}
	s.WaitRefreshes()
	m, _ := discovery.MetroFor("Los Angeles", "US")
	mu.Lock()
	if daily["Los Angeles"] != 3 || broad["Los Angeles"] != 0 {
		t.Fatalf("Los Angeles collected daily %d times and broadly %d times", daily["Los Angeles"], broad["Los Angeles"])
	}
	for _, city := range m.Cities[1:] {
		if broad[city] != 1 {
			t.Errorf("member %s collected %d times", city, broad[city])
		}
	}
	clear(daily)
	clear(broad)
	mu.Unlock()
	today := time.Now().UTC()
	q := catalogQuery(t, "city=Los+Angeles&country=US&area=metro&start_date="+today.AddDate(0, 0, -1).Format(time.DateOnly)+"&end_date="+today.AddDate(0, 0, 1).Format(time.DateOnly))
	list, err := s.Events(t.Context(), q)
	if err != nil || list.Meta.Coverage == nil || list.Meta.Coverage.Status != "complete" || list.Meta.Stale || len(list.Items) != 1 {
		t.Fatalf("kept-fresh area: %+v items=%d %v", list.Meta, len(list.Items), err)
	}
	if err := s.KeepFresh(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	s.WaitRefreshes()
	mu.Lock()
	defer mu.Unlock()
	if len(daily)+len(broad) != 0 {
		t.Fatalf("fresh area collected again: daily %v broad %v", daily, broad)
	}
}

// A cold metro search collects every member city's own scope, reports the
// metro, and lists what the members returned.
func TestMariaDBDiscoverMetroCollectsMembers(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	var mu sync.Mutex
	cities := map[string]int{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		city := req.URL.Query().Get("city")
		mu.Lock()
		cities[city]++
		mu.Unlock()
		events := []any{}
		if city == "Inglewood" {
			events = append(events, map[string]any{"id": "metro-1", "name": "Stadium Show", "dates": map[string]any{"start": map[string]string{"localDate": "2026-10-08", "localTime": "19:00:00"}, "status": map[string]string{"code": "onsale"}},
				"_embedded": map[string]any{"venues": []any{map[string]any{"id": "stadium", "name": "Stadium", "city": map[string]string{"name": "Inglewood"}, "country": map[string]string{"countryCode": "US"}}}}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"_embedded": map[string]any{"events": events}, "page": map[string]int{"number": 0, "totalElements": len(events), "totalPages": 1}})
	}))
	t.Cleanup(upstream.Close)
	cache := kv.NewMemory()
	t.Cleanup(func() { _ = cache.Close() })
	s := discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "metro-fixture", BaseURL: upstream.URL, Catalog: p.Discovery(),
		Coordinator: p.ProviderBudget("metro-fixture", 1000), DailyBudget: 1000, RequestInterval: time.Millisecond})
	t.Cleanup(s.WaitRefreshes)
	list, err := s.Events(t.Context(), catalogQuery(t, "city=Los+Angeles&country=US&area=metro&start_date=2026-10-08&end_date=2026-10-10"))
	if err != nil {
		t.Fatal(err)
	}
	s.WaitRefreshes()
	m, _ := discovery.MetroFor("Los Angeles", "US")
	mu.Lock()
	for _, city := range m.Cities {
		if cities[city] != 1 {
			t.Errorf("member %s collected %d times", city, cities[city])
		}
	}
	mu.Unlock()
	if list.Area == nil || list.Area.Name != "Los Angeles area" || len(list.Area.Cities) != len(m.Cities) {
		t.Fatalf("metro area: %+v", list.Area)
	}
	// A cold visitor's first read may end its bounded wait before every member
	// is collected (53 members at the durable four-a-second pace). Once every
	// member is collected, the metro lists their events, complete and fresh.
	list, err = s.Events(t.Context(), catalogQuery(t, "city=Los+Angeles&country=US&area=metro&start_date=2026-10-08&end_date=2026-10-10"))
	if err != nil || list.Meta.Coverage == nil || list.Meta.Coverage.Status != "complete" || list.Meta.Stale {
		t.Fatalf("collected metro coverage: %+v %v", list.Meta, err)
	}
	if len(list.Items) != 1 || list.Items[0].ID != "ticketmaster:metro-1" {
		t.Fatalf("metro results: %+v", list.Items)
	}
	// One stale member refreshes alone; fresh members make no provider calls.
	if _, err := p.db.ExecContext(t.Context(), `UPDATE discovery_scopes SET last_success=TIMESTAMPADD(HOUR,-7,last_success),next_refresh=UTC_TIMESTAMP(6) WHERE city='Inglewood'`); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	clear(cities)
	mu.Unlock()
	list, err = s.Events(t.Context(), catalogQuery(t, "city=Los+Angeles&country=US&area=metro&start_date=2026-10-08&end_date=2026-10-10"))
	if err != nil || len(list.Items) != 1 || list.Meta.Coverage.Status != "stale" {
		t.Fatalf("stale member serves last-good results: %+v %v", list.Meta, err)
	}
	s.WaitRefreshes()
	mu.Lock()
	defer mu.Unlock()
	if len(cities) != 1 || cities["Inglewood"] != 1 {
		t.Fatalf("refreshed members: %v", cities)
	}
}
