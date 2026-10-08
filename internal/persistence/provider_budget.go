package persistence

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
)

type ProviderBudget struct {
	db      *sql.DB
	timeout time.Duration
	key     [32]byte
	budget  int
}

var _ discovery.Coordinator = (*ProviderBudget)(nil)

func (p *Pool) ProviderBudget(key string, budget int) *ProviderBudget {
	return &ProviderBudget{p.db, p.config.QueryTimeout, sha256.Sum256([]byte(strings.TrimSpace(key))), budget}
}

func (g *ProviderBudget) Wait(ctx context.Context) error {
	for {
		var delay time.Duration
		var blocked bool
		err := historyTransaction(ctx, g.db, g.timeout, "provider budget", func(ctx context.Context, tx *sql.Tx) error {
			delay, blocked = 0, false
			if _, err := tx.ExecContext(ctx, `INSERT INTO provider_budgets (key_hash,window_start,used,budget,next_request,blocked_until) VALUES (?,UTC_TIMESTAMP(6),0,?,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE key_hash=key_hash`, g.key[:], g.budget); err != nil {
				return err
			}
			var window, next, cooldown, now time.Time
			var used, budget int
			if err := tx.QueryRowContext(ctx, `SELECT window_start,used,budget,next_request,blocked_until,UTC_TIMESTAMP(6) FROM provider_budgets WHERE key_hash=? FOR UPDATE`, g.key[:]).Scan(&window, &used, &budget, &next, &cooldown, &now); err != nil {
				return err
			}
			if !now.Before(window.Add(24 * time.Hour)) {
				window, used, budget = now, 0, g.budget
			}
			// A lower configured limit takes effect immediately; raising a limit
			// waits for the next window, avoiding mixed-replica overspending.
			budget = min(budget, g.budget)
			if _, err := tx.ExecContext(ctx, `UPDATE provider_budgets SET window_start=?,used=?,budget=? WHERE key_hash=?`, window, used, budget, g.key[:]); err != nil {
				return err
			}
			if used >= budget {
				cooldown = maxTime(cooldown, window.Add(24*time.Hour))
			}
			if cooldown.After(now) {
				delay, blocked = cooldown.Sub(now), true
				return nil
			}
			if next.After(now) {
				delay = next.Sub(now)
				return nil
			}
			_, err := tx.ExecContext(ctx, `UPDATE provider_budgets SET used=used+1,next_request=TIMESTAMPADD(MICROSECOND,250000,UTC_TIMESTAMP(6)) WHERE key_hash=?`, g.key[:])
			return err
		})
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return &discovery.UnavailableError{RetryAfter: time.Minute}
		}
		if blocked {
			return &discovery.UnavailableError{RetryAfter: delay}
		}
		if delay <= 0 {
			return nil
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func maxTime(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func (g *ProviderBudget) Pause(ctx context.Context, delay time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()
	_, err := g.db.ExecContext(ctx, `UPDATE provider_budgets SET blocked_until=GREATEST(blocked_until,TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6))) WHERE key_hash=?`, min(max(delay, 0), 24*time.Hour).Microseconds(), g.key[:])
	if err != nil {
		return safeError("provider cooldown", err)
	}
	return nil
}
