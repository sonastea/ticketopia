package persistence

import (
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/discussionfollows"
)

func TestMariaDBInboxQueryCosts(t *testing.T) {
	if os.Getenv("MARIADB_INBOX_PLANS") != "1" {
		t.Skip("set MARIADB_INBOX_PLANS=1 to measure a large inbox")
	}
	p, owner, _, root, _ := discussionReadsFixture(t, 3, 0)
	ctx := t.Context()
	hidden, visible := benchmarkPost(93, 0), benchmarkPost(93, 1)
	execSQL(t, p.db, `INSERT INTO discussion_follows (account_id,thread_id,frequency) VALUES (?,?,'daily')`, owner, root)
	execSQL(t, p.db, `INSERT INTO discussion_notifications (notification_id,account_id,thread_id,batch_key,available_at)
 SELECT LPAD(seq,32,'0'),?,?,LPAD(seq,32,'0'),TIMESTAMPADD(SECOND,seq,?) FROM seq_1_to_10000`, owner, root, benchmarkAt)
	execSQL(t, p.db, `INSERT INTO discussion_notification_posts (notification_id,post_id)
 SELECT notification_id,IF(notification_id>=LPAD(9901,32,'0'),?,?) FROM discussion_notifications`, hidden, visible)
	for _, tc := range []struct {
		name string
		hide string
		want int
	}{
		{"all_visible", "", 21},
		{"100_hidden_at_front", hidden, 21},
		{"all_hidden", visible, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.hide != "" {
				execSQL(t, p.db, `UPDATE discussion_posts SET hidden_at=UTC_TIMESTAMP(6) WHERE post_id=?`, tc.hide)
			}
			q := discussionfollows.Query{Limit: 20}
			query, args := notificationListQuery(owner, q)
			where, _ := notificationListScope(owner, q)
			replyJoin := ` FROM discussion_notification_posts np JOIN discussion_posts p ON p.post_id=np.post_id WHERE np.notification_id=n.notification_id AND p.hidden_at IS NULL AND p.removed_at IS NULL`
			old := `SELECT n.notification_id,root.event_id,n.thread_id,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(e.snapshot,'$.name')),''),n.available_at,n.read_at IS NOT NULL,(SELECT COUNT(*)` + replyJoin + `),(SELECT p.post_id` + replyJoin + ` ORDER BY p.created_at DESC,p.post_id DESC LIMIT 1)` + notificationJoin + where + ` ORDER BY n.available_at DESC,n.notification_id DESC LIMIT ?`
			var want []discussionfollows.Notification
			for i, statement := range []string{old, query} {
				var items []discussionfollows.Notification
				err := queryRowsTx(ctx, p.db, statement, args, func(row *sql.Rows) error {
					n := discussionfollows.Notification{Kind: discussionfollows.KindReplies}
					err := row.Scan(&n.ID, &n.EventID, &n.ThreadID, &n.EventName, &n.AvailableAt, &n.Read, &n.ReplyCount, &n.PostID)
					items = append(items, n)
					return err
				})
				if err != nil || len(items) != tc.want {
					t.Fatalf("variant %d got %d items: %v", i, len(items), err)
				}
				if i == 0 {
					want = items
				} else if !reflect.DeepEqual(items, want) {
					t.Fatal("notification fields or order changed")
				}
				var times []float64
				var raw []byte
				for j := range 12 {
					if err := p.db.QueryRowContext(ctx, `ANALYZE FORMAT=JSON `+statement, args...).Scan(&raw); err != nil {
						t.Fatal(err)
					}
					var plan struct {
						Block struct {
							MS float64 `json:"r_total_time_ms"`
						} `json:"query_block"`
					}
					if err := json.Unmarshal(raw, &plan); err != nil {
						t.Fatal(err)
					}
					if j > 0 {
						times = append(times, plan.Block.MS)
					}
				}
				slices.Sort(times)
				t.Logf("variant %d median %.3f ms\n%s", i, times[len(times)/2], raw)
			}
			if len(want) > q.Limit {
				last := want[q.Limit-1]
				later, err := p.DiscussionFollows().Notifications(ctx, owner, discussionfollows.Query{
					Limit: q.Limit, Cursor: &discussionfollows.Cursor{At: last.AvailableAt, ID: last.ID},
				})
				if err != nil || len(later) != q.Limit+1 || !reflect.DeepEqual(later[0], want[q.Limit]) {
					t.Fatalf("continuation did not start at the lookahead: %v", err)
				}
			}
			var samples []time.Duration
			for i := range 22 {
				start := time.Now()
				list, err := p.DiscussionFollows().Notifications(ctx, owner, discussionfollows.Query{Limit: 20})
				elapsed := time.Since(start)
				if err != nil || len(list) != tc.want {
					t.Fatalf("got %d envelopes, want %d: %v", len(list), tc.want, err)
				}
				for _, n := range list {
					if n.ReplyCount != 1 || tc.hide != "" && n.PostID != visible {
						t.Fatalf("unexpected reply for %q: post=%q count=%d, visible target=%q", n.ID, n.PostID, n.ReplyCount, visible)
					}
				}
				if i > 0 {
					samples = append(samples, elapsed)
				}
			}
			slices.Sort(samples)
			t.Logf("median=%s p95=%s", samples[10], samples[19])
		})
	}
}
