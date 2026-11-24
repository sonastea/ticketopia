package persistence

import (
	"encoding/json"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/models"
)

func TestMariaDBCollectionKnownEventsUpgrade(t *testing.T) {
	f := newMaria(t)
	ctx := t.Context()
	old := fstest.MapFS{}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() >= "00027" {
			continue
		}
		data, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		old[e.Name()] = &fstest.MapFile{Data: data}
	}
	if err := migrateFS(ctx, f.migration, old); err != nil {
		t.Fatal(err)
	}
	db, err := connect(ctx, f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	task := history.NewTask(history.Scope{City: "Berlin", Country: "DE"}, "2026-10-08")
	other := history.NewTask(history.Scope{City: "Hamburg", Country: "DE"}, "2026-10-08")
	h := &HistoryRepository{db: db, timeout: f.migration.QueryTimeout}
	if err := h.Schedule(ctx, []history.Task{task, other}); err != nil {
		t.Fatal(err)
	}
	items := make([]models.Event, 100)
	for i := range items {
		items[i] = benchmarkEvent(i)
	}
	legacyOnly, unrelated, partial := sampleEvent("old-interrupted"), sampleEvent("other-task"), sampleEvent("new-partial")
	r := &DiscoveryRepository{db: db, timeout: f.migration.QueryTimeout}
	for _, e := range append(slices.Clone(items), legacyOnly, unrelated) {
		if err := r.Observe(ctx, models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: benchmarkAt}}); err != nil {
			t.Fatal(err)
		}
	}
	execSQL(t, db, `INSERT INTO collection_runs (run_id,task_id,started_at,status)
 SELECT CONCAT('bench-',seq),?,TIMESTAMPADD(HOUR,-seq,?), 'complete' FROM seq_1_to_1000`, task.ID, benchmarkAt)
	execSQL(t, db, `INSERT INTO collection_run_events SELECT r.run_id,o.event_id,o.observed_at
 FROM collection_runs r CROSS JOIN event_observations o WHERE r.task_id=? AND o.event_id LIKE 'ticketmaster:bench-%'`, task.ID)
	execSQL(t, db, `INSERT INTO collection_runs (run_id,task_id,started_at,status,failure)
 VALUES ('interrupted',?,?,'partial','interrupted'),('other',?,?,'complete','')`, task.ID, benchmarkAt, other.ID, benchmarkAt)
	execSQL(t, db, `INSERT INTO collection_run_events SELECT IF(event_id=?,'interrupted','other'),event_id,observed_at
 FROM event_observations WHERE event_id IN (?,?)`, legacyOnly.ID, legacyOnly.ID, unrelated.ID)

	f.migrate(t)
	h = f.open(t).History()
	claim := collectionClaim(t, h, task.ID)
	page := models.EventList{Items: []models.Event{partial}, Total: 2, Meta: models.Freshness{DataAsOf: benchmarkAt}}
	if err := h.Page(ctx, claim, 0, page); err != nil {
		t.Fatal(err)
	}
	if out, err := h.Finish(ctx, claim, "complete", "", time.Hour); err != nil || out.Status != "partial" {
		t.Fatalf("incomplete evidence: %+v %v", out, err)
	}
	expectQueued := func(want []string) {
		t.Helper()
		rows, err := db.QueryContext(ctx, `SELECT event_id FROM event_detail_tasks ORDER BY event_id`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			got = append(got, id)
		}
		if err := rows.Err(); err != nil || !slices.Equal(got, want) {
			t.Fatalf("queued %v, want %v (err %v)", got, want, err)
		}
	}
	expectQueued(nil)
	execSQL(t, db, `UPDATE collection_tasks SET next_refresh=UTC_TIMESTAMP(6) WHERE task_id=?`, task.ID)
	claim = collectionClaim(t, h, task.ID)
	page.Items, page.Total = items, len(items)
	if err := h.Page(ctx, claim, 0, page); err != nil {
		t.Fatal(err)
	}

	legacy := `SELECT DISTINCT old.event_id FROM collection_run_events old JOIN collection_runs r ON r.run_id=old.run_id
 WHERE r.task_id=? AND old.run_id<>? AND NOT EXISTS (SELECT 1 FROM collection_run_events current WHERE current.run_id=? AND current.event_id=old.event_id)`
	for _, tc := range []struct {
		name, query string
		args        []any
	}{
		{"run receipts", legacy, []any{task.ID, claim.RunID, claim.RunID}},
		{"known events", collectionOmissions, []any{task.ID, claim.RunID}},
	} {
		var times []float64
		for i := range 6 {
			var raw []byte
			if err := db.QueryRowContext(ctx, `ANALYZE FORMAT=JSON `+tc.query, tc.args...).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var plan struct {
				Block struct {
					Milliseconds float64 `json:"r_total_time_ms"`
				} `json:"query_block"`
			}
			if err := json.Unmarshal(raw, &plan); err != nil {
				t.Fatal(err)
			}
			if i > 0 {
				times = append(times, plan.Block.Milliseconds)
			}
		}
		slices.Sort(times)
		t.Logf("%s: %.3f ms median (100,000 historical receipts, 102 known events)", tc.name, times[len(times)/2])
	}
	if out, err := h.Finish(ctx, claim, "complete", "", time.Hour); err != nil || out.Status != "complete" {
		t.Fatalf("complete run: %+v %v", out, err)
	}
	expectQueued([]string{partial.ID, legacyOnly.ID})
}
