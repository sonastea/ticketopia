package activity

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
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

// rebuildHold bounds how long a replica keeps an account's last complete view
// while rebuilding it for a newer revision.
const rebuildHold = 10 * time.Second

// errRebuilding is unavailable to new reads; active streams hold their view.
var errRebuilding = fmt.Errorf("%w: rebuilding", ErrUnavailable)

// waiter is a personal request admitted to pending work but not yet holding the
// projection gate.
type waiter struct {
	owner string
	mask  uint8
}

type memberState struct {
	revision uint64
	// partial is the account revision whose rebuild is in progress: batches
	// already applied for it stay done (their dirty bits are cleared), so a
	// rebuild larger than one refresh deadline resumes rather than restarting.
	partial uint64
	// rebuilding is when a newer revision's rebuild began for an account that
	// already had a complete view; stalled marks an SQL error since. Within
	// rebuildHold, unstalled streams keep their last complete view (as during
	// the revision poll) instead of closing.
	rebuilding time.Time
	stalled    bool
	sources    uint8
	bits       [4]bitmap
	dirty      bitmap
	valid      bool
	used       time.Time
	retryAt    time.Time
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
	// Requests queued behind this one join the same batched assembly instead of
	// each repeating ceil(window/256) candidate joins for a single owner.
	for w := range s.waiting {
		wanted[w.owner] |= w.mask
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
			m.revision, m.partial = 0, 0
			m.valid = false
			m.retryAt = time.Time{}
		}
		s.invalidAll = false
		changed = true
	}
	for id := range s.invalidMembers {
		if m := s.members[id]; m != nil {
			m.revision, m.partial = 0, 0
			m.valid = false
			m.retryAt = time.Time{}
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
				m.retryAt = time.Time{}
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
				if id != owner {
					// A queued owner that does not fit retries on its own turn.
					delete(wanted, id)
					continue
				}
				s.metrics.memoryLimit.Add(1)
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
			m.revision, m.partial = 0, 0
			m.retryAt = time.Time{}
			changed = true
		}
		// Retain only source bitmaps required by this account's current views.
		for i := range m.bits {
			if mask&(1<<i) == 0 {
				m.bits[i] = nil
			}
		}
		m.sources = mask
		// A different owner's reconnect can consume a coalesced invalidation
		// while this owner has no active stream. Incomplete membership still
		// needs assembly on re-entry; only an actual failed attempt backs off.
		if !m.valid && !now.Before(m.retryAt) {
			changed = true
		}
	}
	if !changed && !periodic {
		return s.memberResult(s.members[owner])
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
		m := s.members[id]
		if (periodic || !m.valid) && (m.valid || !now.Before(m.retryAt)) {
			ids = append(ids, id)
		}
	}
	progressed := map[string]bool{}
	defer func() {
		// Start backoff after assembly, so timeouts cannot cause every queued
		// tab to immediately repeat the same failed read. An attempt that
		// completed batches resumes without backoff; each attempt progresses.
		retryAt := time.Now().Add(2 * time.Second)
		for _, id := range ids {
			// Only SQL errors back off: capped or time-boxed work resumes.
			if m := s.members[id]; !m.valid && !progressed[id] && m.stalled {
				m.retryAt = retryAt
			}
		}
	}()
	jobs := 0
	for start := 0; start < len(ids); start += 128 {
		chunk := ids[start:min(start+128, len(ids))]
		if !s.acquireJob(ctx) {
			for _, id := range ids[start:] {
				s.members[id].valid = false
			}
			return ErrUnavailable
		}
		started := time.Now()
		revisions, err := repo.MembershipRevisions(ctx, chunk)
		s.metrics.membership.observe(started)
		<-s.jobs
		if err != nil {
			for _, id := range chunk {
				s.members[id].valid = false
				// The refresh's own deadline time-boxes work; only errors stall.
				s.members[id].stalled = ctx.Err() == nil
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
			if m.revision != revisions[id] && m.partial != revisions[id] {
				m.valid = false
				m.partial = revisions[id]
				if m.generation > 0 && m.rebuilding.IsZero() {
					m.rebuilding = now
				}
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
		// Read up to two batches at once (the existing SQL job limit), then apply
		// them in order; each is checked against the chunk's revisions.
		for pos := 0; pos < len(candidates) && !failed; pos += 512 {
			batches := [][]MembershipCandidate{}
			for start := pos; start < min(pos+512, len(candidates)); start += 256 {
				jobs++
				if jobs > 64 || !s.acquireJob(ctx) {
					s.metrics.queueLimit.Add(1)
					failed = true
					break
				}
				batches = append(batches, candidates[start:min(start+256, len(candidates))])
			}
			results := make([]map[string]Membership, len(batches))
			errs := make([]error, len(batches))
			var wg sync.WaitGroup
			for i, batch := range batches {
				wg.Go(func() {
					defer func() { <-s.jobs }()
					started := time.Now()
					results[i], errs[i] = repo.Memberships(ctx, work, batch)
					s.metrics.membership.observe(started)
				})
			}
			wg.Wait()
			for i, batch := range batches {
				// Batches are independent; a failed one does not void its sibling.
				if errs[i] != nil {
					failed = true
					for _, o := range work {
						s.members[o.ID].stalled = s.members[o.ID].stalled || ctx.Err() == nil
					}
					continue
				}
				for _, o := range work {
					m := s.members[o.ID]
					match, ok := results[i][o.ID]
					if !ok || match.Revision != revisions[o.ID] {
						// The account changed mid-rebuild: its progress is void.
						m.partial = 0
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
						m.dirty[slot/64] &^= 1 << (slot % 64)
					}
					progressed[o.ID] = true
					m.stalled = false
				}
			}
		}
		// Recheck after multi-batch assembly. A concurrent writer invalidates rather
		// than combining membership generations in a published private view.
		if !failed && s.acquireJob(ctx) {
			started := time.Now()
			final, err := repo.MembershipRevisions(ctx, chunk)
			s.metrics.membership.observe(started)
			<-s.jobs
			for _, o := range work {
				m := s.members[o.ID]
				if err == nil && final[o.ID] != revisions[o.ID] {
					m.partial = 0
				}
				if err == nil && final[o.ID] == revisions[o.ID] {
					m.valid = true
					m.rebuilding, m.stalled = time.Time{}, false
					m.retryAt = time.Time{}
					m.revision = final[o.ID]
					clear(m.dirty)
					m.generation++
				}
			}
		}
	}
	return s.memberResult(s.members[owner])
}

// memberResult reports a complete membership, a bounded hold while a newer
// revision's rebuild progresses for an account that had a complete view, or
// unavailability.
func (s *Service) memberResult(m *memberState) error {
	switch {
	case m.valid:
		return nil
	case m.partial != 0 && !m.stalled && !m.rebuilding.IsZero() && time.Since(m.rebuilding) < rebuildHold:
		return errRebuilding
	}
	return ErrUnavailable
}
