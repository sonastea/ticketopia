package oauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/oauth"
	"github.com/sonastea/ticketopia/internal/oauth/oauthtest"
)

const (
	clientID = "https://agent.example/oauth/client.json"
	issuer   = "https://ticketopia.example"
	verifier = "a-verifier-that-is-long-enough-for-pkce-0123456789"
)

func challenge(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type fixture struct {
	service *oauth.Service
	repo    *oauthtest.Repository
	now     time.Time
}

func newFixture(t *testing.T, refresh bool) *fixture {
	t.Helper()
	f := &fixture{repo: oauthtest.NewRepository(), now: time.Date(2026, 11, 12, 12, 0, 0, 0, time.UTC)}
	clients := oauthtest.Clients{clientID: {ID: clientID, Name: "Agent", RedirectURIs: []string{"http://127.0.0.1/callback", "https://agent.example/callback"}, Refresh: refresh}}
	f.service = oauth.New(f.repo, clients, issuer+"/")
	oauth.SetNow(f.service, func() time.Time { return f.now })
	return f
}

func authorizeValues(overrides map[string]string) url.Values {
	v := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {"http://127.0.0.1:53682/callback"},
		"code_challenge": {challenge(verifier)}, "code_challenge_method": {"S256"}, "state": {"xyz"}, "scope": {"read write"},
		"resource": {issuer + "/mcp"}}
	for key, value := range overrides {
		if value == "" {
			v.Del(key)
		} else {
			v.Set(key, value)
		}
	}
	return v
}

// approve runs an authorization request through consent and returns the code.
func (f *fixture) approve(t *testing.T, scopes ...string) string {
	t.Helper()
	r, err := f.service.ParseAuthorization(t.Context(), authorizeValues(nil))
	if err != nil {
		t.Fatal(err)
	}
	redirect, err := f.service.Approve(t.Context(), "account-1", r, scopes)
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(redirect)
	if u.Host != "127.0.0.1:53682" || u.Query().Get("state") != "xyz" || u.Query().Get("iss") != issuer {
		t.Fatalf("redirect %s", redirect)
	}
	return u.Query().Get("code")
}

func (f *fixture) exchange(t *testing.T, form url.Values) (oauth.Token, error) {
	t.Helper()
	return f.service.Exchange(t.Context(), form)
}

func codeForm(code string) url.Values {
	return url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID},
		"redirect_uri": {"http://127.0.0.1:53682/callback"}, "code_verifier": {verifier}, "resource": {issuer + "/mcp"}}
}

func oauthCode(err error) string {
	var e *oauth.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// Requests that can't be trusted with a redirect are shown in the browser;
// the rest go back to the client as OAuth errors.
func TestParseAuthorization(t *testing.T) {
	f := newFixture(t, true)
	for name, overrides := range map[string]map[string]string{
		"unknown client":       {"client_id": "https://other.example/client.json"},
		"missing client":       {"client_id": ""},
		"unregistered return":  {"redirect_uri": "https://evil.example/callback"},
		"missing return":       {"redirect_uri": ""},
		"loopback other path":  {"redirect_uri": "http://127.0.0.1:9/other"},
		"loopback other host":  {"redirect_uri": "http://localhost:9/callback"},
		"https with port":      {"redirect_uri": "https://agent.example:8443/callback"},
		"plain http elsewhere": {"redirect_uri": "http://agent.example/callback"},
	} {
		_, err := f.service.ParseAuthorization(t.Context(), authorizeValues(overrides))
		var ce *oauth.ClientError
		if !errors.As(err, &ce) {
			t.Errorf("%s: %v, want a browser error", name, err)
		}
	}
	repeated := authorizeValues(nil)
	repeated.Add("redirect_uri", "http://127.0.0.1:1/callback")
	if _, err := f.service.ParseAuthorization(t.Context(), repeated); err == nil || oauthCode(err) != "" {
		t.Errorf("repeated redirect_uri: %v", err)
	}
	for name, c := range map[string]struct {
		overrides map[string]string
		code      string
	}{
		"implicit flow":   {map[string]string{"response_type": "token"}, "unsupported_response_type"},
		"plain PKCE":      {map[string]string{"code_challenge_method": "plain"}, "invalid_request"},
		"no PKCE":         {map[string]string{"code_challenge": "", "code_challenge_method": ""}, "invalid_request"},
		"short challenge": {map[string]string{"code_challenge": "abc"}, "invalid_request"},
		"other resource":  {map[string]string{"resource": "https://elsewhere.example/mcp"}, "invalid_target"},
		"unknown scope":   {map[string]string{"scope": "read admin"}, "invalid_scope"},
	} {
		r, err := f.service.ParseAuthorization(t.Context(), authorizeValues(c.overrides))
		if oauthCode(err) != c.code {
			t.Errorf("%s: %v, want %s", name, err, c.code)
			continue
		}
		var e *oauth.Error
		errors.As(err, &e)
		u, _ := url.Parse(f.service.ErrorRedirect(r, e))
		if u.Host != "127.0.0.1:53682" || u.Query().Get("error") != c.code || u.Query().Get("state") != "xyz" || u.Query().Get("iss") != issuer {
			t.Errorf("%s redirect: %s", name, u)
		}
	}
	// Omitted scope offers both, and the resource may be omitted or carry a
	// trailing slash.
	r, err := f.service.ParseAuthorization(t.Context(), authorizeValues(map[string]string{"scope": "", "resource": ""}))
	if err != nil || !r.Write() || !r.LocalRedirect() || r.RedirectHost() != "127.0.0.1" {
		t.Fatalf("defaults: %+v %v", r, err)
	}
	if r, err := f.service.ParseAuthorization(t.Context(), authorizeValues(map[string]string{"scope": "read", "resource": issuer + "/mcp/"})); err != nil || r.Write() {
		t.Fatalf("read only: %+v %v", r, err)
	}
}

func TestCodeExchangeIssuesBoundTokens(t *testing.T) {
	f := newFixture(t, true)
	code := f.approve(t, oauth.ScopeRead, oauth.ScopeWrite)
	for name, form := range map[string]url.Values{
		"wrong verifier": func() url.Values { v := codeForm(code); v.Set("code_verifier", strings.Repeat("x", 43)); return v }(),
	} {
		if _, err := f.exchange(t, form); oauthCode(err) != "invalid_grant" {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// A failed exchange consumes the code, so a stolen code cannot be retried.
	if _, err := f.exchange(t, codeForm(code)); oauthCode(err) != "invalid_grant" {
		t.Fatalf("code reused after a failed exchange: %v", err)
	}
	for name, mutate := range map[string]func(url.Values){
		"other client":   func(v url.Values) { v.Set("client_id", "https://other.example/client.json") },
		"other redirect": func(v url.Values) { v.Set("redirect_uri", "http://127.0.0.1:1/callback") },
	} {
		form := codeForm(f.approve(t, oauth.ScopeRead))
		mutate(form)
		if _, err := f.exchange(t, form); oauthCode(err) != "invalid_grant" {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// OAuth 2.1 clients may omit redirect_uri; PKCE binds the code.
	withoutRedirect := codeForm(f.approve(t, oauth.ScopeRead))
	withoutRedirect.Del("redirect_uri")
	if _, err := f.exchange(t, withoutRedirect); err != nil {
		t.Fatalf("exchange without redirect_uri: %v", err)
	}
	code = f.approve(t, oauth.ScopeRead, oauth.ScopeWrite)
	token, err := f.exchange(t, codeForm(code))
	if err != nil || token.TokenType != "Bearer" || token.ExpiresIn != 3600 || token.Scope != "read write" ||
		!strings.HasPrefix(token.AccessToken, oauth.AccessPrefix) || !strings.HasPrefix(token.RefreshToken, oauth.RefreshPrefix) {
		t.Fatalf("token %+v %v", token, err)
	}
	if _, err := f.exchange(t, codeForm(code)); oauthCode(err) != "invalid_grant" {
		t.Fatalf("code reused: %v", err)
	}
	access, err := f.service.Authenticate(t.Context(), token.AccessToken)
	if err != nil || access.AccountID != "account-1" || !slices.Equal(access.Scopes, []string{"read", "write"}) {
		t.Fatalf("access %+v %v", access, err)
	}
	// API tokens and refresh tokens are not access tokens.
	for _, raw := range []string{token.RefreshToken, strings.TrimPrefix(token.AccessToken, oauth.AccessPrefix)} {
		if _, err := f.service.Authenticate(t.Context(), raw); !errors.Is(err, oauth.ErrUnauthenticated) {
			t.Fatalf("%q authenticated: %v", raw, err)
		}
	}
	f.now = f.now.Add(oauth.AccessLifetime)
	if _, err := f.service.Authenticate(t.Context(), token.AccessToken); !errors.Is(err, oauth.ErrUnauthenticated) {
		t.Fatalf("expired access token: %v", err)
	}
	// Codes expire.
	code = f.approve(t)
	f.now = f.now.Add(oauth.CodeLifetime)
	if _, err := f.exchange(t, codeForm(code)); oauthCode(err) != "invalid_grant" {
		t.Fatalf("expired code: %v", err)
	}
}

// Withholding changes on the consent page yields a read-only connection,
// and the consent cannot grant more than the app asked for.
func TestApproveGrantsOnlyAllowedRequestedScopes(t *testing.T) {
	f := newFixture(t, false)
	token, err := f.exchange(t, codeForm(f.approve(t)))
	if err != nil || token.Scope != "read" || token.RefreshToken != "" {
		t.Fatalf("withheld changes: %+v %v", token, err)
	}
	r, _ := f.service.ParseAuthorization(t.Context(), authorizeValues(map[string]string{"scope": "read"}))
	redirect, _ := f.service.Approve(t.Context(), "account-1", r, []string{oauth.ScopeRead, oauth.ScopeWrite})
	u, _ := url.Parse(redirect)
	if token, err := f.exchange(t, codeForm(u.Query().Get("code"))); err != nil || token.Scope != "read" {
		t.Fatalf("consent widened scopes: %+v %v", token, err)
	}
}

func TestRefreshRotatesTokens(t *testing.T) {
	f := newFixture(t, true)
	first, err := f.exchange(t, codeForm(f.approve(t, oauth.ScopeRead, oauth.ScopeWrite)))
	if err != nil {
		t.Fatal(err)
	}
	refresh := func(token string, extra map[string]string) (oauth.Token, error) {
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token}, "client_id": {clientID}}
		for k, v := range extra {
			form.Set(k, v)
		}
		return f.exchange(t, form)
	}
	if _, err := refresh(first.RefreshToken, map[string]string{"client_id": "https://other.example/client.json"}); oauthCode(err) != "invalid_grant" {
		t.Fatalf("other client refreshed: %v", err)
	}
	f.now = f.now.Add(oauth.GrantLifetime - time.Minute)
	second, err := refresh(first.RefreshToken, map[string]string{"scope": "read"})
	if err != nil || second.Scope != "read write" || second.RefreshToken == first.RefreshToken {
		t.Fatalf("refresh: %+v %v", second, err)
	}
	if _, err := f.service.Authenticate(t.Context(), first.AccessToken); err == nil {
		t.Fatal("replaced access token still works")
	}
	// Each refresh extends the connection.
	f.now = f.now.Add(oauth.GrantLifetime - time.Minute)
	third, err := refresh(second.RefreshToken, nil)
	if err != nil {
		t.Fatalf("refresh within the extended lifetime: %v", err)
	}
	f.now = f.now.Add(oauth.GrantLifetime)
	if _, err := refresh(third.RefreshToken, nil); oauthCode(err) != "invalid_grant" {
		t.Fatalf("expired connection refreshed: %v", err)
	}
}

// A rejected scope changes nothing: the existing tokens keep working.
func TestRefreshCannotWidenScopes(t *testing.T) {
	f := newFixture(t, true)
	token, _ := f.exchange(t, codeForm(f.approve(t, oauth.ScopeRead)))
	for _, scope := range []string{"read write", "admin"} {
		form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token.RefreshToken}, "client_id": {clientID}, "scope": {scope}}
		if _, err := f.exchange(t, form); oauthCode(err) != "invalid_scope" {
			t.Fatalf("scope %q: %v", scope, err)
		}
	}
	if _, err := f.service.Authenticate(t.Context(), token.AccessToken); err != nil {
		t.Fatalf("access token lost to a rejected refresh: %v", err)
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token.RefreshToken}, "client_id": {clientID}}
	if _, err := f.exchange(t, form); err != nil {
		t.Fatalf("refresh token lost to a rejected refresh: %v", err)
	}
}

// A rotated refresh token presented again revokes the whole connection.
// When an attacker refreshes a stolen token first, the legitimate client's
// next refresh is that replay, so the attacker's new tokens stop working.
func TestRefreshReplayRevokesConnection(t *testing.T) {
	f := newFixture(t, true)
	first, _ := f.exchange(t, codeForm(f.approve(t, oauth.ScopeRead)))
	refresh := func(token string) (oauth.Token, error) {
		return f.exchange(t, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token}, "client_id": {clientID}})
	}
	attacker, err := refresh(first.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	second := attacker
	if _, err := refresh(first.RefreshToken); oauthCode(err) != "invalid_grant" {
		t.Fatalf("replay: %v", err)
	}
	if _, err := f.service.Authenticate(t.Context(), second.AccessToken); err == nil {
		t.Fatal("replayed refresh token left the connection active")
	}
	if _, err := refresh(second.RefreshToken); oauthCode(err) != "invalid_grant" {
		t.Fatal("the latest refresh token survived a replay")
	}
}

func TestTokenRequestValidation(t *testing.T) {
	f := newFixture(t, true)
	for name, c := range map[string]struct {
		form url.Values
		code string
	}{
		"no grant type":   {url.Values{"client_id": {clientID}}, "invalid_request"},
		"password grant":  {url.Values{"grant_type": {"password"}, "client_id": {clientID}}, "unsupported_grant_type"},
		"no client":       {url.Values{"grant_type": {"authorization_code"}}, "invalid_client"},
		"repeated":        {url.Values{"grant_type": {"authorization_code", "refresh_token"}, "client_id": {clientID}}, "invalid_request"},
		"other resource":  {url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "resource": {"https://x.example/mcp"}}, "invalid_target"},
		"bad verifier":    {url.Values{"grant_type": {"authorization_code"}, "client_id": {clientID}, "code": {"x"}, "code_verifier": {"short"}}, "invalid_request"},
		"unknown refresh": {url.Values{"grant_type": {"refresh_token"}, "client_id": {clientID}, "refresh_token": {oauth.RefreshPrefix + "nope"}}, "invalid_grant"},
	} {
		if _, err := f.exchange(t, c.form); oauthCode(err) != c.code {
			t.Errorf("%s: %v, want %s", name, err, c.code)
		}
	}
}

func TestRevocation(t *testing.T) {
	f := newFixture(t, true)
	token, _ := f.exchange(t, codeForm(f.approve(t, oauth.ScopeRead)))
	grants, err := f.service.Grants(t.Context(), "account-1")
	if err != nil || len(grants) != 1 || grants[0].ClientName != "Agent" || grants[0].Host() != "agent.example" || grants[0].Write() {
		t.Fatalf("grants %+v %v", grants, err)
	}
	if err := f.service.Revoke(t.Context(), "account-2", grants[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Authenticate(t.Context(), token.AccessToken); err != nil {
		t.Fatal("another account revoked the connection")
	}
	if err := f.service.Revoke(t.Context(), "account-1", "not-an-id"); !errors.Is(err, oauth.ErrNotFound) {
		t.Fatalf("malformed ID: %v", err)
	}
	if err := f.service.Revoke(t.Context(), "account-1", grants[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Authenticate(t.Context(), token.AccessToken); err == nil {
		t.Fatal("revoked access token still works")
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token.RefreshToken}, "client_id": {clientID}}
	if _, err := f.exchange(t, form); oauthCode(err) != "invalid_grant" {
		t.Fatalf("revoked refresh token: %v", err)
	}
	// Signing out everywhere also discards codes not yet exchanged.
	code := f.approve(t)
	if err := f.service.RevokeAll(t.Context(), "account-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.exchange(t, codeForm(code)); oauthCode(err) != "invalid_grant" {
		t.Fatalf("pending code survived sign-out everywhere: %v", err)
	}
}

func TestMetadataDescribesOneIssuerAndResource(t *testing.T) {
	f := newFixture(t, true)
	server, resource := f.service.ServerMetadata(), f.service.ResourceMetadata()
	if server.Issuer != issuer || server.AuthorizationEndpoint != issuer+"/oauth/authorize" || server.TokenEndpoint != issuer+"/oauth/token" ||
		!server.ClientIDMetadataDocumentSupported || !slices.Equal(server.CodeChallengeMethodsSupported, []string{"S256"}) ||
		!slices.Equal(server.TokenEndpointAuthMethodsSupported, []string{"none"}) {
		t.Fatalf("server metadata %+v", server)
	}
	if resource.Resource != issuer+"/mcp" || !slices.Equal(resource.AuthorizationServers, []string{issuer}) ||
		f.service.ResourceMetadataURL() != issuer+"/.well-known/oauth-protected-resource/mcp" {
		t.Fatalf("resource metadata %+v", resource)
	}
}
