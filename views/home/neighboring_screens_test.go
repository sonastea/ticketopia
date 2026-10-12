package home

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/follows"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
)

func renderNeighbor(t *testing.T, component templ.Component) string {
	t.Helper()
	var out bytes.Buffer
	if err := component.Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestPublicCollectionIdentityOpensOverview(t *testing.T) {
	event := models.Event{ID: "ticketmaster:identity", Name: "Event with <long> title"}
	origin := "/users/owner?limit=1"
	profile := accounts.PublicProfile{ID: "owner", DisplayName: "Public author"}
	for name, component := range map[string]templ.Component{
		"recommendations": RecommendationList(recommendations.List{Items: []recommendations.Item{{Event: event, Profile: profile, Reason: "A public reason"}}}, true, origin),
		"community":       CommunityRecommendationList(recommendations.Feed{Items: []recommendations.EventGroup{{Event: event, Count: 1}}}, origin),
		"conversations":   DiscussionRoots(discussions.List{Items: []discussions.Post{{ID: "root", EventID: event.ID, Event: event, Body: "A practical question", Profile: &profile}}}, true, origin),
		"public interest": PublicInterestEvents(interests.List{Items: []interests.Item{{Event: event}}}, origin),
	} {
		t.Run(name, func(t *testing.T) {
			body := renderNeighbor(t, component)
			href := `href="` + eventURL(event.ID, "overview", origin) + `"`
			if strings.Count(body, href) != 2 {
				t.Fatalf("artwork/title must share Overview and return context: %s", body)
			}
			if !strings.Contains(body, `aria-label="Overview — Event with &lt;long&gt; title"`) {
				t.Fatal("missing accessible artwork identity")
			}
			for _, forbidden := range []string{"data-save-form", "data-interest-form", "ticket-link"} {
				if strings.Contains(body, forbidden) {
					t.Fatal("public person row repeats private/ticket toolbar")
				}
			}
			if name == "conversations" && (!strings.Contains(body, "Open conversation") || !strings.Contains(body, "Questions &amp; tips") || !strings.Contains(body, "/discussions/root")) {
				t.Fatal("lost direct section/thread entry")
			}
			if name == "recommendations" && !strings.Contains(body, "Recommendations</a>") {
				t.Fatal("lost direct recommendation entry")
			}
		})
	}
}

func TestFollowSearchDisclosureStates(t *testing.T) {
	collection := follows.List{Items: []follows.Item{{}}}
	for _, tc := range []struct {
		name string
		page FollowsPage
		open bool
	}{
		{"first use", FollowsPage{Enabled: true}, true},
		{"revisit", FollowsPage{Enabled: true, List: collection}, false},
		{"submitted empty", FollowsPage{Enabled: true, List: collection, Searched: true}, true},
		{"keyword", FollowsPage{Enabled: true, List: collection, Keyword: "Artist"}, true},
		{"search error", FollowsPage{Enabled: true, List: collection, SearchError: "Unavailable"}, true},
		{"collection error", FollowsPage{Enabled: true, List: collection, Error: "Unavailable"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.page.SearchOpen() != tc.open {
				t.Fatal("incorrect search state")
			}
			body := renderNeighbor(t, Follows(tc.page))
			if strings.Contains(body, `class="follow-search" open`) != tc.open {
				t.Fatal("native disclosure disagrees")
			}
			if !strings.Contains(body, `method="get" action="/follows"`) || !strings.Contains(body, "Following doesn't publish interest") {
				t.Fatal("lost search/private controls")
			}
		})
	}
}

func TestReplyRecordsKeepAvailabilityAndDistinctConversations(t *testing.T) {
	at := time.Date(2026, 10, 11, 13, 42, 0, 0, time.UTC)
	p := DiscussionFollowPage{Notifications: true, URL: "/me/notifications", Inbox: discussionfollows.Page[discussionfollows.Notification]{Items: []discussionfollows.Notification{
		{ID: "one", EventName: "Same event", ReplyCount: 3, AvailableAt: at, URL: "/events/e/discussions/one?post_id=r#post-r"},
		{ID: "two", EventName: "Same event", ReplyCount: 1, AvailableAt: at.Add(time.Minute), Read: true, URL: "/events/e/discussions/two?post_id=s#post-s"},
	}}}
	body := renderNeighbor(t, FollowedDiscussions(p))
	for _, want := range []string{`data-notification-read="false"`, `data-notification-read="true"`, `<strong>Unread</strong>`, "Available <time", "13:42 UTC", "13:43 UTC", "3</strong>", "1</strong>", `href="/events/e/discussions/one?post_id=r#post-r"`, `href="/events/e/discussions/two?post_id=s#post-s"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s", want)
		}
	}
	if strings.Count(body, "Open conversation at reply") != 2 || strings.Count(body, "Mark read</button>") != 1 {
		t.Fatal("collapsed distinct conversations or changed read action")
	}
}

func TestContributionsKeepHiddenTextAndComposerBoundaries(t *testing.T) {
	profile := accounts.PublicProfile{ID: "owner", DisplayName: "Private author"}
	for _, post := range []discussions.Post{{ID: "removed", Removed: true, Body: "secret removed", Profile: &profile}, {ID: "hidden", Hidden: true, Body: "secret hidden", Profile: &profile}} {
		body := renderNeighbor(t, DiscussionPost(post, DiscussionView{ViewerID: "viewer", ModerationEnabled: true}, "/"))
		if strings.Contains(body, "secret") || strings.Contains(body, "Private author") || strings.Contains(body, "data-discussion-draft") {
			t.Fatal("placeholder exposed text/author/composer")
		}
	}
	body := renderNeighbor(t, DiscussionComposer(DiscussionForm{EventID: "e", ViewerID: "owner", Key: "preserved-key", ParentID: "target", CSRF: "csrf", Action: "create"}, "Your reply", "Publish reply"))
	if strings.Count(body, "<textarea") != 1 || strings.Count(body, "<button") != 1 || !strings.Contains(body, `name="parent_id" value="target"`) || !strings.Contains(body, `name="idempotency_key" value="preserved-key"`) {
		t.Fatal("composer controls/draft metadata changed")
	}
	if strings.Index(body, "contribution will be public") > strings.Index(body, "Publish reply") {
		t.Fatal("public audience must be explained beside the publishing action")
	}
}
