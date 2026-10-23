//go:build activityablation

package ticketopia

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/activity"
)

// Replay must keep each recorded generation's shape relative to its read time
// (age, remaining lifetime, revisions) and cycle in recorded order; counts
// replay the last recorded values.
func TestAblationReplay(t *testing.T) {
	recordedAt := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	revision := recordedAt.Add(-time.Hour)
	generation := func(id string) activity.Candidates {
		return activity.Candidates{AsOf: recordedAt, Items: []activity.Candidate{{
			Item:     activity.Item{EventID: id, LatestRecommendedAt: recordedAt.Add(-time.Minute), ExpiresAt: recordedAt.Add(9 * time.Minute)},
			Revision: revision,
		}}}
	}
	a := &ablationRepository{counts: map[string]activity.Count{"e1": {EventID: "e1", RecommendationCount: 3, InterestedCount: 4}}}
	a.generations = []activity.Candidates{generation("g1"), generation("g2")}
	a.replayProjection.Store(true)
	a.replayCounts.Store(true)
	for i, want := range []string{"g1", "g2", "g1"} {
		got, err := a.RefreshCandidates(t.Context(), activity.Candidates{})
		if err != nil {
			t.Fatal(err)
		}
		c := got.Items[0]
		if c.Item.EventID != want || got.AsOf.Sub(c.Item.LatestRecommendedAt) != time.Minute || c.Item.ExpiresAt.Sub(got.AsOf) != 9*time.Minute ||
			!c.Revision.Equal(revision) || time.Since(got.AsOf) > time.Minute {
			t.Fatalf("read %d: %+v at %s", i, c, got.AsOf)
		}
	}
	if !a.generations[0].Items[0].Item.ExpiresAt.Equal(recordedAt.Add(9 * time.Minute)) {
		t.Fatal("replay mutated the recorded generation")
	}
	counts, err := a.Counts(t.Context(), []string{"e1", "e2"})
	if err != nil || len(counts.Items) != 2 || counts.Items[0].InterestedCount != 4 || counts.Items[1] != (activity.Count{EventID: "e2"}) {
		t.Fatal(counts, err)
	}
}

func TestAblationModeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mode")
	a := &ablationRepository{counts: map[string]activity.Count{}}
	go a.watch(t.Context(), path)
	for _, step := range []struct {
		content            string
		projection, counts bool
	}{{"1 no-projection\n", true, false}, {"2 no-counts\n", false, true}, {"3 replayed\n", true, true}, {"4 bogus\n", true, true}, {"5 full\n", false, false}} {
		if err := os.WriteFile(path, []byte(step.content), 0644); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for a.replayProjection.Load() != step.projection || a.replayCounts.Load() != step.counts {
			if time.Now().After(deadline) {
				t.Fatalf("%q not applied", step.content)
			}
			time.Sleep(20 * time.Millisecond)
		}
		// Past two polls: the state holds (an unknown mode keeps the previous one).
		time.Sleep(450 * time.Millisecond)
		if a.replayProjection.Load() != step.projection || a.replayCounts.Load() != step.counts {
			t.Fatalf("%q did not hold", step.content)
		}
	}
}
