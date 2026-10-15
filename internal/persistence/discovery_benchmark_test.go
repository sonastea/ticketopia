package persistence

import (
	"fmt"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
)

type discoverBenchmarkCase struct {
	name  string
	query discovery.Query
	miss  bool
}

func discoverBenchmarkCases(t testing.TB, f queryFixture) []discoverBenchmarkCase {
	var cases []discoverBenchmarkCase
	for _, sort := range []string{"date_asc", "date_desc", "name_asc"} {
		q := benchmarkDiscoverQuery()
		q.Sort = sort
		cases = append(cases, discoverBenchmarkCase{sort + "/first", q, false}, discoverBenchmarkCase{sort + "/later", benchmarkLater(t, f.p.Discovery(), q, f.volume/80), false})
	}
	for _, name := range []string{"venue", "category", "city_category", "count_hit", "count_miss"} {
		q := benchmarkDiscoverQuery()
		switch name {
		case "venue":
			q.City, q.Country, q.VenueID = "", "", "ticketmaster:venue-rare"
		case "category":
			q.City, q.Country, q.CategoryID = "", "", "theatre"
		case "city_category":
			q.CategoryID = "theatre"
		}
		cases = append(cases, discoverBenchmarkCase{name, q, name == "count_miss"})
	}
	return cases
}

func BenchmarkMariaDBDiscover(b *testing.B) {
	for _, volume := range benchmarkVolumes(b) {
		b.Run(fmt.Sprint(volume), func(b *testing.B) {
			f := newQueryFixture(b, volume)
			for _, tc := range discoverBenchmarkCases(b, f) {
				b.Run(tc.name, func(b *testing.B) {
					r := f.p.Discovery()
					if _, err := r.searchAt(b.Context(), tc.query, 6*time.Hour, benchmarkAt); err != nil {
						b.Fatal(err)
					}
					b.ReportAllocs()
					for b.Loop() {
						if tc.miss {
							b.StopTimer()
							discoverCountsMu.Lock()
							delete(discoverCounts, r.countCacheKey(tc.query))
							discoverCountsMu.Unlock()
							b.StartTimer()
						}
						read, err := r.searchAt(b.Context(), tc.query, 6*time.Hour, benchmarkAt)
						if err != nil || len(read.List.Items) == 0 {
							b.Fatal("discover", err)
						}
					}
				})
			}
			for _, name := range []string{"scheduled", "overlapping", "artist", "venue"} {
				b.Run("coverage/"+name, func(b *testing.B) {
					q := benchmarkDiscoverQuery()
					if name != "scheduled" {
						q.City, q.Country = "Paris", "FR"
					}
					if name == "artist" {
						q.City, q.ArtistID = "", "ticketmaster:artist-0"
					}
					if name == "venue" {
						q.City, q.VenueID = "", "ticketmaster:venue-0"
					}
					b.ReportAllocs()
					for b.Loop() {
						tx, err := f.p.db.BeginTx(b.Context(), nil)
						if err != nil {
							b.Fatal(err)
						}
						_, _, _, err = coverageStampAtTx(b.Context(), tx, q, 6*time.Hour, benchmarkAt)
						_ = tx.Rollback()
						if err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
