package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/saved"
	"github.com/sonastea/ticketopia/views/layouts"
)

type startWindow struct {
	at time.Time
	n  int
}
type streamStarts struct {
	mu      sync.Mutex
	entries map[string]startWindow
	limit   int
}

func (s *streamStarts) allow(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	if s.entries == nil {
		s.entries = map[string]startWindow{}
	}
	for key, v := range s.entries {
		if now.Sub(v.at) >= time.Minute {
			delete(s.entries, key)
		}
	}
	v := s.entries[ip]
	if v.at.IsZero() {
		if len(s.entries) >= 4096 {
			return false
		}
		v.at = now
	}
	limit := s.limit
	if limit == 0 {
		limit = 60
	}
	if v.n >= limit {
		return false
	}
	v.n++
	s.entries[ip] = v
	return true
}
func WithLiveActivity(service *activity.Service, starts int) Option {
	return func(a *api) { a.liveActivity = service; a.liveStarts.limit = starts }
}
func (a *api) liveActivityRoutes(e *echo.Echo) {
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if a.liveActivity != nil {
				c.SetRequest(c.Request().WithContext(layouts.WithLiveActivity(c.Request().Context())))
			}
			return next(c)
		}
	})
	e.GET("/api/v1/community/activity", a.liveSnapshot)
	e.GET("/api/v1/community/activity/stream", a.liveStream)
	e.GET("/api/v1/events/activity-counts", a.liveCounts)
}
func activityIDs(raw string, required bool, allowed bool) ([]string, error) {
	invalid := func() ([]string, error) {
		return nil, &accounts.ValidationError{Field: "query", Message: "use at most 101 valid event_id values and no other parameters"}
	}
	if len(raw) > 32768 {
		return invalid()
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		return invalid()
	}
	ids := []string{}
	seen := map[string]bool{}
	for key, values := range q {
		if !allowed || key != "event_id" {
			return invalid()
		}
		for _, id := range values {
			if saved.ValidateID(id) != nil {
				return invalid()
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
			if len(ids) > 101 {
				return invalid()
			}
		}
	}
	if required && len(ids) == 0 {
		return invalid()
	}
	return ids, nil
}
func liveUnavailable(c echo.Context) error {
	return liveProblem(c, 503, "activity_unavailable", "Live activity is unavailable.")
}
func liveProblem(c echo.Context, status int, code, detail string) error {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	c.Response().Header().Set("Content-Type", "application/problem+json")
	return c.JSON(status, map[string]any{"type": "about:blank", "title": http.StatusText(status), "status": status, "code": code, "detail": detail})
}
func (a *api) liveSnapshot(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	if _, err := activityIDs(c.Request().URL.RawQuery, false, false); err != nil {
		return accountProblem(c, err)
	}
	if a.liveActivity == nil {
		return liveUnavailable(c)
	}
	snapshot, err := a.liveActivity.Snapshot(c.Request().Context())
	if err != nil {
		return liveUnavailable(c)
	}
	return c.JSON(200, snapshot)
}
func (a *api) liveCounts(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "private, no-store")
	ids, err := activityIDs(c.Request().URL.RawQuery, true, true)
	if err != nil {
		return accountProblem(c, err)
	}
	if a.liveActivity == nil {
		return liveUnavailable(c)
	}
	counts, err := a.liveActivity.Counts(c.Request().Context(), ids)
	if err != nil {
		return liveUnavailable(c)
	}
	return c.JSON(200, counts)
}
func (a *api) liveStream(c echo.Context) error {
	c.Response().Header().Set("Cache-Control", "private, no-store, no-transform")
	ids, err := activityIDs(c.Request().URL.RawQuery, false, true)
	if err != nil {
		return accountProblem(c, err)
	}
	if a.liveActivity == nil {
		return liveUnavailable(c)
	}
	if !a.liveStarts.allow(c.RealIP()) {
		c.Response().Header().Set("Retry-After", "60")
		return liveProblem(c, 429, "rate_limited", "Try again in one minute.")
	}
	updates, release, err := a.liveActivity.Subscribe(ids)
	if err != nil {
		return liveUnavailable(c)
	}
	defer release()
	// Wait for the shared first read before committing streaming headers.
	var first activity.Update
	select {
	case <-c.Request().Context().Done():
		return nil
	case <-a.shutdown:
		return nil
	case <-a.liveActivity.Done():
		return nil
	case first = <-updates:
	}
	if first.Unavailable {
		return liveUnavailable(c)
	}
	response := c.Response()
	response.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	response.Header().Set("X-Accel-Buffering", "no")
	controller := http.NewResponseController(response.Writer)
	send := func(event string, value any) error {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if err = controller.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
			return err
		}
		if _, err = fmt.Fprintf(response, "retry: 5000\nevent: %s\ndata: %s\n\n", event, data); err != nil {
			return err
		}
		return controller.Flush()
	}
	previous := ""
	emit := func(u activity.Update) error {
		if u.Unavailable {
			return send("unavailable", struct{}{})
		}
		encoded, _ := json.Marshal(u.Snapshot.Items)
		if string(encoded) != previous {
			if err := send("activity.snapshot", u.Snapshot); err != nil {
				return err
			}
			previous = string(encoded)
		}
		if u.Counts != nil && len(ids) > 0 {
			return send("counts.snapshot", u.Counts)
		}
		return nil
	}
	if emit(first) != nil {
		return nil
	}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.Request().Context().Done():
			return nil
		case <-a.shutdown:
			return nil
		case <-a.liveActivity.Done():
			return nil
		case u := <-updates:
			if emit(u) != nil || u.Unavailable {
				return nil
			}
		case now := <-ticker.C:
			if send("heartbeat", map[string]time.Time{"as_of": now.UTC()}) != nil {
				return nil
			}
		}
	}
}
