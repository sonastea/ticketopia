package persistence

import (
	"database/sql"
	"testing"
	"testing/fstest"

	"github.com/sonastea/ticketopia/internal/models"
)

func indexColumns(t *testing.T, db *sql.DB, table, name string) string {
	t.Helper()
	var columns sql.NullString
	if err := db.QueryRowContext(t.Context(), `SELECT GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=?`, table, name).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	return columns.String
}

// Schema 26 adds read indexes to a populated schema-25 database: existing
// observations get has_changes matching the JSON filter it replaces.
func TestMariaDBSchema26QueryIndexesUpgrade(t *testing.T) {
	f := newMaria(t)
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	previous := fstest.MapFS{}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() < "00026" {
			data, err := migrations.ReadFile("migrations/" + entry.Name())
			if err != nil {
				t.Fatal(err)
			}
			previous[entry.Name()] = &fstest.MapFile{Data: data}
		}
	}
	if err := migrateFS(t.Context(), f.migration, previous); err != nil {
		t.Fatal(err)
	}
	r := &DiscoveryRepository{db: db, timeout: f.migration.QueryTimeout}
	e := benchmarkEvent(0)
	if err := r.Observe(t.Context(), models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: benchmarkAt}}); err != nil {
		t.Fatal(err)
	}
	execSQL(t, db, `INSERT INTO event_observations (event_id,observed_at,snapshot,changes) SELECT event_id,TIMESTAMPADD(HOUR,1,observed_at),snapshot,'["name"]' FROM event_observations WHERE event_id=?`, e.ID)
	execSQL(t, db, `INSERT INTO event_observations (event_id,observed_at,snapshot,changes) SELECT event_id,TIMESTAMPADD(HOUR,2,observed_at),snapshot,'[]' FROM event_observations WHERE event_id=? ORDER BY observed_at LIMIT 1`, e.ID)

	f.migrate(t) // through schema 26, with runtime grants
	if err := f.open(t).Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, want := range []struct{ table, index, columns string }{
		{"discussion_posts", "discussion_visible_roots", "event_id,root_id,removed_at,hidden_at,created_at,post_id"},
		{"event_observations", "observation_changes", "event_id,has_changes,observed_at"},
		{"event_detail_tasks", "detail_due", "next_refresh,event_id,lease_until"},
	} {
		if got := indexColumns(t, db, want.table, want.index); got != want.columns {
			t.Fatalf("%s.%s = %q, want %q", want.table, want.index, got, want.columns)
		}
	}
	var total, mismatched int
	if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*),SUM(has_changes<>(JSON_LENGTH(changes)>0)) FROM event_observations`).Scan(&total, &mismatched); err != nil {
		t.Fatal(err)
	}
	if total < 3 || mismatched != 0 {
		t.Fatalf("has_changes disagrees with the JSON filter: %d of %d rows", mismatched, total)
	}
}
