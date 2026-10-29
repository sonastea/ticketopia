package persistence

import (
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/internal/discussions"
)

// statementCounter pins the pool to one session and counts parameterized
// statement executions (every repository statement here takes arguments).
// The probes themselves are argument-free COM_QUERY calls and are not counted.
func statementCounter(t testing.TB, p *Pool) func() int {
	t.Helper()
	p.db.SetMaxOpenConns(1)
	p.db.SetMaxIdleConns(1)
	read := func() (id int64, executed int) {
		if err := p.db.QueryRowContext(t.Context(), `SELECT CONNECTION_ID()`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		var name string
		if err := p.db.QueryRowContext(t.Context(), `SHOW SESSION STATUS LIKE 'Com_stmt_execute'`).Scan(&name, &executed); err != nil {
			t.Fatal(err)
		}
		return id, executed
	}
	session, before := read()
	return func() int {
		t.Helper()
		now, after := read()
		if now != session {
			t.Fatal("statement count spans database sessions")
		}
		delta := after - before
		session, before = now, after
		return delta
	}
}

func fanoutFrequency(i int) string {
	return []string{"immediate", "daily", "weekly", "muted"}[i%4]
}

// seedFollowers inserts n accounts that follow thread; every tenth is paused.
func seedFollowers(t testing.TB, p *Pool, thread, prefix string, n int) []string {
	t.Helper()
	ids := make([]string, n)
	for start := 0; start < n; start += 500 {
		end := min(start+500, n)
		var accountRows, followRows []string
		var accountArgs, followArgs []any
		for i := start; i < end; i++ {
			ids[i] = fmt.Sprintf("%s%0*d", prefix, 32-len(prefix), i)
			accountRows = append(accountRows, `(?,?,'Berlin','DE','["music"]',?)`)
			accountArgs = append(accountArgs, ids[i], fmt.Sprintf("%s-%d@example.test", prefix, i), i%10 == 9)
			followRows = append(followRows, `(?,?,?)`)
			followArgs = append(followArgs, ids[i], thread, fanoutFrequency(i))
		}
		execSQL(t, p.db, `INSERT INTO accounts (account_id,email,city,country,category_ids,notifications_paused) VALUES `+strings.Join(accountRows, ","), accountArgs...)
		execSQL(t, p.db, `INSERT INTO discussion_follows (account_id,thread_id,frequency) VALUES `+strings.Join(followRows, ","), followArgs...)
	}
	return ids
}

func TestMariaDBDiscussionReplyFanoutIsBatched(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	// One frozen session keeps both replies in the same digest windows.
	freezeSessionClock(t, p, time.Now().UTC().Truncate(24*time.Hour).Add(12*time.Hour))
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	author, _ := loginAccount(t, auth, "fanout-author")
	detail := savedDetail("Fanout")
	posts := discussions.New(p.Discussions(), discussionDetailFixture{detail})
	root, _, err := posts.Create(t.Context(), author.ID, detail.Item.ID, "", "", "Who is going?", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	const followers = 1000
	ids := seedFollowers(t, p, root.ID, "fanout", followers)
	execSQL(t, p.db, `INSERT INTO discussion_follows (account_id,thread_id,frequency) VALUES (?,?,'immediate')`, author.ID, root.ID)

	if _, _, err = posts.Create(t.Context(), author.ID, root.EventID, root.ID, "", "First reply", accounts.ID()); err != nil {
		t.Fatal(err)
	}
	// Read digests must reopen when the second reply joins the same window.
	execSQL(t, p.db, `UPDATE discussion_notifications SET read_at=UTC_TIMESTAMP(6)`)
	count := statementCounter(t, p)
	second, _, err := posts.Create(t.Context(), author.ID, root.EventID, root.ID, "", "Second reply", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	// Publication: service retry lookup, then account lock, retry lookup, root
	// lock, insert and read back. Fan-out: one recipient read plus two writes
	// per recipient chunk, independent of how many followers each chunk holds.
	limit := 6 + 1 + 2*((followers+enqueueChunk-1)/enqueueChunk)
	if got := count(); got > limit {
		t.Fatalf("reply to %d followers ran %d statements, want at most %d", followers, got, limit)
	}

	type delivery struct{ envelopes, links, unread int }
	got := map[string]delivery{}
	rows, err := p.db.QueryContext(t.Context(), `SELECT n.account_id,COUNT(DISTINCT n.notification_id),COUNT(np.post_id),COUNT(DISTINCT CASE WHEN n.read_at IS NULL THEN n.notification_id END) FROM discussion_notifications n LEFT JOIN discussion_notification_posts np ON np.notification_id=n.notification_id WHERE n.thread_id=? GROUP BY n.account_id`, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var who string
		var d delivery
		if err = rows.Scan(&who, &d.envelopes, &d.links, &d.unread); err != nil {
			t.Fatal(err)
		}
		got[who] = d
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	if _, ok := got[author.ID]; ok {
		t.Fatal("author notified of own reply")
	}
	for i, who := range ids {
		var want delivery
		switch frequency := fanoutFrequency(i); {
		case i%10 == 9 || frequency == "muted":
		case frequency == "immediate":
			want = delivery{envelopes: 2, links: 2, unread: 1}
		default:
			want = delivery{envelopes: 1, links: 2, unread: 1}
		}
		if got[who] != want {
			t.Fatalf("follower %d (%s, paused=%v): got %+v, want %+v", i, fanoutFrequency(i), i%10 == 9, got[who], want)
		}
	}
	var links int
	if err = p.db.QueryRow(`SELECT COUNT(*) FROM discussion_notification_posts WHERE post_id=?`, second.ID).Scan(&links); err != nil {
		t.Fatal(err)
	}
	if want := len(got); links != want {
		t.Fatalf("second reply linked %d times, want once per recipient (%d)", links, want)
	}
}

func TestMariaDBFollowedDiscussionsPageIsOneQuery(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	author, _ := loginAccount(t, auth, "followed-page-author")
	reader, _ := loginAccount(t, auth, "followed-page-reader")
	detail := savedDetail("FollowedPage")
	posts := discussions.New(p.Discussions(), discussionDetailFixture{detail})
	first, _, err := posts.Create(t.Context(), author.ID, detail.Item.ID, "", "", "Question 0", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	const threads = 100
	roots := []string{first.ID}
	for i := 1; i < threads; i++ {
		id := accounts.ID()
		execSQL(t, p.db, `INSERT INTO discussion_posts (post_id,account_id,event_id,body,idempotency_key,request_hash) VALUES (?,?,?,?,?,?)`, id, author.ID, detail.Item.ID, fmt.Sprintf("Question %d", i), accounts.ID(), make([]byte, 32))
		roots = append(roots, id)
	}
	for i, id := range roots {
		execSQL(t, p.db, `INSERT INTO discussion_follows (account_id,thread_id,frequency,created_at) VALUES (?,?,?,?)`, reader.ID, id, fanoutFrequency(i), time.Date(2026, 10, 1, 0, 0, i/3, 0, time.UTC))
	}
	reply, _, err := posts.Create(t.Context(), author.ID, detail.Item.ID, roots[1], "", "A reply", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	execSQL(t, p.db, `INSERT INTO post_helpful (account_id,post_id) VALUES (?,?),(?,?)`, reader.ID, roots[2], author.ID, roots[2])
	execSQL(t, p.db, `UPDATE discussion_posts SET hidden_at=UTC_TIMESTAMP(6) WHERE post_id=?`, roots[3])
	execSQL(t, p.db, `UPDATE discussion_posts SET removed_at=UTC_TIMESTAMP(6) WHERE post_id=?`, roots[4])
	execSQL(t, p.db, `UPDATE discussion_posts SET hidden_at=UTC_TIMESTAMP(6) WHERE post_id=?`, reply.ID)

	r := p.DiscussionFollows()
	count := statementCounter(t, p)
	items, err := r.List(t.Context(), reader.ID, discussionfollows.Query{Limit: threads})
	if err != nil {
		t.Fatal(err)
	}
	if got := count(); got != 1 {
		t.Fatalf("followed-discussions page of %d ran %d statements, want 1", threads, got)
	}
	if len(items) != threads {
		t.Fatalf("got %d follows, want %d", len(items), threads)
	}
	byThread := map[string]discussionfollows.Follow{}
	for _, item := range items {
		byThread[item.ThreadID] = item
		// The batched projection must match the single-post read exactly.
		want, err := readPost(t.Context(), p.db, reader.ID, item.ThreadID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(item.Root, want) || item.EventID != want.EventID {
			t.Fatalf("thread %s projection differs:\n got %+v\nwant %+v", item.ThreadID, item.Root, want)
		}
	}
	if got := byThread[roots[1]].Root; got.ReplyCount != 0 {
		t.Fatal("hidden reply counted", got.ReplyCount)
	}
	if got := byThread[roots[2]].Root; !got.Helpful || got.HelpfulCount != 2 {
		t.Fatal("helpful state", got.Helpful, got.HelpfulCount)
	}
	if got := byThread[roots[3]].Root; !got.Hidden || got.Body != "" || got.Profile != nil {
		t.Fatal("hidden root leaked", got)
	}
	if got := byThread[roots[4]].Root; !got.Removed || got.Body != "" || got.Profile != nil {
		t.Fatal("removed root leaked", got)
	}
	if got := byThread[roots[5]]; got.Frequency != fanoutFrequency(5) || got.Root.Body != "Question 5" || got.Root.Profile == nil {
		t.Fatal("visible root", got)
	}

	// Pages stay in (created_at, thread_id) order across shared timestamps.
	s := discussionfollows.New(r)
	count() // exclude the comparison reads above
	values := url.Values{"limit": {"7"}}
	var paged []string
	for {
		page, err := s.List(t.Context(), reader.ID, values)
		if err != nil {
			t.Fatal(err)
		}
		if got := count(); got != 1 {
			t.Fatalf("followed-discussions page ran %d statements, want 1", got)
		}
		for _, item := range page.Items {
			paged = append(paged, item.ThreadID)
		}
		if page.NextCursor == nil {
			break
		}
		values.Set("cursor", *page.NextCursor)
	}
	var want []string
	for _, item := range items {
		want = append(want, item.ThreadID)
	}
	if !reflect.DeepEqual(paged, want) {
		t.Fatal("pagination order or coverage changed")
	}
}

func BenchmarkMariaDBDiscussionReplyFanout(b *testing.B) {
	for _, followers := range []int{1, 100, 1000} {
		b.Run(fmt.Sprintf("followers=%d", followers), func(b *testing.B) {
			f := newMaria(b)
			f.migrate(b)
			p := f.open(b)
			author := benchmarkOwner(0)
			execSQL(b, p.db, `INSERT INTO accounts (account_id,email,city,country,category_ids) VALUES (?,'fanout-author@example.test','Berlin','DE','["music"]')`, author)
			detail := savedDetail("FanoutBench")
			posts := discussions.New(p.Discussions(), discussionDetailFixture{detail})
			root, _, err := posts.Create(b.Context(), author, detail.Item.ID, "", "", "Benchmark question", accounts.ID())
			if err != nil {
				b.Fatal(err)
			}
			seedFollowers(b, p, root.ID, "bench", followers)
			c := discussions.Create{Owner: author, EventID: root.EventID, ThreadID: root.ID, Body: "Benchmark reply"}
			i := 0
			b.ReportAllocs()
			for b.Loop() {
				b.StopTimer()
				c.Key = fmt.Sprintf("fanout-%d", i)
				i++
				b.StartTimer()
				if _, fresh, err := p.Discussions().Create(b.Context(), c, nil); err != nil || !fresh {
					b.Fatal("reply", err)
				}
			}
		})
	}
}
