package discovery

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

// benchEvent approximates a normalized Ticketmaster event: about ten artwork
// variants, presales, prices, classifications, a venue and free-text notes.
func benchEvent(i int) models.Event {
	at := time.Date(2026, 11, 1, 20, 0, 0, 0, time.UTC).Add(time.Duration(i) * time.Hour)
	date, clock, zone := at.Format(time.DateOnly), "20:00:00", "America/New_York"
	low, high, fees := "45.50", "189.00", true
	place := models.Place{Name: "Example Hall", Address: "1 Main St", City: "Boston", State: "MA", Country: "United States Of America", CountryCode: "US", PostalCode: "02110", Latitude: "42.36", Longitude: "-71.05"}
	e := models.Event{
		ID: fmt.Sprintf("ticketmaster:show-%d", i), Name: fmt.Sprintf("Example Artist World Tour %d", i),
		Source: models.Source{Provider: "ticketmaster", ID: fmt.Sprintf("show-%d", i), URL: "https://www.ticketmaster.com/event/example"},
		Start:  models.EventStart{DateTime: &at, LocalDate: &date, LocalTime: &clock, Timezone: &zone},
		Status: "onsale", PublicSale: models.PublicSale{Start: &at, End: &at},
		Presales:        []models.Presale{{Name: "Artist presale", Description: strings.Repeat("Presale details. ", 8), URL: "https://example.com/presale", Start: &at, End: &at}},
		PriceRanges:     []models.PriceRange{{Type: "standard", Currency: "USD", Min: &low, Max: &high, FeesIncluded: &fees}},
		Artists:         []models.Artist{{ID: "ticketmaster:artist-1", Name: "Example Artist", Source: models.Source{Provider: "ticketmaster", ID: "artist-1", URL: "https://www.ticketmaster.com/artist/1"}}},
		Venues:          []models.Venue{{ID: "ticketmaster:venue-1", Source: models.Source{Provider: "ticketmaster", ID: "venue-1"}, Timezone: zone, Place: place}},
		Place:           &place,
		Classifications: []models.Classification{{Primary: true, Segment: &models.NamedID{ID: "KZFzniwnSyZfZ7v7nJ", Name: "Music"}, Genre: &models.NamedID{ID: "KnvZfZ7vAeA", Name: "Rock"}, Subgenre: &models.NamedID{ID: "KZazBEonSMnZfZ7v6F1", Name: "Pop"}}},
		Info:            strings.Repeat("Venue information and bag policy. ", 10),
		PleaseNote:      strings.Repeat("Please note the entry requirements. ", 6),
		Tint:            "#335577",
	}
	for j := range 10 {
		e.Images = append(e.Images, models.Image{URL: fmt.Sprintf("https://s1.ticketm.net/dam/a/%d/example_%d_RETINA_PORTRAIT_16_9.jpg", i, j), Width: 640 * (j%3 + 1), Height: 360 * (j%3 + 1), Ratio: "16_9"})
	}
	return e
}

func benchCategories() []models.Category {
	items := make([]models.Category, 0, 6)
	for i := range 6 {
		category := models.Category{NamedID: models.NamedID{ID: fmt.Sprintf("segment-%d", i), Name: fmt.Sprintf("Segment %d", i)}}
		for j := range 30 {
			genre := models.Genre{NamedID: models.NamedID{ID: fmt.Sprintf("genre-%d-%d", i, j), Name: fmt.Sprintf("Genre %d", j)}}
			for k := range 4 {
				genre.Subgenres = append(genre.Subgenres, models.NamedID{ID: fmt.Sprintf("sub-%d-%d-%d", i, j, k), Name: fmt.Sprintf("Subgenre %d", k)})
			}
			category.Genres = append(category.Genres, genre)
		}
		items = append(items, category)
	}
	return items
}

func benchCachedHit[T any](b *testing.B, cache kv.Store, data T) {
	b.Cleanup(func() { _ = cache.Close() })
	s := New(b.Context(), cache, zerolog.Nop(), Config{})
	const key = "ticketmaster:v1:bench"
	now := s.now()
	writeRecord(b.Context(), s, key, cacheRecord[T]{Data: data, FetchedAt: now, FreshUntil: now.Add(time.Hour), StaleUntil: now.Add(24 * time.Hour)})
	fetch := func(context.Context) (T, error) {
		b.Fatal("warm cache hit fetched from the provider")
		var zero T
		return zero, nil
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := cached(b.Context(), s, key, eventPolicy, fetch); err != nil {
			b.Fatal(err)
		}
	}
}

// Warm hits are the steady state for Genres, Categories, catalog search and
// provider-only Discover. The copying store models a network backend, whose
// fresh bytes per Get must be compared in full before reusing a record; its
// transport cost is excluded.
func BenchmarkCachedHit(b *testing.B) {
	page := eventPage{Total: 20, TotalPages: 1}
	for i := range 20 {
		page.Items = append(page.Items, benchEvent(i))
	}
	stores := []struct {
		name string
		new  func() kv.Store
	}{{"memory", kv.NewMemory}, {"copying", func() kv.Store { return &copyingStore{} }}}
	for _, store := range stores {
		b.Run("events_page/"+store.name, func(b *testing.B) { benchCachedHit(b, store.new(), page) })
		b.Run("event_detail/"+store.name, func(b *testing.B) { benchCachedHit(b, store.new(), benchEvent(0)) })
		b.Run("categories/"+store.name, func(b *testing.B) { benchCachedHit(b, store.new(), benchCategories()) })
	}
}
