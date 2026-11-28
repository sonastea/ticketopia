package history

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

type fakeRepository struct {
	tasks           []Task
	pages           int
	status, failure string
	delay           time.Duration
	pageErr         error
}

func (r *fakeRepository) Schedule(_ context.Context, tasks []Task) error { r.tasks = tasks; return nil }
func (r *fakeRepository) Claim(_ context.Context, ids []string) (Claim, error) {
	if len(ids) != len(r.tasks) {
		return Claim{}, errors.New("task selection missing")
	}
	return Claim{Task: r.tasks[0], RunID: "fixture"}, nil
}
func (r *fakeRepository) Page(_ context.Context, _ Claim, _ int, _ models.EventList) error {
	r.pages++
	return r.pageErr
}
func (r *fakeRepository) Finish(_ context.Context, _ Claim, status, failure string, delay time.Duration) (Outcome, error) {
	r.status, r.failure, r.delay = status, failure, delay
	return Outcome{Status: status, Failure: failure}, nil
}

type fakeProvider func(context.Context, discovery.Query) (models.EventList, error)

func (p fakeProvider) Collect(ctx context.Context, q discovery.Query) (models.EventList, error) {
	return p(ctx, q)
}

type areaProvider struct {
	fakeProvider
	reads []discovery.Query
}

func (p *areaProvider) KeepFresh(_ context.Context, q discovery.Query) error {
	p.reads = append(p.reads, q)
	return nil
}

// idleRepository has no task due.
type idleRepository struct{ fakeRepository }

func (r *idleRepository) Claim(context.Context, []string) (Claim, error) { return Claim{}, ErrNoTask }

// A metro scope keeps its area fresh over the whole horizon every few minutes,
// once its city's own days are caught up; only the city gets day-by-day tasks.
func TestCollectorKeepsMetroAreasFresh(t *testing.T) {
	busy := &fakeRepository{}
	p0 := &areaProvider{fakeProvider: func(context.Context, discovery.Query) (models.EventList, error) {
		return models.EventList{Meta: models.Freshness{DataAsOf: time.Now()}}, nil
	}}
	c0 := New(busy, p0, Config{Cities: []Scope{{City: "Los Angeles", Country: "US", Area: discovery.AreaMetro}}, Days: 1, Interval: time.Hour}, zerolog.Nop())
	if _, err := c0.Once(t.Context()); err != nil || len(p0.reads) != 0 {
		t.Fatalf("area read while a day was due: %d %v", len(p0.reads), err)
	}
	r := &idleRepository{}
	p := &areaProvider{fakeProvider: func(context.Context, discovery.Query) (models.EventList, error) {
		return models.EventList{Meta: models.Freshness{DataAsOf: time.Now()}}, nil
	}}
	c := New(r, p, Config{Cities: []Scope{{City: "Los Angeles", Country: "US", Area: discovery.AreaMetro}, {City: "Chicago", Country: "US"}}, Days: 90, Interval: 6 * time.Hour}, zerolog.Nop())
	now := time.Date(2026, 11, 27, 3, 0, 0, 0, time.UTC)
	c.now = func() time.Time { return now }
	for range 3 {
		if _, err := c.Once(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(p.reads) != 1 {
		t.Fatalf("area reads within five minutes: %d", len(p.reads))
	}
	q := p.reads[0]
	if q.City != "Los Angeles" || q.Area != discovery.AreaMetro || q.StartDate != "2026-11-26" || q.EndDate != "2027-02-25" {
		t.Fatalf("area read: %+v", q)
	}
	// Discover's default LA range starts on LA's today, the UTC date before.
	if len(r.tasks) != 2*92 || r.tasks[0].Date != "2026-11-26" || r.tasks[0].Area != "" {
		t.Fatalf("scheduled tasks: %d, first %+v", len(r.tasks), r.tasks[0])
	}
	now = now.Add(areaEvery)
	if _, err := c.Once(t.Context()); err != nil || len(p.reads) != 2 {
		t.Fatalf("area not read again after %s: %d %v", areaEvery, len(p.reads), err)
	}
}

func TestCollectorCoverage(t *testing.T) {
	for _, outcome := range []string{"complete", "empty", "limited", "failed", "partial", "stale", "page failure"} {
		t.Run(outcome, func(t *testing.T) {
			r := &fakeRepository{}
			if outcome == "page failure" {
				r.pageErr = errors.New("database failed")
			}
			p := fakeProvider(func(_ context.Context, q discovery.Query) (models.EventList, error) {
				if q.City != "Chicago" || q.Country != "US" || q.StartDate != "2026-10-07" || q.EndDate != q.StartDate || q.Limit != 100 {
					t.Fatal("collection scope changed")
				}
				if outcome == "failed" || (outcome == "partial" && q.Page == 1) {
					return models.EventList{}, &discovery.UnavailableError{RetryAfter: 24 * time.Hour}
				}
				list := models.EventList{Meta: models.Freshness{DataAsOf: time.Now()}}
				if outcome == "limited" {
					list.Limited = true
				}
				if outcome == "partial" {
					cursor := "next"
					list.NextCursor = &cursor
				}
				if outcome == "stale" {
					list.Meta.Stale = true
				}
				return list, nil
			})
			c := New(r, p, Config{Cities: []Scope{{City: "Chicago", Country: "US"}}, Days: 2, Interval: 6 * time.Hour}, zerolog.Nop())
			c.now = func() time.Time { return time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC) }
			worked, err := c.Once(t.Context())
			if !worked || (err != nil) != (outcome == "page failure") {
				t.Fatalf("collection: %v %v", worked, err)
			}
			want := outcome
			if outcome == "empty" {
				want = "complete"
			}
			if outcome == "stale" {
				want = "failed"
			}
			if outcome == "page failure" {
				want = ""
			}
			// Yesterday through Days after today, in UTC.
			if r.status != want || len(r.tasks) != 4 || r.tasks[0].Date != "2026-10-07" || r.tasks[3].Date != "2026-10-10" {
				t.Fatalf("wrong outcome: %+v", r)
			}
			if (outcome == "failed" || outcome == "partial") && r.delay != 24*time.Hour {
				t.Fatal("provider reset was ignored")
			}
			if outcome == "stale" && r.pages != 0 {
				t.Fatal("stale data advanced collection freshness")
			}
		})
	}
}
