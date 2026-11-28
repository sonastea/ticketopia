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
	"github.com/sonastea/ticketopia/internal/drafts"
	"github.com/sonastea/ticketopia/internal/models"
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
	e.PUT("/api/v1/posts/:post_id/reactions/same-question", a.sameQuestionHandler)
	e.DELETE("/api/v1/posts/:post_id/reactions/same-question", a.sameQuestionHandler)
	e.GET("/api/v1/community/discussions", a.discussionListHandler)
	e.GET("/api/v1/community/questions", a.openQuestionsHandler)
	e.GET("/events/:event_id/discussions", a.discussionCollectionPage)
	e.GET("/events/:event_id/discussions/:discussion_id", a.discussionCollectionPage)
	e.GET("/events/:event_id/discussions/:discussion_id/new-replies", a.newRepliesHandler)
	e.GET("/events/:event_id/discussions/:discussion_id/inline", a.inlineRepliesHandler)
	e.GET("/community/discussions", a.discussionCommunityPage)
	e.GET("/community/questions", a.openQuestionsPage)
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

// discussionWriter authorizes posting and reactions: public, so an API token
// needs publish. Editing and removal use discussionAuthor.
func (a *api) discussionWriter(c echo.Context) (accounts.Principal, error) {
	p, err := a.requireAPI(c, true, false)
	if err == nil && !a.discussionsEnabled() {
		err = discussions.ErrUnavailable
	}
	if err == nil {
		err = a.checkPublic(c, p)
	}
	return p, err
}

// discussionAuthor authorizes editing or removing the person's own posts,
// which only they do, on the website.
func (a *api) discussionAuthor(c echo.Context) (accounts.Principal, error) {
	p, err := a.requireAPI(c, true, false)
	if err == nil {
		err = checkBrowser(p)
	}
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
	var post discussions.Post
	var created bool
	if p.Credential.Kind == "api" {
		// Labelled as an API client's, like every token's public action.
		key := c.Request().Header.Get("Idempotency-Key")
		if err = discussions.CallerKey(key); err == nil {
			post, created, err = a.agentPost(c.Request().Context(), p.Account.ID, id, c.Param("discussion_id"), body.ParentID, body.Body, key, drafts.Client{})
		}
	} else {
		if err := a.accounts.Allow(c.Request().Context(), "discussion-create", p.Account.ID); err != nil {
			return accountProblem(c, err)
		}
		post, created, err = a.discussions.Create(c.Request().Context(), p.Account.ID, id, c.Param("discussion_id"), body.ParentID, body.Body, c.Request().Header.Get("Idempotency-Key"))
	}
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
	p, err := a.discussionAuthor(c)
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
	p, err := a.discussionAuthor(c)
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
func (a *api) openQuestionsHandler(c echo.Context) error {
	p, err := a.discussionViewer(c, false)
	if err != nil {
		return accountProblem(c, err)
	}
	list, err := a.discussions.OpenQuestions(c.Request().Context(), p.Account.ID, c.QueryParams())
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, list)
}
func (a *api) sameQuestionHandler(c echo.Context) error {
	p, err := a.discussionWriter(c)
	if err != nil {
		return accountProblem(c, err)
	}
	if err = emptyDiscussionBody(c); err != nil {
		return accountProblem(c, err)
	}
	post, err := a.discussions.SameQuestion(c.Request().Context(), p.Account.ID, c.Param("post_id"), c.Request().Method == "PUT")
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, post)
}
func (a *api) discussionView(c echo.Context, event models.Event) home.DiscussionView {
	id := event.ID
	v := home.DiscussionView{Enabled: a.discussionsEnabled(), ModerationEnabled: a.moderationEnabled(), Key: accounts.ID(), Phase: home.PhaseOf(event, a.clock())}
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
		return v
	}
	v.VenueTips = a.venueTips(c, v.ViewerID, event)
	return v
}

// venueTips is optional context: a failed read leaves the section out rather
// than failing the event's own conversations.
func (a *api) venueTips(c echo.Context, viewer string, event models.Event) []discussions.Post {
	tips, err := a.discussions.VenueTips(c.Request().Context(), viewer, event)
	if err != nil {
		return nil
	}
	return tips
}
func (a *api) discussionCollectionPage(c echo.Context) error {
	privateAccountResponse(c)
	c.Response().Header().Set("Vary", "X-Ticketopia-Panel, X-Ticketopia-Section")
	id, err := savedEventID(c)
	if err != nil {
		return a.eventTaskError(c, err, c.QueryParam("return_to"))
	}
	p, status, err := a.loadDiscussionPage(c, id, c.Param("discussion_id"), c.QueryParams(), c.Request().URL.RequestURI())
	if err != nil {
		return a.eventTaskError(c, err, c.Request().URL.RequestURI())
	}
	if c.Request().Header.Get("X-Ticketopia-Panel") == "true" {
		page := home.EventPage{Detail: models.EventDetail{Item: p.Event, Meta: p.Meta}, Section: "discussion", ReturnURL: p.ReturnURL, ActionReturnURL: p.URL, Thread: &p, Saves: a.saveView(c, []string{id}), Interests: a.interestView(c, []string{id})}
		a.loadEventCommunity(c, &page)
		if c.Request().Header.Get("X-Ticketopia-Section") == "true" {
			return render(c, status, home.EventSectionResponse(page, true))
		}
		return render(c, http.StatusOK, home.EventContext(page))
	}
	return render(c, status, home.Discussion(p))
}

// inlineRepliesHandler serves a question or tip's replies and reply box as a
// fragment the event's Questions & tips list expands in place, like comment
// threads: cursor pages more replies, reply_to targets one reply, post_id
// adds one that isn't on the page. Links and forms inside point at the
// shareable thread page, so they work anywhere, with or without script.
func (a *api) inlineRepliesHandler(c echo.Context) error {
	privateAccountResponse(c)
	id, err := savedEventID(c)
	thread := c.Param("discussion_id")
	if err == nil {
		// Only a conversation's replies expand: never the event's list.
		err = discussions.PostID(thread)
	}
	var p home.DiscussionPage
	status := http.StatusOK
	if err == nil {
		p, status, err = a.loadDiscussionPage(c, id, thread, c.QueryParams(), "")
	}
	if err != nil {
		status, _, detail, _ := accountError(err)
		return render(c, status, home.InlineRepliesFailure(detail, home.ThreadURL(c.Param("event_id"), thread, "")))
	}
	p.URL = home.ThreadURL(id, thread, p.ReturnURL)
	return render(c, status, home.InlineReplies(p, home.InlineRepliesURL(id, thread, p.ReturnURL)))
}

// Both the discovery shell and shareable thread use the same permission-filtered
// read. Threads retain their event snapshot and never need a provider refresh.
func (a *api) loadDiscussionPage(c echo.Context, id, thread string, query url.Values, rawURL string) (home.DiscussionPage, int, error) {
	principal, err := a.discussionViewer(c, true)
	if err != nil {
		return home.DiscussionPage{}, 0, err
	}
	values := url.Values{}
	for key, value := range query {
		values[key] = value
	}
	origin := safeReturnURL(values.Get("return_to"))
	parent := values.Get("reply_to")
	selected := values.Get("post_id")
	from := values.Get("from")
	for _, key := range []string{"return_to", "reply_to", "post_id", "from"} {
		if len(values[key]) > 1 {
			return home.DiscussionPage{}, 0, &accounts.ValidationError{Field: key, Message: "use one value"}
		}
		values.Del(key)
	}
	p := home.DiscussionPage{View: home.DiscussionView{Enabled: true, ModerationEnabled: a.moderationEnabled(), ViewerID: principal.Account.ID, CSRF: principal.CSRF, Key: accounts.ID()}, ReturnURL: origin, URL: rawURL, ParentID: parent}
	if thread != "" {
		root, err := a.discussions.Thread(c.Request().Context(), principal.Account.ID, id, thread)
		if err != nil {
			return p, 0, err
		}
		p.Root = &root
		if a.discussionFollows != nil && principal.Account.ID != "" {
			p.FollowEnabled = true
			p.NotificationsPaused = principal.Account.Preferences.NotificationsPaused
			follow, followErr := a.discussionFollows.Get(c.Request().Context(), principal.Account.ID, id, thread)
			if followErr == nil {
				p.Follow = &follow
			} else if !errors.Is(followErr, discussions.ErrNotFound) {
				p.FollowError = "Following couldn't load. Refresh to try again."
			}
		}
		p.Event = root.Event
		p.Meta = root.Meta
		if parent != "" {
			target, err := a.discussions.Parent(c.Request().Context(), principal.Account.ID, id, thread, parent)
			if err != nil {
				return p, 0, err
			}
			p.ReplyTo = &target
		}
		if selected != "" {
			target, err := a.discussions.Parent(c.Request().Context(), principal.Account.ID, id, thread, selected)
			if err != nil {
				return p, 0, err
			}
			p.Selected = &target
		}
	} else {
		if selected != "" {
			return p, 0, &accounts.ValidationError{Field: "post_id", Message: "open a thread to select a contribution"}
		}
		if parent != "" {
			return p, 0, &accounts.ValidationError{Field: "reply_to", Message: "open a thread to reply"}
		}
		d, err := a.eventDetail(c.Request().Context(), id)
		if err != nil {
			return p, 0, err
		}
		p.Event = d.Item
		p.Meta = d.Meta
	}
	if from != "" && thread != "" {
		p.View.List, err = a.discussions.RepliesFrom(c.Request().Context(), principal.Account.ID, id, thread, from, values)
	} else if from != "" {
		return p, 0, &accounts.ValidationError{Field: "from", Message: "open a thread to start from a reply"}
	} else {
		p.View.List, err = a.discussions.List(c.Request().Context(), principal.Account.ID, id, thread, values)
	}
	p.View.Phase = home.PhaseOf(p.Event, a.clock())
	status := 200
	if err != nil {
		status, _, p.View.Error, _ = accountError(err)
	} else if thread == "" && !values.Has("cursor") {
		p.View.VenueTips = a.venueTips(c, principal.Account.ID, p.Event)
	}
	if p.Root != nil && err == nil && principal.Account.ID != "" && a.liveRepliesEnabled() {
		// Engagement read from what the page already holds: no extra query.
		p.LiveReplies = true
		p.Engaged = p.Follow != nil || (p.Root.Profile != nil && p.Root.Profile.ID == principal.Account.ID)
		shown := p.View.List.Items
		if p.Selected != nil {
			shown = append(shown[:len(shown):len(shown)], *p.Selected)
		}
		for _, reply := range shown {
			if reply.Profile != nil && reply.Profile.ID == principal.Account.ID {
				p.Engaged = true
			}
		}
	}
	return p, status, nil
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
		catalog, e := a.optionalCategories(c.Request().Context())
		p.Categories = catalog.Items
		p.CategoriesUnavailable = e != nil
	}
	return render(c, status, home.DiscussionCommunity(p))
}
func (a *api) openQuestionsPage(c echo.Context) error {
	principal, err := a.discussionViewer(c, true)
	p := home.OpenQuestionsPage{Enabled: a.discussionsEnabled(), URL: c.Request().URL.RequestURI()}
	if !p.Enabled {
		return render(c, 200, home.OpenQuestions(p))
	}
	if err != nil {
		return a.accountPageError(c, err)
	}
	p.ViewerID, p.CSRF = principal.Account.ID, principal.CSRF
	p.Filters, p.Mine, err = discussions.ParseOpenQuery(principal.Account.ID, c.QueryParams())
	if errors.Is(err, accounts.ErrUnauthenticated) {
		return c.Redirect(303, "/auth/sign-in?return_to="+url.QueryEscape(p.URL))
	}
	if err == nil {
		p.List, err = a.discussions.OpenQuestions(c.Request().Context(), principal.Account.ID, c.QueryParams())
	}
	status := 200
	if err != nil {
		status, _, p.Error, _ = accountError(err)
	}
	if a.events != nil {
		catalog, e := a.optionalCategories(c.Request().Context())
		p.Categories = catalog.Items
		p.CategoriesUnavailable = e != nil
	}
	return render(c, status, home.OpenQuestions(p))
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
			f.Conflict = true
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
			// Select the confirmed contribution even when it is outside the current
			// reply page; clients need not jump to the top or guess where it landed.
			target, _ := url.Parse(location)
			if post.ID != post.ThreadID {
				values := target.Query()
				values.Set("post_id", post.ID)
				target.RawQuery = values.Encode()
			}
			target.Fragment = "post-" + post.ID
			location = target.String()
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
	case "same_question", "not_same_question":
		_, err = a.discussions.SameQuestion(c.Request().Context(), principal.Account.ID, f.PostID, f.Action == "same_question")
		notice = "Same question choice saved."
		if strings.HasPrefix(f.ReturnURL, "/community/questions") && discussions.PostID(f.PostID) == nil {
			// The native form returns to its card, not the top of the list.
			location += "#question-" + f.PostID
		}
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
	return err == nil && (u.Path == "/community/discussions" || u.Path == "/community/questions" || strings.Contains(u.Path, "/discussions"))
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
	if u.Path == "/community/questions" {
		copy := url.Values{}
		for k, v := range values {
			copy[k] = v
		}
		copy.Del("cursor")
		// Validated as a signed-in viewer so "mine" passes; the page itself
		// still requires sign-in for it.
		if _, _, err := discussions.ParseOpenQuery(strings.Repeat("0", 32), copy); err == nil && len(values["cursor"]) <= 1 && len(values.Get("cursor")) <= 1024 {
			return u.String()
		}
		return "/community/questions"
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
			if len(v) != 1 || (k != "return_to" && k != "reply_to" && k != "post_id" && k != "from" && k != "cursor" && k != "limit") {
				return "/"
			}
		}
		if values.Get("reply_to") != "" && (len(parts) != 4 || discussions.PostID(values.Get("reply_to")) != nil) {
			return "/"
		}
		if values.Get("post_id") != "" && (len(parts) != 4 || discussions.PostID(values.Get("post_id")) != nil) {
			return "/"
		}
		if values.Get("from") != "" && (len(parts) != 4 || discussions.PostID(values.Get("from")) != nil || values.Has("cursor")) {
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
