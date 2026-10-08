package discovery

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestCollectionNeverUsesStaleFallback(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 2 {
			w.WriteHeader(503)
			return
		}
		_, _ = w.Write(eventsFixture)
	})
	q := testQuery(t, "city=Chicago&country=US")
	if _, err := s.Events(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	if list, err := s.Collect(t.Context(), q); err != nil || list.Meta.Stale || calls.Load() != 2 {
		t.Fatal("collection reused cached page", err)
	}
	if _, err := s.Collect(t.Context(), q); err == nil {
		t.Fatal("failed collection returned good retained data")
	}
	if list, err := s.Events(t.Context(), q); err != nil || len(list.Items) != 2 || calls.Load() != 3 {
		t.Fatal("failed collection erased retained data", err)
	}
}

type fakeCoordinator struct {
	waits, pauses int
	err           error
}

func (g *fakeCoordinator) Wait(context.Context) error                 { g.waits++; return g.err }
func (g *fakeCoordinator) Pause(context.Context, time.Duration) error { g.pauses++; return nil }

func TestCollectionAndReadsShareCoordinator(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Rate-Limit-Available", "0")
		_, _ = w.Write(eventsFixture)
	})
	g := &fakeCoordinator{}
	s.coordinator = g
	q := testQuery(t, "city=Chicago")
	if _, err := s.Collect(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Events(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	if g.waits != 1 || g.pauses != 1 || calls.Load() != 1 {
		t.Fatal("cache or coordination boundary failed")
	}
	g.err = &UnavailableError{RetryAfter: time.Hour}
	s.gate.blocked = time.Time{} // Exercise coordinator denial, not local cooldown.
	if _, err := s.Collect(t.Context(), q); !errors.As(err, new(*UnavailableError)) || calls.Load() != 1 {
		t.Fatal("budget failure called provider", err)
	}
	if g.waits != 2 {
		t.Fatal("collection bypassed shared budget")
	}
}
