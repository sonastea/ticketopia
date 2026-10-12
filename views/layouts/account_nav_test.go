package layouts

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestAccountNavigationFamiliesAndPermissions(t *testing.T) {
	for _, permissions := range []AccountNavPermissions{{}, {Moderator: true}, {ModerationEnabled: true}, {ModerationEnabled: true, Moderator: true}} {
		for _, family := range accountFamilies(permissions) {
			for _, destination := range family.Links {
				t.Run(destination.Key+"/"+strings.ReplaceAll(family.Label, " ", "-"), func(t *testing.T) {
					var out bytes.Buffer
					if err := AccountNav(destination.Key, permissions).Render(t.Context(), &out); err != nil {
						t.Fatal(err)
					}
					doc, err := html.Parse(&out)
					if err != nil {
						t.Fatal(err)
					}
					current, disclosed := 0, 0
					var visit func(*html.Node, bool)
					visit = func(n *html.Node, inDetails bool) {
						if n.Type == html.ElementNode && n.Data == "details" {
							inDetails = true
							disclosed++
						}
						if n.Type == html.ElementNode && n.Data == "a" {
							attrs := map[string]string{}
							for _, a := range n.Attr {
								attrs[a.Key] = a.Val
							}
							if attrs["aria-current"] == "page" {
								current++
								if inDetails || attrs["href"] != destination.Href {
									t.Fatal("current location hidden or incorrect")
								}
							}
							for _, link := range family.Links {
								if attrs["href"] == link.Href && inDetails {
									t.Fatal("current family hidden")
								}
							}
						}
						for child := n.FirstChild; child != nil; child = child.NextSibling {
							visit(child, inDetails)
						}
					}
					visit(doc, false)
					if current != 1 || disclosed != 1 {
						t.Fatalf("current=%d disclosures=%d", current, disclosed)
					}
					body := renderAccountNav(t, destination.Key, permissions)
					for _, f := range accountFamilies(permissions) {
						for _, link := range f.Links {
							if strings.Count(body, `href="`+link.Href+`"`) != 1 {
								t.Fatalf("destination %s missing or duplicated", link.Href)
							}
						}
					}
					if strings.Contains(body, `href="/moderation"`) != (permissions.ModerationEnabled && permissions.Moderator) {
						t.Fatal("moderator gate changed")
					}
					if strings.Contains(body, `href="/me/reports"`) != permissions.ModerationEnabled {
						t.Fatal("report gate changed")
					}
				})
			}
		}
	}
}

func renderAccountNav(t *testing.T, current string, p AccountNavPermissions) string {
	t.Helper()
	var out bytes.Buffer
	if err := AccountNav(current, p).Render(t.Context(), &out); err != nil {
		t.Fatal(err)
	}
	return out.String()
}
