package persistence

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/models"
)

type DiscussionRepository struct {
	db      *sql.DB
	timeout time.Duration
}

var _ discussions.Repository = (*DiscussionRepository)(nil)

func (p *Pool) Discussions() *DiscussionRepository {
	return &DiscussionRepository{p.db, p.config.QueryTimeout}
}
func (r *DiscussionRepository) Snapshot(ctx context.Context) (time.Time, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var at time.Time
	if err := r.db.QueryRowContext(ctx, `SELECT UTC_TIMESTAMP(6)`).Scan(&at); err != nil {
		return at, safeError("discussion boundary", err)
	}
	return at, nil
}

const postColumns = `p.post_id,p.event_id,COALESCE(p.root_id,p.post_id),COALESCE(p.parent_id,''),a.account_id,a.display_name,a.bio,p.body,p.created_at,p.updated_at,p.removed_at IS NOT NULL,e.snapshot,e.data_as_of,
 (SELECT COUNT(*) FROM discussion_posts replies WHERE replies.root_id=p.post_id AND replies.removed_at IS NULL),
 (SELECT COUNT(*) FROM post_helpful h WHERE h.post_id=p.post_id),
 EXISTS(SELECT 1 FROM post_helpful h WHERE h.post_id=p.post_id AND h.account_id=?),
 COALESCE((SELECT IF(parent.removed_at IS NULL,author.display_name,'Removed contribution') FROM discussion_posts parent JOIN accounts author ON author.account_id=parent.account_id WHERE parent.post_id=p.parent_id),'')`
const postJoin = ` FROM discussion_posts p JOIN accounts a ON a.account_id=p.account_id JOIN event_snapshots e ON e.event_id=p.event_id`

func scanPost(row interface{ Scan(...any) error }) (discussions.Post, error) {
	var p discussions.Post
	var profile accounts.PublicProfile
	var data []byte
	err := row.Scan(&p.ID, &p.EventID, &p.ThreadID, &p.ParentID, &profile.ID, &profile.DisplayName, &profile.Bio, &p.Body, &p.CreatedAt, &p.UpdatedAt, &p.Removed, &data, &p.Meta.DataAsOf, &p.ReplyCount, &p.HelpfulCount, &p.Helpful, &p.ParentName)
	if errors.Is(err, sql.ErrNoRows) {
		return p, discussions.ErrNotFound
	}
	if err != nil {
		return p, safeError("discussion read", err)
	}
	if json.Unmarshal(data, &p.Event) != nil {
		return p, discussions.ErrUnavailable
	}
	p.Meta.Stale = true
	if p.Removed {
		p.Body = ""
		p.ParentName = ""
		p.HelpfulCount = 0
		p.Helpful = false
	} else {
		p.Profile = &profile
	}
	return p, nil
}

type postReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readPost(ctx context.Context, db postReader, viewer, id string) (discussions.Post, error) {
	return scanPost(db.QueryRowContext(ctx, `SELECT `+postColumns+postJoin+` WHERE p.post_id=?`, viewer, id))
}
func (r *DiscussionRepository) Get(ctx context.Context, viewer, id string) (discussions.Post, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return readPost(ctx, r.db, viewer, id)
}
func retryPost(ctx context.Context, db postReader, owner, key string, hash [32]byte) (discussions.Post, error) {
	var id string
	var stored []byte
	err := db.QueryRowContext(ctx, `SELECT post_id,request_hash FROM discussion_posts WHERE account_id=? AND idempotency_key=?`, owner, key).Scan(&id, &stored)
	if errors.Is(err, sql.ErrNoRows) {
		return discussions.Post{}, discussions.ErrNotFound
	}
	if err != nil {
		return discussions.Post{}, safeError("discussion retry", err)
	}
	if subtle.ConstantTimeCompare(hash[:], stored) != 1 {
		return discussions.Post{}, discussions.ErrConflict
	}
	return readPost(ctx, db, owner, id)
}
func (r *DiscussionRepository) Retry(ctx context.Context, owner, key string, hash [32]byte) (discussions.Post, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return retryPost(ctx, r.db, owner, key, hash)
}
func (r *DiscussionRepository) Create(ctx context.Context, c discussions.Create, d *models.EventDetail) (discussions.Post, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	for attempt := range 3 {
		p, fresh, err := r.createOnce(ctx, c, d)
		if err == nil || errors.Is(err, discussions.ErrNotFound) || errors.Is(err, discussions.ErrConflict) || errors.Is(err, discussions.ErrUnavailable) {
			return p, fresh, err
		}
		if !retryable(err) {
			return discussions.Post{}, false, safeError("discussion write", err)
		}
		if attempt == 2 {
			return discussions.Post{}, false, safeError("discussion write", err)
		}
	}
	panic("unreachable")
}
func (r *DiscussionRepository) createOnce(ctx context.Context, c discussions.Create, d *models.EventDetail) (discussions.Post, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return discussions.Post{}, false, err
	}
	defer tx.Rollback()
	// Serialize retry keys across replicas before checking/inserting. Keys and
	// fingerprints survive edits/removal for the lifetime of the contribution.
	var account string
	if err = tx.QueryRowContext(ctx, `SELECT account_id FROM accounts WHERE account_id=? FOR UPDATE`, c.Owner).Scan(&account); err != nil {
		return discussions.Post{}, false, err
	}
	p, err := retryPost(ctx, tx, c.Owner, c.Key, c.Hash)
	if err == nil {
		return p, false, nil
	}
	if !errors.Is(err, discussions.ErrNotFound) {
		return discussions.Post{}, false, err
	}
	if c.ThreadID == "" {
		if d == nil {
			return discussions.Post{}, false, discussions.ErrUnavailable
		}
		args, data, err := prepareSnapshot(*d)
		if err != nil {
			return discussions.Post{}, false, discussions.ErrUnavailable
		}
		if err = upsertSnapshotTx(ctx, tx, *d, args, data); err != nil {
			return discussions.Post{}, false, err
		}
	} else {
		var event string
		var root sql.NullString
		if err = tx.QueryRowContext(ctx, `SELECT event_id,root_id FROM discussion_posts WHERE post_id=? FOR UPDATE`, c.ThreadID).Scan(&event, &root); errors.Is(err, sql.ErrNoRows) {
			return discussions.Post{}, false, discussions.ErrNotFound
		} else if err != nil {
			return discussions.Post{}, false, err
		}
		if event != c.EventID || root.Valid {
			return discussions.Post{}, false, discussions.ErrNotFound
		}
		if c.ParentID != "" {
			var parentRoot string
			if err = tx.QueryRowContext(ctx, `SELECT COALESCE(root_id,post_id) FROM discussion_posts WHERE post_id=? AND event_id=?`, c.ParentID, c.EventID).Scan(&parentRoot); errors.Is(err, sql.ErrNoRows) {
				return discussions.Post{}, false, discussions.ErrNotFound
			} else if err != nil {
				return discussions.Post{}, false, err
			}
			if parentRoot != c.ThreadID {
				return discussions.Post{}, false, discussions.ErrNotFound
			}
		}
	}
	id := accounts.ID()
	_, err = tx.ExecContext(ctx, `INSERT INTO discussion_posts (post_id,account_id,event_id,root_id,parent_id,body,idempotency_key,request_hash) VALUES (?,?,?,NULLIF(?,''),NULLIF(?,''),?,?,?)`, id, c.Owner, c.EventID, c.ThreadID, c.ParentID, c.Body, c.Key, c.Hash[:])
	if err != nil {
		return discussions.Post{}, false, err
	}
	p, err = readPost(ctx, tx, c.Owner, id)
	if err != nil {
		return discussions.Post{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return discussions.Post{}, false, err
	}
	return p, true, nil
}
func (r *DiscussionRepository) Edit(ctx context.Context, owner, id, body string) (discussions.Post, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return discussions.Post{}, safeError("discussion edit", err)
	}
	defer tx.Rollback()
	var who string
	var removed bool
	err = tx.QueryRowContext(ctx, `SELECT account_id,removed_at IS NOT NULL FROM discussion_posts WHERE post_id=? FOR UPDATE`, id).Scan(&who, &removed)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (who != owner || removed)) {
		return discussions.Post{}, discussions.ErrNotFound
	}
	if err != nil {
		return discussions.Post{}, safeError("discussion edit", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE discussion_posts SET updated_at=IF(BINARY body<>BINARY ?,UTC_TIMESTAMP(6),updated_at),body=? WHERE post_id=?`, body, body, id)
	if err != nil {
		return discussions.Post{}, safeError("discussion edit", err)
	}
	p, err := readPost(ctx, tx, owner, id)
	if err != nil {
		return discussions.Post{}, err
	}
	if err = tx.Commit(); err != nil {
		return discussions.Post{}, safeError("discussion edit", err)
	}
	return p, nil
}
func (r *DiscussionRepository) Remove(ctx context.Context, owner, id string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError("discussion removal", err)
	}
	defer tx.Rollback()
	var who string
	err = tx.QueryRowContext(ctx, `SELECT account_id FROM discussion_posts WHERE post_id=? FOR UPDATE`, id).Scan(&who)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && who != owner) {
		return discussions.ErrNotFound
	}
	if err != nil {
		return safeError("discussion removal", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE discussion_posts SET body='',updated_at=IF(removed_at IS NULL,UTC_TIMESTAMP(6),updated_at),removed_at=COALESCE(removed_at,UTC_TIMESTAMP(6)) WHERE post_id=?`, id); err != nil {
		return safeError("discussion removal", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM post_helpful WHERE post_id=?`, id); err != nil {
		return safeError("discussion removal", err)
	}
	if err = tx.Commit(); err != nil {
		return safeError("discussion removal", err)
	}
	return nil
}
func (r *DiscussionRepository) React(ctx context.Context, owner, id string, set bool) (discussions.Post, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return discussions.Post{}, safeError("Helpful write", err)
	}
	defer tx.Rollback()
	var removed bool
	err = tx.QueryRowContext(ctx, `SELECT removed_at IS NOT NULL FROM discussion_posts WHERE post_id=? FOR UPDATE`, id).Scan(&removed)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && removed && set) {
		return discussions.Post{}, discussions.ErrNotFound
	}
	if err != nil {
		return discussions.Post{}, safeError("Helpful write", err)
	}
	if set {
		_, err = tx.ExecContext(ctx, `INSERT INTO post_helpful (account_id,post_id) VALUES (?,?) ON DUPLICATE KEY UPDATE post_id=VALUES(post_id)`, owner, id)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM post_helpful WHERE account_id=? AND post_id=?`, owner, id)
	}
	if err != nil {
		return discussions.Post{}, safeError("Helpful write", err)
	}
	p, err := readPost(ctx, tx, owner, id)
	if err != nil {
		return discussions.Post{}, err
	}
	if err = tx.Commit(); err != nil {
		return discussions.Post{}, safeError("Helpful write", err)
	}
	return p, nil
}
func (r *DiscussionRepository) List(ctx context.Context, viewer, event, thread string, q discussions.Query) ([]discussions.Post, int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	where := ` WHERE p.created_at<=?`
	args := []any{q.Snapshot.UTC()}
	if thread != "" {
		where += ` AND p.root_id=?`
		args = append(args, thread)
	} else {
		where += ` AND p.root_id IS NULL AND (p.removed_at IS NULL OR EXISTS(SELECT 1 FROM discussion_posts child WHERE child.root_id=p.post_id AND child.removed_at IS NULL))`
	}
	if event != "" {
		where += ` AND p.event_id=?`
		args = append(args, event)
	}
	for _, filter := range []struct{ value, path, fallback string }{{q.City, "$.venues[0].city", "$.place.city"}, {q.Country, "$.venues[0].country_code", "$.place.country_code"}} {
		if filter.value != "" {
			where += ` AND COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(e.snapshot,?)),'null'),NULLIF(JSON_UNQUOTE(JSON_EXTRACT(e.snapshot,?)),'null'),'') COLLATE utf8mb4_unicode_ci=?`
			args = append(args, filter.path, filter.fallback, filter.value)
		}
	}
	if q.CategoryID != "" {
		where += ` AND JSON_CONTAINS(e.snapshot,JSON_OBJECT('segment',JSON_OBJECT('id',?)),'$.classifications')=1`
		args = append(args, q.CategoryID)
	}
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*)`+postJoin+where+` AND p.removed_at IS NULL`, args...).Scan(&count); err != nil {
		return nil, 0, safeError("discussion count", err)
	}
	op, order := "<", "DESC"
	if thread != "" {
		op, order = ">", "ASC"
	}
	if q.Cursor != nil {
		where += ` AND (p.created_at` + op + `? OR (p.created_at=? AND p.post_id` + op + `?))`
		args = append(args, q.Cursor.At.UTC(), q.Cursor.At.UTC(), q.Cursor.ID)
	}
	args = append([]any{viewer}, args...)
	args = append(args, q.Limit+1)
	rows, err := r.db.QueryContext(ctx, `SELECT `+postColumns+postJoin+where+` ORDER BY p.created_at `+order+`,p.post_id `+order+` LIMIT ?`, args...)
	if err != nil {
		return nil, 0, safeError("discussion collection", err)
	}
	defer rows.Close()
	items := []discussions.Post{}
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, 0, err
		}
		items = append(items, p)
	}
	if err = rows.Err(); err != nil {
		return nil, 0, safeError("discussion collection", err)
	}
	return items, count, nil
}
