package api

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/views/home"
)

// Web selection is deliberately separate from the shared discovery/API filters.
func webSearch(values url.Values) (url.Values, string, string, error) {
	filters := make(url.Values, len(values))
	for key, value := range values {
		filters[key] = value
	}
	id, section := filters.Get("selected_event"), filters.Get("section")
	if values, ok := filters["recommend"]; ok && (len(values) != 1 || values[0] != "true" || id == "" || section != "community") {
		return nil, "", "", &discovery.ValidationError{Field: "recommend", Message: "open recommendations for a selected event's community section"}
	}
	for _, key := range []string{"selected_event", "section", "recommend"} {
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
	if len(id) > 160 {
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
	if _, err := discovery.ParseQuery(values, time.Now()); err != nil {
		return "/"
	}
	return u.String()
}

func (a *api) eventPageHandler(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	c.Response().Header().Set("Vary", "X-Ticketopia-Panel")
	page := home.EventPage{ReturnURL: safeReturnURL(c.QueryParam("return_to")), Section: "overview"}
	privateAccountResponse(c)
	page.ActionReturnURL = c.Request().URL.RequestURI()
	status := http.StatusOK
	id, err := url.PathUnescape(c.Param("event_id"))
	if err == nil {
		for key, values := range c.QueryParams() {
			if (key != "section" && key != "return_to" && key != "recommend") || len(values) != 1 {
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
			if page.Section == "community" {
				page.Recommendations = a.recommendationView(c, id)
			}
			if page.Section == "discussion" {
				page.Discussions = a.discussionView(c, id)
			}
			if a.interestEnabled() && page.Section == "community" {
				page.Participants, err = a.interests.Participants(c.Request().Context(), id, nil)
				if err != nil {
					page.ParticipantsError = "Public participants couldn't load. Refresh to try again."
					err = nil
				}
			}
		}
	}
	if err != nil {
		status, page.Error = errorMessage(err)
		setRetryAfter(c, err)
		if c.Request().Header.Get("X-Ticketopia-Panel") == "true" {
			return c.String(status, page.Error)
		}
	}
	if c.Request().Header.Get("X-Ticketopia-Panel") == "true" {
		page.ActionReturnURL = home.SelectionURL(page.ReturnURL, id, page.Section)
		return render(c, status, home.EventContext(page))
	}
	return render(c, status, home.Event(page))
}

func destinationHandler(active, title string) echo.HandlerFunc {
	return func(c echo.Context) error {
		return render(c, http.StatusOK, home.Destination(active, title))
	}
}
