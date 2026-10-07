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
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
)

func TestMariaDBRecommendationConcurrencyOwnershipIndependenceAndRestart(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t)}
	auth := accounts.New(pools[0].Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "recommend-owner")
	other, _ := loginAccount(t, auth, "recommend-other")
	detail := savedDetail("RecommendationCase")
	var created atomic.Int32
	var wg sync.WaitGroup
	for i := range 12 {
		wg.Go(func() {
			_, fresh, err := pools[i%3].Recommendations().Set(t.Context(), owner.ID, detail, "See it live")
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
				t.Error(err)
			}
			if _, _, err := pools[i%3].Interests().Set(t.Context(), owner.ID, detail, "private"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatal("not unique across pools", created.Load())
	}
	first, err := pools[0].Recommendations().Get(t.Context(), owner.ID, detail.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pools[1].Recommendations().Get(t.Context(), other.ID, detail.Item.ID); !errors.Is(err, recommendations.ErrNotFound) {
		t.Fatal("owner read crossed account", err)
	}
	if err := pools[1].Recommendations().Remove(t.Context(), other.ID, detail.Item.ID); err != nil {
		t.Fatal(err)
	}
	s := recommendations.New(pools[2].Recommendations(), nil)
	public, err := s.Event(t.Context(), detail.Item.ID, nil)
	if err != nil || len(public.Items) != 1 || *public.Count != 1 || public.Items[0].Profile.ID != owner.ID {
		t.Fatal("private interest hid public recommendation", public, err)
	}
	if _, _, err := pools[1].Recommendations().Set(t.Context(), other.ID, detail, ""); err != nil {
		t.Fatal(err)
	}
	if err := pools[0].Close(); err != nil {
		t.Fatal(err)
	}
	restarted := f.open(t)
	s = recommendations.New(restarted.Recommendations(), saved.New(restarted.Saved(), nil))
	item, fresh, err := s.Set(t.Context(), owner.ID, detail.Item.ID, "See it live")
	if err != nil || fresh || !item.RecommendedAt.Equal(first.RecommendedAt) || !item.UpdatedAt.Equal(first.UpdatedAt) {
		t.Fatal("restart retry changed timestamps", err)
	}
	item, _, err = s.Set(t.Context(), owner.ID, detail.Item.ID, "SEE it live")
	if err != nil || item.Reason != "SEE it live" || !item.RecommendedAt.Equal(first.RecommendedAt) || !item.UpdatedAt.After(first.UpdatedAt) {
		t.Fatal("case-only offline edit failed", item, err)
	}
	for range 2 {
		if err := s.Remove(t.Context(), owner.ID, detail.Item.ID); err != nil {
			t.Fatal(err)
		}
	}
	public, err = s.Event(t.Context(), detail.Item.ID, nil)
	if err != nil || len(public.Items) != 1 || *public.Count != 1 || public.Items[0].Profile.ID != other.ID {
		t.Fatal("withdrawal retained public attribution or removed other's record", err)
	}
	if _, err := restarted.Saved().Get(t.Context(), owner.ID, detail.Item.ID); err != nil {
		t.Fatal("withdrawal removed save")
	}
	if interest, err := restarted.Interests().Get(t.Context(), owner.ID, detail.Item.ID); err != nil || interest.Visibility != "private" {
		t.Fatal("withdrawal changed interest")
	}
	if _, _, err := s.Set(t.Context(), owner.ID, detail.Item.ID, ""); err != nil {
		t.Fatal("cannot republish with retained snapshot", err)
	}
	failed := savedDetail("RecommendationAtomicFailure")
	if _, _, err := restarted.Recommendations().Set(t.Context(), accounts.ID(), failed, "reason"); err == nil {
		t.Fatal("foreign owner accepted")
	}
	if _, err := restarted.Events().Get(t.Context(), failed.Item.ID); !errors.Is(err, events.ErrNotFound) {
		t.Fatal("failed recommendation leaked identity")
	}
	if _, err := restarted.Saved().Snapshot(t.Context(), failed.Item.ID); !errors.Is(err, saved.ErrNotFound) {
		t.Fatal("failed recommendation leaked snapshot")
	}
	old := detail
	old.Item.Name = "Old title"
	old.Meta.DataAsOf = old.Meta.DataAsOf.Add(-time.Hour)
	if _, _, err := restarted.Recommendations().Set(t.Context(), owner.ID, old, "older"); err != nil {
		t.Fatal(err)
	}
	if stored, err := restarted.Saved().Snapshot(t.Context(), detail.Item.ID); err != nil || stored.Item.Name != detail.Item.Name {
		t.Fatal("older write erased last-known data")
	}
}
func TestMariaDBRecommendationFiltersKeysetsAndAudienceBoundaries(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "recommend-pages")
	other, _ := loginAccount(t, auth, "recommend-pages-other")
	for _, id := range []string{"a", "A", "z"} {
		d := savedDetail(id)
		d.Item.Venues[0].City = "Berlin"
		d.Item.Venues[0].CountryCode = "DE"
		if _, _, err := p.Recommendations().Set(t.Context(), owner.ID, d, ""); err != nil {
			t.Fatal(err)
		}
	}
	d := savedDetail("a")
	d.Item.Venues[0].City = "Berlin"
	d.Item.Venues[0].CountryCode = "DE"
	if _, _, err := p.Recommendations().Set(t.Context(), other.ID, d, "Another perspective"); err != nil {
		t.Fatal(err)
	}
	place := savedDetail("Place")
	place.Item.Venues = nil
	place.Item.Place = &models.Place{City: "Chicago", CountryCode: "US"}
	place.Item.Classifications = nil
	if _, _, err := p.Recommendations().Set(t.Context(), owner.ID, place, "Place event"); err != nil {
		t.Fatal(err)
	}
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".event_recommendations SET recommended_at='2026-10-06 10:00:00.000000'")
	s := recommendations.New(p.Recommendations(), nil)
	values := url.Values{"limit": {"1"}, "city": {"berlin"}, "country": {"de"}, "category_id": {"sports"}}
	seen := map[string]bool{}
	ids := []string{}
	for {
		list, err := s.Community(t.Context(), values)
		if err != nil || len(list.Items) != 1 {
			t.Fatal("community page", list, err)
		}
		item := list.Items[0]
		key := item.Event.ID + item.Profile.ID
		if seen[key] {
			t.Fatal("duplicate endorsement")
		}
		seen[key] = true
		ids = append(ids, item.Event.ID)
		if list.NextCursor == nil {
			break
		}
		values.Set("cursor", *list.NextCursor)
		copy, _ := url.ParseQuery(values.Encode())
		copy.Set("city", "Chicago")
		if _, err := s.Community(t.Context(), copy); err == nil {
			t.Fatal("cursor crossed city filters")
		}
		if _, err := s.Event(t.Context(), "ticketmaster:a", url.Values{"cursor": {*list.NextCursor}}); err == nil {
			t.Fatal("cursor crossed event audience")
		}
		if len(seen) > 4 {
			t.Fatal("pagination loop")
		}
	}
	if fmt.Sprint(ids) != "[ticketmaster:z ticketmaster:a ticketmaster:a ticketmaster:A]" {
		t.Fatal("binary ordering", ids)
	}
	for _, tc := range []struct {
		values url.Values
		count  int
	}{{url.Values{"city": {"Chicago"}, "country": {"us"}}, 1}, {url.Values{"city": {"Berlin"}, "category_id": {"music"}}, 0}, {url.Values{"city": {"%"}}, 0}, {url.Values{"city": {"berlin"}}, 4}} {
		list, err := s.Community(t.Context(), tc.values)
		if err != nil || len(list.Items) != tc.count {
			t.Fatal("scope filtering", tc, list, err)
		}
	}
	own, err := s.Own(t.Context(), owner.ID, url.Values{"limit": {"1"}})
	if err != nil || own.NextCursor == nil {
		t.Fatal(err)
	}
	if _, err := s.Public(t.Context(), owner.ID, url.Values{"cursor": {*own.NextCursor}}); err == nil {
		t.Fatal("owner cursor reused in public projection")
	}
	if _, err := s.Own(t.Context(), other.ID, url.Values{"cursor": {*own.NextCursor}}); err == nil {
		t.Fatal("cursor crossed owner")
	}
	public, err := s.Public(t.Context(), owner.ID, nil)
	if err != nil || len(public.Items) != 4 {
		t.Fatal("public profile collection", err)
	}
}
func TestMariaDBRecommendationMigrationPreservesExistingActivity(t *testing.T) {
	f := newMaria(t)
	previous := fstest.MapFS{}
	for _, name := range []string{"00001_event_identity.sql", "00002_accounts.sql", "00003_saved_events.sql", "00004_event_interests.sql"} {
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
	a, raw := loginAccount(t, accounts.New(&AccountRepository{db: db, timeout: f.migration.QueryTimeout}, accountProviderFixture{}), "before-recommend")
	detail := savedDetail("BeforeRecommend")
	if _, _, err := (&SavedRepository{db: db, timeout: f.migration.QueryTimeout}).Save(t.Context(), a.ID, detail); err != nil {
		t.Fatal(err)
	}
	if _, _, err := (&InterestRepository{db: db, timeout: f.migration.QueryTimeout}).Set(t.Context(), a.ID, detail, "private"); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), f.runtime); err == nil {
		t.Fatal("v5 binary accepted v4")
	}
	f.migrate(t)
	p := f.open(t)
	if principal, err := accounts.New(p.Accounts(), nil).Authenticate(t.Context(), raw, "session"); err != nil || principal.Account.ID != a.ID {
		t.Fatal("migration lost session/account", err)
	}
	if _, err := p.Saved().Get(t.Context(), a.ID, detail.Item.ID); err != nil {
		t.Fatal("migration lost save", err)
	}
	if interest, err := p.Interests().Get(t.Context(), a.ID, detail.Item.ID); err != nil || interest.Visibility != "private" {
		t.Fatal("migration changed interest", err)
	}
	if count, err := p.Recommendations().Count(t.Context(), detail.Item.ID); err != nil || count != 0 {
		t.Fatal("migration published existing activity", count, err)
	}
}
