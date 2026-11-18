package home

import (
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
	"golang.org/x/net/html"
)

// Rendered chips are the URLs people follow, so parse them as the handler does.
func TestDateChipLinksParse(t *testing.T) {
	now := time.Now()
	filters := discovery.Query{
		City: "Los Angeles", Country: "US", Keyword: "jazz & blues", Sort: "name_asc", Limit: 40,
		CategoryID: "KZFzniwnSyZfZ7v7nJ", GenreID: "KnvZfZ7vAvE",
		ArtistID: "ticketmaster:Artist", VenueID: "ticketmaster:Venue",
		StartDate: "2000-11-01", EndDate: "2000-12-01",
	}
	paged := filters
	paged.Page, paged.Stored = 1, true
	paged.Cursor = &discovery.Cursor{Date: "2000-11-02", ID: "ticketmaster:Event"}
	markup := renderNeighbor(t, SearchControls(SearchPage{Filters: paged}))
	root, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var chips []*html.Node
	for _, nav := range neighborElements(root, "nav") {
		if neighborAttributes(nav)["aria-label"] == "Date shortcuts" {
			chips = append(chips, neighborElements(nav, "a")...)
		}
	}
	labels := []string{"Tonight", "This weekend", "Next 7 days"}
	if len(chips) != len(labels) {
		t.Fatalf("date chips = %d, want %d", len(chips), len(labels))
	}
	for i, chip := range chips {
		if got := neighborText(chip); got != labels[i] {
			t.Errorf("chip %d label = %q, want %q", i, got, labels[i])
		}
		link := neighborAttributes(chip)["href"]
		u, err := url.Parse(link)
		if err != nil || u.IsAbs() || u.Host != "" || u.Path != "/" {
			t.Fatalf("chip link %q: %v", link, err)
		}
		params := u.Query()
		if params.Has("cursor") || params.Get("start_date") == "" || params.Get("end_date") == "" {
			t.Errorf("%s must set both dates and drop the cursor: %s", labels[i], link)
		}
		got, err := discovery.ParseQuery(params, now)
		if err != nil {
			t.Fatalf("chip link %q: %v", link, err)
		}
		if got.StartDate == filters.StartDate || got.EndDate == filters.EndDate {
			t.Errorf("%s did not replace the date range: %s", labels[i], link)
		}
		got.StartDate, got.EndDate = filters.StartDate, filters.EndDate
		if got != filters {
			t.Errorf("%s changed other filters or paging: got %+v, want %+v", labels[i], got, filters)
		}
	}
	if !strings.Contains(markup, "Events in <strong>Los Angeles, US</strong>") {
		t.Error("command bar omitted the browsed city")
	}
}

func TestDateChipsUseBrowsedCityDay(t *testing.T) {
	// 05:30 UTC on Saturday is still Friday evening in Los Angeles.
	now := time.Date(2026, 10, 24, 5, 30, 0, 0, time.UTC)
	zone := "America/Los_Angeles"
	page := SearchPage{
		Filters: discovery.Query{City: "Los Angeles", StartDate: "2026-10-23", EndDate: "2026-10-23", Limit: 20, Sort: "date_asc"},
		Events:  models.EventList{Items: []models.Event{{Start: models.EventStart{Timezone: &zone}}}},
	}
	chips := dateChipsAt(page, now)
	if !chips[0].Current || chips[1].Current || chips[2].Current {
		t.Fatalf("current chips = %+v", chips)
	}
	if !strings.Contains(chips[1].Href, "start_date=2026-10-23") || !strings.Contains(chips[1].Href, "end_date=2026-10-25") {
		t.Errorf("weekend chip = %s", chips[1].Href)
	}
	// Without a known zone the server's UTC day is used.
	page.Events.Items = nil
	if chips := dateChipsAt(page, now); chips[0].Current || !strings.Contains(chips[0].Href, "start_date=2026-10-24") {
		t.Errorf("UTC fallback chips = %+v", chips)
	}
}
