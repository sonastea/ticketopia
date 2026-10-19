package api

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
)

func liveETag(owner string, revision uint64) string {
	return fmt.Sprintf(`"live-%x-%d"`, sha256.Sum256([]byte(owner)), revision)
}
func parseLiveETag(owner, raw string) (uint64, bool) {
	var n uint64
	prefix := fmt.Sprintf(`"live-%x-`, sha256.Sum256([]byte(owner)))
	_, err := fmt.Sscanf(raw, prefix+`%d"`, &n)
	return n, err == nil && n > 0 && raw == liveETag(owner, n)
}
func (a *api) livePreferences(c echo.Context) error {
	privateAccountResponse(c)
	p, err := a.requireAPI(c, c.Request().Method == http.MethodPut, false)
	if err != nil {
		return accountProblem(c, err)
	}
	var pref accounts.LivePreferences
	if c.Request().Method == http.MethodPut {
		match := c.Request().Header.Get("If-Match")
		if match == "" {
			return liveProblem(c, 428, "precondition_required", "Send the preference ETag in If-Match.")
		}
		revision, ok := parseLiveETag(p.Account.ID, match)
		if !ok {
			return liveProblem(c, 412, "precondition_failed", "Reload live preferences before saving.")
		}
		media, _, e := mime.ParseMediaType(c.Request().Header.Get("Content-Type"))
		if e != nil || media != "application/json" {
			return liveProblem(c, 400, "invalid_preferences", "Send application/json.")
		}
		data, e := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, 8192))
		if e != nil {
			return liveProblem(c, 400, "invalid_preferences", "Use a body no larger than 8 KiB.")
		}
		value, e := activity.DecodePreferences(data)
		if e != nil {
			return liveProblem(c, 400, "invalid_preferences", e.Error())
		}
		pref, err = a.accounts.ReplaceLivePreferences(c.Request().Context(), p.Account.ID, revision, value.Enabled, value.Filters)
	} else {
		pref, err = a.accounts.LivePreferences(c.Request().Context(), p.Account.ID)
	}
	if errors.Is(err, accounts.ErrLiveConflict) {
		return liveProblem(c, 412, "precondition_failed", err.Error())
	}
	if err != nil {
		return accountProblem(c, err)
	}
	c.Response().Header().Set("ETag", liveETag(p.Account.ID, pref.Revision))
	return c.JSON(200, pref)
}
func (a *api) livePreferencesPage(c echo.Context) error {
	privateAccountResponse(c)
	if err := parseAccountForm(c, "enabled", "revision", "location_mode", "live_city", "live_country", "live_category_ids", "live_artist_ids", "live_venue_ids", "live_connection_sources", "live_artist_keyword", "live_venue_keyword", "filter_action", "return_to"); err != nil {
		return a.accountPageError(c, err)
	}
	p, err := a.principal(c, true)
	if err != nil {
		return a.accountPageError(c, err)
	}
	if err = a.checkWrite(c, p); err != nil {
		return a.accountPageError(c, err)
	}
	revision, err := strconv.ParseUint(c.FormValue("revision"), 10, 64)
	if err != nil {
		return a.renderAccount(c, 400, p, "preferences", "Reload preferences before saving.", "", "")
	}
	filters := []activity.Filters{}
	action := c.FormValue("filter_action")
	if action != "" && action != "apply" && action != "reset" && action != "search_artist" && action != "search_venue" {
		return a.renderAccount(c, 400, p, "preferences", "Unknown filter action.", "", "")
	}
	if (action == "search_artist" || action == "search_venue") && c.FormValue("location_mode") == "" {
		return a.renderAccount(c, 400, p, "preferences", "Reload the filter editor before searching.", "", "")
	}
	if c.FormValue("location_mode") != "" {
		f := activity.DefaultPreferences().Filters
		if c.FormValue("filter_action") != "reset" {
			f.Location.Mode = c.FormValue("location_mode")
			if f.Location.Mode == "city" {
				f.Location.City = c.FormValue("live_city")
				f.Location.Country = c.FormValue("live_country")
			}
			f.CategoryIDs = append([]string{}, c.Request().PostForm["live_category_ids"]...)
			f.ArtistIDs = append([]string{}, c.Request().PostForm["live_artist_ids"]...)
			f.VenueIDs = append([]string{}, c.Request().PostForm["live_venue_ids"]...)
			f.Connections.Sources = append([]string{}, c.Request().PostForm["live_connection_sources"]...)
			if len(f.Connections.Sources) > 0 {
				f.Connections.Mode = "any"
			}
		}
		f, err = activity.NormalizeFilters(f)
		if err != nil {
			return a.renderAccount(c, 400, p, "preferences", err.Error(), "", "")
		}
		filters = append(filters, f)
		if action == "search_artist" || action == "search_venue" {
			return a.searchLivePicker(c, p, f, revision)
		}
	}
	_, err = a.accounts.ReplaceLivePreferences(c.Request().Context(), p.Account.ID, revision, c.FormValue("enabled") == "on", filters...)
	if errors.Is(err, accounts.ErrLiveConflict) {
		return a.renderAccount(c, 412, p, "preferences", err.Error(), "", "")
	}
	if err != nil {
		return a.accountPageError(c, err)
	}
	if c.FormValue("return_to") == "/happening-now" {
		return c.Redirect(303, "/happening-now")
	}
	return c.Redirect(303, "/me/preferences?saved=preferences")
}
