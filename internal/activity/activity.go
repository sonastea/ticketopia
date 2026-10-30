// Package activity owns the bounded, identity-minimal public live view.
package activity

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"slices"
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
	AsOf         time.Time `json:"as_of"`
	Items        []Item    `json:"items"`
	encodedItems []byte
	encoded      []byte
	contentHash  [sha256.Size]byte
}

func (s Snapshot) ContentHash() [sha256.Size]byte {
	if s.encodedItems != nil {
		return s.contentHash
	}
	return sha256.Sum256(s.ContentJSON())
}

// Cached snapshots are immutable. Identical public cohorts reuse both the
// selection and its encoded payload; AsOf is refreshed once per projection read.
func (s Snapshot) ContentJSON() []byte {
	if s.encodedItems != nil {
		return s.encodedItems
	}
	data, _ := json.Marshal(s.Items)
	return data
}
func (s Snapshot) MarshalJSON() ([]byte, error) {
	if s.encoded != nil {
		return s.encoded, nil
	}
	return json.Marshal(struct {
		AsOf  time.Time       `json:"as_of"`
		Items json.RawMessage `json:"items"`
	}{s.AsOf, s.ContentJSON()})
}

// encode runs for every warm view on each generation. Splice the already
// encoded items rather than marshalling a RawMessage, which re-validates and
// copies the whole payload.
func (s *Snapshot) encode() {
	s.encoded = nil
	if s.encodedItems == nil {
		s.encodedItems = s.ContentJSON()
		s.contentHash = sha256.Sum256(s.encodedItems)
	}
	asOf, err := s.AsOf.MarshalJSON()
	if err != nil {
		return
	}
	encoded := make([]byte, 0, len(`{"as_of":,"items":}`)+len(asOf)+len(s.encodedItems))
	encoded = append(append(append(encoded, `{"as_of":`...), asOf...), `,"items":`...)
	s.encoded = append(append(encoded, s.encodedItems...), '}')
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
	// MembershipChanged marks an Unavailable update caused by the owner's own
	// committed membership change: the view is cleared, then reconnected quietly
	// rather than with outage backoff.
	MembershipChanged bool
}
type subscriber struct {
	held    time.Time // first skipped delivery while a rebuild is in progress
	owner   string
	ids     []string // owner, ids and filter are immutable after Subscribe
	ch      chan Update
	initial bool
	// terminal marks a queued membership.changed (under mu): the stream ends
	// with it, so later deliveries must not replace it.
	terminal bool
	filter   Filters
	previous [sha256.Size]byte
	// The public cohort key for keyCity, cached under the projection gate.
	keyCity, key string
	keyed        bool
}
type Service struct {
	metrics           metrics
	invalidAll        bool
	invalidMembers    map[string]bool
	unwatchMembership func()
	members           map[string]*memberState
	memberFacts       map[string]MembershipCandidate
	memberSlots       map[string]int
	memberAt          time.Time
	memberSweep       uint64
	// memberIndex is the candidate generation membership bits were last
	// reconciled with; another generation may have reused their slots.
	memberIndex *candidateIndex
	repo        Repository
	mu          sync.Mutex
	subs        map[*subscriber]struct{}
	// private indexes connection-filtered subscriptions by owner (under mu), so
	// a committing membership write touches only its owner's streams.
	private    map[string]map[*subscriber]struct{}
	waiting    map[*waiter]struct{}
	limit      int
	cancel     context.CancelFunc
	done       chan struct{}
	logger     zerolog.Logger
	projection chan struct{}
	index      *candidateIndex
	readAt     time.Time
	readErr    error
	views      map[string]cachedView
	keys       map[Location]string
	viewBytes  int
	jobs       chan struct{}
	pending    chan struct{}
}

type cachedView struct {
	snapshot  Snapshot
	used      time.Time
	bytes     int
	expiresAt time.Time // earliest selected group expiry; zero when none
}

func New(ctx context.Context, repo Repository, limit int, loggers ...zerolog.Logger) *Service {
	ctx, cancel := context.WithCancel(ctx)
	s := &Service{repo: repo, subs: make(map[*subscriber]struct{}), private: map[string]map[*subscriber]struct{}{}, waiting: map[*waiter]struct{}{}, limit: limit, cancel: cancel, done: make(chan struct{})}
	s.projection = make(chan struct{}, 1)
	s.jobs = make(chan struct{}, 2)
	s.pending = make(chan struct{}, 64)
	s.views = map[string]cachedView{}
	s.members = map[string]*memberState{}
	s.invalidMembers = map[string]bool{}
	s.memberFacts = map[string]MembershipCandidate{}
	s.memberSlots = map[string]int{}
	s.keys = map[Location]string{}
	s.logger = zerolog.Nop()
	if repo, ok := repo.(interface{ WatchMembership(func(string)) func() }); ok {
		s.unwatchMembership = repo.WatchMembership(func(owner string) {
			s.mu.Lock()
			defer s.mu.Unlock()
			// Overflow invalidates all scopes rather than dropping a removal.
			if len(s.invalidMembers) < 384 {
				s.invalidMembers[owner] = true
			} else {
				s.invalidAll = true
			}
			for sub := range s.private[owner] {
				select {
				case <-sub.ch:
				default:
				}
				sub.ch <- Update{Unavailable: true, MembershipChanged: true}
				sub.terminal = true
			}
		})
	}
	if len(loggers) > 0 {
		s.logger = loggers[0]
	}
	go s.run(ctx)
	return s
}
func (s *Service) Close()                { s.cancel(); <-s.done }
func (s *Service) Done() <-chan struct{} { return s.done }
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	return s.FilteredSnapshot(ctx, DefaultPreferences().Filters)
}

func (s *Service) FilteredSnapshot(ctx context.Context, filter Filters) (Snapshot, error) {
	return s.OwnerSnapshot(ctx, "", filter)
}

// OwnerSnapshot accepts only a server-authenticated owner; public callers must
// use FilteredSnapshot, which rejects personal connection predicates.
func (s *Service) OwnerSnapshot(ctx context.Context, owner string, filter Filters) (Snapshot, error) {
	select {
	case s.pending <- struct{}{}:
	default:
		s.metrics.queueLimit.Add(1)
		return Snapshot{}, ErrUnavailable
	}
	defer func() { <-s.pending }()
	return s.ownerSnapshot(ctx, owner, filter)
}

// ownerSnapshot is the shared read without request admission. The delivery
// worker uses it directly: pending admission bounds external requests, and a
// reconnect burst filling it must not fail every stream's delivery.
func (s *Service) ownerSnapshot(ctx context.Context, owner string, filter Filters) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	f, err := NormalizeFilters(filter)
	if err != nil {
		return Snapshot{}, err
	}
	if f.Connections.Mode == "any" && owner == "" {
		return Snapshot{}, ErrPreferences
	}
	if f.Connections.Mode == "any" {
		// Register while waiting for the gate, so one assembly covers every
		// queued cold or invalidated owner (bounded by pending admission and
		// the worker's one in-flight read).
		w := &waiter{owner, sourceMask(f)}
		s.mu.Lock()
		s.waiting[w] = struct{}{}
		s.mu.Unlock()
		defer func() { s.mu.Lock(); delete(s.waiting, w); s.mu.Unlock() }()
	}
	select {
	case s.projection <- struct{}{}:
	case <-ctx.Done():
		return Snapshot{}, ErrUnavailable
	case <-s.done:
		return Snapshot{}, ErrUnavailable
	}
	defer func() { <-s.projection }()
	defer s.accountMetrics()
	if ctx.Err() != nil {
		return Snapshot{}, ErrUnavailable
	}
	now := time.Now()
	if now.Sub(s.readAt) >= 2*time.Second {
		// Leave half the budget for in-progress assembly, plus fixed overhead for
		// bounded location keys/readers. Eviction never changes filter semantics.
		if s.index != nil && s.index.bytes+s.viewBytes > MemoryBudget/2-(2<<20) {
			clear(s.views)
			s.viewBytes = 0
		}
		if !s.acquireJob(ctx) {
			return Snapshot{}, ErrUnavailable
		}
		var data Candidates
		started := time.Now()
		if repo, ok := s.repo.(CandidateRefreshRepository); ok {
			var previous Candidates
			if s.index != nil {
				previous = s.index.data
			}
			data, err = repo.RefreshCandidates(ctx, previous)
		} else if repo, ok := s.repo.(CandidateRepository); ok {
			data, err = repo.Candidates(ctx)
		} else {
			var snapshot Snapshot
			snapshot, err = s.repo.Snapshot(ctx)
			data.AsOf = snapshot.AsOf
			for _, item := range snapshot.Items {
				data.Items = append(data.Items, Candidate{Item: item})
			}
		}
		<-s.jobs
		var index *candidateIndex
		if err == nil {
			index, err = buildIndex(data, s.index)
		}
		s.metrics.projection.observe(started)
		if err == nil && index.bytes*2+s.membershipBytes()+s.viewBytes+(8<<20) > MemoryBudget {
			clear(s.views)
			s.viewBytes = 0
			if index.bytes*2+s.membershipBytes()+(8<<20) > MemoryBudget {
				err = ErrMemoryLimit
			}
		}
		// Start time conservatively includes SQL assembly in contribution age.
		s.readAt = now
		s.readErr = err
		if err != nil {
			reason := "projection_failed"
			if errors.Is(err, ErrCandidateLimit) {
				reason = "candidate_limit"
				s.metrics.candidateLimit.Add(1)
			}
			if errors.Is(err, ErrMemoryLimit) {
				reason = "memory_limit"
				s.metrics.memoryLimit.Add(1)
			}
			s.metrics.projectionFailed.Add(1)
			s.logger.Warn().Err(err).Str("reason", reason).Msg("Live activity generation unavailable")
			s.index = nil
			clear(s.views)
			s.viewBytes = 0
			return Snapshot{}, ErrUnavailable
		}
		if s.index == nil || s.index.fingerprint != index.fingerprint {
			clear(s.views)
			s.viewBytes = 0
		}
		s.index = index
	}
	if s.readErr != nil || s.index == nil {
		return Snapshot{}, ErrUnavailable
	}
	if f.Connections.Mode == "any" {
		if err := s.refreshMembership(ctx, owner, f); err != nil {
			if !errors.Is(err, errRebuilding) {
				s.metrics.membershipFailed.Add(1)
				err = ErrUnavailable
			}
			return Snapshot{}, err
		}
	}
	return s.selectOwnerCached(ctx, f, owner)
}

// serverNow conservatively estimates database time: readAt precedes the SQL
// that read AsOf, so the estimate is never earlier than actual server time.
// Expiry is applied per view; one group's expiry never makes the generation
// (and every stream on the replica) unavailable.
func (s *Service) serverNow(now time.Time) time.Time {
	return s.index.data.AsOf.Add(now.Sub(s.readAt))
}
func (s *Service) selectCached(ctx context.Context, f Filters) (Snapshot, error) {
	return s.selectOwnerCached(ctx, f, "")
}
func (s *Service) selectOwnerCached(ctx context.Context, f Filters, owner string) (Snapshot, error) {
	if f.Connections.Mode == "any" {
		s.mu.Lock()
		invalid := s.invalidAll || s.invalidMembers[owner]
		s.mu.Unlock()
		if invalid {
			return Snapshot{}, ErrUnavailable
		}
	}
	key := ""
	if f.Location.Mode == "city" {
		var ok bool
		key, ok = s.keys[f.Location]
		if !ok {
			repo, supported := s.repo.(CandidateRepository)
			if !supported {
				return Snapshot{}, ErrUnavailable
			}
			var err error
			if !s.acquireJob(ctx) {
				return Snapshot{}, ErrUnavailable
			}
			started := time.Now()
			key, err = repo.LocationKey(ctx, f.Location)
			s.metrics.location.observe(started)
			<-s.jobs
			if err != nil {
				return Snapshot{}, err
			}
			if len(s.keys) >= 384 {
				active := map[Location]bool{}
				s.mu.Lock()
				for sub := range s.subs {
					active[sub.filter.Location] = true
				}
				s.mu.Unlock()
				for location := range s.keys {
					if !active[location] {
						delete(s.keys, location)
						break
					}
				}
				if len(s.keys) >= 384 {
					return Snapshot{}, ErrUnavailable
				}
			}
			s.keys[f.Location] = key
		}
	}
	digest := publicKey(key, f)
	var membership bitmap
	if f.Connections.Mode == "any" {
		if s.memberIndex != s.index {
			// A warm read can follow a public generation refresh.
			s.reconcileMembers()
		}
		m := s.members[owner]
		if m == nil || !m.valid || sourceMask(f)&^m.sources != 0 {
			return Snapshot{}, ErrUnavailable
		}
		membership = m.union(f)
		digest += m.key(owner, f)
	}
	now := time.Now()
	serverNow := s.serverNow(now)
	if v, ok := s.views[digest]; ok && !v.expiresAt.IsZero() && !serverNow.Before(v.expiresAt) {
		// A selected group expired: reselect from the in-memory generation.
		s.viewBytes -= v.bytes
		delete(s.views, digest)
	}
	if v, ok := s.views[digest]; ok {
		s.metrics.hits.Add(1)
		v.used = now
		if !v.snapshot.AsOf.Equal(s.index.data.AsOf) {
			v.snapshot.AsOf = s.index.data.AsOf
			v.snapshot.encode()
		}
		s.views[digest] = v
		return v.snapshot, nil
	}
	// Hold mu only to copy the subscriber set: membership writes take it on the
	// request path. Keys are derived under the projection gate (held here).
	s.mu.Lock()
	subs := make([]*subscriber, 0, len(s.subs))
	for sub := range s.subs {
		subs = append(subs, sub)
	}
	s.mu.Unlock()
	active := map[string]bool{}
	for _, sub := range subs {
		city := s.keys[sub.filter.Location]
		if sub.filter.Location.Mode == "all" || city != "" {
			if !sub.keyed || sub.keyCity != city {
				sub.keyCity, sub.key, sub.keyed = city, publicKey(city, sub.filter), true
			}
			key := sub.key
			if sub.filter.Connections.Mode == "any" {
				if m := s.members[sub.owner]; m != nil {
					key += m.key(sub.owner, sub.filter)
				}
			}
			active[key] = true
		}
	}
	extra := []string{}
	for k, v := range s.views {
		if active[k] {
			continue
		}
		if now.Sub(v.used) >= 30*time.Second {
			s.viewBytes -= v.bytes
			delete(s.views, k)
			continue
		}
		extra = append(extra, k)
	}
	budget := 128
	if !active[digest] {
		budget--
	}
	if len(extra) > budget {
		// Membership revisions can retire all active private keys at once. Bound
		// those unreferenced generations too, not just new snapshot-only views.
		slices.SortFunc(extra, func(a, b string) int { return s.views[a].used.Compare(s.views[b].used) })
		for _, key := range extra[:len(extra)-budget] {
			s.viewBytes -= s.views[key].bytes
			delete(s.views, key)
		}
	}
	s.metrics.misses.Add(1)
	started := time.Now()
	view, expiresAt := s.index.selectView(f, key, serverNow, membership)
	view.encode()
	s.metrics.selection.observe(started)
	bytes := len(view.encoded)*4 + len(digest)*2 + 1024
	if s.index.bytes*2+s.viewBytes+bytes+s.membershipBytes()+(8<<20) > MemoryBudget {
		s.metrics.memoryLimit.Add(1)
		return Snapshot{}, ErrUnavailable
	}
	s.views[digest] = cachedView{view, now, bytes, expiresAt}
	s.viewBytes += bytes
	return view, nil
}
func publicKey(city string, f Filters) string {
	data, _ := json.Marshal(struct {
		City       string
		Categories []string
		Artists    []string
		Venues     []string
	}{city, f.CategoryIDs, f.ArtistIDs, f.VenueIDs})
	return string(data)
}
func (s *Service) Counts(ctx context.Context, ids []string) (Counts, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case s.pending <- struct{}{}:
	default:
		s.metrics.queueLimit.Add(1)
		return Counts{}, ErrUnavailable
	}
	defer func() { <-s.pending }()
	if !s.acquireJob(ctx) {
		return Counts{}, ErrUnavailable
	}
	defer func() { <-s.jobs }()
	defer s.metrics.counts.observe(time.Now())
	return s.repo.Counts(ctx, ids)
}
func (s *Service) acquireJob(ctx context.Context) bool {
	select {
	case s.jobs <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	case <-s.done:
		return false
	}
}
func (s *Service) Subscribe(ids []string, filters ...Filters) (<-chan Update, func(), error) {
	return s.SubscribeOwner("", ids, filters...)
}
func (s *Service) SubscribeOwner(owner string, ids []string, filters ...Filters) (<-chan Update, func(), error) {
	return s.SubscribeOwnerContext(context.Background(), owner, ids, filters...)
}

func (s *Service) SubscribeOwnerContext(ctx context.Context, owner string, ids []string, filters ...Filters) (<-chan Update, func(), error) {
	f := DefaultPreferences().Filters
	if len(filters) > 0 {
		f = filters[0]
	}
	var err error
	f, err = NormalizeFilters(f)
	if err != nil {
		return nil, nil, err
	}
	if f.Connections.Mode == "any" && owner == "" {
		return nil, nil, ErrPreferences
	}
	s.mu.Lock()
	select {
	case <-s.done:
		s.mu.Unlock()
		return nil, nil, ErrUnavailable
	default:
	}
	if len(s.subs) >= s.limit {
		s.metrics.streamLimit.Add(1)
		s.mu.Unlock()
		return nil, nil, ErrUnavailable
	}
	sub := &subscriber{owner: owner, ids: append([]string(nil), ids...), ch: make(chan Update, 1), initial: true, filter: f}
	s.subs[sub] = struct{}{}
	if f.Connections.Mode == "any" {
		if s.private[owner] == nil {
			s.private[owner] = map[*subscriber]struct{}{}
		}
		s.private[owner][sub] = struct{}{}
	}
	connections := len(s.subs)
	s.mu.Unlock()
	release := func() {
		s.mu.Lock()
		delete(s.subs, sub)
		if owned := s.private[sub.owner]; owned != nil {
			delete(owned, sub)
			if len(owned) == 0 {
				delete(s.private, sub.owner)
			}
		}
		s.mu.Unlock()
	}
	// A valid warm view is delivered before initial count work. Never block on SQL
	// while holding the subscriber lock. Brief selection contention should not
	// turn a warm subscription into a wait for the next one-second worker tick.
	warm := time.NewTimer(100 * time.Millisecond)
	defer warm.Stop()
	select {
	case s.projection <- struct{}{}:
		if ctx.Err() != nil {
			<-s.projection
			release()
			return nil, nil, ErrUnavailable
		}
		if s.index != nil && s.readErr == nil && time.Since(s.readAt) < 2*time.Second {
			key := s.keys[f.Location]
			if f.Location.Mode == "all" || key != "" {
				view, err := s.selectOwnerCached(ctx, f, owner)
				if err == nil {
					s.mu.Lock()
					if f.Connections.Mode != "any" || (!s.invalidAll && !s.invalidMembers[owner] && !sub.terminal) {
						select {
						case sub.ch <- Update{Snapshot: view}:
						default:
						}
					}
					s.mu.Unlock()
				}
			}
		}
		<-s.projection
	case <-warm.C:
	case <-ctx.Done():
		release()
		return nil, nil, ErrUnavailable
	case <-s.done:
		release()
		return nil, nil, ErrUnavailable
	}
	s.logger.Debug().Int("connections", connections).Msg("Live activity connection opened")
	return sub.ch, release, nil
}
func (s *Service) run(ctx context.Context) {
	defer close(s.done)
	if s.unwatchMembership != nil {
		defer s.unwatchMembership()
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	var lastCounts, lastPoll, lastCleanup, lastMetrics time.Time
	type cachedCount struct {
		value        Count
		asOf, readAt time.Time
	}
	countCache := map[string]cachedCount{}
	for {
		var now time.Time
		select {
		case <-ctx.Done():
			return
		case now = <-tick.C:
		}
		if now.Sub(lastMetrics) >= 15*time.Second {
			s.logger.Info().Interface("activity", s.Stats()).Interface("process", s.ProcessStats()).Msg("Live activity metrics")
			lastMetrics = now
		}
		select {
		case s.projection <- struct{}{}:
			for key, view := range s.views {
				if now.Sub(view.used) >= 30*time.Second {
					s.viewBytes -= view.bytes
					delete(s.views, key)
				}
			}
			for owner, member := range s.members {
				if now.Sub(member.used) >= 30*time.Second {
					delete(s.members, owner)
				}
			}
			if len(s.members) == 0 {
				clear(s.memberFacts)
				clear(s.memberSlots)
				s.memberIndex = nil
			}
			s.mu.Lock()
			idle := len(s.subs) == 0
			s.mu.Unlock()
			if idle && now.Sub(s.readAt) >= 30*time.Second {
				s.index = nil
				clear(s.views)
				s.viewBytes = 0
				clear(s.keys)
				clear(s.members)
				clear(s.memberFacts)
				clear(s.memberSlots)
				s.memberIndex = nil
			}
			s.accountMetrics()
			<-s.projection
		default:
		}
		if now.Sub(lastCleanup) >= time.Minute {
			if s.acquireJob(ctx) {
				started := time.Now()
				_ = s.repo.Cleanup(ctx)
				s.metrics.cleanup.observe(started)
				<-s.jobs
			}
			lastCleanup = now
		}
		// Copy targets under mu (membership writes take it on the request
		// path); their visible IDs are immutable, so collect those outside it.
		s.mu.Lock()
		targets := make([]*subscriber, 0, len(s.subs))
		initial := make([]bool, 0, len(s.subs))
		for sub := range s.subs {
			targets = append(targets, sub)
			initial = append(initial, sub.initial)
		}
		s.mu.Unlock()
		ids := map[string]bool{}
		activeIDs := map[string]bool{}
		due := now.Sub(lastCounts) >= 15*time.Second
		regularCounts := due
		for i, sub := range targets {
			for _, id := range sub.ids {
				activeIDs[id] = true
			}
			if initial[i] {
				due = true
			}
			if regularCounts || initial[i] {
				for _, id := range sub.ids {
					ids[id] = true
				}
			}
		}
		for id := range countCache {
			if !activeIDs[id] {
				delete(countCache, id)
			}
		}
		if len(targets) == 0 {
			continue
		}
		// Bound the complete shared cycle, not just each SQL batch or recipient.
		// A large visible-ID union or failing scope must not turn fanout into
		// hundreds of serial five-second waits before other views can clear.
		cycle, cancelCycle := context.WithTimeout(ctx, 5*time.Second)
		// Read the shared generation every tick; the service owns the two-second
		// refresh and failure backoff. Caching a failed poll here kept broadcasting
		// unavailable, closing every stream, after another reader had recovered.
		started := time.Now()
		snapshot, err := s.ownerSnapshot(cycle, "", DefaultPreferences().Filters)
		if now.Sub(lastPoll) >= 2*time.Second {
			lastPoll = now
			if err != nil {
				s.logger.Warn().Msg("Live activity poll failed")
			}
			s.logger.Debug().Int("connections", len(targets)).Dur("duration", time.Since(started)).Msg("Live activity poll completed")
		}
		totals := Counts{Items: []Count{}}
		if err == nil && due {
			union := make([]string, 0, len(ids))
			for id := range ids {
				if cached, ok := countCache[id]; ok && !regularCounts && now.Sub(cached.readAt) < 15*time.Second {
					totals.Items = append(totals.Items, cached.value)
					if totals.AsOf.IsZero() || cached.asOf.Before(totals.AsOf) {
						totals.AsOf = cached.asOf
					}
					continue
				}
				union = append(union, id)
			}
			for start := 0; start < len(union); start += 101 {
				end := min(start+101, len(union))
				var batch Counts
				batch, err = s.Counts(cycle, union[start:end])
				if err != nil {
					clear(countCache)
					break
				}
				if totals.AsOf.IsZero() || batch.AsOf.Before(totals.AsOf) {
					totals.AsOf = batch.AsOf
				}
				for _, value := range batch.Items {
					countCache[value.EventID] = cachedCount{value, batch.AsOf, now}
				}
				totals.Items = append(totals.Items, batch.Items...)
			}
			if regularCounts {
				lastCounts = now
			}
		}
		byID := map[string]Count{}
		for _, c := range totals.Items {
			byID[c.EventID] = c
		}
		for _, sub := range targets {
			view := snapshot
			viewErr := err
			if viewErr == nil {
				view, viewErr = s.ownerSnapshot(cycle, sub.owner, sub.filter)
			}
			contentHash := view.ContentHash()
			s.mu.Lock()
			if _, ok := s.subs[sub]; !ok {
				s.mu.Unlock()
				continue
			}
			if sub.terminal {
				// A rebuild consumed the invalidation; the queued change still ends
				// this stream and is never replaced by a newer view.
				sub.initial = false
				s.mu.Unlock()
				continue
			}
			membershipChanged := false
			if sub.filter.Connections.Mode == "any" && (s.invalidAll || s.invalidMembers[sub.owner]) {
				viewErr, membershipChanged = ErrUnavailable, true
			}
			// A newer revision is rebuilding for an account that had a complete
			// view, or rebuild work consumed this cycle's deadline before this
			// private view: keep the last view (as during the revision poll)
			// for up to rebuildHold rather than closing the stream. Counts
			// resume with the next delivery.
			if !membershipChanged && sub.filter.Connections.Mode == "any" && viewErr != nil && (errors.Is(viewErr, errRebuilding) || (err == nil && cycle.Err() != nil)) {
				if sub.held.IsZero() {
					sub.held = now
				}
				if now.Sub(sub.held) < rebuildHold {
					s.mu.Unlock()
					continue
				}
			}
			sub.held = time.Time{}
			changed := contentHash != sub.previous
			u := Update{Snapshot: view, Unavailable: viewErr != nil, MembershipChanged: membershipChanged}
			if due && (regularCounts || sub.initial) {
				c := Counts{AsOf: totals.AsOf, Items: []Count{}}
				for _, id := range sub.ids {
					c.Items = append(c.Items, byID[id])
				}
				u.Counts = &c
			}
			if changed || u.Counts != nil || sub.initial || viewErr != nil {
				select {
				case sub.ch <- u:
				default:
					s.metrics.overwritten.Add(1)
					select {
					case <-sub.ch:
					default:
					}
					sub.ch <- u
				}
				sub.initial = false
				sub.terminal = membershipChanged
			}
			if viewErr == nil {
				sub.previous = contentHash
			} else {
				sub.previous = [sha256.Size]byte{}
			}
			s.mu.Unlock()
		}
		cancelCycle()
	}
}
