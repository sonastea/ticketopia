package layouts

import (
	"bytes"
	"io"
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
