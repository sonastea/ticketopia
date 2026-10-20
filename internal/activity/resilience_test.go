package activity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

type failingCandidates struct {
	candidateRepository
	fail            bool
	previousLengths []int
}

func (r *failingCandidates) RefreshCandidates(ctx context.Context, previous Candidates) (Candidates, error) {
	r.previousLengths = append(r.previousLengths, len(previous.Items))
	return r.Candidates(ctx)
}

func (r *failingCandidates) Candidates(ctx context.Context) (Candidates, error) {
	if r.fail {
		return Candidates{}, ErrUnavailable
	}
	return r.candidateRepository.Candidates(ctx)
}

func TestFailedGenerationDropsAttributionAndRecovers(t *testing.T) {
	r := &failingCandidates{candidateRepository: candidateRepository{data: candidateFixture(1)}}
	name := "PrivateChosenName"
	r.data.Items[0].Item.ActorFirstName = &name
	s := New(t.Context(), r, 1)
	defer s.Close()
	if _, err := s.Snapshot(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Hold the same gate used by the worker when advancing the fixture clock.
	s.projection <- struct{}{}
	r.fail = true
	s.readAt = time.Now().Add(-3 * time.Second)
	<-s.projection
	if _, err := s.Snapshot(t.Context()); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	s.projection <- struct{}{}
	if s.index != nil || len(s.views) != 0 {
		t.Error("failed poll retained name-bearing generation")
	}
	r.fail = false
	r.data.Items[0].Item.ActorFirstName = nil
	s.readAt = time.Now().Add(-3 * time.Second)
	<-s.projection
	got, err := s.Snapshot(t.Context())
	if err != nil || len(got.Items) != 1 || got.Items[0].ActorFirstName != nil {
		t.Fatal("failed recovery", got, err)
	}
	if !slices.Equal(r.previousLengths, []int{0, 1, 0}) {
		t.Fatal("failed generation reused on recovery", r.previousLengths)
	}
	stats := s.Stats()
	if stats.Failures["projection_failed"] != 1 || stats.Operations["projection"].Count != 3 {
		t.Fatal(stats)
	}
	encoded, _ := json.Marshal(stats)
	if strings.Contains(string(encoded), name) || strings.Contains(string(encoded), "ticketmaster:") {
		t.Fatal("identity-bearing metrics")
	}
}

func TestQueueOverloadAndMetricsDoNotWaitForSQL(t *testing.T) {
	s := New(t.Context(), &candidateRepository{data: candidateFixture(1)}, 1)
	defer s.Close()
	// Exhaust the bounded admitted work, as a slow database would. Neither a new
	// fallback read nor telemetry may wait for the projection lock in this state.
	s.projection <- struct{}{}
	for range cap(s.pending) {
		s.pending <- struct{}{}
	}
	defer func() {
		for len(s.pending) > 0 {
			<-s.pending
		}
		<-s.projection
	}()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := s.Snapshot(ctx); !errors.Is(err, ErrUnavailable) {
		t.Fatal(err)
	}
	stats := s.Stats()
	if stats.Pending != 64 || stats.Failures["queue_limit"] != 1 {
		t.Fatal(stats)
	}
}

func TestTelemetryConfirmsDuplicateReadReuse(t *testing.T) {
	s := New(t.Context(), &candidateRepository{data: candidateFixture(100)}, 256)
	defer s.Close()
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			if _, err := s.Snapshot(t.Context()); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	stats := s.Stats()
	if stats.Operations["projection"].Count != 1 || stats.CacheMisses != 1 || stats.CacheHits != 31 || stats.Cohorts != 1 || stats.Bytes > MemoryBudget {
		t.Fatal(stats)
	}
	for range 16 {
		wg.Go(func() {
			s.RecordHTTP(429)
			s.RecordHTTP(503)
			s.RecordDelivery(time.Now(), context.Canceled)
			_ = s.Stats()
		})
	}
	wg.Wait()
	stats = s.Stats()
	if stats.HTTP429 != 16 || stats.HTTP503 != 16 || stats.WriteErrors != 16 {
		t.Fatal(stats)
	}
}

type deadlineRepository struct {
	batchRepository
	deadlines []time.Time
}

func (r *deadlineRepository) Counts(ctx context.Context, ids []string) (Counts, error) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return Counts{}, errors.New("unbounded count batch")
	}
	r.mu.Lock()
	r.deadlines = append(r.deadlines, deadline)
	r.mu.Unlock()
	// Let successive batches start at different times. A per-batch timeout
	// would incorrectly renew the work budget each time.
	timer := time.NewTimer(5 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		return Counts{}, ctx.Err()
	}
	return r.batchRepository.Counts(ctx, ids)
}

func TestCountUnionSharesOneCycleDeadline(t *testing.T) {
	r := &deadlineRepository{}
	s := New(t.Context(), r, 3)
	defer s.Close()
	var last <-chan Update
	for tab := range 3 {
		ids := []string{}
		for i := range 101 {
			ids = append(ids, fmt.Sprintf("ticketmaster:%d-%d", tab, i))
		}
		ch, release, err := s.Subscribe(ids)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		last = ch
	}
	select {
	case u := <-last:
		if u.Unavailable {
			t.Fatal("union unavailable")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("union did not complete")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.deadlines) != 3 {
		t.Fatal("wrong batch count", len(r.deadlines))
	}
	for _, deadline := range r.deadlines {
		if !deadline.Equal(r.deadlines[0]) {
			t.Fatal("batch renewed the aggregate work deadline")
		}
	}
}

func TestWarmSubscriptionSurvivesSelectionContention(t *testing.T) {
	s := New(t.Context(), &candidateRepository{data: candidateFixture(30)}, 256)
	defer s.Close()
	if _, err := s.Snapshot(t.Context()); err != nil {
		t.Fatal(err)
	}
	s.projection <- struct{}{}
	released := make(chan struct{})
	go func() {
		timer := time.NewTimer(10 * time.Millisecond)
		defer timer.Stop()
		<-timer.C
		<-s.projection
		close(released)
	}()
	defer func() { <-released }()
	ch, release, err := s.SubscribeOwnerContext(t.Context(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	select {
	case u := <-ch:
		if u.Unavailable || len(u.Snapshot.Items) != 20 {
			t.Fatal("invalid warm view")
		}
	default:
		t.Fatal("warm view delayed until next worker tick")
	}
}
