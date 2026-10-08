package history

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

var ErrNoTask = errors.New("no collection task due")
var ErrLeaseLost = errors.New("collection ownership expired")

type Repository interface {
	Schedule(context.Context, []Task) error
	Claim(context.Context, []string) (Claim, error)
	Page(context.Context, Claim, int, models.EventList) error
	Finish(context.Context, Claim, string, string, time.Duration) (Outcome, error)
}

type Provider interface {
	Collect(context.Context, discovery.Query) (models.EventList, error)
}

type Collector struct {
	repository Repository
	provider   Provider
	config     Config
	logger     zerolog.Logger
	now        func() time.Time
	day        string
	ids        []string
}

func New(repository Repository, provider Provider, config Config, logger zerolog.Logger) *Collector {
	return &Collector{repository: repository, provider: provider, config: config, logger: logger, now: time.Now}
}

// Run stops before its owning SQL pool closes. Durable leases recover abandoned
// runs after a crash, with fencing checked on every page and completion.
func (c *Collector) Run(ctx context.Context) {
	for ctx.Err() == nil {
		worked, err := c.Once(ctx)
		if err != nil && ctx.Err() == nil {
			c.logger.Error().Err(err).Msg("Event history collection failed")
		}
		if worked && err == nil {
			continue
		}
		timer := time.NewTimer(time.Minute)
		select {
		case <-ctx.Done():
			timer.Stop()
		case <-timer.C:
		}
	}
}

func (c *Collector) Once(ctx context.Context) (bool, error) {
	now := c.now().UTC()
	day := now.Format(time.DateOnly)
	if c.day != day {
		tasks := []Task{}
		ids := []string{}
		for _, scope := range c.config.Cities {
			for offset := range c.config.Days {
				task := NewTask(scope, now.AddDate(0, 0, offset).Format(time.DateOnly))
				tasks = append(tasks, task)
				ids = append(ids, task.ID)
			}
		}
		if err := c.repository.Schedule(ctx, tasks); err != nil {
			return false, err
		}
		c.day, c.ids = day, ids
	}
	claim, err := c.repository.Claim(ctx, c.ids)
	if errors.Is(err, ErrNoTask) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	q := discovery.Query{City: claim.City, Country: claim.Country, StartDate: claim.Date, EndDate: claim.Date, Limit: 100}
	status, failure, delay := "complete", "", c.config.Interval
	pages := 0
	// Ten 100-item pages are the provider's accessible 1,000-result window.
	for q.Page = 0; q.Page < 10; q.Page++ {
		fetchCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		list, err := c.provider.Collect(fetchCtx, q)
		cancel()
		if err == nil && (list.Meta.Stale || list.Meta.DataAsOf.IsZero()) {
			err = errors.New("collection requires fresh provider data")
		}
		if err != nil {
			status, failure, delay = "failed", "provider_unavailable", time.Hour
			if pages > 0 {
				status = "partial"
			}
			var unavailable *discovery.UnavailableError
			if errors.As(err, &unavailable) {
				delay = max(delay, unavailable.RetryAfter)
			}
			if ctx.Err() != nil {
				failure = "interrupted"
			}
			break
		}
		if err := c.repository.Page(ctx, claim, q.Page, list); err != nil {
			// No automatic replay of uncertain commits; lease recovery retains any
			// committed pages and marks this attempt interrupted.
			return true, err
		}
		pages++
		if list.Limited {
			status, failure = "limited", "provider_result_cap"
			break
		}
		if list.NextCursor == nil {
			break
		}
		if q.Page == 9 {
			status, failure = "limited", "provider_result_cap"
		}
	}
	// A cancelled caller still gets a bounded best-effort failure receipt.
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	outcome, err := c.repository.Finish(finishCtx, claim, status, failure, delay)
	if err != nil {
		return true, err
	}
	c.logger.Info().Str("city", claim.City).Str("date", claim.Date).Str("coverage", outcome.Status).Str("failure", outcome.Failure).Int("pages", pages).Msg("Event history refreshed")
	return true, nil
}
