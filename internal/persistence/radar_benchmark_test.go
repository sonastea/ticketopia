package persistence

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/radar"
)

// Run against the disposable MariaDB fixture, with -benchmem. Setup and cursor
// traversal are excluded; later measures a real cursor halfway through the set.
func BenchmarkMariaDBRadar(b *testing.B) {
	for _, volume := range []int{1000, 10000} {
		b.Run(fmt.Sprint(volume), func(b *testing.B) {
			f := newMaria(b)
			f.migrate(b)
			p := f.open(b)
			auth := accounts.New(p.Accounts(), accountProviderFixture{})
			owner, _ := loginAccount(b, auth, "radar-benchmark")
			if _, err := auth.UpdatePreferences(b.Context(), owner.ID, accounts.PreferencesPatch{City: pointer("Berlin"), Country: pointer("DE"), CategoryIDs: pointer([]string{"music"})}); err != nil {
				b.Fatal(err)
			}
			at := time.Now().UTC()
			for i := range volume {
				e := sampleEvent(fmt.Sprintf("bench-%06d", i))
				date := at.AddDate(0, 0, 1+i%89).Format(time.DateOnly)
				e.Start.LocalDate, e.Start.DateTime = &date, nil
				e.Venues[0].City, e.Venues[0].CountryCode = "Berlin", "DE"
				e.Classifications = []models.Classification{{Segment: &models.NamedID{ID: "music", Name: "Music"}}}
				e.Name = "Live music with a realistic event snapshot " + fmt.Sprint(i)
				e.Info = strings.Repeat("Event information and admission details. ", 40)
				if err := p.Discovery().Observe(b.Context(), models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: at}}); err != nil {
					b.Fatal(err)
				}
			}
			s := radar.New(p.Radar(), nil, 6*time.Hour)
			q := url.Values{"limit": {"20"}}
			for range volume / 40 {
				page, err := s.List(b.Context(), owner.ID, q)
				if err != nil || page.NextCursor == nil {
					b.Fatal("cursor setup", err)
				}
				q.Set("cursor", *page.NextCursor)
			}
			for _, page := range []struct {
				name  string
				query url.Values
			}{{"first", url.Values{"limit": {"20"}}}, {"later", q}} {
				b.Run(page.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						list, err := s.List(b.Context(), owner.ID, page.query)
						if err != nil || len(list.Items) != 20 {
							b.Fatal(len(list.Items), err)
						}
					}
				})
			}
		})
	}
}
