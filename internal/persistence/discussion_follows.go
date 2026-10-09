package persistence

import (
	"context"
	"database/sql"
	"errors"
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
	args = append(args, q.Limit+1)
	rows, err := r.db.QueryContext(ctx, `SELECT p.event_id,f.thread_id,f.frequency,f.created_at FROM discussion_follows f JOIN discussion_posts p ON p.post_id=f.thread_id`+where+` ORDER BY f.created_at DESC,f.thread_id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, safeError("discussion follow collection", err)
	}
	items := []discussionfollows.Follow{}
	for rows.Next() {
		var f discussionfollows.Follow
		if err = rows.Scan(&f.EventID, &f.ThreadID, &f.Frequency, &f.CreatedAt); err != nil {
			rows.Close()
			return nil, safeError("discussion follow collection", err)
		}
		items = append(items, f)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, safeError("discussion follow collection", err)
	}
	for i := range items {
		items[i].Root, err = readPost(ctx, r.db, who, items[i].ThreadID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}

// enqueueDiscussionReply runs in the publication transaction, after the root
// lock, so a retry or rollback can neither duplicate nor lose notifications.
func enqueueDiscussionReply(ctx context.Context, tx *sql.Tx, post discussions.Post, author string) error {
	if post.ID == post.ThreadID {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT f.account_id,f.frequency FROM discussion_follows f JOIN accounts a ON a.account_id=f.account_id JOIN discussion_posts root ON root.post_id=f.thread_id WHERE f.thread_id=? AND f.account_id<>? AND f.frequency<>'muted' AND a.notifications_paused=FALSE AND root.hidden_at IS NULL AND root.removed_at IS NULL`, post.ThreadID, author)
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
	for _, r := range recipients {
		at := discussionfollows.DeliveryAt(r.frequency, post.CreatedAt)
		batch := post.ID
		if r.frequency != "immediate" {
			batch = r.frequency + ":" + at.Format("2006-01-02")
		}
		id := accounts.ID()
		if _, err = tx.ExecContext(ctx, `INSERT INTO discussion_notifications (notification_id,account_id,thread_id,batch_key,available_at) VALUES (?,?,?,?,?) ON DUPLICATE KEY UPDATE notification_id=notification_id`, id, r.who, post.ThreadID, batch, at); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO discussion_notification_posts (notification_id,post_id) SELECT notification_id,? FROM discussion_notifications WHERE account_id=? AND thread_id=? AND batch_key=?`, post.ID, r.who, post.ThreadID, batch); err != nil {
			return err
		}
	}
	return nil
}

// Visibility is checked at delivery/read time, not just enqueue time. Only
// currently visible replies count; links select the latest visible reply.
const notificationJoin = ` FROM discussion_notifications n JOIN discussion_follows f ON f.account_id=n.account_id AND f.thread_id=n.thread_id JOIN accounts a ON a.account_id=n.account_id JOIN discussion_posts root ON root.post_id=n.thread_id JOIN event_snapshots e ON e.event_id=root.event_id`
const notificationVisible = ` AND n.available_at<=UTC_TIMESTAMP(6) AND a.notifications_paused=FALSE AND f.frequency<>'muted' AND root.hidden_at IS NULL AND root.removed_at IS NULL AND EXISTS(SELECT 1 FROM discussion_notification_posts np JOIN discussion_posts p ON p.post_id=np.post_id WHERE np.notification_id=n.notification_id AND p.hidden_at IS NULL AND p.removed_at IS NULL)`

func (r *DiscussionFollowRepository) Notifications(ctx context.Context, who string, q discussionfollows.Query) ([]discussionfollows.Notification, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	where := ` WHERE n.account_id=?` + notificationVisible
	args := []any{who}
	if q.Cursor != nil {
		where += ` AND (n.available_at<? OR (n.available_at=? AND n.notification_id<?))`
		args = append(args, q.Cursor.At, q.Cursor.At, q.Cursor.ID)
	}
	args = append(args, q.Limit+1)
	visible := ` FROM discussion_notification_posts np JOIN discussion_posts p ON p.post_id=np.post_id WHERE np.notification_id=n.notification_id AND p.hidden_at IS NULL AND p.removed_at IS NULL`
	rows, err := r.db.QueryContext(ctx, `SELECT n.notification_id,root.event_id,n.thread_id,COALESCE(JSON_UNQUOTE(JSON_EXTRACT(e.snapshot,'$.name')),''),n.available_at,n.read_at IS NOT NULL,(SELECT COUNT(*)`+visible+`),(SELECT p.post_id`+visible+` ORDER BY p.created_at DESC,p.post_id DESC LIMIT 1)`+notificationJoin+where+` ORDER BY n.available_at DESC,n.notification_id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, safeError("discussion notifications", err)
	}
	defer rows.Close()
	items := []discussionfollows.Notification{}
	for rows.Next() {
		var n discussionfollows.Notification
		if err = rows.Scan(&n.ID, &n.EventID, &n.ThreadID, &n.EventName, &n.AvailableAt, &n.Read, &n.ReplyCount, &n.PostID); err != nil {
			return nil, safeError("discussion notifications", err)
		}
		items = append(items, n)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError("discussion notifications", err)
	}
	return items, nil
}
func (r *DiscussionFollowRepository) Read(ctx context.Context, who, id string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	// Owner-only and idempotent, including already-read notifications.
	var found string
	err := r.db.QueryRowContext(ctx, `SELECT n.notification_id`+notificationJoin+` WHERE n.account_id=? AND n.notification_id=?`+notificationVisible, who, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		return discussions.ErrNotFound
	}
	if err != nil {
		return safeError("notification read", err)
	}
	_, err = r.db.ExecContext(ctx, `UPDATE discussion_notifications SET read_at=COALESCE(read_at,UTC_TIMESTAMP(6)) WHERE notification_id=? AND account_id=?`, id, who)
	if err != nil {
		return safeError("notification read", err)
	}
	return nil
}
