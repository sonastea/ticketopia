package api

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/views/home"
)

func (a *api) discussionFollowRoutes(e *echo.Echo) {
	path := "/api/v1/events/:event_id/discussions/:discussion_id/follow"
	e.GET(path, a.discussionFollowHandler)
	e.PUT(path, a.discussionFollowHandler)
	e.DELETE(path, a.discussionFollowHandler)
	e.GET("/api/v1/me/discussions", a.discussionFollowListHandler)
	e.GET("/api/v1/me/notifications", a.discussionNotificationListHandler)
	e.PUT("/api/v1/me/notifications/:notification_id/read", a.discussionNotificationReadHandler)
	e.GET("/me/discussions", a.discussionFollowPage)
	e.GET("/me/notifications", a.discussionFollowPage)
	e.POST("/discussion-follow-actions", a.discussionFollowAction)
}
func (a *api) discussionFollowPrincipal(c echo.Context, write, browser bool) (accounts.Principal, error) {
	privateAccountResponse(c)
	p, err := a.principal(c, browser)
	if err == nil && write {
		err = a.checkWrite(c, p)
	}
	if err == nil && (!a.discussionsEnabled() || a.discussionFollows == nil) {
		err = discussionfollows.ErrUnavailable
	}
	return p, err
}
func rejectFollowQuery(c echo.Context) error {
	if len(c.QueryParams()) != 0 {
		return &accounts.ValidationError{Field: "query", Message: "this route does not accept query parameters"}
	}
	return nil
}
func (a *api) discussionFollowHandler(c echo.Context) error {
	p, err := a.discussionFollowPrincipal(c, c.Request().Method != "GET", false)
	if err != nil {
		return accountProblem(c, err)
	}
	if err = rejectFollowQuery(c); err != nil {
		return accountProblem(c, err)
	}
	event, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	thread := c.Param("discussion_id")
	if c.Request().Method == "DELETE" {
		if err = emptyDiscussionBody(c); err == nil {
			err = a.discussionFollows.Remove(c.Request().Context(), p.Account.ID, event, thread)
		}
		if err != nil {
			return accountProblem(c, err)
		}
		return c.NoContent(204)
	}
	var follow discussionfollows.Follow
	if c.Request().Method == "PUT" {
		var body struct {
			Frequency string `json:"frequency"`
		}
		if err = decodeAccountJSON(c, &body); err != nil {
			return accountProblem(c, err)
		}
		follow, err = a.discussionFollows.Set(c.Request().Context(), p.Account.ID, event, thread, body.Frequency)
	} else {
		follow, err = a.discussionFollows.Get(c.Request().Context(), p.Account.ID, event, thread)
	}
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, follow)
}
func (a *api) discussionFollowListHandler(c echo.Context) error {
	p, err := a.discussionFollowPrincipal(c, false, false)
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.discussionFollows.List(c.Request().Context(), p.Account.ID, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) discussionNotificationListHandler(c echo.Context) error {
	p, err := a.discussionFollowPrincipal(c, false, false)
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.discussionFollows.Notifications(c.Request().Context(), p.Account.ID, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) discussionNotificationReadHandler(c echo.Context) error {
	p, err := a.discussionFollowPrincipal(c, true, false)
	if err != nil {
		return accountProblem(c, err)
	}
	if err = rejectFollowQuery(c); err == nil {
		err = emptyDiscussionBody(c)
	}
	if err == nil {
		err = a.discussionFollows.Read(c.Request().Context(), p.Account.ID, c.Param("notification_id"))
	}
	if err != nil {
		return accountProblem(c, err)
	}
	return c.NoContent(204)
}
func safeDiscussionFollowReturn(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 4096 || u.IsAbs() || u.Host != "" || u.RawPath != "" || u.Fragment != "" || strings.ContainsAny(raw, "\\\r\n\x00") {
		return "/me/discussions"
	}
	if u.Path != "/me/discussions" && u.Path != "/me/notifications" {
		return safeDiscussionReturn(raw)
	}
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return u.Path
	}
	// Cursors are owner-scoped at read time; validate shape here without trusting
	// their scope or carrying arbitrary parameters through sign-in/redirects.
	for k, v := range values {
		if len(v) != 1 || (k != "limit" && k != "cursor") || len(v[0]) > 1024 {
			return u.Path
		}
	}
	copy := url.Values{}
	if values.Has("limit") {
		copy.Set("limit", values.Get("limit"))
	}
	if _, err = discussionfollows.ParseQuery(copy, ""); err != nil {
		return u.Path
	}
	return u.String()
}
func (a *api) discussionFollowPage(c echo.Context) error {
	p, err := a.discussionFollowPrincipal(c, false, true)
	if err != nil {
		return a.accountPageError(c, err)
	}
	page := home.DiscussionFollowPage{CSRF: p.CSRF, URL: c.Request().URL.RequestURI(), Notifications: c.Path() == "/me/notifications", Paused: p.Account.Preferences.NotificationsPaused}
	if page.Notifications {
		page.Inbox, err = a.discussionFollows.Notifications(c.Request().Context(), p.Account.ID, c.QueryParams())
	} else {
		page.Follows, err = a.discussionFollows.List(c.Request().Context(), p.Account.ID, c.QueryParams())
	}
	status := http.StatusOK
	if err != nil {
		status, _, page.Error, _ = accountError(err)
	}
	return render(c, status, home.FollowedDiscussions(page))
}
func (a *api) discussionFollowAction(c echo.Context) error {
	privateAccountResponse(c)
	if err := parseAccountForm(c, "action", "event_id", "thread_id", "frequency", "notification_id", "return_to"); err != nil {
		return a.accountPageError(c, err)
	}
	p, err := a.discussionFollowPrincipal(c, true, true)
	if err != nil {
		return a.accountPageError(c, err)
	}
	switch c.FormValue("action") {
	case "follow":
		_, err = a.discussionFollows.Set(c.Request().Context(), p.Account.ID, c.FormValue("event_id"), c.FormValue("thread_id"), c.FormValue("frequency"))
	case "unfollow":
		err = a.discussionFollows.Remove(c.Request().Context(), p.Account.ID, c.FormValue("event_id"), c.FormValue("thread_id"))
	case "read":
		err = a.discussionFollows.Read(c.Request().Context(), p.Account.ID, c.FormValue("notification_id"))
	default:
		err = &accounts.ValidationError{Field: "action", Message: "choose follow, unfollow, or read"}
	}
	if err != nil {
		return a.accountPageError(c, err)
	}
	return c.Redirect(303, safeDiscussionFollowReturn(c.FormValue("return_to")))
}
