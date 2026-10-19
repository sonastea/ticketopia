package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// SourceBit is a compiled allowlist, never an SQL expression supplied by a caller.
func SourceBit(source string) uint8 {
	switch source {
	case "saved":
		return 1
	case "interested":
		return 2
	case "followed_artists":
		return 4
	case "followed_venues":
		return 8
	}
	return 0
}

type MembershipOwner struct {
	ID      string
	Sources uint8
}
type MembershipCandidate struct {
	ID       string
	Revision time.Time
}
type Membership struct {
	Revision uint64
	Matches  map[string]uint8
}
type MembershipRepository interface {
	MembershipRevisions(context.Context, []string) (map[string]uint64, error)
	Memberships(context.Context, []MembershipOwner, []MembershipCandidate) (map[string]Membership, error)
}

type memberState struct {
	revision   uint64
	sources    uint8
	bits       [4]bitmap
	dirty      bitmap
	valid      bool
	used       time.Time
	generation uint64
}

func sourceMask(f Filters) uint8 {
	var mask uint8
	for _, source := range f.Connections.Sources {
		mask |= SourceBit(source)
	}
	return mask
}
func (m *memberState) union(f Filters) bitmap {
	result := make(bitmap, (MaxCandidates+63)/64)
	mask := sourceMask(f)
	for i, b := range m.bits {
		if mask&(1<<i) != 0 {
			for j, v := range b {
				result[j] |= v
			}
		}
	}
	return result
}
func (m *memberState) key(owner string, f Filters) string {
	sources, _ := json.Marshal(f.Connections)
	return fmt.Sprintf("\x00%s/%d/%d/%s", owner, m.revision, m.generation, sources)
}
func (s *Service) membershipBytes() int { return len(s.members)*8192 + len(s.memberFacts)*512 }

// Called under the projection gate: one revision sweep for the union of active
// owners, reusable across tabs, followed by bounded incremental candidate joins.
func (s *Service) refreshMembership(ctx context.Context, owner string, f Filters) error {
	repo, ok := s.repo.(MembershipRepository)
	if !ok {
		return ErrUnavailable
	}
	now := time.Now()
	periodic := now.Sub(s.memberAt) >= 2*time.Second
	wanted := map[string]uint8{owner: sourceMask(f)}
	s.mu.Lock()
	for sub := range s.subs {
		if sub.filter.Connections.Mode == "any" {
			wanted[sub.owner] |= sourceMask(sub.filter)
		}
	}
	s.mu.Unlock()
	for id, m := range s.members {
		if wanted[id] == 0 && now.Sub(m.used) >= 30*time.Second {
			delete(s.members, id)
		}
	}
	changed := false
	s.mu.Lock()
	if s.invalidAll {
		for _, m := range s.members {
			m.revision = 0
			m.valid = false
		}
		s.invalidAll = false
		changed = true
	}
	for id := range s.invalidMembers {
		if m := s.members[id]; m != nil {
			m.revision = 0
			m.valid = false
			changed = true
		}
		delete(s.invalidMembers, id)
	}
	s.mu.Unlock()
	for id := range s.memberFacts {
		slot, exists := s.index.slots[id]
		if !exists || slot != s.memberSlots[id] {
			for _, m := range s.members {
				for _, b := range m.bits {
					if b != nil {
						b[s.memberSlots[id]/64] &^= 1 << (s.memberSlots[id] % 64)
					}
				}
			}
			delete(s.memberFacts, id)
			delete(s.memberSlots, id)
			changed = true
		}
	}
	for _, c := range s.index.data.Items {
		old, exists := s.memberFacts[c.Item.EventID]
		if !exists || !old.Revision.Equal(c.Revision) {
			slot := s.index.slots[c.Item.EventID]
			for _, m := range s.members {
				m.dirty.add(slot)
				m.valid = false
			}
			s.memberFacts[c.Item.EventID] = MembershipCandidate{c.Item.EventID, c.Revision}
			s.memberSlots[c.Item.EventID] = slot
			changed = true
		}
	}
	for id, mask := range wanted {
		m := s.members[id]
		if m == nil {
			if len(s.members) >= 384 || s.index.bytes*2+s.viewBytes+s.membershipBytes()+8192+(8<<20) > MemoryBudget {
				return ErrUnavailable
			}
			m = &memberState{dirty: make(bitmap, (MaxCandidates+63)/64)}
			s.members[id] = m
			changed = true
		}
		m.used = now
		if mask&^m.sources != 0 {
			for i := range m.bits {
				if mask&(1<<i) != 0 && m.bits[i] == nil {
					m.bits[i] = make(bitmap, (MaxCandidates+63)/64)
				}
			}
			m.sources |= mask
			m.valid = false
			m.revision = 0
			changed = true
		}
		// Retain only source bitmaps required by this account's current views.
		for i := range m.bits {
			if mask&(1<<i) == 0 {
				m.bits[i] = nil
			}
		}
		m.sources = mask
	}
	if !changed && !periodic {
		if s.members[owner].valid {
			return nil
		}
		return ErrUnavailable
	}
	if periodic {
		// Failed or slow rebuilds also get a bounded retry delay. Starting the
		// interval before SQL would let every waiting tab retry a timed-out sweep.
		defer func() { s.memberAt = time.Now() }()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	ids := []string{}
	for id := range wanted {
		if periodic || !s.members[id].valid {
			ids = append(ids, id)
		}
	}
	jobs := 0
	for start := 0; start < len(ids); start += 128 {
		chunk := ids[start:min(start+128, len(ids))]
		if !s.acquireJob(ctx) {
			for _, id := range ids[start:] {
				s.members[id].valid = false
			}
			return ErrUnavailable
		}
		revisions, err := repo.MembershipRevisions(ctx, chunk)
		<-s.jobs
		if err != nil {
			for _, id := range chunk {
				s.members[id].valid = false
			}
			continue
		}
		work := []MembershipOwner{}
		for _, id := range chunk {
			m := s.members[id]
			if revisions[id] == 0 {
				m.valid = false
				continue
			}
			if m.revision != revisions[id] {
				m.valid = false
				for _, c := range s.index.data.Items {
					m.dirty.add(s.index.slots[c.Item.EventID])
				}
			}
			if !m.valid {
				work = append(work, MembershipOwner{id, m.sources})
			}
		}
		if len(work) == 0 {
			continue
		}
		candidates := []MembershipCandidate{}
		for _, c := range s.index.data.Items {
			for _, o := range work {
				if s.members[o.ID].dirty.has(s.index.slots[c.Item.EventID]) {
					candidates = append(candidates, MembershipCandidate{c.Item.EventID, c.Revision})
					break
				}
			}
		}
		failed := false
		for pos := 0; pos < len(candidates); pos += 256 {
			jobs++
			if jobs > 64 || !s.acquireJob(ctx) {
				failed = true
				break
			}
			batch := candidates[pos:min(pos+256, len(candidates))]
			matches, err := repo.Memberships(ctx, work, batch)
			<-s.jobs
			if err != nil {
				failed = true
				break
			}
			for _, o := range work {
				m := s.members[o.ID]
				match, ok := matches[o.ID]
				if !ok || match.Revision != revisions[o.ID] {
					failed = true
					break
				}
				for _, c := range batch {
					slot := s.index.slots[c.ID]
					for i, b := range m.bits {
						if b != nil {
							b[slot/64] &^= 1 << (slot % 64)
							if match.Matches[c.ID]&(1<<i) != 0 {
								b.add(slot)
							}
						}
					}
				}
			}
			if failed {
				break
			}
		}
		// Recheck after multi-batch assembly. A concurrent writer invalidates rather
		// than combining membership generations in a published private view.
		if !failed && s.acquireJob(ctx) {
			final, err := repo.MembershipRevisions(ctx, chunk)
			<-s.jobs
			for _, o := range work {
				m := s.members[o.ID]
				if err == nil && final[o.ID] == revisions[o.ID] {
					m.valid = true
					m.revision = final[o.ID]
					clear(m.dirty)
					m.generation++
				}
			}
		}
	}
	if !s.members[owner].valid {
		return ErrUnavailable
	}
	return nil
}
