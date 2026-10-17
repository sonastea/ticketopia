package accounts

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/sonastea/ticketopia/internal/activity"
)

var ErrLiveConflict = errors.New("live preferences changed; reload before saving")

type LivePreferences struct {
	activity.Preferences
	Revision uint64 `json:"revision"`
	Paused   bool   `json:"notifications_paused"`
}
type LiveRepository interface {
	LivePreferences(context.Context, string) (LivePreferences, error)
	ReplaceLivePreferences(context.Context, string, uint64, bool, ...activity.Filters) (LivePreferences, error)
	LiveScopes(context.Context, []string) (map[string]LivePreferences, error)
}

func (s *Service) LivePreferences(ctx context.Context, owner string) (LivePreferences, error) {
	r, ok := s.repo.(LiveRepository)
	if !ok {
		return LivePreferences{}, activity.ErrUnavailable
	}
	return r.LivePreferences(ctx, owner)
}
func (s *Service) ReplaceLivePreferences(ctx context.Context, owner string, revision uint64, enabled bool, filters ...activity.Filters) (LivePreferences, error) {
	r, ok := s.repo.(LiveRepository)
	if !ok {
		return LivePreferences{}, activity.ErrUnavailable
	}
	p, e := r.ReplaceLivePreferences(ctx, owner, revision, enabled, filters...)
	if e == nil && p.Revision != revision {
		s.invalidateLive(owner, false)
	}
	return p, e
}

type liveWatch struct {
	owner, id string
	revision  uint64
	ch        chan LiveChange
}
type LiveChange struct {
	Revision    uint64
	Unavailable bool
}
type liveWatches struct {
	sync.Mutex
	entries map[*liveWatch]bool
	running bool
	cancel  context.CancelFunc
	done    chan struct{}
}

// One batch loop per account service, only while streams exist. Credential IDs
// never leave this boundary; missing/expired/revoked credentials fail closed.
func (s *Service) WatchLive(p Principal, revision uint64) (<-chan LiveChange, func()) {
	w := &liveWatch{p.Account.ID, p.Credential.ID, revision, make(chan LiveChange, 1)}
	s.live.Lock()
	if s.live.entries == nil {
		s.live.entries = map[*liveWatch]bool{}
	}
	s.live.entries[w] = true
	s.startLiveLocked()
	s.live.Unlock()
	return w.ch, func() {
		s.live.Lock()
		delete(s.live.entries, w)
		var done chan struct{}
		if len(s.live.entries) == 0 && s.live.running {
			s.live.cancel()
			done = s.live.done
		}
		s.live.Unlock()
		if done != nil {
			<-done
		}
	}
}
func (s *Service) startLiveLocked() {
	if s.live.running || len(s.live.entries) == 0 {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.live.running = true
	s.live.cancel = cancel
	s.live.done = make(chan struct{})
	go s.pollLive(ctx, s.live.done)
}
func (s *Service) invalidateLive(owner string, unavailable bool) {
	s.live.Lock()
	defer s.live.Unlock()
	for w := range s.live.entries {
		if w.owner == owner {
			select {
			case w.ch <- LiveChange{Unavailable: unavailable}:
			default:
			}
		}
	}
}
func (s *Service) pollLive(ctx context.Context, done chan struct{}) {
	defer func() { s.live.Lock(); s.live.running = false; close(done); s.startLiveLocked(); s.live.Unlock() }()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		s.live.Lock()
		if len(s.live.entries) == 0 {
			s.live.Unlock()
			return
		}
		watches := make([]*liveWatch, 0, len(s.live.entries))
		ids := []string{}
		seen := map[string]bool{}
		for w := range s.live.entries {
			watches = append(watches, w)
			if !seen[w.id] {
				seen[w.id] = true
				ids = append(ids, w.id)
			}
		}
		s.live.Unlock()
		states := map[string]LivePreferences{}
		r, ok := s.repo.(LiveRepository)
		var err error
		if !ok {
			err = activity.ErrUnavailable
		} else {
			// Bound the entire credential sweep, including all batches, below
			// the 30-second authentication lease even at configured capacity.
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			for start := 0; start < len(ids); start += 128 {
				batch, e := r.LiveScopes(ctx, ids[start:min(start+128, len(ids))])
				if e != nil {
					err = e
					break
				}
				for id, p := range batch {
					states[id] = p
				}
			}
			cancel()
		}
		for _, w := range watches {
			p, ok := states[w.id]
			if err != nil || !ok || p.Revision != w.revision || p.Paused || !p.Enabled {
				select {
				case w.ch <- LiveChange{Revision: p.Revision, Unavailable: err != nil || !ok}:
				default:
				}
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
