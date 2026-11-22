package api

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
)

// DefaultLiveReplyChecks is each account's live reply checks per minute per
// replica: five open conversations at the browser's ten-second pace.
const DefaultLiveReplyChecks = 30

// WithLiveReplyChecks sets each account's live reply checks per minute per
// replica; 0 turns live replies off, so conversations update on reload only.
func WithLiveReplyChecks(perMinute int) Option {
	return func(a *api) { a.liveReplies = &replyChecks{limit: perMinute} }
}

// replyChecks is a fixed one-minute window per account. It bounds a runaway
// or shared session, not normal use, so a replica may briefly allow up to
// twice the limit across a window boundary. It limits reply reads, not the
// session lookup before it, which every request to an account route pays.
// At most maxReplyCheckAccounts accounts are tracked a minute; beyond that,
// new accounts wait for the next window rather than growing memory.
type replyChecks struct {
	mu     sync.Mutex
	limit  int
	window time.Time
	counts map[string]int
}

const maxReplyCheckAccounts = 100_000

func (r *replyChecks) enabled() bool { return r != nil && r.limit > 0 }

// allow counts one check, returning the seconds until the window resets
// when the account is over its limit.
func (r *replyChecks) allow(account string, now time.Time) (bool, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now.Sub(r.window) >= time.Minute || r.counts == nil {
		r.window, r.counts = now, map[string]int{}
	}
	n, known := r.counts[account]
	if n >= r.limit || (!known && len(r.counts) >= maxReplyCheckAccounts) {
		return false, max(1, int(math.Ceil((time.Minute - now.Sub(r.window)).Seconds())))
	}
	r.counts[account]++
	return true, 0
}

// liveRepliesEnabled is whether conversations offer live reply checks.
func (a *api) liveRepliesEnabled() bool { return a.discussionsEnabled() && a.liveReplies.enabled() }

// newRepliesHandler answers an open conversation's live check: other
// people's visible replies since its as_of time. It serves the browser only
// (session cookie), like the conversation it updates; agents read threads.
func (a *api) newRepliesHandler(c echo.Context) error {
	privateAccountResponse(c)
	if !a.liveRepliesEnabled() {
		return accountProblem(c, discussions.ErrUnavailable)
	}
	p, err := a.principal(c, true)
	if err != nil {
		return accountProblem(c, err)
	}
	for key, values := range c.QueryParams() {
		if key != "since" || len(values) != 1 {
			return accountProblem(c, &accounts.ValidationError{Field: key, Message: "send one since value"})
		}
	}
	since, err := time.Parse(time.RFC3339Nano, c.QueryParam("since"))
	if err != nil {
		return accountProblem(c, &accounts.ValidationError{Field: "since", Message: "use the as_of time from the conversation or the previous check"})
	}
	if ok, wait := a.liveReplies.allow(p.Account.ID, a.clock()); !ok {
		c.Response().Header().Set("Retry-After", strconv.Itoa(wait))
		return c.JSON(http.StatusTooManyRequests, map[string]any{"code": "rate_limited", "detail": "Checking too often; the conversation will check again shortly."})
	}
	event, err := savedEventID(c)
	if err != nil {
		return accountProblem(c, err)
	}
	out, err := a.discussions.NewReplies(c.Request().Context(), p.Account.ID, event, c.Param("discussion_id"), since)
	if err != nil {
		return accountProblem(c, err)
	}
	return c.JSON(http.StatusOK, out)
}

// LiveReplyChecksFromEnv reads DISCUSSION_LIVE_CHECKS_PER_MINUTE: each
// account's live reply checks per minute per replica, 0 (off) to 6000.
// Unset, empty or blank means DefaultLiveReplyChecks. The upper range is for
// benchmarks, whose simulated participants share a few accounts.
func LiveReplyChecksFromEnv() (int, error) {
	raw := strings.TrimSpace(os.Getenv("DISCUSSION_LIVE_CHECKS_PER_MINUTE"))
	if raw == "" {
		return DefaultLiveReplyChecks, nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 || v > 6000 {
		return 0, fmt.Errorf("DISCUSSION_LIVE_CHECKS_PER_MINUTE must be between 0 (off) and 6000")
	}
	return v, nil
}
