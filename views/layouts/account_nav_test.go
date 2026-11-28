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
		{"profile", "/me", "Account", "Profile"},
		{"privacy", "/me/privacy", "Account", "Privacy"},
		{"preferences", "/me/preferences", "Account", "Preferences"},
		{"security", "/me/security", "Account", "Security & Apps"},
		{"radar", "/radar", "Your events", "Radar"},
		{"interests", "/me/interests", "Your events", "Interests"},
		{"follows", "/follows", "Your events", "Artists & venues"},
		{"drafts", "/me/drafts", "Updates", "Agent drafts"},
		{"reminders", "/me/reminders", "Updates", "On-sale reminders"},
		{"discussions", "/me/discussions", "Updates", "Followed discussions"},
		{"notifications", "/me/notifications", "Updates", "Notifications"},
		{"reports", "/me/reports", "Safety", "Your reports"},
		{"outcomes", "/me/moderation", "Safety", "Moderation outcomes"},
	}
	visible := func(p AccountNavPermissions, key, family string) bool {
		return !(key == "drafts" && !p.Drafts) && !(family == "Safety" && !p.ModerationEnabled)
	}
	for _, permissions := range []AccountNavPermissions{
		{},
		{Moderator: true},
		{ModerationEnabled: true},
		{ModerationEnabled: true, Moderator: true, Drafts: true, PendingDrafts: 2},
	} {
		// The first visible destination of each family is where its link leads.
		first := map[string]string{}
		for _, d := range destinations {
			if _, ok := first[d.family]; !ok && visible(permissions, d.key, d.family) {
				first[d.family] = d.href
			}
		}
		for _, destination := range destinations {
			if !visible(permissions, destination.key, destination.family) {
				continue
			}
			t.Run(fmt.Sprintf("%+v/%s", permissions, destination.key), func(t *testing.T) {
				var out bytes.Buffer
				if err := AccountNav(destination.key, permissions).Render(t.Context(), &out); err != nil {
					t.Fatal(err)
				}
				doc, err := html.Parse(&out)
				if err != nil {
					t.Fatal(err)
				}
				tabs, families := map[string]string{}, map[string]string{}
				current := 0
				var visit func(*html.Node, string)
				visit = func(n *html.Node, area string) {
					attrs := map[string]string{}
					for _, a := range n.Attr {
						attrs[a.Key] = a.Val
					}
					if attrs["role"] == "group" {
						area = "tabs:" + attrs["aria-label"]
					}
					if n.Type == html.ElementNode && n.Data == "ul" {
						area = "families"
					}
					if n.Type == html.ElementNode && n.Data == "a" {
						text := accountNavText(n)
						if area == "families" {
							families[text] = attrs["href"]
						} else {
							tabs[attrs["href"]] = area + "/" + text
						}
						if attrs["aria-current"] == "page" {
							current++
							if area == "families" || attrs["href"] != destination.href {
								t.Fatal("current location hidden or incorrect")
							}
						}
					}
					for child := n.FirstChild; child != nil; child = child.NextSibling {
						visit(child, area)
					}
				}
				visit(doc, "")
				wantTabs, wantFamilies := map[string]string{}, map[string]string{}
				for _, d := range destinations {
					if !visible(permissions, d.key, d.family) {
						continue
					}
					if d.family == destination.family {
						wantTabs[d.href] = "tabs:" + d.family + "/" + d.label
					} else {
						wantFamilies[d.family] = first[d.family]
					}
				}
				// Moderator work is a separate tool beside the families, never a tab.
				if permissions.ModerationEnabled && permissions.Moderator {
					wantFamilies["Review queue"] = "/moderation"
				}
				if current != 1 || !reflect.DeepEqual(tabs, wantTabs) || !reflect.DeepEqual(families, wantFamilies) {
					t.Fatalf("current=%d\ntabs %v\nwant %v\nfamilies %v\nwant %v", current, tabs, wantTabs, families, wantFamilies)
				}
			})
		}
	}
}

func TestAccountNavigationShowsPendingDraftsFromOtherFamilies(t *testing.T) {
	var out bytes.Buffer
	if err := AccountNav("profile", AccountNavPermissions{Drafts: true, PendingDrafts: 3}).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `<a href="/me/drafts">Updates<span class="account-nav-count">3<span class="sr-only"> pending</span></span></a>`) {
		t.Fatalf("pending drafts hidden outside Updates: %s", out.String())
	}
}

func accountNavText(n *html.Node) string {
	if n.Type == html.TextNode {
		return n.Data
	}
	for _, a := range n.Attr {
		if a.Key == "class" && a.Val == "account-nav-count" {
			return "" // Pending counts are not part of the destination name.
		}
	}
	var text strings.Builder
	for child := n.FirstChild; child != nil; child = child.NextSibling {
		text.WriteString(accountNavText(child))
	}
	return strings.TrimSpace(text.String())
}
