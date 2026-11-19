// Package htmlcheck inspects rendered HTML in tests.
package htmlcheck

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// sectionTags group a page into the units the notes budget applies to.
var sectionTags = []string{"section", "fieldset", "form", "aside", "details", "main"}

// NotesOverBudget reports every section holding more than max explanatory
// notes of its own. A note is a p.field-note whose nearest section ancestor is
// that section. Not counted: field help that a form control in the same
// section names in aria-describedby, notes inside list items (they describe
// that item, not the section) and empty live regions waiting for a message.
func NotesOverBudget(r io.Reader, max int) ([]string, error) {
	doc, err := html.Parse(r)
	if err != nil {
		return nil, err
	}
	// Field help: ids a form control describes itself with, by the section
	// the control sits in.
	described := map[string]*html.Node{}
	var collect func(n, section *html.Node)
	collect = func(n, section *html.Node) {
		if n.Type == html.ElementNode {
			if slices.Contains(sectionTags, n.Data) {
				section = n
			}
			if slices.Contains([]string{"input", "select", "textarea", "button"}, n.Data) {
				for _, id := range strings.Fields(attr(n, "aria-describedby")) {
					described[id] = section
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			collect(c, section)
		}
	}
	collect(doc, doc)

	counts := map[*html.Node]int{}
	order := []*html.Node{}
	var walk func(n, section *html.Node, inItem bool)
	walk = func(n, section *html.Node, inItem bool) {
		if n.Type == html.ElementNode {
			if slices.Contains(sectionTags, n.Data) {
				section = n
			}
			if n.Data == "li" {
				inItem = true
			}
			if n.Data == "p" && hasClass(n, "field-note") && !inItem && (attr(n, "id") == "" || described[attr(n, "id")] != section) && (attr(n, "role") != "status" || text(n) != "") {
				if counts[section] == 0 {
					order = append(order, section)
				}
				counts[section]++
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c, section, inItem)
		}
	}
	walk(doc, doc, false)

	over := []string{}
	for _, section := range order {
		if counts[section] > max {
			over = append(over, fmt.Sprintf("%s %d notes", describe(section), counts[section]))
		}
	}
	return over, nil
}

func describe(n *html.Node) string {
	if n.Type != html.ElementNode {
		return "document"
	}
	for _, key := range []string{"id", "aria-labelledby", "aria-label", "action", "class"} {
		if v := attr(n, key); v != "" {
			return fmt.Sprintf("<%s %s=%q>", n.Data, key, v)
		}
	}
	return "<" + n.Data + ">"
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasClass(n *html.Node, class string) bool {
	return slices.Contains(strings.Fields(attr(n, "class")), class)
}

func text(n *html.Node) string {
	if n.Type == html.TextNode {
		return strings.TrimSpace(n.Data)
	}
	var b strings.Builder
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.WriteString(text(c))
	}
	return strings.TrimSpace(b.String())
}
