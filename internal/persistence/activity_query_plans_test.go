package persistence

import (
	"fmt"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
)

func TestMariaDBActivityQueryCosts(t *testing.T) {
	if os.Getenv("MARIADB_ACTIVITY_PLANS") != "1" {
		t.Skip("set MARIADB_ACTIVITY_PLANS=1 to measure activity queries")
	}
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	ctx := t.Context()
	const events = 1000
	const actors = 32
	for start := 0; start < events; start += 100 {
		items := make([]models.Event, 0, 100)
		for i := start; i < start+100; i++ {
			items = append(items, benchmarkEvent(i))
		}
		tx, err := p.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := upsertPageSnapshotsTx(ctx, tx, models.EventList{Items: items, Meta: models.Freshness{DataAsOf: benchmarkAt}}); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	execSQL(t, p.db, `INSERT INTO accounts (account_id,email,display_name)
 SELECT LPAD(seq,32,'0'),CONCAT('activity-plans-',seq,'@example.test'),CONCAT('Actor ',seq) FROM seq_1_to_32`)
	execSQL(t, p.db, `INSERT INTO event_recommendations (account_id,event_id,reason,recommended_at,updated_at)
 SELECT account_id,event_id,'fixture',UTC_TIMESTAMP(6),UTC_TIMESTAMP(6) FROM accounts CROSS JOIN event_snapshots`)
	execSQL(t, p.db, `INSERT INTO event_interests (account_id,event_id)
 SELECT account_id,event_id FROM event_recommendations`)
	execSQL(t, p.db, `INSERT INTO live_recommendation_activity (account_id,event_id,published_at,expires_at)
 SELECT account_id,event_id,UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)+INTERVAL 1 HOUR FROM event_recommendations`)

	r := p.Activity()
	var ids []string
	for i := range 101 {
		ids = append(ids, benchmarkEvent(i).ID)
	}
	prior, err := r.Candidates(ctx)
	if err != nil || len(prior.Items) != events {
		t.Fatalf("initial projection has %d groups: %v", len(prior.Items), err)
	}
	for _, tc := range []struct {
		name string
		read func() error
	}{
		{"cached_facets_projection", func() error {
			result, err := r.RefreshCandidates(ctx, prior)
			if err != nil {
				return err
			}
			if len(result.Items) != events || result.Items[0].Item.RecentRecommendations != actors {
				return fmt.Errorf("unexpected projection: %d groups", len(result.Items))
			}
			return nil
		}},
		{"uncached_101_event_totals", func() error {
			result, err := r.Counts(ctx, ids)
			if err != nil {
				return err
			}
			if len(result.Items) != len(ids) {
				return fmt.Errorf("unexpected result: %d totals", len(result.Items))
			}
			for i, v := range result.Items {
				if v.EventID != ids[i] || v.RecommendationCount != actors || v.InterestedCount != actors {
					return fmt.Errorf("incorrect totals for %q", ids[i])
				}
			}
			return nil
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var samples []time.Duration
			for i := range 22 {
				start := time.Now()
				if err := tc.read(); err != nil {
					t.Fatal(err)
				}
				if i > 0 {
					samples = append(samples, time.Since(start))
				}
			}
			slices.Sort(samples)
			t.Logf("median=%s p95=%s", samples[10], samples[19])
		})
	}
	var raw string
	if err := p.db.QueryRowContext(ctx, `ANALYZE FORMAT=JSON `+activityProjectionSQL, prior.AsOf, prior.AsOf).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	t.Logf("projection plan: %s", raw)
}
