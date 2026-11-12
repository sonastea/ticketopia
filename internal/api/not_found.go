package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/views/home"
)

// httpErrorHandler gives a browser navigation that matches no page (404), or
// a page that accepts only another method (405), a site page with a way back
// to Discover. API, MCP, asset, panel and other non-navigation requests keep
// Echo's JSON response, and every other error keeps the default handling.
func httpErrorHandler(err error, c echo.Context) {
	var routing *echo.HTTPError
	if c.Response().Committed || !errors.As(err, &routing) ||
		(routing.Code != http.StatusNotFound && routing.Code != http.StatusMethodNotAllowed) {
		c.Echo().DefaultHTTPErrorHandler(err, c)
		return
	}
	// The same address answers with HTML or JSON depending on the request, so
	// neither representation may be stored and replayed for the other.
	header := c.Response().Header()
	header.Add("Vary", "Accept")
	header.Set("Cache-Control", "no-store")
	if apiPath(c.Request().URL.Path) {
		apiRoutingProblem(c, routing.Code)
		return
	}
	if !pageNavigation(c.Request()) {
		c.Echo().DefaultHTTPErrorHandler(err, c)
		return
	}
	if c.Request().Method == http.MethodHead {
		header.Set(echo.HeaderContentType, echo.MIMETextHTMLCharsetUTF8)
		c.Response().WriteHeader(routing.Code)
		return
	}
	if renderErr := render(c, routing.Code, home.NotFound(routing.Code)); renderErr != nil {
		c.Echo().DefaultHTTPErrorHandler(err, c)
	}
}

func apiPath(path string) bool { return path == "/api" || strings.HasPrefix(path, "/api/") }

// apiRoutingProblem answers an /api path or method that matches no route in the
// API's problem+json format, keeping Echo's Allow header on 405.
func apiRoutingProblem(c echo.Context, status int) {
	code, detail := "route_not_found", "No API endpoint exists at this path. See /api/v1/openapi.yaml."
	if status == http.StatusMethodNotAllowed {
		code, detail = "method_not_allowed", "This API endpoint does not accept this method; see the Allow header."
	}
	c.Response().Header().Set(echo.HeaderContentType, "application/problem+json")
	if c.Request().Method == http.MethodHead {
		c.Response().WriteHeader(status)
		return
	}
	_ = c.JSON(status, struct {
		Type   string `json:"type"`
		Title  string `json:"title"`
		Status int    `json:"status"`
		Code   string `json:"code"`
		Detail string `json:"detail"`
	}{"about:blank", http.StatusText(status), status, code, detail})
}

// pageNavigation reports whether r asks for an HTML page outside the API,
// MCP and asset paths. Partial requests (htmx and in-page section or preview
// loads) are not navigations: a whole page would not fit where they insert.
func pageNavigation(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	path := r.URL.Path
	for _, prefix := range []string{"/api", "/mcp", "/assets"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return false
		}
	}
	if r.Header.Get("HX-Request") != "" || r.Header.Get("X-Ticketopia-Panel") != "" || r.Header.Get("X-Ticketopia-Section") != "" {
		return false
	}
	return prefersHTML(r.Header.Get("Accept"))
}

// prefersHTML reports whether an Accept header names HTML, by text/html or
// text/*, with a nonzero q value at least that of application/json. Each type
// takes the q of its most specific matching range. A bare */* does not count
// as naming HTML: browsers always list text/html for navigations, while curl,
// fetch and API clients send */* (or nothing) and keep the JSON response.
func prefersHTML(accept string) bool {
	html, json := acceptance{}, acceptance{}
	for _, part := range strings.Split(accept, ",") {
		params := strings.Split(part, ";")
		mediaRange := strings.ToLower(strings.TrimSpace(params[0]))
		q, ok := qValue(params[1:])
		if mediaRange == "" || !ok {
			continue
		}
		html.consider(mediaRange, "text", "html", q)
		json.consider(mediaRange, "application", "json", q)
	}
	return html.specificity >= 2 && html.q > 0 && html.q >= json.q
}

// acceptance is the q value a media type takes from its most specific
// matching range: 3 for type/subtype, 2 for type/*, 1 for */* and 0 for none.
type acceptance struct {
	specificity int
	q           float64
}

func (a *acceptance) consider(mediaRange, typ, subtype string, q float64) {
	specificity := 0
	switch mediaRange {
	case typ + "/" + subtype:
		specificity = 3
	case typ + "/*":
		specificity = 2
	case "*/*":
		specificity = 1
	}
	if specificity == 0 || specificity < a.specificity {
		return
	}
	if specificity > a.specificity || q > a.q {
		a.specificity, a.q = specificity, q
	}
}

// qValue returns a media range's q parameter (1 when absent). ok is false
// for a malformed or out-of-range value, which voids the range.
func qValue(params []string) (q float64, ok bool) {
	for _, param := range params {
		name, value, _ := strings.Cut(param, "=")
		if !strings.EqualFold(strings.TrimSpace(name), "q") {
			continue
		}
		q, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || !(q >= 0 && q <= 1) {
			return 0, false
		}
		return q, true
	}
	return 1, true
}
