package discovery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

type storedTestCatalog struct {
	Catalog
	search func(context.Context, Query, time.Duration) (CatalogRead, error)
	claim  func(context.Context, Query) (string, error)
	page   func(context.Context, Query, string, int, models.EventList) error
	finish func(context.Context, Query, string, string, time.Duration) (string, error)
}

func (c storedTestCatalog) Search(ctx context.Context, q Query, freshFor time.Duration) (CatalogRead, error) {
	return c.search(ctx, q, freshFor)
}

func (c storedTestCatalog) ClaimSearch(ctx context.Context, q Query) (string, error) {
	return c.claim(ctx, q)
}

func (c storedTestCatalog) SearchPage(ctx context.Context, q Query, token string, page int, list models.EventList) error {
	return c.page(ctx, q, token, page, list)
}

func (c storedTestCatalog) FinishSearch(ctx context.Context, q Query, token, status string, delay time.Duration) (string, error) {
	return c.finish(ctx, q, token, status, delay)
}

type storedTestTransport func(*http.Request) (*http.Response, error)

func (f storedTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// Use an in-process transport so synctest can advance slow collection without
// real network I/O or wall-clock waits.
func timedStoredService(t *testing.T, pages int, fetchDelay time.Duration) *Service {
	t.Helper()
	cache := kv.NewMemory()
	t.Cleanup(func() { _ = cache.Close() })
	client := &http.Client{Timeout: 8 * time.Second, Transport: storedTestTransport(func(r *http.Request) (*http.Response, error) {
		if err := storedTestDelay(r.Context(), fetchDelay); err != nil {
			return nil, err
		}
		page := r.URL.Query().Get("page")
		body := fmt.Sprintf(`{"page":{"number":%s,"totalElements":%d,"totalPages":%d},"_embedded":{"events":[{"id":"show-%s","name":"Show"}]}}`, page, pages, pages, page)
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}, Request: r}, nil
	})}
	s := New(t.Context(), cache, zerolog.Nop(), Config{APIKey: "test-secret", HTTPClient: client})
	s.gate.interval = 0
	t.Cleanup(s.WaitRefreshes)
	return s
}

func storedTestDelay(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return ctx.Err()
	}
}

func TestStoredRefreshPageDeadlinesDoNotAccumulate(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const pages = 10
		s := timedStoredService(t, pages, 500*time.Millisecond)
		writes, finalized := 0, false
		s.catalog = storedTestCatalog{
			claim: func(context.Context, Query) (string, error) { return "owner", nil },
			page: func(ctx context.Context, _ Query, _ string, page int, _ models.EventList) error {
				if page != writes {
					t.Fatalf("page replayed or skipped: page=%d writes=%d", page, writes)
				}
				// Each SQL batch fits its own ten-second budget. Earlier pages
				// must not shorten the time available to later ingestion.
				ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
				defer cancel()
				if err := storedTestDelay(ctx, 2*time.Second); err != nil {
					return err
				}
				writes++
				return nil
			},
			finish: func(ctx context.Context, _ Query, token, status string, delay time.Duration) (string, error) {
				if ctx.Err() != nil || token != "owner" || status != "complete" || delay != s.freshFor {
					t.Errorf("collection did not complete: token=%s status=%s delay=%s err=%v", token, status, delay, ctx.Err())
				}
				finalized = true
				return status, nil
			},
		}
		start := time.Now()
		err := s.refreshSearch(testQuery(t, "city=Boston").CollectionScope())
		if err != nil || writes != pages || !finalized || time.Since(start) != 25*time.Second {
			t.Fatalf("multi-page refresh exhausted a shared deadline: writes=%d finalized=%v elapsed=%s err=%v", writes, finalized, time.Since(start), err)
		}
	})
}

func TestStoredColdWaitIsBoundedWithoutCancelingCollection(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := timedStoredService(t, 3, 0)
		var mu sync.Mutex
		var items []models.Event
		var at time.Time
		var complete bool
		s.catalog = storedTestCatalog{
			search: func(context.Context, Query, time.Duration) (CatalogRead, error) {
				mu.Lock()
				defer mu.Unlock()
				status := "not_collected"
				if len(items) > 0 {
					status = "partial"
				}
				if complete {
					status = "complete"
				}
				return CatalogRead{Refresh: !complete, List: models.EventList{
					Items: append([]models.Event{}, items...), Total: len(items),
					Meta: models.Freshness{DataAsOf: at, Coverage: &models.Coverage{Status: status}},
				}}, nil
			},
			claim: func(context.Context, Query) (string, error) { return "owner", nil },
			page: func(ctx context.Context, _ Query, _ string, _ int, list models.EventList) error {
				if err := storedTestDelay(ctx, 5*time.Second); err != nil {
					return err
				}
				mu.Lock()
				defer mu.Unlock()
				items = append(items, list.Items...)
				at = list.Meta.DataAsOf
				return nil
			},
			finish: func(ctx context.Context, _ Query, _ string, status string, _ time.Duration) (string, error) {
				if ctx.Err() != nil || status != "complete" {
					t.Errorf("collection was canceled with the cold wait: status=%s err=%v", status, ctx.Err())
				}
				mu.Lock()
				defer mu.Unlock()
				complete = status == "complete"
				return status, nil
			},
		}
		start := time.Now()
		q := testQuery(t, "city=Boston")
		list, err := s.Events(t.Context(), q)
		if err != nil || time.Since(start) != 12*time.Second || len(list.Items) != 2 || list.Meta.Coverage.Status != "partial" {
			t.Fatalf("cold search did not return committed partial results within its wait budget: list=%+v elapsed=%s err=%v", list, time.Since(start), err)
		}
		s.WaitRefreshes()
		list, err = s.Events(t.Context(), q)
		if err != nil || time.Since(start) != 15*time.Second || len(list.Items) != 3 || list.Meta.Coverage.Status != "complete" {
			t.Fatalf("cold wait aborted background completion: list=%+v elapsed=%s err=%v", list, time.Since(start), err)
		}
	})
}

func TestStoredRefreshBoundsAndFinalizesStalledPage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		s := timedStoredService(t, 1, 0)
		finalized := false
		s.catalog = storedTestCatalog{
			claim: func(context.Context, Query) (string, error) { return "owner", nil },
			page: func(ctx context.Context, _ Query, _ string, _ int, _ models.EventList) error {
				<-ctx.Done()
				return ctx.Err()
			},
			finish: func(ctx context.Context, _ Query, _ string, status string, delay time.Duration) (string, error) {
				if ctx.Err() != nil || status != "failed" || delay != time.Hour {
					t.Errorf("stalled page lost independent failure cleanup: status=%s delay=%s err=%v", status, delay, ctx.Err())
				}
				finalized = true
				return status, nil
			},
		}
		start := time.Now()
		err := s.refreshSearch(testQuery(t, "city=Boston").CollectionScope())
		if !errors.Is(err, context.DeadlineExceeded) || !finalized || time.Since(start) != 20*time.Second {
			t.Fatalf("stalled page did not respect its bound: finalized=%v elapsed=%s err=%v", finalized, time.Since(start), err)
		}
	})
}

func TestStoredRefreshSurvivesRequestCancellation(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(eventsFixture) })
	t.Cleanup(s.WaitRefreshes)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	var committed atomic.Bool
	s.catalog = storedTestCatalog{
		search: func(ctx context.Context, _ Query, _ time.Duration) (CatalogRead, error) {
			if err := ctx.Err(); err != nil {
				return CatalogRead{}, err
			}
			read := CatalogRead{Refresh: !committed.Load()}
			if committed.Load() {
				read.List.Items = []models.Event{{ID: "ticketmaster:show-1"}}
			}
			return read, nil
		},
		claim: func(context.Context, Query) (string, error) { return "owner", nil },
		page: func(ctx context.Context, _ Query, _ string, _ int, _ models.EventList) error {
			close(started)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return ctx.Err()
			}
		},
		finish: func(ctx context.Context, _ Query, _ string, status string, _ time.Duration) (string, error) {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			committed.Store(true)
			close(finished)
			return status, nil
		},
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	q := testQuery(t, "city=Boston")
	go func() { _, err := s.Events(ctx, q); done <- err }()
	<-started
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled caller returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("canceled request kept waiting for the shared refresh")
	}
	unblock.Do(func() { close(release) })
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("browser cancellation aborted shared catalog ingestion")
	}
	list, err := s.Events(t.Context(), q)
	if err != nil || len(list.Items) != 1 {
		t.Fatalf("next visitor lost the refreshed catalog: %+v %v", list, err)
	}
}

func TestStoredRefreshReturnsRetainedResultsWithoutWaiting(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(eventsFixture) })
	t.Cleanup(s.WaitRefreshes)
	started, release, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	s.catalog = storedTestCatalog{
		search: func(context.Context, Query, time.Duration) (CatalogRead, error) {
			return CatalogRead{Refresh: true, List: models.EventList{
				Items: []models.Event{{ID: "ticketmaster:retained"}},
				Meta:  models.Freshness{Stale: true, Coverage: &models.Coverage{Status: "stale"}},
			}}, nil
		},
		claim: func(context.Context, Query) (string, error) { return "owner", nil },
		page: func(ctx context.Context, _ Query, _ string, _ int, _ models.EventList) error {
			close(started)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-release:
				return nil
			}
		},
		finish: func(_ context.Context, _ Query, _ string, status string, _ time.Duration) (string, error) {
			close(finished)
			return status, nil
		},
	}
	type result struct {
		list models.EventList
		err  error
	}
	done := make(chan result, 1)
	go func() {
		list, err := s.Events(t.Context(), testQuery(t, "city=Boston"))
		done <- result{list, err}
	}()
	<-started
	select {
	case got := <-done:
		if got.err != nil || len(got.list.Items) != 1 || got.list.Items[0].ID != "ticketmaster:retained" || !got.list.Meta.Stale || got.list.Meta.Coverage.Status != "stale" {
			t.Fatalf("retained results/freshness lost: %+v %v", got.list, got.err)
		}
	case <-time.After(time.Second):
		unblock.Do(func() { close(release) })
		<-done
		t.Fatal("retained results waited for catalog ingestion")
	}
	unblock.Do(func() { close(release) })
	<-finished
}

func TestStoredRefreshFinalizesIngestionFailure(t *testing.T) {
	for _, failPage := range []int{0, 1} {
		t.Run(fmt.Sprintf("page_%d", failPage), func(t *testing.T) {
			s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"page":{"number":%s,"totalElements":2,"totalPages":2},"_embedded":{"events":[{"id":"show","name":"Show"}]}}`, r.URL.Query().Get("page"))
			})
			t.Cleanup(s.WaitRefreshes)
			ingestionErr := errors.New("database ingestion failed")
			var finalized atomic.Bool
			writes := 0
			s.catalog = storedTestCatalog{
				search: func(context.Context, Query, time.Duration) (CatalogRead, error) {
					return CatalogRead{Refresh: !finalized.Load()}, nil
				},
				claim: func(context.Context, Query) (string, error) { return "owner", nil },
				page: func(_ context.Context, _ Query, _ string, page int, _ models.EventList) error {
					writes++
					if page == failPage {
						return ingestionErr
					}
					return nil
				},
				finish: func(ctx context.Context, _ Query, token, status string, delay time.Duration) (string, error) {
					want := "failed"
					if failPage > 0 {
						want = "partial"
					}
					if ctx.Err() != nil || token != "owner" || status != want || delay != time.Hour {
						t.Errorf("incorrect failure completion: token=%s status=%s delay=%s err=%v", token, status, delay, ctx.Err())
					}
					finalized.Store(true)
					return status, nil
				},
			}
			err := s.refreshSearch(testQuery(t, "city=Boston").CollectionScope())
			if !errors.Is(err, ingestionErr) || !finalized.Load() || writes != failPage+1 {
				t.Fatalf("ingestion failure was not finalized, or retried uncertain writes: err=%v finalized=%v writes=%d", err, finalized.Load(), writes)
			}
		})
	}
}

func TestStoredRefreshShutdownCancelsAndDrainsCompletion(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(eventsFixture) })
	serviceCtx, shutdown := context.WithCancel(t.Context())
	s.ctx = serviceCtx
	defer shutdown()
	t.Cleanup(s.WaitRefreshes)
	started, finishing, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var unblock sync.Once
	t.Cleanup(func() { unblock.Do(func() { close(release) }) })
	s.catalog = storedTestCatalog{
		search: func(context.Context, Query, time.Duration) (CatalogRead, error) {
			return CatalogRead{Refresh: true}, nil
		},
		claim: func(context.Context, Query) (string, error) { return "owner", nil },
		page: func(ctx context.Context, _ Query, _ string, _ int, _ models.EventList) error {
			close(started)
			<-ctx.Done()
			return ctx.Err()
		},
		finish: func(ctx context.Context, _ Query, _ string, status string, delay time.Duration) (string, error) {
			deadline, bounded := ctx.Deadline()
			if ctx.Err() != nil || !bounded || time.Until(deadline) > 5*time.Second || status != "failed" || delay != time.Hour {
				t.Errorf("shutdown lost bounded independent failure cleanup: status=%s delay=%s deadline=%v err=%v", status, delay, deadline, ctx.Err())
			}
			close(finishing)
			select {
			case <-release:
				return status, nil
			case <-ctx.Done():
				return "", ctx.Err()
			}
		},
	}
	done := make(chan error, 1)
	q := testQuery(t, "city=Boston")
	go func() { _, err := s.Events(t.Context(), q); done <- err }()
	<-started
	shutdown()
	select {
	case <-finishing:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not cancel collection and finalize its claim")
	}
	drained := make(chan struct{})
	go func() { s.WaitRefreshes(); close(drained) }()
	select {
	case <-drained:
		t.Fatal("shutdown drain returned before database completion")
	default:
	}
	unblock.Do(func() { close(release) })
	select {
	case <-drained:
	case <-time.After(time.Second):
		t.Fatal("shutdown did not drain the completed refresh")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
