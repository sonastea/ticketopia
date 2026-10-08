package api

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/follows"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/moderation"
	"github.com/sonastea/ticketopia/internal/radar"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
	"github.com/sonastea/ticketopia/views/account"
	"github.com/sonastea/ticketopia/views/home"
)

var errCSRF = errors.New("refresh this page and try again")
var errDisabled = errors.New("accounts are not enabled on this server")

func privateAccountResponse(c echo.Context) {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	// Retain a same-origin form's Origin header while withholding cross-origin
	// referrers. no-referrer can make a legitimate browser POST send Origin:null.
	c.Response().Header().Set("Referrer-Policy", "same-origin")
	if strings.HasPrefix(c.Path(), "/auth/") {
		// Authorization codes must not appear in a subsequent page's Referer.
		c.Response().Header().Set("Referrer-Policy", "no-referrer")
	}
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
}
func (a *api) accountRoutes(e *echo.Echo) {
	e.GET("/auth/sign-in", a.signInPage)
	e.GET("/auth/google/start", a.googleStart)
	e.GET("/auth/google/callback", a.googleCallback)
	e.POST("/auth/logout", a.logoutPage(false))
	e.POST("/auth/logout-all", a.logoutPage(true))
	e.GET("/me", a.accountPage("profile"))
	e.GET("/me/preferences", a.accountPage("preferences"))
	e.GET("/me/interests", a.accountPage("interests"))
	e.POST("/me/profile", a.updateAccountPage(false))
	e.POST("/me/preferences", a.updateAccountPage(true))
	e.POST("/me/tokens", a.createTokenPage)
	e.POST("/me/tokens/:credential_id/revoke", a.revokeTokenPage)
	e.GET("/users/:user_id", a.publicProfilePage)
	e.GET("/api/v1/me", a.meHandler)
	e.GET("/api/v1/me/profile", a.profileHandler)
	e.PATCH("/api/v1/me/profile", a.patchProfileHandler)
	e.GET("/api/v1/me/preferences", a.preferencesHandler)
	e.PATCH("/api/v1/me/preferences", a.patchPreferencesHandler)
	e.GET("/api/v1/users/:user_id", a.publicProfileHandler)
	e.GET("/api/v1/me/tokens", a.tokensHandler)
	e.POST("/api/v1/me/tokens", a.createTokenHandler)
	e.DELETE("/api/v1/me/tokens/:credential_id", a.revokeTokenHandler)
	e.DELETE("/api/v1/me/session", a.deleteSessionHandler)
}
func (a *api) enabled() bool { return a.accounts != nil && a.authConfig.Enabled }
func (a *api) cookie(c echo.Context, kind, raw string, lifetime time.Duration) {
	age := int(lifetime.Seconds())
	expires := time.Now().Add(lifetime)
	if raw == "" {
		age = -1
		expires = time.Unix(1, 0)
	}
	c.SetCookie(&http.Cookie{Name: a.authConfig.CookieName(kind), Value: raw, Path: "/", MaxAge: age, Expires: expires, HttpOnly: true, Secure: a.authConfig.Secure(), SameSite: http.SameSiteLaxMode})
}

// Only a local, known page may be resumed. Never redirect to an auth endpoint,
// encoded slash/backslash, external origin, or user-controlled fragment.
func safeAuthReturn(raw string) string {
	if strings.HasPrefix(raw, "/radar") {
		return safeRadarReturn(raw)
	}
	if strings.HasPrefix(raw, "/follows") {
		return safeFollowReturn(raw)
	}
	if strings.HasPrefix(raw, "/moderation") || strings.HasPrefix(raw, "/posts/") || strings.HasPrefix(raw, "/me/reports") || strings.HasPrefix(raw, "/me/moderation") {
		return safeModerationReturn(raw)
	}
	if isDiscussionReturn(raw) {
		return safeDiscussionReturn(raw)
	}
	if strings.HasPrefix(raw, "/community") {
		return safeCommunityReturn(raw)
	}
	if strings.HasPrefix(raw, "/users/") {
		return safeProfileReturn(raw)
	}
	if len(raw) > 4096 || strings.ContainsAny(raw, "\\\r\n\x00") {
		return "/me"
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || u.Fragment != "" || u.RawPath != "" || !strings.HasPrefix(u.Path, "/") {
		return "/me"
	}
	if u.Path == "/" {
		values, err := url.ParseQuery(u.RawQuery)
		if err != nil {
			return "/me"
		}
		if _, _, _, err := webSearch(values); err != nil {
			return "/me"
		}
		return u.String()
	}
	if u.Path == "/me" || u.Path == "/me/preferences" || u.Path == "/me/interests" || u.Path == "/saved" {
		if u.Path == "/me/interests" && u.RawQuery != "" {
			return safeInterestReturn(raw)
		}
		if u.RawQuery != "" {
			return "/me"
		}
		return u.Path
	}
	if strings.HasPrefix(u.Path, "/events/ticketmaster:") && !strings.Contains(strings.TrimPrefix(u.Path, "/events/"), "/") {
		return u.String()
	}
	return "/me"
}
func (a *api) signInPage(c echo.Context) error {
	privateAccountResponse(c)
	message := ""
	if c.QueryParam("cancelled") == "true" {
		message = "Sign-in was cancelled. You can try again or keep discovering events."
	}
	return render(c, http.StatusOK, account.SignIn(a.enabled(), safeAuthReturn(c.QueryParam("return_to")), message))
}
func (a *api) googleStart(c echo.Context) error {
	privateAccountResponse(c)
	if !a.enabled() {
		return a.accountPageError(c, errDisabled)
	}
	if err := a.accounts.Allow(c.Request().Context(), "login", c.RealIP()); err != nil {
		return a.accountPageError(c, err)
	}
	l, target, err := a.accounts.Begin(c.Request().Context(), safeAuthReturn(c.QueryParam("return_to")))
	if err != nil {
		return a.accountPageError(c, err)
	}
	a.cookie(c, "flow", l.Browser, accounts.FlowLifetime)
	return c.Redirect(http.StatusSeeOther, target)
}
func (a *api) googleCallback(c echo.Context) error {
	privateAccountResponse(c)
	if !a.enabled() {
		return a.accountPageError(c, errDisabled)
	}
	if err := a.accounts.Allow(c.Request().Context(), "callback", c.RealIP()); err != nil {
		return a.accountPageError(c, err)
	}
	cookie, err := c.Cookie(a.authConfig.CookieName("flow"))
	if err != nil {
		return a.accountPageError(c, accounts.ErrFlow)
	}
	a.cookie(c, "flow", "", 0)
	for _, key := range []string{"state", "code", "error"} {
		if len(c.QueryParams()[key]) > 1 {
			return a.accountPageError(c, accounts.ErrFlow)
		}
	}
	if c.QueryParam("error") != "" {
		returnTo, err := a.accounts.Cancel(c.Request().Context(), c.QueryParam("state"), cookie.Value)
		if err != nil {
			return a.accountPageError(c, err)
		}
		return c.Redirect(http.StatusSeeOther, "/auth/sign-in?cancelled=true&return_to="+url.QueryEscape(safeAuthReturn(returnTo)))
	}
	_, raw, returnTo, err := a.accounts.Complete(c.Request().Context(), c.QueryParam("state"), cookie.Value, c.QueryParam("code"))
	if err != nil {
		return a.accountPageError(c, err)
	}
	// Replace any existing browser credential rather than leaving it active.
	if old, err := a.browserPrincipal(c); err == nil {
		_ = a.accounts.Revoke(c.Request().Context(), old.Account.ID, old.Credential.ID)
	}
	a.cookie(c, "session", raw, accounts.CredentialLifetime)
	return c.Redirect(http.StatusSeeOther, safeAuthReturn(returnTo))
}
func (a *api) browserPrincipal(c echo.Context) (accounts.Principal, error) {
	if !a.enabled() {
		return accounts.Principal{}, errDisabled
	}
	cookie, err := c.Cookie(a.authConfig.CookieName("session"))
	if err != nil {
		return accounts.Principal{}, accounts.ErrUnauthenticated
	}
	return a.accounts.Authenticate(c.Request().Context(), cookie.Value, "session")
}
func (a *api) principal(c echo.Context, browserOnly bool) (accounts.Principal, error) {
	privateAccountResponse(c)
	if !a.enabled() {
		return accounts.Principal{}, errDisabled
	}
	if h := c.Request().Header.Get("Authorization"); h != "" {
		if browserOnly || !strings.HasPrefix(h, "Bearer ") {
			return accounts.Principal{}, accounts.ErrUnauthenticated
		}
		return a.accounts.Authenticate(c.Request().Context(), strings.TrimPrefix(h, "Bearer "), "api")
	}
	return a.browserPrincipal(c)
}
func (a *api) checkWrite(c echo.Context, p accounts.Principal) error {
	if p.Credential.Kind == "api" {
		return nil
	}
	if c.Request().Header.Get("Sec-Fetch-Site") == "cross-site" {
		return errCSRF
	}
	if origin := c.Request().Header.Get("Origin"); origin != "" && origin != a.authConfig.BaseURL {
		return errCSRF
	}
	if ref := c.Request().Header.Get("Referer"); ref != "" {
		u, err := url.Parse(ref)
		if err != nil || u.Scheme+"://"+u.Host != a.authConfig.BaseURL {
			return errCSRF
		}
	}
	provided := c.Request().Header.Get("X-CSRF-Token")
	if provided == "" && !strings.HasPrefix(c.Path(), "/api/") {
		provided = c.FormValue("csrf_token")
	}
	if subtle.ConstantTimeCompare([]byte(provided), []byte(p.CSRF)) != 1 {
		return errCSRF
	}
	return nil
}
func (a *api) requireAPI(c echo.Context, write, browserOnly bool) (accounts.Principal, error) {
	if len(c.QueryParams()) > 0 {
		return accounts.Principal{}, &accounts.ValidationError{Field: "query", Message: "this route does not accept query parameters"}
	}
	p, err := a.principal(c, browserOnly)
	if err != nil {
		return p, err
	}
	if write {
		err = a.checkWrite(c, p)
	}
	return p, err
}
func accountError(err error) (int, string, string, map[string]string) {
	var invalid *accounts.ValidationError
	switch {
	case errors.As(err, &invalid):
		return 400, "invalid_input", invalid.Error(), map[string]string{invalid.Field: invalid.Message}
	case errors.Is(err, accounts.ErrUnauthenticated):
		return 401, "unauthenticated", "Sign in to continue.", nil
	case errors.Is(err, errCSRF):
		return 403, "csrf_failed", "Refresh this page and try again.", nil
	case errors.Is(err, accounts.ErrFlow):
		return 400, "invalid_sign_in", "Sign-in expired or could not be verified. Please start again.", nil
	case errors.Is(err, accounts.ErrNotFound):
		return 404, "account_not_found", "This profile could not be found.", nil
	case errors.Is(err, follows.ErrNotFound):
		return 404, "follow_not_found", "This artist, venue or follow couldn't be found. Search again or return to your follows.", nil
	case errors.Is(err, follows.ErrUnavailable):
		return 503, "follows_unavailable", "Your follows couldn't load or change. Please try again shortly.", nil
	case errors.Is(err, radar.ErrChanged):
		return 409, "radar_changed", "Your radar changed while you were browsing. Refresh Radar to see the current matches.", nil
	case errors.Is(err, radar.ErrUnavailable):
		return 503, "radar_unavailable", "Your radar couldn't load. Please try again shortly; your preferences and follows are unchanged.", nil
	case errors.Is(err, discovery.ErrNotFound), errors.Is(err, saved.ErrNotFound), errors.Is(err, interests.ErrNotFound), errors.Is(err, recommendations.ErrNotFound):
		return 404, "event_not_found", "This event could not be found. Return to discovery and try another event.", nil
	case errors.Is(err, saved.ErrUnavailable):
		return 503, "saved_events_unavailable", "We couldn't load or change your saved events. Please try again shortly.", nil
	case errors.Is(err, interests.ErrUnavailable):
		return 503, "event_interests_unavailable", "We couldn't load or change event interest. Please try again shortly.", nil
	case errors.Is(err, recommendations.ErrUnavailable):
		return 503, "event_recommendations_unavailable", "We couldn't load or change recommendations. Please try again shortly.", nil
	case errors.Is(err, discussions.ErrNotFound):
		return 404, "post_not_found", "This contribution couldn't be found or changed.", nil
	case errors.Is(err, discussions.ErrConflict):
		return 409, "idempotency_conflict", "This retry key was used for a different contribution. Use a new key for a new post.", nil
	case errors.Is(err, discussions.ErrUnavailable):
		return 503, "discussions_unavailable", "Discussions couldn't load or change. Please try again shortly.", nil
	case errors.Is(err, moderation.ErrForbidden):
		return 403, "moderator_access_required", "Moderator access is required. Another moderator must handle cases about your own contributions; check your moderation outcomes instead.", nil
	case errors.Is(err, moderation.ErrNotFound):
		return 404, "moderation_record_not_found", "This report or contribution couldn't be found or changed.", nil
	case errors.Is(err, moderation.ErrConflict):
		return 409, "review_changed", "This review or contribution changed. Reload and review the current context before trying again.", nil
	case errors.Is(err, moderation.ErrUnavailable):
		return 503, "moderation_unavailable", "Reports and moderation couldn't load or change. Please try again shortly.", nil
	case errors.Is(err, accounts.ErrLimited):
		return 429, "rate_limited", "Too many attempts. Try again in ten minutes.", nil
	case errors.Is(err, accounts.ErrCredentialLimit):
		return 409, "credential_limit", "Revoke an existing API token before creating another.", nil
	case errors.Is(err, errDisabled):
		return 503, "accounts_disabled", "Accounts are not enabled on this server. Discovery is still available.", nil
	default:
		return 503, "accounts_unavailable", "We couldn't complete this account request. Please try again shortly.", nil
	}
}
func accountProblem(c echo.Context, err error) error {
	privateAccountResponse(c)
	status, code, detail, fields := accountError(err)
	if status == 429 {
		c.Response().Header().Set("Retry-After", "600")
	}
	if status == 401 {
		c.Response().Header().Set("WWW-Authenticate", `Bearer realm="ticketopia"`)
	}
	c.Response().Header().Set("Content-Type", "application/problem+json")
	return c.JSON(status, struct {
		Type   string            `json:"type"`
		Title  string            `json:"title"`
		Status int               `json:"status"`
		Code   string            `json:"code"`
		Detail string            `json:"detail"`
		Fields map[string]string `json:"fields,omitempty"`
	}{"about:blank", http.StatusText(status), status, code, detail, fields})
}
func (a *api) accountPageError(c echo.Context, err error) error {
	privateAccountResponse(c)
	status, _, detail, _ := accountError(err)
	if status == 429 {
		c.Response().Header().Set("Retry-After", "600")
	}
	return render(c, status, account.Failure(detail))
}
func decodeAccountJSON(c echo.Context, target any) error {
	media, _, err := mime.ParseMediaType(c.Request().Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return &accounts.ValidationError{Field: "body", Message: "send application/json"}
	}
	reader := http.MaxBytesReader(c.Response(), c.Request().Body, 16384)
	data, err := io.ReadAll(reader)
	if err != nil {
		return &accounts.ValidationError{Field: "body", Message: "use a JSON body no larger than 16 KiB"}
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(data, &object) != nil || object == nil {
		return &accounts.ValidationError{Field: "body", Message: "send a JSON object"}
	}
	for key, value := range object {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return &accounts.ValidationError{Field: key, Message: "null is not a supported value"}
		}
	}
	keys := make(map[string]bool)
	unique := json.NewDecoder(bytes.NewReader(data))
	_, _ = unique.Token()
	for unique.More() {
		key, _ := unique.Token()
		name, _ := key.(string)
		if keys[name] {
			return &accounts.ValidationError{Field: name, Message: "use each field only once"}
		}
		keys[name] = true
		var value json.RawMessage
		if err := unique.Decode(&value); err != nil {
			return &accounts.ValidationError{Field: "body", Message: "send a JSON object"}
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if decoder.Decode(target) != nil {
		return &accounts.ValidationError{Field: "body", Message: "use only supported fields and value types"}
	}
	return nil
}

type ownProfile struct {
	ID                 string `json:"id"`
	DisplayName        string `json:"display_name"`
	Bio                string `json:"bio"`
	InterestVisibility string `json:"interest_visibility"`
}

func profileOf(a accounts.Account) ownProfile {
	return ownProfile{a.ID, a.DisplayName, a.Bio, a.InterestVisibility}
}
func (a *api) meHandler(c echo.Context) error {
	p, err := a.requireAPI(c, false, false)
	if err != nil {
		return accountProblem(c, err)
	}
	csrf := ""
	if p.Credential.Kind == "session" {
		csrf = p.CSRF
	}
	return c.JSON(200, struct {
		Item accounts.Account `json:"item"`
		CSRF string           `json:"csrf_token,omitempty"`
	}{p.Account, csrf})
}
func (a *api) profileHandler(c echo.Context) error {
	p, err := a.requireAPI(c, false, false)
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, profileOf(p.Account))
}
func (a *api) preferencesHandler(c echo.Context) error {
	p, err := a.requireAPI(c, false, false)
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, p.Account.Preferences)
}
func (a *api) patchProfileHandler(c echo.Context) error {
	p, err := a.requireAPI(c, true, false)
	if err != nil {
		return accountProblem(c, err)
	}
	var patch accounts.ProfilePatch
	if err = decodeAccountJSON(c, &patch); err != nil {
		return accountProblem(c, err)
	}
	updated, err := a.accounts.UpdateProfile(c.Request().Context(), p.Account.ID, patch)
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, profileOf(updated))
}
func (a *api) patchPreferencesHandler(c echo.Context) error {
	p, err := a.requireAPI(c, true, false)
	if err != nil {
		return accountProblem(c, err)
	}
	var patch accounts.PreferencesPatch
	if err = decodeAccountJSON(c, &patch); err != nil {
		return accountProblem(c, err)
	}
	updated, err := a.accounts.UpdatePreferences(c.Request().Context(), p.Account.ID, patch)
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, updated.Preferences)
}
func (a *api) publicProfileHandler(c echo.Context) error {
	privateAccountResponse(c)
	if !a.enabled() {
		return accountProblem(c, errDisabled)
	}
	if len(c.QueryParams()) > 0 {
		return accountProblem(c, &accounts.ValidationError{Field: "query", Message: "this route does not accept query parameters"})
	}
	p, err := a.accounts.Public(c.Request().Context(), c.Param("user_id"))
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, p)
}
func (a *api) tokensHandler(c echo.Context) error {
	p, err := a.requireAPI(c, false, true)
	if err != nil {
		return accountProblem(c, err)
	}
	items, err := a.accounts.Tokens(c.Request().Context(), p.Account.ID)
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(200, struct {
		Items []accounts.Credential `json:"items"`
	}{items})
}
func (a *api) createTokenHandler(c echo.Context) error {
	p, err := a.requireAPI(c, true, true)
	if err != nil {
		return accountProblem(c, err)
	}
	if err = a.accounts.Allow(c.Request().Context(), "tokens", p.Account.ID); err != nil {
		return accountProblem(c, err)
	}
	var body struct {
		Name string `json:"name"`
	}
	if err = decodeAccountJSON(c, &body); err != nil {
		return accountProblem(c, err)
	}
	item, raw, err := a.accounts.CreateToken(c.Request().Context(), p.Account.ID, body.Name)
	if err != nil {
		return accountProblem(c, err)
	}
	c.Response().Header().Set("Location", "/api/v1/me/tokens/"+item.ID)
	return c.JSON(201, struct {
		Item  accounts.Credential `json:"item"`
		Token string              `json:"token"`
	}{item, raw})
}
func (a *api) revokeTokenHandler(c echo.Context) error {
	p, err := a.requireAPI(c, true, true)
	if err != nil {
		return accountProblem(c, err)
	}
	// Credentials are owner-scoped; another account's ID is an idempotent no-op.
	if err = a.accounts.Revoke(c.Request().Context(), p.Account.ID, c.Param("credential_id")); err != nil {
		return accountProblem(c, err)
	}
	return c.NoContent(204)
}
func (a *api) deleteSessionHandler(c echo.Context) error {
	p, err := a.requireAPI(c, true, false)
	if err != nil {
		return accountProblem(c, err)
	}
	if err = a.accounts.Revoke(c.Request().Context(), p.Account.ID, p.Credential.ID); err != nil {
		return accountProblem(c, err)
	}
	if p.Credential.Kind == "session" {
		a.cookie(c, "session", "", 0)
	}
	return c.NoContent(204)
}
func (a *api) accountPage(section string) echo.HandlerFunc {
	return func(c echo.Context) error {
		privateAccountResponse(c)
		if !a.enabled() {
			active := "profile"
			title := "Your profile"
			if section == "interests" {
				active = "interests"
				title = "Your interests"
			}
			return render(c, 200, home.Destination(active, title))
		}
		p, err := a.principal(c, true)
		if errors.Is(err, accounts.ErrUnauthenticated) {
			return c.Redirect(303, "/auth/sign-in?return_to="+url.QueryEscape(c.Request().URL.RequestURI()))
		}
		if err != nil {
			return a.accountPageError(c, err)
		}
		notice := ""
		switch c.QueryParam("saved") {
		case "profile":
			notice = "Your profile and privacy settings were saved."
		case "preferences":
			notice = "Your private preferences were saved."
		case "token":
			notice = "API token revoked."
		}
		return a.renderAccount(c, 200, p, section, "", notice, "")
	}
}
func (a *api) renderAccount(c echo.Context, status int, p accounts.Principal, section, message, notice, token string) error {
	page := account.Page{Account: p.Account, CSRF: p.CSRF, Section: section, Error: message, Notice: notice, NewToken: token}
	page.ModerationEnabled = a.moderationEnabled()
	if page.ModerationEnabled {
		var err error
		page.Moderator, err = a.moderation.IsModerator(c.Request().Context(), p.Account.ID)
		if err != nil {
			return a.accountPageError(c, err)
		}
	}
	if section == "profile" {
		var err error
		page.Tokens, err = a.accounts.Tokens(c.Request().Context(), p.Account.ID)
		if err != nil {
			return a.accountPageError(c, err)
		}
	}
	if section == "interests" {
		values := c.QueryParams()
		values.Del("saved")
		page.EventInterests.ReturnURL = c.Request().URL.RequestURI()
		var err error = interests.ErrUnavailable
		if a.interests != nil {
			page.EventInterests.List, err = a.interests.List(c.Request().Context(), p.Account.ID, false, values)
		}
		if err != nil {
			if status < 400 {
				status, _, page.EventInterests.Error, _ = accountError(err)
			} else {
				_, _, page.EventInterests.Error, _ = accountError(err)
			}
		}
		ids := []string{}
		for _, item := range page.EventInterests.List.Items {
			ids = append(ids, item.Event.ID)
		}
		page.EventInterests.Saves = a.saveView(c, ids)
		page.EventInterests.Interests = a.interestView(c, ids)
		if a.events != nil {
			catalog, err := a.events.Categories(c.Request().Context())
			page.Categories = catalog.Items
			page.CategoriesUnavailable = err != nil
		} else {
			page.CategoriesUnavailable = true
		}
	}
	return render(c, status, account.Settings(page))
}
func parseAccountForm(c echo.Context, allowed ...string) error {
	return parseBoundedAccountForm(c, 16384, allowed...)
}
func parseBoundedAccountForm(c echo.Context, maxBytes int64, allowed ...string) error {
	// Form values (including CSRF) must come from the bounded POST body, never
	// a query string that can leak into navigation history or access logs.
	if len(c.QueryParams()) > 0 {
		return &accounts.ValidationError{Field: "query", Message: "submit form fields in the request body"}
	}
	media, _, err := mime.ParseMediaType(c.Request().Header.Get("Content-Type"))
	if err != nil || media != "application/x-www-form-urlencoded" {
		return &accounts.ValidationError{Field: "body", Message: "submit an ordinary form"}
	}
	c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, maxBytes)
	if err = c.Request().ParseForm(); err != nil {
		message := "use a form no larger than 16 KiB"
		if maxBytes > 16384 {
			message = "use a form no larger than 64 KiB"
		}
		return &accounts.ValidationError{Field: "body", Message: message}
	}
	for key, values := range c.Request().PostForm {
		if key == "csrf_token" {
			if len(values) != 1 {
				return errCSRF
			}
			continue
		}
		found := false
		for _, v := range allowed {
			if key == v {
				found = true
			}
		}
		if !found || (key != "category_ids" && len(values) != 1) {
			return &accounts.ValidationError{Field: key, Message: "unsupported or repeated form field"}
		}
	}
	return nil
}
func (a *api) updateAccountPage(preferences bool) echo.HandlerFunc {
	return func(c echo.Context) error {
		privateAccountResponse(c)
		allowed := []string{"display_name", "bio", "interest_visibility"}
		if preferences {
			allowed = []string{"city", "country", "timezone", "category_ids", "email_reminders", "weekly_digest", "notifications_paused", "section"}
		}
		if err := parseAccountForm(c, allowed...); err != nil {
			return a.accountPageError(c, err)
		}
		p, err := a.principal(c, true)
		if err != nil {
			return a.accountPageError(c, err)
		}
		if err = a.checkWrite(c, p); err != nil {
			return a.accountPageError(c, err)
		}
		section := "profile"
		destination := "/me?saved=profile"
		if !preferences {
			name, bio, visibility := c.FormValue("display_name"), c.FormValue("bio"), c.FormValue("interest_visibility")
			_, err = a.accounts.UpdateProfile(c.Request().Context(), p.Account.ID, accounts.ProfilePatch{DisplayName: &name, Bio: &bio, InterestVisibility: &visibility})
			p.Account.DisplayName, p.Account.Bio, p.Account.InterestVisibility = name, bio, visibility
		} else {
			section = "preferences"
			destination = "/me/preferences?saved=preferences"
			var patch accounts.PreferencesPatch
			if c.FormValue("section") == "interests" {
				section = "interests"
				destination = "/me/interests?saved=preferences"
				ids := c.Request().PostForm["category_ids"]
				if ids == nil {
					ids = []string{}
				}
				patch.CategoryIDs = &ids
				p.Account.Preferences.CategoryIDs = ids
			} else {
				city, country, zone := c.FormValue("city"), c.FormValue("country"), c.FormValue("timezone")
				email, digest, paused := c.FormValue("email_reminders") == "on", c.FormValue("weekly_digest") == "on", c.FormValue("notifications_paused") == "on"
				patch = accounts.PreferencesPatch{City: &city, Country: &country, Timezone: &zone, EmailReminders: &email, WeeklyDigest: &digest, NotificationsPaused: &paused}
				p.Account.Preferences.City, p.Account.Preferences.Country, p.Account.Preferences.Timezone = city, country, zone
				p.Account.Preferences.EmailReminders, p.Account.Preferences.WeeklyDigest, p.Account.Preferences.NotificationsPaused = email, digest, paused
			}
			_, err = a.accounts.UpdatePreferences(c.Request().Context(), p.Account.ID, patch)
		}
		if err != nil {
			status, _, message, _ := accountError(err)
			return a.renderAccount(c, status, p, section, message, "", "")
		}
		return c.Redirect(303, destination)
	}
}
func (a *api) logoutPage(all bool) echo.HandlerFunc {
	return func(c echo.Context) error {
		privateAccountResponse(c)
		if err := parseAccountForm(c); err != nil {
			return a.accountPageError(c, err)
		}
		p, err := a.principal(c, true)
		if err != nil {
			return a.accountPageError(c, err)
		}
		if err = a.checkWrite(c, p); err != nil {
			return a.accountPageError(c, err)
		}
		if all {
			err = a.accounts.RevokeAll(c.Request().Context(), p.Account.ID)
		} else {
			err = a.accounts.Revoke(c.Request().Context(), p.Account.ID, p.Credential.ID)
		}
		if err != nil {
			return a.accountPageError(c, err)
		}
		a.cookie(c, "session", "", 0)
		return c.Redirect(303, "/")
	}
}
func (a *api) createTokenPage(c echo.Context) error {
	privateAccountResponse(c)
	if err := parseAccountForm(c, "name"); err != nil {
		return a.accountPageError(c, err)
	}
	p, err := a.principal(c, true)
	if err != nil {
		return a.accountPageError(c, err)
	}
	if err = a.checkWrite(c, p); err != nil {
		return a.accountPageError(c, err)
	}
	if err = a.accounts.Allow(c.Request().Context(), "tokens", p.Account.ID); err != nil {
		return a.accountPageError(c, err)
	}
	_, raw, err := a.accounts.CreateToken(c.Request().Context(), p.Account.ID, c.FormValue("name"))
	if err != nil {
		status, _, message, _ := accountError(err)
		return a.renderAccount(c, status, p, "profile", message, "", "")
	}
	return a.renderAccount(c, 201, p, "profile", "", "API token created. Copy it now; it won't be shown again.", raw)
}
func (a *api) revokeTokenPage(c echo.Context) error {
	privateAccountResponse(c)
	if err := parseAccountForm(c); err != nil {
		return a.accountPageError(c, err)
	}
	p, err := a.principal(c, true)
	if err != nil {
		return a.accountPageError(c, err)
	}
	if err = a.checkWrite(c, p); err != nil {
		return a.accountPageError(c, err)
	}
	if err = a.accounts.Revoke(c.Request().Context(), p.Account.ID, c.Param("credential_id")); err != nil {
		return a.accountPageError(c, err)
	}
	return c.Redirect(303, "/me?saved=token")
}
func (a *api) publicProfilePage(c echo.Context) error {
	privateAccountResponse(c)
	if !a.enabled() {
		return a.accountPageError(c, errDisabled)
	}
	p, err := a.accounts.Public(c.Request().Context(), c.Param("user_id"))
	if err != nil {
		return a.accountPageError(c, err)
	}
	list := interests.List{Items: []interests.Item{}}
	values := c.QueryParams()
	recommendationValues := url.Values{}
	if raw, ok := values["recommendation_cursor"]; ok {
		recommendationValues["cursor"] = raw
		values.Del("recommendation_cursor")
	}
	if raw, ok := values["limit"]; ok {
		recommendationValues["limit"] = raw
	}
	recommended := recommendations.List{Items: []recommendations.Item{}}
	recommendationError := ""
	if a.recommendations != nil {
		recommended, err = a.recommendations.Public(c.Request().Context(), p.ID, recommendationValues)
		if err != nil {
			return a.accountPageError(c, err)
		}
	} else {
		recommendationError = "Public recommendations are unavailable on this server."
	}
	message := ""
	if a.interests != nil {
		list, err = a.interests.List(c.Request().Context(), p.ID, true, values)
		if err != nil {
			return a.accountPageError(c, err)
		}
	} else {
		message = "Public event interest is unavailable on this server."
	}
	return render(c, 200, account.Public(p, list, c.Request().URL.RequestURI(), message, recommended, recommendationError))
}
