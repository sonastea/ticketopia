package home

import (
	"bytes"
	"net/url"
	"slices"
	"strconv"
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
	"golang.org/x/net/html"
)

func renderNeighbor(t *testing.T, component templ.Component) string {
	t.Helper()
	var out bytes.Buffer
	if err := component.Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func neighborElements(root *html.Node, tag string) []*html.Node {
	var nodes []*html.Node
	var visit func(*html.Node)
	visit = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == tag {
			nodes = append(nodes, n)
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			visit(child)
		}
	}
	visit(root)
	return nodes
}

func neighborAttributes(n *html.Node) map[string]string {
	attrs := map[string]string{}
	for _, a := range n.Attr {
		attrs[a.Key] = a.Val
	}
	return attrs
}

func neighborText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var text strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(neighborText(child))
	}
	return text.String()
}

func neighborDocument(t *testing.T, markup string) *html.Node {
	t.Helper()
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestPublicCollectionIdentityOpensOverview(t *testing.T) {
	event := models.Event{ID: "ticketmaster:identity", Name: "Event with <long> title"}
	origin := "/users/owner?limit=1"
	profile := accounts.PublicProfile{ID: "owner", DisplayName: "Public author"}
	for name, component := range map[string]templ.Component{
		"recommendations": RecommendationList(recommendations.List{Items: []recommendations.Item{{Event: event, Profile: profile, Reason: "A public reason"}}}, listContext{Place: onProfile}, origin),
		"community":       CommunityRecommendationList(recommendations.Feed{Items: []recommendations.EventGroup{{Event: event, Count: 1}}}, origin, listContext{Place: inCommunity}),
		"conversations":   DiscussionRoots(discussions.List{Items: []discussions.Post{{ID: "root", EventID: event.ID, Event: event, Body: "A practical question", Profile: &profile}}}, listContext{Place: inCommunity}, origin),
		"public interest": PublicInterestEvents(interests.List{Items: []interests.Item{{Event: event}}}, origin),
	} {
		t.Run(name, func(t *testing.T) {
			body := renderNeighbor(t, component)
			doc := neighborDocument(t, body)
			var artwork, title, section, conversation string
			linkCounts := map[string]int{}
			for _, link := range neighborElements(doc, "a") {
				attrs := neighborAttributes(link)
				linkCounts[attrs["href"]]++
				if slices.Contains(strings.Fields(attrs["class"]), "event-image-link") {
					artwork = attrs["href"]
					if attrs["aria-label"] != "Overview — "+event.Name {
						t.Fatal("artwork lost its accessible event identity")
					}
				}
				if link.Parent.Data == "h3" && neighborText(link) == event.Name {
					title = attrs["href"]
				}
				if text := neighborText(link); text == "Recommendations" || text == "Questions & tips" {
					section = attrs["href"]
				} else if strings.HasPrefix(neighborText(link), "Open conversation") {
					conversation = attrs["href"]
				}
			}
			if artwork == "" || artwork != title || linkCounts[title] != 2 {
				t.Fatalf("artwork/title must share an Overview destination: %q / %q", artwork, title)
			}
			assertNeighborURL(t, title, "/events/"+event.ID, "", origin)
			for _, forbidden := range []string{"data-save-form", "data-interest-form", "ticket-link"} {
				if strings.Contains(body, forbidden) {
					t.Fatal("public person row repeats private/ticket toolbar")
				}
			}
			if name == "conversations" {
				assertNeighborURL(t, section, "/events/"+event.ID, "discussion", origin)
				assertNeighborURL(t, conversation, "/events/"+event.ID+"/discussions/root", "", origin)
			}
			if name == "recommendations" || name == "community" {
				assertNeighborURL(t, section, "/events/"+event.ID, "community", origin)
			}
		})
	}
}

func TestCollectionPrivacyAndFreshnessRemainVisible(t *testing.T) {
	for _, visibility := range []string{"private", "public"} {
		view := InterestView{Enabled: true, SignedIn: true, States: map[string]interests.State{"e": {Interested: true, Visibility: visibility}}}
		markup := renderNeighbor(t, InterestControl("e", "Event", view, "/me/interests?cursor=next"))
		label := "Remove interest · " + interestLabel(visibility)
		if !strings.Contains(markup, label) || !strings.Contains(markup, `aria-label="`+label+` —`) {
			t.Fatal("collection lost visible privacy or label-in-name", markup)
		}
	}
	meta := models.Freshness{DataAsOf: time.Now().UTC(), Stale: true}
	markup := renderNeighbor(t, CommunityRecommendationList(recommendations.Feed{Items: []recommendations.EventGroup{{Event: models.Event{ID: "e", Status: "onsale"}, Meta: meta, Count: 1}}}, "/community", listContext{Place: inCommunity}))
	warning, facts := strings.Index(markup, "Showing last-known details"), strings.Index(markup, "Price and sale status")
	if warning < 0 || warning > facts || !strings.Contains(markup, "is-stale") {
		t.Fatal("stale warning must precede sale facts", markup)
	}
}

func assertNeighborURL(t *testing.T, raw, path, section, origin string) {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path != path || u.Query().Get("section") != section || u.Query().Get("return_to") != origin {
		t.Fatalf("destination %q: want local %s, section=%q, return_to=%q", raw, path, section, origin)
	}
}

// Pilot collections are mostly empty: copy must fit where it renders, offer
// the action available there, and say "on this page" only after the first page.
func TestEmptyStatesMatchTheirContext(t *testing.T) {
	zero := 0
	none := recommendations.List{Count: &zero}
	for _, tc := range []struct {
		name      string
		component templ.Component
		want      []string
		link      string
	}{
		{"event questions, signed out", DiscussionPanel("e", DiscussionView{Enabled: true}, "/events/e?section=discussion"), []string{"No questions or tips yet", "Sign in to ask the first question"}, ""},
		// Discover's own results cursor is not a conversation page.
		{"preview questions, signed in", DiscussionPanel("e", DiscussionView{Enabled: true, ViewerID: "v"}, "/?city=Berlin&cursor=results&selected_event=e"), []string{"No questions or tips yet", "Ask the first question or share a tip"}, ""},
		{"later event questions", DiscussionPanel("e", DiscussionView{Enabled: true}, "/events/e/discussions?cursor=next&return_to=%2F"), []string{"No more conversations on this page"}, "/events/e/discussions?return_to=%2F"},
		{"event recommendations", RecommendationPanel("e", RecommendationView{Enabled: true, List: none}, "/events/e?section=community"), []string{"No one has recommended this event yet", `data-live-empty="e"`, "data-live-total"}, ""},
		{"community recommendations", CommunityRecommendationList(recommendations.Feed{}, "/community", communityContext("/community", "", "", "")), []string{"No recommendations yet", "Find an event to recommend"}, "/"},
		{"filtered recommendations", CommunityRecommendationList(recommendations.Feed{}, "/community?city=Berlin", communityContext("/community?city=Berlin", "Berlin", "", "")), []string{"No recommended events match these filters", "Reset filters"}, "/community"},
		{"later recommendations", CommunityRecommendationList(recommendations.Feed{}, "/community?city=Berlin&cursor=next", communityContext("/community?city=Berlin&cursor=next", "Berlin", "", "")), []string{"No more recommendations on this page"}, "/community?city=Berlin"},
		{"community conversations", DiscussionRoots(discussions.List{}, communityContext("/community/discussions", "", "", ""), "/community/discussions"), []string{"No conversations yet", "Find an event to ask about"}, "/"},
		{"profile recommendations", PublicRecommendationEvents(recommendations.List{}, "/users/owner", ""), []string{"No public recommendations yet"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := renderNeighbor(t, tc.component)
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Fatalf("missing %q: %s", want, body)
				}
			}
			for _, p := range neighborElements(neighborDocument(t, body), "p") {
				if _, total := neighborAttributes(p)["data-live-total"]; total {
					if _, hidden := neighborAttributes(p)["hidden"]; !hidden {
						t.Fatal("a zero count repeats its empty state")
					}
				}
			}
			later := strings.Contains(tc.name, "later")
			if strings.Contains(body, "Open an event") || strings.Contains(body, "0 visible") || strings.Contains(body, "on this page") != later {
				t.Fatal("empty state is generic or misstates pagination", body)
			}
			if tc.link != "" && !slices.ContainsFunc(neighborElements(neighborDocument(t, body), "a"), func(n *html.Node) bool { return neighborAttributes(n)["href"] == tc.link }) {
				t.Fatalf("empty state lost its %s action: %s", tc.link, body)
			}
		})
	}
}

func TestResultsFreshnessIsOneQuietLine(t *testing.T) {
	meta := models.Freshness{DataAsOf: time.Now().UTC(), Stale: true, Coverage: &models.Coverage{Status: "partial"}}
	body := renderNeighbor(t, ResultsFreshness(meta))
	if strings.Count(body, "<details") != 1 || !strings.Contains(body, "<summary>Last-known, partial results</summary>") || !strings.Contains(body, `data-coverage="partial"`) || strings.Contains(body, "role=") || !strings.Contains(body, "Showing last-known results") || !strings.Contains(body, "Partial coverage") || !strings.Contains(body, "Updated ") {
		t.Fatal("a results warning must be one non-live summary line with its details behind a disclosure", body)
	}
	// Uncollected dates sit above shown results too; the summary must not read as "nothing here".
	body = renderNeighbor(t, ResultsFreshness(models.Freshness{Coverage: &models.Coverage{Status: "not_collected"}}))
	if !strings.Contains(body, "<summary>Results may be incomplete</summary>") || strings.Contains(body, "empty search") {
		t.Fatal("uncollected coverage must say results may be incomplete", body)
	}
	body = renderNeighbor(t, ResultsFreshness(models.Freshness{DataAsOf: time.Now().UTC(), Coverage: &models.Coverage{Status: "complete"}}))
	if strings.Contains(body, "<details") || strings.Count(body, "<p") != 1 || !strings.Contains(body, "Updated ") {
		t.Fatal("ordinary freshness must stay a plain line", body)
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
			body := renderNeighbor(t, Follows(tc.page))
			doc := neighborDocument(t, body)
			searches := neighborElements(doc, "details")
			// Filter to search rather than depend on attribute serialization order.
			searches = slices.DeleteFunc(searches, func(n *html.Node) bool {
				return !slices.Contains(strings.Fields(neighborAttributes(n)["class"]), "follow-search")
			})
			if len(searches) != 1 {
				t.Fatal("expected one native search disclosure")
			}
			_, open := neighborAttributes(searches[0])["open"]
			if open != tc.open {
				t.Fatal("native disclosure disagrees")
			}
			forms := neighborElements(searches[0], "form")
			if len(forms) == 0 || neighborAttributes(forms[0])["method"] != "get" || neighborAttributes(forms[0])["action"] != "/follows" || !strings.Contains(body, "Following doesn't publish interest") {
				t.Fatal("lost search/private controls")
			}
		})
	}
}

func TestReplyRecordsKeepAvailabilityAndDistinctConversations(t *testing.T) {
	at := time.Date(2026, 10, 11, 15, 42, 0, 0, time.FixedZone("CEST", 2*60*60))
	p := DiscussionFollowPage{Notifications: true, CSRF: "viewer-csrf", URL: "/me/notifications?limit=2", Inbox: discussionfollows.Page[discussionfollows.Notification]{Items: []discussionfollows.Notification{
		{ID: "one", EventName: "Same event", ReplyCount: 3, AvailableAt: at, URL: "/events/e/discussions/one?post_id=r#post-r"},
		{ID: "two", EventName: "Same event", ReplyCount: 1, AvailableAt: at.Add(time.Minute), Read: true, URL: "/events/e/discussions/two?post_id=s#post-s"},
	}}}
	body := renderNeighbor(t, FollowedDiscussions(p))
	doc := neighborDocument(t, body)
	records := slices.DeleteFunc(neighborElements(doc, "li"), func(n *html.Node) bool {
		_, record := neighborAttributes(n)["data-notification-read"]
		return !record
	})
	if len(records) != len(p.Inbox.Items) {
		t.Fatal("notifications for distinct conversations were collapsed")
	}
	for i, item := range p.Inbox.Items {
		record := records[i]
		text := strings.Join(strings.Fields(neighborText(record)), " ")
		state, count := "Unread", "3 new replies"
		if item.Read {
			state, count = "Read", "1 new reply"
		}
		if !strings.Contains(text, count+" · "+state) || neighborAttributes(record)["data-notification-read"] != strconv.FormatBool(item.Read) {
			t.Fatalf("notification %s lost its own count/read state: %s", item.ID, text)
		}
		times := neighborElements(record, "time")
		if len(times) != 1 || neighborAttributes(times[0])["datetime"] != item.AvailableAt.Format(time.RFC3339) || neighborText(times[0]) != item.AvailableAt.UTC().Format("Jan 2, 2006 · 15:04 UTC") || !strings.Contains(neighborText(times[0].Parent), "Available ") {
			t.Fatalf("notification %s lost actual availability/UTC labeling", item.ID)
		}
		links := neighborElements(record, "a")
		if len(links) != 1 || neighborAttributes(links[0])["href"] != item.URL || strings.TrimSpace(neighborText(links[0])) != "Open conversation at reply" {
			t.Fatalf("notification %s no longer opens its exact reply", item.ID)
		}
		forms := neighborElements(record, "form")
		if item.Read {
			if len(forms) != 0 {
				t.Fatal("read notification still offers Mark read")
			}
			continue
		}
		if len(forms) != 1 || neighborAttributes(forms[0])["method"] != "post" || neighborAttributes(forms[0])["action"] != "/discussion-follow-actions" {
			t.Fatal("unread notification lost its native read form")
		}
		fields := map[string]string{}
		for _, input := range neighborElements(forms[0], "input") {
			attrs := neighborAttributes(input)
			fields[attrs["name"]] = attrs["value"]
		}
		if fields["notification_id"] != item.ID || fields["return_to"] != p.URL || fields["csrf_token"] != p.CSRF {
			t.Fatal("Mark read targets the wrong notification, return page or viewer credential")
		}
		buttons := neighborElements(forms[0], "button")
		if len(buttons) != 1 || neighborAttributes(buttons[0])["name"] != "action" || neighborAttributes(buttons[0])["value"] != "read" || strings.TrimSpace(neighborText(buttons[0])) != "Mark read" {
			t.Fatal("unread notification lost its explicit Mark read action")
		}
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
	submissions := 0
	for _, button := range neighborElements(neighborDocument(t, body), "button") {
		if neighborAttributes(button)["type"] == "submit" {
			submissions++
		}
	}
	if strings.Count(body, "<textarea") != 1 || submissions != 1 || !strings.Contains(body, `name="parent_id" value="target"`) || !strings.Contains(body, `name="idempotency_key" value="preserved-key"`) {
		t.Fatal("composer controls/draft metadata changed")
	}
	audience, publish := strings.Index(body, "contribution will be public"), strings.Index(body, "Publish reply")
	if audience < 0 || publish < 0 || audience > publish {
		t.Fatal("public audience must be explained beside the publishing action")
	}
}
