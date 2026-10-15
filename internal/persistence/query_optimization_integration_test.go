package persistence

import (
	"database/sql"
	"fmt"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

func discussionIndexColumns(t *testing.T, db *sql.DB, name string) string {
	t.Helper()
	var columns sql.NullString
	if err := db.QueryRowContext(t.Context(), `SELECT GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='discussion_posts' AND INDEX_NAME=?`, name).Scan(&columns); err != nil {
		t.Fatal(err)
	}
	return columns.String
}

func TestMariaDBQueryIndexMigration(t *testing.T) {
	for _, upgrade := range []bool{false, true} {
		t.Run(fmt.Sprint("upgrade=", upgrade), func(t *testing.T) {
			f := newMaria(t)
			db, err := connect(t.Context(), f.migration)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			if upgrade {
				previous := fstest.MapFS{}
				entries, err := migrations.ReadDir("migrations")
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if entry.Name() < "00015" {
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
				for _, name := range []string{"discussion_replies", "discussion_recent"} {
					if got := discussionIndexColumns(t, db, name); got != "root_id,created_at,post_id" {
						t.Fatal(name, got)
					}
				}
				r := &DiscoveryRepository{db: db, timeout: f.migration.QueryTimeout}
				if err := r.Observe(t.Context(), models.EventDetail{Item: benchmarkEvent(0), Meta: models.Freshness{DataAsOf: benchmarkAt}}); err != nil {
					t.Fatal(err)
				}
				(queryFixture{db: db, volume: 1}).seedCommunity(t)
				q := benchmarkDiscoverQuery()
				execSQL(t, db, `INSERT INTO discovery_scopes (scope_id,city,country,start_date,end_date,artist_id,venue_id,last_success,data_as_of,status) VALUES (?,?,?,?,?,'','',?,?,'complete')`, q.ScopeID(), q.City, q.Country, q.StartDate, q.EndDate, benchmarkAt, benchmarkAt)
			}
			f.migrate(t)
			if err := Migrate(t.Context(), f.migration); err != nil {
				t.Fatal(err)
			}
			p := f.open(t)
			if err := p.Ready(t.Context()); err != nil {
				t.Fatal(err)
			}
			if got := discussionIndexColumns(t, db, "discussion_recent"); got != "" {
				t.Fatal("redundant index", got)
			}
			if got := discussionIndexColumns(t, db, "discussion_replies"); got != "root_id,created_at,post_id" {
				t.Fatal("retained index", got)
			}
			var coverageColumns string
			if err := db.QueryRowContext(t.Context(), `SELECT GROUP_CONCAT(COLUMN_NAME ORDER BY SEQ_IN_INDEX) FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='discovery_scopes' AND INDEX_NAME='discovery_coverage'`).Scan(&coverageColumns); err != nil || coverageColumns != "city,country,artist_id,venue_id,start_date,end_date" {
				t.Fatal("coverage index", coverageColumns, err)
			}
			var count int
			if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='discussion_posts' AND CONSTRAINT_NAME='discussion_root_fk' AND COLUMN_NAME='root_id' AND REFERENCED_COLUMN_NAME='post_id'`).Scan(&count); err != nil || count != 1 {
				t.Fatal("root FK", count, err)
			}
			if upgrade {
				tx, err := db.BeginTx(t.Context(), nil)
				if err != nil {
					t.Fatal(err)
				}
				meta, refresh, _, err := coverageStampAtTx(t.Context(), tx, benchmarkDiscoverQuery(), 6*time.Hour, benchmarkAt)
				_ = tx.Rollback()
				if err != nil || refresh || meta.Coverage.Status != "complete" {
					t.Fatal("preserved coverage", meta, refresh, err)
				}
				if err := db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM discussion_posts WHERE root_id=?`, benchmarkPost(0, 0)).Scan(&count); err != nil || count != 64 {
					t.Fatal("preserved replies", count, err)
				}
				if _, err := db.ExecContext(t.Context(), `DELETE FROM discussion_posts WHERE post_id=?`, benchmarkPost(0, 0)); err == nil {
					t.Fatal("root foreign key not enforced")
				}
			}
		})
	}
}

func TestMariaDBBenchmarkCursorTraversal(t *testing.T) {
	f := newQueryFixture(t, 1000)
	for _, sort := range []string{"date_asc", "date_desc", "name_asc"} {
		t.Run(sort, func(t *testing.T) {
			q := benchmarkDiscoverQuery()
			q.City, q.Country, q.Sort = "", "", sort
			order := "local_date,local_time,name,event_id"
			if sort == "date_desc" {
				order = "local_date DESC,local_time DESC,name,event_id"
			}
			if sort == "name_asc" {
				order = "name,local_date,local_time,event_id"
			}
			rows, err := f.db.QueryContext(t.Context(), `SELECT event_id FROM events ORDER BY `+order)
			if err != nil {
				t.Fatal(err)
			}
			var want, got []string
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					t.Fatal(err)
				}
				want = append(want, id)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			rows.Close()
			for {
				read, err := f.p.Discovery().searchAt(t.Context(), q, 6*time.Hour, benchmarkAt)
				if err != nil {
					t.Fatal(err)
				}
				for _, e := range read.List.Items {
					got = append(got, e.ID)
				}
				if len(got) > len(want) {
					t.Fatal("duplicate traversal")
				}
				if read.List.NextCursor == nil {
					break
				}
				v := q.Values()
				v.Set("cursor", *read.List.NextCursor)
				q, err = discovery.ParseQuery(v, benchmarkAt)
				if err != nil {
					t.Fatal(err)
				}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("traversal differs from full collation-aware ordering: got %d want %d", len(got), len(want))
			}
		})
	}
}
