//go:build activityablation

// Diagnostic builds only (go build -tags activityablation): the soak's ablation
// phase removes live-activity SQL from full load while keeping its downstream
// work. TICKETOPIA_ACTIVITY_ABLATION_FILE names a file holding "<seq> <mode>";
// it is polled every 200 ms and each newly applied sequence is logged. Modes:
//
//   - full: every read reaches MariaDB and is recorded for replay.
//   - no-projection: candidate generations replay recorded ones.
//   - no-counts: count reads replay recorded counts.
//   - replayed: both.
//
// Replay keeps generation turnover, selection, encoding, frames and count
// delivery. Membership, cleanup, location keys and request-path SQL (credentials,
// preferences) always reach MariaDB. Release images contain none of this.
package ticketopia

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/persistence"
)

func init() {
	activityRepository = func(ctx context.Context, r *persistence.ActivityRepository) activity.Repository {
		path := os.Getenv("TICKETOPIA_ACTIVITY_ABLATION_FILE")
		if path == "" {
			return r
		}
		a := &ablationRepository{ActivityRepository: r, counts: map[string]activity.Count{}}
		go a.watch(ctx, path)
		return a
	}
}

// ablationGenerations bounds recorded generations: about a minute at the
// service's two-second refresh.
const ablationGenerations = 30

type ablationRepository struct {
	*persistence.ActivityRepository
	replayProjection, replayCounts atomic.Bool
	mu                             sync.Mutex
	generations                    []activity.Candidates
	next                           int
	counts                         map[string]activity.Count
}

func (a *ablationRepository) watch(ctx context.Context, path string) {
	applied := -1
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if data, err := os.ReadFile(path); err == nil {
			fields := strings.Fields(string(data))
			seq := -1
			if len(fields) == 2 {
				seq, _ = strconv.Atoi(fields[0])
			}
			if seq >= 0 && seq != applied {
				projection, counts, ok := ablationMode(fields[1])
				if !ok {
					fmt.Fprintf(os.Stderr, "{\"level\":\"error\",\"component\":\"activity-ablation\",\"seq\":%d,\"message\":\"Unknown activity ablation mode\"}\n", seq)
				} else {
					a.replayProjection.Store(projection)
					a.replayCounts.Store(counts)
				}
				line, _ := json.Marshal(map[string]any{"level": "info", "component": "activity-ablation", "seq": seq, "mode": fields[1], "applied": ok, "message": "Activity ablation mode"})
				fmt.Fprintln(os.Stderr, string(line))
				applied = seq
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func ablationMode(mode string) (projection, counts, ok bool) {
	switch mode {
	case "full":
		return false, false, true
	case "no-projection":
		return true, false, true
	case "no-counts":
		return false, true, true
	case "replayed":
		return true, true, true
	}
	return false, false, false
}

func (a *ablationRepository) Candidates(ctx context.Context) (activity.Candidates, error) {
	return a.RefreshCandidates(ctx, activity.Candidates{})
}

// RefreshCandidates replays recorded generations in order, cycling. Each is
// shifted so its AsOf is the read time, preserving every group's age and
// remaining lifetime as recorded. Revisions are not shifted: membership and
// facet reuse see the recorded metadata revisions, as in full load.
func (a *ablationRepository) RefreshCandidates(ctx context.Context, previous activity.Candidates) (activity.Candidates, error) {
	if a.replayProjection.Load() {
		a.mu.Lock()
		if len(a.generations) > 0 {
			recorded := a.generations[a.next%len(a.generations)]
			a.next++
			a.mu.Unlock()
			shift := time.Now().UTC().Sub(recorded.AsOf)
			result := activity.Candidates{AsOf: recorded.AsOf.Add(shift), Items: make([]activity.Candidate, len(recorded.Items))}
			for i, c := range recorded.Items {
				c.Item.LatestRecommendedAt = c.Item.LatestRecommendedAt.Add(shift)
				c.Item.ExpiresAt = c.Item.ExpiresAt.Add(shift)
				result.Items[i] = c
			}
			return result, nil
		}
		a.mu.Unlock()
	}
	result, err := a.ActivityRepository.RefreshCandidates(ctx, previous)
	if err == nil {
		recorded := activity.Candidates{AsOf: result.AsOf, Items: append([]activity.Candidate(nil), result.Items...)}
		a.mu.Lock()
		if len(a.generations) == ablationGenerations {
			a.generations = append(a.generations[:0], a.generations[1:]...)
		}
		a.generations = append(a.generations, recorded)
		a.next = 0
		a.mu.Unlock()
	}
	return result, err
}

// Counts replays the last recorded count of each event (zero if never read).
func (a *ablationRepository) Counts(ctx context.Context, ids []string) (activity.Counts, error) {
	if a.replayCounts.Load() {
		result := activity.Counts{AsOf: time.Now().UTC(), Items: make([]activity.Count, 0, len(ids))}
		a.mu.Lock()
		for _, id := range ids {
			c, ok := a.counts[id]
			if !ok {
				c = activity.Count{EventID: id}
			}
			result.Items = append(result.Items, c)
		}
		a.mu.Unlock()
		return result, nil
	}
	result, err := a.ActivityRepository.Counts(ctx, ids)
	if err == nil {
		a.mu.Lock()
		for _, c := range result.Items {
			a.counts[c.EventID] = c
		}
		a.mu.Unlock()
	}
	return result, err
}
