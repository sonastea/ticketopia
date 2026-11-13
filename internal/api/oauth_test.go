package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/modelcontextprotocol/go-sdk/oauthex"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/appmcp"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/oauth"
	"github.com/sonastea/ticketopia/internal/oauth/oauthtest"
	"github.com/sonastea/ticketopia/internal/saved"
)

const (
	oauthBase     = "https://events.example"
	agentClientID = "https://agent.example/oauth/client.json"
	agentRedirect = "http://127.0.0.1:53682/callback"
)

type oauthFixture struct {
	app            *api
	routes         http.Handler
	grants         *oauthtest.Repository
	browser, csrf  string
	apiToken       string
	savedEvent     string
	accountStorage *accountStub
	store          *savedStub
}

func newOAuthFixture(t *testing.T, enabled bool, require ...bool) *oauthFixture {
	t.Helper()
	base, repo, browser, bearer, csrf := accountAPI(t)
	event := models.Event{ID: "ticketmaster:Agent", Name: "Agent show", Source: models.Source{Provider: "ticketmaster", ID: "Agent"}}
	store := &savedStub{items: map[string]map[string]saved.Item{}, detail: models.EventDetail{Item: event, Meta: models.Freshness{DataAsOf: time.Now()}}}
	grants := oauthtest.NewRepository()
	clients := oauthtest.Clients{agentClientID: {ID: agentClientID, Name: "Test Agent", RedirectURIs: []string{"http://127.0.0.1/callback"}, Refresh: true}}
	app := mcpAPI(t, WithAccounts(base.authConfig, base.accounts), WithSavedEvents(store),
		WithMCP(appmcp.Config{Enabled: true, OAuth: enabled, RequireSignIn: len(require) > 0 && require[0], RatePerMinute: 600, MaxInFlight: 8}),
		WithMCPOAuth(oauth.New(grants, clients, oauthBase)))
	return &oauthFixture{app: app, routes: app.Routes(), grants: grants, browser: browser, csrf: csrf, apiToken: bearer, savedEvent: event.ID, accountStorage: repo, store: store}
}

func (f *oauthFixture) do(t *testing.T, method, target, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	for key, value := range headers {
		r.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	f.routes.ServeHTTP(w, r)
	return w
}

func (f *oauthFixture) browserRequest(t *testing.T, method, target string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	headers := map[string]string{"Cookie": f.app.authConfig.CookieName("session") + "=" + f.browser}
	body := ""
	if form != nil {
		headers["Content-Type"] = "application/x-www-form-urlencoded"
		headers["Origin"] = oauthBase
		body = form.Encode()
	}
	return f.do(t, method, target, body, headers)
}

var hiddenInput = regexp.MustCompile(`<input type="hidden" name="([a-z_]+)" value="([^"]*)"`)

// consent loads the consent page for an authorization URL as the signed-in
// browser and submits the decision, returning the redirect to the client.
func (f *oauthFixture) consent(t *testing.T, authorizeURL, decision string, write bool) *url.URL {
	t.Helper()
	page := f.browserRequest(t, http.MethodGet, authorizeURL, nil)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Connect Test Agent to Ticketopia?") ||
		!strings.Contains(page.Body.String(), "agent.example") || page.Header().Get("Content-Security-Policy") != "frame-ancestors 'none'" {
		t.Fatalf("consent page: %d %v\n%s", page.Code, page.Header(), page.Body.String())
	}
	form := url.Values{"decision": {decision}}
	for _, m := range hiddenInput.FindAllStringSubmatch(page.Body.String(), -1) {
		form.Set(m[1], html.UnescapeString(m[2]))
	}
	if write {
		form.Set("write", "on")
	}
	response := f.browserRequest(t, http.MethodPost, oauth.AuthorizePath, form)
	if response.Code != http.StatusSeeOther {
		t.Fatalf("decision: %d %s", response.Code, response.Body.String())
	}
	u, err := url.Parse(response.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

// toServer sends every request, whatever its host, to the test server.
type toServer struct{ base *url.URL }

func (s toServer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = s.base.Scheme, s.base.Host
	return http.DefaultTransport.RoundTrip(r)
}

// A real MCP client signs in through the browser when a signed-in tool needs
// it, then asks again for changes when the person first allowed only reading.
func TestMCPOAuthEndToEnd(t *testing.T) {
	f := newOAuthFixture(t, true)
	server := httptest.NewServer(f.routes)
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client := &http.Client{Transport: toServer{base}}

	consents := 0
	handler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		ClientIDMetadataDocumentConfig: &auth.ClientIDMetadataDocumentConfig{URL: agentClientID},
		RedirectURL:                    agentRedirect,
		Client:                         client,
		RequestRefreshToken:            true,
		AuthorizationCodeFetcher: func(_ context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			consents++
			// The person allows only reading the first time.
			u := f.consent(t, args.URL, "allow", consents > 1)
			q := u.Query()
			if u.Host != "127.0.0.1:53682" || q.Get("iss") != oauthBase {
				return nil, fmt.Errorf("redirect %s", u)
			}
			return &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-agent"}, nil).Connect(t.Context(),
		&mcp.StreamableClientTransport{Endpoint: oauthBase + "/mcp", HTTPClient: client, OAuthHandler: handler, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	// Public tools need no sign-in.
	if _, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_categories"}); err != nil || consents != 0 {
		t.Fatalf("public tool: %v consents=%d", err, consents)
	}
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "my_saved_events", Arguments: map[string]any{}})
	if err != nil || result.IsError || consents != 1 {
		t.Fatalf("signed-in read: %+v %v consents=%d", result, err, consents)
	}
	result, err = session.CallTool(t.Context(), &mcp.CallToolParams{Name: "save_event", Arguments: map[string]any{"event_id": f.savedEvent}})
	if err != nil || result.IsError || consents != 2 {
		t.Fatalf("change after step-up: %+v %v consents=%d", result, err, consents)
	}
	grants, _ := f.app.oauth.Grants(t.Context(), f.accountStorage.account.ID)
	if len(grants) != 2 || grants[0].ClientName != "Test Agent" {
		t.Fatalf("grants %+v", grants)
	}

	// The account page lists both connections; disconnecting one stops its token.
	page := f.browserRequest(t, http.MethodGet, "/me", nil)
	if strings.Count(page.Body.String(), "Disconnect Test Agent") != 2 || !strings.Contains(page.Body.String(), "Read-only") ||
		!strings.Contains(page.Body.String(), "Can make changes") {
		t.Fatalf("account page connections:\n%s", page.Body.String())
	}
	source, _ := handler.TokenSource(t.Context())
	token, _ := source.Token()
	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"my_saved_events","arguments":{}}}`
	bearer := map[string]string{"Authorization": "Bearer " + token.AccessToken}
	if r := mcpPost(t, f.routes, call, bearer); r.Code != http.StatusOK {
		t.Fatalf("token before revocation: %d", r.Code)
	}
	var current string
	for _, g := range grants {
		if g.AccessHash == oauth.Hash(token.AccessToken) {
			current = g.ID
		}
	}
	revoke := f.browserRequest(t, http.MethodPost, "/me/connections/"+current+"/revoke", url.Values{"csrf_token": {f.csrf}})
	if revoke.Code != http.StatusSeeOther || revoke.Header().Get("Location") != "/me?saved=connection" {
		t.Fatalf("revoke: %d %v", revoke.Code, revoke.Header())
	}
	r := mcpPost(t, f.routes, call, bearer)
	if r.Code != http.StatusUnauthorized || !strings.Contains(r.Header().Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Fatalf("revoked token: %d %v", r.Code, r.Header())
	}
	// Signing out everywhere disconnects the rest.
	if r := f.browserRequest(t, http.MethodPost, "/auth/logout-all", url.Values{"csrf_token": {f.csrf}}); r.Code != http.StatusSeeOther {
		t.Fatalf("logout-all: %d %s", r.Code, r.Body.String())
	}
	if len(f.grants.Grants) != 0 {
		t.Fatalf("connections after sign-out everywhere: %d", len(f.grants.Grants))
	}
}

func TestMCPOAuthChallengesAndMetadata(t *testing.T) {
	f := newOAuthFixture(t, true)
	for _, path := range []string{oauth.ResourceMetadataPath, oauth.ResourceMetadataInsertion} {
		r := f.do(t, http.MethodGet, path, "", nil)
		var meta oauthex.ProtectedResourceMetadata
		if r.Code != http.StatusOK || json.Unmarshal(r.Body.Bytes(), &meta) != nil || meta.Resource != oauthBase+"/mcp" ||
			meta.AuthorizationServers[0] != oauthBase || r.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Fatalf("%s: %d %s", path, r.Code, r.Body.String())
		}
	}
	r := f.do(t, http.MethodGet, oauth.ServerMetadataPath, "", nil)
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), `"client_id_metadata_document_supported":true`) {
		t.Fatalf("server metadata: %d %s", r.Code, r.Body.String())
	}
	if r := f.do(t, http.MethodOptions, oauth.TokenPath, "", nil); r.Code != http.StatusNoContent || r.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight: %d %v", r.Code, r.Header())
	}
	// Anonymous: public tools work; signed-in tools get a challenge pointing
	// at the metadata; a malformed body is left to the transport.
	if r := mcpRequest(t, f.routes, ""); r.Code != http.StatusOK {
		t.Fatalf("anonymous public tool: %d", r.Code)
	}
	r = mcpPost(t, f.routes, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"save_event","arguments":{}}}`, nil)
	challenge := r.Header().Get("WWW-Authenticate")
	// The challenge is the whole response: the tool call never runs.
	var body map[string]string
	if r.Code != http.StatusUnauthorized || !strings.Contains(challenge, `resource_metadata="`+oauthBase+oauth.ResourceMetadataInsertion+`"`) ||
		!strings.Contains(challenge, `scope="read write"`) || json.Unmarshal(r.Body.Bytes(), &body) != nil || !strings.Contains(body["error"], "sign in") {
		t.Fatalf("anonymous signed-in tool: %d %q %s", r.Code, challenge, r.Body.String())
	}
	// A read-only connection reads, and is asked to step up for changes
	// without the change running.
	access := oauth.AccessPrefix + "read-only-fixture-token"
	f.grants.CreateGrant(oauth.Grant{ID: "0123456789abcdef0123456789abcdef", AccountID: f.accountStorage.account.ID, ClientID: agentClientID, ClientName: "Test Agent",
		Scopes: []string{oauth.ScopeRead}, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), AccessHash: oauth.Hash(access), AccessUntil: time.Now().Add(time.Hour)}, time.Now())
	bearer := map[string]string{"Authorization": "Bearer " + access}
	if r := mcpPost(t, f.routes, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"my_saved_events","arguments":{}}}`, bearer); r.Code != http.StatusOK || strings.Contains(r.Body.String(), `"isError":true`) {
		t.Fatalf("read-only read: %d %s", r.Code, r.Body.String())
	}
	r = mcpPost(t, f.routes, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"save_event","arguments":{"event_id":"`+f.savedEvent+`"}}}`, bearer)
	body = nil
	if r.Code != http.StatusForbidden || !strings.Contains(r.Header().Get("WWW-Authenticate"), `error="insufficient_scope"`) ||
		json.Unmarshal(r.Body.Bytes(), &body) != nil || len(f.store.items[f.accountStorage.account.ID]) != 0 {
		t.Fatalf("read-only change: %d %v %s", r.Code, r.Header(), r.Body.String())
	}
}

func TestOAuthAuthorizeRequests(t *testing.T) {
	f := newOAuthFixture(t, true)
	request := func(overrides map[string]string) string {
		v := url.Values{"response_type": {"code"}, "client_id": {agentClientID}, "redirect_uri": {agentRedirect}, "state": {"s1"},
			"code_challenge": {strings.Repeat("c", 43)}, "code_challenge_method": {"S256"}, "scope": {"read write"}, "resource": {oauthBase + "/mcp"}}
		for key, value := range overrides {
			v.Set(key, value)
		}
		return oauth.AuthorizePath + "?" + v.Encode()
	}
	// Signed out: sign in first, then return to the same request.
	r := f.do(t, http.MethodGet, request(nil), "", nil)
	location, _ := url.Parse(r.Header().Get("Location"))
	if r.Code != http.StatusSeeOther || location.Path != "/auth/sign-in" || safeAuthReturn(location.Query().Get("return_to")) != request(nil) {
		t.Fatalf("signed out: %d %s", r.Code, location)
	}
	signIn := f.do(t, http.MethodGet, location.String(), "", nil)
	if !strings.Contains(signIn.Body.String(), "An app wants to connect to your Ticketopia account") {
		t.Fatalf("sign-in context:\n%s", signIn.Body.String())
	}
	// An untrusted client or return address is shown, never redirected to.
	for name, overrides := range map[string]map[string]string{
		"unknown client":    {"client_id": "https://evil.example/client.json"},
		"unregistered port": {"redirect_uri": "https://evil.example/callback"},
	} {
		r := f.browserRequest(t, http.MethodGet, request(overrides), nil)
		if r.Code != http.StatusBadRequest || r.Header().Get("Location") != "" || !strings.Contains(r.Body.String(), "Nothing was connected") {
			t.Fatalf("%s: %d %v", name, r.Code, r.Header())
		}
	}
	// Other errors return to the client.
	r = f.browserRequest(t, http.MethodGet, request(map[string]string{"code_challenge_method": "plain"}), nil)
	location, _ = url.Parse(r.Header().Get("Location"))
	if r.Code != http.StatusSeeOther || location.Host != "127.0.0.1:53682" || location.Query().Get("error") != "invalid_request" || location.Query().Get("state") != "s1" {
		t.Fatalf("plain PKCE: %d %s", r.Code, location)
	}
	// Cancel returns access_denied; consent without the session's CSRF token issues nothing.
	if u := f.consent(t, request(nil), "deny", true); u.Query().Get("error") != "access_denied" || u.Query().Get("code") != "" {
		t.Fatalf("deny: %s", u)
	}
	form := url.Values{"decision": {"allow"}, "client_id": {agentClientID}, "redirect_uri": {agentRedirect}, "response_type": {"code"},
		"code_challenge": {strings.Repeat("c", 43)}, "code_challenge_method": {"S256"}}
	if r := f.browserRequest(t, http.MethodPost, oauth.AuthorizePath, form); r.Code != http.StatusForbidden || len(f.grants.Codes) != 0 {
		t.Fatalf("missing CSRF: %d codes=%d", r.Code, len(f.grants.Codes))
	}
	form.Set("csrf_token", f.csrf)
	if r := f.browserRequest(t, http.MethodPost, oauth.AuthorizePath, form); r.Code != http.StatusSeeOther || len(f.grants.Codes) != 1 {
		t.Fatalf("allow: %d codes=%d", r.Code, len(f.grants.Codes))
	}
}

func TestOAuthTokenEndpoint(t *testing.T) {
	f := newOAuthFixture(t, true)
	form := "grant_type=authorization_code&client_id=" + url.QueryEscape(agentClientID) + "&code=nope&code_verifier=" + strings.Repeat("v", 43)
	for name, c := range map[string]struct {
		headers map[string]string
		status  int
		code    string
	}{
		"client secret": {map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Authorization": "Basic eDp5"}, 401, "invalid_client"},
		"json body":     {map[string]string{"Content-Type": "application/json"}, 400, "invalid_request"},
		"unknown code":  {map[string]string{"Content-Type": "application/x-www-form-urlencoded"}, 400, "invalid_grant"},
	} {
		r := f.do(t, http.MethodPost, oauth.TokenPath, form, c.headers)
		var body struct{ Error string }
		_ = json.Unmarshal(r.Body.Bytes(), &body)
		if r.Code != c.status || body.Error != c.code || r.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d %s", name, r.Code, r.Body.String())
		}
	}
	f.grants.Fail = io.ErrUnexpectedEOF
	r := f.do(t, http.MethodPost, oauth.TokenPath, form, map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if r.Code != http.StatusServiceUnavailable || strings.Contains(r.Body.String(), "EOF") {
		t.Fatalf("storage failure: %d %s", r.Code, r.Body.String())
	}
}

// An agent disconnects itself at the advertised revocation endpoint: its
// token stops working on /mcp at once and the account page no longer lists it.
func TestOAuthRevocationEndpoint(t *testing.T) {
	f := newOAuthFixture(t, true)
	meta := f.do(t, http.MethodGet, oauth.ServerMetadataPath, "", nil)
	if !strings.Contains(meta.Body.String(), `"revocation_endpoint":"`+oauthBase+oauth.RevocationPath+`"`) {
		t.Fatalf("metadata: %s", meta.Body.String())
	}
	if r := f.do(t, http.MethodOptions, oauth.RevocationPath, "", nil); r.Code != http.StatusNoContent || r.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("preflight: %d %v", r.Code, r.Header())
	}
	access := oauth.AccessPrefix + "revocable-fixture-token"
	f.grants.CreateGrant(oauth.Grant{ID: "0123456789abcdef0123456789abcdef", AccountID: f.accountStorage.account.ID, ClientID: agentClientID, ClientName: "Test Agent",
		Scopes: []string{oauth.ScopeRead}, CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour), AccessHash: oauth.Hash(access), AccessUntil: time.Now().Add(time.Hour)}, time.Now())
	form := url.Values{"token": {access}, "token_type_hint": {"access_token"}, "client_id": {agentClientID}}.Encode()
	formType := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	for name, c := range map[string]struct {
		body    string
		headers map[string]string
		status  int
		code    string
	}{
		"client secret": {form, map[string]string{"Content-Type": "application/x-www-form-urlencoded", "Authorization": "Basic eDp5"}, 401, "invalid_client"},
		"json body":     {form, map[string]string{"Content-Type": "application/json"}, 400, "invalid_request"},
		"missing token": {"client_id=" + url.QueryEscape(agentClientID), formType, 400, "invalid_request"},
	} {
		r := f.do(t, http.MethodPost, oauth.RevocationPath, c.body, c.headers)
		var body struct{ Error string }
		_ = json.Unmarshal(r.Body.Bytes(), &body)
		if r.Code != c.status || body.Error != c.code || r.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d %s", name, r.Code, r.Body.String())
		}
	}
	if len(f.grants.Grants) != 1 {
		t.Fatal("a refused request revoked the connection")
	}
	f.grants.Fail = io.ErrUnexpectedEOF
	r := f.do(t, http.MethodPost, oauth.RevocationPath, form, formType)
	if r.Code != http.StatusServiceUnavailable || r.Header().Get("Retry-After") == "" || strings.Contains(r.Body.String(), "EOF") {
		t.Fatalf("storage failure: %d %v %s", r.Code, r.Header(), r.Body.String())
	}
	f.grants.Fail = nil

	call := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"my_saved_events","arguments":{}}}`
	bearer := map[string]string{"Authorization": "Bearer " + access}
	if r := mcpPost(t, f.routes, call, bearer); r.Code != http.StatusOK {
		t.Fatalf("token before revocation: %d", r.Code)
	}
	// Revoking succeeds, and so does revoking again: the client is told
	// nothing about whether the token was still live, but the log is.
	var logs bytes.Buffer
	f.app.logger = zerolog.New(&logs)
	for range 2 {
		r := f.do(t, http.MethodPost, oauth.RevocationPath, form, formType)
		if r.Code != http.StatusOK || r.Body.Len() != 0 || r.Header().Get("Access-Control-Allow-Origin") != "*" {
			t.Fatalf("revoke: %d %v %q", r.Code, r.Header(), r.Body.String())
		}
	}
	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"outcome":"revoked"`) || !strings.Contains(lines[1], `"outcome":"unknown"`) ||
		!strings.Contains(lines[0], `"client_host":"agent.example"`) || strings.Contains(logs.String(), access) {
		t.Fatalf("revocation logs:\n%s", logs.String())
	}
	if r := mcpPost(t, f.routes, call, bearer); r.Code != http.StatusUnauthorized || !strings.Contains(r.Header().Get("WWW-Authenticate"), `error="invalid_token"`) {
		t.Fatalf("revoked token: %d %v", r.Code, r.Header())
	}
	if page := f.browserRequest(t, http.MethodGet, "/me", nil); strings.Contains(page.Body.String(), "Disconnect Test Agent") {
		t.Fatal("account page still lists the revoked connection")
	}
}

// With OAuth off, nothing new is served and signed-in tools keep their tool
// error, so enabling MCP alone changes no existing client's behavior.
func TestMCPOAuthOffByDefault(t *testing.T) {
	f := newOAuthFixture(t, false)
	for _, path := range []string{oauth.ServerMetadataPath, oauth.ResourceMetadataInsertion, oauth.AuthorizePath} {
		if r := f.do(t, http.MethodGet, path, "", nil); r.Code != http.StatusNotFound {
			t.Errorf("%s: %d", path, r.Code)
		}
	}
	if r := f.do(t, http.MethodPost, oauth.RevocationPath, "token=x&client_id=y", map[string]string{"Content-Type": "application/x-www-form-urlencoded"}); r.Code != http.StatusNotFound {
		t.Errorf("revocation without OAuth: %d", r.Code)
	}
	r := mcpPost(t, f.routes, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"save_event","arguments":{"event_id":"x"}}}`, nil)
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "Authorization: Bearer") {
		t.Fatalf("signed-in tool without OAuth: %d %s", r.Code, r.Body.String())
	}
	if r := mcpPost(t, f.routes, `{}`, map[string]string{"Authorization": "Bearer " + oauth.AccessPrefix + "x"}); r.Code != http.StatusUnauthorized ||
		strings.Contains(r.Header().Get("WWW-Authenticate"), "resource_metadata") {
		t.Fatalf("access token without OAuth: %d %v", r.Code, r.Header())
	}
}

// With sign-in required, every request without a token is challenged,
// including initialize and public tools, and an MCP client signs in as soon
// as it connects. Any valid token, OAuth or API, is enough for public tools.
func TestMCPRequireSignIn(t *testing.T) {
	f := newOAuthFixture(t, true, true)
	for name, body := range map[string]string{
		"initialize":  `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"x","version":"1"}}}`,
		"tools/list":  `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		"public tool": mcpInvalidSearch,
		"bad JSON":    `{`,
	} {
		r := mcpPost(t, f.routes, body, nil)
		var reply map[string]string
		if r.Code != http.StatusUnauthorized || !strings.Contains(r.Header().Get("WWW-Authenticate"), "resource_metadata=") ||
			json.Unmarshal(r.Body.Bytes(), &reply) != nil {
			t.Errorf("%s: %d %v %s", name, r.Code, r.Header(), r.Body.String())
		}
	}
	if r := f.do(t, http.MethodGet, "/mcp", "", map[string]string{"Accept": "text/event-stream"}); r.Code != http.StatusUnauthorized {
		t.Errorf("GET stream: %d", r.Code)
	}
	if r := mcpPost(t, f.routes, mcpInvalidSearch, map[string]string{"Authorization": "Bearer " + f.apiToken}); r.Code != http.StatusOK {
		t.Fatalf("API token on a public tool: %d %s", r.Code, r.Body.String())
	}

	server := httptest.NewServer(f.routes)
	defer server.Close()
	base, _ := url.Parse(server.URL)
	client := &http.Client{Transport: toServer{base}}
	consents := 0
	handler, err := auth.NewAuthorizationCodeHandler(&auth.AuthorizationCodeHandlerConfig{
		ClientIDMetadataDocumentConfig: &auth.ClientIDMetadataDocumentConfig{URL: agentClientID},
		RedirectURL:                    agentRedirect,
		Client:                         client,
		AuthorizationCodeFetcher: func(_ context.Context, args *auth.AuthorizationArgs) (*auth.AuthorizationResult, error) {
			consents++
			q := f.consent(t, args.URL, "allow", false).Query()
			return &auth.AuthorizationResult{Code: q.Get("code"), State: q.Get("state"), Iss: q.Get("iss")}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "test-agent"}, nil).Connect(t.Context(),
		&mcp.StreamableClientTransport{Endpoint: oauthBase + "/mcp", HTTPClient: client, OAuthHandler: handler, DisableStandaloneSSE: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	if consents != 1 {
		t.Fatalf("connecting asked for consent %d times", consents)
	}
	if result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_categories"}); err != nil || consents != 1 {
		t.Fatalf("public tool after sign-in: %+v %v consents=%d", result, err, consents)
	}
}
