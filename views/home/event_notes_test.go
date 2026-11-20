package home

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/htmlcheck"
	eventinterests "github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/views/layouts"
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

func TestResultRowsShowOnlyAnUpcomingSale(t *testing.T) {
	now := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	future, past := now.Add(72*time.Hour), now.Add(-72*time.Hour)
	events := []models.Event{
		{ID: "ticketmaster:Soon", Name: "Soon", Status: "onsale", PublicSale: models.PublicSale{Start: &future}},
		{ID: "ticketmaster:Started", Name: "Started", Status: "onsale", PublicSale: models.PublicSale{Start: &past}},
	}
	ctx := layouts.WithNow(t.Context(), now)
	for name, rows := range map[string]templ.Component{
		"collection": EventRows(events, "/saved", "", SaveView{}, "/saved", InterestView{}),
		"discover":   EventRowsWithActivity(events, "/", "", SaveView{}, "/", InterestView{}, nil),
	} {
		var out bytes.Buffer
		if err := rows.Render(ctx, &out); err != nil {
			t.Fatal(err)
		}
		soon, started, _ := strings.Cut(out.String(), `data-event-id="ticketmaster:Started"`)
		if !strings.Contains(soon, "Public sale starts ") || strings.Contains(started, "Public sale") {
			t.Errorf("%s rows must name only the upcoming sale", name)
		}
	}
}

func TestSaveButtonShowsStateOrTheActionInSaved(t *testing.T) {
	view := SaveView{Enabled: true, SignedIn: true, CSRF: "c", States: map[string]bool{"ticketmaster:A": true, "ticketmaster:B": false}}
	for _, tc := range []struct{ id, returnURL, visible, name string }{
		{"ticketmaster:A", "/", "Saved", "Remove from Saved — A"},
		{"ticketmaster:A", "/saved", "Remove", "Remove from Saved — A"},
		{"ticketmaster:B", "/saved", "Save", "Save — A"},
	} {
		var out bytes.Buffer
		if err := SaveControlContent(tc.id, "A", view, tc.returnURL, false).Render(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), `aria-label="`+tc.name+`"`) || !strings.Contains(out.String(), ">"+tc.visible+"</span>") {
			t.Errorf("%s on %s: want visible %q named %q", tc.id, tc.returnURL, tc.visible, tc.name)
		}
	}
}

// A conversation keeps the budget for the owner and a guest: an empty thread,
// and one with nested replies while replying to a specific contribution.
func TestConversationKeepsTheNotesBudget(t *testing.T) {
	now := time.Now()
	owner := accounts.PublicProfile{ID: "owner", DisplayName: "Riley"}
	other := accounts.PublicProfile{ID: "other", DisplayName: "Sam"}
	root := discussions.Post{ID: "root", ThreadID: "root", EventID: "ticketmaster:E", Profile: &owner, Body: "Coat check?", CreatedAt: now, UpdatedAt: now, ReplyCount: 3}
	direct := discussions.Post{ID: "r1", ThreadID: "root", ParentID: "root", EventID: "ticketmaster:E", Profile: &other, Body: "Yes.", CreatedAt: now, UpdatedAt: now}
	nested := []discussions.Post{direct,
		{ID: "r2", ThreadID: "root", ParentID: "r1", ParentName: "Sam", EventID: "ticketmaster:E", Profile: &owner, Body: "Thanks.", CreatedAt: now, UpdatedAt: now},
		{ID: "r3", ThreadID: "root", ParentID: "r1", ParentName: "Sam", EventID: "ticketmaster:E", Profile: &other, Body: "Sure.", CreatedAt: now, UpdatedAt: now}}
	empty := root
	empty.ReplyCount = 0
	for _, viewer := range []string{"owner", ""} {
		for name, page := range map[string]DiscussionPage{
			"empty":  {Root: &empty},
			"nested": {Root: &root, ReplyTo: &nested[1], ParentID: "r2", View: DiscussionView{List: discussions.List{Items: nested}}},
		} {
			page.Event = models.Event{ID: "ticketmaster:E", Name: "Show"}
			page.FollowEnabled, page.NotificationsPaused = viewer != "", true
			page.ReturnURL, page.URL = "/", "/events/ticketmaster:E/discussions/root"
			page.View.Enabled, page.View.ModerationEnabled, page.View.ViewerID, page.View.CSRF = true, true, viewer, "c"
			var out bytes.Buffer
			if err := Discussion(page).Render(t.Context(), &out); err != nil {
				t.Fatal(err)
			}
			over, err := htmlcheck.NotesOverBudget(&out, 1)
			if err != nil {
				t.Fatal(err)
			}
			if len(over) > 0 {
				t.Errorf("viewer %q, %s thread exceeds the notes budget: %v", viewer, name, over)
			}
		}
	}
}

// Community explains last-known details once, not on every card, and its
// Conversations tab and Happening Now keep the budget too.
func TestCommunityScreensKeepTheNotesBudget(t *testing.T) {
	now := time.Now()
	stale := models.Freshness{DataAsOf: now, Stale: true}
	profile := accounts.PublicProfile{ID: "p", DisplayName: "Riley"}
	groups := []recommendations.EventGroup{}
	for _, id := range []string{"a", "b", "c"} {
		groups = append(groups, recommendations.EventGroup{Event: models.Event{ID: "ticketmaster:" + id, Name: id, Status: "onsale"}, Meta: stale, Count: 1, FirstRecommendedAt: now,
			Recommendations: []recommendations.Item{{Profile: profile, Reason: "Go.", RecommendedAt: now}}})
	}
	post := discussions.Post{ID: "q", ThreadID: "q", EventID: "ticketmaster:a", Profile: &profile, Body: "Coat check?", CreatedAt: now, UpdatedAt: now, Event: models.Event{ID: "ticketmaster:a", Name: "a"}}
	for name, component := range map[string]templ.Component{
		"recommendations":  Community(CommunityPage{Enabled: true, List: recommendations.Feed{Items: groups}, ReturnURL: "/community", CategoriesUnavailable: true}),
		"conversations":    DiscussionCommunity(DiscussionCommunityPage{Enabled: true, List: discussions.List{Items: []discussions.Post{post}}, URL: "/community/discussions"}),
		"happening now":    HappeningNow(HappeningNowPage{Enabled: true, ReturnURL: "/happening-now"}),
		"happening paused": HappeningNow(HappeningNowPage{Enabled: true, Paused: true, ReturnURL: "/happening-now"}),
	} {
		var out bytes.Buffer
		if err := component.Render(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		if name == "recommendations" && strings.Count(out.String(), "Some events show last-known details") != 1 {
			t.Errorf("community must explain last-known details once")
		}
		over, err := htmlcheck.NotesOverBudget(&out, 1)
		if err != nil {
			t.Fatal(err)
		}
		if len(over) > 0 {
			t.Errorf("%s exceeds the notes budget: %v", name, over)
		}
	}
}

// Radar keeps one visible line that names its coverage state, staleness and
// the time zone; each coverage state has its own explanation.
func TestRadarFreshnessNamesEveryCoverageState(t *testing.T) {
	at := time.Date(2026, 11, 19, 8, 0, 0, 0, time.UTC)
	for status, want := range map[string]string{
		"not_collected": "this is not a confirmed empty search",
		"partial":       "more events may exist",
		"stale":         "previously completed coverage",
		"complete":      "Collected coverage for all requested dates",
	} {
		meta := models.Freshness{DataAsOf: at, Stale: status == "stale", Coverage: &models.Coverage{Status: status}}
		var out bytes.Buffer
		if err := RadarFreshness(meta).Render(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		body := out.String()
		if !strings.Contains(body, want) || !strings.Contains(body, "Venue-local event times") || strings.Contains(body, "<details") {
			t.Errorf("%s: want visible %q and the time zone, got %s", status, want, body)
		}
	}
}
