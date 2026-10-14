package persistence

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

// Run against the disposable MariaDB fixture, with -benchmem. Setup and cursor
// traversal are excluded; later measures a real cursor halfway through the set.
func BenchmarkMariaDBDiscover(b *testing.B) {
	for _, volume := range []int{1000, 10000} {
		b.Run(fmt.Sprint(volume), func(b *testing.B) {
			f := newMaria(b)
			f.migrate(b)
			p := f.open(b)
			at := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
			startDate := at.Format(time.DateOnly)
			endDate := at.AddDate(0, 0, 89).Format(time.DateOnly)
			r := p.History()
			tasks := make([]history.Task, 0, 90)
			for day := range 90 {
				tasks = append(tasks, history.NewTask(history.Scope{City: "Berlin", Country: "DE"}, at.AddDate(0, 0, day).Format(time.DateOnly)))
			}
			if err := r.Schedule(b.Context(), tasks); err != nil {
				b.Fatal(err)
			}
			for day, task := range tasks {
				claim, err := r.Claim(b.Context(), []string{task.ID})
				if err != nil {
					b.Fatal(err)
				}
				date := at.AddDate(0, 0, day).Format(time.DateOnly)
				var items []models.Event
				for i := day; i < volume; i += 90 {
					e := sampleEvent(fmt.Sprintf("bench-%06d", i))
					clock := fmt.Sprintf("%02d:%02d:00", (17+(i%6))%24, (i*15)%60)
					e.Start.LocalDate, e.Start.LocalTime, e.Start.DateTime = &date, &clock, nil
					if i%10 == 0 {
						e.Start.LocalTime, e.Start.TimeTBA = nil, true
					}
					e.Venues[0].City, e.Venues[0].CountryCode = "Berlin", "DE"
					e.Classifications = []models.Classification{{Segment: &models.NamedID{ID: "music", Name: "Music"}}}
					e.Name = fmt.Sprintf("Event %04d live in concert %06d", (i*37)%1000, i)
					e.Info = strings.Repeat("Event information and admission details. ", 40)
					items = append(items, e)
				}
				total := len(items)
				for page := 0; page == 0 || page*100 < total; page++ {
					var chunk []models.Event
					if page*100 < total {
						chunk = items[page*100 : min((page+1)*100, total)]
					}
					if err := r.Page(b.Context(), claim, page, models.EventList{Items: chunk, Total: total, Meta: models.Freshness{DataAsOf: at}}); err != nil {
						b.Fatal(err)
					}
				}
				if _, err := r.Finish(b.Context(), claim, "complete", "", 6*time.Hour); err != nil {
					b.Fatal(err)
				}
			}
			cache := kv.NewMemory()
			b.Cleanup(func() { _ = cache.Close() })
			s := discovery.New(b.Context(), cache, zerolog.Nop(), discovery.Config{Catalog: p.Discovery(), FreshFor: 6 * time.Hour})
			b.Cleanup(s.WaitRefreshes)
			for _, sortOrder := range []string{"date_asc", "date_desc", "name_asc"} {
				b.Run(sortOrder, func(b *testing.B) {
					base := url.Values{
						"city":       {"Berlin"},
						"country":    {"DE"},
						"start_date": {startDate},
						"end_date":   {endDate},
						"limit":      {"20"},
						"sort":       {sortOrder},
					}
					later := url.Values{
						"city":       {"Berlin"},
						"country":    {"DE"},
						"start_date": {startDate},
						"end_date":   {endDate},
						"limit":      {"20"},
						"sort":       {sortOrder},
					}
					for range volume / 40 {
						q, err := discovery.ParseQuery(later, at)
						if err != nil {
							b.Fatal(err)
						}
						page, err := s.Events(b.Context(), q)
						if err != nil || page.NextCursor == nil {
							b.Fatal("cursor setup", err)
						}
						later.Set("cursor", *page.NextCursor)
					}
					for _, tc := range []struct {
						name  string
						query url.Values
					}{{"first", base}, {"later", later}} {
						b.Run(tc.name, func(b *testing.B) {
							b.ReportAllocs()
							for b.Loop() {
								q, err := discovery.ParseQuery(tc.query, at)
								if err != nil {
									b.Fatal(err)
								}
								list, err := s.Events(b.Context(), q)
								if err != nil || len(list.Items) != 20 {
									b.Fatal(len(list.Items), err)
								}
							}
						})
					}
				})
			}
		})
	}
}
