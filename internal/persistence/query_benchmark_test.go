package persistence

import (
	"database/sql"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/models"
)

var benchmarkAt = time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)

func benchmarkVolumes(t testing.TB) []int {
	t.Helper()
	if value := os.Getenv("MARIADB_BENCH_EVENTS"); value != "" {
		n, err := strconv.Atoi(value)
		if err != nil || (n != 1000 && n != 10000 && n != 100000) {
			t.Fatal("MARIADB_BENCH_EVENTS must be 1000, 10000, or 100000")
		}
		return []int{n}
	}
	return []int{1000, 10000}
}

func benchmarkEvent(i int) models.Event {
	e := sampleEvent(fmt.Sprintf("bench-%06d", i))
	date, clock := benchmarkAt.AddDate(0, 0, i%30).Format(time.DateOnly), "19:00:00"
	e.Start.LocalDate, e.Start.LocalTime, e.Start.DateTime = &date, &clock, nil
	if i%7 == 0 {
		e.Start.LocalTime, e.Start.TimeTBA = nil, true
	}
	city, country := "Berlin", "DE"
	if i%10 == 8 {
		city = "Hamburg"
	}
	if i%10 == 9 {
		city, country = "Paris", "FR"
	}
	e.Venues[0].City, e.Venues[0].CountryCode = city, country
	e.Venues[0].ID = fmt.Sprintf("ticketmaster:venue-%d", i%20)
	if i%101 == 0 {
		e.Venues[0].ID = "ticketmaster:venue-rare"
	}
	e.Artists = []models.Artist{{ID: fmt.Sprintf("ticketmaster:artist-%d", i%8), Name: fmt.Sprintf("Artist %d", i%8)}}
	if i%103 == 0 {
		e.Artists[0].ID = "ticketmaster:artist-rare"
	}
	category := "music"
	if i%17 == 0 {
		category = "theatre"
	}
	e.Classifications = []models.Classification{{Segment: &models.NamedID{ID: category, Name: category}}}
	// Full-name ties and prefix-only ties both cross page boundaries.
	e.Name = strings.Repeat("Shared concert name ", 7) + fmt.Sprintf("%03d", i%11)
	if i%2 == 0 {
		e.Name = "s" + e.Name[1:]
	}
	e.Info = strings.Repeat("Event information and admission details. ", 40)
	return e
}

type queryFixture struct {
	mariaFixture
	p      *Pool
	db     *sql.DB // migration credentials, exclusively for fixture/diagnostic DDL
	volume int
}

func newQueryFixture(t testing.TB, volume int) queryFixture {
	t.Helper()
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	x := queryFixture{f, p, db, volume}
	// Real ingestion path, bounded transactions; fixture setup is never timed.
	for start := 0; start < volume; start += 100 {
		tx, err := p.db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		for i := start; i < min(start+100, volume); i++ {
			d := models.EventDetail{Item: benchmarkEvent(i), Meta: models.Freshness{DataAsOf: benchmarkAt}}
			args, data, err := prepareSnapshot(d)
			if err == nil {
				err = upsertSnapshotTx(t.Context(), tx, d, args, data)
			}
			if err != nil {
				_ = tx.Rollback()
				t.Fatal(err)
			}
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	var tasks []history.Task
	for day := range 30 {
		tasks = append(tasks, history.NewTask(history.Scope{City: "Berlin", Country: "DE"}, benchmarkAt.AddDate(0, 0, day).Format(time.DateOnly)))
	}
	if err := p.History().Schedule(t.Context(), tasks); err != nil {
		t.Fatal(err)
	}
	execSQL(t, db, `UPDATE collection_tasks SET last_success=?,last_attempt=?`, benchmarkAt, benchmarkAt)
	// Accumulated scopes include enclosing, partially overlapping and
	// artist/venue-specific requests; cardinality grows with the catalog.
	for i := range volume / 10 {
		q := benchmarkDiscoverQuery()
		q.City, q.Country = "Paris", "FR"
		q.StartDate = benchmarkAt.AddDate(0, 0, -(i % 120)).Format(time.DateOnly)
		q.EndDate = benchmarkAt.AddDate(0, 0, 10+i%83).Format(time.DateOnly)
		if i%3 == 0 {
			q.City = ""
			q.ArtistID = fmt.Sprintf("ticketmaster:artist-%d", i%8)
		}
		if i%5 == 0 {
			q.City = ""
			q.VenueID = fmt.Sprintf("ticketmaster:venue-%d", i%20)
		}
		collectedAt := benchmarkAt.Add(-time.Duration(i%48) * time.Hour)
		execSQL(t, db, `INSERT IGNORE INTO discovery_scopes (scope_id,city,country,start_date,end_date,artist_id,venue_id,last_success,data_as_of,status,next_refresh) VALUES (?,?,?,?,?,?,?,?,?,'complete',?)`, q.ScopeID(), q.City, q.Country, q.StartDate, q.EndDate, q.ArtistID, q.VenueID, collectedAt, collectedAt, benchmarkAt.Add(time.Hour))
	}
	x.seedCommunity(t)
	x.configuration(t, os.Getenv("MARIADB_BENCH_CONFIG"))
	x.statistics(t)
	return x
}

// Candidate DDL is confined to the disposable schema. It is not a migration.
func (f queryFixture) configuration(t testing.TB, name string) {
	t.Helper()
	switch name {
	case "", "current":
	case "baseline":
		execSQL(t, f.db, `ALTER TABLE discussion_posts ADD INDEX discussion_recent (root_id,created_at,post_id)`)
		execSQL(t, f.db, `ALTER TABLE discovery_scopes DROP INDEX discovery_coverage, ADD INDEX discovery_coverage (city,country,start_date,end_date)`)
	case "date_time":
		execSQL(t, f.db, `ALTER TABLE events DROP INDEX search_date, ADD INDEX search_date (local_date,local_time,name(120),event_id)`)
	case "date_time_desc":
		execSQL(t, f.db, `ALTER TABLE events DROP INDEX search_date, ADD INDEX search_date (local_date DESC,local_time DESC,name(120),event_id)`)
	case "name_prefix":
		execSQL(t, f.db, `ALTER TABLE events DROP INDEX search_name, ADD INDEX search_name (name(512),local_date,local_time,event_id)`)
	case "coverage_range":
		execSQL(t, f.db, `ALTER TABLE discovery_scopes DROP INDEX discovery_coverage, ADD INDEX discovery_coverage (city,country,artist_id,venue_id,start_date,end_date)`)
	case "coverage_order":
		execSQL(t, f.db, `ALTER TABLE discovery_scopes DROP INDEX discovery_coverage, ADD INDEX discovery_coverage (city,country,artist_id,venue_id,last_success,start_date,end_date)`)
	default:
		t.Fatal("unknown MARIADB_BENCH_CONFIG", name)
	}
}

func (f queryFixture) statistics(t testing.TB) {
	t.Helper()
	for _, table := range []string{"events", "event_snapshots", "event_search_places", "event_search_facets", "collection_tasks", "discovery_scopes", "event_recommendations", "recommendation_feed_events", "discussion_posts", "accounts", "post_helpful"} {
		rows, err := f.db.QueryContext(t.Context(), "ANALYZE TABLE "+table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var a, b, c, d string
			if err := rows.Scan(&a, &b, &c, &d); err != nil {
				t.Fatal(err)
			}
			if c == "error" {
				t.Fatal(d)
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
}

func benchmarkDiscoverQuery() discovery.Query {
	return discovery.Query{City: "Berlin", Country: "DE", StartDate: "2026-10-08", EndDate: "2026-11-06", Limit: 20, Sort: "date_asc"}
}

func benchmarkLater(t testing.TB, r *DiscoveryRepository, q discovery.Query, pages int) discovery.Query {
	t.Helper()
	for range pages {
		read, err := r.searchAt(t.Context(), q, 6*time.Hour, benchmarkAt)
		if err != nil || read.List.NextCursor == nil {
			t.Fatal("cursor setup", err)
		}
		values := q.Values()
		values.Set("cursor", *read.List.NextCursor)
		q, err = discovery.ParseQuery(values, benchmarkAt)
		if err != nil {
			t.Fatal(err)
		}
	}
	if q.Cursor != nil && q.Cursor.Name == "" {
		if err := r.db.QueryRowContext(t.Context(), `SELECT name FROM events WHERE event_id=?`, q.Cursor.ID).Scan(&q.Cursor.Name); err != nil {
			t.Fatal(err)
		}
	}
	return q
}
