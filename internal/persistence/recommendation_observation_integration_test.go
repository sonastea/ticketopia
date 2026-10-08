package persistence

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/events"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
)

func TestMariaDBRecommendationGroupingAntiBumpingAndObservation(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t)}
	auth := accounts.New(pools[0].Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "observed-owner")
	d := savedDetail("ObservedFirst")
	first, created, err := pools[0].Recommendations().Set(t.Context(), owner.ID, d, "An original reason")
	if err != nil || !created || first.Observation == nil || first.Observation.NewPublications != 1 || first.Observation.TotalNewPublications != 1 {
		t.Fatal("first transition not observed", first, created, err)
	}
	for _, reason := range []string{"An original reason", "An edited reason"} {
		item, created, err := pools[1].Recommendations().Set(t.Context(), owner.ID, d, reason)
		if err != nil || created || item.Observation != nil || !item.RecommendedAt.Equal(first.RecommendedAt) {
			t.Fatal("edit/retry counted or bumped", item, created, err)
		}
	}
	owners := []string{owner.ID}
	for i := range 4 {
		who, _ := loginAccount(t, auth, fmt.Sprintf("observed-peer-%d", i))
		owners = append(owners, who.ID)
		if _, _, err := pools[1].Recommendations().Set(t.Context(), who.ID, d, "Another perspective"); err != nil {
			t.Fatal(err)
		}
	}
	next := savedDetail("ObservedNext")
	if _, _, err := pools[1].Recommendations().Set(t.Context(), owners[1], next, "A later event"); err != nil {
		t.Fatal(err)
	}
	s := recommendations.New(pools[2].Recommendations(), nil)
	feed, err := s.Community(t.Context(), nil)
	if err != nil || len(feed.Items) != 2 || feed.Items[0].Event.ID != next.Item.ID || feed.Items[1].Count != 5 || len(feed.Items[1].Recommendations) != recommendations.PreviewLimit || !feed.Items[1].FirstRecommendedAt.Equal(first.RecommendedAt) {
		t.Fatal("not one entry/event with bounded previews and first ordering", feed, err)
	}
	for i := 1; i < len(feed.Items[1].Recommendations); i++ {
		if feed.Items[1].Recommendations[i].RecommendedAt.After(feed.Items[1].Recommendations[i-1].RecommendedAt) {
			t.Fatal("previews not ordered by original publication")
		}
	}
	for range 2 {
		if err := s.Remove(t.Context(), owner.ID, d.Item.ID); err != nil {
			t.Fatal(err)
		}
	}
	var reason string
	if err := pools[0].db.QueryRowContext(t.Context(), `SELECT reason FROM event_recommendations WHERE account_id=? AND event_id=? AND withdrawn_at IS NOT NULL`, owner.ID, d.Item.ID).Scan(&reason); err != nil || reason != "" {
		t.Fatal("withdrawal retained user text", reason, err)
	}
	if _, err := s.Get(t.Context(), owner.ID, d.Item.ID); !errors.Is(err, recommendations.ErrNotFound) {
		t.Fatal("withdrawn endorsement remained readable", err)
	}
	for _, read := range []func() (recommendations.List, error){
		func() (recommendations.List, error) { return s.Own(t.Context(), owner.ID, nil) },
		func() (recommendations.List, error) { return s.Public(t.Context(), owner.ID, nil) },
	} {
		list, err := read()
		if err != nil || len(list.Items) != 0 {
			t.Fatal("withdrawal leaked to account projection", list, err)
		}
	}
	public, err := s.Event(t.Context(), d.Item.ID, nil)
	if err != nil || *public.Count != 4 || len(public.Items) != 4 {
		t.Fatal("withdrawal count/attribution", public, err)
	}
	for _, who := range owners[1:] {
		if err := s.Remove(t.Context(), who, d.Item.ID); err != nil {
			t.Fatal(err)
		}
	}
	feed, err = s.Community(t.Context(), nil)
	if err != nil || len(feed.Items) != 1 || feed.Items[0].Event.ID != next.Item.ID {
		t.Fatal("zero-endorsement event remained in feed", feed, err)
	}
	restarted := f.open(t)
	s = recommendations.New(restarted.Recommendations(), saved.New(restarted.Saved(), nil))
	item, created, err := s.Set(t.Context(), owner.ID, d.Item.ID, "A republished reason")
	if err != nil || !created || !item.RecommendedAt.Equal(first.RecommendedAt) || item.Observation == nil || item.Observation.Reactivations != 1 || item.Observation.TotalNewPublications != 1 || item.Observation.TotalReactivations != 1 {
		t.Fatal("restart reactivation bumped or double-counted publication", item, created, err)
	}
	feed, err = s.Community(t.Context(), nil)
	if err != nil || len(feed.Items) != 2 || feed.Items[0].Event.ID != next.Item.ID || feed.Items[1].Count != 1 || !feed.Items[1].FirstRecommendedAt.Equal(first.RecommendedAt) {
		t.Fatal("all-withdrawn reactivation bumped event", feed, err)
	}
	// Event/endorsement locking makes concurrent reactivations one transition.
	if err := s.Remove(t.Context(), owner.ID, d.Item.ID); err != nil {
		t.Fatal(err)
	}
	var creations, observations atomic.Int32
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Go(func() {
			item, created, err := pools[i%3].Recommendations().Set(t.Context(), owner.ID, d, "Concurrent reactivation")
			if err != nil {
				t.Error(err)
			}
			if created {
				creations.Add(1)
			}
			if item.Observation != nil {
				observations.Add(1)
			}
		})
	}
	wg.Wait()
	if creations.Load() != 1 || observations.Load() != 1 {
		t.Fatal("concurrent reactivation counted multiple times", creations.Load(), observations.Load())
	}
	// Failure in observation persistence rolls back every part of publication.
	failed := savedDetail("ObservedAtomicFailure")
	execSQL(t, f.admin, "REVOKE INSERT ON "+f.runtime.Database+".recommendation_activity FROM '"+f.runtime.User+"'@'%'")
	if _, _, err := restarted.Recommendations().Set(t.Context(), owner.ID, failed, "Must not commit"); err == nil {
		t.Fatal("observation persistence failure silently committed")
	}
	if _, err := restarted.Events().Get(t.Context(), failed.Item.ID); !errors.Is(err, events.ErrNotFound) {
		t.Fatal("failed publication leaked event", err)
	}
	if _, err := restarted.Saved().Snapshot(t.Context(), failed.Item.ID); !errors.Is(err, saved.ErrNotFound) {
		t.Fatal("failed publication leaked snapshot", err)
	}
	if _, err := restarted.Recommendations().Get(t.Context(), owner.ID, failed.Item.ID); !errors.Is(err, recommendations.ErrNotFound) {
		t.Fatal("failed publication leaked recommendation", err)
	}
	execSQL(t, f.admin, "GRANT INSERT ON "+f.runtime.Database+".recommendation_activity TO '"+f.runtime.User+"'@'%'")
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".recommendation_activity SET window_start=UTC_TIMESTAMP(6)-INTERVAL 20 MINUTE,new_publications=90,reactivations=50 WHERE account_id=?", owner.ID)
	item, _, err = restarted.Recommendations().Set(t.Context(), owner.ID, failed, "Now publish")
	if err != nil || item.Observation == nil || item.Observation.NewPublications != 1 || item.Observation.Reactivations != 0 || item.Observation.TotalNewPublications != 2 || item.Observation.TotalReactivations != 2 || item.Observation.WindowStart.Minute()%10 != 0 || item.Observation.WindowStart.Second() != 0 {
		t.Fatal("window reset lost totals or counted rolled-back transition", item, err)
	}
	// Observing a burst never enforces a participation quota.
	for i := range 25 {
		item, _, err = restarted.Recommendations().Set(t.Context(), owner.ID, savedDetail(fmt.Sprintf("ObservedBurst%d", i)), "")
		if err != nil || item.Observation == nil || item.Observation.TotalNewPublications != uint64(i+3) {
			t.Fatal("observe-only burst blocked or miscounted", i, item, err)
		}
	}
	// Different events for the same account race on the single activity row.
	for i := range 12 {
		wg.Go(func() {
			if _, _, err := pools[i%3].Recommendations().Set(t.Context(), owner.ID, savedDetail(fmt.Sprintf("ObservedParallel%d", i)), ""); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	var total uint64
	if err := restarted.db.QueryRowContext(t.Context(), `SELECT total_new_publications FROM recommendation_activity WHERE account_id=?`, owner.ID).Scan(&total); err != nil || total != 39 {
		t.Fatal("parallel activity increments lost", total, err)
	}
	var rows int
	if err := restarted.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM recommendation_activity WHERE account_id=?`, owner.ID).Scan(&rows); err != nil || rows != 1 {
		t.Fatal("observation history is not bounded to one row/account", rows, err)
	}
}

func TestMariaDBRecommendationSchema6UpgradeBackfillsFeedWithoutInventingActivity(t *testing.T) {
	f := newMaria(t)
	previous := fstest.MapFS{}
	for _, name := range []string{"00001_event_identity.sql", "00002_accounts.sql", "00003_saved_events.sql", "00004_event_interests.sql", "00005_event_recommendations.sql", "00006_event_discussions.sql"} {
		data, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		previous[name] = &fstest.MapFile{Data: data}
	}
	if err := migrateFS(t.Context(), f.migration, previous); err != nil {
		t.Fatal(err)
	}
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := &Pool{db: db, config: f.migration}
	auth := accounts.New(old.Accounts(), accountProviderFixture{})
	owner, token := loginAccount(t, auth, "grouped-upgrade")
	other, _ := loginAccount(t, auth, "grouped-upgrade-other")
	d := savedDetail("GroupedUpgrade")
	if _, _, err := old.Saved().Save(t.Context(), owner.ID, d); err != nil {
		t.Fatal(err)
	}
	if _, _, err := old.Interests().Set(t.Context(), owner.ID, d, "private"); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 6, 10, 0, 0, 0, time.UTC)
	execSQL(t, db, `INSERT INTO event_recommendations (account_id,event_id,reason,recommended_at,updated_at) VALUES (?,?,?,?,?),(?,?,?,?,?)`, owner.ID, d.Item.ID, "First reason", at, at, other.ID, d.Item.ID, "Later reason", at.Add(time.Hour), at.Add(time.Hour))
	if _, err := Open(t.Context(), f.runtime); err == nil {
		t.Fatal("schema-7 binary accepted schema 6")
	}
	f.migrate(t)
	p := f.open(t)
	if who, err := accounts.New(p.Accounts(), nil).Authenticate(t.Context(), token, "session"); err != nil || who.Account.ID != owner.ID {
		t.Fatal("upgrade lost session", err)
	}
	if _, err := p.Saved().Get(t.Context(), owner.ID, d.Item.ID); err != nil {
		t.Fatal("upgrade lost save", err)
	}
	if interest, err := p.Interests().Get(t.Context(), owner.ID, d.Item.ID); err != nil || interest.Visibility != "private" {
		t.Fatal("upgrade changed interest", err)
	}
	s := recommendations.New(p.Recommendations(), saved.New(p.Saved(), nil))
	feed, err := s.Community(t.Context(), nil)
	if err != nil || len(feed.Items) != 1 || feed.Items[0].Count != 2 || !feed.Items[0].FirstRecommendedAt.Equal(at) {
		t.Fatal("backfill lost earliest publication/group", feed, err)
	}
	var count int
	if err := p.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM recommendation_activity`).Scan(&count); err != nil || count != 0 {
		t.Fatal("upgrade invented account activity", count, err)
	}
	if item, _, err := s.Set(t.Context(), owner.ID, d.Item.ID, "Edited after upgrade"); err != nil || item.Observation != nil {
		t.Fatal("legacy edit counted", item, err)
	}
	if err := s.Remove(t.Context(), owner.ID, d.Item.ID); err != nil {
		t.Fatal(err)
	}
	item, created, err := s.Set(t.Context(), owner.ID, d.Item.ID, "Restored after upgrade")
	if err != nil || !created || !item.RecommendedAt.Equal(at) || item.Observation == nil || item.Observation.TotalNewPublications != 0 || item.Observation.TotalReactivations != 1 {
		t.Fatal("legacy republication bumped or counted as first publication", item, created, err)
	}
	if err := Migrate(t.Context(), f.migration); err != nil {
		t.Fatal("upgrade not repeatable", err)
	}
}
