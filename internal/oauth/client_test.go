package oauth_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/oauth"
)

func TestValidClientID(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://claude.ai/oauth/claude-code-client-metadata": true,
		"https://vscode.dev/oauth/client-metadata.json":       true,
		"https://agent.example:443/client":                    true,
		"http://agent.example/client":                         false,
		"https://agent.example":                               false,
		"https://agent.example/":                              false,
		"https://agent.example/client?x=1":                    false,
		"https://agent.example/client#x":                      false,
		"https://user@agent.example/client":                   false,
		"https://agent.example:8443/client":                   false,
		"https://agent.example/a/../client":                   false,
		"https://agent.example/" + strings.Repeat("a", 2048):  false,
	} {
		if got := oauth.ValidClientID(raw); got != want {
			t.Errorf("%q: %v, want %v", raw, got, want)
		}
	}
}

func TestPublicAddress(t *testing.T) {
	for raw, want := range map[string]bool{
		"93.184.215.14": true, "2606:2800:21f:cb07:6820:80da:af6b:8b2c": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "0.0.0.0": false, "::1": false, "fd00::1": false, "fe80::1": false,
		"::ffff:127.0.0.1": false, "::ffff:10.0.0.1": false, "64:ff9b::a00:1": false, "2002:a00:1::": false, "224.0.0.1": false,
	} {
		if got := oauth.PublicAddress(netip.MustParseAddr(raw)); got != want {
			t.Errorf("%s: %v, want %v", raw, got, want)
		}
	}
}

func TestRedirectAllowed(t *testing.T) {
	registered := []string{"http://localhost/callback", "http://127.0.0.1/callback", "https://claude.ai/api/mcp/auth_callback", "cursor://anysphere.cursor-mcp/oauth/callback"}
	for raw, want := range map[string]bool{
		"http://localhost:53682/callback":                  true,
		"http://127.0.0.1:1/callback":                      true,
		"http://127.0.0.1/callback":                        true,
		"https://claude.ai/api/mcp/auth_callback":          true,
		"cursor://anysphere.cursor-mcp/oauth/callback":     true,
		"http://[::1]:53682/callback":                      false, // not registered
		"http://localhost:53682/other":                     false,
		"http://localhost:53682/callback?x=1":              false,
		"https://claude.ai/api/mcp/auth_callback/":         false,
		"https://claude.ai:444/api/mcp/auth_callback":      false,
		"https://claude.ai/api/mcp/auth_callback#x":        false,
		"cursor://anysphere.cursor-mcp/oauth/callback?x":   false,
		"javascript://anysphere.cursor-mcp/oauth/callback": false,
	} {
		if got := oauth.RedirectAllowed(registered, raw); got != want {
			t.Errorf("%q: %v, want %v", raw, got, want)
		}
	}
}

const document = `{"client_id":"https://agent.example/client.json","client_name":"  Agent‮  Name ","redirect_uris":["http://127.0.0.1/callback"],"grant_types":["authorization_code","refresh_token"],"response_types":["code"],"token_endpoint_auth_method":"none"}`

func TestParseDocument(t *testing.T) {
	const id = "https://agent.example/client.json"
	client, err := oauth.ParseDocument(id, []byte(document))
	if err != nil || client.Name != "Agent Name" || !client.Refresh || client.Host() != "agent.example" {
		t.Fatalf("client %+v %v", client, err)
	}
	for name, body := range map[string]string{
		"other id":       strings.Replace(document, `"client_id":"https://agent.example/client.json"`, `"client_id":"https://agent.example/other.json"`, 1),
		"secret":         strings.Replace(document, `"none"`, `"client_secret_basic"`, 1),
		"no name":        strings.Replace(document, `"  Agent‮  Name "`, `"‏"`, 1),
		"no redirects":   strings.Replace(document, `["http://127.0.0.1/callback"]`, `[]`, 1),
		"http redirect":  strings.Replace(document, `http://127.0.0.1/callback`, `http://agent.example/callback`, 1),
		"js redirect":    strings.Replace(document, `http://127.0.0.1/callback`, `javascript:alert(1)`, 1),
		"no code grant":  strings.Replace(document, `"authorization_code","refresh_token"`, `"client_credentials"`, 1),
		"not an object":  `[]`,
		"secret present": strings.Replace(document, `}`, `,"client_secret":"x"}`, 1),
		"methods without none": strings.Replace(document, `"none"`,
			`"none","token_endpoint_auth_methods_supported":["private_key_jwt"]`, 1),
	} {
		var ce *oauth.ClientError
		if _, err := oauth.ParseDocument(id, []byte(body)); !errors.As(err, &ce) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// chatGPTDocument is ChatGPT's published document (MCP SEP-3149): it prefers
// private_key_jwt but also lists none, so it connects as a public client.
const chatGPTDocument = `{"client_id":"https://chatgpt.com/oauth/client.json","client_uri":"https://chatgpt.com/","redirect_uris":["https://chatgpt.com/connector_platform_oauth_redirect"],"token_endpoint_auth_method":"private_key_jwt","token_endpoint_auth_methods_supported":["none","private_key_jwt"],"grant_types":["authorization_code","refresh_token","urn:ietf:params:oauth:grant-type:jwt-bearer"],"response_types":["code"],"client_name":"ChatGPT","logo_uri":"https://persistent.oaistatic.com/sonic/misc/openai-logo.png","token_endpoint_auth_signing_alg":"RS256","jwks_uri":"https://chatgpt.com/oauth/jwks.json"}`

func TestParseDocumentAuthMethods(t *testing.T) {
	const id = "https://chatgpt.com/oauth/client.json"
	client, err := oauth.ParseDocument(id, []byte(chatGPTDocument))
	if err != nil || client.Name != "ChatGPT" || !client.Refresh || client.RedirectURIs[0] != "https://chatgpt.com/connector_platform_oauth_redirect" {
		t.Fatalf("client %+v %v", client, err)
	}
	for name, body := range map[string]string{
		"private_key_jwt only": strings.Replace(chatGPTDocument, `["none","private_key_jwt"]`, `["private_key_jwt"]`, 1),
		"legacy singular only": strings.Replace(chatGPTDocument, `"token_endpoint_auth_methods_supported":["none","private_key_jwt"],`, ``, 1),
		"secret despite none":  strings.Replace(chatGPTDocument, `}`, `,"client_secret":"x"}`, 1),
	} {
		var ce *oauth.ClientError
		if _, err := oauth.ParseDocument(id, []byte(body)); !errors.As(err, &ce) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestDocumentTTL(t *testing.T) {
	for header, want := range map[string]time.Duration{
		"": 10 * time.Minute, "max-age=3600": time.Hour, "public, max-age=5": time.Minute,
		"max-age=999999": 24 * time.Hour, "no-store": 0, "max-age=60, no-cache": 0,
	} {
		if got := oauth.DocumentTTL(header); got != want {
			t.Errorf("%q: %v, want %v", header, got, want)
		}
	}
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func respond(status int, contentType, cache, body string) *http.Response {
	h := http.Header{"Content-Type": {contentType}}
	if cache != "" {
		h.Set("Cache-Control", cache)
	}
	return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader(body))}
}

func TestDocumentsFetchValidateAndCache(t *testing.T) {
	const id = "https://agent.example/client.json"
	var fetches atomic.Int32
	reply := respond(http.StatusOK, "application/json", "max-age=120", document)
	d := oauth.NewDocumentsWithClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		fetches.Add(1)
		if r.URL.String() != id || r.Header.Get("Accept") != "application/json" {
			t.Errorf("request %s %v", r.URL, r.Header)
		}
		copy := *reply
		body, _ := io.ReadAll(reply.Body)
		reply.Body = io.NopCloser(strings.NewReader(string(body)))
		copy.Body = io.NopCloser(strings.NewReader(string(body)))
		return &copy, nil
	})})
	now := time.Now()
	oauth.SetDocumentsNow(d, func() time.Time { return now })
	for range 3 {
		if client, err := d.Client(t.Context(), id); err != nil || client.Name != "Agent Name" {
			t.Fatalf("client %+v %v", client, err)
		}
	}
	if fetches.Load() != 1 {
		t.Fatalf("fetched %d times within max-age", fetches.Load())
	}
	now = now.Add(2 * time.Minute)
	if _, err := d.Client(t.Context(), id); err != nil || fetches.Load() != 2 {
		t.Fatalf("expired entry: %v fetches=%d", err, fetches.Load())
	}
	if _, err := d.Client(t.Context(), "http://agent.example/client.json"); err == nil || fetches.Load() != 2 {
		t.Fatal("invalid client_id was fetched")
	}
}

func TestDocumentsRefuseBadResponses(t *testing.T) {
	const id = "https://agent.example/client.json"
	for name, response := range map[string]*http.Response{
		"redirect":  {StatusCode: http.StatusFound, Header: http.Header{"Location": {"https://evil.example/client.json"}}, Body: io.NopCloser(strings.NewReader(""))},
		"not found": respond(http.StatusNotFound, "application/json", "", document),
		"html":      respond(http.StatusOK, "text/html", "", document),
		"too large": respond(http.StatusOK, "application/json", "", document[:len(document)-1]+`,"x":"`+strings.Repeat("a", 6000)+`"}`),
	} {
		followed := false
		d := oauth.NewDocumentsWithClient(&http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			if r.URL.String() != id {
				followed = true
			}
			return response, nil
		})})
		if _, err := d.Client(t.Context(), id); err == nil || followed {
			t.Errorf("%s: err=%v followed=%v", name, err, followed)
		}
	}
	d := oauth.NewDocumentsWithClient(&http.Client{Transport: roundTrip(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("connection refused")
	})})
	var ce *oauth.ClientError
	if _, err := d.Client(t.Context(), id); err == nil || errors.As(err, &ce) {
		t.Fatalf("network failure should be retryable, not a client error: %v", err)
	}
}

// The production dialer refuses internal addresses after DNS resolution, so
// a client_id naming a host that resolves to loopback is never fetched.
func TestDocumentsRefuseInternalAddresses(t *testing.T) {
	d := oauth.NewDocuments()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://127.0.0.1/client.json", nil)
	_, err := oauth.HTTPClient(d).Do(request)
	if err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("loopback fetch: %v", err)
	}
	request, _ = http.NewRequestWithContext(ctx, http.MethodGet, "https://localhost/client.json", nil)
	if _, err := oauth.HTTPClient(d).Do(request); err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("localhost fetch: %v", err)
	}
}
