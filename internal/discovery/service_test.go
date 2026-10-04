package discovery

import (
	"bytes"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/kv"
)

//go:embed testdata/events.json
var eventsFixture []byte

func newTestService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cache := kv.NewMemory()
	t.Cleanup(func() { _ = cache.Close() })
	s := New(t.Context(), cache, zerolog.Nop(), Config{APIKey: "test-secret", BaseURL: server.URL})
	s.gate.interval = 0
	return s
}

func testQuery(t *testing.T, values string) Query {
	t.Helper()
	params, err := url.ParseQuery(values)
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseQuery(params, time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func TestSearchPreservesMetadataAndSharesDetailCache(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		q := r.URL.Query()
		if r.URL.Path != "/events.json" || q.Get("apikey") != "test-secret" || q.Get("page") != "0" || q.Get("segmentId") != musicSegment || q.Get("city") != "Boston" || q.Get("localStartDateTime") != "2026-10-02T00:00:00,2026-12-31T23:59:59" {
			t.Errorf("unexpected upstream filters (path %s)", r.URL.Path)
		}
		_, _ = w.Write(eventsFixture)
	})
	first, err := s.Events(t.Context(), testQuery(t, "city=Boston&country=us"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Events(t.Context(), testQuery(t, "country=US&city=++Boston++&limit=020"))
	if err != nil || calls.Load() != 1 || !second.Meta.DataAsOf.Equal(first.Meta.DataAsOf) {
		t.Fatalf("equivalent filters did not share cache: calls=%d err=%v", calls.Load(), err)
	}
	if len(first.Items) != 2 || first.Items[0].ID == first.Items[1].ID || first.NextCursor != nil {
		t.Fatalf("identity or last-page mismatch: %+v", first)
	}
	event := first.Items[0]
	if event.ID != "ticketmaster:show-1" || event.Source.URL != "https://www.ticketmaster.com/event/show-1" || event.Start.DateTime.Format(time.RFC3339) != "2026-10-12T23:30:00Z" || event.PublicSale.Start.Format(time.RFC3339) != "2026-09-01T14:00:00Z" {
		t.Fatalf("identity, ticket URL, or show/sale dates lost: %+v", event)
	}
	if *event.PriceRanges[0].Min != "25.10" || event.PriceRanges[0].FeesIncluded != nil || event.Venues[0].City != "Boston" || event.Artists[0].ID != "ticketmaster:artist-1" || event.PleaseNote != "All ages." || event.Classifications[0].Subgenre.Name != "Alternative" || event.Status != "rescheduled" {
		t.Fatalf("metadata lost: %+v", event)
	}
	sparse := first.Items[1]
	if sparse.Start.DateTime != nil || sparse.PublicSale.Start != nil || sparse.PriceRanges != nil || sparse.Artists == nil || sparse.Venues == nil {
		t.Fatalf("missing metadata misrepresented: %+v", sparse)
	}
	detail, err := s.Event(t.Context(), event.ID)
	if err != nil || detail.Item.Name != event.Name || calls.Load() != 1 {
		t.Fatalf("list-to-detail caused another request: calls=%d err=%v", calls.Load(), err)
	}
}

func TestConcurrentMissesAndCancelledWaiter(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		_, _ = w.Write(eventsFixture)
	})
	q := testQuery(t, "")
	ctx, cancel := context.WithCancel(t.Context())
	first := make(chan error, 1)
	go func() { _, err := s.Events(ctx, q); first <- err }()
	<-started
	var wg sync.WaitGroup
	for range 30 {
		wg.Go(func() {
			if _, err := s.Events(t.Context(), q); err != nil {
				t.Errorf("shared request failed: %v", err)
			}
		})
	}
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled waiter returned %v", err)
	}
	close(release)
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("concurrent miss made %d calls", calls.Load())
	}
}

func TestStaleFallbackCooldownAndHardExpiry(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) > 1 {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write(eventsFixture)
	})
	now := time.Now()
	s.now = func() time.Time { return now }
	q := testQuery(t, "")
	fresh, err := s.Events(t.Context(), q)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour + time.Second)
	for range 3 {
		stale, err := s.Events(t.Context(), q)
		if err != nil || !stale.Meta.Stale || !stale.Meta.DataAsOf.Equal(fresh.Meta.DataAsOf) || len(stale.Items) != 2 {
			t.Fatalf("stale fallback lost successful observation: %+v %v", stale, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("cooldown made %d calls", calls.Load())
	}
	now = now.Add(24 * time.Hour)
	_, err = s.Events(t.Context(), q)
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) || unavailable.RetryAfter < 119*time.Second || calls.Load() != 2 {
		t.Fatalf("hard expiry or Retry-After failed: %v calls=%d", err, calls.Load())
	}
}

func TestRefreshAfterExpiryAndCorruptCache(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write(eventsFixture)
	})
	now := time.Now()
	s.now = func() time.Time { return now }
	q := testQuery(t, "")
	if _, err := s.Events(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Hour + time.Second)
	refreshed, err := s.Events(t.Context(), q)
	if err != nil || refreshed.Meta.Stale || !refreshed.Meta.DataAsOf.Equal(now) || calls.Load() != 2 {
		t.Fatalf("expired cache not refreshed: %v calls=%d", err, calls.Load())
	}
	_ = s.cache.Set(t.Context(), q.cacheKey(), []byte("broken JSON"), time.Hour)
	if _, err := s.Events(t.Context(), q); err != nil || calls.Load() != 3 {
		t.Fatalf("corrupt cache not replaced: %v calls=%d", err, calls.Load())
	}
}

func TestEmptyResultsAndFilterIsolation(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = fmt.Fprint(w, `{"page":{"number":0,"size":20,"totalElements":0,"totalPages":0}}`)
	})
	for _, city := range []string{"Boston", "Chicago", "Boston"} {
		list, err := s.Events(t.Context(), testQuery(t, "city="+city))
		if err != nil || list.Items == nil || len(list.Items) != 0 || list.NextCursor != nil {
			t.Fatalf("invalid empty results: %+v %v", list, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("filter isolation or empty-result cache failed: %d calls", calls.Load())
	}
}

func TestMissingEventDoesNotServeStaleAndIsNegativelyCached(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = fmt.Fprint(w, `{"id":"show-1","name":"Old show"}`)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	})
	now := time.Now()
	s.now = func() time.Time { return now }
	if _, err := s.Event(t.Context(), "ticketmaster:show-1"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	for range 2 {
		if _, err := s.Event(t.Context(), "ticketmaster:show-1"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("missing event must not resurrect stale data: %v", err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("negative cache missed: calls=%d", calls.Load())
	}
	now = now.Add(6 * time.Minute)
	_, _ = s.Event(t.Context(), "ticketmaster:show-1")
	if calls.Load() != 3 {
		t.Fatal("negative cache did not expire")
	}
}

func TestGenreCatalogLongerTTL(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/classifications/segments/"+musicSegment+".json" {
			t.Errorf("wrong metadata endpoint: %s", r.URL.Path)
		}
		_, _ = fmt.Fprintf(w, `{"id":%q,"_embedded":{"genres":[{"id":"rock","name":"Rock","_embedded":{"subgenres":[{"id":"alt","name":"Alternative"}]}},{"id":"jazz","name":"Jazz"}]}}`, musicSegment)
	})
	now := time.Now()
	s.now = func() time.Time { return now }
	first, err := s.Genres(t.Context())
	if err != nil || len(first.Items) != 2 || first.Items[0].Name != "Jazz" || first.Items[1].Subgenres[0].ID != "alt" {
		t.Fatalf("catalog metadata failed: %+v %v", first, err)
	}
	now = now.Add(23 * time.Hour)
	_, _ = s.Genres(t.Context())
	if calls.Load() != 1 {
		t.Fatal("metadata expired too soon")
	}
	now = now.Add(2 * time.Hour)
	_, _ = s.Genres(t.Context())
	if calls.Load() != 2 {
		t.Fatal("metadata not refreshed")
	}
}

func TestBudgetKeepsCacheReadable(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write(eventsFixture)
	})
	s.gate.budget = 1
	q := testQuery(t, "city=Boston")
	if _, err := s.Events(t.Context(), q); err != nil {
		t.Fatal(err)
	}
	_, err := s.Events(t.Context(), testQuery(t, "city=Chicago"))
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) || calls.Load() != 1 {
		t.Fatalf("budget not enforced: %v calls=%d", err, calls.Load())
	}
	if _, err := s.Events(t.Context(), q); err != nil {
		t.Fatalf("budget blocked cached read: %v", err)
	}
}

func TestProviderFailuresNeverLeakKeyOrCacheInvalidData(t *testing.T) {
	for _, body := range []string{`{"fault":"test-secret"}`, `null`, `{`, `{"page":{"number":0},"_embedded":{"events":[{"name":"no id"}]}}`} {
		t.Run(body, func(t *testing.T) {
			var calls atomic.Int32
			s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				_, _ = fmt.Fprint(w, body)
			})
			var log bytes.Buffer
			s.logger = zerolog.New(&log)
			q := testQuery(t, "")
			_, err := s.Events(t.Context(), q)
			if err == nil || strings.Contains(err.Error()+log.String(), "test-secret") {
				t.Fatalf("invalid provider response or leaked key: %v", err)
			}
			if _, err := s.cache.Get(t.Context(), q.cacheKey()); !errors.Is(err, kv.ErrNotFound) {
				t.Fatal("provider error was cached as valid data")
			}
			_, _ = s.Events(t.Context(), q)
			if calls.Load() != 1 {
				t.Fatal("invalid-response cooldown was ignored")
			}
		})
	}
}

func TestRetryDelayUsesDocumentedQuotaReset(t *testing.T) {
	now := time.Now()
	headers := http.Header{"Retry-After": {"120"}, "Rate-Limit-Available": {"0"}, "Rate-Limit-Reset": {fmt.Sprint(now.Add(time.Hour).UnixMilli())}}
	if delay := retryDelay(headers, now); delay < time.Hour-time.Second || delay > time.Hour {
		t.Fatalf("quota reset not honored: %v", delay)
	}
	headers.Del("Rate-Limit-Available")
	if delay := retryDelay(headers, now); delay != 120*time.Second {
		t.Fatalf("daily reset incorrectly used for burst limit: %v", delay)
	}
	headers.Set("Retry-After", now.Add(2*time.Minute).UTC().Format(http.TimeFormat))
	if delay := retryDelay(headers, now); delay < 119*time.Second || delay > 120*time.Second {
		t.Fatalf("HTTP-date Retry-After not honored: %v", delay)
	}
}

func TestTimeoutRedactsTransportURLAndPausesMisses(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		<-r.Context().Done()
	})
	s.client = &http.Client{Timeout: 20 * time.Millisecond}
	var log bytes.Buffer
	s.logger = zerolog.New(&log)
	_, err := s.Events(t.Context(), testQuery(t, ""))
	var unavailable *UnavailableError
	if !errors.As(err, &unavailable) || strings.Contains(err.Error()+log.String(), "test-secret") {
		t.Fatalf("timeout was not safely represented: %v", err)
	}
	_, _ = s.Events(t.Context(), testQuery(t, "city=Chicago"))
	if calls.Load() != 1 {
		t.Fatal("timeout cooldown did not pause other cache misses")
	}
}

type unavailableStore struct{}

func (unavailableStore) Get(context.Context, string) ([]byte, error) {
	return nil, errors.New("cache offline")
}
func (unavailableStore) Set(context.Context, string, []byte, time.Duration) error {
	return errors.New("cache offline")
}
func (unavailableStore) Close() error { return nil }

func TestSharedCacheFailureStillReusesLocalResults(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		_, _ = w.Write(eventsFixture)
	})
	s.cache = kv.WithFallback(unavailableStore{}, s.cache, zerolog.Nop())
	q := testQuery(t, "")
	for range 3 {
		if _, err := s.Events(t.Context(), q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Event(t.Context(), "ticketmaster:show-1"); err != nil || calls.Load() != 1 {
		t.Fatalf("remote cache failure caused repeat fetches: err=%v calls=%d", err, calls.Load())
	}
}

func TestRequestGatePacesCallsAndHonorsCancellation(t *testing.T) {
	g := requestGate{budget: 10, interval: 10 * time.Millisecond}
	if err := g.wait(t.Context()); err != nil {
		t.Fatal(err)
	}
	next := g.next
	if err := g.wait(t.Context()); err != nil || time.Now().Before(next) {
		t.Fatalf("request started before the allowed interval: %v", err)
	}
	g.next = time.Now().Add(time.Hour)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := g.wait(ctx); !errors.Is(err, context.Canceled) || g.used != 2 {
		t.Fatalf("cancelled wait consumed request budget: %v used=%d", err, g.used)
	}
}

func TestProviderFilterRejectionDoesNotPauseOtherSearches(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, "test-secret")
			return
		}
		_, _ = w.Write(eventsFixture)
	})
	_, err := s.Events(t.Context(), testQuery(t, "genre_id=invalid"))
	var invalid *ValidationError
	if !errors.As(err, &invalid) || strings.Contains(err.Error(), "test-secret") {
		t.Fatalf("provider filter rejection was not safely reported: %v", err)
	}
	if _, err := s.Events(t.Context(), testQuery(t, "city=Boston")); err != nil || calls.Load() != 2 {
		t.Fatalf("rejected filters paused unrelated reads: %v", err)
	}
}

func TestMissingVenueIdentityKeepsLocationAndTBAFlags(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `{"id":"pop-up","name":"Pop-up show","dates":{"start":{"localDate":"2026-10-02","dateTime":"2026-10-02T20:00:00Z","timeTBA":true}},"sales":{"public":{"startDateTime":"2026-09-01T14:00:00Z","startTBD":true}},"_embedded":{"venues":[{"name":"Temporary stage","timezone":"America/New_York","city":{"name":"Boston"}}]}}`)
	})
	detail, err := s.Event(t.Context(), "ticketmaster:pop-up")
	if err != nil {
		t.Fatal(err)
	}
	event := detail.Item
	if len(event.Venues) != 0 || event.Place == nil || event.Place.City != "Boston" || *event.Start.Timezone != "America/New_York" || event.Start.DateTime != nil || event.PublicSale.Start != nil || !event.Start.TimeTBA || !event.PublicSale.StartTBD {
		t.Fatalf("partial metadata or unknown dates misrepresented: %+v", event)
	}
}
