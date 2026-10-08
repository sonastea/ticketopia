package api

import (
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/moderation"
	"github.com/sonastea/ticketopia/views/home"
)

func (a *api) moderationRoutes(e *echo.Echo) {
	e.POST("/api/v1/posts/:post_id/reports", a.reportHandler)
	e.GET("/api/v1/me/reports", a.ownReportsHandler)
	e.GET("/api/v1/me/moderation", a.ownOutcomesHandler)
	e.POST("/api/v1/me/moderation/:decision_id/appeal", a.appealHandler)
	e.GET("/api/v1/moderation", a.moderationQueueHandler)
	e.GET("/api/v1/moderation/posts/:post_id", a.moderationCaseHandler)
	e.POST("/api/v1/moderation/posts/:post_id/decisions", a.moderationDecisionHandler)
	e.GET("/posts/:post_id/report", a.reportPage)
	e.POST("/posts/:post_id/report", a.reportPage)
	e.GET("/me/reports", a.ownModerationPage(false))
	e.GET("/me/moderation", a.ownModerationPage(true))
	e.POST("/me/moderation/:decision_id/appeal", a.appealPage)
	e.GET("/moderation", a.moderationQueuePage)
	e.GET("/moderation/posts/:post_id", a.moderationCasePage)
	e.POST("/moderation/posts/:post_id", a.moderationCasePage)
}
func (a *api) moderationEnabled() bool { return a.discussionsEnabled() && a.moderation != nil }
func (a *api) moderationPrincipal(c echo.Context, browser, write bool) (accounts.Principal, error) {
	p, err := a.principal(c, browser)
	if err != nil {
		return p, err
	}
	if !a.moderationEnabled() {
		return p, moderation.ErrUnavailable
	}
	if write {
		err = a.checkWrite(c, p)
	}
	return p, err
}
func (a *api) moderationWriter(c echo.Context) (accounts.Principal, error) {
	p, err := a.requireAPI(c, true, false)
	if err == nil && !a.moderationEnabled() {
		err = moderation.ErrUnavailable
	}
	return p, err
}
func (a *api) reportHandler(c echo.Context) error {
	p, err := a.moderationWriter(c)
	if err != nil {
		return accountProblem(c, err)
	}
	var input struct {
		Reason  string `json:"reason"`
		Context string `json:"context"`
	}
	if err = decodeAccountJSON(c, &input); err != nil {
		return accountProblem(c, err)
	}
	if err = a.accounts.Allow(c.Request().Context(), "moderation-report", p.Account.ID); err != nil {
		return accountProblem(c, err)
	}
	r, created, err := a.moderation.Report(c.Request().Context(), p.Account.ID, c.Param("post_id"), input.Reason, input.Context)
	if err != nil {
		return accountProblem(c, err)
	}
	status := 200
	if created {
		status = 201
	}
	c.Response().Header().Set("Location", "/api/v1/me/reports")
	return c.JSON(status, r)
}
func (a *api) ownReportsHandler(c echo.Context) error {
	p, err := a.moderationPrincipal(c, false, false)
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.moderation.Reports(c.Request().Context(), p.Account.ID, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) ownOutcomesHandler(c echo.Context) error {
	p, err := a.moderationPrincipal(c, false, false)
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.moderation.Outcomes(c.Request().Context(), p.Account.ID, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) appealHandler(c echo.Context) error {
	p, err := a.moderationWriter(c)
	if err != nil {
		return accountProblem(c, err)
	}
	var input struct {
		Context string `json:"context"`
	}
	if err = decodeAccountJSON(c, &input); err != nil {
		return accountProblem(c, err)
	}
	if err = a.accounts.Allow(c.Request().Context(), "moderation-appeal", p.Account.ID); err == nil {
		err = a.moderation.Appeal(c.Request().Context(), p.Account.ID, c.Param("decision_id"), input.Context)
	}
	if err != nil {
		return accountProblem(c, err)
	}
	return c.NoContent(204)
}
func (a *api) moderationQueueHandler(c echo.Context) error {
	p, err := a.moderationPrincipal(c, false, false)
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.moderation.Queue(c.Request().Context(), p.Account.ID, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) moderationCaseHandler(c echo.Context) error {
	p, err := a.moderationPrincipal(c, false, false)
	if err != nil {
		return accountProblem(c, err)
	}
	if !a.moderationEnabled() {
		return accountProblem(c, moderation.ErrUnavailable)
	}
	v, err := a.moderation.Case(c.Request().Context(), p.Account.ID, c.Param("post_id"), c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, v)
}
func (a *api) moderationDecisionHandler(c echo.Context) error {
	p, err := a.moderationWriter(c)
	if err != nil {
		return accountProblem(c, err)
	}
	if err = a.moderation.RequireModerator(c.Request().Context(), p.Account.ID); err != nil {
		return accountProblem(c, err)
	}
	var input moderation.Review
	if err = decodeAccountJSON(c, &input); err != nil {
		return accountProblem(c, err)
	}
	o, err := a.moderation.Decide(c.Request().Context(), p.Account.ID, c.Param("post_id"), input)
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(201, o)
}
func (a *api) moderationBrowser(c echo.Context, write bool) (accounts.Principal, error) {
	return a.moderationPrincipal(c, true, write)
}
func (a *api) moderationPageError(c echo.Context, err error) error {
	if errors.Is(err, accounts.ErrUnauthenticated) && c.Request().Method == "GET" {
		return c.Redirect(303, "/auth/sign-in?return_to="+url.QueryEscape(safeModerationReturn(c.Request().URL.RequestURI())))
	}
	return a.accountPageError(c, err)
}
func (a *api) reportPage(c echo.Context) error {
	write := c.Request().Method == "POST"
	if write {
		if err := parseBoundedAccountForm(c, 32768, "reason", "context"); err != nil {
			return a.accountPageError(c, err)
		}
	} else if len(c.QueryParams()) != 0 {
		return a.accountPageError(c, &accounts.ValidationError{Field: "query", Message: "this page does not accept query parameters"})
	}
	p, err := a.moderationBrowser(c, write)
	f := home.ReportPage{PostID: c.Param("post_id"), CSRF: p.CSRF, Reason: c.FormValue("reason"), Context: c.FormValue("context")}
	if err != nil {
		if write {
			status, _, message, _ := accountError(err)
			f.Error = message
			return render(c, status, home.ReportConcern(f))
		}
		return a.moderationPageError(c, err)
	}
	status := 200
	if write {
		if err = a.accounts.Allow(c.Request().Context(), "moderation-report", p.Account.ID); err == nil {
			_, _, err = a.moderation.Report(c.Request().Context(), p.Account.ID, f.PostID, f.Reason, f.Context)
		}
		if err == nil {
			return c.Redirect(303, "/me/reports")
		}
		status, _, f.Error, _ = accountError(err)
	}
	f.Post, err = a.discussions.Post(c.Request().Context(), p.Account.ID, f.PostID)
	if err != nil {
		if write {
			status, _, f.Error, _ = accountError(err)
			return render(c, status, home.ReportConcern(f))
		}
		return a.moderationPageError(c, err)
	}
	return render(c, status, home.ReportConcern(f))
}
func (a *api) ownModerationPage(outcomes bool) echo.HandlerFunc {
	return func(c echo.Context) error {
		p, err := a.moderationBrowser(c, false)
		if err != nil {
			return a.moderationPageError(c, err)
		}
		v := home.OwnModerationPage{CSRF: p.CSRF, URL: c.Request().URL.RequestURI(), OutcomesView: outcomes}
		v.Moderator, err = a.moderation.IsModerator(c.Request().Context(), p.Account.ID)
		if err != nil {
			return a.moderationPageError(c, err)
		}
		if outcomes {
			v.Outcomes, err = a.moderation.Outcomes(c.Request().Context(), p.Account.ID, c.QueryParams())
		} else {
			v.Reports, err = a.moderation.Reports(c.Request().Context(), p.Account.ID, c.QueryParams())
		}
		if err != nil {
			return a.moderationPageError(c, err)
		}
		return render(c, 200, home.OwnModeration(v))
	}
}
func (a *api) appealPage(c echo.Context) error {
	if err := parseBoundedAccountForm(c, 32768, "context"); err != nil {
		return a.accountPageError(c, err)
	}
	p, err := a.moderationBrowser(c, true)
	f := home.AppealForm{DecisionID: c.Param("decision_id"), CSRF: p.CSRF, Context: c.FormValue("context")}
	if err == nil {
		err = a.accounts.Allow(c.Request().Context(), "moderation-appeal", p.Account.ID)
	}
	if err == nil {
		err = a.moderation.Appeal(c.Request().Context(), p.Account.ID, f.DecisionID, f.Context)
	}
	if err != nil {
		status, _, message, _ := accountError(err)
		f.Error = message
		return render(c, status, home.AppealFailure(f))
	}
	return c.Redirect(303, "/me/moderation")
}
func (a *api) moderationQueuePage(c echo.Context) error {
	p, err := a.moderationBrowser(c, false)
	if err != nil {
		return a.moderationPageError(c, err)
	}
	list, err := a.moderation.Queue(c.Request().Context(), p.Account.ID, c.QueryParams())
	if err != nil {
		return a.moderationPageError(c, err)
	}
	return render(c, 200, home.ModerationQueue(home.ModerationQueuePage{List: list, URL: c.Request().URL.RequestURI(), All: c.QueryParam("state") == "all"}))
}
func (a *api) moderationCasePage(c echo.Context) error {
	write := c.Request().Method == "POST"
	if write {
		if err := parseBoundedAccountForm(c, 65536, "action", "reason", "notes", "expected_version", "expected_updated_at"); err != nil {
			return a.accountPageError(c, err)
		}
	}
	v := home.ModerationCasePage{URL: c.Request().URL.RequestURI()}
	v.Case.Post.ID = c.Param("post_id")
	if write {
		v.Review = moderation.Review{Action: c.FormValue("action"), Reason: c.FormValue("reason"), Notes: c.FormValue("notes")}
	}
	p, err := a.moderationBrowser(c, write)
	v.CSRF, v.ViewerID = p.CSRF, p.Account.ID
	if err != nil {
		if write {
			status, _, message, _ := accountError(err)
			v.Error = message
			return render(c, status, home.ModerationDraftFailure(v))
		}
		return a.moderationPageError(c, err)
	}
	if err = a.moderation.RequireModerator(c.Request().Context(), p.Account.ID); err != nil {
		if write {
			status, _, message, _ := accountError(err)
			v.Error = message
			return render(c, status, home.ModerationDraftFailure(v))
		}
		return a.moderationPageError(c, err)
	}
	status := 200
	if write {
		n, e1 := strconv.ParseInt(c.FormValue("expected_version"), 10, 64)
		at, e2 := time.Parse(time.RFC3339Nano, c.FormValue("expected_updated_at"))
		if e1 != nil || e2 != nil {
			err = &accounts.ValidationError{Field: "review", Message: "reload the contribution before reviewing"}
		} else {
			v.Review.ExpectedVersion = &n
			v.Review.ExpectedUpdatedAt = at
			_, err = a.moderation.Decide(c.Request().Context(), p.Account.ID, c.Param("post_id"), v.Review)
		}
		if err == nil {
			return c.Redirect(303, "/moderation/posts/"+c.Param("post_id"))
		}
		status, _, v.Error, _ = accountError(err)
	}
	v.Case, err = a.moderation.Case(c.Request().Context(), p.Account.ID, c.Param("post_id"), c.QueryParams())
	if err != nil {
		if write {
			status, _, v.Error, _ = accountError(err)
			v.Case.Post.ID = c.Param("post_id")
			return render(c, status, home.ModerationDraftFailure(v))
		}
		return a.moderationPageError(c, err)
	}
	return render(c, status, home.ModerationCase(v))
}
func safeModerationReturn(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || len(raw) > 2048 || u.IsAbs() || u.Host != "" || u.RawPath != "" || u.Fragment != "" || strings.ContainsAny(raw, "\\\r\n\x00") {
		return "/me"
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "/me"
	}
	if u.Path == "/me/reports" || u.Path == "/me/moderation" || u.Path == "/moderation" {
		copy := url.Values{}
		for k, v := range q {
			copy[k] = v
		}
		copy.Del("cursor")
		if _, err := moderation.ParseQuery(copy, "return", u.Path == "/moderation"); err == nil && len(q["cursor"]) <= 1 && len(q.Get("cursor")) <= 512 {
			return u.String()
		}
		return "/me"
	}
	parts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
	if len(parts) == 3 && parts[0] == "posts" && parts[2] == "report" && accounts.ValidID(parts[1]) && u.RawQuery == "" {
		return u.Path
	}
	if len(parts) == 3 && parts[0] == "moderation" && parts[1] == "posts" && accounts.ValidID(parts[2]) {
		for k, v := range q {
			if len(v) != 1 || (k != "limit" && k != "reports_cursor" && k != "decisions_cursor" && k != "appeals_cursor") || (k != "limit" && len(v[0]) > 512) {
				return "/me"
			}
		}
		limit := url.Values{}
		if v, ok := q["limit"]; ok {
			limit["limit"] = v
		}
		if _, err := moderation.ParseQuery(limit, "return", false); err != nil {
			return "/me"
		}
		return u.String()
	}
	return "/me"
}
