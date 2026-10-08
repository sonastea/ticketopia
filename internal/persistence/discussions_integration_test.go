package persistence

import (
	"context"
	"errors"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/events"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

func TestMariaDBDiscussionLoopRetriesOwnershipAndOfflineRestart(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t)}
	auth := accounts.New(pools[0].Accounts(), accountProviderFixture{})
	who, _ := loginAccount(t, auth, "discussion-owner")
	other, _ := loginAccount(t, auth, "discussion-other")
	detail := savedDetail("DiscussionCase")
	if _, _, err := pools[0].Saved().Save(t.Context(), who.ID, detail); err != nil {
		t.Fatal(err)
	}
	if _, _, err := pools[0].Interests().Set(t.Context(), who.ID, detail, "private"); err != nil {
		t.Fatal(err)
	}
	services := []*discussions.Service{}
	for _, p := range pools {
		services = append(services, discussions.New(p.Discussions(), saved.New(p.Saved(), nil)))
	}
	key := accounts.ID()
	var fresh atomic.Int32
	var wg sync.WaitGroup
	ids := make(chan string, 12)
	for i := range 12 {
		wg.Go(func() {
			post, created, err := services[i%3].Create(t.Context(), who.ID, detail.Item.ID, "", "", "How is the balcony view?", key)
			if err != nil {
				t.Error(err)
				return
			}
			if created {
				fresh.Add(1)
			}
			ids <- post.ID
		})
	}
	wg.Wait()
	close(ids)
	if fresh.Load() != 1 {
		t.Fatal("duplicate retry creation", fresh.Load())
	}
	rootID := ""
	for id := range ids {
		if rootID != "" && rootID != id {
			t.Fatal("duplicate post IDs")
		}
		rootID = id
	}
	root, err := services[0].Thread(t.Context(), "", detail.Item.ID, rootID)
	if err != nil || root.Profile.ID != who.ID {
		t.Fatal(root, err)
	}
	if _, _, err := services[0].Create(t.Context(), who.ID, detail.Item.ID, "", "", "Different payload", key); !errors.Is(err, discussions.ErrConflict) {
		t.Fatal("key reuse", err)
	}
	if _, err := services[0].Edit(t.Context(), other.ID, rootID, "Hijacked"); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatal("ownership edit", err)
	}
	if err := services[0].Remove(t.Context(), other.ID, rootID); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatal("ownership remove", err)
	}
	reply, created, err := services[1].Create(t.Context(), other.ID, detail.Item.ID, rootID, "", "The balcony has a clear view.", accounts.ID())
	if err != nil || !created {
		t.Fatal(reply, err)
	}
	nested, _, err := services[2].Create(t.Context(), who.ID, detail.Item.ID, rootID, reply.ID, "Thank you!", accounts.ID())
	if err != nil || nested.ParentID != reply.ID {
		t.Fatal(nested, err)
	}
	for i := range 10 {
		wg.Go(func() {
			if _, err := services[i%3].React(t.Context(), who.ID, reply.ID, true); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	post, err := pools[0].Discussions().Get(t.Context(), who.ID, reply.ID)
	if err != nil || !post.Helpful || post.HelpfulCount != 1 {
		t.Fatal("Helpful uniqueness", post, err)
	}
	guest, err := pools[0].Discussions().Get(t.Context(), "", reply.ID)
	if err != nil || guest.Helpful || guest.HelpfulCount != 1 {
		t.Fatal("viewer leakage", guest, err)
	}
	for range 2 {
		if _, err := services[0].React(t.Context(), who.ID, reply.ID, false); err != nil {
			t.Fatal(err)
		}
	}
	if err := pools[0].Close(); err != nil {
		t.Fatal(err)
	}
	restarted := f.open(t)
	s := discussions.New(restarted.Discussions(), nil)
	if _, _, err := s.Create(t.Context(), who.ID, detail.Item.ID, "", "", "How is the balcony view?", key); err != nil {
		t.Fatal("offline root retry", err)
	}
	if _, _, err := s.Create(t.Context(), other.ID, detail.Item.ID, rootID, "", "Another offline reply", accounts.ID()); err != nil {
		t.Fatal("offline reply", err)
	}
	edited, err := s.Edit(t.Context(), who.ID, rootID, "How is the BALCONY view?")
	if err != nil || !edited.CreatedAt.Equal(root.CreatedAt) || !edited.UpdatedAt.After(root.UpdatedAt) {
		t.Fatal("offline edit", edited, err)
	}
	for range 2 {
		if err := s.Remove(t.Context(), who.ID, rootID); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := s.Thread(t.Context(), "", detail.Item.ID, rootID)
	if err != nil || !removed.Removed || removed.Body != "" || removed.Profile != nil || removed.ReplyCount != 3 {
		t.Fatal("removed context", removed, err)
	}
	list, err := s.List(t.Context(), "", detail.Item.ID, "", nil)
	if err != nil || list.Count != 0 || len(list.Items) != 1 || !list.Items[0].Removed {
		t.Fatal("placeholder and count", list, err)
	}
	if _, err := s.Edit(t.Context(), who.ID, rootID, "Resurrected"); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatal("resurrected removed post", err)
	}
	if _, err := s.React(t.Context(), other.ID, rootID, true); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatal("reacted to removal", err)
	}
	if _, _, err := s.Create(t.Context(), who.ID, detail.Item.ID, "", "", "How is the balcony view?", key); err != nil {
		t.Fatal("removed retry lost key", err)
	}
	if interest, err := restarted.Interests().Get(t.Context(), who.ID, detail.Item.ID); err != nil || interest.Visibility != "private" {
		t.Fatal("discussion changed interest")
	}
	if _, err := restarted.Saved().Get(t.Context(), who.ID, detail.Item.ID); err != nil {
		t.Fatal("discussion removed save")
	}
	if count, err := restarted.Recommendations().Count(t.Context(), detail.Item.ID); err != nil || count != 0 {
		t.Fatal("discussion implied recommendation")
	}
}

type discussionDetailFixture struct{ detail models.EventDetail }

func (f discussionDetailFixture) Detail(context.Context, string) (models.EventDetail, error) {
	return f.detail, nil
}
func TestMariaDBDiscussionRootAtomicSnapshotAndSchema5Upgrade(t *testing.T) {
	f := newMaria(t)
	base := fstest.MapFS{}
	for _, name := range []string{"00001_event_identity.sql", "00002_accounts.sql", "00003_saved_events.sql", "00004_event_interests.sql", "00005_event_recommendations.sql"} {
		data, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		base[name] = &fstest.MapFile{Data: data}
	}
	if err := migrateFS(t.Context(), f.migration, base); err != nil {
		t.Fatal(err)
	}
	// The previous binary can access schema 5 before its traffic is drained.
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := &Pool{db: db, config: f.migration}
	auth := accounts.New(old.Accounts(), accountProviderFixture{})
	who, token := loginAccount(t, auth, "discussion-upgrade")
	d := savedDetail("DiscussionUpgrade")
	seedLegacySavedEvent(t, db, who.ID, d)
	// Seed using the previous schema's SQL, not the current repository contract.
	execSQL(t, db, `INSERT INTO event_recommendations (account_id,event_id,reason) VALUES (?,?,?)`, who.ID, d.Item.ID, "An existing endorsement")
	f.migrate(t)
	p := f.open(t)
	if a, err := accounts.New(p.Accounts(), nil).Authenticate(t.Context(), token, "session"); err != nil || a.Account.ID != who.ID {
		t.Fatal("upgrade lost account/session", err)
	}
	if item, err := p.Recommendations().Get(t.Context(), who.ID, d.Item.ID); err != nil || item.Reason != "An existing endorsement" {
		t.Fatal("upgrade lost recommendation", err)
	}
	if list, _, err := p.Discussions().List(t.Context(), "", d.Item.ID, "", discussions.Query{Limit: 20, Snapshot: time.Now().Add(time.Hour)}); err != nil || len(list) != 0 {
		t.Fatal("upgrade published existing activity", err)
	}
	newDetail := savedDetail("DiscussionRootAtomic")
	s := discussions.New(p.Discussions(), discussionDetailFixture{newDetail})
	root, _, err := s.Create(t.Context(), who.ID, newDetail.Item.ID, "", "", "A new event question", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot, err := p.Saved().Snapshot(t.Context(), newDetail.Item.ID); err != nil || snapshot.Item.Name != newDetail.Item.Name {
		t.Fatal("root didn't retain complete snapshot", err)
	}
	if list, err := s.List(t.Context(), "", newDetail.Item.ID, "", nil); err != nil || len(list.Items) != 1 || list.Items[0].ID != root.ID {
		t.Fatal("immediate read missed root", list, err)
	}
	failedDetail := savedDetail("DiscussionRootRollback")
	s = discussions.New(p.Discussions(), discussionDetailFixture{failedDetail})
	execSQL(t, f.admin, "REVOKE INSERT ON "+f.runtime.Database+".discussion_posts FROM '"+f.runtime.User+"'@'%'")
	if _, _, err := s.Create(t.Context(), who.ID, failedDetail.Item.ID, "", "", "Must roll back", accounts.ID()); err == nil {
		t.Fatal("missing write privilege accepted")
	}
	if _, err := p.Events().Get(t.Context(), failedDetail.Item.ID); !errors.Is(err, events.ErrNotFound) {
		t.Fatal("failed post leaked event identity", err)
	}
	if _, err := p.Saved().Snapshot(t.Context(), failedDetail.Item.ID); !errors.Is(err, saved.ErrNotFound) {
		t.Fatal("failed post leaked snapshot", err)
	}
}
func TestMariaDBDiscussionPaginationFiltersAndAncestry(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	who, _ := loginAccount(t, auth, "discussion-pages")
	d := savedDetail("DiscussionPages")
	d.Item.Venues[0].City = "Berlin"
	d.Item.Venues[0].CountryCode = "DE"
	if _, _, err := p.Saved().Save(t.Context(), who.ID, d); err != nil {
		t.Fatal(err)
	}
	s := discussions.New(p.Discussions(), saved.New(p.Saved(), nil))
	roots := []discussions.Post{}
	for range 3 {
		post, _, err := s.Create(t.Context(), who.ID, d.Item.ID, "", "", "A useful question", accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		roots = append(roots, post)
	}
	for range 3 {
		if _, _, err := s.Create(t.Context(), who.ID, d.Item.ID, roots[0].ID, "", "A useful answer", accounts.ID()); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Create(t.Context(), who.ID, d.Item.ID, roots[0].ID, roots[1].ID, "Cross-thread", accounts.ID()); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatal("cross-thread parent", err)
	}
	if _, _, err := s.Create(t.Context(), who.ID, "ticketmaster:Other", roots[0].ID, "", "Cross-event", accounts.ID()); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatal("cross-event root", err)
	}
	for _, thread := range []string{"", roots[0].ID} {
		values := url.Values{"limit": {"1"}}
		seen := map[string]bool{}
		var previous discussions.Post
		for {
			list, err := s.List(t.Context(), "", d.Item.ID, thread, values)
			if err != nil || len(list.Items) != 1 || list.Count != 3 {
				t.Fatal(list, err)
			}
			post := list.Items[0]
			if seen[post.ID] {
				t.Fatal("duplicate page")
			}
			seen[post.ID] = true
			if previous.ID != "" && ((thread == "" && post.CreatedAt.After(previous.CreatedAt)) || (thread != "" && post.CreatedAt.Before(previous.CreatedAt))) {
				t.Fatal("wrong chronological order")
			}
			previous = post
			if list.NextCursor == nil {
				break
			}
			values.Set("cursor", *list.NextCursor)
			if _, err := s.List(t.Context(), who.ID, d.Item.ID, thread, values); err == nil {
				t.Fatal("cursor crossed viewer")
			}
			if len(seen) > 3 {
				t.Fatal("pagination loop")
			}
		}
		if len(seen) != 3 {
			t.Fatal("missing posts")
		}
	}
	for _, tc := range []struct {
		city, category string
		count          int
	}{{"berlin", "sports", 3}, {"%", "sports", 0}, {"Berlin", "music", 0}} {
		list, err := s.List(t.Context(), "", "", "", url.Values{"city": {tc.city}, "category_id": {tc.category}})
		if err != nil || list.Count != tc.count {
			t.Fatal(tc, list, err)
		}
	}
}
