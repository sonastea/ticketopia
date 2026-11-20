package home

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"golang.org/x/net/html"
)

func TestInitials(t *testing.T) {
	for name, want := range map[string]string{"Ada Lovelace": "AL", "  maya ": "M", "Jean de la Fontaine": "JF", "émile zola": "ÉZ", "": ""} {
		if got := initials(name); got != want {
			t.Errorf("initials(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestCommunityCardsQuoteReasonsWithFacts(t *testing.T) {
	date := "2026-11-07"
	event := models.Event{ID: "ticketmaster:Card", Name: "Night Market", Status: "onsale", Start: models.EventStart{LocalDate: &date}}
	feed := recommendations.Feed{Items: []recommendations.EventGroup{{
		Event: event, Count: 2, FirstRecommendedAt: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
		Recommendations: []recommendations.Item{
			{Profile: accounts.PublicProfile{ID: "a", DisplayName: "Ada Lovelace"}, Reason: "<b>Go early</b>"},
			{Profile: accounts.PublicProfile{ID: "b", DisplayName: "Grace"}},
		},
	}}}
	markup := renderNeighbor(t, CommunityRecommendationList(feed, "/community", listContext{Place: inCommunity}))
	doc := neighborDocument(t, markup)
	// The reason is quoted as plain text, and only where one was given.
	quotes := neighborElements(doc, "blockquote")
	if len(quotes) != 1 {
		t.Fatalf("quotes = %d; a recommendation without a reason must not render an empty quote", len(quotes))
	}
	if quotes[0].FirstChild == nil || quotes[0].FirstChild.NextSibling != nil || quotes[0].FirstChild.Type != html.TextNode || quotes[0].FirstChild.Data != "<b>Go early</b>" {
		t.Errorf("reason was not quoted as escaped text: %q", neighborText(quotes[0]))
	}
	// Decorative date and initials stay out of the accessibility tree.
	var decorative []string
	for n := range doc.Descendants() {
		if n.Type == html.ElementNode && neighborAttributes(n)["aria-hidden"] == "true" {
			decorative = append(decorative, neighborText(n))
		}
	}
	for _, want := range []string{"Nov7", "AL", "G"} {
		if !slices.Contains(decorative, want) {
			t.Errorf("decorative text %q missing or exposed to assistive technology; hidden: %q", want, decorative)
		}
	}
	if quote, facts := strings.Index(markup, "<blockquote"), strings.Index(markup, `aria-label="Price and sale status"`); facts < 0 || quote > facts {
		t.Error("labelled event facts should follow the recommenders' words")
	}
}

func TestHappeningNowCardsUseStoredArtwork(t *testing.T) {
	maya, date := "Maya", "2026-11-07"
	asOf := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	page := HappeningNowPage{
		Enabled: true, ReturnURL: "/happening-now",
		Snapshot: activity.Snapshot{AsOf: asOf, Items: []activity.Item{
			{EventID: "ticketmaster:Stored", EventTitle: "Stored show", City: "Berlin", ActorFirstName: &maya, RecentRecommendations: 1, LatestRecommendedAt: asOf.Add(-2 * time.Minute)},
			{EventID: "ticketmaster:Missing", EventTitle: "Unstored show", RecentRecommendations: 3, LatestRecommendedAt: asOf},
		}},
		Events: map[string]models.Event{"ticketmaster:Stored": {ID: "ticketmaster:Stored", Start: models.EventStart{LocalDate: &date}, Images: []models.Image{{URL: "https://s1.ticketm.net/a.jpg", Width: 480}}}},
	}
	markup := renderNeighbor(t, HappeningNow(page))
	root, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var cards []*html.Node
	for _, ul := range neighborElements(root, "ul") {
		for _, attr := range ul.Attr {
			if attr.Key == "data-live-list" {
				cards = neighborElements(ul, "li")
			}
		}
	}
	if len(cards) != 2 {
		t.Fatalf("live cards = %d", len(cards))
	}
	for _, card := range cards {
		// Browser checks select [data-live-list] a; the title stays the only link.
		if links := neighborElements(card, "a"); len(links) != 1 {
			t.Errorf("live card has %d links", len(links))
		}
	}
	for _, want := range []string{`src="https://s1.ticketm.net/a.jpg"`, `<span>Nov</span><strong>7</strong>`, `data-live-avatar>M</span>`, `data-live-avatar>3</span>`, `data-live-meta>Berlin · 2 minutes ago</p>`, `data-live-meta>just now</p>`, `<template data-live-art>`} {
		if !strings.Contains(markup, want) {
			t.Errorf("Happening Now omitted %s", want)
		}
	}
	if strings.Count(markup, `class="date-marker"`) != 1 {
		t.Error("an event without a stored snapshot showed a date chip")
	}
	if !strings.Contains(markup, `data-stamp>Oct 23, 2026 · 12:00 UTC</time><span>. Refresh for the latest; live updates appear when connected.</span>`) {
		t.Error("the snapshot line lost its time or its refresh guidance")
	}
	if !strings.Contains(markup, `data-live-empty-state hidden`) {
		t.Error("the empty display line showed beside activity")
	}
}

// Agent-published recommendations name their app beside the author on the
// event, profile and Community views, as escaped plain text, never a link;
// a person's own recommendation shows no "via".
func TestRecommendationViaLabel(t *testing.T) {
	event := models.Event{ID: "ticketmaster:Via", Name: "Via Night"}
	agent := recommendations.Item{Event: event, Profile: accounts.PublicProfile{ID: "a", DisplayName: "Ada"}, Via: `<a href="x">Agent</a>`, ViaHost: "agent.example"}
	own := recommendations.Item{Event: event, Profile: accounts.PublicProfile{ID: "b", DisplayName: "Grace"}, Reason: "Mine"}
	list := recommendations.List{Items: []recommendations.Item{agent, own}}
	for name, markup := range map[string]string{
		"event":     renderNeighbor(t, RecommendationList(list, eventContext(event.ID), "/")),
		"profile":   renderNeighbor(t, RecommendationList(list, listContext{Place: onProfile}, "/users/a")),
		"community": renderNeighbor(t, CommunityRecommendationList(recommendations.Feed{Items: []recommendations.EventGroup{{Event: event, Count: 2, Recommendations: list.Items}}}, "/community", listContext{Place: inCommunity})),
	} {
		if strings.Count(markup, `class="recommendation-via"`) != 1 || !strings.Contains(markup, `<span class="recommendation-via">via &lt;a href=&#34;x&#34;&gt;Agent&lt;/a&gt; (agent.example)</span>`) {
			t.Errorf("%s view via label:\n%s", name, markup)
		}
	}
}

// A post published from an app's draft names the app beside the author in
// the thread and in Community, as escaped plain text; API-token drafts have
// no host, and the person's own posts show no "via".
func TestPostViaLabel(t *testing.T) {
	event := models.Event{ID: "ticketmaster:Via", Name: "Via Night"}
	profile := &accounts.PublicProfile{ID: "a", DisplayName: "Ada"}
	agent := discussions.Post{ID: "p1", ThreadID: "p1", EventID: event.ID, Event: event, Profile: profile, Body: "Cloakroom?", Via: `<b>Agent</b>`, ViaHost: "agent.example"}
	token := discussions.Post{ID: "p2", ThreadID: "p2", EventID: event.ID, Event: event, Profile: profile, Body: "Parking?", Via: "an API client"}
	own := discussions.Post{ID: "p3", ThreadID: "p3", EventID: event.ID, Event: event, Profile: profile, Body: "Mine"}
	list := discussions.List{Items: []discussions.Post{agent, token, own}}
	for name, markup := range map[string]string{
		"event":     renderNeighbor(t, DiscussionRoots(list, listContext{Place: inEvent, EventID: event.ID}, "/")),
		"community": renderNeighbor(t, DiscussionRoots(list, listContext{Place: inCommunity}, "/community/discussions")),
		"thread":    renderNeighbor(t, DiscussionPost(agent, DiscussionView{Enabled: true}, "/")) + renderNeighbor(t, DiscussionPost(token, DiscussionView{Enabled: true}, "/")) + renderNeighbor(t, DiscussionPost(own, DiscussionView{Enabled: true}, "/")),
	} {
		if strings.Count(markup, `class="post-via"`) != 2 || !strings.Contains(markup, `<span class="post-via">via &lt;b&gt;Agent&lt;/b&gt; (agent.example)</span>`) ||
			!strings.Contains(markup, `<span class="post-via">via an API client</span>`) {
			t.Errorf("%s view via label:\n%s", name, markup)
		}
	}
}
