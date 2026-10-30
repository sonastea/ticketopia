package api

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/calendar"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/views/home"
)

func (a *api) eventsHandler(c echo.Context) error {
	query, err := discovery.ParseQuery(c.QueryParams(), a.clock())
	if err != nil {
		return problem(c, err)
	}
	data, err := a.events.Events(c.Request().Context(), query)
	if err != nil {
		return problem(c, err)
	}
	return a.publicJSON(c, data, eventCachePolicy(data.Meta.Stale))
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
	return a.publicJSON(c, data, eventCachePolicy(data.Meta.Stale))
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
	return a.publicJSON(c, h, eventCachePolicy(false))
}

func (a *api) genresHandler(c echo.Context) error {
	if len(c.QueryParams()) > 0 {
		return problem(c, &discovery.ValidationError{Field: "query", Message: "the genre catalog does not accept filters"})
	}
	data, err := a.events.Genres(c.Request().Context())
	if err != nil {
		return problem(c, err)
	}
	return a.publicJSON(c, data, taxonomyCachePolicy(data.Meta, a.clock()))
}

func (a *api) categoriesHandler(c echo.Context) error {
	if len(c.QueryParams()) > 0 {
		return problem(c, &discovery.ValidationError{Field: "query", Message: "the category catalog does not accept filters"})
	}
	data, err := a.events.Categories(c.Request().Context())
	if err != nil {
		return problem(c, err)
	}
	return a.publicJSON(c, data, taxonomyCachePolicy(data.Meta, a.clock()))
}

func (a *api) retrieveEventsHandler(c echo.Context) error {
	// IP- and cookie-derived defaults must not be shared by an HTTP intermediary.
	c.Response().Header().Set("Cache-Control", "private, no-store")
	privateAccountResponse(c)
	if target, ok := canonicalPage(c.Request().URL, homeParam); ok {
		return c.Redirect(http.StatusFound, target)
	}
	filters, selectedID, section, err := webSearch(c.QueryParams())
	page := home.SearchPage{SelectedID: selectedID, Section: section}
	page.ActionReturnURL = c.Request().URL.RequestURI()
	now := a.clock()
	status := http.StatusOK
	partial := c.Request().Header.Get("HX-Request") == "true"
	// through is HTML-only: the Discover URL carries the last loaded page's
	// cursor, so reload and Back render every loaded page in the first response.
	// A stale or edited value on a whole page falls back to the requested page.
	var through string
	if values, ok := filters["through"]; ok {
		if len(values) == 1 && values[0] != "" {
			through = values[0]
		} else if partial && err == nil {
			err = &discovery.ValidationError{Field: "through", Message: "use one results cursor"}
		}
		delete(filters, "through")
	}
	if err == nil {
		page.Filters, err = discovery.ParseQuery(filters, now)
	}
	page.PageCursor = filters.Get("cursor")
	if err == nil {
		page.LocationSource, err = a.locateSearch(c, &page.Filters)
		page.NeedsLocation = page.Filters.City == "" && page.Filters.VenueID == ""
	}
	// Without start_date the range starts today in the browsed city, not on the
	// UTC date, which is already tomorrow during a US evening. A city's zone is
	// learned from its results, so its first search may be corrected once.
	localDefault := err == nil && !page.NeedsLocation && filters.Get("start_date") == "" && filters.Get("cursor") == ""
	localized := localDefault && a.localizeDefaultDates(&page.Filters, filters, now)
	if err == nil && !page.NeedsLocation {
		// Event results take priority: a failed optional catalog refresh must not
		// pause this search before it reaches the shared provider request gate.
		page.Events, err = a.events.Events(c.Request().Context(), page.Filters)
		if err == nil && localDefault && !localized && a.localizeDefaultDates(&page.Filters, filters, now) {
			page.Events, err = a.events.Events(c.Request().Context(), page.Filters)
		}
		if err == nil && through != "" {
			var throughErr error
			page.Events, page.PageCursor, throughErr = a.restorePages(c.Request().Context(), page.Filters, page.Events, page.PageCursor, through, now)
			if partial {
				err = throughErr
			}
		}
		if !partial {
			var catalogErr error
			page.Categories, catalogErr = a.optionalCategories(c.Request().Context())
			page.CategoriesUnavailable = catalogErr != nil
		}
	}
	returnValues := page.Filters.Values()
	if cursor := filters.Get("cursor"); cursor != "" {
		returnValues.Set("cursor", cursor)
	}
	page.ReturnURL = "/?" + returnValues.Encode()
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
	// Restored pages can exceed the 100-ID batch of private and activity reads,
	// so they are read in a few bounded batches, never one query per event.
	page.Saves = a.saveView(c, ids[:min(len(ids), stateBatch)])
	page.Interests = a.interestView(c, ids[:min(len(ids), stateBatch)])
	for start := stateBatch; start < len(ids); start += stateBatch {
		batch := ids[start:min(start+stateBatch, len(ids))]
		saves, interests := a.saveView(c, batch), a.interestView(c, batch)
		page.Saves.States = mergeStates(page.Saves.States, saves.States)
		page.Saves.Error = cmp.Or(page.Saves.Error, saves.Error)
		page.Interests.States = mergeStates(page.Interests.States, interests.States)
		page.Interests.Error = cmp.Or(page.Interests.Error, interests.Error)
	}
	if a.eventActivity != nil && !page.NeedsLocation && page.Error == "" {
		// Only the rendered results are enriched, never the entire result set or
		// the separately selected event. Optional failure leaves discovery usable.
		activityIDs := make([]string, 0, len(page.Events.Items))
		for _, event := range page.Events.Items {
			activityIDs = append(activityIDs, event.ID)
		}
		for start := 0; start < len(activityIDs); start += stateBatch {
			previews, activityErr := a.eventActivity.Previews(c.Request().Context(), activityIDs[start:min(start+stateBatch, len(activityIDs))])
			if activityErr != nil {
				page.Activity = nil
				page.ActivityError = "Community previews couldn't load. Open an event to check its activity."
				break
			}
			page.Activity = mergeStates(page.Activity, previews)
		}
	}
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
			community := home.EventPage{Detail: detail}
			community.Recommendations.Enabled = a.recommendationsEnabled()
			if page.Section == "community" || page.Thread != nil {
				a.loadEventCommunity(c, &community)
			}
			page.Recommendations = community.Recommendations
			page.Participants = community.Participants
			page.ParticipantsError = community.ParticipantsError
			if page.Thread == nil && page.Section == "discussion" {
				page.Discussions = a.discussionView(c, selectedID)
			}
		}
	}
	return render(c, status, home.Index(page))
}

// optionalCategories reads the taxonomy for a page whose content does not
// depend on it. Its shared cache refresh can continue, but it must not hold an
// otherwise usable page behind a slow provider.
func (a *api) optionalCategories(ctx context.Context) (models.CategoryList, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	return a.events.Categories(ctx)
}

// localizeDefaultDates moves an omitted start_date to the city's calendar day
// once its zone is known, keeping an explicit end_date. It reports whether the
// filters changed; an unknown zone or an invalid range leaves them unchanged.
func (a *api) localizeDefaultDates(q *discovery.Query, filters url.Values, now time.Time) bool {
	today, ok := a.events.CityToday(*q, now)
	if !ok || today == q.StartDate {
		return false
	}
	values := q.Values()
	values.Set("start_date", today)
	if filters.Get("end_date") == "" {
		values.Del("end_date")
	}
	localized, err := discovery.ParseQuery(values, now)
	if err != nil {
		return false
	}
	*q = localized
	return true
}

const (
	// stateBatch matches the ID limit of saved, interest and activity reads.
	stateBatch = 100
	// A restore stops at either bound, so a shifted result set whose cursor
	// never recurs costs a bounded walk rather than the whole search.
	maxRestoredPages  = 10
	maxRestoredEvents = 200
)

// restorePages appends the pages after list, which was read with cursor,
// through the page read with through: the last page the browser had loaded.
// It returns the cursor of the last rendered page. A failed later page ends the
// walk early and leaves its cursor to the Load more control.
func (a *api) restorePages(ctx context.Context, q discovery.Query, list models.EventList, cursor, through string, now time.Time) (models.EventList, string, error) {
	values := q.Values()
	values.Set("cursor", through)
	target, err := discovery.ParseQuery(values, now)
	if err != nil {
		return list, cursor, &discovery.ValidationError{Field: "through", Message: "invalid cursor or filters changed; start a new search"}
	}
	pages := maxRestoredPages
	if !q.Stored && !target.Stored {
		// Provider cursors carry their page number, so the walk length is known.
		pages = min(pages, target.Page-q.Page+1)
	}
	seen := make(map[string]bool, len(list.Items))
	for _, event := range list.Items {
		seen[event.ID] = true
	}
	// Only whole pages fit: a trimmed page's remaining events would be
	// unreachable from the Load more cursor that follows it.
	for n := 1; n < pages && cursor != through && list.NextCursor != nil && len(list.Items)+q.Limit <= maxRestoredEvents; n++ {
		values.Set("cursor", *list.NextCursor)
		next, err := discovery.ParseQuery(values, now)
		if err != nil {
			break
		}
		more, err := a.events.Events(ctx, next)
		if err != nil {
			break
		}
		cursor = *list.NextCursor
		// Provider pages can overlap when results shift between reads; a row
		// already rendered keeps its first position. Building a new slice also
		// leaves cached result slices untouched.
		items := slices.Clip(list.Items)
		for _, event := range more.Items {
			if !seen[event.ID] {
				seen[event.ID] = true
				items = append(items, event)
			}
		}
		list.Items = items
		list.NextCursor, list.Limited = more.NextCursor, more.Limited
		list.Meta.Stale = list.Meta.Stale || more.Meta.Stale
		// The oldest supporting read dates the whole list.
		if !more.Meta.DataAsOf.IsZero() && (list.Meta.DataAsOf.IsZero() || more.Meta.DataAsOf.Before(list.Meta.DataAsOf)) {
			list.Meta.DataAsOf = more.Meta.DataAsOf
		}
		if !more.Meta.FreshUntil.IsZero() && (list.Meta.FreshUntil.IsZero() || more.Meta.FreshUntil.Before(list.Meta.FreshUntil)) {
			list.Meta.FreshUntil = more.Meta.FreshUntil
		}
	}
	return list, cursor, nil
}

func mergeStates[V any](dst, src map[string]V) map[string]V {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]V, len(src))
	}
	maps.Copy(dst, src)
	return dst
}

func errorMessage(err error) (int, string) {
	var invalid *discovery.ValidationError
	if errors.As(err, &invalid) {
		return http.StatusBadRequest, invalid.Error()
	}
	if errors.Is(err, discovery.ErrNotFound) {
		return http.StatusNotFound, "This event is no longer available. Try another search."
	}
	if errors.Is(err, calendar.ErrNoDate) {
		return http.StatusNotFound, "This event's date hasn't been announced yet, so it can't be added to a calendar."
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
	} else if errors.Is(err, calendar.ErrNoDate) {
		code = "date_not_announced"
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
