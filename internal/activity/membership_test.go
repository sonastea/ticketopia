package activity

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
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

// slowMembership holds each batch briefly, as real SQL does, so concurrent
// cold owners queue behind the projection gate.
type slowMembership struct {
	*membershipFixture
	maxOwners int
}

func (r *slowMembership) Memberships(ctx context.Context, owners []MembershipOwner, candidates []MembershipCandidate) (map[string]Membership, error) {
	time.Sleep(5 * time.Millisecond)
	r.mu.Lock()
	r.maxOwners = max(r.maxOwners, len(owners))
	r.mu.Unlock()
	return r.membershipFixture.Memberships(ctx, owners, candidates)
}

func TestQueuedColdOwnersShareMembershipBatches(t *testing.T) {
	const owners = 48
	fixture := &membershipFixture{candidateRepository: candidateRepository{data: candidateFixture(1024)}, revisions: map[string]uint64{}, matches: map[string]map[string]uint8{}}
	for i := range owners {
		id := fmt.Sprint("owner-", i)
		fixture.revisions[id] = 1
		fixture.matches[id] = map[string]uint8{fmt.Sprint("ticketmaster:", i): 1}
	}
	r := &slowMembership{membershipFixture: fixture}
	s := New(t.Context(), r, 256)
	defer s.Close()
	f := DefaultPreferences().Filters
	f.Connections = Connections{"any", []string{"saved"}}
	var wg sync.WaitGroup
	for i := range owners {
		wg.Go(func() {
			view, err := s.OwnerSnapshot(t.Context(), fmt.Sprint("owner-", i), f)
			if err != nil || len(view.Items) != 1 || view.Items[0].EventID != fmt.Sprint("ticketmaster:", i) {
				t.Error("cold owner view", i, view.Items, err)
			}
		})
	}
	wg.Wait()
	r.mu.Lock()
	defer r.mu.Unlock()
	// Alone, each owner needs four 256-event batches (192 reads in total).
	if r.reads > owners*4/3 || r.maxOwners < 2 || r.maxOwners > 128 {
		t.Fatalf("queued cold owners were assembled separately: %d reads, at most %d owners per batch", r.reads, r.maxOwners)
	}
}

// flakyMembership fails one batch, as a refresh deadline or SQL error would.
type flakyMembership struct {
	*membershipFixture
	failAt, calls int
}

func (r *flakyMembership) Memberships(ctx context.Context, owners []MembershipOwner, candidates []MembershipCandidate) (map[string]Membership, error) {
	r.mu.Lock()
	r.calls++
	fail := r.calls == r.failAt
	r.mu.Unlock()
	if fail {
		return nil, ErrUnavailable
	}
	return r.membershipFixture.Memberships(ctx, owners, candidates)
}

func TestInterruptedRebuildResumesWithoutRepeatingBatches(t *testing.T) {
	fixture := &membershipFixture{candidateRepository: candidateRepository{data: candidateFixture(1024)}, revisions: map[string]uint64{"owner": 1}, matches: map[string]map[string]uint8{"owner": {"ticketmaster:1000": 1}}}
	r := &flakyMembership{membershipFixture: fixture, failAt: 3}
	s := New(t.Context(), r, 8)
	defer s.Close()
	f := DefaultPreferences().Filters
	f.Connections = Connections{"any", []string{"saved"}}
	if _, err := s.OwnerSnapshot(t.Context(), "owner", f); err == nil {
		t.Fatal("interrupted rebuild served a partial view")
	}
	started := time.Now()
	view, err := s.OwnerSnapshot(t.Context(), "owner", f)
	if err != nil || len(view.Items) != 1 || view.Items[0].EventID != "ticketmaster:1000" {
		t.Fatal("resumed view", view.Items, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Two batches completed before the failure; only the other two are re-read.
	if r.candidates != 1024 || time.Since(started) > time.Second {
		t.Fatalf("rebuild restarted or backed off: %d candidates read, resumed after %s", r.candidates, time.Since(started))
	}
}

// delayedMembership makes each batch slow enough that a rebuild overruns one
// refresh deadline without any SQL error.
type delayedMembership struct {
	*membershipFixture
	delay atomic.Int64
}

func (r *delayedMembership) Memberships(ctx context.Context, owners []MembershipOwner, candidates []MembershipCandidate) (map[string]Membership, error) {
	select {
	case <-time.After(time.Duration(r.delay.Load())):
	case <-ctx.Done():
		// As SQL does when the refresh deadline expires mid-read.
		return nil, ctx.Err()
	}
	return r.membershipFixture.Memberships(ctx, owners, candidates)
}

func TestRemoteRebuildHoldsLastCompleteView(t *testing.T) {
	fixture := &membershipFixture{candidateRepository: candidateRepository{data: candidateFixture(1024)}, revisions: map[string]uint64{"owner": 1}, matches: map[string]map[string]uint8{"owner": {"ticketmaster:10": 2}}}
	r := &delayedMembership{membershipFixture: fixture}
	s := New(t.Context(), r, 8)
	defer s.Close()
	f := DefaultPreferences().Filters
	f.Connections = Connections{"any", []string{"interested"}}
	if _, err := s.OwnerSnapshot(t.Context(), "owner", f); err != nil {
		t.Fatal(err)
	}
	updates, release, err := s.SubscribeOwner("owner", nil, f)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	// Another replica committed a change: only the revision reveals it, and the
	// four-batch rebuild needs more than one two-second refresh.
	fixture.mu.Lock()
	fixture.revisions["owner"] = 2
	fixture.matches["owner"] = map[string]uint8{"ticketmaster:20": 2}
	fixture.mu.Unlock()
	r.delay.Store(int64(1500 * time.Millisecond))
	deadline := time.After(9 * time.Second)
	for {
		select {
		case u := <-updates:
			if u.Unavailable {
				t.Fatal("a progressing remote rebuild closed the stream")
			}
			if len(u.Snapshot.Items) == 1 && u.Snapshot.Items[0].EventID == "ticketmaster:20" {
				// A held view is the previous complete one, never a mixture.
				return
			}
			if len(u.Snapshot.Items) != 1 || u.Snapshot.Items[0].EventID != "ticketmaster:10" {
				t.Fatal("held view mixed revisions", u.Snapshot.Items)
			}
		case <-deadline:
			t.Fatal("rebuilt view was not delivered")
		}
	}
}

// More changed accounts than one sweep's 64 work units can rebuild: accounts
// left for the next sweep keep their last complete view rather than backing off
// and closing, and every account converges.
func TestCappedRebuildHoldsQueuedAccounts(t *testing.T) {
	// Three owner chunks × 40 batches: the third gets none of one sweep's 64 units.
	const owners = 260
	fixture := &membershipFixture{candidateRepository: candidateRepository{data: candidateFixture(MaxCandidates)}, revisions: map[string]uint64{}, matches: map[string]map[string]uint8{}}
	for i := range owners {
		id := fmt.Sprint("owner-", i)
		fixture.revisions[id] = 1
		fixture.matches[id] = map[string]uint8{fmt.Sprint("ticketmaster:", i): 2}
	}
	s := New(t.Context(), fixture, owners)
	defer s.Close()
	f := DefaultPreferences().Filters
	f.Connections = Connections{"any", []string{"interested"}}
	channels := []<-chan Update{}
	for i := range owners {
		owner := fmt.Sprint("owner-", i)
		deadline := time.Now().Add(10 * time.Second)
		for {
			if _, err := s.OwnerSnapshot(t.Context(), owner, f); err == nil {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("cold owner never assembled")
			}
			time.Sleep(10 * time.Millisecond)
		}
		updates, release, err := s.SubscribeOwner(owner, nil, f)
		if err != nil {
			t.Fatal(err)
		}
		defer release()
		channels = append(channels, updates)
	}
	// Another replica changed every account: only revisions reveal it.
	fixture.mu.Lock()
	for i := range owners {
		id := fmt.Sprint("owner-", i)
		fixture.revisions[id] = 2
		fixture.matches[id] = map[string]uint8{fmt.Sprint("ticketmaster:", i+owners): 2}
	}
	fixture.mu.Unlock()
	converged := map[int]bool{}
	deadline := time.After(9500 * time.Millisecond)
	for len(converged) < owners {
		for i, updates := range channels {
			select {
			case u := <-updates:
				if u.Unavailable {
					t.Fatalf("account %d was closed while its rebuild was queued", i)
				}
				if len(u.Snapshot.Items) == 1 && u.Snapshot.Items[0].EventID == fmt.Sprint("ticketmaster:", i+owners) {
					converged[i] = true
				}
			default:
			}
		}
		select {
		case <-deadline:
			t.Fatalf("%d/%d accounts converged", len(converged), owners)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
