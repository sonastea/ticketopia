package persistence

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

func catalogQuery(t *testing.T, raw string) discovery.Query {
	t.Helper()
	v, err := url.ParseQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	q, err := discovery.ParseQuery(v, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	return q
}
func catalogService(t *testing.T, p *Pool, upstream string, budget int) *discovery.Service {
	t.Helper()
	cache := kv.NewMemory()
	t.Cleanup(func() { _ = cache.Close() })
	s := discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "catalog-fixture", BaseURL: upstream, Catalog: p.Discovery(), Coordinator: p.ProviderBudget("catalog-fixture", budget), DailyBudget: budget})
	t.Cleanup(s.WaitRefreshes)
	return s
}
func fixtureCatalog(w http.ResponseWriter, r *http.Request, total int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("size"))
	if size == 0 {
		size = 100
	}
	events := []any{}
	for i := page * size; i < min((page+1)*size, total); i++ {
		events = append(events, map[string]any{"id": fmt.Sprintf("catalog-%03d", i), "name": fmt.Sprintf("Show %03d", i), "dates": map[string]any{"start": map[string]string{"localDate": "2026-10-08", "localTime": "20:00:00"}, "status": map[string]string{"code": "onsale"}}, "_embedded": map[string]any{"venues": []any{map[string]any{"id": "venue", "name": "Arena", "city": map[string]string{"name": "Berlin"}, "country": map[string]string{"countryCode": "DE"}}}, "attractions": []any{map[string]string{"id": "artist", "name": "The Band"}}}})
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"_embedded": map[string]any{"events": events}, "page": map[string]int{"number": page, "totalElements": total, "totalPages": (total + size - 1) / size}})
}

func TestMariaDBDiscoverSQLFiltersAndPagination(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	r := p.Discovery()
	for i := range 36 {
		e := sampleEvent(fmt.Sprintf("sql-%02d", i))
		date := fmt.Sprintf("2026-10-%02d", 8+i%3)
		e.Start.LocalDate = &date
		e.Name = fmt.Sprintf("Show %02d", i)
		e.Venues[0].CountryCode = "DE"
		if i%2 == 0 {
			e.Artists = []models.Artist{{ID: "ticketmaster:Artist", Name: "AB 100%_"}}
		} else {
			e.Artists = []models.Artist{{ID: "ticketmaster:artist", Name: "Other"}}
		}
		if i == 0 {
			e.Venues = append(e.Venues, models.Venue{ID: "ticketmaster:other", Place: models.Place{Name: "Other arena", City: "Paris", CountryCode: "FR"}})
		}
		if err := r.Observe(t.Context(), models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: time.Now()}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, tt := range []struct {
		filters string
		total   int
		first   string
	}{
		{"city=berlin&country=de", 36, "sql-00"},
		{"city=Paris&country=FR", 1, "sql-00"},
		{"country=FR", 1, "sql-00"},
		{"category_id=sports&genre_id=ball&artist_id=ticketmaster:Artist", 18, "sql-00"},
		{"artist_id=ticketmaster:artist", 18, "sql-03"},
		{"keyword=100%25_", 18, "sql-00"},
		{"keyword=AB", 18, "sql-00"},
		{"keyword=arena", 36, "sql-00"},
		{"venue_id=ticketmaster:other", 1, "sql-00"},
		{"sort=date_desc", 36, "sql-02"},
		{"sort=name_asc", 36, "sql-00"},
		{"city=London", 0, ""},
	} {
		q := catalogQuery(t, "start_date=2026-10-08&end_date=2026-10-10&limit=5&"+tt.filters)
		read, err := r.Search(t.Context(), q, 6*time.Hour)
		if err != nil || read.List.Total != tt.total {
			t.Fatalf("%s: %+v %v", tt.filters, read, err)
		}
		if tt.first != "" && read.List.Items[0].ID != "ticketmaster:"+tt.first {
			t.Fatalf("%s ordering: %+v", tt.filters, read.List.Items)
		}
		seen := map[string]bool{}
		for {
			for _, e := range read.List.Items {
				if seen[e.ID] {
					t.Fatal("duplicate page event")
				}
				seen[e.ID] = true
			}
			if read.List.NextCursor == nil {
				break
			}
			v := q.Values()
			v.Set("cursor", *read.List.NextCursor)
			q, err = discovery.ParseQuery(v, time.Now())
			if err != nil || !q.Stored {
				t.Fatal("not a stored cursor", err)
			}
			read, err = r.Search(t.Context(), q, 6*time.Hour)
			if err != nil {
				t.Fatal(err)
			}
		}
		if len(seen) != tt.total {
			t.Fatal("pagination lost results")
		}
	}
	q := catalogQuery(t, "start_date=2026-10-09&end_date=2026-10-09")
	read, err := r.Search(t.Context(), q, 6*time.Hour)
	if err != nil || read.List.Total != 12 {
		t.Fatal("date filtering", err)
	}
	// Backdated snapshots cannot rewind SQL projections.
	e := sampleEvent("sql-00")
	e.Venues[0].City = "London"
	if err := r.Observe(t.Context(), models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: time.Now().Add(-time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	read, err = r.Search(t.Context(), catalogQuery(t, "city=London&start_date=2026-10-08&end_date=2026-10-10"), 6*time.Hour)
	if err != nil || read.List.Total != 0 {
		t.Fatal("backdated projection rewind", err)
	}
}

func TestMariaDBDiscoverOnDemandOutageQuotaRestartAndAPI(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	var calls atomic.Int32
	var outage atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if outage.Load() {
			w.WriteHeader(503)
			return
		}
		fixtureCatalog(w, r, 105)
	}))
	defer upstream.Close()
	s := catalogService(t, p, upstream.URL, 2)
	q := catalogQuery(t, "city=Berlin&country=DE&start_date=2026-10-08&end_date=2026-10-10&limit=5")
	list, err := s.Events(t.Context(), q)
	if err != nil || list.Total != 105 || len(list.Items) != 5 || list.Meta.Coverage.Status != "complete" || calls.Load() != 2 {
		t.Fatalf("on-demand ingestion: %+v %v calls %d", list, err, calls.Load())
	}
	// A different local filter/sort/narrower date uses the broad durable coverage.
	filtered := catalogQuery(t, "city=Berlin&country=DE&start_date=2026-10-08&end_date=2026-10-08&keyword=Band&artist_id=ticketmaster:artist&sort=name_asc")
	list, err = s.Events(t.Context(), filtered)
	if err != nil || list.Total != 105 || calls.Load() != 2 {
		t.Fatal("filtered request spent quota", err)
	}
	// Independent pool/cache/process has the same successful scope and quota.
	restarted := catalogService(t, f.open(t), upstream.URL, 2)
	list, err = restarted.Events(t.Context(), q)
	if err != nil || list.Total != 105 || calls.Load() != 2 {
		t.Fatal("restart lost catalog coverage", err)
	}
	// Expire receipts, exhaust the durable allowance, and retain stored pages.
	execSQL(t, f.admin, `UPDATE `+f.runtime.Database+`.discovery_scopes SET last_success=TIMESTAMPADD(HOUR,-8,UTC_TIMESTAMP(6)),next_refresh=UTC_TIMESTAMP(6)`)
	list, err = restarted.Events(t.Context(), q)
	restarted.WaitRefreshes()
	if err != nil || !list.Meta.Stale || list.Meta.Coverage.Status != "stale" || list.Total != 105 || calls.Load() != 2 {
		t.Fatalf("quota fallback %+v %v", list, err)
	}
	// Reset only this disposable fixture's budget to exercise a real upstream outage.
	execSQL(t, f.admin, `UPDATE `+f.runtime.Database+`.provider_budgets SET used=0,blocked_until=UTC_TIMESTAMP(6)`)
	execSQL(t, f.admin, `UPDATE `+f.runtime.Database+`.discovery_scopes SET next_refresh=UTC_TIMESTAMP(6)`)
	outage.Store(true)
	restarted = catalogService(t, f.open(t), upstream.URL, 2)
	list, err = restarted.Events(t.Context(), q)
	restarted.WaitRefreshes()
	if err != nil || !list.Meta.Stale || list.Total != 105 || calls.Load() != 3 {
		t.Fatalf("outage fallback %+v %v calls=%d", list, err, calls.Load())
	}
	cache := kv.NewMemory()
	defer cache.Close()
	t.Setenv("TICKETMASTER_KEY", "")
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	app, err := api.NewAPI(t.Context(), zerolog.Nop(), cache, api.WithDiscovery(restarted))
	if err != nil {
		t.Fatal(err)
	}
	routes := app.Routes()
	for _, target := range []string{"/api/v1/events?" + q.Values().Encode(), "/?" + q.Values().Encode(), "/api/v1/events/ticketmaster:catalog-000/history", "/api/v1/events/ticketmaster:catalog-000", "/events/ticketmaster:catalog-000?section=overview"} {
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, httptest.NewRequest("GET", target, nil))
		if response.Code != 200 {
			t.Fatalf("%s: %d %s", target, response.Code, response.Body.String())
		}
		for _, private := range []string{"account_id", "saved_at", "followed_at", "preferences", "catalog-fixture", "apikey"} {
			if strings.Contains(response.Body.String(), private) {
				t.Fatalf("public response leaked %s", private)
			}
		}
	}
	unknown := catalogQuery(t, "city=Unknown&country=DE&start_date=2026-10-08&end_date=2026-10-08")
	list, err = restarted.Events(t.Context(), unknown)
	if err != nil || len(list.Items) != 0 || list.Meta.Coverage.Status != "not_collected" {
		t.Fatal("uncollected represented as empty", err)
	}
	stats, err := p.Discovery().OperationalStatus(t.Context(), 6*time.Hour)
	if err != nil || stats["stale_scopes"] < 1 || stats["failed_scopes"] < 1 || stats["quota_used"] != 1 {
		t.Fatalf("operational status: %+v %v", stats, err)
	}
}

func TestMariaDBDiscoverPartialEmptyAndDailyCoverage(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("city") == "Empty" {
			fixtureCatalog(w, r, 0)
			return
		}
		if r.URL.Query().Get("page") == "1" {
			w.WriteHeader(503)
			return
		}
		fixtureCatalog(w, r, 105)
	}))
	defer upstream.Close()
	s := catalogService(t, p, upstream.URL, 100)
	q := catalogQuery(t, "city=Berlin&country=DE&start_date=2026-10-08&end_date=2026-10-10")
	list, err := s.Events(t.Context(), q)
	if err != nil || list.Total != 100 || !list.Limited || list.Meta.Coverage.Status != "partial" {
		t.Fatalf("partial %+v %v", list, err)
	}
	_, err = catalogService(t, f.open(t), upstream.URL, 100).Events(t.Context(), q)
	if err != nil || calls.Load() != 2 {
		t.Fatal("partial restart ignored retry receipt", err)
	}
	execSQL(t, f.admin, `UPDATE `+f.runtime.Database+`.provider_budgets SET blocked_until=UTC_TIMESTAMP(6)`)
	q.City = "Empty"
	list, err = catalogService(t, p, upstream.URL, 100).Events(t.Context(), q)
	if err != nil || list.Total != 0 || list.Meta.Coverage.Status != "complete" {
		t.Fatalf("confirmed empty %+v %v", list, err)
	}
	r := p.History()
	for offset := range 3 {
		task := history.NewTask(history.Scope{City: "Scheduled", Country: "DE"}, fmt.Sprintf("2026-10-%02d", 8+offset))
		if err := r.Schedule(t.Context(), []history.Task{task}); err != nil {
			t.Fatal(err)
		}
		claim := collectionClaim(t, r, task.ID)
		if err := r.Page(t.Context(), claim, 0, models.EventList{Items: []models.Event{}, Meta: models.Freshness{DataAsOf: time.Now()}}); err != nil {
			t.Fatal(err)
		}
		if _, err := r.Finish(t.Context(), claim, "complete", "", 6*time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	q.City = "Scheduled"
	list, err = s.Events(t.Context(), q)
	if err != nil || list.Meta.Coverage.CollectedDays != 3 || list.Meta.Coverage.Status != "complete" || calls.Load() != 3 {
		t.Fatal("daily coverage not reused", err)
	}
}

func TestMariaDBMissingEventDetailAndPublicHistory(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	r := p.History()
	task := history.NewTask(history.Scope{City: "Berlin", Country: "DE"}, "2026-10-08")
	if err := r.Schedule(t.Context(), []history.Task{task}); err != nil {
		t.Fatal(err)
	}
	e := sampleEvent("missing")
	e.Venues[0].CountryCode = "DE"
	e.Name = "History event"
	e.Venues[0].Name = "Old <venue>"
	claim := collectionClaim(t, r, task.ID)
	at := time.Now().Add(-time.Hour)
	if err := r.Page(t.Context(), claim, 0, models.EventList{Items: []models.Event{e}, Total: 1, Meta: models.Freshness{DataAsOf: at}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Finish(t.Context(), claim, "complete", "", time.Hour); err != nil {
		t.Fatal(err)
	}
	execSQL(t, f.admin, `UPDATE `+f.runtime.Database+`.collection_tasks SET next_refresh=UTC_TIMESTAMP(6)`)
	claim = collectionClaim(t, r, task.ID)
	if err := r.Page(t.Context(), claim, 0, models.EventList{Meta: models.Freshness{DataAsOf: time.Now()}}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Finish(t.Context(), claim, "complete", "", time.Hour); err != nil {
		t.Fatal(err)
	}
	var missing atomic.Bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if missing.Load() {
			w.WriteHeader(404)
			return
		}
		fmt.Fprint(w, `{"id":"missing","name":"History event","dates":{"start":{"dateTBA":true},"status":{"code":"onsale"}},"_embedded":{"venues":[{"id":"new","name":"New venue"}]}}`)
	}))
	defer upstream.Close()
	s := catalogService(t, p, upstream.URL, 100)
	pending, err := s.Event(t.Context(), e.ID)
	if err != nil || !pending.Meta.Stale {
		t.Fatal("omitted fresh event was not marked last-known", err)
	}
	worked, err := history.RefreshMissing(t.Context(), r, s)
	if !worked || err != nil {
		t.Fatal("detail not refreshed", err)
	}
	detail, err := s.Event(t.Context(), e.ID)
	if err != nil || !detail.Item.Start.DateTBA || detail.Item.Status != "onsale" || detail.History == nil || len(detail.History.Changes) != 2 {
		t.Fatalf("targeted TBA/history %+v %v", detail, err)
	}
	if detail.History.Changes[1].Summary != "Moved from Old <venue>" {
		t.Fatal("venue name missing from history")
	}
	missing.Store(true)
	execSQL(t, f.admin, `INSERT INTO `+f.runtime.Database+`.event_detail_tasks (event_id) VALUES (?)`, e.ID)
	before := detail.History.LastSeen
	worked, err = history.RefreshMissing(t.Context(), r, s)
	if !worked || err != nil {
		t.Fatal(err)
	}
	h, err := p.Discovery().History(t.Context(), e.ID, time.Time{}, 1)
	if err != nil || !h.LastSeen.Equal(before) {
		t.Fatal("404 advanced history")
	}
	for i := range 3 {
		changed := detail
		date := fmt.Sprintf("2026-12-%02d", i+1)
		changed.Item.Start.DateTBA = false
		changed.Item.Start.LocalDate = &date
		changed.Meta.DataAsOf = before.Add(time.Duration(i+1) * time.Second)
		if err := p.Discovery().Observe(t.Context(), changed); err != nil {
			t.Fatal(err)
		}
	}
	h, err = p.Discovery().History(t.Context(), e.ID, time.Time{}, 1)
	if err != nil || h.NextBefore == nil || len(h.Changes) != 1 {
		t.Fatal("history pagination", err)
	}
	next, err := p.Discovery().History(t.Context(), e.ID, *h.NextBefore, 1)
	if err != nil || len(next.Changes) != 1 || !next.Changes[0].ObservedAt.Before(h.Changes[0].ObservedAt) {
		t.Fatal("history page repeated", err)
	}
}

func TestMariaDBDiscoverClaimsRecoveryAndPagingBeyondProviderCap(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	other := f.open(t)
	q := catalogQuery(t, "city=Berlin&country=DE&start_date=2026-10-08&end_date=2026-10-08&limit=1")
	var wg sync.WaitGroup
	tokens := make(chan string, 2)
	errs := make(chan error, 2)
	for _, r := range []*DiscoveryRepository{p.Discovery(), other.Discovery()} {
		wg.Go(func() { token, err := r.ClaimSearch(t.Context(), q); tokens <- token; errs <- err })
	}
	wg.Wait()
	close(tokens)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var token string
	owned := 0
	for value := range tokens {
		if value != "" {
			token = value
			owned++
		}
	}
	if owned != 1 {
		t.Fatal("duplicate scope ownership")
	}
	e := sampleEvent("retained")
	date := "2026-10-08"
	e.Start.LocalDate = &date
	e.Venues[0].CountryCode = "DE"
	page := models.EventList{Items: []models.Event{e}, Total: 1, Meta: models.Freshness{DataAsOf: time.Now()}}
	if err := p.Discovery().SearchPage(t.Context(), q, token, 0, page); err != nil {
		t.Fatal(err)
	}
	if err := p.Discovery().SearchPage(t.Context(), q, token, 0, page); err != nil {
		t.Fatal("page retry", err)
	}
	execSQL(t, f.admin, `UPDATE `+f.runtime.Database+`.discovery_scopes SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6))`)
	newToken, err := other.Discovery().ClaimSearch(t.Context(), q)
	if err != nil || newToken == "" || newToken == token {
		t.Fatal("claim recovery", err)
	}
	if _, err := p.Discovery().FinishSearch(t.Context(), q, token, "complete", time.Hour); !errors.Is(err, history.ErrLeaseLost) {
		t.Fatal("expired worker was not fenced", err)
	}
	if _, err := other.Discovery().FinishSearch(t.Context(), q, newToken, "failed", time.Hour); err != nil {
		t.Fatal(err)
	}
	read, err := p.Discovery().Search(t.Context(), q, 6*time.Hour)
	if err != nil || read.List.Total != 1 || read.List.Meta.Coverage.Status != "partial" {
		t.Fatal("crash erased committed pages", err)
	}
	execSQL(t, f.admin, `UPDATE `+f.runtime.Database+`.discovery_scopes SET next_refresh=UTC_TIMESTAMP(6)`)
	token, err = p.Discovery().ClaimSearch(t.Context(), q)
	if err != nil || token == "" {
		t.Fatal("shifted-result claim", err)
	}
	page.Total = 2
	if err := p.Discovery().SearchPage(t.Context(), q, token, 0, page); err != nil {
		t.Fatal(err)
	}
	status, err := p.Discovery().FinishSearch(t.Context(), q, token, "complete", time.Hour)
	if err != nil || status != "partial" {
		t.Fatal("shifted totals falsely completed coverage", status, err)
	}
	// Bulk fixture identities use the same public schema; the query must not
	// inherit Ticketmaster's 1,000-result cap for durable SQL pagination.
	for i := range 1002 {
		id := fmt.Sprintf("ticketmaster:overflow-%04d", i)
		execSQL(t, f.admin, `INSERT INTO `+f.runtime.Database+`.events SELECT ?,name,source_url,start_utc,local_date,local_time,timezone,date_tba,date_tbd,time_tba,no_specific_time,status,venues,artists,classifications,place,created_at,updated_at FROM `+f.runtime.Database+`.events WHERE event_id=?`, id, e.ID)
		execSQL(t, f.admin, `INSERT INTO `+f.runtime.Database+`.event_snapshots SELECT ?,JSON_SET(snapshot,'$.id',?,'$.source.id',?),data_as_of FROM `+f.runtime.Database+`.event_snapshots WHERE event_id=?`, id, id, strings.TrimPrefix(id, "ticketmaster:"), e.ID)
		execSQL(t, f.admin, `INSERT INTO `+f.runtime.Database+`.event_search_places VALUES (?,?,?)`, id, "Berlin", "DE")
	}
	q.Page = 1000
	read, err = p.Discovery().Search(t.Context(), q, 6*time.Hour)
	if err != nil || read.List.Total != 1003 || len(read.List.Items) != 1 || read.List.NextCursor == nil {
		t.Fatalf("SQL paging stopped at provider cap %+v %v", read, err)
	}
	v := q.Values()
	v.Set("cursor", *read.List.NextCursor)
	next, err := discovery.ParseQuery(v, time.Now())
	if err != nil || next.Page != 1001 || !next.Stored {
		t.Fatal("stored cursor capped", err)
	}
	v.Set("sort", "name_asc")
	if _, err := discovery.ParseQuery(v, time.Now()); err == nil {
		t.Fatal("stored cursor accepted changed sort")
	}
}
