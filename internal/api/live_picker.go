package api

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/views/home"
	"github.com/sonastea/ticketopia/views/layouts"
)

type liveFilterDraft struct {
	preferences accounts.LivePreferences
	editor      layouts.LiveFilterEditor
}

// Labels are resolved only on deliberate page reads/searches, under the catalog's
// existing authentication contract. Lookup failure never removes a selected ID.
func (a *api) liveEditor(c echo.Context, f activity.Filters, signedIn bool) layouts.LiveFilterEditor {
	e := layouts.LiveFilterEditor{SignedIn: signedIn, Labels: map[string]string{}}
	if !signedIn {
		return e
	}
	if _, err := a.followPrincipal(c, false); err != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(c.Request().Context(), 3*time.Second)
	defer cancel()
	for _, d := range []struct {
		kind string
		ids  []string
	}{{"artist", f.ArtistIDs}, {"venue", f.VenueIDs}} {
		for _, id := range d.ids {
			if ctx.Err() != nil {
				return e
			}
			if detail, err := a.events.CatalogDetail(ctx, d.kind, id); err == nil {
				e.Labels[d.kind+":"+id] = detail.Item.Name
			}
		}
	}
	return e
}

func (a *api) searchLivePicker(c echo.Context, p accounts.Principal, f activity.Filters, revision uint64) error {
	pref, err := a.accounts.LivePreferences(c.Request().Context(), p.Account.ID)
	if err != nil {
		return a.accountPageError(c, err)
	}
	e := a.liveEditor(c, f, true)
	e.Draft = true
	e.Kind = strings.TrimPrefix(c.FormValue("filter_action"), "search_")
	e.Keyword = c.FormValue("live_" + e.Kind + "_keyword")
	if _, err := a.followPrincipal(c, false); err != nil {
		e.Error = "Search is unavailable. Your selections are retained; try again."
	} else {
		list, err := a.events.Catalog(c.Request().Context(), e.Kind, url.Values{"keyword": {e.Keyword}})
		if err != nil {
			e.Error = "Search could not load. Enter a name and search again; your selections are retained."
		} else {
			e.Results = list.Items
		}
	}
	if c.FormValue("return_to") == "/happening-now" {
		page := home.HappeningNowPage{Enabled: a.liveActivity != nil, SignedIn: true, LiveEnabled: c.FormValue("enabled") == "on", Paused: pref.Paused || !pref.Enabled, Filters: pref.Filters, DraftFilters: &f, Editor: e, CSRF: p.CSRF, Revision: revision, ReturnURL: "/happening-now"}
		return a.renderHappeningNow(c, 200, page)
	}
	pref.Filters, pref.Enabled, pref.Revision = f, c.FormValue("enabled") == "on", revision
	return a.renderAccount(c, 200, p, "preferences", "", "", "", liveFilterDraft{pref, e})
}
