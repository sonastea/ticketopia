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

// Transition names bypass templ's style sanitizer, so provider IDs must never
// add declarations or produce an invalid identifier.
func TestTransitionStyleEscapesProviderIDs(t *testing.T) {
	for id, want := range map[string]string{
		"vvG1zZ9ZK0Nr-3_X":     "view-transition-name:event-art-vvG1zZ9ZK0Nr-3_X",
		"a;color:red\"></div>": "view-transition-name:event-art-a_3bcolor_3ared_22_3e_3c_2fdiv_3e",
		"é":                    "view-transition-name:event-art-_e9",
		"":                     "",
	} {
		if got := string(transitionStyle("art", id)); got != want {
			t.Errorf("transitionStyle(%q) = %q, want %q", id, got, want)
		}
	}
}

// Stored tints bypass the style sanitizer too; only #rrggbb reaches CSS.
func TestTintStyleAcceptsOnlyHexColours(t *testing.T) {
	for tint, want := range map[string]string{
		"#3a7bd5":           "--event-tint:#3a7bd5",
		"":                  "",
		"#3A7BD5":           "",
		"red":               "",
		"#3a7bd5;color:red": "",
		"#12345g":           "",
		"url(x)":            "",
	} {
		if got := string(tintStyle(models.Event{Tint: tint})); got != want {
			t.Errorf("tintStyle(%q) = %q, want %q", tint, got, want)
		}
	}
}

func TestNoticeListSplitsOnlyProviderBullets(t *testing.T) {
	for _, tt := range []struct {
		name, text, intro string
		items             []string
	}{
		{"leading bullets", "- No re-entry. - Bags must be clear.", "", []string{"No re-entry.", "Bags must be clear."}},
		{"lead-in", "Please note: - No cameras - All ages\n- Doors at 7", "Please note:", []string{"No cameras", "All ages", "Doors at 7"}},
		{"one bullet stays prose", "Please note: - No cameras", "", nil},
		{"dashes in prose stay prose", "Doors 7pm - 11pm - all ages", "", nil},
		{"plain prose", "Bring photo ID.", "", nil},
		{"hyphenated words", "Re-entry is not-permitted. - First - Second", "Re-entry is not-permitted.", []string{"First", "Second"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			intro, items := noticeList(tt.text)
			if intro != tt.intro || strings.Join(items, "|") != strings.Join(tt.items, "|") {
				t.Fatalf("noticeList(%q) = %q, %q", tt.text, intro, items)
			}
		})
	}
}

func TestEventOverviewLinksVenueToMapsAndOffersShare(t *testing.T) {
	event := models.Event{ID: "ticketmaster:Maps", Name: "Local show", PleaseNote: "- Clear bags only - No re-entry", Place: &models.Place{Name: "Local hall", Address: "12 Main Street", City: "Boston", PostalCode: "02110"}}
	var out bytes.Buffer
	if err := Event(EventPage{Detail: models.EventDetail{Item: event}, Section: "overview", ReturnURL: "/?city=Boston"}).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	markup := out.String()
	for _, want := range []string{
		`href="https://www.google.com/maps/search/?api=1&amp;query=Local+hall%2C+12+Main+Street%2C+Boston%2C+02110"`,
		`data-maps-query="Local hall, 12 Main Street, Boston, 02110"`,
		`<li>Clear bags only</li><li>No re-entry</li>`,
		`data-share-url="/events/ticketmaster:Maps"`,
		`aria-label="Share Local show"`,
	} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %s", want)
		}
	}
}

func TestEventOverviewOffersCalendarOnlyForAnnouncedDates(t *testing.T) {
	date := "2026-11-01"
	for _, tc := range []struct {
		start models.EventStart
		offer bool
	}{
		{models.EventStart{LocalDate: &date, NoSpecificTime: true}, true},
		{models.EventStart{LocalDate: &date, DateTBA: true}, false},
	} {
		var out bytes.Buffer
		event := models.Event{ID: "ticketmaster:Cal 1", Name: "Show", Start: tc.start}
		if err := Event(EventPage{Detail: models.EventDetail{Item: event}, Section: "overview", ReturnURL: "/"}).Render(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(out.String(), `href="/api/v1/events/ticketmaster:Cal%201/calendar.ics"`); got != tc.offer {
			t.Errorf("%+v: calendar link %v, want %v", tc.start, got, tc.offer)
		}
	}
}

func TestEventArtOffersOneCropSizedForItsFrame(t *testing.T) {
	image := func(name, ratio string, w, h int) models.Image {
		return models.Image{URL: "https://s1.ticketm.net/" + name + ".jpg", Ratio: ratio, Width: w, Height: h}
	}
	// Ticketmaster's usual set: several 16:9 and 3:2 renditions, one 4:3.
	images := []models.Image{
		image("portrait-16-9", "16_9", 640, 360), image("portrait-3-2", "3_2", 640, 427),
		image("detail", "16_9", 205, 115), image("artist", "3_2", 305, 203), image("custom", "4_3", 305, 225),
		image("tablet", "16_9", 1024, 576), image("tablet-3-2", "3_2", 1024, 683),
		image("retina", "16_9", 1136, 639), image("large", "16_9", 2048, 1152), image("dup", "16_9", 640, 360),
	}
	for _, tt := range []struct {
		name string
		slot artSlot
		want eventArt
	}{
		// A 3:2 crop covering a 5.5rem square must be 8.25rem wide.
		{"result rows take the tallest crop", artResult, eventArt{
			Src:    "https://s1.ticketm.net/portrait-3-2.jpg",
			SrcSet: "https://s1.ticketm.net/artist.jpg 305w, https://s1.ticketm.net/portrait-3-2.jpg 640w, https://s1.ticketm.net/tablet-3-2.jpg 1024w",
			Sizes:  "(max-width: 20rem) 100vw, (max-width: 28.5rem) 8.3rem, (max-width: 51rem) 16.5rem, 26rem",
		}},
		{"thumbnails cover their square", artThumb, eventArt{
			Src:    "https://s1.ticketm.net/portrait-3-2.jpg",
			SrcSet: "https://s1.ticketm.net/artist.jpg 305w, https://s1.ticketm.net/portrait-3-2.jpg 640w, https://s1.ticketm.net/tablet-3-2.jpg 1024w",
			Sizes:  "8.3rem",
		}},
		{"the detail banner takes the widest crop", artDetail, eventArt{
			Src:    "https://s1.ticketm.net/tablet.jpg",
			SrcSet: "https://s1.ticketm.net/detail.jpg 205w, https://s1.ticketm.net/portrait-16-9.jpg 640w, https://s1.ticketm.net/tablet.jpg 1024w, https://s1.ticketm.net/retina.jpg 1136w, https://s1.ticketm.net/large.jpg 2048w",
			Sizes:  "(max-width: 62rem) 100vw, 50rem",
		}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := artFor(images, tt.slot); got != tt.want {
				t.Errorf("artFor() =\n%+v\nwant\n%+v", got, tt.want)
			}
		})
	}
	t.Run("a single rendition offers no choice", func(t *testing.T) {
		got := artFor([]models.Image{image("only", "4_3", 305, 225), {Ratio: "16_9", Width: 640}}, artResult)
		if got != (eventArt{Src: "https://s1.ticketm.net/only.jpg"}) {
			t.Errorf("artFor() = %+v", got)
		}
		var out bytes.Buffer
		if err := EventImage(models.Event{Images: []models.Image{image("only", "4_3", 305, 225)}}, artResult).Render(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		if body := out.String(); !strings.Contains(body, `src="https://s1.ticketm.net/only.jpg"`) || strings.Contains(body, "srcset") || strings.Contains(body, "sizes") {
			t.Errorf("single rendition rendered %s", body)
		}
	})
}
