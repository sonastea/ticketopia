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

func TestCollectorCoverage(t *testing.T) {
	for _, outcome := range []string{"complete", "empty", "limited", "failed", "partial", "stale", "page failure"} {
		t.Run(outcome, func(t *testing.T) {
			r := &fakeRepository{}
			if outcome == "page failure" {
				r.pageErr = errors.New("database failed")
			}
			p := fakeProvider(func(_ context.Context, q discovery.Query) (models.EventList, error) {
				if q.City != "Chicago" || q.Country != "US" || q.StartDate != "2026-10-08" || q.EndDate != q.StartDate || q.Limit != 100 {
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
			c := New(r, p, Config{Cities: []Scope{{"Chicago", "US"}}, Days: 2, Interval: 6 * time.Hour}, zerolog.Nop())
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
			if r.status != want || len(r.tasks) != 2 || r.tasks[1].Date != "2026-10-09" {
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
