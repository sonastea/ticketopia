// Package calendar writes one event as an iCalendar (RFC 5545) file.
package calendar

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/sonastea/ticketopia/internal/models"
)

// ErrNoDate means the provider has not announced a date, so there is nothing
// to add to a calendar yet.
var ErrNoDate = errors.New("the event date has not been announced")

// Details are the non-provider parts of the file.
type Details struct {
	// PageURL is the absolute event page; empty omits the URL property.
	PageURL string
	// Stamp is the DTSTAMP instant. Use the data's collection time, not the
	// request time, so replicas produce identical bytes for one snapshot.
	Stamp time.Time
}

// Available reports whether Event can produce a file for this event.
func Available(event models.Event) bool {
	_, _, ok := start(event.Start)
	return ok
}

// start returns a known UTC instant, or else the local day for date-only and
// time-TBA events. Placeholder dates never become calendar entries.
func start(s models.EventStart) (time.Time, bool, bool) {
	if s.DateTBA || s.DateTBD {
		return time.Time{}, false, false
	}
	if s.DateTime != nil && !s.DateTime.IsZero() && !s.TimeTBA && !s.NoSpecificTime {
		return s.DateTime.UTC(), false, true
	}
	if s.LocalDate == nil {
		return time.Time{}, false, false
	}
	day, err := time.Parse(time.DateOnly, *s.LocalDate)
	if err != nil {
		return time.Time{}, false, false
	}
	if !s.TimeTBA && !s.NoSpecificTime && s.LocalTime != nil && s.Timezone != nil && *s.Timezone != "" && *s.Timezone != "Local" {
		if zone, err := time.LoadLocation(*s.Timezone); err == nil {
			if at, err := time.ParseInLocation(time.DateOnly+" "+time.TimeOnly, *s.LocalDate+" "+*s.LocalTime, zone); err == nil {
				return at.UTC(), false, true
			}
		}
	}
	return day, true, true
}

// Event returns the file. An unknown end is left open (a timed entry marks the
// start; an all-day entry covers its one day) rather than inventing a duration.
func Event(event models.Event, details Details) ([]byte, error) {
	at, allDay, ok := start(event.Start)
	if !ok {
		return nil, ErrNoDate
	}
	var b strings.Builder
	line := func(name, value string) { fold(&b, name+":"+value) }
	line("BEGIN", "VCALENDAR")
	line("VERSION", "2.0")
	line("PRODID", "-//Ticketopia//Event calendar//EN")
	line("CALSCALE", "GREGORIAN")
	line("METHOD", "PUBLISH")
	line("BEGIN", "VEVENT")
	line("UID", text(event.ID+"@ticketopia"))
	line("DTSTAMP", details.Stamp.UTC().Format("20060102T150405Z"))
	if allDay {
		fold(&b, "DTSTART;VALUE=DATE:"+at.Format("20060102"))
		fold(&b, "DTEND;VALUE=DATE:"+at.AddDate(0, 0, 1).Format("20060102"))
	} else {
		line("DTSTART", at.Format("20060102T150405Z"))
	}
	line("SUMMARY", text(event.Name))
	if location := place(event); location != "" {
		line("LOCATION", text(location))
	}
	var description []string
	if details.PageURL != "" {
		line("URL", details.PageURL)
		description = append(description, "Event details: "+details.PageURL)
	}
	if event.Source.URL != "" {
		description = append(description, "Tickets: "+event.Source.URL)
	}
	if len(description) > 0 {
		line("DESCRIPTION", text(strings.Join(description, "\n")))
	}
	if strings.EqualFold(event.Status, "cancelled") || strings.EqualFold(event.Status, "canceled") {
		line("STATUS", "CANCELLED")
	}
	line("END", "VEVENT")
	line("END", "VCALENDAR")
	return []byte(b.String()), nil
}

func place(event models.Event) string {
	var p models.Place
	if len(event.Venues) > 0 {
		p = event.Venues[0].Place
	} else if event.Place != nil {
		p = *event.Place
	}
	var parts []string
	for _, value := range []string{p.Name, p.Address, p.City, p.State, p.PostalCode, p.CountryCode} {
		if value = strings.TrimSpace(value); value != "" {
			parts = append(parts, value)
		}
	}
	return strings.Join(parts, ", ")
}

// text escapes a TEXT value (RFC 5545 §3.3.11).
var textEscaper = strings.NewReplacer(`\`, `\\`, ";", `\;`, ",", `\,`, "\r\n", `\n`, "\n", `\n`, "\r", `\n`)

func text(value string) string { return textEscaper.Replace(value) }

// fold writes one content line, folded below 75 octets without splitting a
// UTF-8 sequence (RFC 5545 §3.1).
func fold(b *strings.Builder, content string) {
	limit := 75
	for len(content) > limit {
		cut := limit
		for cut > 0 && !utf8.RuneStart(content[cut]) {
			cut--
		}
		b.WriteString(content[:cut])
		b.WriteString("\r\n ")
		content = content[cut:]
		limit = 74 // The leading space counts toward continuation lines.
	}
	b.WriteString(content)
	b.WriteString("\r\n")
}
