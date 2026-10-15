package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/discussions"
)

// Recheck noisy end-to-end samples without reseeding between index layouts.
// No timing or optimizer choice is an assertion. -benchtime controls each sample.
func TestMariaDBQueryComparison(t *testing.T) {
	if os.Getenv("MARIADB_BENCH_COMPARE") != "1" {
		t.Skip("set MARIADB_BENCH_COMPARE=1")
	}
	for _, volume := range benchmarkVolumes(t) {
		t.Run(fmt.Sprint(volume), func(t *testing.T) {
			// Paired schema-14/schema-15 reads must not inherit a candidate event index.
			t.Setenv("MARIADB_BENCH_CONFIG", "current")
			f := newQueryFixture(t, volume)
			type sample struct {
				Configuration, Case string
				Round, Events       int
				NS, B, Allocs       int64
			}
			var samples []sample
			for round := range 5 {
				configs := []string{"baseline", "current"}
				if round%2 != 0 {
					configs = []string{"current", "baseline"}
				}
				for _, config := range configs {
					execSQL(t, f.db, `ALTER TABLE discussion_posts DROP INDEX IF EXISTS discussion_recent`)
					execSQL(t, f.db, `ALTER TABLE discovery_scopes DROP INDEX discovery_coverage, ADD INDEX discovery_coverage (city,country,artist_id,venue_id,start_date,end_date)`)
					f.configuration(t, config)
					f.statistics(t)
					measure := func(name string, run func(*testing.B)) {
						failed := false
						result := testing.Benchmark(func(b *testing.B) { defer func() { failed = failed || b.Failed() }(); run(b) })
						if failed {
							t.Fatal("paired benchmark failed", name)
						}
						samples = append(samples, sample{config, name, round, volume, result.NsPerOp(), result.AllocedBytesPerOp(), result.AllocsPerOp()})
						t.Log(config, name, result.String(), result.MemString())
					}
					for _, tc := range communityBenchmarkQueries() {
						measure("discussions/"+tc.name, func(b *testing.B) {
							q := discussions.Query{City: tc.q.City, Country: tc.q.Country, CategoryID: tc.q.CategoryID, Snapshot: benchmarkAt, Limit: 20}
							b.ReportAllocs()
							for b.Loop() {
								if _, _, err := f.p.Discussions().List(b.Context(), "", "", "", q); err != nil {
									b.Fatal(err)
								}
							}
						})
					}
					for _, name := range []string{"category", "count_miss", "coverage_artist", "coverage_venue"} {
						measure("discover/"+name, func(b *testing.B) {
							q := benchmarkDiscoverQuery()
							r := f.p.Discovery()
							if name == "category" {
								q.City, q.Country, q.CategoryID = "", "", "theatre"
							}
							if name == "coverage_artist" {
								q.City, q.Country, q.ArtistID = "", "FR", "ticketmaster:artist-0"
							}
							if name == "coverage_venue" {
								q.City, q.Country, q.VenueID = "", "FR", "ticketmaster:venue-0"
							}
							b.ReportAllocs()
							for b.Loop() {
								if name == "count_miss" {
									b.StopTimer()
									discoverCountsMu.Lock()
									delete(discoverCounts, r.countCacheKey(q))
									discoverCountsMu.Unlock()
									b.StartTimer()
								}
								if name == "coverage_artist" || name == "coverage_venue" {
									tx, err := f.p.db.BeginTx(b.Context(), nil)
									if err != nil {
										b.Fatal(err)
									}
									_, _, _, err = coverageStampAtTx(b.Context(), tx, q, 6*time.Hour, benchmarkAt)
									_ = tx.Rollback()
									if err != nil {
										b.Fatal(err)
									}
								} else if _, err := r.searchAt(b.Context(), q, 6*time.Hour, benchmarkAt); err != nil {
									b.Fatal(err)
								}
							}
						})
					}
				}
			}
			dir := os.Getenv("MARIADB_BENCH_PLAN_DIR")
			if dir == "" {
				dir = t.TempDir()
			}
			if err := os.MkdirAll(dir, 0755); err != nil {
				t.Fatal(err)
			}
			data, err := json.MarshalIndent(samples, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("%d-paired-benchmarks.json", volume)), data, 0644); err != nil {
				t.Fatal(err)
			}
		})
	}
}
