package activity

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

type membershipFixture struct {
	candidateRepository
	revisions         map[string]uint64
	matches           map[string]map[string]uint8
	reads, candidates int
	revisionReads     int
	fail              bool
}

func (r *membershipFixture) MembershipRevisions(_ context.Context, ids []string) (map[string]uint64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.revisionReads++
	if r.fail {
		return nil, ErrUnavailable
	}
	result := map[string]uint64{}
	for _, id := range ids {
		result[id] = r.revisions[id]
	}
	return result, nil
}
func (r *membershipFixture) Memberships(_ context.Context, owners []MembershipOwner, candidates []MembershipCandidate) (map[string]Membership, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	r.candidates += len(candidates)
	result := map[string]Membership{}
	for _, o := range owners {
		m := Membership{Revision: r.revisions[o.ID], Matches: map[string]uint8{}}
		for _, c := range candidates {
			m.Matches[c.ID] = r.matches[o.ID][c.ID] & o.Sources
		}
		result[o.ID] = m
	}
	return result, nil
}
func TestPrivateMembershipReuseAndIncrementalCandidates(t *testing.T) {
	r := &membershipFixture{candidateRepository: candidateRepository{data: candidateFixture(300)}, revisions: map[string]uint64{"owner": 1, "other": 1}, matches: map[string]map[string]uint8{"owner": {"ticketmaster:250": 1, "ticketmaster:251": 2}, "other": {}}}
	s := New(t.Context(), r, 256)
	defer s.Close()
	f := DefaultPreferences().Filters
	f.Connections = Connections{"any", []string{"interested", "saved"}}
	var wg sync.WaitGroup
	for range 30 {
		wg.Go(func() {
			view, err := s.OwnerSnapshot(t.Context(), "owner", f)
			if err != nil || len(view.Items) != 2 {
				t.Error("shared owner view", view, err)
			}
		})
	}
	wg.Wait()
	r.mu.Lock()
	reads := r.reads
	r.mu.Unlock()
	if reads != 2 {
		t.Fatal("duplicate tabs rebuilt membership", reads)
	}
	if _, err := s.FilteredSnapshot(t.Context(), f); err == nil {
		t.Fatal("public personalization")
	}
	view, err := s.OwnerSnapshot(t.Context(), "other", f)
	if err != nil || len(view.Items) != 0 {
		t.Fatal("owner leakage", view, err)
	}
	refresh := func() { s.projection <- struct{}{}; s.readAt = time.Time{}; s.memberAt = time.Time{}; <-s.projection }
	r.mu.Lock()
	reads = r.reads
	r.data.Items[0], r.data.Items[299] = r.data.Items[299], r.data.Items[0]
	r.data.Items[0].Item.EventTitle = "Corrected title"
	r.mu.Unlock()
	refresh()
	if _, err = s.OwnerSnapshot(t.Context(), "owner", f); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	if r.reads != reads {
		t.Error("reordering/attribution rebuilt membership")
	}
	r.data.Items[250].Revision = time.Now()
	r.candidates = 0
	r.mu.Unlock()
	refresh()
	if _, err = s.OwnerSnapshot(t.Context(), "owner", f); err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	if r.candidates != 1 {
		t.Error("metadata correction not incremental", r.candidates)
	}
	r.fail = true
	r.mu.Unlock()
	refresh()
	if _, err = s.OwnerSnapshot(t.Context(), "owner", f); err == nil {
		t.Fatal("failed membership served old view")
	}
	r.mu.Lock()
	revisionReads := r.revisionReads
	r.mu.Unlock()
	for range 30 {
		if _, err := s.OwnerSnapshot(t.Context(), "owner", f); err == nil {
			t.Fatal("failed membership view widened")
		}
	}
	r.mu.Lock()
	if r.revisionReads != revisionReads {
		t.Error("failed membership retried once per tab")
	}
	r.mu.Unlock()
	if _, err = s.Snapshot(t.Context()); err != nil {
		t.Fatal("private failure broke public view")
	}
	r.mu.Lock()
	r.fail = false
	r.revisions["owner"]++
	delete(r.matches["owner"], "ticketmaster:250")
	r.mu.Unlock()
	refresh()
	view, err = s.OwnerSnapshot(t.Context(), "owner", f)
	if err != nil || len(view.Items) != 1 || view.Items[0].EventID != "ticketmaster:251" {
		t.Fatal("removal", view, err)
	}
}

func TestConnectionCompositionReference(t *testing.T) {
	data := candidateFixture(300)
	x, err := buildIndex(data, nil)
	if err != nil {
		t.Fatal(err)
	}
	for mask := uint8(1); mask < 16; mask++ {
		f := DefaultPreferences().Filters
		f.Connections = Connections{Mode: "any", Sources: []string{}}
		for _, source := range []string{"saved", "interested", "followed_artists", "followed_venues"} {
			if mask&SourceBit(source) != 0 {
				f.Connections.Sources = append(f.Connections.Sources, source)
			}
		}
		f.CategoryIDs = []string{"2"}
		b := make(bitmap, (MaxCandidates+63)/64)
		want := []string{}
		for i, c := range data.Items {
			if uint8(i%16)&mask != 0 {
				b.add(x.slots[c.Item.EventID])
				if (i%3 == 2 || i%5 == 2) && len(want) < 20 {
					want = append(want, c.Item.EventID)
				}
			}
		}
		got := x.selectView(f, "", b)
		if len(got.Items) != len(want) {
			t.Fatal(mask, len(got.Items), len(want))
		}
		for i, item := range got.Items {
			if item.EventID != want[i] {
				t.Fatal(fmt.Sprint(mask), i, item.EventID, want[i])
			}
		}
	}
}
