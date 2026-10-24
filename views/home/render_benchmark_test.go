package home

import (
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

func benchmarkRenderEvent(i int) models.Event {
	date, clock, zone := time.Date(2026, 11, 1+i%20, 0, 0, 0, 0, time.UTC).Format(time.DateOnly), "19:30:00", "Europe/Berlin"
	low, high := "39.50", "129.00"
	e := models.Event{
		ID: fmt.Sprintf("ticketmaster:Bench%03d", i), Name: fmt.Sprintf("Artist %d live in concert with very special guests", i), Status: "onsale",
		Source:          models.Source{Provider: "ticketmaster", ID: fmt.Sprintf("Bench%03d", i), URL: "https://www.ticketmaster.de/event/bench"},
		Start:           models.EventStart{LocalDate: &date, LocalTime: &clock, Timezone: &zone},
		PriceRanges:     []models.PriceRange{{Type: "standard", Currency: "EUR", Min: &low, Max: &high}},
		Artists:         []models.Artist{{ID: fmt.Sprintf("ticketmaster:artist-%d", i), Name: fmt.Sprintf("Artist %d", i)}},
		Venues:          []models.Venue{{ID: "ticketmaster:venue", Place: models.Place{Name: "Uber Arena", City: "Berlin", CountryCode: "DE"}}},
		Classifications: []models.Classification{{Primary: true, Segment: &models.NamedID{ID: "music", Name: "Music"}, Genre: &models.NamedID{ID: "rock", Name: "Rock"}}},
		Tint:            "#5a3d8c",
	}
	for w := range 10 {
		e.Images = append(e.Images, models.Image{URL: fmt.Sprintf("https://s1.ticketm.net/dam/a/%03d/%03d_RETINA_PORTRAIT_%d.jpg", w, i, w), Width: 640 + 64*w, Height: 360 + 36*w, Ratio: "16_9"})
	}
	return e
}

func benchmarkRenderPages() map[string]templ.Component {
	search := SearchPage{Filters: discovery.Query{City: "Berlin", Country: "DE", StartDate: "2026-11-01", EndDate: "2026-11-30", Limit: 20, Sort: "date_asc"}, ReturnURL: "/?city=Berlin&country=DE"}
	asOf := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	live := HappeningNowPage{Enabled: true, ReturnURL: "/happening-now", Snapshot: activity.Snapshot{AsOf: asOf}, Events: map[string]models.Event{}}
	for i := range 20 {
		e := benchmarkRenderEvent(i)
		search.Events.Items = append(search.Events.Items, e)
		name := fmt.Sprintf("Fan%d", i)
		live.Snapshot.Items = append(live.Snapshot.Items, activity.Item{EventID: e.ID, EventTitle: e.Name, City: "Berlin", ActorFirstName: &name, RecentRecommendations: 1 + i%4, LatestRecommendedAt: asOf.Add(-time.Duration(i) * time.Minute)})
		live.Events[e.ID] = e
	}
	return map[string]templ.Component{"discover": Index(search), "happening_now": HappeningNow(live)}
}

// Parallel runs share the class-merge and zone caches across goroutines, so
// contention there shows as parallel ns/op failing to fall with -cpu.
func BenchmarkRender(b *testing.B) {
	for name, page := range benchmarkRenderPages() {
		b.Run(name+"/serial", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := page.Render(b.Context(), io.Discard); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(name+"/parallel", func(b *testing.B) {
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if err := page.Render(b.Context(), io.Discard); err != nil {
						b.Error(err)
						return
					}
				}
			})
		})
	}
}
