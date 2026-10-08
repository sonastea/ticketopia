package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/saved"
	"github.com/sonastea/ticketopia/views/home"
)

func (a *api) discussionRoutes(e *echo.Echo) {
	e.GET("/api/v1/events/:event_id/discussions", a.discussionListHandler)
	e.POST("/api/v1/events/:event_id/discussions", a.createDiscussionHandler)
	e.GET("/api/v1/events/:event_id/discussions/:discussion_id", a.discussionThreadHandler)
	e.GET("/api/v1/events/:event_id/discussions/:discussion_id/replies", a.discussionListHandler)
	e.POST("/api/v1/events/:event_id/discussions/:discussion_id/replies", a.createDiscussionHandler)
	e.PATCH("/api/v1/posts/:post_id", a.editPostHandler)
	e.DELETE("/api/v1/posts/:post_id", a.removePostHandler)
	e.PUT("/api/v1/posts/:post_id/reactions/helpful", a.helpfulHandler)
	e.DELETE("/api/v1/posts/:post_id/reactions/helpful", a.helpfulHandler)
	e.GET("/api/v1/community/discussions", a.discussionListHandler)
	e.GET("/events/:event_id/discussions", a.discussionCollectionPage)
	e.GET("/events/:event_id/discussions/:discussion_id", a.discussionCollectionPage)
	e.GET("/community/discussions", a.discussionCommunityPage)
	e.POST("/discussion-actions", a.discussionActionPage)
}
func (a *api) discussionsEnabled() bool { return a.enabled() && a.discussions != nil }
func (a *api) discussionViewer(c echo.Context, browser bool) (accounts.Principal, error) {
	privateAccountResponse(c)
	if !a.discussionsEnabled() {
		return accounts.Principal{}, discussions.ErrUnavailable
	}
	p, err := a.principal(c, browser)
	if errors.Is(err, accounts.ErrUnauthenticated) && c.Request().Header.Get("Authorization") == "" {
		return accounts.Principal{}, nil
	}
	return p, err
}
func (a *api) discussionWriter(c echo.Context) (accounts.Principal, error) {
	p, err := a.requireAPI(c, true, false)
	if err == nil && !a.discussionsEnabled() {
		err = discussions.ErrUnavailable
	}
	return p, err
}
func (a *api) discussionListHandler(c echo.Context) error {
	p, err := a.discussionViewer(c, false)
	if err != nil {
		return accountProblem(c, err)
	}
	event := ""
	if c.Param("event_id") != "" {
		event, err = savedEventID(c)
		if err != nil {
			return accountProblem(c, err)
		}
	}
	list, err := a.discussions.List(c.Request().Context(), p.Account.ID, event, c.Param("discussion_id"), c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) discussionThreadHandler(c echo.Context) error {
	p, err := a.discussionViewer(c, false)
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
	post, err := a.discussions.Thread(c.Request().Context(), p.Account.ID, id, c.Param("discussion_id"))
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, post)
}
func (a *api) createDiscussionHandler(c echo.Context) error {
	p, err := a.discussionWriter(c)
	if err != nil {
		return accountProblem(c, err)
	}
	id, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	var body struct {
		Body     string `json:"body"`
		ParentID string `json:"parent_id"`
	}
	if err := decodeAccountJSON(c, &body); err != nil {
		return accountProblem(c, err)
	}
	if len(c.Request().Header.Values("Idempotency-Key")) != 1 {
		return accountProblem(c, &accounts.ValidationError{Field: "idempotency_key", Message: "send one Idempotency-Key header"})
	}
	if err := a.accounts.Allow(c.Request().Context(), "discussion-create", p.Account.ID); err != nil {
		return accountProblem(c, err)
	}
	post, created, err := a.discussions.Create(c.Request().Context(), p.Account.ID, id, c.Param("discussion_id"), body.ParentID, body.Body, c.Request().Header.Get("Idempotency-Key"))
	if err != nil {
		return accountProblem(c, err)
	}
	status := 200
	if created {
		status = 201
	}
	c.Response().Header().Set("Location", "/api/v1/events/"+url.PathEscape(id)+"/discussions/"+post.ThreadID)
	return c.JSON(status, post)
}
func (a *api) editPostHandler(c echo.Context) error {
	p, err := a.discussionWriter(c)
	if err != nil {
		return accountProblem(c, err)
	}
	var body struct {
		Body string `json:"body"`
	}
	if err = decodeAccountJSON(c, &body); err != nil {
		return accountProblem(c, err)
	}
	post, err := a.discussions.Edit(c.Request().Context(), p.Account.ID, c.Param("post_id"), body.Body)
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, post)
}
func emptyDiscussionBody(c echo.Context) error {
	data, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, 1))
	if err != nil || len(data) != 0 {
		return &accounts.ValidationError{Field: "body", Message: "send an empty request body"}
	}
	return nil
}
func (a *api) removePostHandler(c echo.Context) error {
	p, err := a.discussionWriter(c)
	if err != nil {
		return accountProblem(c, err)
	}
	if err = emptyDiscussionBody(c); err != nil {
		return accountProblem(c, err)
	}
	if err = a.discussions.Remove(c.Request().Context(), p.Account.ID, c.Param("post_id")); err != nil {
		return accountProblem(c, err)
	}
	return c.NoContent(204)
}
func (a *api) helpfulHandler(c echo.Context) error {
	p, err := a.discussionWriter(c)
	if err != nil {
		return accountProblem(c, err)
	}
	if err = emptyDiscussionBody(c); err != nil {
		return accountProblem(c, err)
	}
	post, err := a.discussions.React(c.Request().Context(), p.Account.ID, c.Param("post_id"), c.Request().Method == "PUT")
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, post)
}
func (a *api) discussionView(c echo.Context, id string) home.DiscussionView {
	v := home.DiscussionView{Enabled: a.discussionsEnabled(), Key: accounts.ID()}
	if !v.Enabled {
		return v
	}
	p, err := a.discussionViewer(c, true)
	if err != nil {
		v.Error = "Discussions couldn't load. Refresh to try again."
		return v
	}
	v.ViewerID, v.CSRF = p.Account.ID, p.CSRF
	v.List, err = a.discussions.List(c.Request().Context(), v.ViewerID, id, "", nil)
	if err != nil {
		v.Error = "Discussions couldn't load. Refresh to try again."
	}
	return v
}
func (a *api) discussionCollectionPage(c echo.Context) error {
	principal, err := a.discussionViewer(c, true)
	if err != nil {
		return a.accountPageError(c, err)
	}
	id, err := savedEventID(c)
	if err != nil {
		return a.accountPageError(c, err)
	}
	values := c.QueryParams()
	origin := safeReturnURL(values.Get("return_to"))
	parent := values.Get("reply_to")
	for _, key := range []string{"return_to", "reply_to"} {
		if len(values[key]) > 1 {
			return a.accountPageError(c, &accounts.ValidationError{Field: key, Message: "use one value"})
		}
		values.Del(key)
	}
	p := home.DiscussionPage{View: home.DiscussionView{Enabled: true, ViewerID: principal.Account.ID, CSRF: principal.CSRF, Key: accounts.ID()}, ReturnURL: origin, URL: c.Request().URL.RequestURI(), ParentID: parent}
	thread := c.Param("discussion_id")
	if thread != "" {
		root, err := a.discussions.Thread(c.Request().Context(), principal.Account.ID, id, thread)
		if err != nil {
			return a.accountPageError(c, err)
		}
		p.Root = &root
		p.Event = root.Event
		if parent != "" {
			target, err := a.discussions.Parent(c.Request().Context(), principal.Account.ID, id, thread, parent)
			if err != nil {
				return a.accountPageError(c, err)
			}
			p.ReplyTo = &target
		}
	} else {
		if parent != "" {
			return a.accountPageError(c, &accounts.ValidationError{Field: "reply_to", Message: "open a thread to reply"})
		}
		d, err := a.eventDetail(c.Request().Context(), id)
		if err != nil {
			return a.accountPageError(c, err)
		}
		p.Event = d.Item
	}
	p.View.List, err = a.discussions.List(c.Request().Context(), principal.Account.ID, id, thread, values)
	status := 200
	if err != nil {
		status, _, p.View.Error, _ = accountError(err)
	}
	return render(c, status, home.Discussion(p))
}
func (a *api) discussionCommunityPage(c echo.Context) error {
	principal, err := a.discussionViewer(c, true)
	p := home.DiscussionCommunityPage{Enabled: a.discussionsEnabled(), URL: c.Request().URL.RequestURI()}
	if !p.Enabled {
		return render(c, 200, home.DiscussionCommunity(p))
	}
	if err != nil {
		return a.accountPageError(c, err)
	}
	p.Filters, err = discussions.ParseQuery(c.QueryParams(), "community:"+principal.Account.ID, true)
	if err == nil {
		p.List, err = a.discussions.List(c.Request().Context(), principal.Account.ID, "", "", c.QueryParams())
	}
	status := 200
	if err != nil {
		status, _, p.Error, _ = accountError(err)
	}
	if a.events != nil {
		catalog, e := a.events.Categories(c.Request().Context())
		p.Categories = catalog.Items
		p.CategoriesUnavailable = e != nil
	}
	return render(c, status, home.DiscussionCommunity(p))
}
func (a *api) discussionActionPage(c echo.Context) error {
	privateAccountResponse(c)
	if err := parseBoundedAccountForm(c, 65536, "action", "body", "event_id", "thread_id", "parent_id", "post_id", "idempotency_key", "return_to"); err != nil {
		return a.accountPageError(c, err)
	}
	f := home.DiscussionForm{EventID: c.FormValue("event_id"), ThreadID: c.FormValue("thread_id"), ParentID: c.FormValue("parent_id"), PostID: c.FormValue("post_id"), Body: c.FormValue("body"), Key: c.FormValue("idempotency_key"), Action: c.FormValue("action"), ReturnURL: safeDiscussionReturn(c.FormValue("return_to"))}
	jsonResponse := strings.Contains(c.Request().Header.Get("Accept"), "application/json")
	principal, err := a.principal(c, true)
	f.CSRF = principal.CSRF
	f.ViewerID = principal.Account.ID
	failure := func(err error) error {
		if jsonResponse {
			return accountProblem(c, err)
		}
		if errors.Is(err, discussions.ErrConflict) {
			f.Key = accounts.ID()
		}
		status, _, message, _ := accountError(err)
		return render(c, status, home.DiscussionFailure(f, message, a.enabled() && errors.Is(err, accounts.ErrUnauthenticated)))
	}
	if err != nil {
		return failure(err)
	}
	if err = a.checkWrite(c, principal); err != nil {
		return failure(err)
	}
	if !a.discussionsEnabled() {
		return failure(discussions.ErrUnavailable)
	}
	notice := "Contribution saved."
	location := f.ReturnURL
	switch f.Action {
	case "create":
		if err = a.accounts.Allow(c.Request().Context(), "discussion-create", principal.Account.ID); err != nil {
			return failure(err)
		}
		post, _, e := a.discussions.Create(c.Request().Context(), principal.Account.ID, f.EventID, f.ThreadID, f.ParentID, f.Body, f.Key)
		err = e
		if err == nil {
			location = home.ThreadURL(post.EventID, post.ThreadID, home.DiscussionOrigin(f.ReturnURL))
			notice = "Your contribution was published."
		}
	case "edit":
		_, err = a.discussions.Edit(c.Request().Context(), principal.Account.ID, f.PostID, f.Body)
	case "remove":
		err = a.discussions.Remove(c.Request().Context(), principal.Account.ID, f.PostID)
		notice = "Your contribution was removed. Replies are kept."
	case "helpful", "unhelpful":
		_, err = a.discussions.React(c.Request().Context(), principal.Account.ID, f.PostID, f.Action == "helpful")
		notice = "Helpful choice saved."
	default:
		err = &accounts.ValidationError{Field: "action", Message: "choose a supported discussion action"}
	}
	if err != nil {
		return failure(err)
	}
	if jsonResponse {
		return c.JSON(200, struct {
			Location string `json:"location"`
			Notice   string `json:"notice"`
		}{location, notice})
	}
	return c.Redirect(303, location)
}
func isDiscussionReturn(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Path == "/community/discussions" || strings.Contains(u.Path, "/discussions"))
}
func safeDiscussionReturn(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || u.IsAbs() || u.Host != "" || u.RawPath != "" || u.Fragment != "" || strings.ContainsAny(raw, "\\\r\n\x00") {
		return "/"
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "/"
	}
	if u.Path == "/community/discussions" {
		copy := url.Values{}
		for k, v := range values {
			copy[k] = v
		}
		copy.Del("cursor")
		if _, err := discussions.ParseQuery(copy, "community", true); err == nil && len(values["cursor"]) <= 1 && len(values.Get("cursor")) <= 1024 {
			return u.String()
		}
		return "/community/discussions"
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) >= 3 && len(parts) <= 4 && parts[0] == "events" && parts[2] == "discussions" && saved.ValidateID(parts[1]) == nil {
		if len(parts) == 4 && discussions.PostID(parts[3]) != nil {
			return "/"
		}
		for k, v := range values {
			if len(v) != 1 || (k != "return_to" && k != "reply_to" && k != "cursor" && k != "limit") {
				return "/"
			}
		}
		if values.Get("reply_to") != "" && (len(parts) != 4 || discussions.PostID(values.Get("reply_to")) != nil) {
			return "/"
		}
		if len(values.Get("cursor")) > 1024 {
			return "/"
		}
		return u.String()
	}
	if isDiscussionReturn(raw) {
		return "/"
	}
	return safeInterestReturn(raw)
}
