package api

import (
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/radar"
	"github.com/sonastea/ticketopia/views/home"
)

func (a *api) radarRoutes(e *echo.Echo) {
	e.GET("/radar", a.radarPage)
	e.GET("/api/v1/me/radar", a.radarHandler)
}
func (a *api) radarHandler(c echo.Context) error {
	p, err := a.principal(c, false)
	if err != nil {
		return accountProblem(c, err)
	}
	if a.radar == nil {
		return accountProblem(c, radar.ErrUnavailable)
	}
	list, err := a.radar.List(c.Request().Context(), p.Account.ID, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}

// Validate pagination syntax without binding a guest's return URL to an owner.
// The service checks the actual authenticated owner before any data is returned.
func safeRadarReturn(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || strings.ContainsAny(raw, "\\\r\n\x00") || u.IsAbs() || u.Host != "" || u.Path != "/radar" || u.RawPath != "" || u.Fragment != "" {
		return "/radar"
	}
	v, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "/radar"
	}
	for key, entries := range v {
		if len(entries) != 1 || (key != "limit" && key != "cursor") {
			return "/radar"
		}
	}
	withoutCursor := url.Values{}
	for key, entries := range v {
		withoutCursor[key] = entries
	}
	withoutCursor.Del("cursor")
	if _, err := radar.ParseQuery(withoutCursor, "", time.Now()); err != nil {
		return "/radar"
	}
	if cursor, ok := v["cursor"]; ok && (cursor[0] == "" || len(cursor[0]) > 1024) {
		return "/radar"
	}
	return u.String()
}
func (a *api) radarPage(c echo.Context) error {
	privateAccountResponse(c)
	page := home.RadarPage{Enabled: a.enabled(), ReturnURL: safeRadarReturn(c.Request().URL.RequestURI())}
	if !a.enabled() {
		return render(c, 200, home.Radar(page))
	}
	p, err := a.principal(c, true)
	if errors.Is(err, accounts.ErrUnauthenticated) {
		return c.Redirect(303, "/auth/sign-in?return_to="+url.QueryEscape(page.ReturnURL))
	}
	if err != nil {
		return a.accountPageError(c, err)
	}
	if a.radar == nil {
		err = radar.ErrUnavailable
	} else {
		page.List, err = a.radar.List(c.Request().Context(), p.Account.ID, c.QueryParams())
	}
	status := 200
	if err != nil {
		status, _, page.Error, _ = accountError(err)
	} else {
		ids := []string{}
		for _, item := range page.List.Items {
			ids = append(ids, item.Event.ID)
		}
		page.Saves = a.saveView(c, ids)
		page.Interests = a.interestView(c, ids)
	}
	return render(c, status, home.Radar(page))
}
