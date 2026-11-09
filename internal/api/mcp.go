package api

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/appmcp"
	"golang.org/x/time/rate"
)

// WithMCP serves the public agent tools at /mcp when config.Enabled.
func WithMCP(config appmcp.Config) Option {
	return func(a *api) { a.mcp = config }
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
	handler := appmcp.Handler(appmcp.Deps{Events: a.events, Details: interestDetails{a}, Now: a.clock, Logger: a.logger, Describe: describeForAgents})
	e.Any("/mcp", echo.WrapHandler(handler), a.mcpBoundary(newIPLimiter(rate, a.clock), make(chan struct{}, inFlight)))
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
//   - concurrent requests are bounded and excess is refused, not queued;
//   - bodies are capped and responses kept out of caches.
func (a *api) mcpBoundary(limiter *ipLimiter, inFlight chan struct{}) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Applied as headers are written: the transport sets its own no-cache,
			// which would still allow intermediaries to store responses.
			c.Response().Before(func() { c.Response().Header().Set("Cache-Control", "no-store") })
			if origin := c.Request().Header.Get("Origin"); origin != "" && !a.allowedOrigin(origin) {
				return mcpError(c, http.StatusForbidden, 0, "cross-origin requests are not allowed")
			}
			if ok, wait := limiter.allow(c.RealIP()); !ok {
				return mcpError(c, http.StatusTooManyRequests, max(1, int(math.Ceil(wait.Seconds()))), "too many requests; retry after the Retry-After delay")
			}
			select {
			case inFlight <- struct{}{}:
				defer func() { <-inFlight }()
			default:
				return mcpError(c, http.StatusServiceUnavailable, 1, "busy; retry shortly")
			}
			if c.Request().Body != nil {
				body, err := io.ReadAll(http.MaxBytesReader(c.Response(), c.Request().Body, mcpBodyLimit))
				if err != nil {
					return mcpError(c, http.StatusRequestEntityTooLarge, 0, "request body too large")
				}
				if trimmed := bytes.TrimLeft(body, " \t\r\n"); len(trimmed) > 0 && trimmed[0] == '[' {
					return mcpError(c, http.StatusBadRequest, 0, "JSON-RPC batch requests are not supported; send one request per POST")
				}
				c.Request().Body = io.NopCloser(bytes.NewReader(body))
			}
			return next(c)
		}
	}
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
