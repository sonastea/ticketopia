package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

// copyingStore models a network backend: every Get returns fresh bytes, and a
// key can disappear (TTL expiry or eviction) independently of record times.
type copyingStore struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (s *copyingStore) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.values[key]
	if !ok {
		return nil, kv.ErrNotFound
	}
	return append([]byte(nil), value...), nil
}

func (s *copyingStore) Set(_ context.Context, key string, value []byte, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.values == nil {
		s.values = map[string][]byte{}
	}
	s.values[key] = append([]byte(nil), value...)
	return nil
}

func (s *copyingStore) evict(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, key)
}

func (*copyingStore) Close() error { return nil }

func TestDecodedRecordsFollowStoreExpiryAndInvalidation(t *testing.T) {
	store := &copyingStore{}
	s := New(t.Context(), store, zerolog.Nop(), Config{})
	now := time.Now()
	s.now = func() time.Time { return now }
	const key = "ticketmaster:v1:test"
	var fetches atomic.Int32
	read := func() ([]models.Genre, models.Freshness, error) {
		return cached(t.Context(), s, key, genrePolicy, func(context.Context) ([]models.Genre, error) {
			n := fetches.Add(1)
			return []models.Genre{{NamedID: models.NamedID{ID: fmt.Sprint("fetched-", n)}}}, nil
		})
	}
	write := func(id string, at time.Time) {
		writeRecord(t.Context(), s, key, cacheRecord[[]models.Genre]{Data: []models.Genre{{NamedID: models.NamedID{ID: id}}}, FetchedAt: at, FreshUntil: at.Add(genrePolicy.fresh), StaleUntil: at.Add(genrePolicy.retain)})
	}
	expect := func(step, id string, wantFetches int32) {
		t.Helper()
		items, meta, err := read()
		if err != nil || len(items) != 1 || items[0].ID != id || fetches.Load() != wantFetches || meta.Stale {
			t.Fatalf("%s: items=%+v meta=%+v fetches=%d err=%v; want %s after %d fetches", step, items, meta, fetches.Load(), err, id, wantFetches)
		}
	}

	write("first", now)
	expect("cold decode", "first", 0)
	expect("warm hit", "first", 0)

	// Another replica's write changes the shared bytes and must win at once.
	write("replica", now.Add(time.Second))
	expect("overwrite", "replica", 0)

	// Corruption is never masked by an earlier decoded record.
	_ = store.Set(t.Context(), key, []byte("broken JSON"), time.Hour)
	expect("corrupt entry", "fetched-1", 1)

	// Record freshness still governs remembered records.
	now = now.Add(genrePolicy.fresh + time.Second)
	expect("freshness expiry", "fetched-2", 2)

	// Backend expiry or eviction drops the remembered record too.
	store.evict(key)
	if _, ok := readRecord[[]models.Genre](t.Context(), s, key); ok {
		t.Fatal("evicted key served a remembered record")
	}
	s.decoded.mu.Lock()
	_, remembered := s.decoded.entries[key]
	s.decoded.mu.Unlock()
	if remembered {
		t.Fatal("evicted key kept its decoded record")
	}
	expect("after eviction", "fetched-3", 3)
}

func TestCachedResultsSurviveCallerModification(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/classifications.json" {
			_, _ = w.Write(categoriesFixture)
			return
		}
		_, _ = w.Write(eventsFixture)
	})
	q := testQuery(t, "city=Boston")
	read := func() (models.EventList, models.CategoryList) {
		t.Helper()
		list, err := s.Events(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		categories, err := s.Categories(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		return list, categories
	}
	content := func() string {
		t.Helper()
		list, categories := read()
		data, _ := json.Marshal([]any{list.Items, list.Total, list.NextCursor, categories.Items})
		return string(data)
	}
	want := content()
	for range 3 {
		list, categories := read()
		// Shared records reach callers clipped, so appends always copy rather
		// than racing other callers through spare backing capacity.
		if cap(list.Items) != len(list.Items) || cap(categories.Items) != len(categories.Items) {
			t.Fatalf("cached slices exposed spare capacity: events %d/%d categories %d/%d", len(list.Items), cap(list.Items), len(categories.Items), cap(categories.Items))
		}
		list.Items = append(list.Items, models.Event{ID: "ticketmaster:intruder"})
		edited := append([]models.Event(nil), list.Items...)
		edited[0].Name = "edited copy"
		list.Items = edited
		list.Total, list.Meta.Stale = -1, true
		categories.Items = append(categories.Items[:0:0], models.Category{NamedID: models.NamedID{ID: "intruder"}})
	}
	if got := content(); got != want {
		t.Fatalf("caller modifications leaked into cached results:\n got %s\nwant %s", got, want)
	}
	// A fresh fetch belongs to its callers; later hits decode the stored bytes
	// instead of sharing that value.
	s.now = func() time.Time { return time.Now().Add(eventPolicy.fresh + time.Minute) }
	fetched, _ := read()
	fetched.Items[0].Name = "mutated fetch"
	fetched.Items[0].Images[0].URL = "mutated fetch"
	s.now = time.Now
	if got := content(); got != want || calls.Load() != 3 {
		t.Fatalf("a mutated fetch leaked into later hits (calls=%d):\n got %s\nwant %s", calls.Load(), got, want)
	}
}

func TestMemoryStoreKeepsSetCallersPrivate(t *testing.T) {
	store := kv.NewMemory()
	t.Cleanup(func() { _ = store.Close() })
	value := []byte("original")
	if err := store.Set(t.Context(), "key", value, time.Hour); err != nil {
		t.Fatal(err)
	}
	copy(value, "modified")
	got, err := store.Get(t.Context(), "key")
	if err != nil || string(got) != "original" {
		t.Fatalf("Set retained the caller's buffer: %q %v", got, err)
	}
}

func TestDecodedCacheStaysWithinBudget(t *testing.T) {
	var c decodedCache
	chunk := make([]byte, decodedBudget/32)
	for i := range 200 {
		c.put(fmt.Sprint("key-", i), chunk, i)
		if c.size > decodedBudget || c.size != len(c.entries)*len(chunk) {
			t.Fatalf("budget accounting drifted: size=%d entries=%d", c.size, len(c.entries))
		}
	}
	c.put("key-199", chunk, "replaced")
	if c.get("key-199", chunk) != "replaced" || c.size != len(c.entries)*len(chunk) {
		t.Fatalf("replacing a key double-counted it: size=%d entries=%d", c.size, len(c.entries))
	}
	c.put("huge", make([]byte, decodedBudget/8), "huge")
	if c.get("huge", make([]byte, decodedBudget/8)) != nil {
		t.Fatal("an oversized record evicted the working set")
	}
	if c.get("key-199", append([]byte{1}, chunk[1:]...)) != nil {
		t.Fatal("different bytes returned a remembered record")
	}
}

func TestConcurrentHitsAndWritesShareRecordsSafely(t *testing.T) {
	s := New(t.Context(), kv.NewMemory(), zerolog.Nop(), Config{})
	t.Cleanup(func() { _ = s.cache.Close() })
	const key = "ticketmaster:v1:race"
	write := func(id string) {
		now := s.now()
		writeRecord(t.Context(), s, key, cacheRecord[[]models.Genre]{Data: []models.Genre{{NamedID: models.NamedID{ID: id}}}, FetchedAt: now, FreshUntil: now.Add(time.Hour), StaleUntil: now.Add(2 * time.Hour)})
	}
	write("v0")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			for range 200 {
				items, _, err := cached(t.Context(), s, key, genrePolicy, func(context.Context) ([]models.Genre, error) {
					return nil, errors.New("fetched despite a fresh record")
				})
				if err != nil || len(items) != 1 || !strings.HasPrefix(items[0].ID, "v") {
					errs <- fmt.Errorf("items=%+v err=%v", items, err)
					return
				}
			}
		})
	}
	for i := range 50 {
		write(fmt.Sprint("v", i+1))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
