package layouts

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestAccountNavigationFamiliesAndPermissions(t *testing.T) {
	// Keep the destination contract independent of accountFamilies: removing a
	// destination from production must fail the test, not silently remove a case.
	destinations := []struct{ key, href, family, label string }{
		{"profile", "/me", "Account", "Profile & privacy"},
		{"preferences", "/me/preferences", "Account", "Preferences"},
		{"radar", "/radar", "Your events", "Radar"},
		{"interests", "/me/interests", "Your events", "Interests"},
		{"follows", "/follows", "Your events", "Artists & venues"},
		{"reminders", "/me/reminders", "Updates", "On-sale reminders"},
		{"discussions", "/me/discussions", "Updates", "Followed discussions"},
		{"notifications", "/me/notifications", "Updates", "Reply notifications"},
		{"reports", "/me/reports", "Safety", "Your reports"},
		{"outcomes", "/me/moderation", "Safety", "Moderation outcomes"},
		{"queue", "/moderation", "Safety", "Moderator review queue"},
	}
	type linkIdentity struct{ label, family string }
	for _, permissions := range []AccountNavPermissions{{}, {Moderator: true}, {ModerationEnabled: true}, {ModerationEnabled: true, Moderator: true}} {
		expected := map[string]linkIdentity{}
		for _, destination := range destinations {
			if destination.family == "Safety" && (!permissions.ModerationEnabled || destination.key == "queue" && !permissions.Moderator) {
				continue
			}
			expected[destination.href] = linkIdentity{destination.label, destination.family}
		}
		for _, destination := range destinations {
			if _, visible := expected[destination.href]; !visible {
				continue
			}
			t.Run(fmt.Sprintf("safety=%t/moderator=%t/%s", permissions.ModerationEnabled, permissions.Moderator, destination.key), func(t *testing.T) {
				var out bytes.Buffer
				if err := AccountNav(destination.key, permissions).Render(t.Context(), &out); err != nil {
					t.Fatal(err)
				}
				doc, err := html.Parse(&out)
				if err != nil {
					t.Fatal(err)
				}
				current, disclosed := 0, 0
				links := map[string]linkIdentity{}
				var visit func(*html.Node, bool, string)
				visit = func(n *html.Node, inDetails bool, family string) {
					attrs := map[string]string{}
					for _, a := range n.Attr {
						attrs[a.Key] = a.Val
					}
					if attrs["role"] == "group" {
						family = attrs["aria-label"]
					}
					if n.Type == html.ElementNode && n.Data == "details" {
						inDetails = true
						disclosed++
						if _, open := attrs["open"]; open {
							t.Fatal("secondary families must start in a closed native disclosure")
						}
					}
					if n.Type == html.ElementNode && n.Data == "a" {
						if _, duplicate := links[attrs["href"]]; duplicate {
							t.Fatalf("duplicate destination %s", attrs["href"])
						}
						links[attrs["href"]] = linkIdentity{accountNavText(n), family}
						if attrs["aria-current"] == "page" {
							current++
							if inDetails || attrs["href"] != destination.href {
								t.Fatal("current location hidden or incorrect")
							}
						}
						if inDetails == (expected[attrs["href"]].family == destination.family) {
							t.Fatal("current family must be direct; other families must be disclosed")
						}
					}
					for child := n.FirstChild; child != nil; child = child.NextSibling {
						visit(child, inDetails, family)
					}
				}
				visit(doc, false, "")
				if current != 1 || disclosed != 1 {
					t.Fatalf("current=%d disclosures=%d", current, disclosed)
				}
				if !reflect.DeepEqual(links, expected) {
					t.Fatalf("destinations/labels/families: got %v, want %v", links, expected)
				}
			})
		}
	}
}

func accountNavText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	var text strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(accountNavText(child))
	}
	return strings.TrimSpace(text.String())
}
