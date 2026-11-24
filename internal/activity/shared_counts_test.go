package activity

import (
	"context"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

// generationRepository is batchRepository with a write counter, like the
// MariaDB repository's.
type generationRepository struct {
	batchRepository
	gen atomic.Uint64
}

func (r *generationRepository) Generation() uint64 { return r.gen.Load() }

func (r *generationRepository) reads() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.batches)
}

func TestSharedCountsReadOnlyMissingTotals(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		repo := &generationRepository{}
		s := New(t.Context(), repo, 4)
		defer s.Close()
		s.ShareCounts(DefaultSharedCounts)

		first, err := s.Counts(t.Context(), []string{"a", "b"})
		if err != nil || len(first.Items) != 2 {
			t.Fatal(first, err)
		}
		// Repeats are answered without SQL; new events alone are read; order
		// and duplicates follow the request.
		got, err := s.Counts(t.Context(), []string{"c", "a", "c", "b"})
		if err != nil {
			t.Fatal(err)
		}
		var order []string
		for _, c := range got.Items {
			order = append(order, c.EventID)
		}
		reads := repo.reads()
		if !slices.Equal(order, []string{"c", "a", "c", "b"}) || len(reads) != 2 || !slices.Equal(reads[1], []string{"c"}) {
			t.Fatalf("shared totals: order=%v reads=%v", order, reads)
		}
		if got.AsOf.After(first.AsOf) {
			t.Fatalf("as_of must be the oldest total returned: %v after %v", got.AsOf, first.AsOf)
		}
		if _, err := s.Counts(t.Context(), []string{"a", "b", "c"}); err != nil || len(repo.reads()) != 2 {
			t.Fatalf("a fully shared request read again: %v %v", repo.reads(), err)
		}

		// A write in this process ends sharing at once.
		repo.gen.Add(1)
		if _, err := s.Counts(t.Context(), []string{"a"}); err != nil || len(repo.reads()) != 3 {
			t.Fatalf("a total read before a write was served after it: %v", repo.reads())
		}
		// So does the window.
		time.Sleep(DefaultSharedCounts)
		if _, err := s.Counts(t.Context(), []string{"a"}); err != nil || len(repo.reads()) != 4 {
			t.Fatalf("an expired total was served: %v", repo.reads())
		}
	})
}

func TestCountsReadEveryTimeWithoutSharing(t *testing.T) {
	repo := &generationRepository{}
	s := New(t.Context(), repo, 4)
	defer s.Close()
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			if _, err := s.Counts(context.Background(), []string{"a", "a"}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	reads := repo.reads()
	if len(reads) != 3 || !slices.Equal(reads[0], []string{"a", "a"}) {
		t.Fatalf("default Counts changed: %v", reads)
	}
}
