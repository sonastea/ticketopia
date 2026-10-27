package layouts

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestAppShellKeepsSidebarContract(t *testing.T) {
	var out bytes.Buffer
	if err := App("discover", "Test").Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{
		`data-sidebar`,
		`data-nav-order`,
		`data-nav-group="browse"`,
		`data-nav-group="your-space"`,
		`data-nav-item="discover"`,
		`data-nav-item="community"`,
		`data-nav-item="saved"`,
		`data-nav-item="interests"`,
		`data-sidebar-toggle`,
		`aria-controls="desktop-nav"`,
		`data-nav-customize`,
		`aria-pressed="false"`,
		`data-nav-customize-done`,
		`data-nav-customize-cancel`,
		`data-nav-customize-reset`,
		`role="status"`,
		`aria-current="page"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing sidebar contract %q", want)
		}
	}
}

func TestSidebarEnhancementsStayHiddenWithoutJavaScript(t *testing.T) {
	var out bytes.Buffer
	if err := App("discover", "Test").Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	tokens := html.NewTokenizer(&out)
	controls := 0
	for {
		if tokens.Next() == html.ErrorToken {
			if err := tokens.Err(); err != io.EOF {
				t.Fatal(err)
			}
			break
		}
		token := tokens.Token()
		if token.Type != html.StartTagToken || token.Data != "button" {
			continue
		}
		attrs := make(map[string]string)
		for _, attr := range token.Attr {
			attrs[attr.Key] = attr.Val
		}
		_, toggle := attrs["data-sidebar-toggle"]
		_, customize := attrs["data-nav-customize"]
		if !toggle && !customize {
			continue
		}
		controls++
		if _, hidden := attrs["hidden"]; !hidden {
			t.Errorf("enhancement-only button is exposed without JavaScript: %v", attrs)
		}
		if attrs["type"] != "button" {
			t.Error("sidebar control can accidentally submit a form")
		}
	}
	if controls != 2 {
		t.Errorf("found %d enhancement controls, want 2", controls)
	}
}

// Browsers silently ignore malformed rules, so prefetching would just stop.
func TestSpeculationRulesPrefetchEventResultsOutsideDiscover(t *testing.T) {
	var out bytes.Buffer
	if err := App("saved", "Test").Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	doc, err := html.Parse(&out)
	if err != nil {
		t.Fatal(err)
	}
	var rules string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "script" && slices.Contains(n.Attr, html.Attribute{Key: "type", Val: "speculationrules"}) && n.FirstChild != nil {
			rules = n.FirstChild.Data
		}
		for child := n.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(doc)
	var parsed struct {
		Prefetch []struct {
			Where     map[string][]map[string]any `json:"where"`
			Eagerness string                      `json:"eagerness"`
		} `json:"prefetch"`
	}
	if err := json.Unmarshal([]byte(rules), &parsed); err != nil {
		t.Fatalf("speculation rules are not JSON: %v\n%s", err, rules)
	}
	if len(parsed.Prefetch) != 1 || parsed.Prefetch[0].Eagerness != "conservative" || len(parsed.Prefetch[0].Where["and"]) != 3 {
		t.Fatalf("unexpected speculation rules: %+v", parsed)
	}
	if got := fmt.Sprint(parsed.Prefetch[0].Where["and"][2]); !strings.Contains(got, "[data-results-url] [data-event-link]") {
		t.Errorf("Discover's in-page previews are not excluded: %s", got)
	}
}

func TestNavItemKeepsReorderContract(t *testing.T) {
	var out bytes.Buffer
	if err := NavItem("/", "Discover", "discover", true).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{
		`data-nav-item="discover"`,
		`data-nav-drag`,
		`aria-label="Move Discover up"`,
		`aria-label="Move Discover down"`,
		`aria-current="page"`,
		`data-label="Discover"`,
		`class="nav-label"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing nav item contract %q", want)
		}
	}
}

func TestNavGroupKeepsReorderContract(t *testing.T) {
	var out bytes.Buffer
	if err := NavGroup("browse", "Browse").Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	html := out.String()
	for _, want := range []string{
		`data-nav-group="browse"`,
		`data-nav-group-drag`,
		`aria-label="Move Browse section up"`,
		`aria-label="Move Browse section down"`,
		`class="nav-group-label"`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("missing nav group contract %q", want)
		}
	}
}
