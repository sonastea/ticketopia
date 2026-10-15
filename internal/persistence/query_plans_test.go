package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/discussions"
)

// Plans are evidence, never assertions about optimizer choices. Both commands
// execute with the same parameters used by the repository and benchmark cases.
func TestMariaDBQueryPlans(t *testing.T) {
	if os.Getenv("MARIADB_BENCH_PLANS") != "1" {
		t.Skip("set MARIADB_BENCH_PLANS=1")
	}
	for _, volume := range benchmarkVolumes(t) {
		t.Run(fmt.Sprint(volume), func(t *testing.T) {
			f := newQueryFixture(t, volume)
			if os.Getenv("MARIADB_BENCH_COMPARE") == "1" {
				for _, config := range []string{"baseline", "current", "date_time", "date_time_desc", "name_prefix", "coverage_range", "coverage_order"} {
					t.Run(config, func(t *testing.T) {
						t.Setenv("MARIADB_BENCH_CONFIG", config)
						execSQL(t, f.db, `ALTER TABLE discussion_posts DROP INDEX IF EXISTS discussion_recent`)
						execSQL(t, f.db, `ALTER TABLE events DROP INDEX search_date, ADD INDEX search_date (local_date,name(120),event_id)`)
						execSQL(t, f.db, `ALTER TABLE events DROP INDEX search_name, ADD INDEX search_name (name(120),event_id)`)
						execSQL(t, f.db, `ALTER TABLE discovery_scopes DROP INDEX discovery_coverage, ADD INDEX discovery_coverage (city,country,artist_id,venue_id,start_date,end_date)`)
						f.configuration(t, config)
						f.statistics(t)
						captureQueryPlans(t, f)
					})
				}
			} else {
				captureQueryPlans(t, f)
			}
		})
	}
}

func captureQueryPlans(t *testing.T, f queryFixture) {
	volume := f.volume
	var version string
	if err := f.db.QueryRowContext(t.Context(), `SELECT VERSION()`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	var serverCache string
	if err := f.db.QueryRowContext(t.Context(), `SELECT CONCAT('query_cache_type=',@@query_cache_type,', query_cache_size=',@@query_cache_size,', buffer_pool_bytes=',@@innodb_buffer_pool_size)`).Scan(&serverCache); err != nil {
		t.Fatal(err)
	}
	var indexBytes int64
	if err := f.db.QueryRowContext(t.Context(), `SELECT COALESCE(SUM(INDEX_LENGTH),0) FROM information_schema.TABLES WHERE TABLE_SCHEMA=?`, f.runtime.Database).Scan(&indexBytes); err != nil {
		t.Fatal(err)
	}
	indexSizes := map[string]int64{}
	rows, err := f.admin.QueryContext(t.Context(), `SELECT table_name,index_name,stat_value*@@innodb_page_size FROM mysql.innodb_index_stats WHERE database_name=? AND stat_name='size'`, f.runtime.Database)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var table, index string
		var bytes int64
		if err := rows.Scan(&table, &index, &bytes); err != nil {
			t.Fatal(err)
		}
		indexSizes[table+"."+index] = bytes
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	dir := os.Getenv("MARIADB_BENCH_PLAN_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	capture := func(name, query string, args []any) {
		t.Helper()
		plans := map[string]json.RawMessage{}
		var samples []float64
		// First execution warms this SELECT; no claim of a cold buffer pool.
		for i, prefix := range []string{"EXPLAIN", "ANALYZE", "ANALYZE", "ANALYZE", "ANALYZE", "ANALYZE", "ANALYZE"} {
			var data []byte
			if err := f.db.QueryRowContext(t.Context(), prefix+" FORMAT=JSON "+query, args...).Scan(&data); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			plans[prefix] = json.RawMessage(data)
			if i >= 2 {
				var timing struct {
					Block struct {
						MS float64 `json:"r_total_time_ms"`
					} `json:"query_block"`
				}
				if err := json.Unmarshal(data, &timing); err != nil {
					t.Fatal(err)
				}
				samples = append(samples, timing.Block.MS)
			}
		}
		encodedSamples, err := json.Marshal(samples)
		if err != nil {
			t.Fatal(err)
		}
		plans["execution_ms_samples"] = encodedSamples
		record := struct {
			Version       string                     `json:"mariadb_version"`
			Dataset       int                        `json:"events"`
			Configuration string                     `json:"configuration"`
			Reference     time.Time                  `json:"reference_time"`
			Cache         string                     `json:"cache_state"`
			IndexBytes    int64                      `json:"schema_secondary_index_bytes"`
			IndexSizes    map[string]int64           `json:"index_bytes"`
			SQL           string                     `json:"sql"`
			Parameters    []any                      `json:"parameters"`
			Plans         map[string]json.RawMessage `json:"plans"`
		}{version, volume, os.Getenv("MARIADB_BENCH_CONFIG"), benchmarkAt, "warm after fixture and one ANALYZE execution; direct SQL bypasses Go count cache; " + serverCache, indexBytes, indexSizes, query, args, plans}
		data, err := json.MarshalIndent(record, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, fmt.Sprintf("%d-%s-%s.json", volume, os.Getenv("MARIADB_BENCH_CONFIG"), strings.ReplaceAll(name, "/", "-")))
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
		t.Log(path)
	}
	for _, tc := range discoverBenchmarkCases(t, f) {
		query, args := discoverPageQuery(tc.query, tc.query.Cursor)
		capture("discover/"+tc.name, query, args)
		// Experimental predicate rewrite, derived from the actual SELECT.
		// Keep it diagnostic-only until repeated benchmarks justify adoption.
		if c := tc.query.Cursor; c != nil {
			candidate := query
			switch tc.query.Sort {
			case "date_asc":
				candidate = strings.Replace(query, `(e.local_date,COALESCE(e.local_time,''),e.name,e.event_id)>(?,?,?,?)`, `(e.local_date>? OR (e.local_date=? AND (COALESCE(e.local_time,'')>? OR (COALESCE(e.local_time,'')=? AND (e.name>? OR (e.name=? AND e.event_id>?))))))`, 1)
				args = append(append([]any(nil), args[:len(args)-5]...), c.Date, c.Date, c.Time, c.Time, c.Name, c.Name, c.ID, tc.query.Limit+1)
			case "name_asc":
				candidate = strings.Replace(query, `(e.name,e.local_date,COALESCE(e.local_time,''),e.event_id)>(?,?,?,?)`, `(e.name>? OR (e.name=? AND (e.local_date>? OR (e.local_date=? AND (COALESCE(e.local_time,'')>? OR (COALESCE(e.local_time,'')=? AND e.event_id>?))))))`, 1)
				args = append(append([]any(nil), args[:len(args)-5]...), c.Name, c.Name, c.Date, c.Date, c.Time, c.Time, c.ID, tc.query.Limit+1)
			case "date_desc":
				candidate = strings.Replace(query, `(e.name,e.event_id)>(?,?)`, `(e.name>? OR (e.name=? AND e.event_id>?))`, 1)
				args = append(append([]any(nil), args[:len(args)-3]...), c.Name, c.Name, c.ID, tc.query.Limit+1)
			}
			if candidate == query {
				t.Fatal("repository cursor predicate changed; update the diagnostic rewrite")
			}
			capture("expanded-cursor/"+tc.name, candidate, args)
		}
		if tc.name == "count_miss" {
			query, args = discoverCountQuery(tc.query)
			capture("discover/count", query, args)
		}
	}
	q := benchmarkDiscoverQuery()
	query, args := scheduledCoverageQuery(q, 6*time.Hour, benchmarkAt)
	capture("coverage/scheduled", query, args)
	q.City, q.Country = "Paris", "FR"
	for _, scope := range []string{"broad", "artist", "venue"} {
		q.City = "Paris"
		q.ArtistID, q.VenueID = "", ""
		if scope == "artist" {
			q.City = ""
			q.ArtistID = "ticketmaster:artist-0"
		}
		if scope == "venue" {
			q.City = ""
			q.VenueID = "ticketmaster:venue-0"
		}
		query, args = onDemandCoverageQuery(q, 6*time.Hour, benchmarkAt)
		capture("coverage/"+scope, query, args)
	}
	// Separate the two overlap predicates from last-success ordering: neither
	// a date-range key nor a success-order key can optimize every boundary.
	q.City, q.ArtistID, q.VenueID = "", "ticketmaster:artist-0", ""
	q.StartDate = benchmarkAt.AddDate(0, 0, -90).Format(time.DateOnly)
	query, args = onDemandCoverageQuery(q, 6*time.Hour, benchmarkAt)
	capture("coverage/narrow-start", query, args)
	q.StartDate = benchmarkAt.Format(time.DateOnly)
	q.EndDate = benchmarkAt.AddDate(0, 0, 80).Format(time.DateOnly)
	query, args = onDemandCoverageQuery(q, 6*time.Hour, benchmarkAt)
	capture("coverage/narrow-end", query, args)
	for _, tc := range communityBenchmarkQueries() {
		query, args := recommendationCommunityQuery(tc.q)
		capture("recommendations/"+tc.name, query, args)
		groups, err := f.p.Recommendations().Community(t.Context(), tc.q)
		if err != nil {
			t.Fatal(err)
		}
		var ids []any
		for _, g := range groups {
			ids = append(ids, g.Event.ID)
		}
		if len(ids) > 0 {
			query, args = recommendationPreviewQuery(ids)
			capture("recommendation-previews/"+tc.name, query, args)
		}
		dq := discussions.Query{City: tc.q.City, Country: tc.q.Country, CategoryID: tc.q.CategoryID, Limit: 20, Snapshot: benchmarkAt}
		query, args, count, countArgs := discussionQueries("", "", "", dq)
		capture("discussions/"+tc.name, query, args)
		capture("discussion-count/"+tc.name, count, countArgs)
	}
	for _, root := range []string{"", benchmarkPost(0, 0)} {
		name := "event"
		if root != "" {
			name = "thread"
		}
		query, args, count, countArgs := discussionQueries("", benchmarkEvent(0).ID, root, discussions.Query{Limit: 20, Snapshot: benchmarkAt})
		capture("discussions/"+name, query, args)
		capture("discussion-count/"+name, count, countArgs)
	}
}
