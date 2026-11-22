package home

import (
	"fmt"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

// eventGroup names the stretch of the calendar a date-sorted Discover result
// falls in, so a long list reads as an agenda: Today, This week, Next week,
// the rest of this month, then each month. Keys carry the stretch's actual
// dates, so app.js only merges a Load more heading into the previous page's
// when both name the same days.
type eventGroup struct {
	Key   string
	Label string
	Range string
}

// discoverRow is one rendered Discover item: an optional group heading before
// the event, and whether the event leads the first page.
type discoverRow struct {
	Event models.Event
	Group *eventGroup
	// Kind names the stretch the event sits in (day, week, later, month,
	// tba), empty when results aren't grouped.
	Kind string
	Lead bool
	// Countdown is the lead's marker: Today/Tonight, Tomorrow, In N days.
	Countdown string
}

// rowMode is how a result row sits in Discover's agenda.
type rowMode struct {
	Lead      bool
	Kind      string
	Countdown string
}

// discoverRows groups date-sorted results under calendar headings and marks
// the soonest event on the first page as the lead. Name-sorted results stay
// one ungrouped list: a calendar heading would interrupt their order.
func discoverRows(page SearchPage, now time.Time) []discoverRow {
	events := page.Events.Items
	rows := make([]discoverRow, 0, len(events))
	ascending := page.Filters.Sort == "" || page.Filters.Sort == "date_asc"
	dated := ascending || page.Filters.Sort == "date_desc"
	today := agendaDay(page, now)
	previous := ""
	for i, event := range events {
		row := discoverRow{Event: event}
		if dated {
			group := groupFor(event.Start, today)
			row.Kind, _, _ = strings.Cut(group.Key, ":")
			if group.Key != previous {
				row.Group = &group
				previous = group.Key
			}
		}
		// "Up next" only for the soonest-first page that starts the search
		// (a restored Back render starts there too), and only for an event
		// with a known date that hasn't passed.
		row.Lead = i == 0 && ascending && page.Filters.Page == 0 && upcoming(event.Start, today)
		if row.Lead {
			row.Countdown = countdown(event.Start, today)
		}
		rows = append(rows, row)
	}
	return rows
}

// agendaDay is the browsed city's calendar day: the one the handler learned
// from the city's zone, otherwise the zone of these results (as the date
// shortcuts use), otherwise UTC.
func agendaDay(page SearchPage, now time.Time) time.Time {
	if page.Today != "" {
		if day, err := time.Parse(time.DateOnly, page.Today); err == nil {
			return day
		}
	}
	return searchToday(page.Events.Items, now)
}

func eventDay(start models.EventStart) (time.Time, bool) {
	if start.DateTBA || start.DateTBD || start.LocalDate == nil {
		return time.Time{}, false
	}
	date, err := time.Parse(time.DateOnly, *start.LocalDate)
	return date, err == nil
}

func calendarDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func upcoming(start models.EventStart, today time.Time) bool {
	date, ok := eventDay(start)
	return ok && !date.Before(calendarDay(today))
}

func groupFor(start models.EventStart, today time.Time) eventGroup {
	date, ok := eventDay(start)
	if !ok {
		return eventGroup{Key: "tba", Label: "Date to be announced"}
	}
	day := calendarDay(today)
	// Weeks run Monday to Sunday, matching the This weekend shortcut's Sunday end.
	sunday := day.AddDate(0, 0, (7-int(day.Weekday()))%7)
	nextSunday := sunday.AddDate(0, 0, 7)
	switch {
	case date.Equal(day):
		return eventGroup{Key: "day:" + day.Format(time.DateOnly), Label: "Today", Range: date.Format("Mon, Jan 2")}
	case date.After(day) && !date.After(sunday):
		return eventGroup{Key: "week:" + sunday.Format(time.DateOnly), Label: "This week", Range: weekRange(day.AddDate(0, 0, 1), sunday)}
	case date.After(sunday) && !date.After(nextSunday):
		return eventGroup{Key: "week:" + nextSunday.Format(time.DateOnly), Label: "Next week", Range: weekRange(sunday.AddDate(0, 0, 1), nextSunday)}
	}
	month := date.Format("2006-01")
	// The rest of this month after next week; earlier days of this month
	// (an explicit past range) keep the plain month heading.
	if date.After(day) && date.Year() == day.Year() && date.Month() == day.Month() {
		return eventGroup{Key: "later:" + month, Label: "Later in " + date.Format("January")}
	}
	if date.Year() == day.Year() {
		return eventGroup{Key: "month:" + month, Label: date.Format("January")}
	}
	return eventGroup{Key: "month:" + month, Label: date.Format("January 2006")}
}

func weekRange(from, through time.Time) string {
	if from.Equal(through) {
		return through.Format("Mon, Jan 2")
	}
	if from.Month() == through.Month() {
		return from.Format("Jan 2") + "–" + through.Format("2")
	}
	return from.Format("Jan 2") + " – " + through.Format("Jan 2")
}

// countdown says how soon the lead is, relative to the browsed city's day.
func countdown(start models.EventStart, today time.Time) string {
	date, ok := eventDay(start)
	if !ok {
		return "Up next"
	}
	switch days := int(date.Sub(calendarDay(today)).Hours() / 24); {
	case days == 0:
		if clock, err := time.Parse(time.TimeOnly, deref(start.LocalTime)); err == nil && clock.Hour() >= 17 {
			return "Up next · Tonight"
		}
		return "Up next · Today"
	case days == 1:
		return "Up next · Tomorrow"
	case days < 14:
		return fmt.Sprintf("Up next · In %d days", days)
	}
	return "Up next"
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
