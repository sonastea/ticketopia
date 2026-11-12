package api

import (
	"encoding/json"
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
	if head.Body.Len() != 0 {
		t.Fatalf("HEAD must not send a body:\n%s", head.Body)
	}

	// Both representations share addresses, so neither may be cached for the other.
	for name, w := range map[string]*httptest.ResponseRecorder{
		"HTML 404": page, "HTML 405": action, "HTML HEAD": head,
		"JSON 404": serve(http.MethodGet, "/no-such-page", "application/json"),
		"JSON 405": serve(http.MethodGet, "/saved/ticketmaster:1", "application/json"),
		"API 404":  serve(http.MethodGet, "/api/v1/no-such-resource", browser),
	} {
		if !strings.Contains(w.Header().Get("Vary"), "Accept") || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s must carry Vary: Accept and Cache-Control: no-store: %v", name, w.Header())
		}
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
		{"HTML refused", http.MethodGet, "/no-such-page", "text/html;q=0, application/json", nil},
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

// Unmatched /api routes answer in the API's problem+json format, for browsers too.
func TestUnknownAPIRoutesAnswerProblems(t *testing.T) {
	routes := (&api{}).Routes()
	for _, tc := range []struct {
		method, path, accept, code string
		status                     int
	}{
		{http.MethodGet, "/api/v1/no-such-resource", "text/html,*/*;q=0.8", "route_not_found", http.StatusNotFound},
		{http.MethodGet, "/api", "", "route_not_found", http.StatusNotFound},
		{http.MethodDelete, "/api/v1/events", "application/json", "method_not_allowed", http.StatusMethodNotAllowed},
	} {
		r := httptest.NewRequest(tc.method, tc.path, nil)
		if tc.accept != "" {
			r.Header.Set("Accept", tc.accept)
		}
		w := httptest.NewRecorder()
		routes.ServeHTTP(w, r)
		var problem struct {
			Status int
			Code   string
		}
		if err := json.Unmarshal(w.Body.Bytes(), &problem); err != nil || w.Code != tc.status || problem.Status != tc.status || problem.Code != tc.code ||
			w.Header().Get("Content-Type") != "application/problem+json" || w.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s %s: %d %v %s", tc.method, tc.path, w.Code, w.Header(), w.Body)
		}
		if tc.status == http.StatusMethodNotAllowed && !strings.Contains(w.Header().Get("Allow"), http.MethodGet) {
			t.Errorf("405 must keep Allow: %v", w.Header())
		}
	}
	head := httptest.NewRecorder()
	routes.ServeHTTP(head, httptest.NewRequest(http.MethodHead, "/api/v1/no-such-resource", nil))
	if head.Code != http.StatusNotFound || head.Body.Len() != 0 {
		t.Fatalf("HEAD: %d %q", head.Code, head.Body)
	}
}

func TestPrefersHTML(t *testing.T) {
	for _, tc := range []struct {
		accept string
		want   bool
	}{
		{"text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7", true}, // Chrome
		{"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8", true},                                                                         // Firefox, Safari
		{"TEXT/HTML", true},
		{"Text/Html; Q=0.5", true},
		{"text/html;level=1", true},
		{"text/*", true},
		{"application/json, text/html", true},
		{"application/json;q=0.9, text/html", true},
		{"text/*;q=0, text/html", true},
		{"", false},
		{"*/*", false}, // curl, fetch
		{"application/json", false},
		{"text/html;q=0, application/json", false},
		{"TEXT/HTML;Q=0, application/json", false},
		{"text/html;q=0", false},
		{"text/html;q=0.000", false},
		{"text/html;q=0.5, application/json", false},
		{"text/html;q=0.5, */*", false},
		{"text/html, text/*;q=0.1, application/*", true},
		{"text/html;q=0.5, application/*;q=0.9", false},
		{"text/html;q=2", false},
		{"text/html;q=NaN", false},
		{"text/plain", false},
	} {
		if got := prefersHTML(tc.accept); got != tc.want {
			t.Errorf("prefersHTML(%q) = %v, want %v", tc.accept, got, tc.want)
		}
	}
}
