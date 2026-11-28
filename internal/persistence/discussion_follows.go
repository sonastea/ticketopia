package persistence

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/internal/discussions"
)

type DiscussionFollowRepository struct {
	db      *sql.DB
	timeout time.Duration
}

var _ discussionfollows.Repository = (*DiscussionFollowRepository)(nil)

func (p *Pool) DiscussionFollows() *DiscussionFollowRepository {
	return &DiscussionFollowRepository{p.db, p.config.QueryTimeout}
}
func readDiscussionFollow(ctx context.Context, db postReader, who, event, thread string) (discussionfollows.Follow, error) {
	f := discussionfollows.Follow{EventID: event, ThreadID: thread}
	err := db.QueryRowContext(ctx, `SELECT f.frequency,f.created_at FROM discussion_follows f JOIN discussion_posts p ON p.post_id=f.thread_id WHERE f.account_id=? AND f.thread_id=? AND p.event_id=? AND p.root_id IS NULL`, who, thread, event).Scan(&f.Frequency, &f.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return f, discussions.ErrNotFound
	}
	if err != nil {
		return f, safeError("discussion follow read", err)
	}
	f.Root, err = readPost(ctx, db, who, thread)
	return f, err
}
func (r *DiscussionFollowRepository) Get(ctx context.Context, who, event, thread string) (discussionfollows.Follow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return readDiscussionFollow(ctx, r.db, who, event, thread)
}
func (r *DiscussionFollowRepository) Set(ctx context.Context, who, event, thread, frequency string) (discussionfollows.Follow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return discussionfollows.Follow{}, safeError("discussion follow write", err)
	}
	defer tx.Rollback()
	// The same root lock serializes reply publication and subscription changes.
	var found string
	err = tx.QueryRowContext(ctx, `SELECT post_id FROM discussion_posts WHERE post_id=? AND event_id=? AND root_id IS NULL FOR UPDATE`, thread, event).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return discussionfollows.Follow{}, discussions.ErrNotFound
	}
	if err != nil {
		return discussionfollows.Follow{}, safeError("discussion follow write", err)
	}
	// Cancel unsent/unread envelopes when changing frequency; don't replay history.
	_, err = tx.ExecContext(ctx, `DELETE n FROM discussion_notifications n JOIN discussion_follows f ON f.account_id=n.account_id AND f.thread_id=n.thread_id WHERE f.account_id=? AND f.thread_id=? AND f.frequency<>? AND n.read_at IS NULL`, who, thread, frequency)
	if err != nil {
		return discussionfollows.Follow{}, safeError("discussion follow write", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO discussion_follows (account_id,thread_id,frequency) VALUES (?,?,?) ON DUPLICATE KEY UPDATE frequency=VALUES(frequency)`, who, thread, frequency)
	if err != nil {
		return discussionfollows.Follow{}, safeError("discussion follow write", err)
	}
	f, err := readDiscussionFollow(ctx, tx, who, event, thread)
	if err != nil {
		return f, err
	}
	if err = tx.Commit(); err != nil {
		return f, safeError("discussion follow write", err)
	}
	return f, nil
}
func (r *DiscussionFollowRepository) Remove(ctx context.Context, who, event, thread string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return safeError("discussion unfollow", err)
	}
	defer tx.Rollback()
	var found string
	err = tx.QueryRowContext(ctx, `SELECT post_id FROM discussion_posts WHERE post_id=? AND event_id=? AND root_id IS NULL FOR UPDATE`, thread, event).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return safeError("discussion unfollow", err)
	}
	_, err = tx.ExecContext(ctx, `DELETE FROM discussion_follows WHERE account_id=? AND thread_id=?`, who, thread)
	if err != nil {
		return safeError("discussion unfollow", err)
	}
	if err = tx.Commit(); err != nil {
		return safeError("discussion unfollow", err)
	}
	return nil
}
func (r *DiscussionFollowRepository) List(ctx context.Context, who string, q discussionfollows.Query) ([]discussionfollows.Follow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	where := ` WHERE f.account_id=?`
	args := []any{who}
	if q.Cursor != nil {
		where += ` AND (f.created_at<? OR (f.created_at=? AND f.thread_id<?))`
		args = append(args, q.Cursor.At, q.Cursor.At, q.Cursor.ID)
	}
	// The viewer argument fills postColumns' Helpful and Same question placeholders.
	args = append([]any{who, who}, append(args, q.Limit+1)...)
	// Roots load with the page, using the same projection as readPost.
	rows, err := r.db.QueryContext(ctx, `SELECT f.frequency,f.created_at,`+postColumns+` FROM discussion_follows f JOIN discussion_posts p ON p.post_id=f.thread_id JOIN accounts a ON a.account_id=p.account_id JOIN event_snapshots e ON e.event_id=p.event_id`+where+` ORDER BY f.created_at DESC,f.thread_id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, safeError("discussion follow collection", err)
	}
	defer rows.Close()
	items := []discussionfollows.Follow{}
	for rows.Next() {
		var f discussionfollows.Follow
		if f.Root, err = scanPost(prefixedRow{rows, []any{&f.Frequency, &f.CreatedAt}}); err != nil {
			return nil, err
		}
		f.EventID, f.ThreadID = f.Root.EventID, f.Root.ID
		items = append(items, f)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError("discussion follow collection", err)
	}
	return items, nil
}

// prefixedRow scans leading columns ahead of a shared projection.
type prefixedRow struct {
	row    interface{ Scan(...any) error }
	prefix []any
}

func (r prefixedRow) Scan(dest ...any) error {
	return r.row.Scan(slices.Concat(r.prefix, dest)...)
}

// enqueueChunk bounds recipients per multi-row statement (five placeholders each).
const enqueueChunk = 500

// enqueueDiscussionReply runs in the publication transaction, after the root
// lock, so a retry or rollback can neither duplicate nor lose notifications.
func enqueueDiscussionReply(ctx context.Context, tx *sql.Tx, post discussions.Post, author string) error {
	if post.ID == post.ThreadID {
		return nil
	}
	// Account order gives concurrent writers a consistent row-lock order.
	rows, err := tx.QueryContext(ctx, `SELECT f.account_id,f.frequency FROM discussion_follows f JOIN accounts a ON a.account_id=f.account_id JOIN discussion_posts root ON root.post_id=f.thread_id WHERE f.thread_id=? AND f.account_id<>? AND f.frequency<>'muted' AND a.notifications_paused=FALSE AND root.hidden_at IS NULL AND root.removed_at IS NULL ORDER BY f.account_id`, post.ThreadID, author)
	if err != nil {
		return err
	}
	type recipient struct{ who, frequency string }
	recipients := []recipient{}
	for rows.Next() {
		var r recipient
		if err = rows.Scan(&r.who, &r.frequency); err != nil {
			rows.Close()
			return err
		}
		recipients = append(recipients, r)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	batches := map[string]string{"immediate": post.ID}
	for _, frequency := range []string{"daily", "weekly"} {
		batches[frequency] = frequency + ":" + discussionfollows.DeliveryAt(frequency, post.CreatedAt).Format("2006-01-02")
	}
	for chunk := range slices.Chunk(recipients, enqueueChunk) {
		envelopes := make([]any, 0, 5*len(chunk))
		who := make([]any, 0, len(chunk))
		for _, r := range chunk {
			envelopes = append(envelopes, accounts.ID(), r.who, post.ThreadID, batches[r.frequency], discussionfollows.DeliveryAt(r.frequency, post.CreatedAt))
			who = append(who, r.who)
		}
		// A reply committed after its digest window was read is still unseen.
		if _, err = tx.ExecContext(ctx, `INSERT INTO discussion_notifications (notification_id,account_id,thread_id,batch_key,available_at) VALUES `+strings.TrimSuffix(strings.Repeat("(?,?,?,?,?),", len(chunk)), ",")+` ON DUPLICATE KEY UPDATE read_at=NULL`, envelopes...); err != nil {
			return err
		}
		// The root lock also serializes frequency changes, so each recipient's
		// current frequency still names the envelope written above.
		links := append([]any{post.ID, post.ThreadID, batches["immediate"], batches["daily"], batches["weekly"]}, who...)
		if _, err = tx.ExecContext(ctx, `INSERT INTO discussion_notification_posts (notification_id,post_id) SELECT n.notification_id,? FROM discussion_notifications n JOIN discussion_follows f ON f.account_id=n.account_id AND f.thread_id=n.thread_id WHERE n.thread_id=? AND n.batch_key=CASE f.frequency WHEN 'immediate' THEN ? WHEN 'daily' THEN ? WHEN 'weekly' THEN ? END AND n.account_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",")+`)`, links...); err != nil {
			return err
		}
	}
	return nil
}

// Visibility is checked at delivery/read time, not just enqueue time. Only
// currently visible replies count; links select the latest visible reply.
const notificationScopeJoin = ` FROM discussion_notifications n JOIN discussion_follows f ON f.account_id=n.account_id AND f.thread_id=n.thread_id JOIN accounts a ON a.account_id=n.account_id JOIN discussion_posts root ON root.post_id=n.thread_id`
const notificationJoin = notificationScopeJoin + ` JOIN event_snapshots e ON e.event_id=root.event_id`
const notificationVisible = ` AND n.available_at<=UTC_TIMESTAMP(6) AND a.notifications_paused=FALSE AND f.frequency<>'muted' AND root.hidden_at IS NULL AND root.removed_at IS NULL AND EXISTS(SELECT 1 FROM discussion_notification_posts np JOIN discussion_posts p ON p.post_id=np.post_id WHERE np.notification_id=n.notification_id AND p.hidden_at IS NULL AND p.removed_at IS NULL)`

func notificationListScope(who string, q discussionfollows.Query) (string, []any) {
	where := ` WHERE n.account_id=?` + notificationVisible
	args := []any{who}
	if q.Cursor != nil {
		where += ` AND (n.available_at<? OR (n.available_at=? AND n.notification_id<?))`
		args = append(args, q.Cursor.At, q.Cursor.At, q.Cursor.ID)
	}
	return where, append(args, q.Limit+1)
}

func notificationListQuery(who string, q discussionfollows.Query) (string, []any) {
	where, args := notificationListScope(who, q)
	// Apply visibility before LIMIT, but defer the event snapshot and reply
	// projections until after paging. Posts have a snapshot foreign key.
	keys := `SELECT STRAIGHT_JOIN n.notification_id,root.event_id,n.thread_id,n.available_at,n.read_at` + notificationScopeJoin + where +
		` ORDER BY n.available_at DESC,n.notification_id DESC LIMIT ?`
	visible := ` FROM discussion_notification_posts np JOIN discussion_posts p ON p.post_id=np.post_id WHERE np.notification_id=page.notification_id AND p.hidden_at IS NULL AND p.removed_at IS NULL`
	return `SELECT page.notification_id,page.event_id,page.thread_id,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(e.snapshot,'$.name')),''),page.available_at,page.read_at IS NOT NULL,(SELECT COUNT(*)` + visible + `),(SELECT p.post_id` + visible + ` ORDER BY p.created_at DESC,p.post_id DESC LIMIT 1)
 FROM (` + keys + `) page STRAIGHT_JOIN event_snapshots e ON e.event_id=page.event_id ORDER BY page.available_at DESC,page.notification_id DESC`, args
}

// Notifications merges reply and Helpful updates into one newest-first page:
// each kind reads its own keyset page of up to Limit+1, and the merge keeps
// the first Limit+1, so the shared cursor works across both.
func (r *DiscussionFollowRepository) Notifications(ctx context.Context, who string, q discussionfollows.Query) ([]discussionfollows.Notification, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args := notificationListQuery(who, q)
	items := []discussionfollows.Notification{}
	err := queryRowsTx(ctx, r.db, query, args, func(rows *sql.Rows) error {
		n := discussionfollows.Notification{Kind: discussionfollows.KindReplies}
		if err := rows.Scan(&n.ID, &n.EventID, &n.ThreadID, &n.EventName, &n.AvailableAt, &n.Read, &n.ReplyCount, &n.PostID); err != nil {
			return err
		}
		items = append(items, n)
		return nil
	})
	if err != nil {
		return nil, safeError("discussion notifications", err)
	}
	query, args = helpfulListQuery(who, q)
	err = queryRowsTx(ctx, r.db, query, args, func(rows *sql.Rows) error {
		n := discussionfollows.Notification{Kind: discussionfollows.KindHelpful}
		var first, second sql.NullString
		if err := rows.Scan(&n.ID, &n.EventID, &n.ThreadID, &n.PostID, &n.EventName, &n.AvailableAt, &n.Read, &n.HelpfulCount, &first, &second); err != nil {
			return err
		}
		n.Reply = n.PostID != n.ThreadID
		for _, name := range []sql.NullString{first, second} {
			if name.Valid && name.String != "" {
				n.HelpfulNames = append(n.HelpfulNames, name.String)
			}
		}
		items = append(items, n)
		return nil
	})
	if err != nil {
		return nil, safeError("helpful notifications", err)
	}
	slices.SortFunc(items, func(a, b discussionfollows.Notification) int {
		if c := b.AvailableAt.Compare(a.AvailableAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	if len(items) > q.Limit+1 {
		items = items[:q.Limit+1]
	}
	return items, nil
}

// Helpful updates: one envelope per post per UTC day for its author. Delivery
// counts that day's Helpful marks from other people that still stand, so
// unmarking before delivery cancels a mark, and an envelope with none left,
// or whose post is withheld, is not shown. Pause withholds them like replies.
func helpfulMarks(n string) string {
	return ` FROM post_helpful h WHERE h.post_id=` + n + `.post_id AND h.created_day=DATE(` + n + `.window_start) AND h.account_id<>` + n + `.account_id`
}

const helpfulScopeJoin = ` FROM helpful_notifications hn JOIN accounts a ON a.account_id=hn.account_id JOIN discussion_posts p ON p.post_id=hn.post_id`

var helpfulVisible = ` AND hn.available_at<=UTC_TIMESTAMP(6) AND a.notifications_paused=FALSE AND p.account_id=hn.account_id AND p.hidden_at IS NULL AND p.removed_at IS NULL AND EXISTS(SELECT 1` + helpfulMarks("hn") + `)`

// helpfulName is the nth newest opted-in first name among the counted marks.
func helpfulName(offset int) string {
	return `(SELECT m.activity_first_name FROM post_helpful h JOIN accounts m ON m.account_id=h.account_id WHERE h.post_id=page.post_id AND h.created_day=DATE(page.window_start) AND h.account_id<>page.account_id AND m.activity_first_name IS NOT NULL ORDER BY h.created_at DESC,h.account_id DESC LIMIT 1 OFFSET ` + strconv.Itoa(offset) + `)`
}

func helpfulListQuery(who string, q discussionfollows.Query) (string, []any) {
	where := ` WHERE hn.account_id=?` + helpfulVisible
	args := []any{who}
	if q.Cursor != nil {
		where += ` AND (hn.available_at<? OR (hn.available_at=? AND hn.notification_id<?))`
		args = append(args, q.Cursor.At, q.Cursor.At, q.Cursor.ID)
	}
	args = append(args, q.Limit+1)
	keys := `SELECT hn.notification_id,hn.account_id,hn.post_id,hn.window_start,hn.available_at,hn.read_at` + helpfulScopeJoin + where + ` ORDER BY hn.available_at DESC,hn.notification_id DESC LIMIT ?`
	names := make([]string, discussionfollows.MaxHelpfulNames)
	for i := range names {
		names[i] = helpfulName(i)
	}
	return `SELECT page.notification_id,p.event_id,COALESCE(p.root_id,p.post_id),p.post_id,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(e.snapshot,'$.name')),''),page.available_at,page.read_at IS NOT NULL,(SELECT COUNT(*)` + helpfulMarks("page") + `),` + strings.Join(names, ",") + `
 FROM (` + keys + `) page JOIN discussion_posts p ON p.post_id=page.post_id JOIN event_snapshots e ON e.event_id=p.event_id ORDER BY page.available_at DESC,page.notification_id DESC`, args
}

// enqueueHelpful runs in the Helpful write's transaction after a new mark:
// the post author's envelope for the mark's own UTC day, unless the marker is
// the author or the author has paused notifications. The day comes from the
// stored mark, so a mark at midnight can't fall between windows.
func enqueueHelpful(ctx context.Context, tx *sql.Tx, marker, post string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO helpful_notifications (notification_id,account_id,post_id,window_start,available_at)
 SELECT ?,p.account_id,p.post_id,DATE(h.created_at),DATE(h.created_at)+INTERVAL 1 DAY
 FROM post_helpful h JOIN discussion_posts p ON p.post_id=h.post_id JOIN accounts a ON a.account_id=p.account_id
 WHERE h.account_id=? AND h.post_id=? AND p.account_id<>h.account_id AND a.notifications_paused=FALSE
 ON DUPLICATE KEY UPDATE notification_id=notification_id`, accounts.ID(), marker, post)
	return err
}

func (r *DiscussionFollowRepository) Read(ctx context.Context, who, id string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	// Owner-only and idempotent, including already-read notifications.
	var found string
	err := r.db.QueryRowContext(ctx, `SELECT n.notification_id`+notificationJoin+` WHERE n.account_id=? AND n.notification_id=?`+notificationVisible, who, id).Scan(&found)
	table := "discussion_notifications"
	if errors.Is(err, sql.ErrNoRows) {
		table = "helpful_notifications"
		err = r.db.QueryRowContext(ctx, `SELECT hn.notification_id`+helpfulScopeJoin+` WHERE hn.account_id=? AND hn.notification_id=?`+helpfulVisible, who, id).Scan(&found)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return discussions.ErrNotFound
	}
	if err != nil {
		return safeError("notification read", err)
	}
	_, err = r.db.ExecContext(ctx, `UPDATE `+table+` SET read_at=COALESCE(read_at,UTC_TIMESTAMP(6)) WHERE notification_id=? AND account_id=?`, id, who)
	if err != nil {
		return safeError("notification read", err)
	}
	return nil
}
