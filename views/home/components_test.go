package home

import (
	"bytes"
	"strings"
	"testing"

	"github.com/a-h/templ"
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

func TestEmptyPreviewCanReceiveKeyboardFocus(t *testing.T) {
	var out bytes.Buffer
	if err := Index(SearchPage{NeedsLocation: true}).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `id="event-context" class="event-context" aria-label="Event preview" tabindex="0"`) {
		t.Fatal("bounded preview must be keyboard-scrollable even without interactive event content")
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
