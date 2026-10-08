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
	"github.com/sonastea/ticketopia/internal/follows"
	"github.com/sonastea/ticketopia/internal/models"
)

func followDetail(kind, id string) models.CatalogDetail {
	t := models.FollowTarget{Kind: kind, ID: "ticketmaster:" + id, Name: "Same name", Source: models.Source{Provider: "ticketmaster", ID: id, URL: "https://example.com/" + id}}
	if kind == "venue" {
		t.Location = &models.Place{Name: t.Name, City: "Boston", Address: "1 Main St"}
		t.Timezone = "America/New_York"
	}
	return models.CatalogDetail{Item: t, Meta: models.Freshness{DataAsOf: time.Now().UTC().Truncate(time.Microsecond)}}
}
func TestMariaDBFollowsAtomicPrivacyConcurrencyAndRestart(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t)}
	auth := accounts.New(pools[0].Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "follow-owner")
	other, _ := loginAccount(t, auth, "follow-other")
	for _, kind := range []string{"artist", "venue"} {
		detail := followDetail(kind, "Case_ID")
		var count atomic.Int32
		var wg sync.WaitGroup
		for i := range 12 {
			wg.Go(func() {
				_, created, err := pools[i%3].Follows().Follow(t.Context(), owner.ID, detail)
				if err != nil {
					t.Error(err)
				}
				if created {
					count.Add(1)
				}
			})
		}
		wg.Wait()
		if count.Load() != 1 {
			t.Fatal("duplicate follow created", kind, count.Load())
		}
		first, err := pools[0].Follows().Get(t.Context(), owner.ID, kind, detail.Item.ID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pools[1].Follows().Get(t.Context(), other.ID, kind, detail.Item.ID); !errors.Is(err, follows.ErrNotFound) {
			t.Fatal("foreign owner read follow", err)
		}
		restarted := f.open(t)
		service := follows.New(restarted.Follows(), nil)
		item, created, err := service.Follow(t.Context(), owner.ID, kind, detail.Item.ID)
		if err != nil || created || !item.FollowedAt.Equal(first.FollowedAt) || !item.Meta.Stale {
			t.Fatal("retry/restart not durable", err)
		}
		if kind == "venue" && (item.Target.Location.City != "Boston" || item.Target.Timezone != detail.Item.Timezone) {
			t.Fatal("venue metadata lost")
		}
		if _, _, err := service.Follow(t.Context(), other.ID, kind, detail.Item.ID); err != nil {
			t.Fatal("retained provider-free follow failed", err)
		}
		for range 2 {
			if err := service.Remove(t.Context(), owner.ID, kind, detail.Item.ID); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := service.Get(t.Context(), other.ID, kind, detail.Item.ID); err != nil {
			t.Fatal("owner removal changed another follow", err)
		}
		if _, _, err := service.Follow(t.Context(), owner.ID, kind, detail.Item.ID); err != nil {
			t.Fatal("retained re-follow failed", err)
		}
		old := detail
		old.Item.Name = "Old name"
		old.Meta.DataAsOf = old.Meta.DataAsOf.Add(-time.Hour)
		if _, _, err := restarted.Follows().Follow(t.Context(), owner.ID, old); err != nil {
			t.Fatal(err)
		}
		item, err = restarted.Follows().Get(t.Context(), owner.ID, kind, detail.Item.ID)
		if err != nil || item.Target.Name != detail.Item.Name {
			t.Fatal("older snapshot overwrote metadata", err)
		}
		failed := followDetail(kind, "AtomicFailure")
		if _, _, err := restarted.Follows().Follow(t.Context(), accounts.ID(), failed); err == nil {
			t.Fatal("foreign account accepted")
		}
		if _, err := restarted.Follows().Snapshot(t.Context(), kind, failed.Item.ID); !errors.Is(err, follows.ErrNotFound) {
			t.Fatal("failed follow leaked catalog row", err)
		}
		_, _, column, collection, _ := followTables(kind)
		if _, err := restarted.db.ExecContext(t.Context(), `UPDATE `+collection+` SET followed_at=UTC_TIMESTAMP(6)`); err == nil {
			t.Fatal("runtime can rewrite follow times", column)
		}
	}
	list, err := follows.New(pools[0].Follows(), nil).List(t.Context(), owner.ID, url.Values{})
	if err != nil || len(list.Items) != 2 {
		t.Fatal("same provider ID collided across kinds", err)
	}
}
func TestMariaDBFollowKeysetsAndMigrationPreservation(t *testing.T) {
	f := newMaria(t)
	legacy := fstest.MapFS{}
	files, _ := migrations.ReadDir("migrations")
	for _, file := range files {
		if file.Name() < "00010" {
			data, _ := migrations.ReadFile("migrations/" + file.Name())
			legacy[file.Name()] = &fstest.MapFile{Data: data}
		}
	}
	if err := migrateFS(t.Context(), f.migration, legacy); err != nil {
		t.Fatal(err)
	}
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	owner, raw := loginAccount(t, accounts.New(&AccountRepository{db: db, timeout: time.Second}, accountProviderFixture{}), "pre-follow")
	detail := savedDetail("BeforeFollows")
	if _, _, err := (&SavedRepository{db: db, timeout: time.Second}).Save(t.Context(), owner.ID, detail); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), f.runtime); err == nil {
		t.Fatal("schema 9 accepted")
	}
	f.migrate(t)
	p := f.open(t)
	if _, err := accounts.New(p.Accounts(), nil).Authenticate(t.Context(), raw, "session"); err != nil {
		t.Fatal("migration lost session", err)
	}
	if _, err := p.Saved().Get(t.Context(), owner.ID, detail.Item.ID); err != nil {
		t.Fatal("migration lost save", err)
	}
	for _, kind := range []string{"artist", "venue"} {
		for _, id := range []string{"a", "A"} {
			if _, _, err := p.Follows().Follow(t.Context(), owner.ID, followDetail(kind, id)); err != nil {
				t.Fatal(err)
			}
		}
		_, _, _, table, _ := followTables(kind)
		execSQL(t, f.admin, `UPDATE `+f.runtime.Database+`.`+table+` SET followed_at='2026-10-08 10:00:00.000000'`)
	}
	s := follows.New(p.Follows(), nil)
	values := url.Values{"limit": {"1"}}
	ids := []string{}
	for range 5 {
		list, err := s.List(t.Context(), owner.ID, values)
		if err != nil || len(list.Items) != 1 {
			t.Fatal("keyset page", err)
		}
		ids = append(ids, list.Items[0].Target.Kind+":"+list.Items[0].Target.ID)
		if list.NextCursor == nil {
			break
		}
		values.Set("cursor", *list.NextCursor)
	}
	if fmt.Sprint(ids) != "[venue:ticketmaster:a venue:ticketmaster:A artist:ticketmaster:a artist:ticketmaster:A]" {
		t.Fatal("binary kind/ID ordering lost", ids)
	}
	list, err := s.List(t.Context(), owner.ID, url.Values{"kind": {"artist"}})
	if err != nil || len(list.Items) != 2 {
		t.Fatal("kind filtering failed", err)
	}
	if err := Migrate(t.Context(), f.migration); err != nil {
		t.Fatal("migration not repeatable", err)
	}
}
