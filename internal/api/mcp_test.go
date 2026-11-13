package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/appmcp"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/kv"
)

const mcpInvalidSearch = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_events","arguments":{"limit":500}}}`

func mcpPost(t *testing.T, routes http.Handler, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "http://ticketopia.test/mcp", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	return response
}

func mcpRequest(t *testing.T, routes http.Handler, origin string) *httptest.ResponseRecorder {
	t.Helper()
	headers := map[string]string{}
	if origin != "" {
		headers["Origin"] = origin
	}
	return mcpPost(t, routes, mcpInvalidSearch, headers)
}

func mcpAPI(t *testing.T, options ...Option) *api {
	t.Helper()
	t.Setenv("TICKETMASTER_KEY", "")
	app, err := NewAPI(t.Context(), zerolog.Nop(), kv.NewMemory(), options...)
	if err != nil {
		t.Fatal(err)
	}
	return app
}

func mcpRoutesFor(t *testing.T, options ...Option) http.Handler {
	t.Helper()
	return mcpAPI(t, options...).Routes()
}

func enabledMCP(rate int) Option {
	return WithMCP(appmcp.Config{Enabled: true, RatePerMinute: rate, MaxInFlight: 8})
}

func TestMCPIsOffUnlessEnabled(t *testing.T) {
	if code := mcpRequest(t, mcpRoutesFor(t), "").Code; code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
		t.Fatalf("disabled /mcp answered %d", code)
	}
}

// A tool call through Echo uses the JSON API's validation and wording, and
// is never cached.
func TestMCPToolCallMatchesAPIValidation(t *testing.T) {
	// Unset limits fall back to their defaults rather than refusing traffic.
	response := mcpRequest(t, mcpRoutesFor(t, WithMCP(appmcp.Config{Enabled: true})), "")
	body := response.Body.String()
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(body, `"isError":true`) || !strings.Contains(body, "limit: must be an integer from 1 to 100") {
		t.Fatalf("status %d cache %q body %s", response.Code, response.Header().Get("Cache-Control"), body)
	}
}

func TestMCPRateLimitsEachClient(t *testing.T) {
	routes := mcpRoutesFor(t, enabledMCP(2))
	for i := range 2 {
		if code := mcpRequest(t, routes, "").Code; code != http.StatusOK {
			t.Fatalf("request %d: %d", i, code)
		}
	}
	response := mcpRequest(t, routes, "")
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("third request: %d retry %q", response.Code, response.Header().Get("Retry-After"))
	}
}

// One request is one rate-limited call: a JSON-RPC batch cannot carry many.
func TestMCPRejectsBatches(t *testing.T) {
	batch := "  [" + strings.Repeat(mcpInvalidSearch+",", 50) + mcpInvalidSearch + "]"
	response := mcpPost(t, mcpRoutesFor(t, enabledMCP(60)), batch, nil)
	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "batch") {
		t.Fatalf("batch: %d %s", response.Code, response.Body.String())
	}
	large := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_events","arguments":{"keyword":"` + strings.Repeat("x", mcpBodyLimit) + `"}}}`
	if code := mcpPost(t, mcpRoutesFor(t, enabledMCP(60)), large, nil).Code; code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: %d", code)
	}
}

// Only configured or loopback origins pass, with accounts disabled too. A
// rebinding attacker controls both Host and Origin, so matching them proves
// nothing.
func TestMCPOriginAllowlist(t *testing.T) {
	routes := mcpRoutesFor(t, WithMCP(appmcp.Config{Enabled: true, RatePerMinute: 600, AllowedOrigins: []string{"https://ticketopia.example"}}))
	for origin, want := range map[string]int{
		"https://evil.example":       http.StatusForbidden,
		"null":                       http.StatusForbidden,
		"http://ticketopia.example":  http.StatusForbidden, // scheme must match
		"https://ticketopia.example": http.StatusOK,
		"http://localhost:6274":      http.StatusOK,
		"http://127.0.0.1:6274":      http.StatusOK,
	} {
		if code := mcpRequest(t, routes, origin).Code; code != want {
			t.Errorf("origin %q: %d, want %d", origin, code, want)
		}
	}
	rebound := mcpPost(t, routes, mcpInvalidSearch, map[string]string{"Origin": "http://ticketopia.test"})
	if rebound.Code != http.StatusForbidden {
		t.Fatalf("origin matching the request Host: %d", rebound.Code)
	}
}

// A reverse proxy connecting over loopback forwards the public Host; the
// transport must not mistake that for DNS rebinding.
func TestMCPServesPublicHostOverLoopback(t *testing.T) {
	server := httptest.NewServer(mcpRoutesFor(t, enabledMCP(60)))
	defer server.Close()
	request, _ := http.NewRequestWithContext(t.Context(), http.MethodPost, server.URL+"/mcp", strings.NewReader(mcpInvalidSearch))
	request.Host = "ticketopia.example"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("public Host over loopback: %d", response.StatusCode)
	}
}

// Excess concurrent requests are refused at once rather than queued, and
// counted as busy refusals.
func TestMCPBoundsConcurrentRequests(t *testing.T) {
	app := mcpAPI(t)
	inFlight := make(chan struct{}, 1)
	release, entered := make(chan struct{}), make(chan struct{})
	metrics := appmcp.NewMetrics()
	handler := app.mcpBoundary(newIPLimiter(600, time.Now), newIPLimiter(600, time.Now), inFlight, metrics)(func(c echo.Context) error {
		entered <- struct{}{}
		<-release
		return c.NoContent(http.StatusOK)
	})
	e := echo.New()
	serve := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader("{}"))
		_ = handler(e.NewContext(request, response))
		return response
	}
	var wg sync.WaitGroup
	wg.Go(func() { serve() })
	<-entered
	if response := serve(); response.Code != http.StatusServiceUnavailable || response.Header().Get("Retry-After") != "1" {
		t.Fatalf("second concurrent request: %d %q", response.Code, response.Header().Get("Retry-After"))
	}
	close(release)
	wg.Wait()
	if got := metrics.Snapshot().Refusals[appmcp.RefusedBusy]; got != 1 {
		t.Fatalf("busy refusals counted: %d", got)
	}
}

func TestMCPLimiterCapsTrackedClients(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	l := newIPLimiter(60, func() time.Time { return now })
	for i := range mcpClients {
		if ok, _ := l.allow(fmt.Sprintf("2001:db8::%x", i)); !ok {
			t.Fatalf("client %d refused before the cap", i)
		}
	}
	if ok, _ := l.allow("2001:db8::ffff:1"); ok || len(l.clients) != mcpClients {
		t.Fatalf("new client admitted past the cap (%d tracked)", len(l.clients))
	}
	if ok, _ := l.allow("2001:db8::1"); !ok {
		t.Fatal("known client refused at the cap")
	}
	// Rejections at capacity are cheap: no rescan until the next minute.
	swept := l.swept
	now = now.Add(time.Second) // a rescan would record this time
	for range 1000 {
		l.allow("2001:db8::ffff:2")
	}
	if l.swept != swept {
		t.Fatal("rejected clients triggered extra sweeps")
	}
	now = now.Add(2 * time.Minute)
	if ok, _ := l.allow("2001:db8::ffff:1"); !ok || len(l.clients) != 1 {
		t.Fatalf("idle clients not swept: %d tracked", len(l.clients))
	}
}

// Provider cooldowns reach agents as text, since a tool error has no headers.
func TestMCPToolErrorsCarryRetryDelay(t *testing.T) {
	if got := describeForAgents(&discovery.UnavailableError{RetryAfter: 90 * time.Second}); !strings.HasSuffix(got, "Retry after 90 seconds.") {
		t.Fatalf("unavailable: %q", got)
	}
	if got := describeForAgents(&discovery.ValidationError{Field: "limit", Message: "must be an integer from 1 to 100"}); strings.Contains(got, "Retry") {
		t.Fatalf("validation: %q", got)
	}
}

func TestMCPConfigFromEnv(t *testing.T) {
	for _, name := range []string{"MCP_ENABLED", "MCP_RATE_PER_MINUTE", "MCP_MAX_IN_FLIGHT", "MCP_ALLOWED_ORIGINS", "AUTH_BASE_URL", "MCP_OAUTH_ENABLED", "MCP_REQUIRE_SIGN_IN"} {
		t.Setenv(name, "")
	}
	if c, err := appmcp.ConfigFromEnv(); err != nil || c.Enabled || c.OAuth || c.RatePerMinute != 60 || c.MaxInFlight != 8 || len(c.AllowedOrigins) != 0 {
		t.Fatalf("defaults %+v %v", c, err)
	}
	// Disabled MCP reads nothing else, so its settings cannot stop startup.
	for name, value := range map[string]string{"MCP_RATE_PER_MINUTE": "0", "MCP_ALLOWED_ORIGINS": "null", "AUTH_BASE_URL": "ftp://elsewhere/path", "MCP_OAUTH_ENABLED": "maybe"} {
		t.Setenv(name, value)
	}
	if _, err := appmcp.ConfigFromEnv(); err != nil {
		t.Fatalf("disabled MCP validated its settings: %v", err)
	}
	for _, name := range []string{"MCP_RATE_PER_MINUTE", "MCP_ALLOWED_ORIGINS", "MCP_OAUTH_ENABLED"} {
		t.Setenv(name, "")
	}
	t.Setenv("MCP_ENABLED", "true")
	if c, err := appmcp.ConfigFromEnv(); err == nil {
		t.Fatalf("invalid AUTH_BASE_URL origin accepted: %+v", c)
	}
	// AUTH_BASE_URL is read directly, so the public origin applies without
	// accounts, with trailing slashes trimmed as accounts trim them.
	t.Setenv("AUTH_BASE_URL", "https://Ticketopia.example///")
	if c, _ := appmcp.ConfigFromEnv(); !slices.Equal(c.AllowedOrigins, []string{"https://ticketopia.example"}) {
		t.Fatalf("AUTH_BASE_URL origin: %v", c.AllowedOrigins)
	}
	t.Setenv("MCP_ALLOWED_ORIGINS", "https://a.example, http://b.example:8080")
	if c, _ := appmcp.ConfigFromEnv(); !slices.Equal(c.AllowedOrigins, []string{"https://a.example", "http://b.example:8080"}) {
		t.Fatalf("explicit origins: %v", c.AllowedOrigins)
	}
	t.Setenv("MCP_ALLOWED_ORIGINS", "")
	// With accounts, OAuth and required sign-in default on; each can be
	// turned off. Without accounts they default off and cannot be enabled.
	resolve := func(accounts bool) (appmcp.Config, error) {
		c, err := appmcp.ConfigFromEnv()
		if err != nil {
			return c, err
		}
		return c.Resolve(accounts)
	}
	if c, err := resolve(true); err != nil || !c.OAuth || !c.RequireSignIn {
		t.Fatalf("defaults with accounts: %+v %v", c, err)
	}
	if c, err := resolve(false); err != nil || c.OAuth || c.RequireSignIn {
		t.Fatalf("defaults without accounts: %+v %v", c, err)
	}
	t.Setenv("MCP_REQUIRE_SIGN_IN", "false")
	if c, err := resolve(true); err != nil || !c.OAuth || c.RequireSignIn {
		t.Fatalf("optional sign-in: %+v %v", c, err)
	}
	t.Setenv("MCP_OAUTH_ENABLED", "false")
	if c, err := resolve(true); err != nil || c.OAuth || c.RequireSignIn {
		t.Fatalf("OAuth off: %+v %v", c, err)
	}
	for _, name := range []string{"MCP_OAUTH_ENABLED", "MCP_REQUIRE_SIGN_IN"} {
		t.Setenv("MCP_OAUTH_ENABLED", "")
		t.Setenv("MCP_REQUIRE_SIGN_IN", "")
		t.Setenv(name, "true")
		if _, err := resolve(false); err == nil {
			t.Fatalf("%s=true accepted without accounts", name)
		}
	}
	t.Setenv("MCP_OAUTH_ENABLED", "")
	t.Setenv("MCP_REQUIRE_SIGN_IN", "")
	for name, value := range map[string]string{"MCP_ENABLED": "sometimes", "MCP_OAUTH_ENABLED": "maybe", "MCP_REQUIRE_SIGN_IN": "always", "MCP_RATE_PER_MINUTE": "0", "MCP_MAX_IN_FLIGHT": "257", "MCP_ALLOWED_ORIGINS": "null"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("MCP_ENABLED", "true")
			t.Setenv(name, value)
			if _, err := appmcp.ConfigFromEnv(); err == nil {
				t.Fatalf("%s=%s accepted", name, value)
			}
		})
	}
}

// A sent token must be valid: with accounts disabled none can be, and only
// the Bearer scheme is accepted. No token keeps the public tools.
func TestMCPRejectsUnusableTokens(t *testing.T) {
	routes := mcpRoutesFor(t, enabledMCP(600))
	for _, header := range []string{"Bearer not-a-real-token", "Basic dXNlcjpwYXNz", "bearer lowercase"} {
		response := mcpPost(t, routes, mcpInvalidSearch, map[string]string{"Authorization": header})
		if response.Code != http.StatusUnauthorized || !strings.HasPrefix(response.Header().Get("WWW-Authenticate"), "Bearer") {
			t.Errorf("%q: %d %q", header, response.Code, response.Header().Get("WWW-Authenticate"))
		}
	}
	if code := mcpRequest(t, routes, "").Code; code != http.StatusOK {
		t.Fatalf("anonymous public call: %d", code)
	}
}
