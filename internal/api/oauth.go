package api

import (
	"errors"
	"math"
	"mime"
	"net/http"
	"net/url"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/oauth"
	"github.com/sonastea/ticketopia/views/account"
)

// authorizeFields are the authorization request parameters the consent form
// carries back; everything is validated again when the person decides.
var authorizeFields = []string{"client_id", "redirect_uri", "response_type", "state", "scope", "code_challenge", "code_challenge_method", "resource"}

// oauthRoutes serve the authorization server for agent sign-in to /mcp.
func (a *api) oauthRoutes(e *echo.Echo, tokenLimiter *ipLimiter) {
	e.GET(oauth.ServerMetadataPath, a.oauthMetadata(func() any { return a.oauth.ServerMetadata() }))
	e.GET(oauth.ResourceMetadataPath, a.oauthMetadata(func() any { return a.oauth.ResourceMetadata() }))
	e.GET(oauth.ResourceMetadataInsertion, a.oauthMetadata(func() any { return a.oauth.ResourceMetadata() }))
	for _, path := range []string{oauth.ServerMetadataPath, oauth.ResourceMetadataPath, oauth.ResourceMetadataInsertion, oauth.TokenPath, oauth.RevocationPath} {
		e.OPTIONS(path, oauthPreflight)
	}
	e.GET(oauth.AuthorizePath, a.oauthAuthorize)
	e.POST(oauth.AuthorizePath, a.oauthDecide)
	// Token and revocation requests share one per-IP allowance.
	e.POST(oauth.TokenPath, a.oauthClientEndpoint(tokenLimiter, "token", a.oauthToken))
	e.POST(oauth.RevocationPath, a.oauthClientEndpoint(tokenLimiter, "revocation", a.oauthRevoke))
	e.POST("/me/connections/:grant_id/revoke", a.revokeConnectionPage)
}

// Metadata and the token and revocation endpoints carry no cookies or ambient authority, so
// browser-based clients may call them from any origin.
func oauthCORS(c echo.Context) {
	h := c.Response().Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	h.Set("Access-Control-Allow-Headers", "Content-Type, MCP-Protocol-Version")
}

func oauthPreflight(c echo.Context) error {
	oauthCORS(c)
	return c.NoContent(http.StatusNoContent)
}

func (a *api) oauthMetadata(document func() any) echo.HandlerFunc {
	return func(c echo.Context) error {
		oauthCORS(c)
		c.Response().Header().Set("Cache-Control", "public, max-age=300")
		return c.JSON(http.StatusOK, document())
	}
}

// consentHeaders keep the consent page out of frames, so another site cannot
// overlay it and trick a click on Allow.
func consentHeaders(c echo.Context) {
	privateAccountResponse(c)
	c.Response().Header().Set("Content-Security-Policy", "frame-ancestors 'none'")
	c.Response().Header().Set("X-Frame-Options", "DENY")
}

// authorizationFailure answers an invalid request: in the browser when the
// client cannot be trusted with a redirect, otherwise back to the client.
func (a *api) authorizationFailure(c echo.Context, r oauth.Request, err error) error {
	var clientErr *oauth.ClientError
	if errors.As(err, &clientErr) {
		return render(c, http.StatusBadRequest, account.ConnectFailure(clientErr.Message))
	}
	var oauthErr *oauth.Error
	if errors.As(err, &oauthErr) {
		return c.Redirect(http.StatusSeeOther, a.oauth.ErrorRedirect(r, oauthErr))
	}
	return a.accountPageError(c, err)
}

func (a *api) oauthAuthorize(c echo.Context) error {
	consentHeaders(c)
	if err := a.accounts.Allow(c.Request().Context(), "oauth-authorize", c.RealIP()); err != nil {
		return a.accountPageError(c, err)
	}
	// The request must fit the sign-in return URL if the person signs in first.
	if len(c.Request().URL.RequestURI()) > 4000 {
		return render(c, http.StatusBadRequest, account.ConnectFailure("The app's sign-in request is too long. Return to the app and try connecting again."))
	}
	r, err := a.oauth.ParseAuthorization(c.Request().Context(), c.QueryParams())
	if err != nil {
		return a.authorizationFailure(c, r, err)
	}
	p, err := a.browserPrincipal(c)
	if errors.Is(err, accounts.ErrUnauthenticated) {
		return c.Redirect(http.StatusSeeOther, "/auth/sign-in?return_to="+url.QueryEscape(c.Request().URL.RequestURI()))
	}
	if err != nil {
		return a.accountPageError(c, err)
	}
	return a.renderConsent(c, p, r)
}

func (a *api) renderConsent(c echo.Context, p accounts.Principal, r oauth.Request) error {
	page := account.ConsentPage{ClientName: r.Client.Name, ClientHost: r.Client.Host(), RedirectHost: r.RedirectHost(),
		LocalRedirect: r.LocalRedirect(), Write: r.Write(), Email: p.Account.Email, CSRF: p.CSRF, Action: oauth.AuthorizePath}
	for _, key := range authorizeFields {
		if v := c.QueryParam(key); v != "" {
			page.Fields = append(page.Fields, account.HiddenField{Name: key, Value: v})
		}
	}
	return render(c, http.StatusOK, account.Consent(page))
}

func (a *api) oauthDecide(c echo.Context) error {
	consentHeaders(c)
	if err := parseAccountForm(c, append([]string{"decision", "write"}, authorizeFields...)...); err != nil {
		return a.accountPageError(c, err)
	}
	p, err := a.principal(c, true)
	if err != nil {
		return a.accountPageError(c, err)
	}
	if err = a.checkWrite(c, p); err != nil {
		return a.accountPageError(c, err)
	}
	if err = a.accounts.Allow(c.Request().Context(), "oauth-authorize", c.RealIP()); err != nil {
		return a.accountPageError(c, err)
	}
	values := url.Values{}
	for _, key := range authorizeFields {
		if v, ok := c.Request().PostForm[key]; ok {
			values[key] = v
		}
	}
	r, err := a.oauth.ParseAuthorization(c.Request().Context(), values)
	if err != nil {
		return a.authorizationFailure(c, r, err)
	}
	if c.FormValue("decision") != "allow" {
		return c.Redirect(http.StatusSeeOther, a.oauth.ErrorRedirect(r, &oauth.Error{Code: "access_denied", Description: "the person declined to connect"}))
	}
	scopes := []string{oauth.ScopeRead}
	if c.FormValue("write") == "on" {
		scopes = append(scopes, oauth.ScopeWrite)
	}
	redirect, err := a.oauth.Approve(c.Request().Context(), p.Account.ID, r, scopes)
	if err != nil {
		return a.accountPageError(c, err)
	}
	return c.Redirect(http.StatusSeeOther, redirect)
}

func oauthProblem(c echo.Context, status int, code, description string) error {
	return c.JSON(status, map[string]string{"error": code, "error_description": description})
}

// oauthClientEndpoint applies the rules the token and revocation endpoints
// share (per-IP limit, public clients, form bodies) and maps handle's errors
// to OAuth error responses; handle writes the success response.
func (a *api) oauthClientEndpoint(limiter *ipLimiter, name string, handle func(echo.Context, url.Values) error) echo.HandlerFunc {
	return func(c echo.Context) error {
		oauthCORS(c)
		c.Response().Header().Set("Cache-Control", "no-store")
		c.Response().Header().Set("Pragma", "no-cache")
		if ok, wait := limiter.allow(c.RealIP()); !ok {
			c.Response().Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(wait.Seconds())))))
			return oauthProblem(c, http.StatusTooManyRequests, "temporarily_unavailable", "too many "+name+" requests; retry after the Retry-After delay")
		}
		// Only public clients: a secret would mean the client is not the one
		// its metadata document describes.
		if c.Request().Header.Get("Authorization") != "" {
			return oauthProblem(c, http.StatusUnauthorized, "invalid_client", "only public clients without client authentication are supported")
		}
		media, _, err := mime.ParseMediaType(c.Request().Header.Get("Content-Type"))
		if err != nil || media != "application/x-www-form-urlencoded" {
			return oauthProblem(c, http.StatusBadRequest, "invalid_request", "send an application/x-www-form-urlencoded body")
		}
		c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, 16384)
		if err := c.Request().ParseForm(); err != nil {
			return oauthProblem(c, http.StatusBadRequest, "invalid_request", "the request body is invalid or too large")
		}
		err = handle(c, c.Request().PostForm)
		var oauthErr *oauth.Error
		if errors.As(err, &oauthErr) {
			status := http.StatusBadRequest
			if oauthErr.Code == "invalid_client" {
				status = http.StatusUnauthorized
			}
			return oauthProblem(c, status, oauthErr.Code, oauthErr.Description)
		}
		if err != nil {
			a.logger.Warn().Err(err).Msg("OAuth " + name + " request failed")
			c.Response().Header().Set("Retry-After", "30")
			return oauthProblem(c, http.StatusServiceUnavailable, "temporarily_unavailable", "accounts are unavailable; retry shortly")
		}
		return nil
	}
}

func (a *api) oauthToken(c echo.Context, form url.Values) error {
	token, err := a.oauth.Exchange(c.Request().Context(), form)
	if err != nil {
		return err
	}
	return c.JSON(http.StatusOK, token)
}

// oauthRevoke is the RFC 7009 revocation endpoint: 200 with an empty body
// whether or not the token was live, 503 with Retry-After when storage fails.
// Each answered request is logged with its outcome and the host of the
// claimed client_id (caller-supplied, so capped), never the token or account.
func (a *api) oauthRevoke(c echo.Context, form url.Values) error {
	outcome, err := a.oauth.RevokeToken(c.Request().Context(), form)
	if err != nil {
		return err
	}
	host := oauth.Client{ID: form.Get("client_id")}.Host()
	if len(host) > 100 {
		host = host[:100]
	}
	a.logger.Info().Str("outcome", string(outcome)).Str("client_host", host).Msg("OAuth token revocation")
	return c.NoContent(http.StatusOK)
}

func (a *api) revokeConnectionPage(c echo.Context) error {
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
	// Revoking an unknown or already removed connection leaves nothing to undo.
	if err = a.oauth.Revoke(c.Request().Context(), p.Account.ID, c.Param("grant_id")); err != nil && !errors.Is(err, oauth.ErrNotFound) {
		return a.accountPageError(c, err)
	}
	return c.Redirect(http.StatusSeeOther, "/me?saved=connection")
}
