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
			var catalogErr error
			page.Categories, catalogErr = a.events.Categories(c.Request().Context())
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
	if partial {
		if page.NeedsLocation {
			return c.String(http.StatusBadRequest, "Choose a city before loading events.")
		}
		return render(c, status, home.MoreEventsList(page))
	}
	if err == nil && selectedID != "" {
		detail, selectionErr := a.eventDetail(c.Request().Context(), selectedID)
		if selectionErr != nil {
			_, page.SelectionError = errorMessage(selectionErr)
		} else {
			page.Selected = &detail
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
