package activity

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
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

// CandidateRefreshRepository reuses only immutable public facets from the last
// successful generation. Eligibility, attribution and revisions are always read
// again; a failed poll discards the prior generation before the next attempt.
type CandidateRefreshRepository interface {
	RefreshCandidates(context.Context, Candidates) (Candidates, error)
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
	x := &candidateIndex{data: data, slots: make(map[string]int, len(data.Items)), cities: map[string]bitmap{}, categories: map[string]bitmap{}, artists: map[string]bitmap{}, venues: map[string]bitmap{}}
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
	// New bitmaps clear every removed slot before it can be reused. A distinct
	// key is charged once with its bitmap; repeated values are interned.
	add := func(sets map[string]bitmap, key string, slot int) {
		if sets[key] == nil {
			sets[key] = make(bitmap, (MaxCandidates+63)/64)
			x.bytes += bitmapBytes + keyOverhead + stringBytes(key)
		}
		sets[key].add(slot)
	}
	cities := map[string]bool{}
	for _, c := range data.Items {
		if len(c.Item.EventTitle)+len(c.Item.City)+len(c.CityKey) > MaxFactBytes {
			return nil, ErrMemoryLimit
		}
		x.bytes += CandidateBytes(c)
		if !cities[c.Item.City] {
			cities[c.Item.City] = true
			x.bytes += keyOverhead + stringBytes(c.Item.City)
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
				// One oversized lineup cannot allocate bitmaps past the budget.
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
	x.fingerprint = fingerprint(data.Items)
	return x, nil
}

// fingerprint hashes every candidate fact through a small reused buffer,
// never retaining the name-bearing generation. Reflection-based JSON encoding
// dominated 10,000-candidate builds. Length prefixes keep field and list
// boundaries unambiguous. Add new Candidate/Item fields here; the test
// TestFingerprintCoversEveryCandidateField fails until they are hashed.
func fingerprint(items []Candidate) [sha256.Size]byte {
	h := sha256.New()
	buf := make([]byte, 0, 4096)
	number := func(v int64) { buf = binary.LittleEndian.AppendUint64(buf, uint64(v)) }
	text := func(s string) {
		number(int64(len(s)))
		buf = append(buf, s...)
	}
	instant := func(t time.Time) { number(t.Unix()); number(int64(t.Nanosecond())) }
	for _, c := range items {
		item := c.Item
		text(item.EventID)
		text(item.EventTitle)
		text(item.City)
		if item.ActorFirstName == nil {
			number(-1)
		} else {
			text(*item.ActorFirstName)
		}
		number(int64(item.RecentRecommendations))
		instant(item.LatestRecommendedAt)
		instant(item.ExpiresAt)
		text(c.CityKey)
		for _, ids := range [][]string{c.CategoryIDs, c.ArtistIDs, c.VenueIDs} {
			number(int64(len(ids)))
			for _, id := range ids {
				text(id)
			}
		}
		instant(c.Revision)
		if len(buf) >= 4096 {
			h.Write(buf)
			buf = buf[:0]
		}
	}
	h.Write(buf)
	var sum [sha256.Size]byte
	h.Sum(sum[:0])
	return sum
}

// selectView excludes every group whose earliest contribution has expired by
// now (conservative server time), so a partially expired group's count, time
// or name is never shown before the next projection recomputes it. It also
// returns the earliest expiry among selected groups: the view's own deadline.
func (x *candidateIndex) selectView(f Filters, cityKey string, now time.Time, memberships ...bitmap) (Snapshot, time.Time) {
	result := Snapshot{AsOf: x.data.AsOf, Items: []Item{}}
	var expiresAt time.Time
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
		expiry := c.Item.ExpiresAt
		if !expiry.IsZero() && !expiry.After(now) {
			continue
		}
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
		if !expiry.IsZero() && (expiresAt.IsZero() || expiry.Before(expiresAt)) {
			expiresAt = expiry
		}
		if len(result.Items) == 20 {
			break
		}
	}
	return result, expiresAt
}
