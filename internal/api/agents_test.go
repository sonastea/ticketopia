package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/appmcp"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/oauth"
)

func TestAgentGuideReflectsAvailableFeatures(t *testing.T) {
	const authBase = "https://Issuer.example"
	for _, tc := range []struct {
		name, configuredOrigin, wantMCPURL          string
		accounts, mcp, oauth, requireSignIn, drafts bool
	}{
		{name: "public API only"},
		{name: "public MCP without an origin", mcp: true, wantMCPURL: "/mcp"},
		{name: "public MCP with invalid origin", mcp: true, configuredOrigin: "https://bad.example/agents", wantMCPURL: "/mcp"},
		{name: "public MCP behind TLS proxy", mcp: true, configuredOrigin: "https://public.example/", wantMCPURL: "https://public.example/mcp"},
		{name: "accounts and no MCP", accounts: true},
		{name: "MCP with API tokens", accounts: true, mcp: true, requireSignIn: true, wantMCPURL: authBase + "/mcp"},
		{name: "all features", accounts: true, mcp: true, oauth: true, drafts: true, configuredOrigin: "https://ignored.example", wantMCPURL: authBase + "/mcp"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("AUTH_BASE_URL", tc.configuredOrigin)
			options := []Option{WithMCP(appmcp.Config{Enabled: tc.mcp, OAuth: tc.oauth, RequireSignIn: tc.requireSignIn})}
			if tc.accounts {
				config := accounts.Config{Enabled: true, BaseURL: authBase, ClientID: "fixture", ClientSecret: "fixture"}
				options = append(options, WithAccounts(config, accounts.New(nil, nil)))
			}
			if tc.oauth {
				options = append(options, WithMCPOAuth(oauth.New(nil, nil, authBase)))
			}
			if tc.drafts {
				options = append(options, WithAgentDrafts(nil), WithEventRecommendations(nil))
			}
			app, err := NewAPI(t.Context(), zerolog.Nop(), kv.NewMemory(), options...)
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodGet, "http://internal.example:8090/agents", nil)
			request.Header.Set("X-Forwarded-Host", "forwarded.example")
			request.Header.Set("X-Forwarded-Proto", "https")
			response := httptest.NewRecorder()
			app.Routes().ServeHTTP(response, request)
			if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/html; charset=UTF-8" ||
				response.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("guide should be public HTML without caching: %d %v", response.Code, response.Header())
			}
			body := response.Body.String()
			check := func(visible bool, markers ...string) {
				t.Helper()
				for _, marker := range markers {
					if strings.Contains(body, marker) != visible {
						t.Errorf("guide shows %q = %v; want %v", marker, !visible, visible)
					}
				}
			}
			check(true, `href="/api/v1/openapi.yaml"`)
			check(false, "internal.example", "forwarded.example")
			check(tc.mcp, `id="mcp-endpoint"`, `href="#connect"`)
			if tc.mcp {
				check(true, "<code>"+tc.wantMCPURL+"</code>")
				check(tc.wantMCPURL == "/mcp", "Use the site address in your browser")
			}
			check(tc.requireSignIn, "MCP asks you to sign in even for public tools")
			check(tc.oauth, "claude mcp add", "opencode mcp add", "Also allow posting publicly", "app last refreshes its access",
				"and remove all token and app access")
			check(tc.mcp && tc.accounts && !tc.oauth, "Set up an API token")
			check(tc.accounts && !tc.oauth, "and revoke every token")
			check(tc.accounts, `id="api-tokens"`, `href="#api-tokens"`, `href="#revoke"`, authBase+"/api/v1/me/profile",
				"curl --header @-", "can read your email", "Only if you also allowed posting as you",
				"wait in <strong>Agent drafts</strong> until you publish them")
			check(tc.drafts, `href="#drafts"`, `href="/me/drafts"`)
		})
	}
}
