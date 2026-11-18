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
	doc, err := html.Parse(&out)
	if err != nil {
		t.Fatal(err)
	}
	// These hooks are read by app.js; verify they sit on the controls and
	// ancestors the script uses, not just somewhere in the document.
	require := func(root *html.Node, tag string, attrs map[string]string) *html.Node {
		t.Helper()
		for n := range root.Descendants() {
			if n.Type != html.ElementNode || (tag != "" && n.Data != tag) {
				continue
			}
			matches := true
			for key, value := range attrs {
				if !slices.ContainsFunc(n.Attr, func(a html.Attribute) bool {
					return a.Key == key && (a.Val == value || key == "class" && slices.Contains(strings.Fields(a.Val), value))
				}) {
					matches = false
					break
				}
			}
			if matches {
				return n
			}
		}
		t.Fatalf("no element with tag %q and attributes %v", tag, attrs)
		return nil
	}
	sidebar := require(doc, "aside", map[string]string{"data-sidebar": ""})
	nav := require(sidebar, "nav", map[string]string{"data-nav-order": "", "id": "desktop-nav"})
	require(sidebar, "button", map[string]string{"data-sidebar-toggle": "", "aria-controls": "desktop-nav"})
	require(sidebar, "button", map[string]string{"data-nav-customize": "", "aria-pressed": "false"})
	actions := require(sidebar, "", map[string]string{"data-nav-customize-actions": ""})
	for _, action := range []string{"done", "cancel", "reset"} {
		require(actions, "button", map[string]string{"data-nav-customize-" + action: ""})
	}
	require(sidebar, "", map[string]string{"data-nav-status": "", "role": "status"})

	browse := require(nav, "section", map[string]string{"data-nav-group": "browse"})
	yourSpace := require(nav, "section", map[string]string{"data-nav-group": "your-space"})
	require(browse, "h2", map[string]string{"class": "nav-group-label"})
	require(browse, "", map[string]string{"data-nav-group-drag": ""})
	discover := require(browse, "", map[string]string{"data-nav-item": "discover"})
	link := require(discover, "a", map[string]string{"href": "/", "aria-current": "page", "data-label": "Discover"})
	require(link, "", map[string]string{"class": "nav-label"})
	require(discover, "", map[string]string{"data-nav-drag": ""})
	for _, direction := range []string{"up", "down"} {
		require(browse, "button", map[string]string{"data-move-group": direction, "aria-label": "Move Browse section " + direction})
		require(discover, "button", map[string]string{"data-move-item": direction, "aria-label": "Move Discover " + direction})
	}
	require(browse, "", map[string]string{"data-nav-item": "community"})
	require(yourSpace, "", map[string]string{"data-nav-item": "saved"})
	require(yourSpace, "", map[string]string{"data-nav-item": "interests"})
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
