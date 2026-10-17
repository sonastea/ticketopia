package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func candidateFixture(n int) Candidates {
	now := time.Now().UTC()
	data := Candidates{AsOf: now}
	for i := 0; i < n; i++ {
		data.Items = append(data.Items, Candidate{Item: Item{EventID: fmt.Sprintf("ticketmaster:%d", i), EventTitle: "A concert", RecentRecommendations: 2, LatestRecommendedAt: now.Add(-time.Duration(i) * time.Millisecond), ExpiresAt: now.Add(time.Minute)}, CityKey: fmt.Sprint(i % 7), CategoryIDs: []string{fmt.Sprint(i % 3), fmt.Sprint(i % 5)}})
	}
	return data
}
func TestCompleteWindowReferenceAndStableSlots(t *testing.T) {
	data := candidateFixture(300)
	data.Items[10].CityKey = "" // Missing location matches Everywhere only.
	x, err := buildIndex(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	for city := -1; city < 8; city++ {
		for category := -1; category < 6; category++ {
			f := DefaultPreferences().Filters
			key := fmt.Sprint(city)
			if city >= 0 {
				f.Location = Location{Mode: "city", City: "Example", Country: "DE"}
			}
			if category >= 0 {
				f.CategoryIDs = []string{fmt.Sprint(category), "4"}
			}
			want := []Item{}
			for _, c := range data.Items {
				if city >= 0 && c.CityKey != key {
					continue
				}
				match := category < 0
				for _, id := range c.CategoryIDs {
					if id == fmt.Sprint(category) || id == "4" {
						match = true
					}
				}
				if match {
					want = append(want, c.Item)
				}
				if len(want) == 20 {
					break
				}
			}
			got := x.selectView(f, key)
			if !reflect.DeepEqual(got.Items, want) {
				t.Fatalf("city=%d category=%d", city, category)
			}
		}
	}
	data.Items[0], data.Items[299] = data.Items[299], data.Items[0]
	data.Items[1].Item.EventTitle = "Corrected title"
	y, err := buildIndex(data, x)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(x.slots, y.slots) {
		t.Fatal("reorder changed membership slots")
	}
	data.Items = data.Items[1:]
	data.Items = append(data.Items, Candidate{Item: Item{EventID: "ticketmaster:new"}, CityKey: "new"})
	z, err := buildIndex(data, y)
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := z.slots["ticketmaster:299"]; exists {
		t.Fatal("removed slot retained")
	}
	if z.cities["5"] != nil && z.cities["5"].has(z.slots["ticketmaster:new"]) {
		t.Fatal("reused slot retained old facets")
	}
}
func TestCandidateBudgets(t *testing.T) {
	if _, err := buildIndex(candidateFixture(MaxCandidates+1), nil); err == nil {
		t.Fatal("truncated overflowing window")
	}
	data := candidateFixture(1)
	data.Items[0].Item.EventTitle = strings.Repeat("x", MemoryBudget)
	if _, err := buildIndex(data, nil); err == nil {
		t.Fatal("memory budget ignored")
	}
}

type candidateRepository struct {
	batchRepository
	data Candidates
}

func (r *candidateRepository) Candidates(context.Context) (Candidates, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.polls++
	return r.data, nil
}
func (*candidateRepository) LocationKey(_ context.Context, l Location) (string, error) {
	return l.City, nil
}
func TestSharedColdReadsAndWarmSubscription(t *testing.T) {
	r := &candidateRepository{data: candidateFixture(300)}
	s := New(t.Context(), r, 256)
	defer s.Close()
	f := DefaultPreferences().Filters
	f.Location = Location{Mode: "city", City: "1", Country: "DE"}
	f.CategoryIDs = []string{"2", "2", "1"}
	var wg sync.WaitGroup
	for range 30 {
		wg.Go(func() {
			if _, err := s.FilteredSnapshot(t.Context(), f); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	r.mu.Lock()
	polls := r.polls
	r.mu.Unlock()
	if polls != 1 || len(s.views) != 1 {
		t.Fatal("duplicate cohort work", polls, len(s.views))
	}
	first, _ := s.FilteredSnapshot(t.Context(), f)
	f.CategoryIDs = []string{"1", "2"}
	second, _ := s.FilteredSnapshot(t.Context(), f)
	if len(first.encoded) == 0 || &first.encoded[0] != &second.encoded[0] {
		t.Fatal("identical filters re-encoded the shared view")
	}
	ch, release, err := s.Subscribe(nil, f)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	select {
	case u := <-ch:
		if len(u.Snapshot.Items) == 0 {
			t.Fatal("missing warm view")
		}
	default:
		t.Fatal("warm subscription waited for poll/counts")
	}
}
func TestFilterValidationAndCanonicalization(t *testing.T) {
	f, err := DecodePublicFilters([]byte(` {"location":{"mode":"city","city":"  Berlin   Mitte ","country":"de"},"category_ids":["Music","Arts","Music"]} `))
	if err != nil || f.Location.City != "Berlin Mitte" || f.Location.Country != "DE" || strings.Join(f.CategoryIDs, ",") != "Arts,Music" {
		t.Fatal(f, err)
	}
	for _, raw := range []string{`{"connections":{"mode":"all_events","sources":[]}}`, `{"category_ids":[],"category_ids":["Music"]}`, `{"location":{"mode":"radius"}}`, `{"location":{"mode":"city","city":"Berlin"}}`, `{"location":{"mode":"all","city":"Berlin"}}`, `{"category_ids":null}`, `{"Category_ids":[]}`, `{"artist_ids":["ticketmaster:a"]}`} {
		if _, err := DecodePublicFilters([]byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	p := DefaultPreferences()
	p.Filters = f
	data, _ := json.Marshal(p)
	if _, err := DecodePreferences(data); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshotCohortBudget(t *testing.T) {
	r := &candidateRepository{data: candidateFixture(30)}
	s := New(t.Context(), r, 256)
	defer s.Close()
	f := DefaultPreferences().Filters
	ch, release, err := s.Subscribe(nil, f)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_ = ch
	for i := 0; i < 300; i++ {
		f.CategoryIDs = []string{fmt.Sprintf("category%d", i)}
		if _, err := s.FilteredSnapshot(t.Context(), f); err != nil {
			t.Fatal(err)
		}
	}
	s.projection <- struct{}{}
	if len(s.views) > 129 {
		t.Fatal("unbounded snapshot-only cohorts", len(s.views))
	}
	<-s.projection
}
func BenchmarkCandidateSelection(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			data := candidateFixture(n)
			x, err := buildIndex(data, nil)
			if err != nil {
				b.Fatal(err)
			}
			f := DefaultPreferences().Filters
			f.Location = Location{Mode: "city", City: "1", Country: "DE"}
			f.CategoryIDs = []string{"2"}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				x.selectView(f, "1")
			}
		})
	}
}
func BenchmarkCandidateBuild(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			data := candidateFixture(n)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := buildIndex(data, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
