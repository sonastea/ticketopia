package persistence

import (
	"slices"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

// A city search starts from that place's rows (searchFrom). It must return
// exactly what the place filter returned before: the same events, order and
// totals, across sorts and pages, for a minority city, a city spelled in
// another case, and events with venues in several cities.
func TestMariaDBDiscoverCitySearchMatchesPlaceFilter(t *testing.T) {
	f := newQueryFixture(t, 1000)
	r := f.p.Discovery()
	date := benchmarkAt.Format(time.DateOnly)
	for _, extra := range []struct {
		id     string
		venues [][2]string
	}{
		{"place-two-cities", [][2]string{{"Paris", "FR"}, {"Berlin", "DE"}}},
		{"place-same-city-twice", [][2]string{{"Paris", "FR"}, {"paris", "FR"}}},
	} {
		e := sampleEvent(extra.id)
		e.Start.LocalDate = &date
		e.Venues = nil
		for i, v := range extra.venues {
			e.Venues = append(e.Venues, models.Venue{ID: "ticketmaster:" + extra.id + string(rune('a'+i)), Name: "Hall", City: v[0], CountryCode: v[1]})
		}
		if err := r.Observe(t.Context(), models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: benchmarkAt}}); err != nil {
			t.Fatal(err)
		}
	}
	orders := map[string]string{
		"date_asc":  `e.local_date,e.local_time,e.name,e.event_id`,
		"date_desc": `e.local_date DESC,e.local_time DESC,e.name,e.event_id`,
		"name_asc":  `e.name,e.local_date,e.local_time,e.event_id`,
	}
	for _, place := range [][2]string{{"Paris", "FR"}, {"paris", "FR"}, {"Berlin", "DE"}} {
		for sort, order := range orders {
			q := benchmarkDiscoverQuery()
			q.City, q.Country, q.Sort = place[0], place[1], sort
			where, args := searchWhere(q, true)
			rows, err := f.p.db.QueryContext(t.Context(), `SELECT e.event_id FROM events e JOIN event_snapshots s ON s.event_id=e.event_id WHERE `+where+` ORDER BY `+order, args...)
			if err != nil {
				t.Fatal(err)
			}
			var want []string
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

			var got []string
			total := -1
			for page := 0; ; page++ {
				read, err := r.searchAt(t.Context(), q, 6*time.Hour, benchmarkAt)
				if err != nil {
					t.Fatal(place, sort, page, err)
				}
				if total < 0 {
					total = read.List.Total
				}
				for _, e := range read.List.Items {
					got = append(got, e.ID)
				}
				if read.List.NextCursor == nil {
					break
				}
				values := q.Values()
				values.Set("cursor", *read.List.NextCursor)
				if q, err = discovery.ParseQuery(values, benchmarkAt); err != nil {
					t.Fatal(err)
				}
			}
			if len(want) == 0 || !slices.Equal(got, want) || total != len(want) {
				t.Fatalf("%s %s: city search differs from the place filter: got %d events (total %d), want %d", place, sort, len(got), total, len(want))
			}
		}
	}
}
