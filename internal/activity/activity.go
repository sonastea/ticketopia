// Package activity owns the bounded, identity-minimal public live view.
package activity

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

var ErrUnavailable = errors.New("live activity unavailable")

type Item struct {
	EventID               string    `json:"event_id"`
	EventTitle            string    `json:"event_title"`
	City                  string    `json:"city"`
	ActorFirstName        *string   `json:"actor_first_name"`
	RecentRecommendations int       `json:"recent_recommendations"`
	LatestRecommendedAt   time.Time `json:"latest_recommended_at"`
	ExpiresAt             time.Time `json:"expires_at"`
}
type Snapshot struct {
	AsOf  time.Time `json:"as_of"`
	Items []Item    `json:"items"`
}
type Count struct {
	EventID             string `json:"event_id"`
	RecommendationCount int    `json:"recommendation_count"`
	InterestedCount     int    `json:"interested_count"`
}
type Counts struct {
	AsOf  time.Time `json:"as_of"`
	Items []Count   `json:"items"`
}
type Repository interface {
	Snapshot(context.Context) (Snapshot, error)
	Counts(context.Context, []string) (Counts, error)
	Cleanup(context.Context) error
}
type Update struct {
	Snapshot    Snapshot
	Counts      *Counts
	Unavailable bool
}
type subscriber struct {
	ids     []string
	ch      chan Update
	initial bool
}
type Service struct {
	repo   Repository
	mu     sync.Mutex
	subs   map[*subscriber]struct{}
	limit  int
	cancel context.CancelFunc
	done   chan struct{}
	logger zerolog.Logger
}

func New(ctx context.Context, repo Repository, limit int, loggers ...zerolog.Logger) *Service {
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{repo: repo, subs: make(map[*subscriber]struct{}), limit: limit, cancel: cancel, done: make(chan struct{})}
	s.logger = zerolog.Nop()
	if len(loggers) > 0 {
		s.logger = loggers[0]
	}
	go s.run(ctx)
	return s
}
func (s *Service) Close()                                         { s.cancel(); <-s.done }
func (s *Service) Done() <-chan struct{}                          { return s.done }
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) { return s.repo.Snapshot(ctx) }
func (s *Service) Counts(ctx context.Context, ids []string) (Counts, error) {
	return s.repo.Counts(ctx, ids)
}
func (s *Service) Subscribe(ids []string) (<-chan Update, func(), error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-s.done:
		return nil, nil, ErrUnavailable
	default:
	}
	if len(s.subs) >= s.limit {
		return nil, nil, ErrUnavailable
	}
	sub := &subscriber{ids: append([]string(nil), ids...), ch: make(chan Update, 1), initial: true}
	s.subs[sub] = struct{}{}
	s.logger.Debug().Int("connections", len(s.subs)).Msg("Live activity connection opened")
	return sub.ch, func() { s.mu.Lock(); delete(s.subs, sub); s.mu.Unlock() }, nil
}
func (s *Service) run(ctx context.Context) {
	defer close(s.done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var previous string
	var lastCounts, lastPoll, lastCleanup time.Time
	var snapshot Snapshot
	var readErr error
	for {
		var now time.Time
		select {
		case <-ctx.Done():
			return
		case now = <-tick.C:
		}
		if now.Sub(lastCleanup) >= time.Minute {
			_ = s.repo.Cleanup(ctx)
			lastCleanup = now
		}
		s.mu.Lock()
		targets := make([]*subscriber, 0, len(s.subs))
		ids := map[string]bool{}
		due := now.Sub(lastCounts) >= 15*time.Second
		for sub := range s.subs {
			targets = append(targets, sub)
			if sub.initial {
				due = true
			}
			for _, id := range sub.ids {
				ids[id] = true
			}
		}
		s.mu.Unlock()
		if len(targets) == 0 {
			previous = ""
			continue
		}
		if now.Sub(lastPoll) >= 2*time.Second {
			started := time.Now()
			snapshot, readErr = s.repo.Snapshot(ctx)
			lastPoll = now
			if readErr != nil {
				s.logger.Warn().Msg("Live activity poll failed")
			}
			s.logger.Debug().Int("connections", len(targets)).Dur("duration", time.Since(started)).Msg("Live activity poll completed")
		}
		err := readErr
		encoded, _ := json.Marshal(snapshot.Items)
		changed := string(encoded) != previous
		totals := Counts{Items: []Count{}}
		if err == nil && due {
			union := make([]string, 0, len(ids))
			for id := range ids {
				union = append(union, id)
			}
			for start := 0; start < len(union); start += 101 {
				end := min(start+101, len(union))
				var batch Counts
				batch, err = s.repo.Counts(ctx, union[start:end])
				if err != nil {
					break
				}
				totals.AsOf = batch.AsOf
				totals.Items = append(totals.Items, batch.Items...)
			}
			lastCounts = now
		}
		byID := map[string]Count{}
		for _, c := range totals.Items {
			byID[c.EventID] = c
		}
		s.mu.Lock()
		for _, sub := range targets {
			if _, ok := s.subs[sub]; !ok {
				continue
			}
			u := Update{Snapshot: snapshot, Unavailable: err != nil}
			if due {
				c := Counts{AsOf: totals.AsOf, Items: []Count{}}
				for _, id := range sub.ids {
					c.Items = append(c.Items, byID[id])
				}
				u.Counts = &c
			}
			if changed || due || sub.initial || err != nil {
				select {
				case sub.ch <- u:
				default:
					select {
					case <-sub.ch:
					default:
					}
					sub.ch <- u
				}
				sub.initial = false
			}
		}
		s.mu.Unlock()
		if err == nil {
			previous = string(encoded)
		} else {
			previous = ""
		}
	}
}
