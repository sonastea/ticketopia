package api

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/calendar"
	"github.com/sonastea/ticketopia/internal/discovery"
)

const calendarMIME = "text/calendar; charset=utf-8"

// eventCalendarHandler shares event detail's public read and cache policy. The
// file carries only public event facts, so one representation serves everyone.
func (a *api) eventCalendarHandler(c echo.Context) error {
	id, err := url.PathUnescape(c.Param("event_id"))
	if err != nil {
		return problem(c, &discovery.ValidationError{Field: "event_id", Message: "invalid URL encoding"})
	}
	if len(c.QueryParams()) > 0 {
		return problem(c, &discovery.ValidationError{Field: "query", Message: "the calendar file does not accept parameters"})
	}
	data, err := a.eventDetail(c.Request().Context(), id)
	if err != nil {
		return problem(c, err)
	}
	details := calendar.Details{Stamp: data.Meta.DataAsOf}
	if a.authConfig.BaseURL != "" {
		details.PageURL = a.authConfig.BaseURL + "/events/" + url.PathEscape(id)
	}
	policy := eventCachePolicy(data.Meta.Stale)
	if details.Stamp.IsZero() {
		// Without a collection time the stamp is the request's, so the bytes
		// differ per response and must not be revalidated.
		details.Stamp, policy = a.clock(), "no-store"
	}
	body, err := calendar.Event(data.Item, details)
	if err != nil {
		return problem(c, err)
	}
	c.Response().Header().Set(echo.HeaderContentDisposition, fmt.Sprintf(`attachment; filename="%s.ics"`, calendarFilename(data.Item.Name)))
	return a.publicBytes(c, calendarMIME, body, "", policy)
}

// calendarFilename is an ASCII slug of the event name, so the header needs no
// quoting or encoding rules.
func calendarFilename(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(name) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(r)
			dash = false
			if b.Len() >= 60 {
				break
			}
		} else {
			dash = true
		}
	}
	if b.Len() == 0 {
		return "event"
	}
	return b.String()
}
