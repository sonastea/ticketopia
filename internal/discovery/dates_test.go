package discovery

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

// runDates is a run of 30 listings a day, read from a start date.
type runDates struct {
	Catalog
	all []models.EventDate
}

func (r runDates) Dates(_ context.Context, _ models.Event, from, _ string, _ time.Time, limit int) ([]models.EventDate, error) {
	var out []models.EventDate
	for _, d := range r.all {
		if d.LocalDate >= from && len(out) < limit {
			out = append(out, d)
		}
	}
	return out, nil
}

// Following an event's last listed date always reaches the dates after it, so
// an agent or a person paging through a long run never loops.
func TestEventDatesPageForwardThroughLongRuns(t *testing.T) {
	r := runDates{}
	start := time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC)
	for day := range 14 {
		for slot := range 30 {
			clock := fmt.Sprintf("%02d:%02d:00", 12+slot/4, slot%4*15)
			r.all = append(r.all, models.EventDate{ID: fmt.Sprintf("ticketmaster:d%02ds%02d", day, slot), LocalDate: start.AddDate(0, 0, day).Format(time.DateOnly), LocalTime: &clock})
		}
	}
	s := New(t.Context(), kv.NewMemory(), zerolog.Nop(), Config{Catalog: r})
	s.now = func() time.Time { return start }
	event := func(d models.EventDate) models.Event {
		date := d.LocalDate
		return models.Event{ID: d.ID, Start: models.EventStart{LocalDate: &date}}
	}
	current, seen := r.all[0], map[string]bool{}
	for page := 0; ; page++ {
		dates, more := s.eventDates(t.Context(), event(current))
		if len(dates) == 0 || page > 10 {
			t.Fatalf("page %d: %d dates, more=%v", page, len(dates), more)
		}
		found := false
		for _, d := range dates {
			found = found || d.ID == current.ID
			seen[d.ID] = true
		}
		if !found {
			t.Fatalf("page %d omits the opened listing %s", page, current.ID)
		}
		if !more {
			break
		}
		last := dates[len(dates)-1]
		if last.ID == current.ID && page > 0 {
			t.Fatalf("page %d did not move past %s", page, current.ID)
		}
		current = last
	}
	if len(seen) != len(r.all) {
		t.Fatalf("reached %d of %d dates", len(seen), len(r.all))
	}
}
