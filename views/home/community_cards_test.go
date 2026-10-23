package home

import (
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
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
	for _, want := range []string{`class="recommendation-list community-cards"`, `class="recommendation-event"`, `<div class="date-marker" aria-hidden="true"><span>Nov</span><strong>7</strong>`, `<blockquote class="recommendation-reason">&lt;b&gt;Go early&lt;/b&gt;</blockquote>`, `<span class="avatar" aria-hidden="true">AL</span>`, `<span class="avatar" aria-hidden="true">G</span>`, `aria-label="Price and sale status"`} {
		if !strings.Contains(markup, want) {
			t.Errorf("community card omitted %s", want)
		}
	}
	if strings.Count(markup, "<blockquote") != 1 {
		t.Error("a recommendation without a reason rendered an empty quote")
	}
	if quote, facts := strings.Index(markup, "<blockquote"), strings.Index(markup, "Price and sale status"); quote > facts {
		t.Error("event facts should follow the recommenders' words")
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
	if !strings.Contains(markup, `data-live-empty-state hidden`) {
		t.Error("the empty display line showed beside activity")
	}
}
