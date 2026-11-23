package discovery

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

// A busy search must cost one catalog query, not one per visitor.
func TestStoredReadsShareOneQueryPerWindow(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := timedStoredService(t, 1, 0)
		s.stored.ttl = DefaultSharedReads
		var calls atomic.Int32
		release := make(chan struct{})
		s.catalog = storedTestCatalog{search: func(ctx context.Context, _ Query, _ time.Duration) (CatalogRead, error) {
			calls.Add(1)
			<-release
			return CatalogRead{List: models.EventList{Items: []models.Event{{ID: "ticketmaster:a"}}, Total: 1, Meta: models.Freshness{DataAsOf: time.Now()}}}, nil
		}}
		q := testQuery(t, "city=Boston&country=US")

		// The first visitor leaves mid-query; the others still get the result.
		leaving, leave := context.WithCancel(t.Context())
		var wg sync.WaitGroup
		errs := make(chan error, 9)
		wg.Go(func() { _, err := s.Events(leaving, q); errs <- err })
		for range 8 {
			wg.Go(func() {
				list, err := s.Events(t.Context(), q)
				if err == nil && len(list.Items) != 1 {
					err = errors.New("missing shared items")
				}
				errs <- err
			})
		}
		synctest.Wait()
		leave()
		close(release)
		wg.Wait()
		close(errs)
		canceled := 0
		for err := range errs {
			if errors.Is(err, context.Canceled) {
				canceled++
			} else if err != nil {
				t.Fatal(err)
			}
		}
		if calls.Load() != 1 || canceled != 1 {
			t.Fatalf("concurrent visitors were not coalesced: queries=%d canceled=%d", calls.Load(), canceled)
		}

		list, err := s.Events(t.Context(), q)
		if err != nil || calls.Load() != 1 {
			t.Fatalf("a repeat within the window queried again: queries=%d err=%v", calls.Load(), err)
		}
		// Shared items must not be changed by one caller's append.
		_ = append(list.Items, models.Event{ID: "ticketmaster:intruder"})
		if again, _ := s.Events(t.Context(), q); len(again.Items) != 1 {
			t.Fatalf("a caller's append changed the shared read: %+v", again.Items)
		}

		time.Sleep(DefaultSharedReads)
		if _, err := s.Events(t.Context(), q); err != nil || calls.Load() != 2 {
			t.Fatalf("an expired read was served: queries=%d err=%v", calls.Load(), err)
		}
	})
}

// Results an on-demand collection stores are served at once, not after the window.
func TestStoredReadsEndWhenCollectionFinishes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := timedStoredService(t, 1, 0)
		s.stored.ttl = DefaultSharedReads
		var mu sync.Mutex
		ids, complete := []string{"ticketmaster:old"}, false
		s.catalog = storedTestCatalog{
			search: func(context.Context, Query, time.Duration) (CatalogRead, error) {
				mu.Lock()
				defer mu.Unlock()
				list := models.EventList{Meta: models.Freshness{DataAsOf: time.Now()}}
				for _, id := range ids {
					list.Items = append(list.Items, models.Event{ID: id})
				}
				return CatalogRead{Refresh: !complete, List: list}, nil
			},
			claim: func(context.Context, Query) (string, error) { return "owner", nil },
			page: func(_ context.Context, _ Query, _ string, _ int, _ models.EventList) error {
				mu.Lock()
				defer mu.Unlock()
				ids = []string{"ticketmaster:new"}
				return nil
			},
			finish: func(_ context.Context, _ Query, _ string, status string, _ time.Duration) (string, error) {
				mu.Lock()
				defer mu.Unlock()
				complete = true
				return status, nil
			},
		}
		q := testQuery(t, "city=Boston&country=US")
		if list, err := s.Events(t.Context(), q); err != nil || list.Items[0].ID != "ticketmaster:old" {
			t.Fatalf("first read: %+v %v", list.Items, err)
		}
		s.WaitRefreshes()
		if list, err := s.Events(t.Context(), q); err != nil || list.Items[0].ID != "ticketmaster:new" {
			t.Fatalf("a shared read outlived the collection that replaced it: %+v %v", list.Items, err)
		}
	})
}

func TestStoredReadsDoNotRememberFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := timedStoredService(t, 1, 0)
		s.stored.ttl = DefaultSharedReads
		var calls atomic.Int32
		s.catalog = storedTestCatalog{search: func(context.Context, Query, time.Duration) (CatalogRead, error) {
			if calls.Add(1) == 1 {
				return CatalogRead{}, errors.New("database busy")
			}
			return CatalogRead{List: models.EventList{Items: []models.Event{{ID: "ticketmaster:a"}}, Meta: models.Freshness{DataAsOf: time.Now()}}}, nil
		}}
		q := testQuery(t, "city=Boston&country=US")
		if _, err := s.Events(t.Context(), q); err == nil {
			t.Fatal("the failed read was not reported")
		}
		if list, err := s.Events(t.Context(), q); err != nil || len(list.Items) != 1 || calls.Load() != 2 {
			t.Fatalf("a failure was served from the shared window: queries=%d err=%v", calls.Load(), err)
		}
	})
}

// A read taken before collection stored new results must not outlive them.
func TestStoredReadsDropReadsOlderThanRefresh(t *testing.T) {
	c := storedReads{ttl: DefaultSharedReads}
	now := time.Now()
	gen := c.generation()
	c.clear()
	c.put("k", CatalogRead{Refresh: true}, gen, 0, now)
	if _, ok := c.get("k", now, 0); ok {
		t.Fatal("a read from before the refresh was kept")
	}
	c.put("k", CatalogRead{}, c.generation(), 0, now)
	if _, ok := c.get("k", now, 0); !ok {
		t.Fatal("a current read was not kept")
	}
	// A catalog write in this process ends the shared window at once.
	if _, ok := c.get("k", now, 1); ok {
		t.Fatal("a read was served after a catalog write")
	}
	for i := range storedReadEntries + 10 {
		c.put(string(rune('a'+i%26))+time.Duration(i).String(), CatalogRead{}, c.generation(), 0, now)
	}
	if len(c.entries) > storedReadEntries {
		t.Fatalf("remembered %d reads, bound is %d", len(c.entries), storedReadEntries)
	}
}
