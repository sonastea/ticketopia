package history

import (
	"context"
	"errors"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/telemetry"
	"go.opentelemetry.io/otel/codes"
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

// AreaRefresher is an optional Provider capability: read a metro search as a
// visitor would, refreshing only its member cities whose coverage is due.
type AreaRefresher interface {
	KeepFresh(context.Context, discovery.Query) error
}

// areaEvery is how often metro areas are checked. A check reads coverage
// only; provider requests happen only for members whose coverage expired.
const areaEvery = 5 * time.Minute

type Collector struct {
	repository Repository
	provider   Provider
	config     Config
	logger     zerolog.Logger
	now        func() time.Time
	day        string
	ids        []string
	detailAt   time.Time
	areaAt     time.Time
}

// horizon is the scheduled dates: from yesterday through Days after today, in
// UTC. That covers every city's local today (a US evening is already
// tomorrow in UTC) and Discover's default range, today through 90 days later.
func (c *Collector) horizon(now time.Time) (string, string, []string) {
	dates := make([]string, 0, c.config.Days+2)
	for offset := -1; offset <= c.config.Days; offset++ {
		dates = append(dates, now.AddDate(0, 0, offset).Format(time.DateOnly))
	}
	return dates[0], dates[len(dates)-1], dates
}

// keepAreasFresh reads each metro scope over the scheduled horizon. Its city
// is covered by its day-by-day tasks; other members refresh through ordinary
// on-demand collection, one broad request each, only when due.
func (c *Collector) keepAreasFresh(ctx context.Context, now time.Time) {
	refresher, ok := c.provider.(AreaRefresher)
	if !ok || now.Sub(c.areaAt) < areaEvery {
		return
	}
	c.areaAt = now
	start, end, _ := c.horizon(now)
	for _, scope := range c.config.Cities {
		if scope.Area == "" {
			continue
		}
		q := discovery.Query{City: scope.City, Country: scope.Country, StartDate: start, EndDate: end, Limit: 1}.WithArea(scope.Area)
		if err := refresher.KeepFresh(ctx, q); err != nil && ctx.Err() == nil {
			c.logger.Warn().Err(err).Str("city", scope.City).Msg("Metro area refresh incomplete")
		}
	}
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

func (c *Collector) Once(ctx context.Context) (_ bool, err error) {
	ctx, span := telemetry.Tracer().Start(ctx, "history.collect")
	defer func() { telemetry.EndSpan(span, err) }()
	now := c.now().UTC()
	if now.Sub(c.detailAt) >= time.Minute {
		if r, ok := c.repository.(DetailRepository); ok {
			if p, ok := c.provider.(DetailProvider); ok {
				c.detailAt = now
				worked, err := RefreshMissing(ctx, r, p)
				if worked || err != nil {
					return worked, err
				}
			}
		}
	}
	day := now.Format(time.DateOnly)
	if c.day != day {
		tasks := []Task{}
		ids := []string{}
		_, _, dates := c.horizon(now)
		for _, scope := range c.config.Cities {
			for _, date := range dates {
				task := NewTask(Scope{City: scope.City, Country: scope.Country}, date)
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
		// Only once the cities' own days are caught up: a metro's city is then
		// covered by its tasks, so the area read refreshes only other members.
		c.keepAreasFresh(ctx, now)
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
	if status == "failed" || status == "partial" {
		span.SetStatus(codes.Error, "")
	}
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	outcome, err := c.repository.Finish(finishCtx, claim, status, failure, delay)
	if err != nil {
		return true, err
	}
	telemetry.LogTrace(c.logger.Info(), ctx).Str("city", claim.City).Str("date", claim.Date).Str("coverage", outcome.Status).Str("failure", outcome.Failure).Int("pages", pages).Msg("Event history refreshed")
	return true, nil
}
