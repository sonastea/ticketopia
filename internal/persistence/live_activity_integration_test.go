package persistence

import (
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
)

func TestMariaDBLiveActivityBrowser(t *testing.T) {
	script := os.Getenv("LIVE_ACTIVITY_BROWSER_SCRIPT")
	if script == "" {
		t.Skip("set LIVE_ACTIVITY_BROWSER_SCRIPT to run browser journeys")
	}
	runActivityBrowser(t, script)
}

func TestMariaDBLiveActivityLifecycleAndReplica(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	a, b := f.open(t), f.open(t)
	auth := accounts.New(a.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "live-owner")
	other, _ := loginAccount(t, auth, "live-other")
	name, display, bio := "Anne-Marie", "Forbidden Full Surname", "Forbidden biography"
	if _, err := auth.UpdateProfile(t.Context(), owner.ID, accounts.ProfilePatch{ActivityFirstName: &name, DisplayName: &display, Bio: &bio}); err != nil {
		t.Fatal(err)
	}
	writer := a.Recommendations()
	writer.EnableLiveActivity(true)
	reader := b.Activity()
	detail := savedDetail("LiveExample")
	service := activity.New(t.Context(), reader, 2)
	defer service.Close()
	updates, release, err := service.Subscribe([]string{detail.Item.ID})
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	var wg sync.WaitGroup
	for range 6 {
		wg.Go(func() {
			if _, _, err := writer.Set(t.Context(), owner.ID, detail, "Forbidden reason"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	select {
	case update := <-updates:
		if update.Unavailable || len(update.Snapshot.Items) != 1 || update.Snapshot.Items[0].RecentRecommendations != 1 {
			t.Fatal("replica did not receive unique first publication", update)
		}
		body, _ := json.Marshal(update)
		for _, secret := range []string{display, bio, "Forbidden reason", owner.ID, owner.Email, "/users/", "display_name", "profile"} {
			if strings.Contains(string(body), secret) {
				t.Fatalf("public snapshot leaked %q", secret)
			}
		}
		if !strings.Contains(string(body), name) {
			t.Fatal("explicit name missing")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("replica missed healthy delivery target")
	}
	snapshot, err := reader.Snapshot(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	original := snapshot.Items[0].LatestRecommendedAt
	recommendation, err := writer.Get(t.Context(), owner.ID, detail.Item.ID)
	if err != nil || !original.Equal(recommendation.RecommendedAt) {
		t.Fatal("activity did not retain original publication time", err)
	}
	if _, _, err := writer.Set(t.Context(), owner.ID, detail, "Edited"); err != nil {
		t.Fatal(err)
	}
	empty := ""
	if _, err := auth.UpdateProfile(t.Context(), owner.ID, accounts.ProfilePatch{ActivityFirstName: &empty}); err != nil {
		t.Fatal(err)
	}
	snapshot, err = reader.Snapshot(t.Context())
	if err != nil || snapshot.Items[0].ActorFirstName != nil || !snapshot.Items[0].LatestRecommendedAt.Equal(original) {
		t.Fatal("clearing name changed publication or retained identity", err)
	}
	if _, _, err := writer.Set(t.Context(), other.ID, detail, ""); err != nil {
		t.Fatal(err)
	}
	if _, _, err := a.Interests().Set(t.Context(), owner.ID, detail, "private"); err != nil {
		t.Fatal(err)
	}
	snapshot, err = reader.Snapshot(t.Context())
	if err != nil || snapshot.Items[0].RecentRecommendations != 2 || snapshot.Items[0].ActorFirstName != nil {
		t.Fatal("group projection failed", err)
	}
	counts, err := reader.Counts(t.Context(), []string{detail.Item.ID, "ticketmaster:unknown"})
	if err != nil || counts.Items[0].RecommendationCount != 2 || counts.Items[0].InterestedCount != 1 || counts.Items[1].RecommendationCount != 0 {
		t.Fatal("aggregate semantics failed", counts, err)
	}
	if err := writer.Remove(t.Context(), owner.ID, detail.Item.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := writer.Set(t.Context(), owner.ID, detail, ""); err != nil {
		t.Fatal(err)
	}
	snapshot, err = reader.Snapshot(t.Context())
	if err != nil || snapshot.Items[0].RecentRecommendations != 1 {
		t.Fatal("reactivation announced again", err)
	}
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".live_recommendation_activity SET expires_at=UTC_TIMESTAMP(6)-INTERVAL 1 SECOND")
	snapshot, err = reader.Snapshot(t.Context())
	if err != nil || len(snapshot.Items) != 0 {
		t.Fatal("expired entries retained", err)
	}
	if err := reader.Cleanup(t.Context()); err != nil {
		t.Fatal(err)
	}
	old := savedDetail("DisabledPublication")
	writer.EnableLiveActivity(false)
	if _, _, err := writer.Set(t.Context(), owner.ID, old, ""); err != nil {
		t.Fatal(err)
	}
	writer.EnableLiveActivity(true)
	if err := writer.Remove(t.Context(), owner.ID, old.Item.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := writer.Set(t.Context(), owner.ID, old, ""); err != nil {
		t.Fatal(err)
	}
	snapshot, err = reader.Snapshot(t.Context())
	if err != nil || len(snapshot.Items) != 0 {
		t.Fatal("activation backfilled old publication", err)
	}
}

func TestMariaDBLiveActivityAtomicPublication(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "live-rollback")
	writer := p.Recommendations()
	writer.EnableLiveActivity(true)
	detail := savedDetail("LiveRollback")
	execSQL(t, f.admin, "REVOKE INSERT ON "+f.runtime.Database+".live_recommendation_activity FROM '"+f.runtime.User+"'@'%'")
	if _, _, err := writer.Set(t.Context(), owner.ID, detail, ""); err == nil {
		t.Fatal("publication unexpectedly succeeded")
	}
	var n int
	if err := p.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM event_recommendations WHERE account_id=?`, owner.ID).Scan(&n); err != nil || n != 0 {
		t.Fatal("partial publication committed", n, err)
	}
	execSQL(t, f.admin, "GRANT INSERT ON "+f.runtime.Database+".live_recommendation_activity TO '"+f.runtime.User+"'@'%'")
	if _, _, err := writer.Set(t.Context(), owner.ID, detail, ""); err != nil {
		t.Fatal(err)
	}
	execSQL(t, f.admin, "REVOKE DELETE ON "+f.runtime.Database+".live_recommendation_activity FROM '"+f.runtime.User+"'@'%'")
	if err := writer.Remove(t.Context(), owner.ID, detail.Item.ID); err == nil {
		t.Fatal("withdrawal unexpectedly succeeded")
	}
	if _, err := writer.Get(t.Context(), owner.ID, detail.Item.ID); err != nil {
		t.Fatal("partial withdrawal committed", err)
	}
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".accounts SET activity_first_name='private@example.com' WHERE account_id=?", owner.ID)
	snapshot, err := p.Activity().Snapshot(t.Context())
	if err != nil || len(snapshot.Items) != 1 || snapshot.Items[0].ActorFirstName != nil {
		t.Fatal("invalid stored name exposed", err)
	}
}
