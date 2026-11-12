package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestUnknownPagesAnswerBrowsersWithASitePage(t *testing.T) {
	routes := (&api{}).Routes()
	serve := func(method, path, accept string, headers ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		if accept != "" {
			r.Header.Set("Accept", accept)
		}
		for i := 0; i+1 < len(headers); i += 2 {
			r.Header.Set(headers[i], headers[i+1])
		}
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		return w
	}
	const browser = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"

	page := serve(http.MethodGet, "/no-such-page", browser)
	body := page.Body.String()
	if page.Code != http.StatusNotFound || page.Header().Get("Content-Type") != "text/html; charset=UTF-8" ||
		!strings.Contains(body, "<title>Page not found · Ticketopia</title>") || !strings.Contains(body, `href="/"`) ||
		!strings.Contains(body, `class="app-shell"`) || !strings.Contains(page.Header().Get("Vary"), "Accept") {
		t.Fatalf("browser 404 must be a site page with a way back to Discover: %d %v\n%s", page.Code, page.Header(), body)
	}
	if strings.Contains(body, `aria-current="page"`) {
		t.Fatal("a missing page must not mark a navigation destination current")
	}

	// A form action opened as a page keeps its status and Allow header.
	action := serve(http.MethodGet, "/saved/ticketmaster:1", browser)
	if action.Code != http.StatusMethodNotAllowed || !strings.Contains(action.Header().Get("Allow"), http.MethodPost) ||
		!strings.Contains(action.Body.String(), "This address belongs to an action.") {
		t.Fatalf("browser 405 must explain the action address: %d %v\n%s", action.Code, action.Header(), action.Body)
	}

	head := serve(http.MethodHead, "/no-such-page", browser)
	if head.Code != http.StatusNotFound || !strings.HasPrefix(head.Header().Get("Content-Type"), "text/html") {
		t.Fatalf("HEAD must match GET's status and type: %d %v", head.Code, head.Header())
	}

	for _, tc := range []struct {
		name    string
		method  string
		path    string
		accept  string
		headers []string
	}{
		{"no Accept", http.MethodGet, "/no-such-page", "", nil},
		{"JSON client", http.MethodGet, "/no-such-page", "application/json", nil},
		{"API path", http.MethodGet, "/api/v1/no-such-resource", browser, nil},
		{"API root", http.MethodGet, "/api", browser, nil},
		{"MCP subpath", http.MethodGet, "/mcp/extra", browser, nil},
		{"section load", http.MethodGet, "/no-such-page", browser, []string{"X-Ticketopia-Section", "true"}},
		{"htmx", http.MethodGet, "/no-such-page", browser, []string{"HX-Request", "true"}},
		{"form post", http.MethodPost, "/no-such-page", browser, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := serve(tc.method, tc.path, tc.accept, tc.headers...)
			if w.Code != http.StatusNotFound && w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("status = %d", w.Code)
			}
			if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") || strings.Contains(w.Body.String(), "<html") {
				t.Fatalf("non-navigation must keep JSON: %v\n%s", w.Header(), w.Body)
			}
		})
	}
}
