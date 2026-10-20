package activity

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

type batchRepository struct {
	mu      sync.Mutex
	batches [][]string
	polls   int
}

func (r *batchRepository) Snapshot(context.Context) (Snapshot, error) {
	r.mu.Lock()
	r.polls++
	r.mu.Unlock()
	return Snapshot{AsOf: time.Now().UTC(), Items: []Item{}}, nil
}
func (r *batchRepository) Counts(_ context.Context, ids []string) (Counts, error) {
	r.mu.Lock()
	r.batches = append(r.batches, append([]string(nil), ids...))
	r.mu.Unlock()
	result := Counts{AsOf: time.Now().UTC(), Items: []Count{}}
	for _, id := range ids {
		result.Items = append(result.Items, Count{EventID: id, InterestedCount: 3})
	}
	return result, nil
}
func (*batchRepository) Cleanup(context.Context) error { return nil }

func TestUnionBatchingAndSlowSubscriberIsolation(t *testing.T) {
	repo := &batchRepository{}
	service := New(t.Context(), repo, 4)
	defer service.Close()
	all := []<-chan Update{}
	for offset := range 3 {
		ids := []string{}
		for i := range 101 {
			ids = append(ids, fmt.Sprintf("ticketmaster:event%d", offset*50+i))
		}
		ch, release, err := service.Subscribe(ids)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		all = append(all, ch)
	}
	// This subscriber deliberately never drains its latest-state buffer.
	_, release, err := service.Subscribe(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, ch := range all {
		select {
		case u := <-ch:
			if u.Unavailable || u.Counts == nil || len(u.Counts.Items) != 101 {
				t.Fatal("incomplete subscriber counts", u)
			}
		case <-time.After(4 * time.Second):
			t.Fatal("slow subscriber blocked delivery")
		}
	}
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if repo.polls != 1 || len(repo.batches) != 2 {
		t.Fatal("reads were not shared", repo.polls, len(repo.batches))
	}
	seen := map[string]bool{}
	for _, batch := range repo.batches {
		if len(batch) > 101 {
			t.Fatal("unbounded count query")
		}
		for _, id := range batch {
			if seen[id] {
				t.Fatal("duplicate union read")
			}
			seen[id] = true
		}
	}
	if len(seen) != 201 {
		t.Fatal("missing requested IDs", len(seen))
	}
}

func TestJoiningTabsReuseFreshCounts(t *testing.T) {
	repo := &batchRepository{}
	s := New(t.Context(), repo, 4)
	defer s.Close()
	wait := func(ch <-chan Update) {
		t.Helper()
		timer := time.NewTimer(4 * time.Second)
		defer timer.Stop()
		for {
			select {
			case u := <-ch:
				if u.Unavailable {
					t.Fatal("unavailable")
				}
				if u.Counts != nil {
					return
				}
			case <-timer.C:
				t.Fatal("initial counts missing")
			}
		}
	}
	first, release, err := s.Subscribe([]string{"ticketmaster:shared"})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	wait(first)
	second, release, err := s.Subscribe([]string{"ticketmaster:shared", "ticketmaster:new"})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	wait(second)
	repo.mu.Lock()
	defer repo.mu.Unlock()
	if len(repo.batches) != 2 || len(repo.batches[1]) != 1 || repo.batches[1][0] != "ticketmaster:new" {
		t.Fatal("joining tab reread existing counts", repo.batches)
	}
}
