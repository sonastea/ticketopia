package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

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
	page := account.ReminderPage{Account: p.Account, CSRF: p.CSRF, Enabled: a.reminders != nil, Lead: 30, Now: time.Now(), ReturnURL: c.Request().URL.RequestURI()}
	if a.reminders == nil {
		return render(c, 200, account.Reminders(page))
	}
	if c.Param("event_id") == "" {
		page.List, err = a.reminders.List(c.Request().Context(), p.Account.ID, c.QueryParams())
	} else {
		values := c.QueryParams()
		if len(values) != 0 && (len(values) != 1 || len(values["saved"]) != 1 || (values.Get("saved") != "reminder" && values.Get("saved") != "stopped")) {
			return a.accountPageError(c, &accounts.ValidationError{Field: "query", Message: "unsupported reminder query"})
		}
		id, idErr := savedEventID(c)
		if idErr != nil {
			return a.accountPageError(c, idErr)
		}
		err = a.loadReminderDetail(c, &page, id)
		switch values.Get("saved") {
		case "reminder":
			page.Notice = "Reminder timing saved. Email permission and global pause are unchanged."
		case "stopped":
			page.Notice = "This reminder is stopped. Your saved event and other reminders are unchanged. A message already being sent cannot be recalled."
		}
	}
	if err != nil {
		return a.accountPageError(c, err)
	}
	return render(c, 200, account.Reminders(page))
}

func (a *api) loadReminderDetail(c echo.Context, page *account.ReminderPage, id string) error {
	if a.saved == nil {
		return reminders.ErrUnavailable
	}
	item, err := a.saved.Get(c.Request().Context(), page.Account.ID, id)
	if err != nil {
		return err
	}
	page.Event = &item.Event
	configured, err := a.reminders.Get(c.Request().Context(), page.Account.ID, id)
	if err == nil {
		page.Own = &configured
		page.Lead = configured.LeadMinutes
	} else if !errors.Is(err, reminders.ErrNotFound) {
		return err
	}
	return nil
}

// HTML timing choices normalize to the existing service/JSON lead_minutes.
// Legacy minute-only submissions remain valid; every field is still single-valued.
func reminderFormLead(c echo.Context) (int, error) {
	choice := c.FormValue("timing")
	if choice != "" && choice != "custom" {
		switch choice {
		case "30", "60", "1440":
			lead, _ := strconv.Atoi(choice)
			return lead, nil
		default:
			return 0, &accounts.ValidationError{Field: "timing", Message: "choose 30 minutes, 1 hour, 1 day, or Custom"}
		}
	}
	if _, present := c.Request().PostForm["timing"]; present && choice == "" {
		return 0, &accounts.ValidationError{Field: "timing", Message: "choose a lead time"}
	}
	lead, err := strconv.Atoi(c.FormValue("lead_minutes"))
	if err != nil {
		return 0, &accounts.ValidationError{Field: "lead_minutes", Message: "choose 1–10080 whole minutes (up to seven days)"}
	}
	return lead, reminders.Lead(lead)
}

func (a *api) reminderWritePage(c echo.Context) error {
	privateAccountResponse(c)
	formErr := parseAccountForm(c, "action", "timing", "lead_minutes")
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
	// Authenticate and verify CSRF before rereading any owner-only detail for an
	// invalid form. Malformed/oversized bodies remain generic failures.
	if formErr != nil {
		var validation *accounts.ValidationError
		if !errors.As(formErr, &validation) || validation.Field == "body" || validation.Field == "query" {
			return a.accountPageError(c, formErr)
		}
	}
	if formErr == nil && c.FormValue("action") == "stop" {
		// Preserve idempotent stop, including an event already removed from Saved.
		if err = a.reminders.Remove(c.Request().Context(), p.Account.ID, id); err != nil {
			return a.accountPageError(c, err)
		}
		return c.Redirect(303, "/me/reminders/"+url.PathEscape(id)+"?saved=stopped")
	}
	page := account.ReminderPage{Account: p.Account, CSRF: p.CSRF, Enabled: true, Lead: 30, Now: time.Now()}
	if err = a.loadReminderDetail(c, &page, id); err != nil {
		return a.accountPageError(c, err)
	}
	page.Submitted = true
	page.Choice, page.Custom = c.FormValue("timing"), c.FormValue("lead_minutes")
	if _, present := c.Request().PostForm["timing"]; !present {
		page.Choice = "custom"
	}
	err = formErr
	if err == nil {
		switch c.FormValue("action") {
		case "set":
			lead, parseErr := reminderFormLead(c)
			if parseErr != nil {
				err = parseErr
				break
			}
			_, err = a.reminders.Set(c.Request().Context(), p.Account.ID, id, lead)
		default:
			err = &accounts.ValidationError{Field: "action", Message: "choose set or stop"}
		}
	}
	if err != nil {
		status, _, message, _ := accountError(err)
		var validation *accounts.ValidationError
		if errors.As(err, &validation) {
			message = validation.Message
		}
		page.Error = "Changes weren't saved. " + message
		return render(c, status, account.Reminders(page))
	}
	return c.Redirect(303, "/me/reminders/"+url.PathEscape(id)+"?saved=reminder")
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
