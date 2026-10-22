package persistence

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/kv"
)

func TestMariaDBActivityMembership(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	a := f.open(t)
	b := f.open(t)
	auth := accounts.New(a.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "membership-owner")
	other, _ := loginAccount(t, auth, "membership-other")
	seedLiveEntityWindow(t, a, other.ID)
	service := activity.New(t.Context(), b.Activity(), 256)
	defer service.Close()
	filter := activity.DefaultPreferences().Filters
	filter.Connections = activity.Connections{Mode: "any", Sources: []string{"saved", "interested", "followed_artists", "followed_venues"}}
	revision := func() uint64 {
		t.Helper()
		r, err := a.Activity().MembershipRevisions(t.Context(), []string{owner.ID})
		if err != nil {
			t.Fatal(err)
		}
		return r[owner.ID]
	}
	expect := func(n int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for {
			view, err := service.OwnerSnapshot(t.Context(), owner.ID, filter)
			if err == nil && len(view.Items) == n {
				body, _ := json.Marshal(view)
				for _, secret := range []string{owner.ID, other.ID, "membership", "sources", "visibility", "account_id"} {
					if strings.Contains(string(body), secret) {
						t.Fatal("private payload leak", secret)
					}
				}
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("want %d got %d: %v", n, len(view.Items), err)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	expect(0)
	local := activity.New(t.Context(), a.Activity(), 256)
	defer local.Close()
	if _, err := local.OwnerSnapshot(t.Context(), owner.ID, filter); err != nil {
		t.Fatal(err)
	}
	updates, release, err := local.SubscribeOwner(owner.ID, nil, filter)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("missing warm local view")
	}
	d, err := a.Saved().Snapshot(t.Context(), "ticketmaster:LiveFilter_0")
	if err != nil {
		t.Fatal(err)
	}
	before := revision()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, _, err := a.Saved().Save(t.Context(), owner.ID, d); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if revision() != before+1 {
		t.Fatal("concurrent/idempotent save revision")
	}
	select {
	case update := <-updates:
		if !update.Unavailable || !update.MembershipChanged {
			t.Fatal("local mutation retained old membership or was not marked as a membership change")
		}
	case <-time.After(time.Second):
		t.Fatal("local mutation did not invalidate subscription")
	}
	release()
	expect(1)
	if _, _, err := a.Interests().Set(t.Context(), owner.ID, d, "private"); err != nil {
		t.Fatal(err)
	}
	expect(1)
	before = revision()
	if _, _, err := a.Interests().Set(t.Context(), owner.ID, d, "public"); err != nil {
		t.Fatal(err)
	}
	if revision() != before {
		t.Fatal("visibility changed membership")
	}
	if err := a.Saved().Remove(t.Context(), owner.ID, d.Item.ID); err != nil {
		t.Fatal(err)
	}
	expect(1)
	if err := a.Interests().Remove(t.Context(), owner.ID, d.Item.ID); err != nil {
		t.Fatal(err)
	}
	expect(0)
	before = revision()
	if err := a.Interests().Remove(t.Context(), owner.ID, d.Item.ID); err != nil {
		t.Fatal(err)
	}
	if revision() != before {
		t.Fatal("idempotent removal revision")
	}
	for _, target := range []struct {
		kind, id string
		count    int
	}{{"artist", "A", 2}, {"venue", "Hall", 3}} {
		// Use the established catalog snapshot shape to avoid provider reads.
		snapshot, e := a.Follows().Snapshot(t.Context(), target.kind, "ticketmaster:"+target.id)
		if e != nil {
			t.Fatal(e)
		}
		before = revision()
		for range 2 {
			if _, _, err := a.Follows().Follow(t.Context(), owner.ID, snapshot); err != nil {
				t.Fatal(err)
			}
		}
		if revision() != before+1 {
			t.Fatal("follow no-op revision")
		}
		expect(target.count)
		if err := a.Follows().Remove(t.Context(), owner.ID, target.kind, snapshot.Item.ID); err != nil {
			t.Fatal(err)
		}
		expect(0)
	}
	// Real rollback cannot advance the revision ahead of its relationship.
	before = revision()
	tx, err := a.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.ExecContext(t.Context(), `INSERT INTO saved_events(account_id,event_id) VALUES (?,?)`, owner.ID, d.Item.ID); err != nil {
		t.Fatal(err)
	}
	if err = bumpActivityMembership(t.Context(), tx, owner.ID); err != nil {
		t.Fatal(err)
	}
	if err = tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if revision() != before {
		t.Fatal("rolled back revision escaped")
	}
	expect(0)
	// Force the real writer's revision update to fail after inserting a save.
	// The relationship and its revision must still roll back together.
	execSQL(t, a.db, `UPDATE accounts SET activity_membership_revision=18446744073709551615 WHERE account_id=?`, owner.ID)
	if _, _, err := a.Saved().Save(t.Context(), owner.ID, d); err == nil {
		t.Fatal("revision failure was ignored")
	}
	var count int
	if err := a.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM saved_events WHERE account_id=? AND event_id=?`, owner.ID, d.Item.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("failed writer left relationship behind", count, err)
	}
	execSQL(t, a.db, `UPDATE accounts SET activity_membership_revision=? WHERE account_id=?`, before, owner.ID)
	data, err := a.Activity().Candidates(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	stale := activity.MembershipCandidate{ID: data.Items[0].Item.EventID, Revision: data.Items[0].Revision.Add(-time.Second)}
	if _, err := a.Activity().Memberships(t.Context(), []activity.MembershipOwner{{ID: owner.ID, Sources: 15}}, []activity.MembershipCandidate{stale}); err == nil {
		t.Fatal("mixed metadata accepted")
	}
	for _, sources := range []uint8{4, 8} {
		if _, err := a.Activity().Memberships(t.Context(), []activity.MembershipOwner{{ID: owner.ID, Sources: sources}}, []activity.MembershipCandidate{stale}); err == nil {
			t.Fatal("facet join accepted mixed metadata", sources)
		}
	}
	// Event-ID sources cannot mix facts, so a metadata correction after the
	// generation must not fail (and back off) their assembly.
	if _, _, err := a.Interests().Set(t.Context(), owner.ID, savedDetail(strings.TrimPrefix(stale.ID, "ticketmaster:")), "private"); err != nil {
		t.Fatal(err)
	}
	matched, err := a.Activity().Memberships(t.Context(), []activity.MembershipOwner{{ID: owner.ID, Sources: 3}}, []activity.MembershipCandidate{stale})
	if err != nil || matched[owner.ID].Matches[stale.ID] != 2 {
		t.Fatal("event-ID membership rejected after metadata correction", matched, err)
	}
	if err := a.Interests().Remove(t.Context(), owner.ID, stale.ID); err != nil {
		t.Fatal(err)
	}
	// Other account's seeded saves remain independent, and public callers fail closed.
	view, err := service.OwnerSnapshot(t.Context(), other.ID, filter)
	if err != nil || len(view.Items) != 4 {
		t.Fatal("owner isolation", len(view.Items), err)
	}
	if _, err := service.FilteredSnapshot(t.Context(), filter); err == nil {
		t.Fatal("public personal filter accepted")
	}
}

func TestMariaDBMembershipBenchmark(t *testing.T) {
	if testing.Short() {
		t.Skip("populated membership baseline")
	}
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	owner, _ := loginAccount(t, accounts.New(p.Accounts(), accountProviderFixture{}), "membership-benchmark")
	seedLiveFilterWindow(t, p, owner.ID, 256)
	data, err := p.Activity().Candidates(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	candidates := []activity.MembershipCandidate{}
	for _, c := range data.Items {
		candidates = append(candidates, activity.MembershipCandidate{ID: c.Item.EventID, Revision: c.Revision})
	}
	for _, n := range []int{1, 256} {
		for i := 0; i < n; i++ {
			execSQL(t, p.db, `INSERT IGNORE INTO saved_events(account_id,event_id) VALUES (?,?)`, owner.ID, fmt.Sprintf("ticketmaster:LiveFilter_%d", i))
		}
		start := time.Now()
		for range 20 {
			result, err := p.Activity().Memberships(t.Context(), []activity.MembershipOwner{{ID: owner.ID, Sources: 1}}, candidates)
			if err != nil || len(result[owner.ID].Matches) != n {
				t.Fatal(n, result, err)
			}
		}
		t.Logf("candidate-scoped saved matches=%d candidates=256 average=%s samples=20", n, time.Since(start)/20)
	}
}

func TestMariaDBInterestMembershipRace(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	owner, _ := loginAccount(t, accounts.New(p.Accounts(), accountProviderFixture{}), "interest-membership-race")
	detail := savedDetail("InterestMembershipRace")
	for range 50 {
		if _, _, err := p.Interests().Set(t.Context(), owner.ID, detail, "private"); err != nil {
			t.Fatal(err)
		}
		before, err := p.Activity().MembershipRevisions(t.Context(), []string{owner.ID})
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Go(func() {
			if _, _, err := p.Interests().Set(t.Context(), owner.ID, detail, "public"); err != nil {
				t.Error(err)
			}
		})
		wg.Go(func() {
			if err := p.Interests().Remove(t.Context(), owner.ID, detail.Item.ID); err != nil {
				t.Error(err)
			}
		})
		wg.Wait()
		var count int
		var revision uint64
		if err := p.db.QueryRowContext(t.Context(), `SELECT activity_membership_revision,(SELECT COUNT(*) FROM event_interests WHERE account_id=? AND event_id=?) FROM accounts WHERE account_id=?`, owner.ID, detail.Item.ID, owner.ID).Scan(&revision, &count); err != nil {
			t.Fatal(err)
		}
		// Update then remove changes membership once. Remove then reinsert changes
		// it twice; a visibility-only write cannot recreate an unrevisioned row.
		if revision != before[owner.ID]+1+uint64(count) {
			t.Fatal("unrevisioned concurrent transition", before[owner.ID], revision, count)
		}
	}
}

// A same-replica membership change closes the owner's private stream with the
// distinct membership.changed event, so browsers reconnect quietly instead of
// applying outage backoff; it never sends unavailable for this cause.
func TestMariaDBLocalMembershipChangeEvent(t *testing.T) {
	t.Setenv("IP_GEOLOCATION_ENABLED", "false")
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, raw := loginAccount(t, auth, "membership-event")
	seedLiveFilterWindow(t, p, owner.ID, 3)
	filter := activity.DefaultPreferences().Filters
	filter.Connections = activity.Connections{Mode: "any", Sources: []string{"saved"}}
	if _, err := p.Accounts().ReplaceLivePreferences(t.Context(), owner.ID, 1, true, filter); err != nil {
		t.Fatal(err)
	}
	service := activity.New(t.Context(), p.Activity(), 8)
	defer service.Close()
	server := httptest.NewUnstartedServer(nil)
	config := accounts.Config{Enabled: true, BaseURL: "http://" + server.Listener.Addr().String(), ClientID: "fixture", ClientSecret: "fixture"}
	app, err := api.NewAPI(t.Context(), zerolog.Nop(), kv.NewMemory(), api.WithAccounts(config, auth), api.WithLiveActivity(service, 60))
	if err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = app.Routes()
	server.Start()
	defer server.Close()
	req, _ := http.NewRequestWithContext(t.Context(), "GET", server.URL+"/api/v1/me/community/activity/stream", nil)
	req.AddCookie(&http.Cookie{Name: "ticketopia_session", Value: raw})
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatal("private stream status", resp.Status)
	}
	defer resp.Body.Close()
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() && !strings.HasPrefix(scanner.Text(), "data:") {
	}
	d, err := p.Saved().Snapshot(t.Context(), "ticketmaster:LiveFilter_1")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Saved().Save(t.Context(), owner.ID, d); err != nil {
		t.Fatal(err)
	}
	rest := make(chan string, 1)
	go func() { data, _ := io.ReadAll(resp.Body); rest <- string(data) }()
	select {
	case data := <-rest:
		if !strings.Contains(data, "event: membership.changed") || strings.Contains(data, "event: unavailable") {
			t.Fatal("local membership change frame", data)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("local membership change did not close the stream")
	}
}
