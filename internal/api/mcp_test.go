package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/appmcp"
	"github.com/sonastea/ticketopia/internal/kv"
)

const mcpInvalidSearch = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"find_events","arguments":{"limit":500}}}`

func mcpRequest(t *testing.T, routes http.Handler, origin string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "http://ticketopia.test/mcp", strings.NewReader(mcpInvalidSearch))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if origin != "" {
		request.Header.Set("Origin", origin)
	}
	response := httptest.NewRecorder()
	routes.ServeHTTP(response, request)
	return response
}

func mcpRoutesFor(t *testing.T, options ...Option) http.Handler {
	t.Helper()
	t.Setenv("TICKETMASTER_KEY", "")
	app, err := NewAPI(t.Context(), zerolog.Nop(), kv.NewMemory(), options...)
	if err != nil {
		t.Fatal(err)
	}
	return app.Routes()
}

func TestMCPIsOffUnlessEnabled(t *testing.T) {
	if code := mcpRequest(t, mcpRoutesFor(t), "").Code; code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
		t.Fatalf("disabled /mcp answered %d", code)
	}
}

// A tool call through Echo uses the JSON API's validation and wording, and
// is never cached.
func TestMCPToolCallMatchesAPIValidation(t *testing.T) {
	response := mcpRequest(t, mcpRoutesFor(t, WithMCP(appmcp.Config{Enabled: true, RatePerMinute: 60})), "")
	body := response.Body.String()
	if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "no-store" ||
		!strings.Contains(body, `"isError":true`) || !strings.Contains(body, "limit: must be an integer from 1 to 100") {
		t.Fatalf("status %d cache %q body %s", response.Code, response.Header().Get("Cache-Control"), body)
	}
}

func TestMCPRateLimitsEachClient(t *testing.T) {
	routes := mcpRoutesFor(t, WithMCP(appmcp.Config{Enabled: true, RatePerMinute: 2}))
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

func TestMCPRejectsCrossOriginBrowsers(t *testing.T) {
	routes := mcpRoutesFor(t, WithMCP(appmcp.Config{Enabled: true, RatePerMinute: 60}))
	if code := mcpRequest(t, routes, "https://evil.example").Code; code != http.StatusForbidden {
		t.Fatalf("cross-origin: %d", code)
	}
	if code := mcpRequest(t, routes, "http://ticketopia.test").Code; code != http.StatusOK {
		t.Fatalf("same origin: %d", code)
	}
}

func TestMCPConfigFromEnv(t *testing.T) {
	t.Setenv("MCP_ENABLED", "")
	t.Setenv("MCP_RATE_PER_MINUTE", "")
	if c, err := appmcp.ConfigFromEnv(); err != nil || c.Enabled || c.RatePerMinute != 60 {
		t.Fatalf("defaults %+v %v", c, err)
	}
	for name, value := range map[string]string{"MCP_ENABLED": "sometimes", "MCP_RATE_PER_MINUTE": "0"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, value)
			if _, err := appmcp.ConfigFromEnv(); err == nil {
				t.Fatalf("%s=%s accepted", name, value)
			}
		})
	}
}
