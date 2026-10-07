package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
	"github.com/sonastea/ticketopia/views/home"
)

func (a *api) savedRoutes(e *echo.Echo) {
	e.GET("/saved", a.savedPage)
	e.POST("/saved/:event_id", a.savePage)
	e.GET("/api/v1/me/saved-events", a.savedHandler)
	e.GET("/api/v1/me/saved-events/states", a.savedStatesHandler)
	e.GET("/api/v1/me/saved-events/:event_id", a.getSavedHandler)
	e.PUT("/api/v1/me/saved-events/:event_id", a.saveHandler)
	e.DELETE("/api/v1/me/saved-events/:event_id", a.removeSavedHandler)
}
func (a *api) savesEnabled() bool { return a.enabled() && a.saved != nil }

// All private state is loaded in one bounded query, never per event/provider.
func (a *api) saveView(c echo.Context, ids []string) home.SaveView {
	view := home.SaveView{Enabled: a.savesEnabled()}
	if !view.Enabled {
		return view
	}
	p, err := a.principal(c, true)
	if errors.Is(err, accounts.ErrUnauthenticated) {
		return view
	}
	if err != nil {
		view.Error = "Saved state is temporarily unavailable. Refresh to try again."
		return view
	}
	view.SignedIn, view.CSRF = true, p.CSRF
	view.States, err = a.saved.States(c.Request().Context(), p.Account.ID, ids)
	if err != nil {
		view.Error = "Saved state is temporarily unavailable. Refresh to try again."
	}
	return view
}
func (a *api) eventDetail(ctx context.Context, id string) (models.EventDetail, error) {
	if a.saved != nil {
		if saved.ValidateID(id) != nil {
			return models.EventDetail{}, &discovery.ValidationError{Field: "event_id", Message: "use a ticketmaster: prefixed ID"}
		}
		return a.saved.Detail(ctx, id)
	}
	return a.events.Event(ctx, id)
}
func (a *api) savedPrincipal(c echo.Context, write bool) (accounts.Principal, error) {
	p, err := a.principal(c, false)
	if err != nil {
		return p, err
	}
	if a.saved == nil {
		return p, saved.ErrUnavailable
	}
	if write {
		if len(c.QueryParams()) != 0 {
			return p, &accounts.ValidationError{Field: "query", Message: "this route does not accept query parameters"}
		}
		if err := a.checkWrite(c, p); err != nil {
			return p, err
		}
		// No client metadata/owner/visibility is accepted for save or remove.
		data, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, 1))
		if err != nil || len(data) != 0 {
			return p, &accounts.ValidationError{Field: "body", Message: "send an empty request body"}
		}
	}
	return p, nil
}
func savedEventID(c echo.Context) (string, error) {
	id, err := url.PathUnescape(c.Param("event_id"))
	if err != nil {
		return "", &accounts.ValidationError{Field: "event_id", Message: "invalid URL encoding"}
	}
	return id, saved.ValidateID(id)
}
func (a *api) savedHandler(c echo.Context) error {
	p, err := a.savedPrincipal(c, false)
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.saved.List(c.Request().Context(), p.Account.ID, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(http.StatusOK, list)
}
func (a *api) getSavedHandler(c echo.Context) error {
	p, err := a.requireAPI(c, false, false)
	if err != nil {
		return accountProblem(c, err)
	}
	if a.saved == nil {
		return accountProblem(c, saved.ErrUnavailable)
	}
	id, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	item, err := a.saved.Get(c.Request().Context(), p.Account.ID, id)
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, item)
}
func (a *api) savedStatesHandler(c echo.Context) error {
	p, err := a.savedPrincipal(c, false)
	if err != nil {
		return accountProblem(c, err)
	}
	for key := range c.QueryParams() {
		if key != "event_id" {
			return accountProblem(c, &accounts.ValidationError{Field: key, Message: "use only event_id"})
		}
	}
	states, err := a.saved.States(c.Request().Context(), p.Account.ID, c.QueryParams()["event_id"])
	if err != nil {
		return accountProblem(c, err)
	}
	csrf := ""
	if p.Credential.Kind == "session" {
		csrf = p.CSRF
	}
	return c.JSON(200, struct {
		States map[string]bool `json:"states"`
		CSRF   string          `json:"csrf_token,omitempty"`
	}{states, csrf})
}
func (a *api) saveHandler(c echo.Context) error {
	p, err := a.savedPrincipal(c, true)
	if err != nil {
		return accountProblem(c, err)
	}
	id, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	item, created, err := a.saved.Save(c.Request().Context(), p.Account.ID, id)
	if err != nil {
		return accountProblem(c, err)
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
		c.Response().Header().Set("Location", "/api/v1/me/saved-events/"+url.PathEscape(id))
	}
	return c.JSON(status, item)
}
func (a *api) removeSavedHandler(c echo.Context) error {
	p, err := a.savedPrincipal(c, true)
	if err != nil {
		return accountProblem(c, err)
	}
	id, err := savedEventID(c)
	if err == nil {
		err = a.saved.Remove(c.Request().Context(), p.Account.ID, id)
	}
	if err != nil {
		return accountProblem(c, err)
	}
	return c.NoContent(http.StatusNoContent)
}
func (a *api) savedPage(c echo.Context) error {
	privateAccountResponse(c)
	if !a.enabled() {
		return render(c, 200, home.Destination("saved", "Saved events"))
	}
	p, err := a.principal(c, true)
	if errors.Is(err, accounts.ErrUnauthenticated) {
		return c.Redirect(303, "/auth/sign-in?return_to=%2Fsaved")
	}
	if err != nil {
		return a.accountPageError(c, err)
	}
	page := home.SavedPage{ReturnURL: c.Request().URL.RequestURI(), Saves: home.SaveView{Enabled: a.savesEnabled(), SignedIn: true, CSRF: p.CSRF, States: map[string]bool{}}}
	status := 200
	if a.saved == nil {
		err = saved.ErrUnavailable
	} else {
		page.List, err = a.saved.List(c.Request().Context(), p.Account.ID, c.QueryParams())
	}
	if err != nil {
		status, _, page.Error, _ = accountError(err)
	}
	for _, item := range page.List.Items {
		page.Saves.States[item.Event.ID] = true
	}
	ids := []string{}
	for _, item := range page.List.Items {
		ids = append(ids, item.Event.ID)
	}
	page.Interests = a.interestView(c, ids)
	return render(c, status, home.Saved(page))
}
func (a *api) savePage(c echo.Context) error {
	privateAccountResponse(c)
	failure := func(err error) error {
		if strings.Contains(c.Request().Header.Get("Accept"), "application/json") {
			return accountProblem(c, err)
		}
		return a.accountPageError(c, err)
	}
	if err := parseAccountForm(c, "action", "return_to"); err != nil {
		return failure(err)
	}
	p, err := a.principal(c, true)
	if err != nil {
		return failure(err)
	}
	if err = a.checkWrite(c, p); err != nil {
		return failure(err)
	}
	if a.saved == nil {
		return failure(saved.ErrUnavailable)
	}
	id, err := savedEventID(c)
	if err != nil {
		return failure(err)
	}
	switch c.FormValue("action") {
	case "save":
		_, _, err = a.saved.Save(c.Request().Context(), p.Account.ID, id)
	case "remove":
		err = a.saved.Remove(c.Request().Context(), p.Account.ID, id)
	default:
		err = &accounts.ValidationError{Field: "action", Message: "choose save or remove"}
	}
	if err != nil {
		return failure(err)
	}
	if strings.Contains(c.Request().Header.Get("Accept"), "application/json") {
		return c.JSON(200, struct {
			ID    string `json:"event_id"`
			Saved bool   `json:"saved"`
		}{id, c.FormValue("action") == "save"})
	}
	return c.Redirect(303, safeInterestReturn(c.FormValue("return_to")))
}
func safeSaveReturn(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Path == "/saved" && !u.IsAbs() && u.Host == "" && u.Fragment == "" && u.RawPath == "" && !strings.ContainsAny(raw, "\\\r\n\x00") && len(raw) <= 4096 {
		values, err := url.ParseQuery(u.RawQuery)
		if err == nil {
			for k, entries := range values {
				if (k != "limit" && k != "cursor") || len(entries) != 1 {
					return "/saved"
				}
			}
			return u.String()
		}
	}
	return safeAuthReturn(raw)
}
