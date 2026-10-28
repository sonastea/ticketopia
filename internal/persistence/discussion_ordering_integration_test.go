package persistence

import (
	"database/sql"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/moderation"
)

// Freeze only this fixture's single runtime connection, never the server clock.
func freezeSessionClock(t *testing.T, p *Pool, at time.Time) {
	t.Helper()
	p.db.SetMaxOpenConns(1)
	p.db.SetMaxIdleConns(1)
	execSQL(t, p.db, `SET timestamp=?`, at.Unix())
}

func TestMariaDBModerationLatestDecisionSurvivesClockRollback(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	author, _ := loginAccount(t, auth, "decision-author")
	mod, _ := loginAccount(t, auth, "decision-moderator")
	operator, err := Open(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer operator.Close()
	if _, err = operator.SetModerator(t.Context(), mod.ID, "ordering fixture", true); err != nil {
		t.Fatal(err)
	}

	base := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	freezeSessionClock(t, p, base)
	detail := savedDetail("DecisionClock")
	posts := discussions.New(p.Discussions(), discussionDetailFixture{detail})
	root, _, err := posts.Create(t.Context(), author.ID, detail.Item.ID, "", "", "Review this question", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	s := moderation.New(p.Moderation())
	decide := func(action string, at time.Time) moderation.Outcome {
		t.Helper()
		freezeSessionClock(t, p, at)
		c, err := s.Case(t.Context(), mod.ID, root.ID, nil)
		if err != nil {
			t.Fatal(err)
		}
		o, err := s.Decide(t.Context(), mod.ID, root.ID, moderation.Review{
			Action: action, Reason: "Reviewed " + action,
			ExpectedVersion: &c.Version, ExpectedUpdatedAt: c.Post.UpdatedAt,
		})
		if err != nil {
			t.Fatal(err)
		}
		return o
	}
	decide("hide", base.Add(time.Second))
	restored := decide("restore", base.Add(3*time.Second))
	// A database-host wall-clock correction steps time back, but the case
	// version and all three decisions still advance in commit order. App-host
	// clock skew or NTP slewing alone does not trigger this rare scenario.
	latest := decide("hide", base.Add(2*time.Second))
	if !latest.CreatedAt.After(restored.CreatedAt) {
		t.Errorf("decision order regressed with the database clock: latest=%v restore=%v", latest.CreatedAt, restored.CreatedAt)
	}
	post, err := posts.Post(t.Context(), author.ID, root.ID)
	if err != nil || !post.Hidden {
		t.Fatalf("last committed decision did not hide the post: hidden=%v err=%v", post.Hidden, err)
	}
	if !latest.CanAppeal {
		t.Error("newest committed hide has can_appeal=false because an older restore has a later wall-clock timestamp")
	}
	if err = s.Appeal(t.Context(), author.ID, latest.ID, "Please review the latest hide"); err != nil {
		t.Errorf("appeal of newest committed hide rejected: %v", err)
	}
}

func TestMariaDBLateDigestReplyStaysUnread(t *testing.T) {
	for _, frequency := range []string{"daily", "weekly"} {
		t.Run(frequency, func(t *testing.T) {
			f := newMaria(t)
			f.migrate(t)
			writer, reader := f.open(t), f.open(t)
			auth := accounts.New(writer.Accounts(), accountProviderFixture{})
			author, _ := loginAccount(t, auth, "digest-author")
			follower, _ := loginAccount(t, auth, "digest-follower")
			if _, err := auth.UpdatePreferences(t.Context(), follower.ID, accounts.PreferencesPatch{NotificationsPaused: pointer(false)}); err != nil {
				t.Fatal(err)
			}
			now, err := reader.Discussions().Snapshot(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			boundary := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
			if frequency == "weekly" {
				boundary = boundary.AddDate(0, 0, -(int(boundary.Weekday())+6)%7)
			}
			freezeSessionClock(t, reader, boundary.Add(-2*time.Second))
			freezeSessionClock(t, writer, boundary.Add(-2*time.Second))
			detail := savedDetail("LateDigest")
			posts := discussions.New(writer.Discussions(), discussionDetailFixture{detail})
			root, _, err := posts.Create(t.Context(), author.ID, detail.Item.ID, "", "", "Question", accounts.ID())
			if err != nil {
				t.Fatal(err)
			}
			if _, err = writer.DiscussionFollows().Set(t.Context(), follower.ID, root.EventID, root.ID, frequency); err != nil {
				t.Fatal(err)
			}
			freezeSessionClock(t, writer, boundary.Add(-time.Second))
			if _, _, err = posts.Create(t.Context(), author.ID, root.EventID, root.ID, "", "First reply", accounts.ID()); err != nil {
				t.Fatal(err)
			}

			// Reproduce createOnce's transaction up to the point just before
			// enqueueDiscussionReply. A scheduler/DB delay can carry this
			// uncommitted reply across midnight while the reader opens a digest
			// containing only the previously committed reply.
			tx, err := writer.db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			var locked string
			if err = tx.QueryRowContext(t.Context(), `SELECT account_id FROM accounts WHERE account_id=? FOR UPDATE`, author.ID).Scan(&locked); err != nil {
				t.Fatal(err)
			}
			if err = tx.QueryRowContext(t.Context(), `SELECT post_id FROM discussion_posts WHERE post_id=? FOR UPDATE`, root.ID).Scan(&locked); err != nil {
				t.Fatal(err)
			}
			id := accounts.ID()
			if _, err = tx.ExecContext(t.Context(), `INSERT INTO discussion_posts (post_id,account_id,event_id,root_id,body,idempotency_key,request_hash) VALUES (?,?,?,?,?,?,?)`, id, author.ID, root.EventID, root.ID, "Late committed reply", accounts.ID(), make([]byte, 32)); err != nil {
				t.Fatal(err)
			}
			late, err := readPost(t.Context(), tx, author.ID, id)
			if err != nil {
				t.Fatal(err)
			}
			if !discussionfollows.DeliveryAt(frequency, late.CreatedAt).Equal(boundary) {
				t.Fatal("late reply did not belong to the closing digest window")
			}
			// Both connections advance normally across the same one-second
			// midnight boundary; this does not require clock skew or rollback.
			if _, err = tx.ExecContext(t.Context(), `SET timestamp=?`, boundary.Unix()); err != nil {
				t.Fatal(err)
			}
			freezeSessionClock(t, reader, boundary)
			inbox := discussionfollows.New(reader.DiscussionFollows())
			before, err := inbox.Notifications(t.Context(), follower.ID, nil)
			if err != nil || len(before.Items) != 1 || before.Items[0].ReplyCount != 1 || before.Items[0].Read {
				t.Fatalf("expected one unread digest containing only the first reply: %+v %v", before, err)
			}
			if err = inbox.Read(t.Context(), follower.ID, before.Items[0].ID); err != nil {
				t.Fatal(err)
			}
			if err = enqueueDiscussionReply(t.Context(), tx, late, author.ID); err != nil {
				t.Fatal(err)
			}
			if err = tx.Commit(); err != nil {
				t.Fatal(err)
			}
			after, err := inbox.Notifications(t.Context(), follower.ID, nil)
			if err != nil {
				t.Fatal(err)
			}
			unread := 0
			for _, n := range after.Items {
				if !n.Read {
					unread += n.ReplyCount
				}
			}
			if unread == 0 {
				t.Fatalf("newly committed reply was silently added to an already-read digest: notifications=%d reply_count=%d read=%v", len(after.Items), after.Items[0].ReplyCount, after.Items[0].Read)
			}
		})
	}
}
