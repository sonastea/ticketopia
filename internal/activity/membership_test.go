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
	refresh := func() {
		s.projection <- struct{}{}
		s.readAt = time.Time{}
		s.memberAt = time.Time{}
		for _, m := range s.members {
			m.retryAt = time.Time{}
		}
		<-s.projection
	}
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
		got, _ := x.selectView(f, "", data.AsOf, b)
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

func TestInactiveOwnersRebuildIndependentlyAfterInvalidation(t *testing.T) {
	r := &membershipFixture{candidateRepository: candidateRepository{data: candidateFixture(1)}, revisions: map[string]uint64{"a": 1, "b": 1}, matches: map[string]map[string]uint8{"a": {}, "b": {}}}
	s := New(t.Context(), r, 256)
	defer s.Close()
	f := DefaultPreferences().Filters
	f.Connections = Connections{Mode: "any", Sources: []string{"saved"}}
	for _, owner := range []string{"a", "b"} {
		if _, err := s.OwnerSnapshot(t.Context(), owner, f); err != nil {
			t.Fatal(err)
		}
	}
	// Local mutations close their old streams; both owners can consequently be
	// inactive when the first reconnect consumes the coalesced invalidations.
	r.mu.Lock()
	for _, owner := range []string{"a", "b"} {
		r.revisions[owner]++
		r.matches[owner]["ticketmaster:0"] = 1
	}
	r.mu.Unlock()
	s.mu.Lock()
	s.invalidMembers["a"], s.invalidMembers["b"] = true, true
	s.mu.Unlock()
	for _, owner := range []string{"a", "b"} {
		view, err := s.OwnerSnapshot(t.Context(), owner, f)
		if err != nil || len(view.Items) != 1 {
			t.Fatalf("owner %s could not rebuild after another owner's reconnect: %v", owner, err)
		}
	}
}

func TestPrivateCohortsStayBoundedAcrossMembershipChanges(t *testing.T) {
	r := &membershipFixture{candidateRepository: candidateRepository{data: candidateFixture(1)}, revisions: map[string]uint64{}, matches: map[string]map[string]uint8{}}
	owners := []string{}
	for i := range 130 {
		id := fmt.Sprintf("owner-%d", i)
		owners = append(owners, id)
		r.revisions[id] = 1
		r.matches[id] = map[string]uint8{}
	}
	s := New(t.Context(), r, 130)
	defer s.Close()
	f := DefaultPreferences().Filters
	f.Connections = Connections{Mode: "any", Sources: []string{"saved"}}
	for _, owner := range owners {
		if _, err := s.OwnerSnapshot(t.Context(), owner, f); err != nil {
			t.Fatal(err)
		}
		_, release, err := s.SubscribeOwner(owner, nil, f)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
	}
	for round := range 3 {
		r.mu.Lock()
		for _, owner := range owners {
			r.revisions[owner]++
			if round%2 == 0 {
				r.matches[owner]["ticketmaster:0"] = 1
			} else {
				delete(r.matches[owner], "ticketmaster:0")
			}
		}
		r.mu.Unlock()
		s.projection <- struct{}{}
		s.memberAt = time.Time{}
		<-s.projection
		for _, owner := range owners {
			if _, err := s.OwnerSnapshot(t.Context(), owner, f); err != nil {
				t.Fatal(err)
			}
		}
		s.projection <- struct{}{}
		cohorts := len(s.views)
		<-s.projection
		if cohorts > len(owners)+128 {
			t.Fatalf("old private revisions retained %d cohorts for %d active owners", cohorts, len(owners))
		}
	}
}

func TestWarmPrivateViewRequiresEveryRequestedSource(t *testing.T) {
	r := &membershipFixture{candidateRepository: candidateRepository{data: candidateFixture(2)}, revisions: map[string]uint64{"owner": 1}, matches: map[string]map[string]uint8{"owner": {"ticketmaster:0": 1, "ticketmaster:1": 2}}}
	s := New(t.Context(), r, 1)
	defer s.Close()
	f := DefaultPreferences().Filters
	f.Connections = Connections{Mode: "any", Sources: []string{"saved"}}
	if _, err := s.OwnerSnapshot(t.Context(), "owner", f); err != nil {
		t.Fatal(err)
	}
	f.Connections.Sources = []string{"interested"}
	ch, release, err := s.SubscribeOwner("owner", nil, f)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	select {
	case <-ch:
		t.Fatal("unloaded source published as an empty warm view")
	default:
	}
	select {
	case u := <-ch:
		if u.Unavailable || len(u.Snapshot.Items) != 1 || u.Snapshot.Items[0].EventID != "ticketmaster:1" {
			t.Fatal("requested membership was not loaded")
		}
	case <-time.After(4 * time.Second):
		t.Fatal("membership view did not rebuild")
	}
}
