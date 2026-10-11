package home

import (
	"bytes"
	"strings"
	"testing"

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

func TestEventRowDetailsLinkRendersIcon(t *testing.T) {
	var out bytes.Buffer
	event := models.Event{ID: "ticketmaster:Details", Name: "Named event"}
	if err := EventRows([]models.Event{event}, "/", "", SaveView{}, "/", InterestView{}).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	markup := out.String()
	if strings.Contains(markup, "@layouts.Icon") {
		t.Fatal("event row rendered an icon component as literal text")
	}
	_, link, found := strings.Cut(markup, `aria-label="View event details for Named event">`)
	if !found {
		t.Fatal("missing event details link")
	}
	link, _, _ = strings.Cut(link, "</a>")
	if !strings.Contains(link, "View event details</span>") || !strings.Contains(link, "<svg") {
		t.Fatal("event details link must render its label and SVG icon", link)
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

func TestEventContextActivityIsVisibleBelowDetails(t *testing.T) {
	for _, section := range []string{"overview", "discussion", "community"} {
		t.Run(section, func(t *testing.T) {
			var out bytes.Buffer
			page := EventPage{Detail: models.EventDetail{Item: models.Event{ID: "ticketmaster:Context", Name: "Local event", Info: "Event description"}}, Section: section}
			if err := EventContext(page).Render(t.Context(), &out); err != nil {
				t.Fatal(err)
			}
			markup := out.String()
			details := strings.Index(markup, "</details>")
			discussion := strings.Index(markup, `id="context-discussion-heading"`)
			community := strings.Index(markup, `id="context-community-heading"`)
			if details < 0 || discussion <= details || community <= discussion || strings.Contains(markup, `aria-label="Event sections"`) {
				t.Fatal("activity must follow event details without section tabs")
			}
			if strings.Contains(markup, "Event description") != (section == "overview") {
				t.Fatal("Overview selection lost the event description")
			}
			if section == "overview" && strings.Index(markup, "Event description") > details {
				t.Fatal("secondary Overview content must stay in the details disclosure, not push activity down")
			}
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
	for _, want := range []string{"The Local Band", "Useful description.", "1 question or tip", "2 public recommendations", "Where is the entrance?", "&lt;script&gt;Public reason&lt;/script&gt;", "Read conversation", "data-thread-link"} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing useful row information %q", want)
		}
	}
	if strings.Count(markup, "The Local Band") != 1 || strings.Contains(markup, event.Info) || strings.Index(markup, "Useful description.") > strings.Index(markup, "<details") {
		t.Fatal("facts duplicated, description not previewed, or preview hidden in disclosure")
	}
	out.Reset()
	if err := EventRowsWithActivity([]models.Event{event}, "/", "", SaveView{}, "/", InterestView{}, nil).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "row-activity") || strings.Contains(out.String(), "0 questions") {
		t.Fatal("event with no known activity fabricated engagement")
	}
	if got := previewText(strings.Repeat("界", 201)); got != strings.Repeat("界", 200)+"…" {
		t.Fatal("preview split a Unicode character", got)
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
			if strings.Contains(markup, "reminder-button") != (tc.saved && tc.reminders) {
				t.Fatal("reminder action must require a saved event and enabled reminders")
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
