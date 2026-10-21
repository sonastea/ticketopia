package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
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

// Duplicate tabs/fallback readers should reuse a warm cohort without work
// proportional to the candidate window on every read.
func BenchmarkWarmSharedSnapshot(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			r := &candidateRepository{data: candidateFixture(n)}
			s := New(b.Context(), r, 256)
			defer s.Close()
			if _, err := s.Snapshot(b.Context()); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			for b.Loop() {
				if _, err := s.Snapshot(b.Context()); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// A fact omitted from the fingerprint would let a cached view (for example a
// redacted first name or corrected city) survive a changed generation.
func TestFingerprintCoversEveryCandidateField(t *testing.T) {
	base := candidateFixture(2)
	original := fingerprint(base.Items)
	mutate := func(v reflect.Value) {
		switch v.Interface().(type) {
		case string:
			v.SetString(v.String() + "changed")
		case int:
			v.SetInt(v.Int() + 1)
		case time.Time:
			v.Set(reflect.ValueOf(v.Interface().(time.Time).Add(time.Nanosecond)))
		case *string:
			changed := "Changed"
			v.Set(reflect.ValueOf(&changed))
		case []string:
			v.Set(reflect.ValueOf(append(slices.Clone(v.Interface().([]string)), "changed")))
		default:
			t.Fatalf("unhandled candidate field type %s", v.Type())
		}
	}
	candidate := reflect.TypeFor[Candidate]()
	for i := range candidate.NumField() {
		paths := [][]int{{i}}
		if candidate.Field(i).Type == reflect.TypeFor[Item]() {
			paths = nil
			for j := range candidate.Field(i).Type.NumField() {
				paths = append(paths, []int{i, j})
			}
		}
		for _, field := range paths {
			items := slices.Clone(base.Items)
			mutate(reflect.ValueOf(&items[1]).Elem().FieldByIndex(field))
			if fingerprint(items) == original {
				t.Errorf("fingerprint ignores %v", field)
			}
		}
	}
	// Moving a value across a list boundary must also change the digest.
	split := slices.Clone(base.Items)
	split[0].CategoryIDs, split[1].CategoryIDs = []string{"a", "b"}, []string{"c"}
	joined := slices.Clone(base.Items)
	joined[0].CategoryIDs, joined[1].CategoryIDs = []string{"a"}, []string{"b", "c"}
	if fingerprint(split) == fingerprint(joined) {
		t.Fatal("ambiguous list boundary")
	}
}

// With ten-minute contributions, a large window has an expiry nearly every
// poll. Expiry must remove the affected group from each view at its boundary
// (including groups beyond the public twenty) without making the whole
// generation, and therefore every stream on the replica, unavailable.
func TestExpiryIsPerViewAndKeepsGenerationAvailable(t *testing.T) {
	data := candidateFixture(30)
	data.AsOf = time.Now().UTC()
	for i := range data.Items {
		data.Items[i].Item.ExpiresAt = data.AsOf.Add(time.Minute)
	}
	late := DefaultPreferences().Filters
	late.CategoryIDs = []string{"late"}
	data.Items[29].CategoryIDs = []string{"late"}
	data.Items[29].Item.ExpiresAt = data.AsOf.Add(400 * time.Millisecond)
	data.Items[0].Item.ExpiresAt = data.AsOf.Add(200 * time.Millisecond)
	x, err := buildIndex(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	boundary := data.Items[29].Item.ExpiresAt
	if view, deadline := x.selectView(late, "", boundary.Add(-time.Nanosecond)); len(view.Items) != 1 || !deadline.Equal(boundary) {
		t.Fatal("view deadline lost", view.Items, deadline)
	}
	if view, _ := x.selectView(late, "", boundary); len(view.Items) != 0 {
		t.Fatal("expired group selected at its boundary")
	}
	s := New(t.Context(), &candidateRepository{data: data}, 4)
	defer s.Close()
	read := func(f Filters) []string {
		t.Helper()
		view, err := s.FilteredSnapshot(t.Context(), f)
		if err != nil {
			t.Fatal("expiry interrupted the generation", err)
		}
		ids := []string{}
		for _, item := range view.Items {
			ids = append(ids, item.EventID)
		}
		return ids
	}
	everywhere := DefaultPreferences().Filters
	if ids := read(everywhere); len(ids) != 20 || ids[0] != data.Items[0].Item.EventID {
		t.Fatal(ids)
	}
	if ids := read(late); !slices.Equal(ids, []string{data.Items[29].Item.EventID}) {
		t.Fatal(ids)
	}
	time.Sleep(250 * time.Millisecond)
	// The cached top-20 view is reselected: the next group takes the slot.
	if ids := read(everywhere); len(ids) != 20 || slices.Contains(ids, data.Items[0].Item.EventID) || ids[19] != data.Items[20].Item.EventID {
		t.Fatal("expired top group retained or not replaced", ids)
	}
	if ids := read(late); len(ids) != 1 {
		t.Fatal("unexpired group beyond the public limit removed early", ids)
	}
	time.Sleep(200 * time.Millisecond)
	if ids := read(late); len(ids) != 0 {
		t.Fatal("group beyond the public limit outlived its expiry", ids)
	}
	if n := s.Stats().Operations["projection"].Count; n != 1 {
		t.Fatal("expiry required a projection", n)
	}
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
			got, _ := x.selectView(f, key, data.AsOf)
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
	for _, raw := range []string{`{"connections":{"mode":"all_events","sources":[]}}`, `{"category_ids":[],"category_ids":["Music"]}`, `{"location":{"mode":"radius"}}`, `{"location":{"mode":"city","city":"Berlin"}}`, `{"location":{"mode":"all","city":"Berlin"}}`, `{"category_ids":null}`, `{"Category_ids":[]}`, `{"artist_ids":["ticketmaster:a/b"]}`} {
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

func TestEntityCompositionReferenceAndCache(t *testing.T) {
	data := candidateFixture(300)
	for i := range data.Items {
		data.Items[i].ArtistIDs = []string{fmt.Sprintf("ticketmaster:A%d", i%7), "ticketmaster:Shared", "ticketmaster:Shared"}
		data.Items[i].VenueIDs = []string{fmt.Sprintf("ticketmaster:V%d", i%5), fmt.Sprintf("ticketmaster:V%d", i%3)}
	}
	r := &candidateRepository{data: data}
	s := New(t.Context(), r, 256)
	defer s.Close()
	for _, artists := range [][]string{{}, {"ticketmaster:A1", "ticketmaster:A2"}, {"ticketmaster:Shared"}, {"ticketmaster:shared"}} {
		for _, venues := range [][]string{{}, {"ticketmaster:V1", "ticketmaster:V2"}, {"ticketmaster:missing"}} {
			for _, categories := range [][]string{{}, {"1", "2"}} {
				for _, city := range []string{"", "1"} {
					f := DefaultPreferences().Filters
					f.ArtistIDs, f.VenueIDs, f.CategoryIDs = artists, venues, categories
					if city != "" {
						f.Location = Location{Mode: "city", City: city, Country: "DE"}
					}
					matches := func(want, facts []string) bool {
						return len(want) == 0 || slices.ContainsFunc(want, func(id string) bool { return slices.Contains(facts, id) })
					}
					want := []Item{}
					for _, c := range data.Items {
						if (city == "" || c.CityKey == city) && matches(artists, c.ArtistIDs) && matches(venues, c.VenueIDs) && matches(categories, c.CategoryIDs) {
							want = append(want, c.Item)
						}
						if len(want) == 20 {
							break
						}
					}
					got, err := s.FilteredSnapshot(t.Context(), f)
					if err != nil || !reflect.DeepEqual(got.Items, want) {
						t.Fatalf("filter %+v: %v, got %v want %v", f, err, got.Items, want)
					}
					f.ArtistIDs = append(append([]string{}, artists...), artists...)
					f.VenueIDs = append(append([]string{}, venues...), venues...)
					slices.Reverse(f.ArtistIDs)
					slices.Reverse(f.VenueIDs)
					again, err := s.FilteredSnapshot(t.Context(), f)
					if err != nil || &got.encoded[0] != &again.encoded[0] {
						t.Fatal("canonical view not reused", err)
					}
				}
			}
		}
	}
	if r.polls != 1 {
		t.Fatal("SQL scaled with filters", r.polls)
	}
}

func TestEntityPreferenceValidation(t *testing.T) {
	for _, kind := range []string{"artist_ids", "venue_ids"} {
		f, err := DecodePublicFilters([]byte(`{"` + kind + `":["ticketmaster:b","ticketmaster:A","ticketmaster:b"]}`))
		if err != nil {
			t.Fatal(err)
		}
		ids := f.ArtistIDs
		if kind == "venue_ids" {
			ids = f.VenueIDs
		}
		if !slices.Equal(ids, []string{"ticketmaster:A", "ticketmaster:b"}) {
			t.Fatal(ids)
		}
		for _, value := range []string{`null`, `["A"]`, `["ticketmaster:"]`, `["Ticketmaster:A"]`, `["ticketmaster:a b"]`, `["ticketmaster:a/b"]`, `["ticketmaster:` + strings.Repeat("x", 129) + `"]`, `[` + strings.Repeat(`"ticketmaster:a",`, 16) + `"ticketmaster:b"]`} {
			if _, err := DecodePublicFilters([]byte(`{"` + kind + `":` + value + `}`)); err == nil {
				t.Fatal("accepted", kind, value)
			}
		}
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
				x.selectView(f, "1", data.AsOf)
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
