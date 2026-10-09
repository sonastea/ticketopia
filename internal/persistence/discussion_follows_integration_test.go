package persistence

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/internal/discussions"
)

func TestMariaDBDiscussionFollowsDeliveryPrivacyAndFrequency(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	author, _ := loginAccount(t, auth, "follow-author")
	follower, _ := loginAccount(t, auth, "follow-reader")
	other, _ := loginAccount(t, auth, "follow-other")
	if _, err := auth.UpdatePreferences(t.Context(), follower.ID, accounts.PreferencesPatch{NotificationsPaused: pointer(false)}); err != nil {
		t.Fatal(err)
	}
	detail := savedDetail("DiscussionFollows")
	posts := discussions.New(p.Discussions(), discussionDetailFixture{detail})
	s := discussionfollows.New(p.DiscussionFollows())
	root, _, err := posts.Create(t.Context(), author.ID, detail.Item.ID, "", "", "Where is the accessible entrance?", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = s.Set(t.Context(), follower.ID, root.EventID, root.ID, "immediate"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.Set(t.Context(), other.ID, root.EventID, root.ID, "immediate"); err != nil {
		t.Fatal(err)
	} // paused by default
	if _, err = s.Set(t.Context(), follower.ID, "ticketmaster:WrongEvent", root.ID, "daily"); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatal("event mismatch", err)
	}
	key := accounts.ID()
	var wg sync.WaitGroup
	replyIDs := make(chan string, 8)
	for range 8 {
		wg.Go(func() {
			post, _, err := posts.Create(t.Context(), author.ID, root.EventID, root.ID, "", "SECRET REPLY TEXT", key)
			if err != nil {
				t.Error(err)
				return
			}
			replyIDs <- post.ID
		})
	}
	wg.Wait()
	close(replyIDs)
	reply := ""
	for id := range replyIDs {
		if reply != "" && reply != id {
			t.Fatal("duplicate reply")
		}
		reply = id
	}
	inbox, err := s.Notifications(t.Context(), follower.ID, nil)
	if err != nil || len(inbox.Items) != 1 || inbox.Items[0].PostID != reply {
		t.Fatal("atomic exactly once notification", inbox, err)
	}
	data, _ := json.Marshal(inbox)
	if strings.Contains(string(data), "SECRET") || strings.Contains(string(data), author.ID) {
		t.Fatal("notification copied content or author", string(data))
	}
	id := inbox.Items[0].ID
	if err = s.Read(t.Context(), other.ID, id); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatal("cross-owner read", err)
	}
	for range 2 {
		if err = s.Read(t.Context(), follower.ID, id); err != nil {
			t.Fatal(err)
		}
	}
	// Own replies and paused accounts do not generate notifications.
	if _, _, err = posts.Create(t.Context(), follower.ID, root.EventID, root.ID, "", "Thanks", accounts.ID()); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Notifications(t.Context(), follower.ID, nil); err != nil || len(got.Items) != 1 || !got.Items[0].Read {
		t.Fatal(got, err)
	}
	if got, err := s.Notifications(t.Context(), other.ID, nil); err != nil || len(got.Items) != 0 {
		t.Fatal("paused delivered", got, err)
	}
	// Hiding/removal AFTER enqueue suppresses payloads and read actions. This
	// fixture changes public visibility only; no report evidence is consulted.
	execSQL(t, p.db, `UPDATE discussion_posts SET hidden_at=UTC_TIMESTAMP(6) WHERE post_id=?`, reply)
	if got, err := s.Notifications(t.Context(), follower.ID, nil); err != nil || len(got.Items) != 0 {
		t.Fatal("hidden reply delivered", got, err)
	}
	if err = s.Read(t.Context(), follower.ID, id); !errors.Is(err, discussions.ErrNotFound) {
		t.Fatal("hidden read", err)
	}
	execSQL(t, p.db, `UPDATE discussion_posts SET hidden_at=NULL WHERE post_id=?`, reply)
	if err = posts.Remove(t.Context(), author.ID, reply); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Notifications(t.Context(), follower.ID, nil); err != nil || len(got.Items) != 0 {
		t.Fatal("removed reply delivered", got, err)
	}
	if _, err = s.Set(t.Context(), follower.ID, root.EventID, root.ID, "daily"); err != nil {
		t.Fatal(err)
	}
	var newest string
	for range 3 {
		post, _, err := posts.Create(t.Context(), author.ID, root.EventID, root.ID, "", "Daily reply", accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		newest = post.ID
	}
	if got, err := s.Notifications(t.Context(), follower.ID, nil); err != nil || len(got.Items) != 0 {
		t.Fatal("digest before due", got, err)
	}
	var batches, links int
	if err = p.db.QueryRow(`SELECT COUNT(*) FROM discussion_notifications WHERE account_id=? AND read_at IS NULL`, follower.ID).Scan(&batches); err != nil || batches != 1 {
		t.Fatal("not coalesced", batches, err)
	}
	if err = p.db.QueryRow(`SELECT COUNT(*) FROM discussion_notification_posts np JOIN discussion_notifications n ON n.notification_id=np.notification_id WHERE n.account_id=? AND n.read_at IS NULL`, follower.ID).Scan(&links); err != nil || links != 3 {
		t.Fatal("missing digest replies", links, err)
	}
	execSQL(t, p.db, `UPDATE discussion_notifications SET available_at=UTC_TIMESTAMP(6) WHERE account_id=? AND read_at IS NULL`, follower.ID)
	inbox, err = s.Notifications(t.Context(), follower.ID, nil)
	if err != nil || len(inbox.Items) != 1 || inbox.Items[0].ReplyCount != 3 || inbox.Items[0].PostID != newest {
		t.Fatal("digest delivery", inbox, err)
	}
	// Hidden latest reply selects a different visible reply, never its body.
	execSQL(t, p.db, `UPDATE discussion_posts SET hidden_at=UTC_TIMESTAMP(6) WHERE post_id=?`, newest)
	inbox, err = s.Notifications(t.Context(), follower.ID, nil)
	if err != nil || len(inbox.Items) != 1 || inbox.Items[0].ReplyCount != 2 || inbox.Items[0].PostID == newest {
		t.Fatal("digest privacy", inbox, err)
	}
	if _, err = s.Set(t.Context(), follower.ID, root.EventID, root.ID, "weekly"); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, _, err = posts.Create(t.Context(), author.ID, root.EventID, root.ID, "", "Weekly reply", accounts.ID()); err != nil {
			t.Fatal(err)
		}
	}
	if err = p.db.QueryRow(`SELECT COUNT(*) FROM discussion_notifications WHERE account_id=? AND read_at IS NULL`, follower.ID).Scan(&batches); err != nil || batches != 1 {
		t.Fatal("weekly coalescing", batches, err)
	}
	if _, err = s.Set(t.Context(), follower.ID, root.EventID, root.ID, "muted"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = posts.Create(t.Context(), author.ID, root.EventID, root.ID, "", "Muted reply", accounts.ID()); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Notifications(t.Context(), follower.ID, nil); err != nil || len(got.Items) != 0 {
		t.Fatal("mute delivered", got, err)
	}
	if _, err = s.Set(t.Context(), follower.ID, root.EventID, root.ID, "immediate"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = posts.Create(t.Context(), author.ID, root.EventID, root.ID, "", "Another reply", accounts.ID()); err != nil {
		t.Fatal(err)
	}
	execSQL(t, p.db, `UPDATE discussion_posts SET hidden_at=UTC_TIMESTAMP(6) WHERE post_id=?`, root.ID)
	if got, err := s.Notifications(t.Context(), follower.ID, nil); err != nil || len(got.Items) != 0 {
		t.Fatal("hidden root delivered", got, err)
	}
	if got, err := s.List(t.Context(), follower.ID, nil); err != nil || len(got.Items) != 1 || got.Items[0].Root.Body != "" || got.Items[0].Root.Profile != nil {
		t.Fatal("hidden follow leaked", got, err)
	}
	execSQL(t, p.db, `UPDATE discussion_posts SET hidden_at=NULL WHERE post_id=?`, root.ID)
	if _, err = auth.UpdatePreferences(t.Context(), follower.ID, accounts.PreferencesPatch{NotificationsPaused: pointer(true)}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Notifications(t.Context(), follower.ID, nil); err != nil || len(got.Items) != 0 {
		t.Fatal("account pause delivered", got, err)
	}
	if _, _, err = posts.Create(t.Context(), author.ID, root.EventID, root.ID, "", "During pause", accounts.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err = auth.UpdatePreferences(t.Context(), follower.ID, accounts.PreferencesPatch{NotificationsPaused: pointer(false)}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Notifications(t.Context(), follower.ID, nil); err != nil || len(got.Items) != 1 {
		t.Fatal("pause backlog", got, err)
	}
	// Fresh pool proves durable follows/notifications; no provider reads needed.
	restarted := discussionfollows.New(f.open(t).DiscussionFollows())
	if got, err := restarted.Notifications(t.Context(), follower.ID, url.Values{"limit": {"1"}}); err != nil || len(got.Items) != 1 {
		t.Fatal("restart", got, err)
	}
	for range 2 {
		if err = s.Remove(t.Context(), follower.ID, root.EventID, root.ID); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := s.Notifications(t.Context(), follower.ID, nil); err != nil || len(got.Items) != 0 {
		t.Fatal("unfollow backlog", got, err)
	}
	if got, err := s.List(t.Context(), follower.ID, nil); err != nil || len(got.Items) != 0 {
		t.Fatal("unfollow retained", got, err)
	}
	if _, err = s.Set(t.Context(), follower.ID, root.EventID, root.ID, "immediate"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Notifications(t.Context(), follower.ID, nil); err != nil || len(got.Items) != 0 {
		t.Fatal("refollow replayed", got, err)
	}
}

func TestMariaDBDiscussionFollowUpgradeRollbackAndPagination(t *testing.T) {
	f := newMaria(t)
	// Model the drained schema-11 binary and preserve its existing account/post.
	base := fstest.MapFS{}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "00012_") {
			continue
		}
		data, err := migrations.ReadFile("migrations/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		base[entry.Name()] = &fstest.MapFile{Data: data}
	}
	if err = migrateFS(t.Context(), f.migration, base); err != nil {
		t.Fatal(err)
	}
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := &Pool{db: db, config: f.migration}
	oldAuth := accounts.New(old.Accounts(), accountProviderFixture{})
	reader, raw := loginAccount(t, oldAuth, "follow-upgrade-reader")
	detail := savedDetail("FollowUpgrade")
	seedLegacySavedEvent(t, db, reader.ID, detail)
	id := accounts.ID()
	execSQL(t, db, `INSERT INTO discussion_posts (post_id,account_id,event_id,body,idempotency_key,request_hash) VALUES (?,?,?,?,?,?)`, id, reader.ID, detail.Item.ID, "Retained question", accounts.ID(), make([]byte, 32))
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	if _, err = auth.Authenticate(t.Context(), raw, "session"); err != nil {
		t.Fatal("upgrade lost credential", err)
	}
	if _, err = auth.UpdatePreferences(t.Context(), reader.ID, accounts.PreferencesPatch{NotificationsPaused: pointer(false)}); err != nil {
		t.Fatal(err)
	}
	writer, _ := loginAccount(t, auth, "follow-upgrade-writer")
	s := discussionfollows.New(p.DiscussionFollows())
	posts := discussions.New(p.Discussions(), discussionDetailFixture{detail})
	if _, err = s.Set(t.Context(), reader.ID, detail.Item.ID, id, "immediate"); err != nil {
		t.Fatal(err)
	}
	// Revoking only queue insertion makes reply publication roll back too.
	grant := fmt.Sprintf("%s.discussion_notifications", f.runtime.Database)
	execSQL(t, f.admin, "REVOKE INSERT ON "+grant+" FROM '"+f.runtime.User+"'@'%'")
	key := accounts.ID()
	_, _, err = posts.Create(t.Context(), writer.ID, detail.Item.ID, id, "", "Atomic reply", key)
	execSQL(t, f.admin, "GRANT INSERT ON "+grant+" TO '"+f.runtime.User+"'@'%'")
	if err == nil {
		t.Fatal("queue failure committed reply")
	}
	var count int
	if err = p.db.QueryRow(`SELECT COUNT(*) FROM discussion_posts WHERE idempotency_key=?`, key).Scan(&count); err != nil || count != 0 {
		t.Fatal("reply survived rollback", count, err)
	}
	if _, created, err := posts.Create(t.Context(), writer.ID, detail.Item.ID, id, "", "Atomic reply", key); err != nil || !created {
		t.Fatal("retry after rollback", created, err)
	}
	for range 4 {
		if _, _, err = posts.Create(t.Context(), writer.ID, detail.Item.ID, id, "", "Page reply", accounts.ID()); err != nil {
			t.Fatal(err)
		}
	}
	seen := map[string]bool{}
	values := url.Values{"limit": {"2"}}
	for {
		page, err := s.Notifications(t.Context(), reader.ID, values)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range page.Items {
			if seen[n.ID] {
				t.Fatal("duplicate page item")
			}
			seen[n.ID] = true
		}
		if page.NextCursor == nil {
			break
		}
		values.Set("cursor", *page.NextCursor)
	}
	if len(seen) != 5 {
		t.Fatal("missing notifications", len(seen))
	}
	for range 3 {
		root, _, err := posts.Create(t.Context(), writer.ID, detail.Item.ID, "", "", "Another question", accounts.ID())
		if err != nil {
			t.Fatal(err)
		}
		if _, err = s.Set(t.Context(), reader.ID, root.EventID, root.ID, "muted"); err != nil {
			t.Fatal(err)
		}
	}
	values = url.Values{"limit": {"2"}}
	seen = map[string]bool{}
	for {
		page, err := s.List(t.Context(), reader.ID, values)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range page.Items {
			if seen[f.ThreadID] {
				t.Fatal("duplicate follow")
			}
			seen[f.ThreadID] = true
		}
		if page.NextCursor == nil {
			break
		}
		values.Set("cursor", *page.NextCursor)
	}
	if len(seen) != 4 {
		t.Fatal("missing follows", len(seen))
	}
}
