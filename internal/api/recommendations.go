package api

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/views/home"
)

func (a *api) recommendationRoutes(e *echo.Echo) {
	e.GET("/community", a.communityPage)
	e.POST("/recommendations/:event_id", a.recommendationPage)
	e.GET("/events/:event_id/recommendations", a.eventRecommendationsPage)
	e.GET("/api/v1/recommendations", a.communityRecommendationsHandler)
	e.GET("/api/v1/me/event-recommendations", a.ownRecommendationsHandler)
	e.GET("/api/v1/me/event-recommendations/:event_id", a.getRecommendationHandler)
	e.PUT("/api/v1/me/event-recommendations/:event_id", a.setRecommendationHandler)
	e.DELETE("/api/v1/me/event-recommendations/:event_id", a.withdrawRecommendationHandler)
	e.GET("/api/v1/events/:event_id/recommendations", a.eventRecommendationsHandler)
	e.GET("/api/v1/users/:user_id/event-recommendations", a.profileRecommendationsHandler)
}
func (a *api) recommendationsEnabled() bool { return a.enabled() && a.recommendations != nil }
func (a *api) publicRecommendationCheck(c echo.Context) error {
	privateAccountResponse(c)
	if !a.recommendationsEnabled() {
		return recommendations.ErrUnavailable
	}
	return nil
}
func (a *api) recommendationPrincipal(c echo.Context, write bool) (accounts.Principal, error) {
	p, err := a.principal(c, false)
	if err != nil {
		return p, err
	}
	if a.recommendations == nil {
		return p, recommendations.ErrUnavailable
	}
	if write {
		if len(c.QueryParams()) != 0 {
			return p, &accounts.ValidationError{Field: "query", Message: "this route does not accept query parameters"}
		}
		err = a.checkWrite(c, p)
	}
	return p, err
}
func (a *api) getRecommendationHandler(c echo.Context) error {
	p, err := a.recommendationPrincipal(c, false)
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
	item, err := a.recommendations.Get(c.Request().Context(), p.Account.ID, id)
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, item)
}
func (a *api) setRecommendationHandler(c echo.Context) error {
	p, err := a.recommendationPrincipal(c, true)
	if err != nil {
		return accountProblem(c, err)
	}
	id, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if err := decodeAccountJSON(c, &body); err != nil {
		return accountProblem(c, err)
	}
	item, created, err := a.recommendations.Set(c.Request().Context(), p.Account.ID, id, body.Reason)
	if err != nil {
		return accountProblem(c, err)
	}
	status := 200
	if created {
		status = 201
		c.Response().Header().Set("Location", "/api/v1/me/event-recommendations/"+url.PathEscape(id))
	}
	return c.JSON(status, item)
}
func (a *api) withdrawRecommendationHandler(c echo.Context) error {
	p, err := a.recommendationPrincipal(c, true)
	if err != nil {
		return accountProblem(c, err)
	}
	data, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, 1))
	if err != nil || len(data) != 0 {
		return accountProblem(c, &accounts.ValidationError{Field: "body", Message: "send an empty request body"})
	}
	id, err := savedEventID(c)
	if err == nil {
		err = a.recommendations.Remove(c.Request().Context(), p.Account.ID, id)
	}
	if err != nil {
		return accountProblem(c, err)
	}
	return c.NoContent(204)
}
func (a *api) ownRecommendationsHandler(c echo.Context) error {
	p, err := a.recommendationPrincipal(c, false)
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.recommendations.Own(c.Request().Context(), p.Account.ID, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) communityRecommendationsHandler(c echo.Context) error {
	if err := a.publicRecommendationCheck(c); err != nil {
		return accountProblem(c, err)
	}
	list, err := a.recommendations.Community(c.Request().Context(), c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) eventRecommendationsHandler(c echo.Context) error {
	if err := a.publicRecommendationCheck(c); err != nil {
		return accountProblem(c, err)
	}
	id, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.recommendations.Event(c.Request().Context(), id, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) profileRecommendationsHandler(c echo.Context) error {
	if err := a.publicRecommendationCheck(c); err != nil {
		return accountProblem(c, err)
	}
	profile, err := a.accounts.Public(c.Request().Context(), c.Param("user_id"))
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.recommendations.Public(c.Request().Context(), profile.ID, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}

// Selected-event loading only. No per-card queries or private interest projection
// are needed to display explicitly public recommendations.
func (a *api) recommendationView(c echo.Context, id string) home.RecommendationView {
	v := home.RecommendationView{Enabled: a.recommendationsEnabled(), Open: c.QueryParam("recommend") == "true"}
	if !v.Enabled {
		return v
	}
	p, err := a.principal(c, true)
	if err == nil {
		v.SignedIn, v.CSRF, v.ViewerID = true, p.CSRF, p.Account.ID
		item, err := a.recommendations.Get(c.Request().Context(), p.Account.ID, id)
		if err == nil {
			v.Own = &item
		} else if !errors.Is(err, recommendations.ErrNotFound) {
			v.Error = "Your recommendation couldn't load. Refresh to try again."
		}
	} else if !errors.Is(err, accounts.ErrUnauthenticated) {
		v.Error = "Your recommendation couldn't load. Refresh to try again."
	}
	v.List, err = a.recommendations.Event(c.Request().Context(), id, nil)
	if err != nil {
		v.ListError = "Public recommendations couldn't load. Refresh to try again."
	}
	return v
}
func (a *api) communityPage(c echo.Context) error {
	privateAccountResponse(c)
	p := home.CommunityPage{Enabled: a.recommendationsEnabled(), ReturnURL: c.Request().URL.RequestURI()}
	status := 200
	var err error
	p.Filters, err = recommendations.ParseQuery(c.QueryParams(), "community", true)
	if err == nil && p.Enabled {
		p.List, err = a.recommendations.Community(c.Request().Context(), c.QueryParams())
	}
	if err != nil {
		status, _, p.Error, _ = accountError(err)
	}
	// Catalog failure never blocks durable community reads or clears the filter.
	if p.Enabled && a.events != nil {
		catalog, catalogErr := a.events.Categories(c.Request().Context())
		p.Categories = catalog.Items
		p.CategoriesUnavailable = catalogErr != nil
	}
	return render(c, status, home.Community(p))
}
func (a *api) recommendationPage(c echo.Context) error {
	privateAccountResponse(c)
	jsonResponse := strings.Contains(c.Request().Header.Get("Accept"), "application/json")
	if err := parseAccountForm(c, "action", "reason", "return_to"); err != nil {
		if jsonResponse {
			return accountProblem(c, err)
		}
		return a.accountPageError(c, err)
	}
	id, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	target := safeRecommendationReturn(c.FormValue("return_to"))
	p, principalErr := a.principal(c, true)
	failure := func(err error) error {
		if jsonResponse {
			return accountProblem(c, err)
		}
		status, _, message, _ := accountError(err)
		return render(c, status, home.RecommendationFailure(id, c.FormValue("reason"), p.CSRF, target, message, c.FormValue("action") == "remove", a.enabled() && errors.Is(err, accounts.ErrUnauthenticated)))
	}
	if principalErr != nil {
		return failure(principalErr)
	}
	if err := a.checkWrite(c, p); err != nil {
		return failure(err)
	}
	if a.recommendations == nil {
		return failure(recommendations.ErrUnavailable)
	}
	notice := "Your recommendation was published."
	switch c.FormValue("action") {
	case "set":
		_, created, setErr := a.recommendations.Set(c.Request().Context(), p.Account.ID, id, c.FormValue("reason"))
		err = setErr
		if !created {
			notice = "Your recommendation was updated."
		}
	case "remove":
		err = a.recommendations.Remove(c.Request().Context(), p.Account.ID, id)
		notice = "Your recommendation was withdrawn."
	default:
		err = &accounts.ValidationError{Field: "action", Message: "choose set or remove"}
	}
	if err != nil {
		return failure(err)
	}
	if jsonResponse {
		v := a.recommendationView(c, id)
		if v.Error != "" {
			return failure(recommendations.ErrUnavailable)
		}
		var html bytes.Buffer
		if err := home.RecommendationPanel(id, v, target).Render(c.Request().Context(), &html); err != nil {
			return failure(recommendations.ErrUnavailable)
		}
		return c.JSON(200, struct {
			ID     string `json:"event_id"`
			HTML   string `json:"html"`
			Notice string `json:"notice"`
		}{id, html.String(), notice})
	}
	// The explicit-open flag is navigation intent, not persisted publication state.
	if u, err := url.Parse(target); err == nil && u.Query().Has("recommend") {
		values := u.Query()
		values.Del("recommend")
		u.RawQuery = values.Encode()
		target = u.String()
	}
	return c.Redirect(303, target)
}
func safeCommunityReturn(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Path != "/community" || len(raw) > 4096 || u.IsAbs() || u.Host != "" || u.RawPath != "" || u.Fragment != "" || strings.ContainsAny(raw, "\\\r\n\x00") {
		return "/community"
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "/community"
	}
	if _, err := recommendations.ParseQuery(values, "community", true); err != nil {
		return "/community"
	}
	return u.String()
}
func safeRecommendationReturn(raw string) string {
	if strings.HasPrefix(raw, "/community") {
		return safeCommunityReturn(raw)
	}
	if strings.HasPrefix(raw, "/users/") {
		return safeProfileReturn(raw)
	}
	return safeInterestReturn(raw)
}
func safeProfileReturn(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || u.IsAbs() || u.Host != "" || u.RawPath != "" || u.Fragment != "" || !strings.HasPrefix(u.Path, "/users/") || !accounts.ValidID(strings.TrimPrefix(u.Path, "/users/")) || strings.ContainsAny(raw, "\\\r\n\x00") {
		return "/"
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "/"
	}
	for key, entries := range values {
		if len(entries) != 1 || (key != "limit" && key != "cursor" && key != "recommendation_cursor") {
			return "/"
		}
	}
	return u.String()
}
func (a *api) eventRecommendationsPage(c echo.Context) error {
	if err := a.publicRecommendationCheck(c); err != nil {
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
	list, err := a.recommendations.Event(c.Request().Context(), id, values)
	if err != nil {
		return a.accountPageError(c, err)
	}
	detail, err := a.eventDetail(c.Request().Context(), id)
	if err != nil {
		return a.accountPageError(c, err)
	}
	return render(c, 200, home.EventRecommendations(detail.Item, list, c.Request().URL.RequestURI(), origin))
}
