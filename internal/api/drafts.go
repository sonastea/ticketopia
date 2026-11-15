package api

import (
	"errors"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/drafts"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
	"github.com/sonastea/ticketopia/views/account"
	"github.com/sonastea/ticketopia/views/home"
)

// Agent drafts are reviewed on the web: the page only lists them, and
// publishing is the signed-in person's CSRF-protected POST, never a GET or
// an agent call.
func (a *api) draftRoutes(e *echo.Echo) {
	e.GET("/me/drafts", a.draftsPage)
	e.POST("/me/drafts/:draft_id", a.draftDecisionPage)
}

// draftsEnabled reports whether drafts can be stored and published.
func (a *api) draftsEnabled() bool {
	return a.enabled() && a.drafts != nil && a.recommendations != nil
}

func (a *api) draftsPage(c echo.Context) error {
	privateAccountResponse(c)
	if !a.draftsEnabled() {
		return render(c, 200, account.Drafts(account.DraftsPage{}))
	}
	p, err := a.principal(c, true)
	if errors.Is(err, accounts.ErrUnauthenticated) {
		return c.Redirect(303, "/auth/sign-in?return_to="+url.QueryEscape(c.Request().URL.RequestURI()))
	}
	if err != nil {
		return a.accountPageError(c, err)
	}
	page := account.DraftsPage{Enabled: true, CSRF: p.CSRF}
	values := c.QueryParams()
	for key, v := range values {
		if len(v) != 1 || (key != "saved" && key != "event_id" && key != "thread_id" && key != "post_id") {
			return a.accountPageError(c, &accounts.ValidationError{Field: "query", Message: "unsupported drafts query"})
		}
	}
	event, thread, post := values.Get("event_id"), values.Get("thread_id"), values.Get("post_id")
	switch values.Get("saved") {
	case "":
	case "published":
		page.Notice = "Your recommendation was published."
		if saved.ValidateID(event) == nil {
			page.NoticeURL, page.NoticeLink = "/events/"+url.PathEscape(event)+"?section=community", "See it on the event"
		}
	case "question", "reply":
		page.Notice = "Your question or tip was published."
		if values.Get("saved") == "reply" {
			page.Notice = "Your reply was published."
		}
		if saved.ValidateID(event) == nil && discussions.PostID(thread) == nil && discussions.PostID(post) == nil {
			page.NoticeURL, page.NoticeLink = home.ContributionURL(event, thread, post), "See it in the conversation"
		}
	case "discarded":
		page.Notice = "The draft was discarded. Nothing was published."
	case "changed":
		page.Notice = "Your recommendation for that event changed after you opened this page, so nothing was published. Review the draft again."
	default:
		return a.accountPageError(c, &accounts.ValidationError{Field: "saved", Message: "unsupported drafts notice"})
	}
	ctx := c.Request().Context()
	if page.Drafts, err = a.drafts.List(ctx, p.Account.ID); err != nil {
		return a.accountPageError(c, err)
	}
	// Each recommendation draft shows the reason it would replace; the form
	// carries what was shown, and publishing refuses if it changed meanwhile.
	// Each reply shows the question or tip, and the reply, it answers.
	page.Current = map[string]*recommendations.Item{}
	page.Posts = map[string]*discussions.Post{}
	for _, d := range page.Drafts {
		switch d.Kind {
		case drafts.KindRecommendation:
			if _, seen := page.Current[d.EventID]; seen {
				continue
			}
			item, err := a.recommendations.Get(ctx, p.Account.ID, d.EventID)
			switch {
			case err == nil:
				page.Current[d.EventID] = &item
			case errors.Is(err, recommendations.ErrNotFound):
				page.Current[d.EventID] = nil
			default:
				return a.accountPageError(c, err)
			}
		case drafts.KindReply:
			if err := a.draftConversation(c, p.Account.ID, d, page.Posts); err != nil {
				return a.accountPageError(c, err)
			}
		}
	}
	// Read after listing, so the badge matches the list.
	page.Navigation = a.accountNavigation(c, p.Account.ID)
	return render(c, 200, account.Drafts(page))
}

// draftConversation adds a reply draft's thread root and parent to posts by
// ID, nil when it no longer exists on the event or discussions are off: the
// page then offers only Discard.
func (a *api) draftConversation(c echo.Context, owner string, d drafts.Draft, posts map[string]*discussions.Post) error {
	ids := []string{d.ThreadID}
	if d.ParentID != "" {
		ids = append(ids, d.ParentID)
	}
	for _, id := range ids {
		if _, seen := posts[id]; seen {
			continue
		}
		if !a.discussionsEnabled() {
			posts[id] = nil
			continue
		}
		post, err := a.discussions.Parent(c.Request().Context(), owner, d.EventID, d.ThreadID, id)
		switch {
		case err == nil:
			posts[id] = &post
		case errors.Is(err, discussions.ErrNotFound):
			posts[id] = nil
		default:
			return err
		}
	}
	return nil
}

// draftDecisionPage publishes or discards one of the caller's drafts.
// Publishing consumes the draft in the same transaction as the
// recommendation or post, so a repeated or concurrent submission finds it
// gone (404) and a failure leaves it in place. For a reason, the form carries
// the recommendation state the person saw; if it changed since, nothing is
// published and the page asks for a fresh review. Questions, tips and replies
// publish through publishDiscussionDraft. Each validates the text and target
// again and attributes the result to the app that wrote it. Agent
// publications log nothing per account, so no observation log here.
func (a *api) draftDecisionPage(c echo.Context) error {
	privateAccountResponse(c)
	if err := parseAccountForm(c, "action", "has_current", "current_hash"); err != nil {
		return a.accountPageError(c, err)
	}
	p, err := a.principal(c, true)
	if err != nil {
		return a.accountPageError(c, err)
	}
	if err = a.checkWrite(c, p); err != nil {
		return a.accountPageError(c, err)
	}
	if !a.draftsEnabled() {
		return a.accountPageError(c, drafts.ErrUnavailable)
	}
	ctx, id := c.Request().Context(), c.Param("draft_id")
	switch c.FormValue("action") {
	case "publish":
		draft, err := a.drafts.Get(ctx, p.Account.ID, id)
		if err != nil {
			return a.accountPageError(c, err)
		}
		if draft.Discussion() {
			return a.publishDiscussionDraft(c, p.Account.ID, draft)
		}
		reviewed := &recommendations.Current{Active: c.FormValue("has_current") == "1", ReasonHash: c.FormValue("current_hash")}
		if has := c.FormValue("has_current"); has != "0" && has != "1" {
			return a.accountPageError(c, &accounts.ValidationError{Field: "has_current", Message: "reload your drafts and review again"})
		}
		_, _, err = a.recommendations.Publish(ctx, p.Account.ID, draft.EventID, recommendations.Write{Reason: draft.Text,
			Via: draft.ClientName, ViaHost: draft.ClientHost, Expect: reviewed, Draft: draft.ID})
		if errors.Is(err, recommendations.ErrChanged) {
			return c.Redirect(303, "/me/drafts?saved=changed")
		}
		if err != nil {
			return a.accountPageError(c, err)
		}
		return c.Redirect(303, "/me/drafts?saved=published&event_id="+url.QueryEscape(draft.EventID))
	case "discard":
		// Nothing pending, including another account's draft, is 404 as for
		// Publish.
		discarded, err := a.drafts.Discard(ctx, p.Account.ID, id)
		if err == nil && !discarded {
			err = drafts.ErrNotFound
		}
		if err != nil {
			return a.accountPageError(c, err)
		}
		return c.Redirect(303, "/me/drafts?saved=discarded")
	default:
		return a.accountPageError(c, &accounts.ValidationError{Field: "action", Message: "choose publish or discard"})
	}
}

// publishDiscussionDraft publishes a question, tip or reply draft as the
// person's post, with the discussion routes' rules and rate limit. The post
// is attributed to the app that wrote it and consumes the draft in the same
// transaction; the draft's own idempotency key makes a repeat find the same
// post rather than a duplicate.
func (a *api) publishDiscussionDraft(c echo.Context, owner string, draft drafts.Draft) error {
	if !a.discussionsEnabled() {
		return a.accountPageError(c, discussions.ErrUnavailable)
	}
	ctx := c.Request().Context()
	if err := a.accounts.Allow(ctx, "discussion-create", owner); err != nil {
		return a.accountPageError(c, err)
	}
	post, _, err := a.discussions.Publish(ctx, owner, draft.EventID, draft.ThreadID, draft.ParentID, draft.Text, draft.PublishKey(),
		discussions.Attribution{Via: draft.ClientName, ViaHost: draft.ClientHost, Draft: draft.ID})
	if err != nil {
		return a.accountPageError(c, err)
	}
	notice := "question"
	if draft.Kind == drafts.KindReply {
		notice = "reply"
	}
	return c.Redirect(303, "/me/drafts?"+url.Values{"saved": {notice}, "event_id": {post.EventID}, "thread_id": {post.ThreadID}, "post_id": {post.ID}}.Encode())
}
