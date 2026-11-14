package api

import (
	"cmp"
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/telemetry"
	otelattribute "go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Request logging keeps logs readable at a glance and small on disk:
// successful requests are not logged; failures and slow requests are counted
// per route and status and logged once a minute; the first server error per
// route each minute and every panic are logged at once. Routes are templates
// (/events/:event_id), never IDs; only paths matching no route are kept, as
// examples of what was probed, truncated and without query strings.
const (
	slowRequest = 2 * time.Second
	// maxRequestGroups bounds one minute's lines however varied the traffic;
	// requests beyond it are counted in a single overflow line.
	maxRequestGroups = 100
	maxProbePaths    = 50
	probeExamples    = 5
	maxLoggedPath    = 100
	maxLoggedError   = 300
	noRoute          = "(no route)"
)

var (
	httpRequests, _ = telemetry.Meter().Int64Counter("ticketopia.http.server.requests", metric.WithUnit("{request}"),
		metric.WithDescription("HTTP requests by method, route and status class (2xx, 3xx, 4xx, 5xx)."))
	httpDuration, _ = telemetry.Meter().Float64Histogram("ticketopia.http.server.duration", metric.WithUnit("s"),
		metric.WithDescription("HTTP request duration by method and route, excluding event streams."),
		metric.WithExplicitBucketBoundaries(telemetry.DurationBuckets...))
)

// requestKey groups responses; status 0 groups slow successful requests.
type requestKey struct {
	method, route string
	status        int
}

type requestGroup struct {
	count    int
	slowest  time.Duration
	reported int // already logged individually
	paths    map[string]int
}

type requestLog struct {
	logger   zerolog.Logger
	mu       sync.Mutex
	groups   map[requestKey]*requestGroup
	overflow int
}

func newRequestLog(logger zerolog.Logger) *requestLog {
	return &requestLog{logger: logger, groups: map[requestKey]*requestGroup{}}
}

// run logs each minute's summary until ctx ends.
func (l *requestLog) run(ctx context.Context, every time.Duration) {
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			l.flush()
		}
	}
}

func (l *requestLog) middleware(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c echo.Context) (err error) {
		path := c.Request().URL.Path
		if path == "/healthz" || path == "/readyz" {
			return next(c)
		}
		start := time.Now()
		defer func() {
			panicked := false
			if r := recover(); r != nil {
				// net/http's own signal to abort the response quietly.
				if r == http.ErrAbortHandler {
					panic(r)
				}
				panicked = true
				stack := debug.Stack()
				if len(stack) > 4096 {
					stack = stack[:4096]
				}
				l.logger.Error().Str("method", c.Request().Method).Str("path", routeOf(c)).Str("panic", truncate(fmt.Sprint(r), maxLoggedError)).
					Bytes("stack", stack).Msg("HTTP handler panicked")
				err = echo.ErrInternalServerError
			}
			// Settle the response here, as Echo would after the chain returns,
			// so the logged status is the one the client gets.
			var handlerErr error
			if err != nil {
				handlerErr = err
				c.Error(err)
				err = nil
			}
			stream := strings.HasPrefix(c.Response().Header().Get(echo.HeaderContentType), "text/event-stream")
			l.observe(c.Request().Method, c.Path(), path, c.Response().Status, time.Since(start), handlerErr, stream, panicked)
		}()
		return next(c)
	}
}

func routeOf(c echo.Context) string {
	if c.Path() == "" {
		return noRoute
	}
	return c.Path()
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func statusClass(status int) string {
	if status < 100 || status > 599 {
		return "other"
	}
	return strconv.Itoa(status/100) + "xx"
}

func knownMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions:
		return method
	}
	return "OTHER"
}

// observe records one response. Long-lived event streams are counted but
// never timed: their duration is how long a reader stayed.
func (l *requestLog) observe(method, route, path string, status int, d time.Duration, err error, stream, panicked bool) {
	method = knownMethod(method)
	metricRoute := route
	if metricRoute == "" {
		metricRoute = "unmatched"
	}
	ctx := context.Background()
	httpRequests.Add(ctx, 1, metric.WithAttributes(otelattribute.String("method", method), otelattribute.String("route", metricRoute),
		otelattribute.String("status_class", statusClass(status))))
	if !stream {
		httpDuration.Record(ctx, d.Seconds(), metric.WithAttributes(otelattribute.String("method", method), otelattribute.String("route", metricRoute)))
	}
	switch {
	case status >= 400:
		l.add(requestKey{method, route, status}, path, d, err, panicked)
	case d > slowRequest && !stream:
		l.add(requestKey{method, route, 0}, path, d, nil, false)
	}
}

func (l *requestLog) add(key requestKey, path string, d time.Duration, err error, panicked bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	g := l.groups[key]
	if g == nil {
		if len(l.groups) >= maxRequestGroups {
			l.overflow++
			return
		}
		g = &requestGroup{}
		l.groups[key] = g
	}
	g.count++
	g.slowest = max(g.slowest, d)
	if key.route == "" {
		path = truncate(path, maxLoggedPath)
		if _, seen := g.paths[path]; seen || len(g.paths) < maxProbePaths {
			if g.paths == nil {
				g.paths = map[string]int{}
			}
			g.paths[path]++
		}
	}
	if key.status < 500 {
		return
	}
	if panicked {
		g.reported++
		return
	}
	if g.reported == 0 {
		g.reported++
		event := l.logger.Error().Str("method", key.method).Str("path", displayRoute(key.route)).Int("status", key.status).
			Int64("durationMs", d.Milliseconds())
		if err != nil {
			event = event.Str("error", truncate(err.Error(), maxLoggedError))
		}
		event.Msg("HTTP request failed")
	}
}

func displayRoute(route string) string {
	if route == "" {
		return noRoute
	}
	return route
}

// flush logs the minute's groups, most severe first, and starts a new minute.
func (l *requestLog) flush() {
	l.mu.Lock()
	groups, overflow := l.groups, l.overflow
	l.groups, l.overflow = map[requestKey]*requestGroup{}, 0
	l.mu.Unlock()
	keys := make([]requestKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b requestKey) int {
		return cmp.Or(cmp.Compare(b.status, a.status), cmp.Compare(a.route, b.route), cmp.Compare(a.method, b.method))
	})
	for _, key := range keys {
		g := groups[key]
		var event *zerolog.Event
		message := "HTTP client errors"
		switch {
		case key.status >= 500:
			if g.count <= g.reported {
				continue // already logged individually
			}
			event, message = l.logger.Error(), "HTTP server errors"
		case key.status == 0:
			event, message = l.logger.Warn(), "Slow HTTP requests"
		case key.status == http.StatusTooManyRequests:
			event = l.logger.Warn()
		case key.status == http.StatusNotFound && key.route != "":
			// A known page or API route answering 404: possibly a broken link.
			event = l.logger.Warn()
		default:
			// Unknown paths (mostly scanners), validation and auth refusals.
			event = l.logger.Info()
		}
		event = event.Str("method", key.method).Str("path", displayRoute(key.route)).Int("count", g.count).
			Int64("durationMs", g.slowest.Milliseconds()).Str("window", "1m")
		if key.status != 0 {
			event = event.Int("status", key.status)
		}
		if len(g.paths) > 0 {
			event = event.Str("examples", examples(g.paths))
		}
		event.Msg(message)
	}
	if overflow > 0 {
		l.logger.Warn().Int("count", overflow).Int("groups", maxRequestGroups).Str("window", "1m").Msg("HTTP responses over the per-minute group limit")
	}
}

// examples lists the most frequent probed paths, for example
// "/wp-login.php ×20, /.env ×3".
func examples(paths map[string]int) string {
	list := make([]string, 0, len(paths))
	for p := range paths {
		list = append(list, p)
	}
	slices.SortFunc(list, func(a, b string) int { return cmp.Or(cmp.Compare(paths[b], paths[a]), cmp.Compare(a, b)) })
	if len(list) > probeExamples {
		list = list[:probeExamples]
	}
	for i, p := range list {
		list[i] = p + " ×" + strconv.Itoa(paths[p])
	}
	return strings.Join(list, ", ")
}
