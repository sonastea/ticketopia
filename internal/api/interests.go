package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/views/home"
)

func (a *api) interestRoutes(e *echo.Echo) {
	e.POST("/interests/:event_id", a.interestPage)
	e.GET("/api/v1/me/event-interests", a.interestsHandler)
	e.GET("/api/v1/me/event-interests/states", a.interestStatesHandler)
	e.GET("/api/v1/me/event-interests/:event_id", a.getInterestHandler)
	e.PUT("/api/v1/me/event-interests/:event_id", a.setInterestHandler)
	e.DELETE("/api/v1/me/event-interests/:event_id", a.removeInterestHandler)
	e.GET("/api/v1/events/:event_id/interest", a.interestCountHandler)
	e.GET("/api/v1/events/:event_id/interested-users", a.participantsHandler)
	e.GET("/api/v1/users/:user_id/event-interests", a.publicInterestsHandler)
	e.GET("/events/:event_id/interested-users", a.participantsPage)
}
func (a *api) interestEnabled() bool { return a.enabled() && a.interests != nil }
func (a *api) interestView(c echo.Context, ids []string) home.InterestView {
	view := home.InterestView{Enabled: a.interestEnabled(), DefaultVisibility: "private"}
	if !view.Enabled {
		return view
	}
	owner := ""
	p, err := a.principal(c, true)
	if err == nil {
		owner = p.Account.ID
		view.SignedIn = true
		view.CSRF = p.CSRF
		view.DefaultVisibility = p.Account.InterestVisibility
	} else if !errors.Is(err, accounts.ErrUnauthenticated) {
		view.Error = "Your interest state couldn't load. Refresh to try again."
	}
	view.States, err = a.interests.States(c.Request().Context(), owner, ids)
	if err != nil {
		view.Error = "Interest counts and choices couldn't load. Refresh to try again."
	}
	return view
}
func (a *api) interestPrincipal(c echo.Context, write bool) (accounts.Principal, error) {
	p, err := a.principal(c, false)
	if err != nil {
		return p, err
	}
	if a.interests == nil {
		return p, interests.ErrUnavailable
	}
	if write {
		if len(c.QueryParams()) != 0 {
			return p, &accounts.ValidationError{Field: "query", Message: "this route does not accept query parameters"}
		}
		err = a.checkWrite(c, p)
	}
	return p, err
}
func (a *api) interestsHandler(c echo.Context) error {
	p, err := a.interestPrincipal(c, false)
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.interests.List(c.Request().Context(), p.Account.ID, false, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) getInterestHandler(c echo.Context) error {
	p, err := a.interestPrincipal(c, false)
	if err != nil {
		return accountProblem(c, err)
	}
	if len(c.QueryParams()) != 0 {
		return accountProblem(c, &accounts.ValidationError{Field: "query", Message: "this route does not accept query parameters"})
	}
	id, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	item, err := a.interests.Get(c.Request().Context(), p.Account.ID, id)
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, item)
}
func (a *api) interestStatesHandler(c echo.Context) error {
	p, err := a.interestPrincipal(c, false)
	if err != nil {
		return accountProblem(c, err)
	}
	for key := range c.QueryParams() {
		if key != "event_id" {
			return accountProblem(c, &accounts.ValidationError{Field: key, Message: "use only event_id"})
		}
	}
	states, err := a.interests.States(c.Request().Context(), p.Account.ID, c.QueryParams()["event_id"])
	if err != nil {
		return accountProblem(c, err)
	}
	csrf := ""
	if p.Credential.Kind == "session" {
		csrf = p.CSRF
	}
	return c.JSON(200, struct {
		States            map[string]interests.State `json:"states"`
		CSRF              string                     `json:"csrf_token,omitempty"`
		DefaultVisibility string                     `json:"default_visibility"`
	}{states, csrf, p.Account.InterestVisibility})
}
func (a *api) setInterestHandler(c echo.Context) error {
	p, err := a.interestPrincipal(c, true)
	if err != nil {
		return accountProblem(c, err)
	}
	id, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	var body struct {
		Visibility *string `json:"visibility"`
	}
	if err := decodeAccountJSON(c, &body); err != nil {
		return accountProblem(c, err)
	}
	item, created, err := a.interests.Set(c.Request().Context(), p.Account.ID, id, body.Visibility, p.Account.InterestVisibility)
	if err != nil {
		return accountProblem(c, err)
	}
	status := 200
	if created {
		status = 201
		c.Response().Header().Set("Location", "/api/v1/me/event-interests/"+url.PathEscape(id))
	}
	return c.JSON(status, item)
}
func (a *api) removeInterestHandler(c echo.Context) error {
	p, err := a.interestPrincipal(c, true)
	if err != nil {
		return accountProblem(c, err)
	}
	data, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, 1))
	if err != nil || len(data) != 0 {
		return accountProblem(c, &accounts.ValidationError{Field: "body", Message: "send an empty request body"})
	}
	id, err := savedEventID(c)
	if err == nil {
		err = a.interests.Remove(c.Request().Context(), p.Account.ID, id)
	}
	if err != nil {
		return accountProblem(c, err)
	}
	return c.NoContent(204)
}
func (a *api) publicInterestCheck(c echo.Context) error {
	privateAccountResponse(c) // Visibility changes must not leave cacheable identities.
	if !a.interestEnabled() {
		return interests.ErrUnavailable
	}
	return nil
}
func (a *api) interestCountHandler(c echo.Context) error {
	if err := a.publicInterestCheck(c); err != nil {
		return accountProblem(c, err)
	}
	if len(c.QueryParams()) != 0 {
		return accountProblem(c, &accounts.ValidationError{Field: "query", Message: "this route does not accept query parameters"})
	}
	id, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	states, err := a.interests.States(c.Request().Context(), "", []string{id})
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, struct {
		Count int `json:"count"`
	}{states[id].Count})
}
func (a *api) participantsHandler(c echo.Context) error {
	if err := a.publicInterestCheck(c); err != nil {
		return accountProblem(c, err)
	}
	id, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.interests.Participants(c.Request().Context(), id, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) publicInterestsHandler(c echo.Context) error {
	if err := a.publicInterestCheck(c); err != nil {
		return accountProblem(c, err)
	}
	profile, err := a.accounts.Public(c.Request().Context(), c.Param("user_id"))
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.interests.List(c.Request().Context(), profile.ID, true, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) interestPage(c echo.Context) error {
	privateAccountResponse(c)
	failure := func(err error) error {
		if strings.Contains(c.Request().Header.Get("Accept"), "application/json") {
			return accountProblem(c, err)
		}
		return a.accountPageError(c, err)
	}
	if err := parseAccountForm(c, "action", "visibility", "return_to"); err != nil {
		return failure(err)
	}
	p, err := a.principal(c, true)
	if err != nil {
		return failure(err)
	}
	if err := a.checkWrite(c, p); err != nil {
		return failure(err)
	}
	if a.interests == nil {
		return failure(interests.ErrUnavailable)
	}
	id, err := savedEventID(c)
	if err != nil {
		return failure(err)
	}
	switch c.FormValue("action") {
	case "set":
		visibility := c.FormValue("visibility")
		_, _, err = a.interests.Set(c.Request().Context(), p.Account.ID, id, &visibility, p.Account.InterestVisibility)
	case "remove":
		err = a.interests.Remove(c.Request().Context(), p.Account.ID, id)
	default:
		err = &accounts.ValidationError{Field: "action", Message: "choose set or remove"}
	}
	if err != nil {
		return failure(err)
	}
	if strings.Contains(c.Request().Header.Get("Accept"), "application/json") {
		states, err := a.interests.States(c.Request().Context(), p.Account.ID, []string{id})
		if err != nil {
			return failure(err)
		}
		return c.JSON(200, struct {
			ID    string          `json:"event_id"`
			State interests.State `json:"state"`
		}{id, states[id]})
	}
	return c.Redirect(303, safeRecommendationReturn(c.FormValue("return_to")))
}
func safeInterestReturn(raw string) string {
	u, err := url.Parse(raw)
	if err == nil && u.Path == "/me/interests" && !u.IsAbs() && u.Host == "" && u.Fragment == "" && u.RawPath == "" && !strings.ContainsAny(raw, "\\\r\n\x00") && len(raw) <= 4096 {
		values, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return "/me/interests"
		}
		for k, entries := range values {
			if (k != "limit" && k != "cursor" && k != "saved") || len(entries) != 1 {
				return "/me/interests"
			}
		}
		return u.String()
	}
	return safeSaveReturn(raw)
}
func (a *api) participantsPage(c echo.Context) error {
	if err := a.publicInterestCheck(c); err != nil {
		return a.accountPageError(c, err)
	}
	id, err := savedEventID(c)
	if err != nil {
		return a.accountPageError(c, err)
	}
	values := c.QueryParams()
	if len(values["return_to"]) > 1 {
		return a.accountPageError(c, &accounts.ValidationError{Field: "return_to", Message: "use one return destination"})
	}
	origin := safeReturnURL(values.Get("return_to"))
	values.Del("return_to")
	detail, err := a.eventDetail(c.Request().Context(), id)
	if err != nil {
		return a.accountPageError(c, err)
	}
	list, err := a.interests.Participants(c.Request().Context(), id, values)
	if err != nil {
		return a.accountPageError(c, err)
	}
	if origin != "/" {
		values.Set("return_to", origin)
	}
	paginationURL := "/events/" + url.PathEscape(id) + "/interested-users"
	if len(values) != 0 {
		paginationURL += "?" + values.Encode()
	}
	return render(c, 200, home.InterestedUsers(detail.Item, list, paginationURL, origin))
}
