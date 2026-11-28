package home

import (
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/models"
)

func TestPhaseOfUsesTheEventsOwnDay(t *testing.T) {
	str := func(s string) *string { return &s }
	// 07:30 UTC on Nov 28 is still Nov 27 in Los Angeles.
	now := time.Date(2026, 11, 28, 7, 30, 0, 0, time.UTC)
	la := func(date string) models.Event {
		return models.Event{Start: models.EventStart{LocalDate: str(date), Timezone: str("America/Los_Angeles")}}
	}
	venueZone := models.Event{Start: models.EventStart{LocalDate: str("2026-11-27")}, Venues: []models.Venue{{Timezone: "America/Los_Angeles"}}}
	noZone := func(date string) models.Event { return models.Event{Start: models.EventStart{LocalDate: str(date)}} }
	for name, tc := range map[string]struct {
		e    models.Event
		want EventPhase
	}{
		"before":                   {la("2026-11-28"), PhaseBefore},
		"on the local day":         {la("2026-11-27"), PhaseToday},
		"after":                    {la("2026-11-26"), PhaseAfter},
		"venue time zone":          {venueZone, PhaseToday},
		"unknown date":             {models.Event{}, PhaseBefore},
		"date to be announced":     {models.Event{Start: models.EventStart{LocalDate: str("2026-11-20"), DateTBA: true}}, PhaseBefore},
		"no zone, a day ahead":     {noZone("2026-11-29"), PhaseToday},
		"no zone, two days ahead":  {noZone("2026-11-30"), PhaseBefore},
		"no zone, a day behind":    {noZone("2026-11-27"), PhaseToday},
		"no zone, two days behind": {noZone("2026-11-26"), PhaseAfter},
		"blank zone uses venue":    {models.Event{Start: models.EventStart{LocalDate: str("2026-11-27"), Timezone: str("")}, Venues: []models.Venue{{Timezone: "America/Los_Angeles"}}}, PhaseToday},
		"invalid zone uses venue":  {models.Event{Start: models.EventStart{LocalDate: str("2026-11-28"), Timezone: str("Mars/Olympus")}, Venues: []models.Venue{{Timezone: "America/Los_Angeles"}}}, PhaseBefore},
		"server Local is ignored":  {models.Event{Start: models.EventStart{LocalDate: str("2026-11-29"), Timezone: str("Local")}}, PhaseToday},
	} {
		if got := PhaseOf(tc.e, now); got != tc.want {
			t.Errorf("%s: got %q want %q", name, got, tc.want)
		}
	}
	// Around UTC midnight, Tokyo's day has long turned and Los Angeles' hasn't.
	for _, tc := range []struct {
		now        time.Time
		zone, date string
		want       EventPhase
	}{
		{time.Date(2026, 11, 27, 23, 59, 0, 0, time.UTC), "Asia/Tokyo", "2026-11-27", PhaseAfter},
		{time.Date(2026, 11, 28, 0, 1, 0, 0, time.UTC), "America/Los_Angeles", "2026-11-28", PhaseBefore},
		{time.Date(2026, 11, 28, 0, 1, 0, 0, time.UTC), "America/Los_Angeles", "2026-11-27", PhaseToday},
	} {
		if got := PhaseOf(models.Event{Start: models.EventStart{LocalDate: str(tc.date), Timezone: str(tc.zone)}}, tc.now); got != tc.want {
			t.Errorf("%s %s at %s: got %q want %q", tc.zone, tc.date, tc.now, got, tc.want)
		}
	}
}

func TestDiscussionPanelFollowsPhaseAndKeepsVenueTipsApart(t *testing.T) {
	after := renderNeighbor(t, DiscussionPanel("e", DiscussionView{Enabled: true, ViewerID: "v", Phase: PhaseAfter}, "/events/e?section=discussion"))
	for _, want := range []string{"How was it? Share something that helps the next visitor.", "Tip for the next visitor:"} {
		if !strings.Contains(after, want) {
			t.Errorf("after-show panel lacks %q", want)
		}
	}
	if strings.Contains(after, "What time should I arrive?") || strings.Contains(after, "From past shows") {
		t.Error("after-show panel kept a before-show starter or an empty venue-tips section")
	}
	today := renderNeighbor(t, DiscussionPanel("e", DiscussionView{Enabled: true, Phase: PhaseToday}, "/events/e?section=discussion"))
	if !strings.Contains(today, "Heading there tonight? Share a tip") {
		t.Error("on-the-day empty line missing")
	}

	long := strings.Repeat("Use the side door on Main Street. ", 20)
	tip := discussions.Post{ID: "r1", ThreadID: "t1", EventID: "past", Body: long, HelpfulCount: 2,
		Profile: &accounts.PublicProfile{ID: "a1", DisplayName: "Alex Example"}, Event: models.Event{ID: "past", Name: "Last Spring's Show"}}
	body := renderNeighbor(t, DiscussionPanel("e", DiscussionView{Enabled: true, VenueTips: []discussions.Post{tip}}, "/events/e?section=discussion"))
	for _, want := range []string{"From past shows at this venue", "Not part of this event", "Read the reply on Last Spring&#39;s Show", " by Alex Example", "2 Helpful", "/events/past/discussions/t1?post_id=r1#post-r1", "…"} {
		if !strings.Contains(body, want) {
			t.Errorf("venue tips lack %q", want)
		}
	}
	// The tip doesn't join this event's own conversations or their count.
	if strings.Contains(body, "visible</p>") || !strings.Contains(body, "No questions or tips yet") {
		t.Error("a venue tip counted toward the event's own conversations")
	}
	if strings.Contains(body, long) {
		t.Error("a long venue tip was not shortened to an excerpt")
	}
}
