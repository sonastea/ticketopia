package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog"
)

func requestLogRoutes(t *testing.T) (*echo.Echo, *requestLog, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	l := newRequestLog(zerolog.New(&logs))
	e := echo.New()
	e.HTTPErrorHandler = httpErrorHandler
	e.Use(l.middleware)
	e.GET("/healthz", func(c echo.Context) error { return c.NoContent(http.StatusServiceUnavailable) })
	e.GET("/ok", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	e.GET("/events/:event_id", func(c echo.Context) error { return echo.ErrNotFound })
	e.GET("/broken", func(c echo.Context) error { return errors.New("database unavailable") })
	e.GET("/panics", func(echo.Context) error { panic("boom") })
	e.GET("/busy", func(c echo.Context) error { return c.NoContent(http.StatusTooManyRequests) })
	return e, l, &logs
}

func logLines(t *testing.T, logs *bytes.Buffer) []map[string]any {
	t.Helper()
	var lines []map[string]any
	for _, raw := range strings.Split(strings.TrimSpace(logs.String()), "\n") {
		if raw == "" {
			continue
		}
		var line map[string]any
		if err := json.Unmarshal([]byte(raw), &line); err != nil {
			t.Fatalf("%v: %s", err, raw)
		}
		lines = append(lines, line)
	}
	logs.Reset()
	return lines
}

func get(e *echo.Echo, path string) int {
	w := httptest.NewRecorder()
	e.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w.Code
}

// Successes and health checks write nothing; the first server error per
// route is logged at once and repeats wait for the minute's summary; panics
// become 500s logged with their stack.
func TestRequestLogImmediateEvents(t *testing.T) {
	e, _, logs := requestLogRoutes(t)
	if get(e, "/ok") != http.StatusOK || get(e, "/healthz") != http.StatusServiceUnavailable {
		t.Fatal("unexpected status")
	}
	if logs.Len() != 0 {
		t.Fatalf("logged a success or health check: %s", logs.String())
	}
	for range 3 {
		if code := get(e, "/broken"); code != http.StatusInternalServerError {
			t.Fatalf("broken: %d", code)
		}
	}
	lines := logLines(t, logs)
	if len(lines) != 1 || lines[0]["message"] != "HTTP request failed" || lines[0]["level"] != "error" || lines[0]["path"] != "/broken" ||
		lines[0]["status"] != float64(500) || !strings.Contains(lines[0]["error"].(string), "database unavailable") {
		t.Fatalf("server errors: %v", lines)
	}
	if code := get(e, "/panics"); code != http.StatusInternalServerError {
		t.Fatalf("panic answered %d", code)
	}
	lines = logLines(t, logs)
	if len(lines) != 1 || lines[0]["message"] != "HTTP handler panicked" || lines[0]["panic"] != "boom" || lines[0]["path"] != "/panics" ||
		!strings.Contains(lines[0]["stack"].(string), "request_log_test.go") {
		t.Fatalf("panic: %v", lines)
	}
}

// A minute's failures become one line per route and status: routes are
// templates, never IDs; unknown paths are summarized with examples.
func TestRequestLogMinuteSummary(t *testing.T) {
	e, l, logs := requestLogRoutes(t)
	for range 3 {
		get(e, "/broken")
	}
	get(e, "/panics")
	for _, id := range []string{"a1", "b2"} {
		get(e, "/events/"+id)
	}
	get(e, "/busy")
	for range 4 {
		get(e, "/wp-login.php")
	}
	get(e, "/.env?token=secret")
	l.observe(http.MethodGet, "/ok", "/ok", http.StatusOK, 3*time.Second, nil, false, false)
	l.observe(http.MethodGet, "/live", "/live", http.StatusOK, time.Hour, nil, true, false) // an event stream
	logs.Reset()

	l.flush()
	got := map[string]map[string]any{}
	for _, line := range logLines(t, logs) {
		got[line["message"].(string)+" "+line["path"].(string)] = line
	}
	if len(got) != 5 {
		t.Fatalf("summary lines: %v", got)
	}
	// One /broken error was logged already; the summary carries the total.
	if s := got["HTTP server errors /broken"]; s["count"] != float64(3) || s["level"] != "error" || s["window"] != "1m" {
		t.Fatalf("server errors: %v", s)
	}
	if s := got["HTTP client errors /events/:event_id"]; s["count"] != float64(2) || s["status"] != float64(404) || s["level"] != "warn" {
		t.Fatalf("missing events: %v", s)
	}
	if s := got["HTTP client errors /busy"]; s["status"] != float64(429) || s["level"] != "warn" {
		t.Fatalf("rate limited: %v", s)
	}
	if s := got["HTTP client errors (no route)"]; s["count"] != float64(5) || s["level"] != "info" || s["examples"] != "/wp-login.php ×4, /.env ×1" {
		t.Fatalf("probes: %v", s)
	}
	if s := got["Slow HTTP requests /ok"]; s["count"] != float64(1) || s["durationMs"] != float64(3000) || s["level"] != "warn" || s["status"] != nil {
		t.Fatalf("slow: %v", s)
	}
	for key := range got {
		if strings.Contains(key, "/panics") || strings.Contains(key, "/live") || strings.Contains(key, "a1") {
			t.Errorf("unexpected line %s", key)
		}
	}
	// The next minute starts empty.
	l.flush()
	if logs.Len() != 0 {
		t.Fatalf("second flush: %s", logs.String())
	}
}

// However varied the failing traffic, a minute yields a bounded number of lines.
func TestRequestLogBoundsGroups(t *testing.T) {
	_, l, logs := requestLogRoutes(t)
	for i := range maxRequestGroups + 25 {
		l.observe(http.MethodGet, "/r"+string(rune('a'+i%26))+string(rune('a'+i/26)), "/x", http.StatusBadRequest, time.Millisecond, nil, false, false)
	}
	l.flush()
	lines := logLines(t, logs)
	last := lines[len(lines)-1]
	if len(lines) != maxRequestGroups+1 || last["message"] != "HTTP responses over the per-minute group limit" || last["count"] != float64(25) {
		t.Fatalf("%d lines, last %v", len(lines), last)
	}
}
