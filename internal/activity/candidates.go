package activity

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"time"
)

const MaxCandidates = 10000
const MemoryBudget = 64 << 20
const MaxFactBytes = 64 << 10

var ErrCandidateLimit = fmt.Errorf("%w: candidate_limit", ErrUnavailable)
var ErrMemoryLimit = fmt.Errorf("%w: memory_limit", ErrUnavailable)

// Candidate is internal: matching facts must never be serialized in actor DTOs.
type Candidate struct {
	Item        Item
	CityKey     string
	CategoryIDs []string
	ArtistIDs   []string
	VenueIDs    []string
	Revision    time.Time
}
type Candidates struct {
	AsOf  time.Time
	Items []Candidate
}
type CandidateRepository interface {
	Candidates(context.Context) (Candidates, error)
	LocationKey(context.Context, Location) (string, error)
}
type bitmap []uint64

func (b bitmap) add(slot int)      { b[slot/64] |= 1 << (slot % 64) }
func (b bitmap) has(slot int) bool { return b[slot/64]&(1<<(slot%64)) != 0 }

type candidateIndex struct {
	data        Candidates
	slots       map[string]int
	cities      map[string]bitmap
	categories  map[string]bitmap
	artists     map[string]bitmap
	venues      map[string]bitmap
	bytes       int
	fingerprint [sha256.Size]byte
}

func buildIndex(data Candidates, previous *candidateIndex) (*candidateIndex, error) {
	if len(data.Items) > MaxCandidates {
		return nil, ErrCandidateLimit
	}
	x := &candidateIndex{data: data, slots: map[string]int{}, cities: map[string]bitmap{}, categories: map[string]bitmap{}, artists: map[string]bitmap{}, venues: map[string]bitmap{}}
	used := make([]bool, MaxCandidates)
	for _, c := range data.Items {
		if previous != nil {
			if slot, ok := previous.slots[c.Item.EventID]; ok {
				x.slots[c.Item.EventID] = slot
				used[slot] = true
			}
		}
	}
	next := 0
	for _, c := range data.Items {
		if _, ok := x.slots[c.Item.EventID]; !ok {
			for used[next] {
				next++
			}
			x.slots[c.Item.EventID] = next
			used[next] = true
		}
	}
	// New bitmaps clear every removed slot before it can be reused.
	add := func(sets map[string]bitmap, key string, slot int) {
		if sets[key] == nil {
			sets[key] = make(bitmap, (MaxCandidates+63)/64)
			x.bytes += 1600 + len(key)*2
		}
		sets[key].add(slot)
	}
	for _, c := range data.Items {
		if len(c.Item.EventTitle)+len(c.Item.City)+len(c.CityKey) > MaxFactBytes {
			return nil, ErrMemoryLimit
		}
		x.bytes += 1024 + 4*(len(c.Item.EventID)+len(c.Item.EventTitle)+len(c.Item.City)+len(c.CityKey))
		if c.Item.ActorFirstName != nil {
			x.bytes += 4 * len(*c.Item.ActorFirstName)
		}
		slot := x.slots[c.Item.EventID]
		if c.CityKey != "" {
			add(x.cities, c.CityKey, slot)
		}
		for _, dimension := range []struct {
			ids  []string
			sets map[string]bitmap
		}{{c.CategoryIDs, x.categories}, {c.ArtistIDs, x.artists}, {c.VenueIDs, x.venues}} {
			for _, id := range dimension.ids {
				add(dimension.sets, id, slot)
				x.bytes += 128 + 4*len(id)
				if x.bytes > MemoryBudget/2-(2<<20) {
					return nil, ErrMemoryLimit
				}
			}
		}
		// Reserve half for old/in-progress data and encoded views.
		if x.bytes > MemoryBudget/2-(2<<20) {
			return nil, ErrMemoryLimit
		}
	}
	// Hash one bounded candidate at a time: retaining or marshaling the entire
	// generation would duplicate name-bearing output and spike assembly memory.
	h := sha256.New()
	encoder := json.NewEncoder(h)
	for _, c := range data.Items {
		if err := encoder.Encode(c); err != nil {
			return nil, ErrUnavailable
		}
	}
	copy(x.fingerprint[:], h.Sum(nil))
	return x, nil
}
func (x *candidateIndex) selectView(f Filters, cityKey string, memberships ...bitmap) Snapshot {
	result := Snapshot{AsOf: x.data.AsOf, Items: []Item{}}
	union := func(ids []string, sets map[string]bitmap) bitmap {
		if len(ids) == 0 {
			return nil
		}
		result := make(bitmap, (MaxCandidates+63)/64)
		for _, id := range ids {
			for i, word := range sets[id] {
				result[i] |= word
			}
		}
		return result
	}
	categories := union(f.CategoryIDs, x.categories)
	artists := union(f.ArtistIDs, x.artists)
	venues := union(f.VenueIDs, x.venues)
	city := x.cities[cityKey]
	for _, c := range x.data.Items {
		slot := x.slots[c.Item.EventID]
		if len(memberships) > 0 && memberships[0] != nil && !memberships[0].has(slot) {
			continue
		}
		if f.Location.Mode == "city" && (city == nil || !city.has(slot)) {
			continue
		}
		if categories != nil && !categories.has(slot) {
			continue
		}
		if artists != nil && !artists.has(slot) || venues != nil && !venues.has(slot) {
			continue
		}
		result.Items = append(result.Items, c.Item)
		if len(result.Items) == 20 {
			break
		}
	}
	return result
}
