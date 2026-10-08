package persistence

import (
	"errors"
	"fmt"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/events"
	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/saved"
)

func TestMariaDBInterestPrivacyConcurrencyIndependenceAndRestart(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t)}
	auth := accounts.New(pools[0].Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "interest-owner")
	other, _ := loginAccount(t, auth, "interest-other")
	detail := savedDetail("InterestCase")
	var created atomic.Int32
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Go(func() {
			_, fresh, err := pools[i%3].Interests().Set(t.Context(), owner.ID, detail, "private")
			if err != nil {
				t.Error(err)
			}
			if fresh {
				created.Add(1)
			}
		})
	}
	for i := range 6 {
		wg.Go(func() {
			if _, _, err := pools[i%3].Saved().Save(t.Context(), owner.ID, detail); err != nil {
				t.Error("concurrent independent save", err)
			}
		})
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatal("interest not unique across pools", created.Load())
	}
	first, err := pools[0].Interests().Get(t.Context(), owner.ID, detail.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pools[1].Interests().Get(t.Context(), other.ID, detail.Item.ID); !errors.Is(err, interests.ErrNotFound) {
		t.Fatal("private choice crossed owners")
	}
	if err := pools[1].Interests().Remove(t.Context(), other.ID, detail.Item.ID); err != nil {
		t.Fatal(err)
	}
	public, _ := pools[1].Interests().Participants(t.Context(), detail.Item.ID, interests.Query{Limit: 20})
	if len(public) != 0 {
		t.Fatal("private identity leaked")
	}
	if _, _, err := pools[1].Interests().Set(t.Context(), other.ID, detail, "public"); err != nil {
		t.Fatal(err)
	}
	states, err := pools[2].Interests().States(t.Context(), "", []string{detail.Item.ID})
	if err != nil || states[detail.Item.ID].Count != 2 || states[detail.Item.ID].Interested || states[detail.Item.ID].Visibility != "" {
		t.Fatal("public counts leaked private fields", states, err)
	}
	states, err = pools[2].Interests().States(t.Context(), owner.ID, []string{detail.Item.ID})
	if err != nil || !states[detail.Item.ID].Interested || states[detail.Item.ID].Visibility != "private" {
		t.Fatal("own choice not distinct from count")
	}
	public, err = pools[2].Interests().Participants(t.Context(), detail.Item.ID, interests.Query{Limit: 20})
	if err != nil || len(public) != 1 || public[0].Profile.ID != other.ID {
		t.Fatal("participant visibility wrong", public, err)
	}
	if _, _, err := pools[2].Saved().Save(t.Context(), owner.ID, detail); err != nil {
		t.Fatal(err)
	}
	if err := pools[2].Interests().Remove(t.Context(), owner.ID, detail.Item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := pools[1].Saved().Get(t.Context(), owner.ID, detail.Item.ID); err != nil {
		t.Fatal("interest removal removed save")
	}
	if _, _, err := pools[1].Interests().Set(t.Context(), owner.ID, detail, "private"); err != nil {
		t.Fatal(err)
	}
	if err := pools[1].Saved().Remove(t.Context(), owner.ID, detail.Item.ID); err != nil {
		t.Fatal(err)
	}
	first, err = pools[1].Interests().Get(t.Context(), owner.ID, detail.Item.ID)
	if err != nil {
		t.Fatal("unsave removed interest")
	}
	if err := pools[0].Close(); err != nil {
		t.Fatal(err)
	}
	restarted := f.open(t)
	service := interests.New(restarted.Interests(), saved.New(restarted.Saved(), nil))
	item, fresh, err := service.Set(t.Context(), owner.ID, detail.Item.ID, nil, "public")
	if err != nil || fresh || item.Visibility != "private" || !item.InterestedAt.Equal(first.InterestedAt) {
		t.Fatal("restart/retry/new default changed existing choice", err)
	}
	visibility := "public"
	item, fresh, err = service.Set(t.Context(), owner.ID, detail.Item.ID, &visibility, "private")
	if err != nil || fresh || !item.InterestedAt.Equal(first.InterestedAt) {
		t.Fatal("visibility update reset timestamp", err)
	}
	visibility = "private"
	if _, _, err := service.Set(t.Context(), owner.ID, detail.Item.ID, &visibility, "private"); err != nil {
		t.Fatal(err)
	}
	public, _ = restarted.Interests().Participants(t.Context(), detail.Item.ID, interests.Query{Limit: 20})
	if len(public) != 1 || public[0].Profile.ID != other.ID {
		t.Fatal("privacy revocation retained public identity")
	}
	for range 2 {
		if err := service.Remove(t.Context(), owner.ID, detail.Item.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := service.Set(t.Context(), owner.ID, detail.Item.ID, nil, "private"); err != nil {
		t.Fatal("could not reselect using retained metadata", err)
	}
	failed := savedDetail("InterestAtomicFailure")
	if _, _, err := restarted.Interests().Set(t.Context(), accounts.ID(), failed, "private"); err == nil {
		t.Fatal("foreign account accepted")
	}
	if _, err := restarted.Events().Get(t.Context(), failed.Item.ID); !errors.Is(err, events.ErrNotFound) {
		t.Fatal("failed interest leaked identity")
	}
	if _, err := restarted.Saved().Snapshot(t.Context(), failed.Item.ID); !errors.Is(err, saved.ErrNotFound) {
		t.Fatal("failed interest leaked snapshot")
	}
	old := detail
	old.Item.Name = "Old interest title"
	old.Meta.DataAsOf = old.Meta.DataAsOf.Add(-time.Hour)
	if _, _, err := restarted.Interests().Set(t.Context(), owner.ID, old, "private"); err != nil {
		t.Fatal(err)
	}
	stored, err := restarted.Saved().Snapshot(t.Context(), detail.Item.ID)
	if err != nil || stored.Item.Name != detail.Item.Name {
		t.Fatal("interest erased newer snapshot")
	}
}
func TestMariaDBInterestKeysetsAndPublicAudienceScopes(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "interest-pagination")
	for _, id := range []string{"a", "A", "z"} {
		if _, _, err := p.Interests().Set(t.Context(), owner.ID, savedDetail(id), "public"); err != nil {
			t.Fatal(err)
		}
	}
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".event_interests SET interested_at='2026-10-06 10:00:00.000000'")
	s := interests.New(p.Interests(), nil)
	values := url.Values{"limit": {"1"}}
	ids := []string{}
	for {
		list, err := s.List(t.Context(), owner.ID, false, values)
		if err != nil || len(list.Items) != 1 {
			t.Fatal("interest page", err)
		}
		ids = append(ids, list.Items[0].Event.ID)
		if list.NextCursor == nil {
			break
		}
		values.Set("cursor", *list.NextCursor)
		if _, err := s.List(t.Context(), owner.ID, true, values); err == nil {
			t.Fatal("own cursor used for public activity")
		}
		if _, err := s.List(t.Context(), accounts.ID(), false, values); err == nil {
			t.Fatal("cursor crossed owners")
		}
		if len(ids) > 3 {
			t.Fatal("interest pagination loop")
		}
	}
	if fmt.Sprint(ids) != "[ticketmaster:z ticketmaster:a ticketmaster:A]" {
		t.Fatal("binary interest order", ids)
	}
	for _, subject := range []string{"participant-two", "participant-three"} {
		a, _ := loginAccount(t, auth, subject)
		if _, _, err := p.Interests().Set(t.Context(), a.ID, savedDetail("a"), "public"); err != nil {
			t.Fatal(err)
		}
	}
	values = url.Values{"limit": {"1"}}
	seen := map[string]bool{}
	for {
		list, err := s.Participants(t.Context(), "ticketmaster:a", values)
		if err != nil || len(list.Items) != 1 {
			t.Fatal("participant page", err)
		}
		id := list.Items[0].Profile.ID
		if seen[id] {
			t.Fatal("participant duplicate")
		}
		seen[id] = true
		if list.NextCursor == nil {
			break
		}
		values.Set("cursor", *list.NextCursor)
		if _, err := s.Participants(t.Context(), "ticketmaster:A", values); err == nil {
			t.Fatal("participant cursor crossed events")
		}
		if len(seen) > 3 {
			t.Fatal("participant loop")
		}
	}
	if len(seen) != 3 {
		t.Fatal("participant lost", seen)
	}
	visibility := "private"
	if _, _, err := s.Set(t.Context(), owner.ID, "ticketmaster:a", &visibility, "private"); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(t.Context(), owner.ID, true, nil)
	if err != nil || len(list.Items) != 2 {
		t.Fatal("private interest in public profile")
	}
}
func TestMariaDBInterestMigrationPreservesSavesAccountsAndSessions(t *testing.T) {
	f := newMaria(t)
	previous := fstest.MapFS{}
	for _, name := range []string{"00001_event_identity.sql", "00002_accounts.sql", "00003_saved_events.sql"} {
		data, _ := migrations.ReadFile("migrations/" + name)
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
	a, raw := loginAccount(t, accounts.New(&AccountRepository{db: db, timeout: f.migration.QueryTimeout}, accountProviderFixture{}), "pre-interest")
	detail := savedDetail("BeforeInterest")
	seedLegacySavedEvent(t, db, a.ID, detail)
	if _, err := Open(t.Context(), f.runtime); err == nil {
		t.Fatal("v4 binary accepted v3")
	}
	f.migrate(t)
	p := f.open(t)
	if principal, err := accounts.New(p.Accounts(), nil).Authenticate(t.Context(), raw, "session"); err != nil || principal.Account.ID != a.ID {
		t.Fatal("interest migration lost account/session")
	}
	if item, err := p.Saved().Get(t.Context(), a.ID, detail.Item.ID); err != nil || item.Event.ID != detail.Item.ID {
		t.Fatal("interest migration lost save")
	}
	states, err := p.Interests().States(t.Context(), a.ID, []string{detail.Item.ID})
	if err != nil || states[detail.Item.ID].Interested || states[detail.Item.ID].Count != 0 {
		t.Fatal("migration turned save into interest")
	}
}
