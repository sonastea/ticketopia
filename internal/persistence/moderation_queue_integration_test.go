package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/moderation"
)

// discussionReadsFixture seeds n reported posts (some hidden or removed), replies,
// recommendations and Helpful marks on one shared event, as on an event/thread
// page; account n is the moderator and authors the one excluded case.
func discussionReadsFixture(t testing.TB, n, infoBytes int) (*Pool, string, string, string, int) {
	t.Helper()
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	for i := range n + 1 {
		execSQL(t, p.db, `INSERT INTO accounts (account_id,email,display_name) VALUES (?,?,?)`, benchmarkOwner(i), fmt.Sprintf("discussion-reads-%d@example.test", i), fmt.Sprintf("Author %d", i))
	}
	who := benchmarkOwner(n)
	e := benchmarkEvent(0)
	e.Info = strings.Repeat("x", infoBytes)
	d := models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: benchmarkAt}}
	if err := p.Discovery().Observe(t.Context(), d); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	root := benchmarkPost(91, 0)
	for i := range n + 1 {
		id := benchmarkPost(91, i)
		var hidden, removed any
		if i%7 == 1 {
			hidden = benchmarkAt
		}
		if i%11 == 2 {
			removed = benchmarkAt
		}
		execSQL(t, p.db, `INSERT INTO discussion_posts (post_id,account_id,event_id,body,idempotency_key,request_hash,created_at,updated_at,hidden_at,removed_at) VALUES (?,?,?,'Public body',?,?,?,?,?,?)`, id, benchmarkOwner(i), e.ID, id, make([]byte, 32), benchmarkAt, benchmarkAt, hidden, removed)
		execSQL(t, p.db, `INSERT INTO moderation_reports (report_id,post_id,reporter_id,reason,context,reported_body) VALUES (?,?,?,'spam','','Private reported body')`, benchmarkPost(92, i), id, benchmarkOwner((i+1)%(n+1)))
		if i == n { // A moderator must not see their own case, even at a page edge.
			continue
		}
		reply := benchmarkPost(93, i)
		execSQL(t, p.db, `INSERT INTO discussion_posts (post_id,account_id,event_id,root_id,parent_id,body,idempotency_key,request_hash,created_at,updated_at) VALUES (?,?,?,?,?,'Reply',?,?,?,?)`, reply, benchmarkOwner(i), e.ID, root, root, reply, make([]byte, 32), benchmarkAt, benchmarkAt)
		execSQL(t, p.db, `INSERT INTO event_recommendations (account_id,event_id,reason,recommended_at,updated_at) VALUES (?,?,'Recommendation',?,?)`, benchmarkOwner(i), e.ID, benchmarkAt, benchmarkAt)
		execSQL(t, p.db, `INSERT INTO post_helpful (account_id,post_id) VALUES (?,?)`, who, id)
	}
	return p, who, e.ID, root, len(data)
}

// The queue reads its page keys, then every post projection in one statement.
// The budget is a statement count, not a timing gate.
func TestMariaDBModerationQueueStatementsAreBounded(t *testing.T) {
	p, who, _, _, _ := discussionReadsFixture(t, 105, 1600)
	for _, tc := range []struct {
		name string
		q    moderation.Query
		want int
	}{
		{"empty", moderation.Query{Limit: 20, State: "pending", At: benchmarkAt.Add(-time.Hour), After: benchmarkPost(91, 0)}, 0},
		{"minimum", moderation.Query{Limit: 1, State: "pending"}, 2},
		{"default", moderation.Query{Limit: 20, State: "pending"}, 21},
		{"maximum", moderation.Query{Limit: 100, State: "all"}, 101},
		{"tied_cursor", moderation.Query{Limit: 20, State: "pending", At: benchmarkAt, After: benchmarkPost(91, 70)}, 21},
	} {
		t.Run(tc.name, func(t *testing.T) {
			count := statementCounter(t, p)
			items, err := p.Moderation().Queue(t.Context(), who, tc.q)
			if err != nil || len(items) != tc.want {
				t.Fatalf("queue: rows=%d want=%d err=%v", len(items), tc.want, err)
			}
			got := count()
			t.Logf("rows=%d statements=%d", len(items), got)
			for i, item := range items {
				if item.Post.Profile != nil && item.Post.Profile.ID == who {
					t.Fatal("own case exposed")
				}
				if item.Post.ID == benchmarkPost(91, 105) || item.PendingReports != 1 || item.PendingAppeals != 0 {
					t.Fatal("queue filtering/counts changed")
				}
				if i > 0 && items[i-1].Post.ID <= item.Post.ID {
					t.Fatal("tied timestamp order changed")
				}
				if tc.q.After != "" && item.Post.ID >= tc.q.After {
					t.Fatal("cursor was not exclusive")
				}
				// Preserve the authoritative redacted projection, including profile,
				// Helpful, parent labels, replies, and last-known event metadata.
				want, err := readPost(t.Context(), p.db, "", item.Post.ID)
				if err != nil || !reflect.DeepEqual(item.Post, want) {
					t.Fatalf("post projection differs: %v", err)
				}
			}
			if got > 2 {
				t.Errorf("queue ran %d statements for %d rows; want at most 2", got, len(items))
			}
		})
	}
	t.Run("canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		items, err := p.Moderation().Queue(ctx, who, moderation.Query{Limit: 20, State: "pending"})
		if err == nil || len(items) != 0 {
			t.Fatalf("canceled queue: rows=%d err=%v", len(items), err)
		}
	})
}
