package api

import (
	"math"
	"net/http"
	"net/url"
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

// mcpBodyLimit bounds one JSON-RPC request; tool arguments are a few fields.
const mcpBodyLimit = 64 << 10

func (a *api) mcpRoutes(e *echo.Echo) {
	if !a.mcp.Enabled {
		return
	}
	handler := appmcp.Handler(appmcp.Deps{Events: a.events, Details: interestDetails{a}, Now: a.clock, Logger: a.logger,
		Describe: func(err error) string { _, message := errorMessage(err); return message }})
	e.Any("/mcp", echo.WrapHandler(handler), a.mcpBoundary(newIPLimiter(a.mcp.RatePerMinute, a.clock)))
}

// mcpBoundary rejects browser cross-origin requests (the MCP transport
// requires Origin validation, and CORS stays disabled), limits each client IP
// on this replica, bounds the body, and keeps responses out of caches.
func (a *api) mcpBoundary(limiter *ipLimiter) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			// Applied as headers are written: the transport sets its own no-cache,
			// which would still allow intermediaries to store responses.
			c.Response().Before(func() { c.Response().Header().Set("Cache-Control", "no-store") })
			if origin := c.Request().Header.Get("Origin"); origin != "" && !a.sameOrigin(c, origin) {
				return c.JSON(http.StatusForbidden, map[string]string{"error": "cross-origin requests are not allowed"})
			}
			if ok, wait := limiter.allow(c.RealIP()); !ok {
				c.Response().Header().Set("Retry-After", strconv.Itoa(max(1, int(math.Ceil(wait.Seconds())))))
				return c.JSON(http.StatusTooManyRequests, map[string]string{"error": "too many requests; retry after the Retry-After delay"})
			}
			c.Request().Body = http.MaxBytesReader(c.Response(), c.Request().Body, mcpBodyLimit)
			return next(c)
		}
	}
}

// sameOrigin accepts the request's own host or the configured public origin.
func (a *api) sameOrigin(c echo.Context, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Host == c.Request().Host {
		return true
	}
	base, err := url.Parse(a.authConfig.BaseURL)
	return err == nil && a.authConfig.BaseURL != "" && u.Scheme == base.Scheme && u.Host == base.Host
}

// ipLimiter is a per-replica token bucket per client IP: perMinute requests
// per minute, with bursts up to a minute's allowance. Idle entries are swept
// so the map stays bounded by recently active clients.
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
		for key, c := range l.clients {
			// A full bucket refills within a minute, so older entries carry no state.
			if now.Sub(c.seen) > time.Minute {
				delete(l.clients, key)
			}
		}
		l.swept = now
	}
	c, ok := l.clients[ip]
	if !ok {
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
