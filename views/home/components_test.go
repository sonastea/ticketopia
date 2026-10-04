package home

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sonastea/ticketopia/views/components/button"
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
