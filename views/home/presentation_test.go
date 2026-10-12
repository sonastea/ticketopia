package home

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sonastea/ticketopia/internal/models"
	"golang.org/x/net/html"
)

func TestRenderedEventFactsShowPricesAndUnknownDetails(t *testing.T) {
	min, max := "20", "40"
	date, clock := "2026-10-12", "19:30:00"
	known := models.EventStart{LocalDate: &date, LocalTime: &clock}
	for _, tt := range []struct {
		name   string
		prices []models.PriceRange
		start  models.EventStart
		price  string
		want   []string
		absent string
	}{
		{"range", []models.PriceRange{{Currency: "USD", Min: &min, Max: &max}}, known, "USD 20 - 40", []string{"Mon, Oct 12, 2026 · 7:30 PM"}, "Price not listed"},
		{"equal bounds", []models.PriceRange{{Currency: "USD", Min: &min, Max: &min}}, known, "USD 20", nil, "USD 20 - 20"},
		{"minimum only", []models.PriceRange{{Currency: "USD", Min: &min}}, known, "USD 20", nil, "Price not listed"},
		{"unknown", nil, models.EventStart{}, "Price not listed", []string{"Event date to be announced", "Venue to be announced"}, "7:30 PM"},
		{"incomplete price", []models.PriceRange{{Min: &min}, {Currency: "USD"}}, known, "Price not listed", nil, "USD 20"},
		{"date TBA overrides supplied date", nil, models.EventStart{LocalDate: &date, LocalTime: &clock, DateTBA: true}, "Price not listed", []string{"Event date to be announced"}, "Mon, Oct 12, 2026"},
		{"time TBA overrides supplied time", nil, models.EventStart{LocalDate: &date, LocalTime: &clock, TimeTBA: true}, "Price not listed", []string{"Mon, Oct 12, 2026 · Time to be announced"}, "7:30 PM"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			page := EventPage{Detail: models.EventDetail{Item: models.Event{PriceRanges: tt.prices, Start: tt.start}}}
			var out bytes.Buffer
			if err := EventHeadingFacts(page).Render(t.Context(), &out); err != nil {
				t.Fatal(err)
			}
			doc, err := html.Parse(strings.NewReader(out.String()))
			if err != nil {
				t.Fatal(err)
			}
			var price string
			var walk func(*html.Node, bool)
			walk = func(n *html.Node, summary bool) {
				for _, attr := range n.Attr {
					summary = summary || (attr.Key == "aria-label" && attr.Val == "Price and sale status")
				}
				if summary && n.Type == html.TextNode {
					price += n.Data
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					walk(child, summary)
				}
			}
			walk(doc, false)
			if got := strings.TrimSpace(strings.TrimSuffix(price, "Sale status unknown")); got != tt.price {
				t.Errorf("rendered price = %q, want %q", got, tt.price)
			}
			for _, want := range tt.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("rendered facts omitted %q: %s", want, out.String())
				}
			}
			if strings.Contains(out.String(), tt.absent) {
				t.Errorf("rendered facts misrepresented unavailable details: %s", out.String())
			}
		})
	}
}
