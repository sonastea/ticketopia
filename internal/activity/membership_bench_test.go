package activity

import (
	"fmt"
	"testing"
	"time"
)

// Isolate the already-built, already-checked generation: no SQL, background
// worker, generation refresh, expiry or membership revision poll is timed.
// Each benchmark operation is one tick's reads for all tabs of one owner.
func BenchmarkWarmPrivateFanout(b *testing.B) {
	for _, n := range []int{100, 1000, 10000} {
		for _, tabs := range []int{1, 256} {
			for _, direct := range []bool{false, true} {
				path := "OwnerSnapshot"
				if direct {
					path = "cachedSelectionOnly"
				}
				b.Run(fmt.Sprintf("groups=%d/tabs=%d/%s", n, tabs, path), func(b *testing.B) {
					x, err := buildIndex(candidateFixture(n), nil)
					if err != nil {
						b.Fatal(err)
					}
					f := DefaultPreferences().Filters
					f.Connections = Connections{Mode: "any", Sources: []string{"saved"}}
					m := &memberState{sources: 1, revision: 1, generation: 1, dirty: make(bitmap, (MaxCandidates+63)/64)}
					m.bits[0] = make(bitmap, (MaxCandidates+63)/64)
					m.bits[0].add(x.slots[x.data.Items[0].Item.EventID])
					s := &Service{
						repo: &membershipFixture{}, index: x,
						projection: make(chan struct{}, 1), jobs: make(chan struct{}, 2), pending: make(chan struct{}, 64), done: make(chan struct{}),
						members: map[string]*memberState{"owner": m}, memberFacts: map[string]MembershipCandidate{}, memberSlots: map[string]int{},
						invalidMembers: map[string]bool{}, views: map[string]cachedView{}, keys: map[Location]string{},
						subs: map[*subscriber]struct{}{}, private: map[string]map[*subscriber]struct{}{"owner": {}}, waiting: map[*waiter]struct{}{},
					}
					s.reconcileMembers()
					m.valid = true
					for range tabs {
						sub := &subscriber{owner: "owner", filter: f}
						s.subs[sub] = struct{}{}
						s.private["owner"][sub] = struct{}{}
					}
					s.readAt, s.memberAt = time.Now(), time.Now()
					view, err := s.OwnerSnapshot(b.Context(), "owner", f)
					if err != nil || len(view.Items) != 1 {
						b.Fatalf("warm private snapshot: items=%d err=%v", len(view.Items), err)
					}
					b.ReportAllocs()
					for b.Loop() {
						// Keep the test on the warm path even in long profiling runs.
						s.readAt, s.memberAt = time.Now(), time.Now()
						for range tabs {
							if direct {
								_, err = s.selectOwnerCached(b.Context(), f, "owner")
							} else {
								_, err = s.OwnerSnapshot(b.Context(), "owner", f)
							}
							if err != nil {
								b.Fatal(err)
							}
						}
					}
				})
			}
		}
	}
}
