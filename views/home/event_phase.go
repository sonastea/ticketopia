package home

import (
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

// EventPhase is where an event's date falls, in its own time zone, for
// conversation prompts. It is a prompt only: nothing records attendance.
type EventPhase string

const (
	// PhaseBefore is also the default for an unknown or unannounced date.
	PhaseBefore EventPhase = ""
	PhaseToday  EventPhase = "today"
	PhaseAfter  EventPhase = "after"
)

// PhaseOf compares the event's local date with today in the event's (or, when
// that is unusable, its first venue's) time zone. Without a usable zone, it is today from one day
// before to one day after UTC's date, so no event looks over early.
func PhaseOf(e models.Event, now time.Time) EventPhase {
	s := e.Start
	if s.LocalDate == nil || s.DateTBA || s.DateTBD {
		return PhaseBefore
	}
	day, err := time.Parse(time.DateOnly, *s.LocalDate)
	if err != nil {
		return PhaseBefore
	}
	if loc, ok := eventZone(e); ok {
		today := now.In(loc).Format(time.DateOnly)
		switch {
		case *s.LocalDate > today:
			return PhaseBefore
		case *s.LocalDate < today:
			return PhaseAfter
		}
		return PhaseToday
	}
	today, _ := time.Parse(time.DateOnly, now.UTC().Format(time.DateOnly))
	switch {
	case day.After(today.AddDate(0, 0, 1)):
		return PhaseBefore
	case day.Before(today.AddDate(0, 0, -1)):
		return PhaseAfter
	}
	return PhaseToday
}

// composerStarters work for any event category: they ask about going, not
// about a particular kind of performer.
var composerStarters = map[EventPhase][]string{
	PhaseBefore: {
		"First time at this venue. Any tips for getting in?",
		"What time should I arrive?",
		"Has anyone been to one of these before?",
	},
	PhaseToday: {
		"Heading there tonight. Which entrance is quickest?",
		"How early is everyone getting there?",
		"Tip for tonight: ",
	},
	PhaseAfter: {
		"Tip for the next visitor: ",
		"How did getting in and out go?",
		"What would you do differently next time?",
	},
}

// phaseEmpty is the empty Questions & tips line for each phase.
func phaseEmpty(p EventPhase) string {
	switch p {
	case PhaseToday:
		return "No questions or tips yet. Heading there tonight? Share a tip, or ask what you'd like to know."
	case PhaseAfter:
		return "No questions or tips yet. How was it? Share something that helps the next visitor."
	}
	return "No questions or tips yet. First time at this venue? Ask what you'd like to know before you go, or share something that would help others."
}

// eventZone is the event's named time zone, else its first venue's. Blank,
// unloadable and "Local" (the server's own zone) names don't count.
func eventZone(e models.Event) (*time.Location, bool) {
	names := []string{}
	if e.Start.Timezone != nil {
		names = append(names, *e.Start.Timezone)
	}
	if len(e.Venues) > 0 {
		names = append(names, e.Venues[0].Timezone)
	}
	for _, name := range names {
		if name == "" || name == "Local" {
			continue
		}
		if loc, ok := discovery.LoadZone(name); ok {
			return loc, true
		}
	}
	return nil, false
}
