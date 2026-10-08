package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

func collectionClaim(t *testing.T, r *HistoryRepository, id string) history.Claim {
	t.Helper()
	claim, err := r.Claim(t.Context(), []string{id})
	if err != nil {
		t.Fatal(err)
	}
	return claim
}

func TestMariaDBEventHistoryRetentionAndChanges(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	r := p.History()
	task := history.NewTask(history.Scope{City: "Berlin", Country: "DE"}, "2026-10-08")
	if err := r.Schedule(t.Context(), []history.Task{task}); err != nil {
		t.Fatal(err)
	}
	claim := collectionClaim(t, r, task.ID)
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	e := sampleEvent("history")
	e.Artists = []models.Artist{{ID: "ticketmaster:Artist", Name: "Artist"}, {ID: "ticketmaster:artist", Name: "Different artist"}}
	e.PublicSale.Start = &at
	page := func(e models.Event, at time.Time, n int) {
		t.Helper()
		if err := r.Page(t.Context(), claim, n, models.EventList{Items: []models.Event{e}, Total: 1, Meta: models.Freshness{DataAsOf: at}}); err != nil {
			t.Fatal(err)
		}
	}
	page(e, at, 0)
	page(e, at, 0) // Retry cannot duplicate a receipt or observation.
	changed := e
	changed.Status = "postponed"
	date := "2026-11-10"
	changed.Start.LocalDate = &date
	changed.Venues = []models.Venue{{ID: "ticketmaster:new-venue", Place: models.Place{Name: "New venue"}}}
	sale := at.Add(24 * time.Hour)
	changed.PublicSale.Start = &sale
	page(changed, at.Add(time.Hour), 1)
	changed.Status = "cancelled"
	page(changed, at.Add(2*time.Hour), 2)
	// An older concurrently collected page is historical, not a metadata rewind.
	page(e, at.Add(-time.Hour), 3)
	if _, err := r.Finish(t.Context(), claim, "partial", "provider_unavailable", time.Hour); err != nil {
		t.Fatal(err)
	}
	observations, err := r.Observations(t.Context(), e.ID, time.Time{}, 100)
	if err != nil || len(observations) != 4 || len(observations[0].Changes) != 1 || observations[0].Changes[0].Kind != "cancellation" || len(observations[1].Changes) != 4 || len(observations[3].Changes) != 0 {
		t.Fatalf("observations: %+v %v", observations, err)
	}
	for i, kind := range []string{"date", "venue", "sale_time", "postponement"} {
		if observations[1].Changes[i].Kind != kind {
			t.Fatalf("missing %s change", kind)
		}
	}
	freshness, err := r.Freshness(t.Context(), e.ID)
	if err != nil || !freshness.FirstSeen.Equal(at.Add(-time.Hour)) || !freshness.LastSeen.Equal(at.Add(2*time.Hour)) || freshness.LastChanged == nil || !freshness.LastChanged.Equal(freshness.LastSeen) {
		t.Fatalf("freshness: %+v %v", freshness, err)
	}
	current, err := p.Events().Get(t.Context(), e.ID)
	if err != nil || current.Status != "cancelled" || *current.Start.LocalDate != date {
		t.Fatal("older collection replaced current event", err)
	}
	var artists, venues, mappings, pages int
	if err := p.db.QueryRowContext(t.Context(), `SELECT (SELECT COUNT(*) FROM artists),(SELECT COUNT(*) FROM venues),(SELECT COUNT(*) FROM artist_providers),(SELECT COUNT(*) FROM collection_pages)`).Scan(&artists, &venues, &mappings, &pages); err != nil || artists != 2 || venues != 2 || mappings != 2 || pages != 4 {
		t.Fatalf("catalog/receipt counts %d %d %d %d %v", artists, venues, mappings, pages, err)
	}
	// Failed and empty successful refreshes preserve all good observations.
	for _, status := range []string{"failed", "complete"} {
		execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".collection_tasks SET next_refresh=UTC_TIMESTAMP(6) WHERE task_id=?", task.ID)
		claim = collectionClaim(t, r, task.ID)
		if status == "complete" {
			if err := r.Page(t.Context(), claim, 0, models.EventList{Items: []models.Event{}, Meta: models.Freshness{DataAsOf: at.Add(3 * time.Hour)}}); err != nil {
				t.Fatal(err)
			}
		}
		failure := ""
		if status == "failed" {
			failure = "provider_unavailable"
		}
		if _, err := r.Finish(t.Context(), claim, status, failure, time.Hour); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := f.open(t)
	stored, err := restarted.Saved().Snapshot(t.Context(), e.ID)
	if err != nil || stored.Item.Status != "cancelled" || !stored.Meta.Stale || !stored.Meta.DataAsOf.Equal(at.Add(2*time.Hour)) {
		t.Fatal("restart lost last good metadata", err)
	}
	again, err := restarted.History().Observations(t.Context(), e.ID, time.Time{}, 100)
	if err != nil || len(again) != 4 {
		t.Fatal("failure/absence erased history", err)
	}
	// Public fallback works with accounts disabled and never exposes owner state.
	cache := kv.NewMemory()
	defer cache.Close()
	t.Setenv("TICKETMASTER_KEY", "")
	a, err := api.NewAPI(t.Context(), zerolog.Nop(), cache, api.WithSavedEvents(restarted.Saved()))
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	a.Routes().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/events/"+e.ID, nil))
	var detail models.EventDetail
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &detail) != nil || detail.Item.Status != "cancelled" || !detail.Meta.Stale {
		t.Fatalf("durable public fallback: %d %s", rec.Code, rec.Body.String())
	}
	for _, query := range []string{`UPDATE event_observations SET changes='[]'`, `DELETE FROM event_observations`, `DELETE FROM artists`, `DELETE FROM collection_pages`} {
		if _, err := restarted.db.ExecContext(t.Context(), query); err == nil {
			t.Fatal("runtime can destroy immutable history", query)
		}
	}
}

func TestMariaDBCollectionClaimsFencingAndAtomicPages(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t)}
	r := pools[0].History()
	task := history.NewTask(history.Scope{City: "Chicago", Country: "US"}, "2026-10-08")
	if err := r.Schedule(t.Context(), []history.Task{task}); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	claims := make(chan history.Claim, 3)
	for _, p := range pools {
		wg.Go(func() {
			claim, err := p.History().Claim(t.Context(), []string{task.ID})
			if err == nil {
				claims <- claim
			} else if !errors.Is(err, history.ErrNoTask) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	close(claims)
	if len(claims) != 1 {
		t.Fatal("independent workers collected same task", len(claims))
	}
	first := <-claims
	list := models.EventList{Items: []models.Event{sampleEvent("atomic-page"), {ID: "bad"}}, Total: 2, Meta: models.Freshness{DataAsOf: time.Now().UTC()}}
	if err := r.Page(t.Context(), first, 0, list); err == nil {
		t.Fatal("invalid page committed")
	}
	var count int
	if err := pools[0].db.QueryRowContext(t.Context(), `SELECT (SELECT COUNT(*) FROM events)+(SELECT COUNT(*) FROM event_observations)+(SELECT COUNT(*) FROM collection_pages)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("page partially persisted", count, err)
	}
	list.Items = list.Items[:1]
	if err := r.Page(t.Context(), first, 0, list); err != nil {
		t.Fatal(err)
	}
	// Simulate a crashed owner without sleeping. Its successfully committed page
	// remains, but the new run must fence every late write from the old owner.
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".collection_tasks SET lease_until=TIMESTAMPADD(SECOND,-1,UTC_TIMESTAMP(6)) WHERE task_id=?", task.ID)
	second := collectionClaim(t, pools[1].History(), task.ID)
	if first.RunID == second.RunID {
		t.Fatal("ownership not renewed")
	}
	if err := r.Page(t.Context(), first, 1, list); !errors.Is(err, history.ErrLeaseLost) {
		t.Fatal("stale page writer was not fenced", err)
	}
	if _, err := r.Finish(t.Context(), first, "complete", "", time.Hour); !errors.Is(err, history.ErrLeaseLost) {
		t.Fatal("stale completion was not fenced", err)
	}
	var status, failure string
	if err := pools[0].db.QueryRowContext(t.Context(), `SELECT status,failure FROM collection_runs WHERE run_id=?`, first.RunID).Scan(&status, &failure); err != nil || status != "partial" || failure != "interrupted" {
		t.Fatal("abandoned run lost coverage", status, failure, err)
	}
	list.Limited = true
	if err := pools[1].History().Page(t.Context(), second, 0, list); err != nil {
		t.Fatal(err)
	}
	if _, err := pools[1].History().Finish(t.Context(), second, "limited", "provider_result_cap", time.Hour); err != nil {
		t.Fatal(err)
	}
	var success sql.NullTime
	if err := pools[0].db.QueryRowContext(t.Context(), `SELECT last_success FROM collection_tasks WHERE task_id=?`, task.ID).Scan(&success); err != nil || success.Valid {
		t.Fatal("capped collection claimed complete freshness", err)
	}
}

func TestMariaDBSharedProviderBudget(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t)}
	var wg sync.WaitGroup
	var mu sync.Mutex
	successes := 0
	for i := range 12 {
		wg.Go(func() {
			err := pools[i%3].ProviderBudget("same-secret", 3).Wait(t.Context())
			if err == nil {
				mu.Lock()
				successes++
				mu.Unlock()
			} else if !errors.As(err, new(*discovery.UnavailableError)) {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if successes != 3 {
		t.Fatal("budget exceeded across replicas", successes)
	}
	if err := pools[0].Close(); err != nil {
		t.Fatal(err)
	}
	restarted := f.open(t)
	if err := restarted.ProviderBudget("same-secret", 3).Wait(t.Context()); !errors.As(err, new(*discovery.UnavailableError)) {
		t.Fatal("restart reset budget", err)
	}
	// Account for DB-time rollover without changing the provider limit or key.
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".provider_budgets SET window_start=TIMESTAMPADD(HOUR,-25,UTC_TIMESTAMP(6)),next_request=UTC_TIMESTAMP(6)")
	g := restarted.ProviderBudget("same-secret", 3)
	if err := g.Wait(t.Context()); err != nil {
		t.Fatal("budget did not roll over", err)
	}
	if err := g.Pause(t.Context(), 2*time.Hour); err != nil {
		t.Fatal(err)
	}
	var unavailable *discovery.UnavailableError
	if err := pools[1].ProviderBudget("same-secret", 3).Wait(t.Context()); !errors.As(err, &unavailable) || unavailable.RetryAfter < 119*time.Minute {
		t.Fatal("cooldown not shared", err)
	}
	if err := pools[1].ProviderBudget("different-key", 1).Wait(t.Context()); err != nil {
		t.Fatal("different keys share quota", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := g.Wait(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled request reserved quota", err)
	}
	if err := restarted.Close(); err != nil {
		t.Fatal(err)
	}
	if err := g.Wait(t.Context()); !errors.As(err, &unavailable) {
		t.Fatal("failed database allowed unbudgeted call", err)
	}
}

func TestMariaDBSparseAndStaleObservationFacts(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	r := p.History()
	task := history.NewTask(history.Scope{City: "Berlin", Country: "DE"}, "2026-10-08")
	if err := r.Schedule(t.Context(), []history.Task{task}); err != nil {
		t.Fatal(err)
	}
	claim := collectionClaim(t, r, task.ID)
	at := time.Now().UTC().Truncate(time.Microsecond)
	e := sampleEvent("sparse")
	for i := range 3 {
		item := e
		if i == 1 {
			item.Start = models.EventStart{}
			item.Venues = nil
			item.Status = ""
		}
		if err := r.Page(t.Context(), claim, i, models.EventList{Items: []models.Event{item}, Total: 1, Meta: models.Freshness{DataAsOf: at.Add(time.Duration(i) * time.Minute)}}); err != nil {
			t.Fatal(err)
		}
	}
	items, err := r.Observations(t.Context(), e.ID, time.Time{}, 100)
	if err != nil || len(items) != 3 || len(items[0].Changes) != 0 || len(items[1].Changes) != 0 || items[1].Event.Status != "" {
		t.Fatal("sparse data generated false changes or lost actual evidence", err)
	}
	// A stale DTO can still be bookmarked; it cannot advance observation history.
	detail := models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: at.Add(3 * time.Minute), Stale: true}}
	args, data, err := prepareSnapshot(detail)
	if err != nil {
		t.Fatal(err)
	}
	err = historyTransaction(t.Context(), p.db, p.config.QueryTimeout, "stale fixture", func(ctx context.Context, tx *sql.Tx) error { return upsertSnapshotTx(ctx, tx, detail, args, data) })
	if err != nil {
		t.Fatal(err)
	}
	items, err = r.Observations(t.Context(), e.ID, time.Time{}, 100)
	if err != nil || len(items) != 3 {
		t.Fatal("stale snapshot advanced history", err)
	}
}

func TestMariaDBIncompleteCollectionCannotClaimComplete(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	r := p.History()
	task := history.NewTask(history.Scope{City: "Berlin", Country: "DE"}, "2026-10-08")
	if err := r.Schedule(t.Context(), []history.Task{task}); err != nil {
		t.Fatal(err)
	}
	claim := collectionClaim(t, r, task.ID)
	for page := range 2 {
		// Pagination shifted: both pages return the same occurrence, not two
		// distinct events. Both good observations still belong to this run.
		list := models.EventList{Items: []models.Event{sampleEvent("shifted")}, Total: 2, Meta: models.Freshness{DataAsOf: time.Now().UTC()}}
		if err := r.Page(t.Context(), claim, page, list); err != nil {
			t.Fatal(err)
		}
	}
	outcome, err := r.Finish(t.Context(), claim, "complete", "", time.Hour)
	if err != nil || outcome.Status != "partial" || outcome.Failure != "result_set_changed" {
		t.Fatal("incomplete coverage claimed successful freshness", outcome, err)
	}
}

func TestMariaDBHistorySchema8UpgradePreservesActivity(t *testing.T) {
	f := newMaria(t)
	previous := fstest.MapFS{}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "00009_event_history.sql" {
			continue
		}
		data, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		previous[entry.Name()] = &fstest.MapFile{Data: data}
	}
	if err := migrateFS(t.Context(), f.migration, previous); err != nil {
		t.Fatal(err)
	}
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := &Pool{db: db, config: f.migration}
	owner, token := loginAccount(t, accounts.New(old.Accounts(), accountProviderFixture{}), "history-upgrade")
	detail := savedDetail("HistoryUpgrade")
	seedLegacySavedEvent(t, db, owner.ID, detail)
	execSQL(t, db, `INSERT INTO event_interests (account_id,event_id,visibility) VALUES (?,?,'private')`, owner.ID, detail.Item.ID)
	f.migrate(t)
	p := f.open(t)
	if who, err := accounts.New(p.Accounts(), nil).Authenticate(t.Context(), token, "session"); err != nil || who.Account.ID != owner.ID {
		t.Fatal("upgrade lost account/session", err)
	}
	if item, err := p.Saved().Get(t.Context(), owner.ID, detail.Item.ID); err != nil || !item.Meta.DataAsOf.Equal(detail.Meta.DataAsOf) || item.Event.Name != detail.Item.Name {
		t.Fatal("upgrade altered last good snapshot", err)
	}
	if item, err := p.Interests().Get(t.Context(), owner.ID, detail.Item.ID); err != nil || item.Visibility != "private" {
		t.Fatal("upgrade changed private interest", err)
	}
	if observations, err := p.History().Observations(t.Context(), detail.Item.ID, time.Time{}, 100); err != nil || len(observations) != 0 {
		t.Fatal("migration invented dated history", err)
	}
	if err := Migrate(t.Context(), f.migration); err != nil {
		t.Fatal("migration not repeatable", err)
	}
}

func TestMariaDBScheduledProviderCollectionEndToEnd(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		items := []map[string]any{}
		start, end := 0, 100
		if page == 1 {
			start, end = 100, 101
		}
		for i := start; i < end; i++ {
			items = append(items, map[string]any{"id": "scheduled-" + strconv.Itoa(i), "name": "Scheduled event", "dates": map[string]any{"status": map[string]any{"code": "onsale"}}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"page": map[string]int{"number": page, "totalElements": 101, "totalPages": 2}, "_embedded": map[string]any{"events": items}})
	}))
	defer server.Close()
	cache := kv.NewMemory()
	defer cache.Close()
	provider := discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "fixture", BaseURL: server.URL, DailyBudget: 3, Coordinator: p.ProviderBudget("fixture", 3)})
	collector := history.New(p.History(), provider, history.Config{Cities: []history.Scope{{City: "Berlin", Country: "DE"}}, Days: 1, Interval: time.Hour}, zerolog.Nop())
	if worked, err := collector.Once(t.Context()); err != nil || !worked {
		t.Fatal("scheduled refresh failed", worked, err)
	}
	var status string
	var count, used int
	if err := p.db.QueryRowContext(t.Context(), `SELECT (SELECT status FROM collection_runs LIMIT 1),(SELECT COUNT(*) FROM event_observations),(SELECT used FROM provider_budgets LIMIT 1)`).Scan(&status, &count, &used); err != nil || status != "complete" || count != 101 || used != 2 {
		t.Fatal("scheduled collection failed to persist complete coverage", status, count, used, err)
	}
	if _, err := provider.Event(t.Context(), "ticketmaster:scheduled-0"); err != nil {
		t.Fatal("scheduled page did not seed detail cache", err)
	}
	if calls.Load() != 2 {
		t.Fatal("interactive cache read used scheduled quota")
	}
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".collection_tasks SET next_refresh=UTC_TIMESTAMP(6)")
	if worked, err := collector.Once(t.Context()); err != nil || !worked {
		t.Fatal("provider failure not recorded", worked, err)
	}
	if stored, err := p.Saved().Snapshot(t.Context(), "ticketmaster:scheduled-0"); err != nil || stored.Item.Status != "onsale" {
		t.Fatal("failure erased good data", err)
	}
	if worked, err := collector.Once(t.Context()); err != nil || worked || calls.Load() != 3 {
		t.Fatal("scheduler ignored durable retry time", worked, err)
	}
}
