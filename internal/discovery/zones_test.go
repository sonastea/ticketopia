package discovery

import (
	"fmt"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

func TestCityTodayLearnsZoneFromCityResults(t *testing.T) {
	s := &Service{}
	// 01:30 UTC on Saturday is still Friday evening in Los Angeles.
	now := time.Date(2026, 10, 24, 1, 30, 0, 0, time.UTC)
	la := "America/Los_Angeles"
	events := []models.Event{{}, {Start: models.EventStart{Timezone: &la}}}
	q := Query{City: "Los Angeles", Country: "US"}
	if _, ok := s.CityToday(q, now); ok {
		t.Fatal("zone known before any results")
	}
	s.learnCityZone(q, events)
	if today, ok := s.CityToday(Query{City: "los angeles", Country: "US", Keyword: "jazz"}, now); !ok || today != "2026-10-23" {
		t.Fatalf("city today = %q, %v", today, ok)
	}
	if _, ok := s.CityToday(Query{City: "Los Angeles", Country: "GB"}, now); ok {
		t.Error("zone shared across countries")
	}
	// Artist and venue searches are not limited to the city's zone.
	artist := Query{City: "Boston", Country: "US", ArtistID: "ticketmaster:A1"}
	s.learnCityZone(artist, events)
	if _, ok := s.CityToday(Query{City: "Boston", Country: "US"}, now); ok {
		t.Error("artist search taught a city zone")
	}
	if _, ok := s.CityToday(artist, now); ok {
		t.Error("artist search used a city zone")
	}
	// Results without a loadable zone teach nothing.
	bad := "Invalid/Zone"
	s.learnCityZone(Query{City: "Nowhere"}, []models.Event{{Start: models.EventStart{Timezone: &bad}}})
	if _, ok := s.CityToday(Query{City: "Nowhere"}, now); ok {
		t.Error("invalid zone remembered")
	}
}

func TestCityZoneMemoIsBounded(t *testing.T) {
	s := &Service{}
	utc := "UTC"
	events := []models.Event{{Start: models.EventStart{Timezone: &utc}}}
	for i := range cityZoneMemo + 1 {
		s.learnCityZone(Query{City: fmt.Sprintf("City %d", i)}, events)
	}
	if len(s.zones.known) != cityZoneMemo || len(s.zones.order) != cityZoneMemo {
		t.Fatalf("memo size = %d/%d", len(s.zones.known), len(s.zones.order))
	}
	if _, ok := s.CityToday(Query{City: "City 0"}, time.Now()); ok {
		t.Error("oldest city was not evicted")
	}
}
