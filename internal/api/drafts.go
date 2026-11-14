package api

import (
	"errors"
	"net/url"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/drafts"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
	"github.com/sonastea/ticketopia/views/account"
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
		if len(v) != 1 || (key != "saved" && key != "event_id") {
			return a.accountPageError(c, &accounts.ValidationError{Field: "query", Message: "unsupported drafts query"})
		}
	}
	switch values.Get("saved") {
	case "":
	case "published":
		page.Notice = "Your recommendation was published."
		if id := values.Get("event_id"); saved.ValidateID(id) == nil {
			page.EventID = id
		}
	case "discarded":
		page.Notice = "The draft was discarded. Nothing was published."
	case "changed":
		page.Notice = "Your recommendation for that event changed after you opened this page, so nothing was published. Review the draft again."
	default:
		return a.accountPageError(c, &accounts.ValidationError{Field: "saved", Message: "unsupported drafts notice"})
	}
	if page.Drafts, err = a.drafts.List(c.Request().Context(), p.Account.ID); err != nil {
		return a.accountPageError(c, err)
	}
	// Each draft shows the reason it would replace; the form carries what
	// was shown, and publishing refuses if it changed meanwhile.
	page.Current = map[string]*recommendations.Item{}
	for _, d := range page.Drafts {
		if _, seen := page.Current[d.EventID]; seen {
			continue
		}
		item, err := a.recommendations.Get(c.Request().Context(), p.Account.ID, d.EventID)
		switch {
		case err == nil:
			page.Current[d.EventID] = &item
		case errors.Is(err, recommendations.ErrNotFound):
			page.Current[d.EventID] = nil
		default:
			return a.accountPageError(c, err)
		}
	}
	// Read after listing, so the badge matches the list.
	page.Navigation = a.accountNavigation(c, p.Account.ID)
	return render(c, 200, account.Drafts(page))
}

// draftDecisionPage publishes or discards one of the caller's drafts.
// Publishing consumes the draft in the same transaction as the
// recommendation, so a repeated or concurrent submission finds it gone (404)
// and a failure leaves it in place. The form carries the recommendation
// state the person saw; if it changed since, nothing is published and the
// page asks for a fresh review. It validates the reason and event again and
// attributes the recommendation to the app that wrote it. Agent
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
		reviewed := &recommendations.Current{Active: c.FormValue("has_current") == "1", ReasonHash: c.FormValue("current_hash")}
		if has := c.FormValue("has_current"); has != "0" && has != "1" {
			return a.accountPageError(c, &accounts.ValidationError{Field: "has_current", Message: "reload your drafts and review again"})
		}
		draft, err := a.drafts.Get(ctx, p.Account.ID, id)
		if err != nil {
			return a.accountPageError(c, err)
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
		if err = a.drafts.Discard(ctx, p.Account.ID, id); err != nil {
			return a.accountPageError(c, err)
		}
		return c.Redirect(303, "/me/drafts?saved=discarded")
	default:
		return a.accountPageError(c, &accounts.ValidationError{Field: "action", Message: "choose publish or discard"})
	}
}
