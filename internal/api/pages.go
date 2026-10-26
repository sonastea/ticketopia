package api

import (
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/saved"
	"github.com/sonastea/ticketopia/views/home"
)

// canonicalPage drops query keys an HTML page does not support, such as utm_*
// or fbclid added to shared links, so the caller can redirect instead of
// rejecting them. Kept pairs retain their order and encoding, so duplicate or
// invalid supported values still reach the parser. /api/v1 routes stay strict.
func canonicalPage(u *url.URL, supported func(string) bool) (string, bool) {
	if u.RawQuery == "" {
		return "", false
	}
	parts := strings.Split(u.RawQuery, "&")
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		key, _, _ := strings.Cut(part, "=")
		if key, err := url.QueryUnescape(key); err == nil && supported(key) {
			kept = append(kept, part)
		}
	}
	if len(kept) == len(parts) {
		return "", false
	}
	target := u.EscapedPath()
	if len(kept) > 0 {
		target += "?" + strings.Join(kept, "&")
	}
	return target, true
}

var webSelectionParams = []string{"selected_event", "section", "recommend", "selected_thread", "reply_to", "discussion_cursor"}

func homeParam(key string) bool {
	return discovery.QueryParam(key) || key == "genre_category_id" || key == "through" || slices.Contains(webSelectionParams, key)
}

func eventPageParam(key string) bool {
	return key == "section" || key == "return_to" || key == "recommend"
}

// Web selection is deliberately separate from the shared discovery/API filters.
func webSearch(values url.Values) (url.Values, string, string, error) {
	filters := make(url.Values, len(values))
	for key, value := range values {
		filters[key] = value
	}
	id, section := filters.Get("selected_event"), filters.Get("section")
	thread := filters.Get("selected_thread")
	if thread != "" && (id == "" || section != "discussion" || discussions.PostID(thread) != nil) {
		return nil, "", "", &discovery.ValidationError{Field: "selected_thread", Message: "open a valid thread for a selected event's discussion"}
	}
	if parent := filters.Get("reply_to"); parent != "" && (thread == "" || discussions.PostID(parent) != nil) {
		return nil, "", "", &discovery.ValidationError{Field: "reply_to", Message: "choose a contribution in the selected thread"}
	}
	if cursor := filters.Get("discussion_cursor"); cursor != "" && (id == "" || section != "discussion" || len(cursor) > 1024) {
		return nil, "", "", &discovery.ValidationError{Field: "discussion_cursor", Message: "paginate the selected thread"}
	}
	if values, ok := filters["recommend"]; ok && (len(values) != 1 || values[0] != "true" || id == "" || section != "community") {
		return nil, "", "", &discovery.ValidationError{Field: "recommend", Message: "open recommendations for a selected event's community section"}
	}
	for _, key := range webSelectionParams {
		if len(filters[key]) > 1 {
			return nil, "", "", &discovery.ValidationError{Field: key, Message: "use one value"}
		}
		delete(filters, key)
	}
	// The ordinary GET form carries the category its genre options belonged to.
	// Switching category clears the old genre even without browser enhancement.
	if previous, ok := filters["genre_category_id"]; ok {
		if len(previous) != 1 {
			return nil, "", "", &discovery.ValidationError{Field: "genre_category_id", Message: "use one value"}
		}
		if len(filters["category_id"]) == 1 && len(filters["genre_id"]) == 1 && strings.TrimSpace(previous[0]) != strings.TrimSpace(filters.Get("category_id")) {
			filters.Del("genre_id")
		}
		delete(filters, "genre_category_id")
	}
	if id != "" && saved.ValidateID(id) != nil {
		return nil, "", "", &discovery.ValidationError{Field: "selected_event", Message: "invalid event ID"}
	}
	if section == "" {
		section = "overview"
	}
	if section != "overview" && section != "discussion" && section != "community" {
		return nil, "", "", &discovery.ValidationError{Field: "section", Message: "choose overview, discussion, or community"}
	}
	return filters, id, section, nil
}

func safeReturnURL(raw string) string {
	if strings.HasPrefix(raw, "/happening-now") {
		u, err := url.Parse(raw)
		if err != nil || len(raw) > 8192 || u.IsAbs() || u.Host != "" || u.Path != "/happening-now" || u.Fragment != "" {
			return "/"
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return "/"
		}
		if _, err := happeningFilters(q); err != nil {
			return "/"
		}
		return u.String()
	}
	if strings.HasPrefix(raw, "/me/discussions") || strings.HasPrefix(raw, "/me/notifications") {
		return safeDiscussionFollowReturn(raw)
	}
	if strings.HasPrefix(raw, "/radar") {
		return safeRadarReturn(raw)
	}
	if isDiscussionReturn(raw) {
		return safeDiscussionReturn(raw)
	}
	if strings.HasPrefix(raw, "/community") {
		return safeCommunityReturn(raw)
	}
	if strings.HasPrefix(raw, "/users/") {
		return safeProfileReturn(raw)
	}
	if strings.HasPrefix(raw, "/me/interests") {
		if result := safeInterestReturn(raw); strings.HasPrefix(result, "/me/interests") {
			return result
		}
		return "/"
	}
	if strings.HasPrefix(raw, "/saved") {
		if result := safeSaveReturn(raw); strings.HasPrefix(result, "/saved") {
			return result
		}
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || u.IsAbs() || u.Host != "" || u.Path != "/" || u.Fragment != "" {
		return "/"
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "/"
	}
	filters, _, _, err := webSearch(values)
	if err != nil {
		return "/"
	}
	// Discover ignores a stale through cursor, so it needs no validation here.
	filters.Del("through")
	if _, err := discovery.ParseQuery(filters, time.Now()); err != nil {
		return "/"
	}
	return u.String()
}

func (a *api) eventPageHandler(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	c.Response().Header().Set("Vary", "X-Ticketopia-Panel, X-Ticketopia-Section")
	if target, ok := canonicalPage(c.Request().URL, eventPageParam); ok {
		return c.Redirect(http.StatusFound, target)
	}
	page := home.EventPage{ReturnURL: safeReturnURL(c.QueryParam("return_to")), Section: "overview"}
	privateAccountResponse(c)
	page.ActionReturnURL = c.Request().URL.RequestURI()
	status := http.StatusOK
	id, err := url.PathUnescape(c.Param("event_id"))
	if err == nil {
		for key, values := range c.QueryParams() {
			if !eventPageParam(key) || len(values) != 1 {
				err = &discovery.ValidationError{Field: key, Message: "use one value for a supported event parameter"}
				break
			}
		}
	}
	if section := c.QueryParam("section"); section != "" {
		page.Section = section
		if section != "overview" && section != "discussion" && section != "community" {
			err = &discovery.ValidationError{Field: "section", Message: "choose overview, discussion, or community"}
		}
	}
	if values, ok := c.QueryParams()["recommend"]; ok && (len(values) != 1 || values[0] != "true" || page.Section != "community") {
		err = &discovery.ValidationError{Field: "recommend", Message: "open recommendations in this event's community section"}
	}
	if err == nil {
		page.Detail, err = a.eventDetail(c.Request().Context(), id)
		if err == nil {
			page.Saves = a.saveView(c, []string{id})
			page.Interests = a.interestView(c, []string{id})
			page.Recommendations.Enabled = a.recommendationsEnabled()
			if page.Section == "community" {
				a.loadEventCommunity(c, &page)
			}
			if page.Section == "discussion" {
				page.Discussions = a.discussionView(c, id)
			}
		}
	}
	if err != nil {
		status, page.Error = errorMessage(err)
		setRetryAfter(c, err)
		if c.Request().Header.Get("X-Ticketopia-Panel") == "true" || c.Request().Header.Get("X-Ticketopia-Section") == "true" {
			return c.String(status, page.Error)
		}
	}
	if c.Request().Header.Get("X-Ticketopia-Panel") == "true" {
		page.ActionReturnURL = home.SelectionURL(page.ReturnURL, id, page.Section)
		if c.Request().Header.Get("X-Ticketopia-Section") == "true" {
			return render(c, status, home.EventSectionResponse(page, true))
		}
		return render(c, status, home.EventContext(page))
	}
	if page.Error == "" {
		page.ActionReturnURL = home.EventActionURL(page)
	}
	if c.Request().Header.Get("X-Ticketopia-Section") == "true" {
		return render(c, status, home.EventSectionResponse(page, false))
	}
	return render(c, status, home.Event(page))
}

// Community is loaded only for Recommendations or explicitly selected threads.
// Optional community failures must not hide event details.
func (a *api) loadEventCommunity(c echo.Context, page *home.EventPage) {
	id := page.Detail.Item.ID
	page.Recommendations = a.recommendationView(c, id)
	if a.interestEnabled() {
		var err error
		page.Participants, err = a.interests.Participants(c.Request().Context(), id, nil)
		if err != nil {
			page.ParticipantsError = "Public participants couldn't load. Refresh to try again."
		}
	}
}

func destinationHandler(active, title string) echo.HandlerFunc {
	return func(c echo.Context) error {
		return render(c, http.StatusOK, home.Destination(active, title))
	}
}
