package home

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/htmlcheck"
	eventinterests "github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
)

// The event page and its preview keep to one explanatory note per section in
// every section, signed in, with community content present.
func TestEventScreensKeepTheNotesBudget(t *testing.T) {
	now := time.Now()
	sale := now.Add(48 * time.Hour)
	event := models.Event{ID: "ticketmaster:Notes", Name: "Local show", Info: "About it", Status: "onsale",
		Source: models.Source{URL: "https://www.ticketmaster.com/notes"}, PublicSale: models.PublicSale{Start: &sale}}
	profile := accounts.PublicProfile{ID: "p1", DisplayName: "Riley"}
	post := discussions.Post{ID: "q1", EventID: event.ID, Profile: &profile, Body: "Coat check?", CreatedAt: now, UpdatedAt: now}
	count, none := 1, 0
	type state struct {
		name          string
		signedIn, has bool
	}
	for _, st := range []state{{"signed in", true, true}, {"guest, empty", false, false}} {
		for _, section := range []string{"overview", "discussion", "community"} {
			page := EventPage{
				Detail: models.EventDetail{Item: event, Meta: models.Freshness{DataAsOf: now}}, Section: section, ReturnURL: "/", ActionReturnURL: "/",
				Saves:           SaveView{Enabled: true, SignedIn: true, CSRF: "c", Reminders: true, States: map[string]bool{}},
				Interests:       InterestView{Enabled: true, SignedIn: true, CSRF: "c", DefaultVisibility: "private"},
				Discussions:     DiscussionView{Enabled: true, ViewerID: profile.ID, CSRF: "c", List: discussions.List{Items: []discussions.Post{post}, Count: 1}},
				Recommendations: RecommendationView{Enabled: true, SignedIn: true, ViewerID: "viewer", CSRF: "c", List: recommendations.List{Items: []recommendations.Item{{Profile: profile, Reason: "Great view.", RecommendedAt: now, UpdatedAt: now}}, Count: &count}},
				Participants:    eventinterests.Participants{Items: []eventinterests.Participant{{Profile: profile}}},
			}
			if !st.signedIn {
				page.Saves.SignedIn, page.Interests.SignedIn, page.Recommendations.SignedIn = false, false, false
				page.Discussions.ViewerID, page.Recommendations.ViewerID = "", ""
			}
			if !st.has {
				page.Discussions.List = discussions.List{}
				page.Recommendations.List = recommendations.List{Count: &none}
				page.Participants = eventinterests.Participants{}
			}
			for name, render := range map[string]func(*bytes.Buffer) error{
				"page":    func(b *bytes.Buffer) error { return Event(page).Render(t.Context(), b) },
				"preview": func(b *bytes.Buffer) error { return EventContext(page).Render(t.Context(), b) },
			} {
				var out bytes.Buffer
				if err := render(&out); err != nil {
					t.Fatal(err)
				}
				over, err := htmlcheck.NotesOverBudget(&out, 1)
				if err != nil {
					t.Fatal(err)
				}
				if len(over) > 0 {
					t.Errorf("%s %s (%s) exceeds the notes budget: %v", name, section, st.name, over)
				}
			}
		}
	}
}

func TestSaleTimingShowsOnlyAWaitableSaleInTheHero(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	for _, tc := range []struct {
		name, status string
		start        *time.Time
		upcoming     bool
	}{
		{"presale already on sale", "onsale", &future, true},
		{"sale ahead", "offsale", &future, true},
		{"sale started", "onsale", &past, false},
		{"canceled", "canceled", &future, false},
		{"postponed", "postponed", &future, false},
		{"no sale time", "onsale", nil, false},
	} {
		event := models.Event{Status: tc.status, PublicSale: models.PublicSale{Start: tc.start}}
		up, gone := upcomingSaleLabel(event, now), pastSaleLabel(event, now)
		if (up != "") != tc.upcoming {
			t.Errorf("%s: upcoming label %q", tc.name, up)
		}
		if tc.upcoming && !strings.HasPrefix(up, "Public sale starts ") {
			t.Errorf("%s: upcoming label must say the sale starts: %q", tc.name, up)
		}
		if (gone != "") != (tc.start == &past) {
			t.Errorf("%s: past label %q", tc.name, gone)
		}
	}
}
