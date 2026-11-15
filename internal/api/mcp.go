package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/appmcp"
	"github.com/sonastea/ticketopia/internal/oauth"
	"golang.org/x/net/idna"
	"golang.org/x/time/rate"
)

// WithMCP serves the agent tools at /mcp when config.Enabled: public tools,
// and signed-in tools for requests with a valid API token.
func WithMCP(config appmcp.Config) Option {
	return func(a *api) { a.mcp = config }
}

// WithMCPOAuth lets agents sign in to /mcp through OAuth when MCP and
// config.OAuth are enabled and accounts are on.
func WithMCPOAuth(service *oauth.Service) Option {
	return func(a *api) { a.oauth = service }
}

// signInRequired reports whether every /mcp request needs a valid token.
func (a *api) signInRequired() bool {
	return a.mcp.Enabled && a.mcp.RequireSignIn && a.enabled()
}

// oauthEnabled reports whether OAuth sign-in for /mcp is served.
func (a *api) oauthEnabled() bool {
	return a.mcp.Enabled && a.mcp.OAuth && a.oauth != nil && a.enabled()
}

const (
	// mcpBodyLimit bounds one JSON-RPC request; tool arguments are a few fields.
	mcpBodyLimit = 64 << 10
	// mcpClients caps tracked client IPs per replica, like live-activity starts.
	mcpClients = 4096
)

func (a *api) mcpRoutes(e *echo.Echo) {
	if !a.mcp.Enabled {
		return
	}
	rate, inFlight := a.mcp.RatePerMinute, a.mcp.MaxInFlight
	if rate < 1 {
		rate = appmcp.DefaultRatePerMinute
	}
	if inFlight < 1 {
		inFlight = appmcp.DefaultMaxInFlight
	}
	metrics := appmcp.NewMetrics()
	go metrics.Run(a.runCtx, a.logger, time.Minute)
	deps := appmcp.Deps{Events: a.events, Details: interestDetails{a}, Now: a.clock, Logger: a.logger, Describe: describeForAgents, Metrics: metrics,
		DescribePrivate: func(err error) string { _, _, detail, _ := accountError(err); return detail }}
	// Signed-in tools need accounts; assign only real services, never typed nils.
	if a.enabled() && a.saved != nil {
		deps.Saved = a.saved
		if a.reminders != nil {
			deps.Reminders = a.reminders
		}
	}
	if a.enabled() {
		deps.Accounts, deps.Profiles = a.accounts, a.accounts
		if a.radar != nil {
			deps.Radar = a.radar
		}
		if a.follows != nil {
			deps.Follows, deps.Catalog = a.follows, a.events
			deps.DescribeCatalog = describeCatalogForAgents
		}
		if a.interests != nil {
			deps.Interests = a.interests
		}
		if a.recommendations != nil {
			deps.Recommendations = a.recommendations
			// Drafts are published as recommendations, so they need both.
			if a.drafts != nil {
				deps.Drafts, deps.DraftsURL = a.drafts, strings.TrimRight(a.authConfig.BaseURL, "/")+"/me/drafts"
			}
		}
		deps.PublishLimit = appmcp.NewPublishLimiter(appmcp.PublishesPerHour, a.clock)
		// Community reads are public tools: they serve agents exactly what
		// /api/v1/events/{id}/community and /users/{id}/activity serve.
		if a.eventCommunityEnabled() {
			deps.EventCommunity = communityReads{a}
		}
		if a.userActivityEnabled() {
			deps.ProfileActivity = communityReads{a}
		}
		if a.recommendationsEnabled() {
			deps.EventRecommendations, deps.CommunityRecommendations = a.recommendations, a.recommendations
		}
		if a.interestEnabled() {
			deps.InterestedPeople = a.interests
		}
		if a.discussionsEnabled() {
			deps.Discussions = a.discussions
		}
	}
	boundary := a.mcpBoundary(newIPLimiter(rate, a.clock), newIPLimiter(rate, a.clock), make(chan struct{}, inFlight), metrics)
	e.Any("/mcp", echo.WrapHandler(mcpAuthenticated(appmcp.Handler(deps))), boundary)
	if a.oauthEnabled() {
		a.oauthRoutes(e, newIPLimiter(rate, a.clock))
	}
}

type mcpCallerKey struct{}

// mcpCaller is the account behind a verified API token or OAuth access token.
// API tokens carry read and write (apiTokenScopes); OAuth connections carry
// what the person allowed, plus the app's name and client ID.
type mcpCaller struct {
	account    string
	scopes     []string
	clientName string
	clientID   string
	grantID    string
}

// apiTokenScopes are listed explicitly, never derived from oauth.Scopes, so
// a new scope such as publish is never granted to API tokens by accident.
var apiTokenScopes = []string{oauth.ScopeRead, oauth.ScopeWrite}

func (c mcpCaller) can(scope string) bool { return slices.Contains(c.scopes, scope) }

// mcpAuthenticated hands the account the boundary verified to the tools as
// MCP token info. The SDK middleware only runs for requests carrying a token,
// so anonymous agents keep the public tools. The boundary's account check is
// the only token check, including expiry: the SDK's own would answer 401
// without the Bearer challenge for a token expiring mid-request.
func mcpAuthenticated(handler http.Handler) http.Handler {
	bearer := auth.RequireBearerToken(func(_ context.Context, _ string, r *http.Request) (*auth.TokenInfo, error) {
		caller, ok := r.Context().Value(mcpCallerKey{}).(mcpCaller)
		if !ok {
			return nil, auth.ErrInvalidToken
		}
		info := &auth.TokenInfo{UserID: caller.account, Scopes: caller.scopes}
		if caller.clientName != "" {
			info.Extra = map[string]any{appmcp.ExtraClientName: caller.clientName, appmcp.ExtraClientHost: attributionHost(caller.clientID),
				appmcp.ExtraGrantID: caller.grantID}
		}
		return info, nil
	}, &auth.RequireBearerTokenOptions{AllowMissingExpiration: true})(handler)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			bearer.ServeHTTP(w, r)
			return
		}
		handler.ServeHTTP(w, r)
	})
}

// attributionHost is the client's metadata host in canonical ASCII (IDNA)
// form, as shown publicly in "via" labels, so lookalike Unicode is visible
// and every valid DNS name fits the stored 253 bytes.
func attributionHost(clientID string) string {
	host := strings.ToLower(oauth.Client{ID: clientID}.Host())
	if ascii, err := idna.Lookup.ToASCII(host); err == nil {
		return ascii
	}
	return host
}

// describeForAgents is the JSON API's error wording plus its Retry-After
// delay, which a tool error cannot carry as a header.
func describeForAgents(err error) string {
	_, message := errorMessage(err)
	if seconds, ok := retryAfterSeconds(err); ok {
		message += fmt.Sprintf(" Retry after %d seconds.", seconds)
	}
	return message
}

// describeCatalogForAgents words artist and venue search errors as
// /api/v1/artists and /venues do, plus their Retry-After delay.
func describeCatalogForAgents(err error) string {
	_, _, detail, _ := accountError(catalogError(err))
	if seconds, ok := retryAfterSeconds(err); ok {
		detail += fmt.Sprintf(" Retry after %d seconds.", seconds)
	}
	return detail
}

func mcpError(c echo.Context, status int, retry int, message string) error {
	if retry > 0 {
		c.Response().Header().Set("Retry-After", strconv.Itoa(retry))
	}
	return c.JSON(status, map[string]string{"error": message})
}

// mcpBoundary protects /mcp before the transport runs:
//   - browser requests must come from an allowed origin (the MCP transport
//     requires Origin validation against DNS rebinding; CORS stays disabled);
//   - each client IP is rate limited on this replica, and one request is one
//     call: JSON-RPC batches are rejected so a request cannot carry many;
//   - a bearer API token or OAuth access token, if sent, must be valid, and
//     its account is limited too; cookies are never used, so browsers cannot
//     act with ambient sign-in;
//   - with sign-in required, any request without a token gets a 401
//     challenge; otherwise, with OAuth on, signed-in tool calls without one
//     do; a tool needing a scope the token lacks (write, or publish, which
//     API tokens never hold) gets a 403;
//   - concurrent requests are bounded and excess is refused, not queued;
//   - bodies are capped and responses kept out of caches.
func (a *api) mcpBoundary(limiter, accountLimiter *ipLimiter, inFlight chan struct{}, metrics *appmcp.Metrics) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Applied as headers are written: the transport sets its own no-cache,
			// which would still allow intermediaries to store responses.
			c.Response().Before(func() { c.Response().Header().Set("Cache-Control", "no-store") })
			if origin := c.Request().Header.Get("Origin"); origin != "" && !a.allowedOrigin(origin) {
				metrics.Refuse(appmcp.RefusedOrigin)
				return mcpError(c, http.StatusForbidden, 0, "cross-origin requests are not allowed")
			}
			if ok, wait := limiter.allow(c.RealIP()); !ok {
				metrics.Refuse(appmcp.RefusedIPRate)
				return mcpError(c, http.StatusTooManyRequests, max(1, int(math.Ceil(wait.Seconds()))), "too many requests; retry after the Retry-After delay")
			}
			select {
			case inFlight <- struct{}{}:
				defer func() { <-inFlight }()
			default:
				metrics.Refuse(appmcp.RefusedBusy)
				return mcpError(c, http.StatusServiceUnavailable, 1, "busy; retry shortly")
			}
			// Token checks read the database, so they run inside the concurrency bound.
			var caller *mcpCaller
			if header := c.Request().Header.Get("Authorization"); header != "" {
				verified, err := a.mcpCaller(c, header)
				if err != nil {
					if errors.Is(err, accounts.ErrUnauthenticated) {
						c.Response().Header().Set("WWW-Authenticate", a.mcpChallenge("invalid_token", ""))
						metrics.Refuse(appmcp.RefusedUnauthorized)
						return mcpError(c, http.StatusUnauthorized, 0, "invalid, expired or revoked token")
					}
					metrics.Refuse(appmcp.RefusedAccountsUnavailable)
					return mcpError(c, http.StatusServiceUnavailable, 30, "accounts are unavailable; retry shortly")
				}
				if ok, wait := accountLimiter.allow(verified.account); !ok {
					metrics.Refuse(appmcp.RefusedAccountRate)
					return mcpError(c, http.StatusTooManyRequests, max(1, int(math.Ceil(wait.Seconds()))), "too many requests for this account; retry after the Retry-After delay")
				}
				caller = &verified
				c.SetRequest(c.Request().WithContext(context.WithValue(c.Request().Context(), mcpCallerKey{}, verified)))
			}
			// With sign-in required, every request needs a token, so all agent
			// traffic is attributed to an account and limited per account.
			if caller == nil && a.signInRequired() {
				c.Response().Header().Set("WWW-Authenticate", a.mcpChallenge("", strings.Join(oauth.DefaultScopes, " ")))
				metrics.Refuse(appmcp.RefusedSignInRequired)
				return mcpError(c, http.StatusUnauthorized, 0, "sign in to Ticketopia to use its agent tools")
			}
			if c.Request().Body != nil {
				body, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, mcpBodyLimit))
				if err != nil {
					metrics.Refuse(appmcp.RefusedTooLarge)
					return mcpError(c, http.StatusRequestEntityTooLarge, 0, "request body too large")
				}
				if trimmed := bytes.TrimLeft(body, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
					metrics.Refuse(appmcp.RefusedBatch)
					return mcpError(c, http.StatusBadRequest, 0, "JSON-RPC batch requests are not supported; send one request per POST")
				}
				// Scope checks apply to API tokens too, with or without OAuth:
				// API tokens never hold publish.
				if handled, err := a.mcpAuthorizeCall(c, body, caller, metrics); handled {
					return err
				}
				c.Request().Body = io.NopCloser(bytes.NewReader(body))
			}
			return next(c)
		}
	}
}

// mcpCaller verifies a bearer token: an OAuth access token when OAuth is on,
// otherwise an account API token as /api/v1 accepts. Accounts being off means
// no token can be valid.
func (a *api) mcpCaller(c echo.Context, header string) (mcpCaller, error) {
	// The scheme is case-insensitive, as the MCP SDK parses it.
	fields := strings.Fields(header)
	if len(fields) != 2 || !strings.EqualFold(fields[0], "bearer") || !a.enabled() || a.accounts == nil {
		return mcpCaller{}, accounts.ErrUnauthenticated
	}
	if strings.HasPrefix(fields[1], oauth.AccessPrefix) {
		if !a.oauthEnabled() {
			return mcpCaller{}, accounts.ErrUnauthenticated
		}
		access, err := a.oauth.Authenticate(c.Request().Context(), fields[1])
		if errors.Is(err, oauth.ErrUnauthenticated) {
			return mcpCaller{}, accounts.ErrUnauthenticated
		}
		if err != nil {
			return mcpCaller{}, err
		}
		return mcpCaller{access.AccountID, access.Scopes, access.ClientName, access.ClientID, access.GrantID}, nil
	}
	p, err := a.authenticateRequest(c, fields[1], "api")
	if err != nil {
		return mcpCaller{}, err
	}
	return mcpCaller{account: p.Account.ID, scopes: slices.Clone(apiTokenScopes)}, nil
}

// mcpChallenge is the WWW-Authenticate value for /mcp. With OAuth on it
// points clients at the protected resource metadata (RFC 9728) and the
// scopes they should request.
func (a *api) mcpChallenge(errorCode, scope string) string {
	challenge := `Bearer realm="ticketopia"`
	if errorCode != "" {
		challenge += `, error="` + errorCode + `"`
	}
	if a.oauthEnabled() {
		challenge += `, resource_metadata="` + a.oauth.ResourceMetadataURL() + `"`
		if scope != "" {
			challenge += `, scope="` + scope + `"`
		}
	}
	return challenge
}

// mcpAuthorizeCall answers with HTTP challenges so clients start sign-in
// (401, with OAuth on) or ask the person to allow changes or posting (403
// insufficient_scope naming the scopes to request) instead of showing a tool
// error. Unless sign-in is required (checked earlier for every request),
// initialize, tools/list and public tools stay anonymous.
// It reports whether it answered the request, which then must not continue.
func (a *api) mcpAuthorizeCall(c echo.Context, body []byte, caller *mcpCaller, metrics *appmcp.Metrics) (bool, error) {
	var call struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(body, &call) != nil || call.Method != "tools/call" {
		return false, nil
	}
	scope, signedIn := appmcp.SignedInTools[call.Params.Name]
	if !signedIn {
		return false, nil
	}
	// Ask for publish only for the tools that need it, so ordinary sign-in
	// never offers it.
	requested := strings.Join(oauth.DefaultScopes, " ")
	if scope == oauth.ScopePublish {
		requested += " " + oauth.ScopePublish
	}
	if caller == nil {
		if !a.oauthEnabled() {
			// Without OAuth the tool explains how to connect with an API token.
			return false, nil
		}
		c.Response().Header().Set("WWW-Authenticate", a.mcpChallenge("", requested))
		metrics.Refuse(appmcp.RefusedSignInRequired)
		return true, mcpError(c, http.StatusUnauthorized, 0, "sign in to Ticketopia to use "+call.Params.Name)
	}
	if !caller.can(scope) {
		c.Response().Header().Set("WWW-Authenticate", a.mcpChallenge("insufficient_scope", requested))
		metrics.Refuse(appmcp.RefusedInsufficientScope)
		message := "this connection is read-only; reconnect and allow changes to use " + call.Params.Name
		if scope == oauth.ScopePublish {
			message = "this connection can't post publicly; reconnect and allow posting publicly to use " + call.Params.Name
		}
		return true, mcpError(c, http.StatusForbidden, 0, message)
	}
	return false, nil
}

// allowedOrigin accepts configured origins (MCP_ALLOWED_ORIGINS, default
// AUTH_BASE_URL) and loopback origins for local tools such as the MCP
// Inspector. The request's own Host is never trusted: under DNS rebinding an
// attacker controls both.
func (a *api) allowedOrigin(raw string) bool {
	origin, ok := appmcp.Origin(raw)
	if !ok {
		return false
	}
	u, _ := url.Parse(origin)
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return slices.Contains(a.mcp.AllowedOrigins, origin)
}

// ipLimiter is a per-replica token bucket per client IP: perMinute requests
// per minute, with bursts up to a minute's allowance. Idle entries are swept
// at most once a minute, and at most mcpClients IPs are tracked: new IPs are
// refused while it is full, as for live-activity starts, so rotating
// addresses can neither grow it nor make each request rescan it.
type ipLimiter struct {
	mu        sync.Mutex
	perMinute int
	now       func() time.Time
	clients   map[string]*client
	swept     time.Time
}

type client struct {
	limiter *rate.Limiter
	seen    time.Time
}

func newIPLimiter(perMinute int, now func() time.Time) *ipLimiter {
	return &ipLimiter{perMinute: perMinute, now: now, clients: map[string]*client{}}
}

func (l *ipLimiter) allow(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	if now.Sub(l.swept) > time.Minute {
		l.sweep(now)
	}
	c, ok := l.clients[ip]
	if !ok {
		if len(l.clients) >= mcpClients {
			return false, time.Minute
		}
		c = &client{limiter: rate.NewLimiter(rate.Limit(float64(l.perMinute)/60), l.perMinute)}
		l.clients[ip] = c
	}
	c.seen = now
	reservation := c.limiter.ReserveN(now, 1)
	if wait := reservation.DelayFrom(now); wait > 0 {
		reservation.CancelAt(now)
		return false, wait
	}
	return true, 0
}

// sweep drops clients idle for a minute (a full bucket refills within that,
// so they carry no state) and reallocates the map so its peak size is freed.
func (l *ipLimiter) sweep(now time.Time) {
	kept := make(map[string]*client, len(l.clients))
	for key, c := range l.clients {
		if now.Sub(c.seen) <= time.Minute {
			kept[key] = c
		}
	}
	l.clients, l.swept = kept, now
}
