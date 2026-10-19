package api

import (
	"errors"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/views/home"
)

func happeningFilters(q url.Values) (activity.Filters, error) {
	f := activity.DefaultPreferences().Filters
	for key, values := range q {
		if key != "live_category_ids" && key != "live_artist_ids" && key != "live_venue_ids" && len(values) != 1 {
			return f, errors.New("Use one value per filter field.")
		}
		switch key {
		case "location_mode", "live_city", "live_country", "live_category_ids", "live_artist_ids", "live_venue_ids", "filter_action":
		default:
			return f, errors.New("Unknown activity filter.")
		}
	}
	if action := q.Get("filter_action"); action != "" && action != "apply" && action != "reset" {
		return f, errors.New("Unknown filter action.")
	}
	if q.Get("filter_action") == "reset" {
		return f, nil
	}
	if mode := q.Get("location_mode"); mode != "" {
		f.Location.Mode = mode
	}
	if f.Location.Mode == "city" {
		f.Location.City, f.Location.Country = q.Get("live_city"), q.Get("live_country")
	}
	f.CategoryIDs = append([]string{}, q["live_category_ids"]...)
	f.ArtistIDs = append([]string{}, q["live_artist_ids"]...)
	f.VenueIDs = append([]string{}, q["live_venue_ids"]...)
	return activity.NormalizeFilters(f)
}

func (a *api) happeningNowPage(c echo.Context) error {
	privateAccountResponse(c)
	p := home.HappeningNowPage{Enabled: a.liveActivity != nil, Filters: activity.DefaultPreferences().Filters, ReturnURL: c.Request().URL.RequestURI()}
	if !p.Enabled {
		return render(c, 200, home.HappeningNow(p))
	}
	status := 200
	principal, err := a.principal(c, true)
	if err == nil {
		pref, err := a.accounts.LivePreferences(c.Request().Context(), principal.Account.ID)
		if err != nil {
			return a.accountPageError(c, err)
		}
		p.SignedIn, p.CSRF, p.Revision = true, principal.CSRF, pref.Revision
		p.Filters, p.LiveEnabled, p.Paused = pref.Filters, pref.Enabled, pref.Paused || !pref.Enabled
	} else if !errors.Is(err, accounts.ErrUnauthenticated) && !errors.Is(err, errDisabled) {
		return a.accountPageError(c, err)
	} else {
		if len(c.Request().URL.RawQuery) > 8192 {
			p.Error = "Activity filters are too large."
		} else {
			q, queryErr := url.ParseQuery(c.Request().URL.RawQuery)
			if queryErr == nil {
				p.Filters, queryErr = happeningFilters(q)
			}
			if queryErr != nil {
				p.Error = queryErr.Error()
			}
		}
		if p.Error != "" {
			status = 400
		}
	}
	p.Editor = a.liveEditor(c, p.Filters, p.SignedIn)
	return a.renderHappeningNow(c, status, p)
}

func (a *api) renderHappeningNow(c echo.Context, status int, p home.HappeningNowPage) error {
	if a.events != nil {
		catalog, _ := a.events.Categories(c.Request().Context())
		p.Categories = catalog.Items
	}
	if p.Enabled && !p.Paused && p.Error == "" {
		if !a.liveStarts.allow(c.RealIP()) {
			status, p.Error = 429, "Live activity is temporarily unavailable. Refresh in one minute."
			c.Response().Header().Set("Retry-After", "60")
		} else {
			var err error
			owner := ""
			if p.SignedIn {
				principal, authErr := a.principal(c, true)
				if authErr != nil {
					return a.accountPageError(c, authErr)
				}
				owner = principal.Account.ID
			}
			p.Snapshot, err = a.liveActivity.OwnerSnapshot(c.Request().Context(), owner, p.Filters)
			if err != nil {
				status, p.Error = 503, "Live activity is unavailable. Refresh to try again."
			}
		}
	}
	return render(c, status, home.HappeningNow(p))
}
