package api

import (
	"errors"
	"net/http"
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
		(routing.Code != http.StatusNotFound && routing.Code != http.StatusMethodNotAllowed) ||
		!pageNavigation(c.Request()) {
		c.Echo().DefaultHTTPErrorHandler(err, c)
		return
	}
	header := c.Response().Header()
	header.Add("Vary", "Accept")
	header.Set("Cache-Control", "no-store")
	if renderErr := render(c, routing.Code, home.NotFound(routing.Code)); renderErr != nil {
		c.Echo().DefaultHTTPErrorHandler(err, c)
	}
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
	return strings.Contains(r.Header.Get("Accept"), "text/html")
}
