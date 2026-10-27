package calendar

import (
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/sonastea/ticketopia/internal/models"
)

func ptr[T any](v T) *T { return &v }

func TestEventStartsOnlyFromAnnouncedDates(t *testing.T) {
	instant := time.Date(2026, 11, 2, 1, 30, 0, 0, time.FixedZone("CST", -6*3600))
	for _, tc := range []struct {
		name  string
		start models.EventStart
		want  []string
	}{
		{"timed", models.EventStart{DateTime: &instant, LocalDate: ptr("2026-11-01")}, []string{"DTSTART:20261102T073000Z\r\n"}},
		{"no specific time", models.EventStart{LocalDate: ptr("2026-11-01"), NoSpecificTime: true}, []string{"DTSTART;VALUE=DATE:20261101\r\n", "DTEND;VALUE=DATE:20261102\r\n"}},
		{"time TBA", models.EventStart{DateTime: &instant, LocalDate: ptr("2026-12-31"), TimeTBA: true}, []string{"DTSTART;VALUE=DATE:20261231\r\n", "DTEND;VALUE=DATE:20270101\r\n"}},
	} {
		event := models.Event{ID: "ticketmaster:x", Name: "Show", Start: tc.start}
		if !Available(event) {
			t.Fatalf("%s: unavailable", tc.name)
		}
		body, err := Event(event, Details{Stamp: instant})
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range tc.want {
			if !strings.Contains(string(body), want) {
				t.Errorf("%s: missing %q in\n%s", tc.name, want, body)
			}
		}
		if tc.name == "timed" && strings.Contains(string(body), "DTEND") {
			t.Errorf("timed event invented an end:\n%s", body)
		}
	}
	for _, start := range []models.EventStart{
		{DateTime: &instant, LocalDate: ptr("2026-11-01"), DateTBA: true},
		{LocalDate: ptr("2026-11-01"), DateTBD: true},
		{TimeTBA: true}, {LocalDate: ptr("soon")},
	} {
		event := models.Event{Start: start}
		if _, err := Event(event, Details{}); Available(event) || !errors.Is(err, ErrNoDate) {
			t.Errorf("%+v produced a calendar entry", start)
		}
	}
}

func TestEventEscapesAndFoldsProviderText(t *testing.T) {
	instant := time.Date(2026, 11, 2, 1, 30, 0, 0, time.UTC)
	event := models.Event{
		ID: "ticketmaster:x", Name: "Rock, Paper; Scissors\\Live\nTour " + strings.Repeat("é", 60), Status: "cancelled",
		Start:  models.EventStart{DateTime: &instant},
		Source: models.Source{URL: "https://www.ticketmaster.com/event/x"},
		Venues: []models.Venue{{Place: models.Place{Name: "The Hall", Address: "1 Main St", City: "Chicago"}}},
	}
	body, err := Event(event, Details{PageURL: "https://ticketopia.example/events/ticketmaster:x", Stamp: instant})
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		`SUMMARY:Rock\, Paper\; Scissors\\Live\nTour é`, "LOCATION:The Hall\\, 1 Main St\\, Chicago\r\n",
		"STATUS:CANCELLED\r\n", "URL:https://ticketopia.example/events/ticketmaster:x\r\n", "UID:ticketmaster:x@ticketopia\r\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q in\n%s", want, s)
		}
	}
	if !strings.HasPrefix(s, "BEGIN:VCALENDAR\r\n") || !strings.HasSuffix(s, "END:VCALENDAR\r\n") {
		t.Fatalf("not one calendar:\n%s", s)
	}
	unfolded := strings.ReplaceAll(s, "\r\n ", "")
	if !strings.Contains(unfolded, "Tour "+strings.Repeat("é", 60)+"\r\n") {
		t.Fatal("folding changed the summary")
	}
	for _, line := range strings.Split(strings.TrimSuffix(s, "\r\n"), "\r\n") {
		if len(line) > 75 || strings.ContainsAny(line, "\r\n") {
			t.Errorf("unfolded line %q", line)
		}
		if !utf8.ValidString(line) {
			t.Errorf("split UTF-8 sequence in %q", line)
		}
	}
}
