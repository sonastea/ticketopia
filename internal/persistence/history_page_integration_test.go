package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/models"
)

// Batched collection pages must leave exactly the rows that sequential
// sequentialSnapshotTx calls leave, including duplicates, backdated pages,
// sparse updates and statements split by the payload bound.
func TestMariaDBCollectionPageMatchesSequentialWrites(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	g := newMaria(t)
	g.migrate(t)
	q := g.open(t)
	task := history.NewTask(history.Scope{City: "Batch", Country: "DE"}, "2026-10-08")
	if err := p.History().Schedule(t.Context(), []history.Task{task}); err != nil {
		t.Fatal(err)
	}
	claim := collectionClaim(t, p.History(), task.ID)
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	a, b := sampleEvent("z-batch"), sampleEvent("a-batch")
	a.Artists = []models.Artist{{ID: "ticketmaster:shared", Name: "First"}}
	a.Info = strings.Repeat("chunked snapshot ", 35000)
	for i := range 130 {
		a.Artists = append(a.Artists, models.Artist{ID: fmt.Sprintf("ticketmaster:extra-%03d", i), Name: "Extra"})
	}
	b.Artists = []models.Artist{{ID: "ticketmaster:shared", Name: "Second"}}
	duplicate := a
	duplicate.Name = "Discard duplicate"
	sparse := a
	sparse.Status, sparse.Start, sparse.Venues = "", models.EventStart{}, nil
	cancelled := a
	cancelled.Status = "cancelled"
	// Backdated facts are observed but must not replace newer metadata.
	backdated := b
	backdated.Name = "Backdated name"
	backdated.Venues = []models.Venue{{ID: "ticketmaster:old-venue", Place: models.Place{Name: "Old venue", City: "Hamburg", CountryCode: "de"}}}
	pages := []models.EventList{
		{Items: []models.Event{a, b, duplicate}, Meta: models.Freshness{DataAsOf: at}},
		{Items: []models.Event{backdated, a}, Meta: models.Freshness{DataAsOf: at.Add(-time.Hour)}},
		{Items: []models.Event{duplicate, b}, Meta: models.Freshness{DataAsOf: at}},
		{Items: []models.Event{sparse, b}, Meta: models.Freshness{DataAsOf: at.Add(time.Hour)}},
		{Items: []models.Event{cancelled, b}, Meta: models.Freshness{DataAsOf: at.Add(2 * time.Hour)}},
	}
	read := func(db *sql.DB, query string, args ...any) string { return readRows(t, db, query, args...) }
	tables := snapshotTables
	// Compare after every page: later newer pages would mask earlier drift.
	for n, list := range pages {
		if err := p.History().Page(t.Context(), claim, n, list); err != nil {
			t.Fatal(err)
		}
		err := historyTransaction(t.Context(), q.db, time.Minute, "sequential page", func(ctx context.Context, tx *sql.Tx) error {
			for _, e := range list.Items {
				d := models.EventDetail{Item: e, Meta: list.Meta}
				args, data, err := prepareSnapshot(d)
				if err != nil {
					return err
				}
				if err := sequentialSnapshotTx(ctx, tx, d, args, data); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, query := range tables {
			if got, want := read(p.db, query), read(q.db, query); got != want {
				t.Fatalf("page %d: %s\n got %.2000s\nwant %.2000s", n, query, got, want)
			}
		}
	}
	// Each run receives an event once, at its first observation in that run.
	got := read(p.db, `SELECT event_id,observed_at FROM collection_run_events WHERE run_id=? ORDER BY event_id`, claim.RunID)
	want, _ := json.Marshal([][]any{{b.ID, at}, {a.ID, at}})
	if got != string(want) {
		t.Fatalf("run receipts: got %s, want %s", got, want)
	}
}

func TestMariaDBCollectionPageStatementsAreBounded(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	task := history.NewTask(history.Scope{City: "Count", Country: "DE"}, "2026-10-08")
	if err := p.History().Schedule(t.Context(), []history.Task{task}); err != nil {
		t.Fatal(err)
	}
	claim := collectionClaim(t, p.History(), task.ID)
	list := models.EventList{Total: 100, Meta: models.Freshness{DataAsOf: benchmarkAt}}
	for i := range 100 {
		list.Items = append(list.Items, benchmarkEvent(i))
	}
	count := statementCounter(t, p)
	if err := p.History().Page(t.Context(), claim, 0, list); err != nil {
		t.Fatal(err)
	}
	// About 30 set-based statements, plus payload-bounded splits of the
	// snapshot and observation inserts; never a statement per event.
	if got := count(); got > 40 {
		t.Fatalf("100-event page ran %d statements, want at most 40", got)
	}
	var receipts int
	if err := p.db.QueryRow(`SELECT COUNT(*) FROM collection_run_events WHERE run_id=?`, claim.RunID).Scan(&receipts); err != nil {
		t.Fatal(err)
	}
	if receipts != 100 {
		t.Fatalf("got %d receipts, want 100", receipts)
	}
}

// Overlapping pages lock in event-ID order, whatever their page order, and
// never overwrite a newer single-event write.
func TestMariaDBCollectionPagesConcurrentWriters(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p, q := f.open(t), f.open(t)
	items := []models.Event{}
	for i := range 12 {
		e := sampleEvent(fmt.Sprintf("batch-%02d", i))
		e.Artists = []models.Artist{{ID: fmt.Sprintf("ticketmaster:ref-%02d", 11-i), Name: "Shared"}}
		items = append(items, e)
	}
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for round := range 5 {
		tasks := []history.Task{
			history.NewTask(history.Scope{City: fmt.Sprintf("Batch A %d", round), Country: "DE"}, "2026-10-08"),
			history.NewTask(history.Scope{City: fmt.Sprintf("Batch B %d", round), Country: "DE"}, "2026-10-08"),
		}
		if err := p.History().Schedule(t.Context(), tasks); err != nil {
			t.Fatal(err)
		}
		claims := []history.Claim{collectionClaim(t, p.History(), tasks[0].ID), collectionClaim(t, q.History(), tasks[1].ID)}
		pageAt := at.Add(time.Duration(round) * time.Hour)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i, pool := range []*Pool{p, q} {
			wg.Go(func() {
				<-start
				events := slices.Clone(items)
				if i == 1 {
					slices.Reverse(events)
				}
				if err := pool.History().Page(t.Context(), claims[i], 0, models.EventList{Items: events, Meta: models.Freshness{DataAsOf: pageAt}}); err != nil {
					t.Error(err)
				}
			})
		}
		wg.Go(func() {
			<-start
			for _, e := range items {
				e.Status = "cancelled"
				d := models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: pageAt.Add(30 * time.Minute)}}
				args, data, err := prepareSnapshot(d)
				if err != nil {
					t.Error(err)
					return
				}
				if err := historyTransaction(t.Context(), p.db, time.Minute, "single writer", func(ctx context.Context, tx *sql.Tx) error { return upsertSnapshotTx(ctx, tx, d, args, data) }); err != nil {
					t.Error(err)
					return
				}
			}
		})
		close(start)
		wg.Wait()
		for _, e := range items {
			got, err := p.Saved().Snapshot(t.Context(), e.ID)
			if err != nil || got.Item.Status != "cancelled" {
				t.Fatalf("round %d: newer single writer lost for %s: %v", round, e.ID, err)
			}
		}
	}
}

// readRows encodes a query's rows for exact cross-database comparison.
func readRows(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	result := [][]any{}
	for rows.Next() {
		v, ptr := make([]any, len(cols)), make([]any, len(cols))
		for i := range v {
			ptr[i] = &v[i]
		}
		if err := rows.Scan(ptr...); err != nil {
			t.Fatal(err)
		}
		for i, x := range v {
			if b, ok := x.([]byte); ok {
				v[i] = string(b)
			}
		}
		result = append(result, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	return string(encoded)
}

// snapshotTables are every table a snapshot write touches, in stable order.
var snapshotTables = []string{
	`SELECT ` + eventColumns + ` FROM events ORDER BY event_id`,
	`SELECT provider,source_id,event_id FROM event_providers ORDER BY provider,source_id`,
	`SELECT event_id,snapshot,data_as_of FROM event_snapshots ORDER BY event_id`,
	`SELECT event_id,observed_at,snapshot,changes FROM event_observations ORDER BY event_id,observed_at`,
	`SELECT event_id,first_seen,last_seen,last_changed,snapshot FROM event_history_state ORDER BY event_id`,
	`SELECT event_id,city,country FROM event_search_places ORDER BY event_id,city,country`,
	`SELECT event_id,kind,value FROM event_search_facets ORDER BY event_id,kind,value`,
	`SELECT artist_id,snapshot,first_seen,last_seen FROM artists ORDER BY artist_id`,
	`SELECT provider,source_id,artist_id FROM artist_providers ORDER BY provider,source_id`,
	`SELECT venue_id,snapshot,first_seen,last_seen FROM venues ORDER BY venue_id`,
	`SELECT provider,source_id,venue_id FROM venue_providers ORDER BY provider,source_id`,
}

// Single-event writes share the set-based writer and must leave exactly the
// rows sequentialSnapshotTx leaves, including stale reads (snapshot without an
// observation), equal-timestamp retries, backdated facts and repeated artists.
func TestMariaDBSnapshotMatchesSequentialWrite(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	g := newMaria(t)
	g.migrate(t)
	q := g.open(t)
	at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	e := sampleEvent("single")
	e.Artists = []models.Artist{{ID: "ticketmaster:b", Name: "B"}, {ID: "ticketmaster:a", Name: "A"}, {ID: "ticketmaster:b", Name: "Repeated B"}}
	renamed := e
	renamed.Name = "Renamed"
	renamed.Venues = []models.Venue{{ID: "ticketmaster:new-venue", Place: models.Place{Name: "New venue", City: "Köln", CountryCode: "de"}}}
	cancelled := renamed
	cancelled.Status = "cancelled"
	details := []models.EventDetail{
		{Item: e, Meta: models.Freshness{DataAsOf: at}},
		{Item: e, Meta: models.Freshness{DataAsOf: at}},
		{Item: renamed, Meta: models.Freshness{DataAsOf: at.Add(time.Hour)}},
		{Item: e, Meta: models.Freshness{DataAsOf: at.Add(-time.Hour)}},
		{Item: cancelled, Meta: models.Freshness{DataAsOf: at.Add(2 * time.Hour), Stale: true}},
		{Item: renamed, Meta: models.Freshness{DataAsOf: at.Add(3 * time.Hour)}},
	}
	for n, d := range details {
		args, data, err := prepareSnapshot(d)
		if err != nil {
			t.Fatal(err)
		}
		for db, write := range map[*sql.DB]func(context.Context, *sql.Tx, models.EventDetail, []any, []byte) error{p.db: upsertSnapshotTx, q.db: sequentialSnapshotTx} {
			if err := historyTransaction(t.Context(), db, time.Minute, "snapshot", func(ctx context.Context, tx *sql.Tx) error { return write(ctx, tx, d, args, data) }); err != nil {
				t.Fatal(err)
			}
		}
		for _, query := range snapshotTables {
			if got, want := readRows(t, p.db, query), readRows(t, q.db, query); got != want {
				t.Fatalf("write %d: %s\n got %.2000s\nwant %.2000s", n, query, got, want)
			}
		}
	}
}
