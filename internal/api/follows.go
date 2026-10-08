package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/follows"
	"github.com/sonastea/ticketopia/views/home"
)

func (a *api) followRoutes(e *echo.Echo) {
	e.GET("/follows", a.followsPage)
	e.POST("/follows/:kind/:target_id", a.followPage)
	e.GET("/api/v1/artists", a.catalogHandler("artist"))
	e.GET("/api/v1/venues", a.catalogHandler("venue"))
	e.GET("/api/v1/me/follows", a.followsHandler)
	e.GET("/api/v1/me/follows/:kind/:target_id", a.getFollowHandler)
	e.PUT("/api/v1/me/follows/:kind/:target_id", a.followHandler)
	e.DELETE("/api/v1/me/follows/:kind/:target_id", a.removeFollowHandler)
}
func (a *api) followPrincipal(c echo.Context, write bool) (accounts.Principal, error) {
	p, err := a.principal(c, false)
	if err != nil {
		return p, err
	}
	if a.follows == nil {
		return p, follows.ErrUnavailable
	}
	if write {
		if len(c.QueryParams()) != 0 {
			return p, &accounts.ValidationError{Field: "query", Message: "this route does not accept query parameters"}
		}
		if err := a.checkWrite(c, p); err != nil {
			return p, err
		}
		data, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, 1))
		if err != nil || len(data) != 0 {
			return p, &accounts.ValidationError{Field: "body", Message: "send an empty request body"}
		}
	}
	return p, nil
}
func followTarget(c echo.Context) (string, string, error) {
	id, err := url.PathUnescape(c.Param("target_id"))
	if err != nil {
		return "", "", &accounts.ValidationError{Field: "target_id", Message: "invalid URL encoding"}
	}
	kind := c.Param("kind")
	return kind, id, follows.Validate(kind, id)
}
func catalogError(err error) error {
	var invalid *discovery.ValidationError
	if errors.As(err, &invalid) {
		return &accounts.ValidationError{Field: invalid.Field, Message: invalid.Message}
	}
	return follows.ErrUnavailable
}
func (a *api) catalogHandler(kind string) echo.HandlerFunc {
	return func(c echo.Context) error {
		if _, err := a.followPrincipal(c, false); err != nil {
			return accountProblem(c, err)
		}
		list, err := a.events.Catalog(c.Request().Context(), kind, c.QueryParams())
		if err != nil {
			setRetryAfter(c, err)
			return accountProblem(c, catalogError(err))
		}
		return c.JSON(200, list)
	}
}
func (a *api) followsHandler(c echo.Context) error {
	p, err := a.followPrincipal(c, false)
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.follows.List(c.Request().Context(), p.Account.ID, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) getFollowHandler(c echo.Context) error {
	p, err := a.followPrincipal(c, false)
	if err != nil {
		return accountProblem(c, err)
	}
	if len(c.QueryParams()) != 0 {
		return accountProblem(c, &accounts.ValidationError{Field: "query", Message: "this route does not accept query parameters"})
	}
	kind, id, err := followTarget(c)
	if err != nil {
		return accountProblem(c, err)
	}
	item, err := a.follows.Get(c.Request().Context(), p.Account.ID, kind, id)
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, item)
}
func (a *api) followHandler(c echo.Context) error {
	p, err := a.followPrincipal(c, true)
	if err != nil {
		return accountProblem(c, err)
	}
	kind, id, err := followTarget(c)
	if err != nil {
		return accountProblem(c, err)
	}
	item, created, err := a.follows.Follow(c.Request().Context(), p.Account.ID, kind, id)
	if err != nil {
		return accountProblem(c, err)
	}
	status := 200
	if created {
		status = 201
		c.Response().Header().Set("Location", "/api/v1/me/follows/"+kind+"/"+url.PathEscape(id))
	}
	return c.JSON(status, item)
}
func (a *api) removeFollowHandler(c echo.Context) error {
	p, err := a.followPrincipal(c, true)
	if err != nil {
		return accountProblem(c, err)
	}
	kind, id, err := followTarget(c)
	if err == nil {
		err = a.follows.Remove(c.Request().Context(), p.Account.ID, kind, id)
	}
	if err != nil {
		return accountProblem(c, err)
	}
	return c.NoContent(204)
}

// Search and collection pagination use separate cursors. A GET never follows.
func followPageQueries(values url.Values) (url.Values, url.Values, error) {
	search, collection := url.Values{}, url.Values{}
	for key, entries := range values {
		if len(entries) != 1 {
			return nil, nil, &accounts.ValidationError{Field: key, Message: "use one value"}
		}
		switch key {
		case "kind", "keyword", "city", "country", "search_cursor":
			if key == "search_cursor" {
				search["cursor"] = entries
			} else {
				search[key] = entries
			}
		case "filter", "limit", "cursor":
			if key == "filter" {
				collection["kind"] = entries
			} else {
				collection[key] = entries
			}
		default:
			return nil, nil, &accounts.ValidationError{Field: key, Message: "use a supported follows parameter"}
		}
	}
	kind := search.Get("kind")
	if kind == "" {
		kind = "artist"
	}
	search.Del("kind")
	if kind != "artist" && kind != "venue" {
		return nil, nil, &accounts.ValidationError{Field: "kind", Message: "choose artist or venue"}
	}
	// Switching back to artists clears the previously displayed venue filters.
	if kind == "artist" {
		search.Del("city")
		search.Del("country")
	}
	if search.Get("keyword") != "" {
		if _, err := discovery.ParseCatalogQuery(kind, search); err != nil {
			return nil, nil, catalogError(err)
		}
	} else if len(search) > 0 {
		return nil, nil, &accounts.ValidationError{Field: "keyword", Message: "enter a name to search"}
	}
	search.Set("kind", kind)
	return search, collection, nil
}
func safeFollowReturn(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || strings.ContainsAny(raw, "\\\r\n\x00") || u.IsAbs() || u.Host != "" || u.Path != "/follows" || u.RawPath != "" || u.Fragment != "" {
		return "/follows"
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "/follows"
	}
	if _, _, err := followPageQueries(values); err != nil {
		return "/follows"
	}
	return u.String()
}
func (a *api) followsPage(c echo.Context) error {
	privateAccountResponse(c)
	page := home.FollowsPage{Enabled: a.enabled(), ReturnURL: c.Request().URL.RequestURI(), Kind: "artist"}
	if !a.enabled() {
		return render(c, 200, home.Follows(page))
	}
	p, err := a.principal(c, true)
	if errors.Is(err, accounts.ErrUnauthenticated) {
		return c.Redirect(303, "/auth/sign-in?return_to="+url.QueryEscape(safeFollowReturn(page.ReturnURL)))
	}
	if err != nil {
		return a.accountPageError(c, err)
	}
	page.CSRF = p.CSRF
	search, collection, err := followPageQueries(c.QueryParams())
	if err != nil {
		status, _, message, _ := accountError(err)
		page.Error = message
		return render(c, status, home.Follows(page))
	}
	page.Kind, page.Keyword, page.City, page.Country = search.Get("kind"), search.Get("keyword"), search.Get("city"), search.Get("country")
	page.Filter = collection.Get("kind")
	status := 200
	if a.follows == nil {
		err = follows.ErrUnavailable
	} else {
		page.List, err = a.follows.List(c.Request().Context(), p.Account.ID, collection)
	}
	if err != nil {
		status, _, page.Error, _ = accountError(err)
	}
	if page.Keyword != "" && a.follows != nil {
		search.Del("kind")
		page.Searched = true
		page.Search, err = a.events.Catalog(c.Request().Context(), page.Kind, search)
		if err == nil {
			ids := []string{}
			for _, target := range page.Search.Items {
				ids = append(ids, target.ID)
			}
			page.States, err = a.follows.States(c.Request().Context(), p.Account.ID, page.Kind, ids)
		}
		if err != nil {
			status, _, page.SearchError, _ = accountError(catalogError(err))
			if status == 503 {
				page.SearchError = "Artist and venue search is temporarily unavailable. Try again shortly; you can still manage your follows below."
			}
		}
	}
	return render(c, status, home.Follows(page))
}
func (a *api) followPage(c echo.Context) error {
	privateAccountResponse(c)
	failure := func(err error) error {
		status, _, message, _ := accountError(err)
		return render(c, status, home.FollowFailure(message, safeFollowReturn(c.FormValue("return_to"))))
	}
	if err := parseAccountForm(c, "action", "return_to"); err != nil {
		return failure(err)
	}
	p, err := a.principal(c, true)
	if err != nil {
		return failure(err)
	}
	if err := a.checkWrite(c, p); err != nil {
		return failure(err)
	}
	if a.follows == nil {
		return failure(follows.ErrUnavailable)
	}
	kind, id, err := followTarget(c)
	if err != nil {
		return failure(err)
	}
	switch c.FormValue("action") {
	case "follow":
		_, _, err = a.follows.Follow(c.Request().Context(), p.Account.ID, kind, id)
	case "unfollow":
		err = a.follows.Remove(c.Request().Context(), p.Account.ID, kind, id)
	default:
		err = &accounts.ValidationError{Field: "action", Message: "choose follow or unfollow"}
	}
	if err != nil {
		return failure(err)
	}
	return c.Redirect(303, safeFollowReturn(c.FormValue("return_to")))
}
