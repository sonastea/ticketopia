package discovery

import (
	"strings"
	"sync"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

// cityZoneMemo bounds the remembered city time zones. Each entry is a short
// city/country key and a zone name, learned from that city's own results.
const cityZoneMemo = 1024

type cityZones struct {
	mu    sync.Mutex
	known map[string]*time.Location
	order []string
}

// EventsToday returns now in the time zone of the first event, or its first
// venue, that names a loadable zone. Ticketmaster dates are venue-local, so
// this is the calendar day a city's results are filtered by.
func EventsToday(events []models.Event, now time.Time) (time.Time, bool) {
	if loc, ok := eventsZone(events); ok {
		return now.In(loc), true
	}
	return now.UTC(), false
}

func eventsZone(events []models.Event) (*time.Location, bool) {
	for _, event := range events {
		zone := ""
		if event.Start.Timezone != nil {
			zone = *event.Start.Timezone
		} else if len(event.Venues) > 0 {
			zone = event.Venues[0].Timezone
		}
		if zone == "" {
			continue
		}
		if loc, err := time.LoadLocation(zone); err == nil {
			return loc, true
		}
	}
	return nil, false
}

// CityToday returns the calendar date in q's city once a search for that city
// has returned events with a known zone. Artist/venue searches are excluded:
// their results are not limited to the city.
func (s *Service) CityToday(q Query, now time.Time) (string, bool) {
	key, ok := cityZoneKey(q)
	if !ok {
		return "", false
	}
	s.zones.mu.Lock()
	loc, ok := s.zones.known[key]
	s.zones.mu.Unlock()
	if !ok {
		return "", false
	}
	return now.In(loc).Format(time.DateOnly), true
}

func (s *Service) learnCityZone(q Query, events []models.Event) {
	key, ok := cityZoneKey(q)
	if !ok {
		return
	}
	loc, ok := eventsZone(events)
	if !ok {
		return
	}
	s.zones.mu.Lock()
	defer s.zones.mu.Unlock()
	if s.zones.known == nil {
		s.zones.known = map[string]*time.Location{}
	}
	if _, ok := s.zones.known[key]; !ok {
		if len(s.zones.order) >= cityZoneMemo {
			delete(s.zones.known, s.zones.order[0])
			s.zones.order = s.zones.order[1:]
		}
		s.zones.order = append(s.zones.order, key)
	}
	s.zones.known[key] = loc
}

func cityZoneKey(q Query) (string, bool) {
	if q.City == "" || q.ArtistID != "" || q.VenueID != "" {
		return "", false
	}
	return strings.ToLower(q.City) + "\x00" + q.Country, true
}
