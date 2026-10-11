package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/reminders"
	"github.com/sonastea/ticketopia/views/account"
)

func (a *api) reminderRoutes(e *echo.Echo) {
	e.GET("/api/v1/me/reminders", a.remindersHandler)
	e.GET("/api/v1/me/reminders/:event_id/public-on-sale", a.reminderHandler)
	e.PUT("/api/v1/me/reminders/:event_id/public-on-sale", a.reminderHandler)
	e.DELETE("/api/v1/me/reminders/:event_id/public-on-sale", a.reminderHandler)
	e.GET("/me/reminders", a.remindersPage)
	e.GET("/me/reminders/:event_id", a.remindersPage)
	e.POST("/me/reminders/:event_id", a.reminderWritePage)
	e.GET("/reminders/unsubscribe/:token", a.unsubscribePage)
	e.POST("/reminders/unsubscribe/:token", a.unsubscribePage)
}
func (a *api) remindersHandler(c echo.Context) error {
	p, err := a.principal(c, false)
	if err != nil {
		return accountProblem(c, err)
	}
	if a.reminders == nil {
		return accountProblem(c, reminders.ErrUnavailable)
	}
	privateAccountResponse(c)
	page, err := a.reminders.List(c.Request().Context(), p.Account.ID, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, page)
}
func (a *api) reminderHandler(c echo.Context) error {
	p, err := a.requireAPI(c, c.Request().Method != "GET", false)
	if err != nil {
		return accountProblem(c, err)
	}
	if a.reminders == nil {
		return accountProblem(c, reminders.ErrUnavailable)
	}
	privateAccountResponse(c)
	id, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	if c.Request().Method == "DELETE" {
		data, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, 1))
		if err != nil || len(data) != 0 {
			return accountProblem(c, &accounts.ValidationError{Field: "body", Message: "send an empty request body"})
		}
		if err = a.reminders.Remove(c.Request().Context(), p.Account.ID, id); err != nil {
			return accountProblem(c, err)
		}
		return c.NoContent(204)
	}
	var item reminders.Item
	if c.Request().Method == "PUT" {
		var body struct {
			LeadMinutes *int `json:"lead_minutes"`
		}
		if err = decodeAccountJSON(c, &body); err != nil {
			return accountProblem(c, err)
		}
		if body.LeadMinutes == nil {
			return accountProblem(c, &accounts.ValidationError{Field: "lead_minutes", Message: "specify a lead time in minutes"})
		}
		item, err = a.reminders.Set(c.Request().Context(), p.Account.ID, id, *body.LeadMinutes)
	} else {
		item, err = a.reminders.Get(c.Request().Context(), p.Account.ID, id)
	}
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, item)
}
func (a *api) remindersPage(c echo.Context) error {
	privateAccountResponse(c)
	if !a.enabled() {
		return render(c, 200, account.Reminders(account.ReminderPage{}))
	}
	p, err := a.principal(c, true)
	if errors.Is(err, accounts.ErrUnauthenticated) {
		return c.Redirect(303, "/auth/sign-in?return_to="+url.QueryEscape(c.Request().URL.RequestURI()))
	}
	if err != nil {
		return a.accountPageError(c, err)
	}
	page := account.ReminderPage{Account: p.Account, CSRF: p.CSRF, Enabled: a.reminders != nil, Lead: 30, ReturnURL: c.Request().URL.RequestURI()}
	if a.reminders == nil {
		return render(c, 200, account.Reminders(page))
	}
	if c.Param("event_id") == "" {
		page.List, err = a.reminders.List(c.Request().Context(), p.Account.ID, c.QueryParams())
	} else {
		if len(c.QueryParams()) != 0 {
			return a.accountPageError(c, &accounts.ValidationError{Field: "query", Message: "this route does not accept query parameters"})
		}
		id, idErr := savedEventID(c)
		if idErr != nil {
			return a.accountPageError(c, idErr)
		}
		if a.saved == nil {
			return a.accountPageError(c, reminders.ErrUnavailable)
		}
		item, getErr := a.saved.Get(c.Request().Context(), p.Account.ID, id)
		if getErr != nil {
			return a.accountPageError(c, getErr)
		}
		page.Event = &item.Event
		configured, getErr := a.reminders.Get(c.Request().Context(), p.Account.ID, id)
		if getErr == nil {
			page.Own = &configured
			page.Lead = configured.LeadMinutes
		} else if !errors.Is(getErr, reminders.ErrNotFound) {
			err = getErr
		}
	}
	if err != nil {
		return a.accountPageError(c, err)
	}
	return render(c, 200, account.Reminders(page))
}
func (a *api) reminderWritePage(c echo.Context) error {
	privateAccountResponse(c)
	if err := parseAccountForm(c, "action", "lead_minutes"); err != nil {
		return a.accountPageError(c, err)
	}
	p, err := a.principal(c, true)
	if err != nil {
		return a.accountPageError(c, err)
	}
	if err = a.checkWrite(c, p); err != nil {
		return a.accountPageError(c, err)
	}
	if a.reminders == nil {
		return a.accountPageError(c, reminders.ErrUnavailable)
	}
	id, err := savedEventID(c)
	if err != nil {
		return a.accountPageError(c, err)
	}
	switch c.FormValue("action") {
	case "stop":
		err = a.reminders.Remove(c.Request().Context(), p.Account.ID, id)
	case "set":
		lead, parseErr := strconv.Atoi(c.FormValue("lead_minutes"))
		if parseErr != nil {
			return a.accountPageError(c, &accounts.ValidationError{Field: "lead_minutes", Message: "choose a number of minutes"})
		}
		_, err = a.reminders.Set(c.Request().Context(), p.Account.ID, id, lead)
	default:
		err = &accounts.ValidationError{Field: "action", Message: "choose set or stop"}
	}
	if err != nil {
		return a.accountPageError(c, err)
	}
	return c.Redirect(303, "/me/reminders/"+url.PathEscape(id))
}
func (a *api) unsubscribePage(c echo.Context) error {
	privateAccountResponse(c)
	c.Response().Header().Set("Referrer-Policy", "no-referrer")
	if a.reminders == nil {
		return a.accountPageError(c, reminders.ErrUnavailable)
	}
	token := c.Param("token")
	if !accounts.ValidID(token) {
		return a.accountPageError(c, reminders.ErrNotFound)
	}
	done := false
	if c.Request().Method == "POST" {
		if err := parseAccountForm(c); err != nil {
			return a.accountPageError(c, err)
		}
		if err := a.reminders.Unsubscribe(c.Request().Context(), token); err != nil {
			return a.accountPageError(c, err)
		}
		done = true
	}
	// The unguessable token is a capability for this reminder only. GET never
	// mutates state, so mail-link scanners cannot silently unsubscribe anyone.
	return render(c, 200, account.ReminderUnsubscribe(token, done))
}
