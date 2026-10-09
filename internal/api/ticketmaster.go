package api

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/views/home"
)

func (a *api) eventsHandler(c echo.Context) error {
	query, err := discovery.ParseQuery(c.QueryParams(), time.Now())
	if err != nil {
		return problem(c, err)
	}
	data, err := a.events.Events(c.Request().Context(), query)
	if err != nil {
		return problem(c, err)
	}
	return c.JSON(http.StatusOK, data)
}

func (a *api) eventHandler(c echo.Context) error {
	// Echo routes against RawPath when present, so encoded provider-scoped IDs
	// must be decoded once before application validation.
	id, err := url.PathUnescape(c.Param("event_id"))
	if err != nil {
		return problem(c, &discovery.ValidationError{Field: "event_id", Message: "invalid URL encoding"})
	}
	data, err := a.eventDetail(c.Request().Context(), id)
	if err != nil {
		return problem(c, err)
	}
	return c.JSON(http.StatusOK, data)
}

func (a *api) eventHistoryHandler(c echo.Context) error {
	id, err := url.PathUnescape(c.Param("event_id"))
	if err != nil {
		return problem(c, &discovery.ValidationError{Field: "event_id", Message: "invalid URL encoding"})
	}
	values := c.QueryParams()
	for k, v := range values {
		if (k != "before" && k != "limit") || len(v) != 1 {
			return problem(c, &discovery.ValidationError{Field: k, Message: "use one supported parameter"})
		}
	}
	var before time.Time
	if value := values.Get("before"); value != "" {
		before, err = time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return problem(c, &discovery.ValidationError{Field: "before", Message: "use an RFC3339 observation timestamp"})
		}
	}
	limit := 20
	if value := values.Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
		if err != nil {
			return problem(c, &discovery.ValidationError{Field: "limit", Message: "use 1 to 100"})
		}
	}
	h, err := a.events.History(c.Request().Context(), id, before, limit)
	if err != nil {
		return problem(c, err)
	}
	return c.JSON(http.StatusOK, h)
}

func (a *api) genresHandler(c echo.Context) error {
	if len(c.QueryParams()) > 0 {
		return problem(c, &discovery.ValidationError{Field: "query", Message: "the genre catalog does not accept filters"})
	}
	data, err := a.events.Genres(c.Request().Context())
	if err != nil {
		return problem(c, err)
	}
	return c.JSON(http.StatusOK, data)
}

func (a *api) categoriesHandler(c echo.Context) error {
	if len(c.QueryParams()) > 0 {
		return problem(c, &discovery.ValidationError{Field: "query", Message: "the category catalog does not accept filters"})
	}
	data, err := a.events.Categories(c.Request().Context())
	if err != nil {
		return problem(c, err)
	}
	return c.JSON(http.StatusOK, data)
}

func (a *api) retrieveEventsHandler(c echo.Context) error {
	// IP- and cookie-derived defaults must not be shared by an HTTP intermediary.
	c.Response().Header().Set("Cache-Control", "private, no-store")
	privateAccountResponse(c)
	filters, selectedID, section, err := webSearch(c.QueryParams())
	page := home.SearchPage{SelectedID: selectedID, Section: section}
	page.ActionReturnURL = c.Request().URL.RequestURI()
	if err == nil {
		page.Filters, err = discovery.ParseQuery(filters, time.Now())
	}
	status := http.StatusOK
	partial := c.Request().Header.Get("HX-Request") == "true"
	if err == nil {
		page.LocationSource, err = a.locateSearch(c, &page.Filters)
		page.NeedsLocation = page.Filters.City == "" && page.Filters.VenueID == ""
	}
	returnValues := page.Filters.Values()
	if cursor := filters.Get("cursor"); cursor != "" {
		returnValues.Set("cursor", cursor)
	}
	page.ReturnURL = "/?" + returnValues.Encode()
	if err == nil && !page.NeedsLocation {
		// Event results take priority: a failed optional catalog refresh must not
		// pause this search before it reaches the shared provider request gate.
		page.Events, err = a.events.Events(c.Request().Context(), page.Filters)
		if !partial {
			// Taxonomy is optional. Its shared cache refresh can continue, but it
			// must not hold an otherwise usable page behind a slow provider.
			catalogCtx, cancel := context.WithTimeout(c.Request().Context(), time.Second)
			var catalogErr error
			page.Categories, catalogErr = a.events.Categories(catalogCtx)
			cancel()
			page.CategoriesUnavailable = catalogErr != nil
		}
	}
	if err != nil {
		status, page.Error = errorMessage(err)
		setRetryAfter(c, err)
		if partial {
			return c.String(status, page.Error)
		}
	}
	ids := make([]string, 0, len(page.Events.Items)+1)
	for _, event := range page.Events.Items {
		ids = append(ids, event.ID)
	}
	if selectedID != "" {
		ids = append(ids, selectedID)
	}
	page.Saves = a.saveView(c, ids)
	page.Interests = a.interestView(c, ids)
	if partial {
		if page.NeedsLocation {
			return c.String(http.StatusBadRequest, "Choose a city before loading events.")
		}
		return render(c, status, home.MoreEventsList(page))
	}
	if err == nil && selectedID != "" {
		var detail models.EventDetail
		var selectionErr error
		if thread := c.QueryParam("selected_thread"); thread != "" || c.QueryParam("discussion_cursor") != "" {
			values := url.Values{"return_to": {page.ReturnURL}}
			for source, target := range map[string]string{"reply_to": "reply_to", "discussion_cursor": "cursor"} {
				if value := c.QueryParam(source); value != "" {
					values.Set(target, value)
				}
			}
			threadURL := home.ThreadURL(selectedID, thread, page.ReturnURL)
			u, _ := url.Parse(threadURL)
			u.RawQuery = values.Encode()
			p, _, e := a.loadDiscussionPage(c, selectedID, thread, values, u.String())
			selectionErr = e
			if e == nil {
				page.Thread = &p
				detail.Item = p.Event
				detail.Meta = p.Meta
			}
		} else {
			detail, selectionErr = a.eventDetail(c.Request().Context(), selectedID)
		}
		if selectionErr != nil {
			_, page.SelectionError = errorMessage(selectionErr)
		} else {
			page.Selected = &detail
			if section == "community" {
				page.Recommendations = a.recommendationView(c, selectedID)
			}
			if section == "discussion" && page.Thread == nil {
				page.Discussions = a.discussionView(c, selectedID)
			}
			if a.interestEnabled() && section == "community" {
				page.Participants, selectionErr = a.interests.Participants(c.Request().Context(), selectedID, nil)
				if selectionErr != nil {
					page.ParticipantsError = "Public participants couldn't load. Refresh to try again."
				}
			}
		}
	}
	return render(c, status, home.Index(page))
}

func errorMessage(err error) (int, string) {
	var invalid *discovery.ValidationError
	if errors.As(err, &invalid) {
		return http.StatusBadRequest, invalid.Error()
	}
	if errors.Is(err, discovery.ErrNotFound) {
		return http.StatusNotFound, "This event is no longer available. Try another search."
	}
	return http.StatusServiceUnavailable, "We couldn't refresh the events. Please try again shortly."
}

func setRetryAfter(c echo.Context, err error) {
	var unavailable *discovery.UnavailableError
	if errors.As(err, &unavailable) {
		c.Response().Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(unavailable.RetryAfter.Seconds())))))
	} else if errors.Is(err, context.DeadlineExceeded) {
		c.Response().Header().Set("Retry-After", "30")
	}
}

func problem(c echo.Context, err error) error {
	status, detail := errorMessage(err)
	code := "provider_unavailable"
	var fields map[string]string
	var invalid *discovery.ValidationError
	if errors.As(err, &invalid) {
		code = "invalid_query"
		fields = map[string]string{invalid.Field: invalid.Message}
	} else if errors.Is(err, discovery.ErrNotFound) {
		code = "event_not_found"
	}
	setRetryAfter(c, err)
	c.Response().Header().Set(echo.HeaderContentType, "application/problem+json")
	return c.JSON(status, struct {
		Type   string            `json:"type"`
		Title  string            `json:"title"`
		Status int               `json:"status"`
		Code   string            `json:"code"`
		Detail string            `json:"detail"`
		Fields map[string]string `json:"fields,omitempty"`
	}{"about:blank", http.StatusText(status), status, code, detail, fields})
}
