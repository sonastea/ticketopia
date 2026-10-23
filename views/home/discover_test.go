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
	filters, err := discovery.ParseQuery(url.Values{"city": {"Los Angeles"}, "country": {"US"}, "keyword": {"jazz & blues"}, "category_id": {"all"}, "sort": {"date_desc"}}, now)
	if err != nil {
		t.Fatal(err)
	}
	markup := renderNeighbor(t, SearchControls(SearchPage{Filters: filters}))
	root, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		t.Fatal(err)
	}
	var links []string
	for _, nav := range neighborElements(root, "nav") {
		for _, a := range neighborElements(nav, "a") {
			for _, attr := range a.Attr {
				if attr.Key == "href" {
					links = append(links, attr.Val)
				}
			}
		}
	}
	if len(links) != 3 {
		t.Fatalf("date chips = %v", links)
	}
	for _, link := range links {
		u, err := url.Parse(link)
		if err != nil || u.Path != "/" {
			t.Fatalf("chip link %q: %v", link, err)
		}
		got, err := discovery.ParseQuery(u.Query(), now)
		if err != nil {
			t.Fatalf("chip link %q: %v", link, err)
		}
		if got.City != "Los Angeles" || got.Country != "US" || got.Keyword != "jazz & blues" || got.Sort != "date_desc" || got.StartDate == filters.StartDate && got.EndDate == filters.EndDate {
			t.Errorf("chip link %q parsed as %+v", link, got)
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
