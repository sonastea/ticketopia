package home

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/a-h/templ"
	"github.com/sonastea/ticketopia/internal/eventactivity"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
	"github.com/sonastea/ticketopia/views/components/button"
	"golang.org/x/net/html"
)

func TestComponentLinksRejectScriptURLs(t *testing.T) {
	var out bytes.Buffer
	if err := button.Button(button.Props{Href: "javascript:alert(1)"}).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "javascript:") {
		t.Fatal("component link bypassed templ URL sanitization")
	}
}

func TestEmptyCollectionsDistinguishLaterPages(t *testing.T) {
	for _, origin := range []string{"/saved", "/saved?limit=3", "/saved?limit=3&cursor=older"} {
		savedMarkup := renderNeighbor(t, Saved(SavedPage{ReturnURL: origin}))
		later := strings.Contains(origin, "cursor=")
		if strings.Contains(savedMarkup, "Earlier saves may still be in your collection") != later || strings.Contains(savedMarkup, "Keep something worth coming back to.") == later {
			t.Fatalf("empty Saved page misrepresents the whole collection: %s", origin)
		}
		interestOrigin := strings.Replace(origin, "/saved", "/me/interests", 1)
		interestMarkup := renderNeighbor(t, InterestCollection(InterestCollectionPage{ReturnURL: interestOrigin}))
		if strings.Contains(interestMarkup, "Earlier choices may still be in your collection") != later {
			t.Fatalf("empty Interests page lost recovery guidance: %s", interestOrigin)
		}
		for _, body := range []string{savedMarkup, interestMarkup} {
			doc := neighborDocument(t, body)
			found := false
			for _, link := range neighborElements(doc, "a") {
				if _, ok := neighborAttributes(link)["data-collection-recovery"]; ok {
					found = true
					if neighborAttributes(link)["href"] != "/saved" && neighborAttributes(link)["href"] != "/me/interests" {
						t.Fatal("empty-page recovery must return to its own newest collection")
					}
				}
			}
			if found != later {
				t.Fatal("only later empty pages should prioritize newest-page recovery", origin)
			}
		}
	}
}

func TestProviderSaleStatesStayExplicit(t *testing.T) {
	for status, want := range map[string]string{"onsale": "On sale", "offsale": "Off sale", "soldout": "Sold out", "sold_out": "Sold out", "canceled": "Canceled", "postponed": "Postponed", "rescheduled": "Rescheduled", "": "Sale status unknown"} {
		if got := statusLabel(status); got != want {
			t.Errorf("%q: got %q, want %q", status, got, want)
		}
	}
}

func TestEventSectionRegionOwnsOnlyContextArtwork(t *testing.T) {
	page := EventPage{Detail: models.EventDetail{Item: models.Event{ID: "ticketmaster:Region", Name: "Region event"}}, Section: "overview", ReturnURL: "/saved", ActionReturnURL: "/events/ticketmaster:Region?return_to=%2Fsaved"}
	for _, panel := range []bool{false, true} {
		var out bytes.Buffer
		if err := EventSectionRegion(page, panel).Render(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		markup := out.String()
		if strings.Contains(markup, `class="event-image`) != panel {
			t.Fatal("standalone identity artwork moved into section")
		}
		for _, want := range []string{"data-event-section-region", `data-event-id="ticketmaster:Region"`, `data-section="overview"`, `data-presentation="` + eventPresentation(panel) + `"`, "data-section-status", "data-section-body"} {
			if !strings.Contains(markup, want) {
				t.Fatal("missing fragment contract", want)
			}
		}
		if strings.Contains(markup, "event-primary-actions") || strings.Contains(markup, "event-section-nav") || strings.Contains(markup, "event-participation") {
			t.Fatal("section region contains persistent shell")
		}
	}
}

func TestEventRowOpensOverviewAndExposesTickets(t *testing.T) {
	var out bytes.Buffer
	event := models.Event{ID: "ticketmaster:Details", Name: "Named event"}
	event.Source.URL = "https://www.ticketmaster.com/example"
	if err := EventRows([]models.Event{event}, "/", "", SaveView{}, "/", InterestView{}).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	markup := out.String()
	if strings.Contains(markup, "@layouts.Icon") {
		t.Fatal("event row rendered an icon component as literal text")
	}
	_, link, found := strings.Cut(markup, `aria-label="Tickets on Ticketmaster for Named event">`)
	if !found {
		t.Fatal("missing direct ticket link")
	}
	link, _, _ = strings.Cut(link, "</a>")
	if !strings.Contains(link, "Tickets on Ticketmaster</span>") || !strings.Contains(link, "<svg") {
		t.Fatal("ticket link must render its label and SVG icon", link)
	}
	if strings.Count(markup, `href="/events/ticketmaster:Details"`) != 2 {
		t.Fatal("title and artwork must share the ordinary Overview link", markup)
	}
	for _, unwanted := range []string{"<details", "More event information", "View event details", "section=discussion", "Interested", "On-sale reminder"} {
		if strings.Contains(markup, unwanted) {
			t.Errorf("unexpected list control %q", unwanted)
		}
	}
}

func TestCollectionRowsKeepSaveIndependentAndRemindersInEventView(t *testing.T) {
	event := models.Event{ID: "ticketmaster:Saved", Name: "Saved show"}
	saves := SaveView{Enabled: true, SignedIn: true, Reminders: true, States: map[string]bool{event.ID: true}}
	interest := InterestView{Enabled: true, SignedIn: true, States: map[string]interests.State{event.ID: {Interested: true, Visibility: "private"}}}
	for _, origin := range []string{"/", "/saved", "/me/interests", "/radar"} {
		t.Run(origin, func(t *testing.T) {
			var out bytes.Buffer
			if err := EventRows([]models.Event{event}, origin, "", saves, origin, interest).Render(t.Context(), &out); err != nil {
				t.Fatal(err)
			}
			markup := out.String()
			if !strings.Contains(markup, "data-save-form") || strings.Contains(markup, "On-sale reminder") {
				t.Fatal("list lost Save or exposed reminder settings")
			}
			if strings.Contains(markup, "data-interest-form") != (origin == "/me/interests") {
				t.Fatal("interest management belongs only in its collection")
			}
		})
	}
	var out bytes.Buffer
	if err := EventActions(event.ID, event.Name, saves, "/").Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "On-sale reminder") {
		t.Fatal("event view lost eligible reminders")
	}
}

func TestInterestedNamesIncludeVisibleLabelAndCountStaysSeparate(t *testing.T) {
	id := "ticketmaster:InterestName"
	for _, visibility := range []string{"private", "public"} {
		var out bytes.Buffer
		view := InterestView{Enabled: true, SignedIn: true, CSRF: "fixture", DefaultVisibility: "private", States: map[string]interests.State{id: {Interested: true, Visibility: visibility, Count: 12}}}
		if err := InterestControl(id, "Named event", view, "/").Render(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		want := "Interested · " + interestLabel(visibility)
		if !strings.Contains(out.String(), `aria-label="`+want+` — Remove interest for Named event"`) || !strings.Contains(out.String(), `aria-pressed="true"`) {
			t.Fatal("selected action lacks visible label in accessible name", out.String())
		}
		out.Reset()
		if err := InterestDetails(id, view, "/").Render(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), ">12</strong> people interested") {
			t.Fatal("aggregate confused with own choice")
		}
		if strings.Contains(out.String(), "Interest visibility:") || strings.Contains(out.String(), "data-interest-visibility") || strings.Contains(out.String(), " open") {
			t.Fatal("privacy state repeated or editor open by default")
		}
		if !strings.Contains(out.String(), "Change visibility</summary>") || !strings.Contains(out.String(), `name="interest-privacy"`) {
			t.Fatal("missing compact exclusive visibility disclosure")
		}
	}
	var out bytes.Buffer
	if err := InterestDetails(id, InterestView{Enabled: true, Error: "Temporarily unavailable"}, "/").Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "data-interest-count") {
		t.Fatal("failed state fabricated zero")
	}
}

func TestSearchComponentsKeepNativeFormContract(t *testing.T) {
	var out bytes.Buffer
	if err := SearchControls(SearchPage{NeedsLocation: true}).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{`action="/" method="get"`, `<details id="filters"`, `data-slot="native-collapsible"`, `<select id="category" name="category_id"`, `<select id="genre" name="genre_id" disabled`, `name="genre_category_id"`, `type="submit"`, `for="city"`, `autocomplete="address-level2"`, `data-slot="input"`} {
		if !strings.Contains(html, want) {
			t.Errorf("missing native search contract %q", want)
		}
	}
}

func TestPartialStoredPaginationDoesNotClaimProviderCap(t *testing.T) {
	for _, stored := range []bool{true, false} {
		page := SearchPage{Events: models.EventList{Limited: true}}
		if stored {
			page.Events.Meta.Coverage = &models.Coverage{Status: "partial", TotalDays: 3}
		}
		var out bytes.Buffer
		if err := Pagination(page).Render(t.Context(), &out); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "Coverage is partial") != stored || strings.Contains(out.String(), "search limit") == stored {
			t.Fatal("partial collection confused with provider cap", out.String())
		}
	}
}

func TestEmptyPreviewCanReceiveKeyboardFocus(t *testing.T) {
	var out bytes.Buffer
	if err := Index(SearchPage{NeedsLocation: true}).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `id="event-context" class="event-context" aria-label="Event preview" tabindex="0"`) {
		t.Fatal("bounded preview must be keyboard-scrollable even without interactive event content")
	}
}

func TestEventContextOpensRequestedSectionWithoutInformationDisclosure(t *testing.T) {
	for _, section := range []string{"overview", "discussion", "community"} {
		t.Run(section, func(t *testing.T) {
			var out bytes.Buffer
			page := EventPage{Detail: models.EventDetail{Item: models.Event{ID: "ticketmaster:Context", Name: "Local event", Info: "Event description"}}, Section: section}
			if err := EventContext(page).Render(t.Context(), &out); err != nil {
				t.Fatal(err)
			}
			markup := out.String()
			if strings.Contains(markup, "Event details &amp; actions") || strings.Count(markup, `aria-label="Event sections"`) != 1 {
				t.Fatal("event must have one section navigation and no information disclosure")
			}
			if strings.Contains(markup, "Event description") != (section == "overview") {
				t.Fatal("Overview selection lost the event description")
			}
			if section == "overview" && strings.Index(markup, "Event description") > strings.Index(markup, "<details") {
				t.Fatal("Overview content must precede secondary disclosures")
			}
		})
	}
}

func TestEventOverviewKeepsDecisionContentVisibleAndActionsIndependent(t *testing.T) {
	event := models.Event{ID: "ticketmaster:Overview", Name: "Local show", Info: "A supplied description", PleaseNote: "Bring photo ID", Status: "canceled", Source: models.Source{URL: "https://www.ticketmaster.com/overview"}, Place: &models.Place{Name: "Local hall", Address: "12 Main Street"}}
	page := EventPage{Detail: models.EventDetail{Item: event, Meta: models.Freshness{Stale: true, DataAsOf: time.Now()}}, Section: "overview", Saves: SaveView{Enabled: true, SignedIn: true, Reminders: true, States: map[string]bool{event.ID: true}}, Interests: InterestView{Enabled: true, SignedIn: true}}
	for name, component := range map[string]templ.Component{"panel": EventContext(page), "page": Event(page)} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := component.Render(t.Context(), &out); err != nil {
				t.Fatal(err)
			}
			markup := out.String()
			for _, text := range []string{"A supplied description", "Bring photo ID", "12 Main Street", `Tickets<span class="ticket-provider"> on Ticketmaster</span>`, "Showing last-known details", `data-status="canceled"`, "Price not listed"} {
				if !strings.Contains(markup, text) {
					t.Errorf("missing decision content %q", text)
				}
			}
			if strings.Count(markup, "data-save-form") != 1 || strings.Count(markup, `class="interest-form"`) != 1 || strings.Count(markup, "On-sale reminder</span>") != 1 {
				t.Fatal("actions duplicated or lost")
			}
			if strings.Index(markup, "ticket-link") > strings.Index(markup, "A supplied description") || strings.Index(markup, "Bring photo ID") > strings.Index(markup, "data-interest-form") || strings.Index(markup, "data-interest-form") > strings.Index(markup, "reminder-button") {
				t.Fatal("decision facts, primary actions, and secondary participation out of order")
			}
			doc, err := html.Parse(strings.NewReader(markup))
			if err != nil {
				t.Fatal(err)
			}
			var walk func(*html.Node, bool)
			walk = func(n *html.Node, disclosed bool) {
				disclosed = disclosed || n.Data == "details"
				if disclosed && n.Type == html.TextNode && (strings.Contains(n.Data, event.Info) || strings.Contains(n.Data, event.PleaseNote) || strings.Contains(n.Data, "on Ticketmaster")) {
					t.Error("essential content hidden in disclosure")
				}
				for child := n.FirstChild; child != nil; child = child.NextSibling {
					walk(child, disclosed)
				}
			}
			walk(doc, false)
		})
	}
}

func TestDiscoveryRowsExposeDetailsAndOnlyRealPublicActivity(t *testing.T) {
	id := "ticketmaster:Row"
	event := models.Event{ID: id, Name: "A local show", Info: strings.Repeat("Useful description. ", 30), Artists: []models.Artist{{Name: "The Local Band"}}}
	var out bytes.Buffer
	activity := map[string]eventactivity.Preview{id: {
		Questions: 1, Question: &eventactivity.Contribution{ID: "root", Body: "Where is the entrance?", AuthorID: "author", AuthorName: "Public author"},
		Recommendations: 2, Recommendation: &eventactivity.Contribution{Body: "<script>Public reason</script>", AuthorID: "author", AuthorName: "Public author"},
	}}
	if err := EventRowsWithActivity([]models.Event{event}, "/?city=Berlin", "", SaveView{}, "/", InterestView{}, activity).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	markup := out.String()
	for _, want := range []string{"The Local Band", "1 question or tip", "2 public recommendations", "section=discussion", "section=community", "Ticket link unavailable."} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing useful row information %q", want)
		}
	}
	// Provider descriptions are mostly policy boilerplate; Overview shows them in full.
	if strings.Count(markup, "The Local Band") != 1 || strings.Contains(markup, "Useful description.") || strings.Contains(markup, "<details") {
		t.Fatal("facts duplicated, row repeats the provider description, or details hidden in disclosure")
	}
	// The artwork duplicates the title link, so only the title is a tab stop.
	if strings.Count(markup, "data-event-link") != 4 || strings.Count(markup, `tabindex="-1" aria-hidden="true"`) != 1 {
		t.Fatal("row artwork adds a second tab stop for the same event")
	}
	for _, unwanted := range []string{"Where is the entrance?", "Public reason", "Public author", "Read conversation", "data-thread-link"} {
		if strings.Contains(markup, unwanted) {
			t.Errorf("row includes full community content %q", unwanted)
		}
	}
	out.Reset()
	if err := EventRowsWithActivity([]models.Event{event}, "/", "", SaveView{}, "/", InterestView{}, nil).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "row-activity") || strings.Contains(out.String(), "0 questions") {
		t.Fatal("event with no known activity fabricated engagement")
	}
	for _, tc := range []struct {
		name    string
		artists []string
		want    string
	}{
		{"Taylor Swift | The Eras Tour", []string{"Taylor Swift"}, ""},
		{"Bulls vs. Lakers", []string{"Chicago Bulls", "Los Angeles Lakers"}, "Chicago Bulls, Los Angeles Lakers"},
		{"Bulls vs. Lakers", []string{"Bulls", "Lakers"}, ""},
		{"Summer Fest", []string{"Opening Act", "summer fest"}, "Opening Act"},
	} {
		event := models.Event{Name: tc.name}
		for _, name := range tc.artists {
			event.Artists = append(event.Artists, models.Artist{Name: name})
		}
		if got := featuringLabel(event); got != tc.want {
			t.Errorf("featuringLabel(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSaveFeedbackStaysOutsideActionControls(t *testing.T) {
	event := models.Event{ID: "ticketmaster:Layout", Name: "Layout regression"}
	view := SaveView{Enabled: true, SignedIn: true, CSRF: "fixture"}
	page := EventPage{Detail: models.EventDetail{Item: event}, Saves: view}
	for name, component := range map[string]templ.Component{
		"discovery": EventRows([]models.Event{event}, "/", "", view, "/", InterestView{}),
		"load more": MoreEventsList(SearchPage{Events: models.EventList{Items: []models.Event{event}}, Saves: view}),
		"preview":   EventContext(page),
		"event":     Event(page),
		"Saved":     Saved(SavedPage{List: saved.List{Items: []saved.Item{{Event: event}}}, Saves: view, ReturnURL: "/saved"}),
	} {
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			if err := component.Render(t.Context(), &out); err != nil {
				t.Fatal(err)
			}
			doc, err := html.Parse(&out)
			if err != nil {
				t.Fatal(err)
			}
			hasAttribute := func(node *html.Node, key string) bool {
				for _, attr := range node.Attr {
					if attr.Key == key {
						return true
					}
				}
				return false
			}
			var form, feedback *html.Node
			var walk func(*html.Node)
			walk = func(node *html.Node) {
				if hasAttribute(node, "data-save-form") {
					form = node
				}
				if hasAttribute(node, "data-save-feedback") {
					feedback = node
				}
				for child := node.FirstChild; child != nil; child = child.NextSibling {
					walk(child)
				}
			}
			walk(doc)
			if form == nil || feedback == nil {
				t.Fatal("missing save form or local feedback")
			}
			if !hasAttribute(feedback.Parent, "data-event-actions") || form.Parent.Parent != feedback.Parent {
				t.Fatal("feedback must be a sibling of the controls row, not part of button alignment")
			}
			if !hasAttribute(feedback, "hidden") {
				t.Fatal("empty feedback must not take space before a save")
			}
		})
	}
}

func TestOnSaleReminderIsAnEventAction(t *testing.T) {
	id := "ticketmaster:Reminder"
	for _, tc := range []struct {
		name      string
		saved     bool
		reminders bool
	}{
		{"saved with reminders", true, true},
		{"unsaved", false, true},
		{"reminders disabled", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			view := SaveView{Enabled: true, SignedIn: true, CSRF: "fixture", Reminders: tc.reminders, States: map[string]bool{id: tc.saved}}
			if err := SaveControl(id, "Named event", view, "/saved").Render(t.Context(), &out); err != nil {
				t.Fatal(err)
			}
			markup := out.String()
			if strings.Contains(markup, "reminder-button") != tc.reminders {
				t.Fatal("reminder settings require supported configuration")
			}
			if tc.reminders {
				_, link, _ := strings.Cut(markup, `<a href="/me/reminders/`)
				link, _, _ = strings.Cut(link, ">")
				if strings.Contains(link, " hidden") == tc.saved {
					t.Fatal("reminder settings must be hidden until the save is confirmed")
				}
			}
			if tc.saved && tc.reminders {
				for _, want := range []string{`href="/me/reminders/ticketmaster:Reminder"`, "button-quiet reminder-button", `aria-label="On-sale reminder for Named event"`, "<span>On-sale reminder</span>"} {
					if !strings.Contains(markup, want) {
						t.Errorf("missing reminder action contract %q", want)
					}
				}
			}
		})
	}
}
