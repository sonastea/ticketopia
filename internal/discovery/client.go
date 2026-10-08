package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sync"
	"time"
)

var ErrNotFound = errors.New("event not found")

// Coordinator persists request accounting across processes/restarts. A failed
// coordinator must fail closed, never silently permit an unbudgeted request.
type Coordinator interface {
	Wait(context.Context) error
	Pause(context.Context, time.Duration) error
}

// UnavailableError deliberately carries no request URL, API key, or provider body.
type UnavailableError struct {
	RetryAfter time.Duration
}

func (e *UnavailableError) Error() string { return "event provider temporarily unavailable" }

type requestGate struct {
	mu       sync.Mutex
	next     time.Time
	blocked  time.Time
	window   time.Time
	used     int
	budget   int
	interval time.Duration
}

func (g *requestGate) wait(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := time.Now()
		g.mu.Lock()
		if g.window.IsZero() || !now.Before(g.window.Add(24*time.Hour)) {
			g.window, g.used = now, 0
		}
		blocked := g.blocked
		if g.used >= g.budget && g.window.Add(24*time.Hour).After(blocked) {
			blocked = g.window.Add(24 * time.Hour)
		}
		if now.Before(blocked) {
			g.mu.Unlock()
			return &UnavailableError{RetryAfter: blocked.Sub(now)}
		}
		delay := g.next.Sub(now)
		if delay <= 0 {
			g.next = now.Add(g.interval)
			g.used++
			g.mu.Unlock()
			return nil
		}
		g.mu.Unlock()
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func (g *requestGate) pause(delay time.Duration) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if until := time.Now().Add(delay); until.After(g.blocked) {
		g.blocked = until
	}
}

func (s *Service) get(ctx context.Context, path string, query url.Values, target any) error {
	if s.key == "" {
		return &UnavailableError{RetryAfter: time.Minute}
	}
	var gateErr error
	if s.coordinator != nil {
		// Keep this process's cooldown even if persisting it failed transiently.
		s.gate.mu.Lock()
		remaining := time.Until(s.gate.blocked)
		s.gate.mu.Unlock()
		if remaining > 0 {
			gateErr = &UnavailableError{RetryAfter: remaining}
		} else {
			gateErr = s.coordinator.Wait(ctx)
		}
	} else {
		gateErr = s.gate.wait(ctx)
	}
	if gateErr != nil {
		return gateErr
	}
	query.Set("apikey", s.key)
	query.Set("locale", "en")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.baseURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return s.unavailable(30 * time.Second)
	}
	req.Header.Set("Accept", "application/json")
	res, err := s.client.Do(req)
	if err != nil {
		// net/http errors include the URL (and therefore the secret query key).
		return s.unavailable(30 * time.Second)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if res.StatusCode == http.StatusBadRequest {
		// A rejected filter must not pause unrelated searches or expose the body.
		return &ValidationError{Field: "query", Message: "Ticketmaster could not use these filters; try a different search"}
	}
	if res.StatusCode != http.StatusOK {
		delay := 30 * time.Second
		if res.StatusCode == http.StatusTooManyRequests {
			delay = time.Minute
		}
		if parsed := retryDelay(res.Header, time.Now()); parsed > delay {
			delay = parsed
		}
		return s.unavailable(delay)
	}
	// A successful last-quota response remains usable; later misses wait for reset.
	if res.Header.Get("Rate-Limit-Available") == "0" {
		delay := retryDelay(res.Header, time.Now())
		if delay <= 0 {
			delay = 24 * time.Hour
		}
		s.pause(delay)
	}
	const maxBody = 8 << 20
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody+1))
	if err != nil || len(body) > maxBody || json.Unmarshal(body, target) != nil {
		return s.unavailable(30 * time.Second)
	}
	return nil
}

func (s *Service) unavailable(delay time.Duration) error {
	s.pause(delay)
	s.logger.Warn().Dur("retry_after", delay).Msg("Ticketmaster unavailable; pausing external requests")
	return &UnavailableError{RetryAfter: delay}
}

func (s *Service) pause(delay time.Duration) {
	s.gate.pause(delay)
	if s.coordinator != nil {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(s.ctx), 3*time.Second)
		defer cancel()
		if err := s.coordinator.Pause(ctx, delay); err != nil {
			s.logger.Error().Err(err).Msg("Unable to persist Ticketmaster cooldown")
		}
	}
}

func retryDelay(headers http.Header, now time.Time) time.Duration {
	var delay time.Duration
	if seconds, err := strconv.ParseInt(headers.Get("Retry-After"), 10, 32); err == nil && seconds > 0 {
		delay = time.Duration(seconds) * time.Second
	} else if date, err := http.ParseTime(headers.Get("Retry-After")); err == nil {
		delay = date.Sub(now)
	}
	// Ticketmaster documents Rate-Limit-Reset as Unix milliseconds. It describes
	// the daily quota, so do not apply it to ordinary per-second throttling.
	if headers.Get("Rate-Limit-Available") == "0" {
		if millis, err := strconv.ParseInt(headers.Get("Rate-Limit-Reset"), 10, 64); err == nil {
			if reset := time.UnixMilli(millis).Sub(now); reset > delay {
				delay = reset
			}
		}
	}
	return min(max(delay, 0), 24*time.Hour)
}
