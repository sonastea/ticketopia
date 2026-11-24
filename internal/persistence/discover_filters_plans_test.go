package persistence

import (
	"database/sql"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/sonastea/ticketopia/internal/models"
)

func TestMariaDBDiscoverFilterPlans(t *testing.T) {
	if os.Getenv("MARIADB_FILTER_PLANS") != "1" {
		t.Skip("set MARIADB_FILTER_PLANS=1 to compare Discover filter plans")
	}
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	for start := 0; start < 10000; start += 100 {
		items := make([]models.Event, 0, 100)
		for i := start; i < start+100; i++ {
			e := benchmarkEvent(i)
			// Spread every country across the whole date range independently of
			// benchmarkEvent's city distribution.
			date := benchmarkAt.AddDate(0, 0, (i/10)%30).Format("2006-01-02")
			e.Start.LocalDate = &date
			if i%10 == 0 {
				v := e.Venues[0]
				v.ID, v.City = "ticketmaster:second-city", "Hamburg"
				e.Venues = append(e.Venues, v)
			}
			items = append(items, e)
		}
		tx, err := p.db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := upsertPageSnapshotsTx(t.Context(), tx, models.EventList{Items: items, Meta: models.Freshness{DataAsOf: benchmarkAt}}); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	execSQL(t, db, `ANALYZE TABLE events,event_snapshots,event_search_places,event_search_facets`)
	for _, tc := range []struct {
		name, country, artist string
		days                  int
	}{
		{"country_rare", "FR", "", 0},
		{"country_common", "DE", "", 0},
		{"artist_rare", "", "ticketmaster:artist-rare", 0},
		{"artist_common", "", "ticketmaster:artist-0", 0},
		{"date_de_01", "DE", "", 1},
		{"date_de_03", "DE", "", 3},
		{"date_de_07", "DE", "", 7},
		{"date_de_14", "DE", "", 14},
		{"date_de_30", "DE", "", 30},
		{"date_fr_01", "FR", "", 1},
		{"date_fr_07", "FR", "", 7},
		{"date_fr_30", "FR", "", 30},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := benchmarkDiscoverQuery()
			q.City, q.Country, q.ArtistID = "", tc.country, tc.artist
			if tc.days > 0 {
				q.StartDate = benchmarkAt.Format("2006-01-02")
				q.EndDate = benchmarkAt.AddDate(0, 0, tc.days-1).Format("2006-01-02")
			}
			for _, kind := range []string{"page", "count"} {
				t.Run(kind, func(t *testing.T) {
					query, args := discoverPageQuery(q, nil)
					if kind == "count" {
						query, args = discoverCountQuery(q)
					}
					from, _ := searchFrom(q)
					probe := q
					probe.Country, probe.ArtistID = "", ""
					where, prefix := searchWhere(probe, false)
					const join = ` STRAIGHT_JOIN events e ON e.event_id=filter_ids.event_id STRAIGHT_JOIN event_snapshots s ON s.event_id=e.event_id WHERE `
					var alternative string
					var bindings []any
					if tc.days > 0 {
						where, prefix = searchWhere(q, true)
						alternative = ` FROM events e FORCE INDEX (search_date) STRAIGHT_JOIN event_snapshots s ON s.event_id=e.event_id WHERE `
					} else if tc.country != "" {
						alternative = ` FROM (SELECT DISTINCT event_id FROM event_search_places WHERE country=?) filter_ids` + join
						bindings = []any{tc.country}
					} else {
						alternative = ` FROM event_search_facets filter_ids` + join + `filter_ids.kind='artist' AND filter_ids.value=? AND `
						bindings = []any{tc.artist}
					}
					candidate := strings.Replace(query, from, alternative+where, 1)
					bindings = append(bindings, prefix...)
					if kind == "page" {
						bindings = append(bindings, q.Limit+1)
					}
					rows := func(statement string, params []any) []string {
						t.Helper()
						var result []string
						err := queryRowsTx(t.Context(), db, statement, params, func(row *sql.Rows) error {
							var value string
							var asOf, unconfirmed any
							var err error
							if kind == "count" {
								err = row.Scan(&value)
							} else {
								err = row.Scan(&value, &asOf, &unconfirmed)
							}
							result = append(result, value)
							return err
						})
						if err != nil {
							t.Fatal(err)
						}
						return result
					}
					if want, got := rows(query, args), rows(candidate, bindings); len(want) == 0 || !slices.Equal(want, got) {
						t.Fatalf("results differ (current %d, candidate %d)", len(want), len(got))
					}
					var times [2][]float64
					var plans [2]json.RawMessage
					for i := range 12 {
						for k := range 2 {
							j := (i + k) % 2
							statement, params := query, args
							if j == 1 {
								statement, params = candidate, bindings
							}
							var raw []byte
							if err := db.QueryRowContext(t.Context(), `ANALYZE FORMAT=JSON `+statement, params...).Scan(&raw); err != nil {
								t.Fatal(err)
							}
							var result struct {
								Block struct {
									MS float64 `json:"r_total_time_ms"`
								} `json:"query_block"`
							}
							if err := json.Unmarshal(raw, &result); err != nil {
								t.Fatal(err)
							}
							if i > 0 {
								times[j] = append(times[j], result.Block.MS)
							}
							plans[j] = raw
						}
					}
					for i, samples := range times {
						slices.Sort(samples)
						t.Logf("variant %d median %.3f ms\n%s", i, samples[len(samples)/2], plans[i])
					}
				})
			}
		})
	}
}
