// Package history owns scheduled collection and meaningful event changes. It
// never interprets a missing result, failed request, or 404 as cancellation.
package history

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

type Change struct {
	Kind   string          `json:"kind"`
	Before json.RawMessage `json:"before"`
	After  json.RawMessage `json:"after"`
}

func ChangeSummary(c Change) string {
	switch c.Kind {
	case "date":
		var start models.EventStart
		if json.Unmarshal(c.Before, &start) == nil {
			if start.DateTBA || start.DateTBD {
				return "Previously listed with an unconfirmed date"
			}
			if start.LocalDate != nil {
				label := *start.LocalDate
				if start.LocalTime != nil && !start.TimeTBA && !start.NoSpecificTime {
					label += " at " + *start.LocalTime
				}
				if start.Timezone != nil {
					label += " (" + *start.Timezone + ")"
				}
				return "Previously scheduled for " + label
			}
		}
		return "Schedule changed (previous date or time not confirmed)"
	case "venue":
		return "Venue information changed"
	case "sale_time":
		return "Ticket sale schedule changed"
	case "cancellation":
		return "Ticketmaster explicitly marked this event cancelled"
	case "postponement":
		return "Ticketmaster explicitly marked this event postponed"
	default:
		var status string
		if json.Unmarshal(c.After, &status) == nil {
			return "Status changed to " + status
		}
		return "Event status changed"
	}
}

// Changes compares facts, not presentation. Venue ordering, price/image edits,
// title changes and equivalent UTC instants are not reminder-worthy changes.
// Unknown venue/status/sale fields alone do not imply removal or cancellation.
func Changes(before, after models.Event) []Change {
	after = KnownFacts(before, after)
	changes := []Change{}
	add := func(kind string, old, next any) {
		if !reflect.DeepEqual(old, next) {
			b, _ := json.Marshal(old)
			a, _ := json.Marshal(next)
			changes = append(changes, Change{kind, b, a})
		}
	}
	start := func(s models.EventStart) models.EventStart {
		if s.DateTime != nil {
			t := s.DateTime.UTC().Truncate(time.Microsecond)
			s.DateTime = &t
		}
		return s
	}
	if after.Start.DateTime != nil || after.Start.LocalDate != nil || after.Start.DateTBA || after.Start.DateTBD || after.Start.TimeTBA || after.Start.NoSpecificTime {
		add("date", start(before.Start), start(after.Start))
	}
	venues := func(e models.Event) []string {
		set := map[string]bool{}
		for _, v := range e.Venues {
			id := v.ID
			if id == "" {
				id = strings.Join([]string{v.Name, v.Address, v.City, v.CountryCode}, "|")
			}
			set[id] = true
		}
		ids := []string{}
		for id := range set {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		return ids
	}
	if len(after.Venues) > 0 {
		add("venue", venues(before), venues(after))
	}
	sale := func(s models.PublicSale) models.PublicSale {
		for _, p := range []**time.Time{&s.Start, &s.End} {
			if *p != nil {
				t := (*p).UTC().Truncate(time.Microsecond)
				*p = &t
			}
		}
		return s
	}
	type saleTimes struct {
		Public   models.PublicSale   `json:"public"`
		Presales []models.PublicSale `json:"presales"`
	}
	times := func(e models.Event) saleTimes {
		windows := map[string]models.PublicSale{}
		for _, p := range e.Presales {
			if p.Start == nil && p.End == nil {
				continue
			}
			window := sale(models.PublicSale{Start: p.Start, End: p.End})
			data, _ := json.Marshal(window)
			windows[string(data)] = window
		}
		ordered := []string{}
		for window := range windows {
			ordered = append(ordered, window)
		}
		sort.Strings(ordered)
		presales := []models.PublicSale{}
		for _, key := range ordered {
			presales = append(presales, windows[key])
		}
		return saleTimes{sale(e.PublicSale), presales}
	}
	add("sale_time", times(before), times(after))
	status := func(s string) string {
		s = strings.ToLower(strings.TrimSpace(s))
		if s == "unknown" {
			return ""
		}
		if s == "canceled" {
			return "cancelled"
		}
		return s
	}
	old, next := status(before.Status), status(after.Status)
	if next != "" && old != next {
		kind := "status"
		if next == "cancelled" {
			kind = "cancellation"
		} else if next == "postponed" {
			kind = "postponement"
		}
		add(kind, old, next)
	}
	return changes
}

// KnownFacts retains previously known change-detection facts through a sparse
// successful response. The immutable observation still stores the actual DTO;
// this baseline is not exposed as a claim about current provider completeness.
func KnownFacts(before, after models.Event) models.Event {
	if strings.TrimSpace(after.Status) == "" || strings.EqualFold(strings.TrimSpace(after.Status), "unknown") {
		after.Status = before.Status
	}
	if len(after.Venues) == 0 {
		after.Venues = before.Venues
	}
	s := after.Start
	if s.DateTime == nil && s.LocalDate == nil && !s.DateTBA && !s.DateTBD && !s.TimeTBA && !s.NoSpecificTime {
		after.Start = before.Start
	} else {
		unknownDate := s.DateTBA || s.DateTBD
		unknownTime := s.TimeTBA || s.NoSpecificTime
		sameOptional := func(a, b *string) bool { return a == nil || (b != nil && *a == *b) }
		if s.DateTime == nil && !unknownDate && !unknownTime && sameOptional(s.LocalDate, before.Start.LocalDate) && sameOptional(s.LocalTime, before.Start.LocalTime) {
			after.Start.DateTime = before.Start.DateTime
		}
		if s.LocalDate == nil && !unknownDate {
			after.Start.LocalDate = before.Start.LocalDate
		}
		if s.LocalTime == nil && !unknownDate && !unknownTime {
			after.Start.LocalTime = before.Start.LocalTime
		}
		if s.DateTime == nil && s.LocalTime == nil && !unknownDate && !unknownTime {
			after.Start.TimeTBA = before.Start.TimeTBA
			after.Start.NoSpecificTime = before.Start.NoSpecificTime
		}
		if s.Timezone == nil {
			after.Start.Timezone = before.Start.Timezone
		}
	}
	if after.PublicSale.Start == nil && !after.PublicSale.StartTBD {
		after.PublicSale.Start = before.PublicSale.Start
		after.PublicSale.StartTBD = before.PublicSale.StartTBD
	}
	if after.PublicSale.End == nil {
		after.PublicSale.End = before.PublicSale.End
	}
	knownPresales := false
	for _, p := range after.Presales {
		knownPresales = knownPresales || p.Start != nil || p.End != nil
	}
	if !knownPresales {
		after.Presales = before.Presales
	} else {
		// Copy before merging optional times; callers' observation evidence must
		// never be mutated by change detection.
		after.Presales = append([]models.Presale(nil), after.Presales...)
		for i, p := range after.Presales {
			for _, old := range before.Presales {
				if strings.EqualFold(strings.TrimSpace(p.Name), strings.TrimSpace(old.Name)) {
					if p.Start == nil {
						after.Presales[i].Start = old.Start
					}
					if p.End == nil {
						after.Presales[i].End = old.End
					}
					break
				}
			}
		}
	}
	return after
}

type Scope struct {
	City    string `json:"city"`
	Country string `json:"country"`
}

func (s Scope) ID() string {
	sum := sha256.Sum256([]byte(strings.ToLower(s.City) + "\x00" + s.Country))
	return hex.EncodeToString(sum[:])
}

type Task struct {
	ID string
	Scope
	Date string
}

func NewTask(scope Scope, date string) Task {
	sum := sha256.Sum256([]byte(scope.ID() + "\x00" + date))
	return Task{hex.EncodeToString(sum[:]), scope, date}
}

type Claim struct {
	Task
	RunID string
}

type Outcome struct {
	Status  string
	Failure string
}

type Observation struct {
	Event      models.Event
	ObservedAt time.Time
	Changes    []Change
}

type Freshness struct {
	FirstSeen   time.Time
	LastSeen    time.Time
	LastChanged *time.Time
}
