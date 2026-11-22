package home

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/eventactivity"
	"github.com/sonastea/ticketopia/internal/models"
)

func dated(day string) models.EventStart { return models.EventStart{LocalDate: &day} }

func TestGroupForReadsTheCalendarFromTheBrowsedDay(t *testing.T) {
	// Saturday, Nov 21 2026: this week ends tomorrow; next week is Nov 23–29.
	today := time.Date(2026, 11, 21, 20, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		start      models.EventStart
		key, label string
	}{
		{dated("2026-11-21"), "day:2026-11-21", "Today"},
		{dated("2026-11-22"), "week:2026-11-22", "This week"},
		{dated("2026-11-23"), "week:2026-11-29", "Next week"},
		{dated("2026-11-29"), "week:2026-11-29", "Next week"},
		{dated("2026-11-30"), "later:2026-11", "Later in November"},
		{dated("2026-11-02"), "month:2026-11", "November"},
		{dated("2026-12-14"), "month:2026-12", "December"},
		{dated("2027-01-03"), "month:2027-01", "January 2027"},
		{models.EventStart{DateTBA: true}, "tba", "Date to be announced"},
	} {
		if got := groupFor(tc.start, today); got.Key != tc.key || got.Label != tc.label {
			t.Errorf("%v: got %q/%q, want %q/%q", *tc.start.LocalDate, got.Key, got.Label, tc.key, tc.label)
		}
	}
	// On a Sunday, the week ends today and "next week" starts tomorrow.
	sunday := time.Date(2026, 11, 22, 9, 0, 0, 0, time.UTC)
	if got := groupFor(dated("2026-11-23"), sunday); got.Label != "Next week" || got.Range != "Nov 23–29" {
		t.Errorf("Sunday's tomorrow: %+v", got)
	}
}

func TestDiscoverRowsGroupDatedResultsAndLeadOnlyAnUpcomingFirstPage(t *testing.T) {
	now := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	rows := func(sort string, pageNumber int, cursor string, days ...string) []discoverRow {
		events := make([]models.Event, len(days))
		for i, day := range days {
			events[i] = models.Event{ID: day, Start: dated(day)}
		}
		return discoverRows(SearchPage{Filters: discovery.Query{Sort: sort, Page: pageNumber}, PageCursor: cursor, Events: models.EventList{Items: events}}, now)
	}
	got := rows("date_asc", 0, "", "2026-12-01", "2026-12-02", "2027-01-05")
	if got[0].Group == nil || got[1].Group != nil || got[2].Group == nil || !got[0].Lead || got[1].Lead {
		t.Fatalf("headings open each stretch once and the soonest event leads: %+v", got)
	}
	if !rows("date_asc", 0, "", "2026-12-01")[0].Lead {
		t.Fatal("a single upcoming result lost its lead")
	}
	// A Back render restores from page one even though its cursor names the last page.
	if !rows("date_asc", 0, "restored-cursor", "2026-12-01", "2026-12-02")[0].Lead {
		t.Fatal("a restored first page lost its lead")
	}
	if rows("date_asc", 1, "cursor", "2026-12-01", "2026-12-02")[0].Lead {
		t.Fatal("a loaded page repeated the lead")
	}
	if rows("date_asc", 0, "", "2026-11-01", "2026-12-02")[0].Lead {
		t.Fatal("a past event was marked up next")
	}
	// Past and later days of the same month never share a heading, either way round.
	for _, sort := range []string{"date_asc", "date_desc"} {
		days := []string{"2026-11-01", "2026-11-30"}
		if sort == "date_desc" {
			days = []string{"2026-11-30", "2026-11-01"}
		}
		got := rows(sort, 0, "", days...)
		if got[0].Group == nil || got[1].Group == nil || got[0].Group.Label == got[1].Group.Label {
			t.Fatalf("%s: earlier and later November merged: %+v / %+v", sort, got[0].Group, got[1].Group)
		}
	}
	for _, row := range rows("name_asc", 0, "", "2026-12-01", "2027-01-05") {
		if row.Group != nil || row.Lead {
			t.Fatal("alphabetical results were split by date or given an up-next lead")
		}
	}
}

func TestDiscoverRowsUseTheCitysDayAcrossPages(t *testing.T) {
	// 03:00 UTC on Sunday Nov 22 is still Saturday evening in Chicago. A page
	// without zone data must group against the city's day, not UTC's.
	now := time.Date(2026, 11, 22, 3, 0, 0, 0, time.UTC)
	page := SearchPage{Today: "2026-11-21", Filters: discovery.Query{Sort: "date_asc", Page: 1}, Events: models.EventList{Items: []models.Event{{ID: "a", Start: dated("2026-11-22")}}}}
	if got := discoverRows(page, now)[0].Group; got == nil || got.Label != "This week" {
		t.Fatalf("grouped against UTC's day: %+v", got)
	}
}

func TestAgendaRowsShortenWhatTheHeadingAlreadySays(t *testing.T) {
	start := dated("2026-12-06")
	clock := "19:30:00"
	start.LocalTime = &clock
	for kind, want := range map[string]string{"week": "Sunday · 7:30 PM", "month": "Sun, Dec 6 · 7:30 PM", "": ""} {
		if got := agendaStartLabel(start, kind); got != want {
			t.Errorf("%q: got %q, want %q", kind, got, want)
		}
	}
	if got := agendaStartLabel(models.EventStart{DateTBA: true}, "tba"); got != "" {
		t.Errorf("an undated row lost its explicit label: %q", got)
	}
}

func TestFromPriceLabelLeadsWithTheLowestKnownPrice(t *testing.T) {
	low, high := "25.00", "40.00"
	for _, tc := range []struct {
		prices []models.PriceRange
		want   string
	}{
		{[]models.PriceRange{{Currency: "USD", Min: &low, Max: &high}}, "From USD 25.00"},
		{[]models.PriceRange{{Currency: "USD", Min: &low, Max: &low}}, "USD 25.00"},
		// Provider order doesn't decide the lowest price, and a minimum-only
		// range isn't presented as a fixed price.
		{[]models.PriceRange{{Currency: "USD", Min: &high, Max: &high}, {Currency: "USD", Min: &low, Max: &low}}, "From USD 25.00"},
		{[]models.PriceRange{{Currency: "USD", Min: &low}}, "From USD 25.00"},
		{[]models.PriceRange{{Min: &low}}, ""},
		{nil, ""},
	} {
		if got := fromPriceLabel(tc.prices); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestFallbackTintPrefersArtworkAndSpreadsDerivedHues(t *testing.T) {
	if got := fallbackTint(models.Event{ID: "a", Tint: "#8a5577"}); got != "#8a5577" {
		t.Fatalf("stored artwork tint ignored: %q", got)
	}
	if got := fallbackTint(models.Event{}); got != "" {
		t.Fatalf("an event-less live card should use the CSS brand tint, got %q", got)
	}
	seen := map[string]bool{}
	for i := range 12 {
		seen[fallbackTint(models.Event{ID: fmt.Sprintf("ticketmaster:Review_%d", i)})] = true
	}
	if len(seen) < 4 {
		t.Fatalf("sequential IDs share too few placeholder hues: %v", seen)
	}
}

func TestCountdownSaysHowSoonTheLeadIs(t *testing.T) {
	today := time.Date(2026, 11, 21, 12, 0, 0, 0, time.UTC)
	evening, afternoon := "19:30:00", "13:00:00"
	at := func(day string, clock *string) models.EventStart { s := dated(day); s.LocalTime = clock; return s }
	for _, tc := range []struct {
		start models.EventStart
		want  string
	}{
		{at("2026-11-21", &evening), "Up next · Tonight"},
		{at("2026-11-21", &afternoon), "Up next · Today"},
		{func() models.EventStart { s := at("2026-11-21", &evening); s.TimeTBA = true; return s }(), "Up next · Today"},
		{at("2026-11-22", nil), "Up next · Tomorrow"},
		{at("2026-11-25", nil), "Up next · In 4 days"},
		{at("2026-12-20", nil), "Up next"},
	} {
		if got := countdown(tc.start, today); got != tc.want {
			t.Errorf("%s: got %q, want %q", *tc.start.LocalDate, got, tc.want)
		}
	}
}

func TestTheLeadSkipsAnEventThatHasStarted(t *testing.T) {
	now := time.Date(2026, 11, 22, 2, 0, 0, 0, time.UTC) // 8pm in Chicago
	started := time.Date(2026, 11, 22, 1, 30, 0, 0, time.UTC)
	later := time.Date(2026, 11, 23, 1, 30, 0, 0, time.UTC)
	first, second := dated("2026-11-21"), dated("2026-11-22")
	first.DateTime, second.DateTime = &started, &later
	page := SearchPage{Today: "2026-11-21", Filters: discovery.Query{Sort: "date_asc"}, Events: models.EventList{Items: []models.Event{{ID: "a", Start: first}, {ID: "b", Start: second}}}}
	rows := discoverRows(page, now)
	if rows[0].Lead || !rows[1].Lead || rows[1].Countdown != "Up next · Tomorrow" {
		t.Fatalf("an event already under way stayed up next: %+v", rows)
	}
}

func TestOnlyTheLeadQuotesItsNewestRecommendation(t *testing.T) {
	events := []models.Event{{ID: "a", Start: dated("2026-12-01")}, {ID: "b", Start: dated("2026-12-02")}}
	reason := &eventactivity.Contribution{Body: "<b>Go early</b>", AuthorName: "Riley"}
	page := SearchPage{Today: "2026-11-21", Filters: discovery.Query{Sort: "date_asc"}, Events: models.EventList{Items: events},
		Activity: map[string]eventactivity.Preview{"a": {Recommendations: 1, Recommendation: reason}, "b": {Recommendations: 1, Recommendation: &eventactivity.Contribution{Body: "Second row", AuthorName: "Sam"}}}}
	var out bytes.Buffer
	if err := DiscoverRows(page).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	if strings.Count(body, "lead-quote") != 1 || !strings.Contains(body, "&lt;b&gt;Go early&lt;/b&gt;") || !strings.Contains(body, "Riley recommends this event") || strings.Contains(body, "Second row") {
		t.Fatalf("the lead should quote its own escaped reason, and no other row any: %s", body)
	}
	page.Activity["a"] = eventactivity.Preview{Recommendations: 1, Recommendation: &eventactivity.Contribution{AuthorName: "Riley"}}
	out.Reset()
	if err := DiscoverRows(page).Render(t.Context(), &out); err != nil || strings.Contains(out.String(), "lead-quote") {
		t.Fatal("a reason-less recommendation produced an empty quote")
	}
}
