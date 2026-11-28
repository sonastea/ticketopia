package persistence

import (
	"cmp"
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"slices"
	"strings"
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

const postColumns = postHead + `e.snapshot` + postTail

// postPageColumns send each page event's snapshot once; see firstOfEvent.
const postPageColumns = postHead + `IF(page.first,e.snapshot,NULL)` + postTail
const postHead = `p.post_id,p.event_id,COALESCE(p.root_id,p.post_id),COALESCE(p.parent_id,''),a.account_id,a.display_name,a.bio,p.body,p.created_at,p.updated_at,p.removed_at IS NOT NULL,p.hidden_at IS NOT NULL,`
const postTail = `,e.data_as_of,
 (SELECT COUNT(*) FROM discussion_posts replies WHERE replies.root_id=p.post_id AND replies.removed_at IS NULL AND replies.hidden_at IS NULL),
 (SELECT COUNT(*) FROM post_helpful h WHERE h.post_id=p.post_id),
 EXISTS(SELECT 1 FROM post_helpful h WHERE h.post_id=p.post_id AND h.account_id=?),
 (SELECT COUNT(*) FROM discussion_same_question s WHERE s.post_id=p.post_id),
 EXISTS(SELECT 1 FROM discussion_same_question s WHERE s.post_id=p.post_id AND s.account_id=?),
 COALESCE((SELECT CASE WHEN parent.removed_at IS NOT NULL THEN 'Removed contribution' WHEN parent.hidden_at IS NOT NULL THEN 'Hidden contribution' ELSE author.display_name END FROM discussion_posts parent JOIN accounts author ON author.account_id=parent.account_id WHERE parent.post_id=p.parent_id),''),
 COALESCE(p.via_client_name,''),COALESCE(p.via_client_host,''),
 (p.root_id IS NULL AND (SELECT COUNT(*) FROM discussion_posts mine WHERE mine.account_id=p.account_id AND mine.removed_at IS NULL AND mine.hidden_at IS NULL)<3
  AND NOT EXISTS(SELECT 1 FROM discussion_posts earlier WHERE earlier.account_id=p.account_id AND earlier.root_id IS NULL AND earlier.removed_at IS NULL AND earlier.hidden_at IS NULL AND (earlier.created_at<p.created_at OR (earlier.created_at=p.created_at AND earlier.post_id<p.post_id))))`
const postJoin = ` FROM discussion_posts p JOIN accounts a ON a.account_id=p.account_id JOIN event_snapshots e ON e.event_id=p.event_id`

func scanPost(row interface{ Scan(...any) error }) (discussions.Post, error) {
	var p discussions.Post
	data, err := scanPostRow(row, &p)
	if err != nil {
		return p, err
	}
	if json.Unmarshal(data, &p.Event) != nil {
		return p, discussions.ErrUnavailable
	}
	return p, nil
}

// scanPostRow scans and redacts a post into p, returning its undecoded snapshot.
func scanPostRow(row interface{ Scan(...any) error }, p *discussions.Post) ([]byte, error) {
	var profile accounts.PublicProfile
	var data []byte
	err := row.Scan(&p.ID, &p.EventID, &p.ThreadID, &p.ParentID, &profile.ID, &profile.DisplayName, &profile.Bio, &p.Body, &p.CreatedAt, &p.UpdatedAt, &p.Removed, &p.Hidden, &data, &p.Meta.DataAsOf, &p.ReplyCount, &p.HelpfulCount, &p.Helpful, &p.SameQuestionCount, &p.SameQuestion, &p.ParentName, &p.Via, &p.ViaHost, &p.FirstPost)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, discussions.ErrNotFound
	}
	if err != nil {
		return nil, safeError("discussion read", err)
	}
	p.Meta.Stale = true
	if p.Removed || p.Hidden {
		if p.Removed {
			p.Hidden = false
		}
		p.Body = ""
		p.ParentName = ""
		p.Via, p.ViaHost = "", ""
		p.HelpfulCount = 0
		p.Helpful = false
		p.SameQuestionCount = 0
		p.SameQuestion = false
		p.FirstPost = false
	} else {
		p.Profile = &profile
	}
	return data, nil
}

type postReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

const postSelect = `SELECT ` + postColumns + postJoin

func readPost(ctx context.Context, db postReader, viewer, id string) (discussions.Post, error) {
	return scanPost(db.QueryRowContext(ctx, postSelect+` WHERE p.post_id=?`, viewer, viewer, id))
}

// readPosts is readPost for a bounded set of IDs in one statement, keyed by ID.
// A missing post is ErrNotFound, as it would be for readPost.
func readPosts(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, viewer string, ids []any) (map[string]discussions.Post, error) {
	posts := make(map[string]discussions.Post, len(ids))
	if len(ids) == 0 {
		return posts, nil
	}
	var scanErr error
	err := queryRowsTx(ctx, db, postSelect+` WHERE p.post_id IN (`+placeholders(len(ids))+`)`, append([]any{viewer, viewer}, ids...), func(rows *sql.Rows) error {
		p, err := scanPost(rows)
		scanErr = err
		posts[p.ID] = p
		return err
	})
	if scanErr != nil {
		return nil, scanErr
	}
	if err != nil {
		return nil, safeError("discussion read", err)
	}
	for _, id := range ids {
		if _, ok := posts[id.(string)]; !ok {
			return nil, discussions.ErrNotFound
		}
	}
	return posts, nil
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
		if err == nil || errors.Is(err, discussions.ErrNotFound) || errors.Is(err, discussions.ErrConflict) || errors.Is(err, discussions.ErrUnavailable) || errors.Is(err, discussions.ErrDraftGone) {
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
	// The draft is consumed with the post, or nothing happens, under the
	// account lock taken above, as recommendation drafts are.
	if c.Draft != "" {
		result, err := tx.ExecContext(ctx, `DELETE FROM agent_drafts WHERE account_id=? AND draft_id=? AND expires_at>UTC_TIMESTAMP(6)`, c.Owner, c.Draft)
		if err != nil {
			return discussions.Post{}, false, err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			if err == nil {
				err = discussions.ErrDraftGone
			}
			return discussions.Post{}, false, err
		}
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
	_, err = tx.ExecContext(ctx, `INSERT INTO discussion_posts (post_id,account_id,event_id,root_id,parent_id,body,idempotency_key,request_hash,via_client_name,via_client_host) VALUES (?,?,?,NULLIF(?,''),NULLIF(?,''),?,?,?,?,?)`,
		id, c.Owner, c.EventID, c.ThreadID, c.ParentID, c.Body, c.Key, c.Hash[:], viaValue(c.Via), viaValue(c.ViaHost))
	if err != nil {
		return discussions.Post{}, false, err
	}
	p, err = readPost(ctx, tx, c.Owner, id)
	if err != nil {
		return discussions.Post{}, false, err
	}
	if err = enqueueDiscussionReply(ctx, tx, p, c.Owner); err != nil {
		return discussions.Post{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return discussions.Post{}, false, err
	}
	invalidateDiscoverCounts()
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
	err = tx.QueryRowContext(ctx, `SELECT account_id,(removed_at IS NOT NULL OR hidden_at IS NOT NULL) FROM discussion_posts WHERE post_id=? FOR UPDATE`, id).Scan(&who, &removed)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (who != owner || removed)) {
		return discussions.Post{}, discussions.ErrNotFound
	}
	if err != nil {
		return discussions.Post{}, safeError("discussion edit", err)
	}
	// Attribution describes the current words: the person's change clears it.
	// Assignments run left to right, so these compare against the old body.
	_, err = tx.ExecContext(ctx, `UPDATE discussion_posts SET via_client_name=IF(BINARY body<>BINARY ?,NULL,via_client_name),via_client_host=IF(BINARY body<>BINARY ?,NULL,via_client_host),review_version=review_version+IF(BINARY body<>BINARY ?,1,0),updated_at=IF(BINARY body<>BINARY ?,UTC_TIMESTAMP(6),updated_at),body=? WHERE post_id=?`, body, body, body, body, body, id)
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
	if _, err = tx.ExecContext(ctx, `UPDATE discussion_posts SET body='',via_client_name=NULL,via_client_host=NULL,review_version=review_version+IF(removed_at IS NULL,1,0),updated_at=IF(removed_at IS NULL,UTC_TIMESTAMP(6),updated_at),removed_at=COALESCE(removed_at,UTC_TIMESTAMP(6)) WHERE post_id=?`, id); err != nil {
		return safeError("discussion removal", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM post_helpful WHERE post_id=?`, id); err != nil {
		return safeError("discussion removal", err)
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM discussion_same_question WHERE post_id=?`, id); err != nil {
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
	err = tx.QueryRowContext(ctx, `SELECT (removed_at IS NOT NULL OR hidden_at IS NOT NULL) FROM discussion_posts WHERE post_id=? FOR UPDATE`, id).Scan(&removed)
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

// SameQuestion sets or clears the owner's mark on someone else's visible
// question or tip. Marks only annotate; they never change any list order.
func (r *DiscussionRepository) SameQuestion(ctx context.Context, owner, id string, set bool) (discussions.Post, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return discussions.Post{}, safeError("Same question write", err)
	}
	defer tx.Rollback()
	var author string
	var reply, withheld bool
	err = tx.QueryRowContext(ctx, `SELECT account_id,root_id IS NOT NULL,(removed_at IS NOT NULL OR hidden_at IS NOT NULL) FROM discussion_posts WHERE post_id=? FOR UPDATE`, id).Scan(&author, &reply, &withheld)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (reply || (withheld && set))) {
		return discussions.Post{}, discussions.ErrNotFound
	}
	if err != nil {
		return discussions.Post{}, safeError("Same question write", err)
	}
	if author == owner && set {
		return discussions.Post{}, discussions.ErrOwnQuestion
	}
	if set {
		_, err = tx.ExecContext(ctx, `INSERT IGNORE INTO discussion_same_question (account_id,post_id) VALUES (?,?)`, owner, id)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM discussion_same_question WHERE account_id=? AND post_id=?`, owner, id)
	}
	if err != nil {
		return discussions.Post{}, safeError("Same question write", err)
	}
	p, err := readPost(ctx, tx, owner, id)
	if err != nil {
		return discussions.Post{}, err
	}
	if err = tx.Commit(); err != nil {
		return discussions.Post{}, safeError("Same question write", err)
	}
	return p, nil
}
func discussionQueries(viewer, event, thread string, q discussions.Query) (string, []any, string, []any) {
	where := ` WHERE p.created_at<=?`
	args := []any{q.Snapshot.UTC()}
	if thread != "" {
		where += ` AND p.root_id=?`
		args = append(args, thread)
	} else {
		where += ` AND p.root_id IS NULL`
	}
	if event != "" {
		where += ` AND p.event_id=?`
		args = append(args, event)
	}
	if q.Open {
		// Visible questions and tips with no visible reply, on events that have
		// not started (or whose date is unknown). A removed or hidden reply
		// returns the root at its original position. Eligibility uses the
		// database's current time, not the cursor's creation snapshot, so an
		// event that starts mid-pagination leaves later pages. A date-only event
		// counts until its own local day ends; without a usable time zone, a day
		// later than UTC's, so no event leaves early.
		where += ` AND p.removed_at IS NULL AND p.hidden_at IS NULL AND NOT EXISTS(SELECT 1 FROM discussion_posts answer WHERE answer.root_id=p.post_id AND answer.removed_at IS NULL AND answer.hidden_at IS NULL)
 AND EXISTS(SELECT 1 FROM events ev WHERE ev.event_id=p.event_id AND (ev.start_utc>=UTC_TIMESTAMP(6) OR (ev.start_utc IS NULL AND (ev.local_date IS NULL
  OR ev.local_date>=COALESCE(DATE(CONVERT_TZ(UTC_TIMESTAMP(),'+00:00',ev.timezone)),UTC_DATE()-INTERVAL 1 DAY)))))`
	}
	if q.Connected != "" {
		// The signed-in viewer's own private connections only; the match is
		// never returned or shown to anyone else.
		where += ` AND (EXISTS(SELECT 1 FROM saved_events cs WHERE cs.account_id=? AND cs.event_id=p.event_id)
 OR EXISTS(SELECT 1 FROM event_interests ci WHERE ci.account_id=? AND ci.event_id=p.event_id)
 OR EXISTS(SELECT 1 FROM artist_follows ca JOIN event_search_facets cf ON cf.kind='artist' AND cf.value=ca.artist_id WHERE ca.account_id=? AND cf.event_id=p.event_id)
 OR EXISTS(SELECT 1 FROM venue_follows cv JOIN event_search_facets cf ON cf.kind='venue' AND cf.value=cv.venue_id WHERE cv.account_id=? AND cf.event_id=p.event_id))`
		args = append(args, q.Connected, q.Connected, q.Connected, q.Connected)
	}
	filters, filterArgs := communityFilters(q.City, q.Country, q.CategoryID)
	where += filters
	args = append(args, filterArgs...)
	countSQL := `SELECT COUNT(*)` + postJoin + where + ` AND p.removed_at IS NULL AND p.hidden_at IS NULL`
	countArgs := append([]any(nil), args...)
	if thread == "" && !q.Open {
		// Only the page needs tombstone roots to keep visible replies reachable.
		// The exact total counts visible posts, never those placeholders.
		where += ` AND ((p.removed_at IS NULL AND p.hidden_at IS NULL) OR EXISTS(SELECT 1 FROM discussion_posts child WHERE child.root_id=p.post_id AND child.removed_at IS NULL AND child.hidden_at IS NULL))`
	}
	op, order := "<", "DESC"
	if thread != "" {
		op, order = ">", "ASC"
	}
	if q.Cursor != nil {
		where += ` AND (p.created_at` + op + `? OR (p.created_at=? AND p.post_id` + op + `?))`
		args = append(args, q.Cursor.At.UTC(), q.Cursor.At.UTC(), q.Cursor.ID)
	}
	args = append([]any{viewer, viewer}, args...)
	args = append(args, q.Limit+1)
	ordering := ` ORDER BY p.created_at ` + order + `,p.post_id ` + order
	keys := `SELECT p.post_id,p.event_id` + postJoin + where + ordering + ` LIMIT ?`
	return `SELECT ` + postPageColumns + ` FROM (` + firstOfEvent(keys, "post_id") + `) page JOIN discussion_posts p ON p.post_id=page.post_id JOIN accounts a ON a.account_id=p.account_id JOIN event_snapshots e ON e.event_id=p.event_id` + ordering, args, countSQL, countArgs
}

func (r *DiscussionRepository) List(ctx context.Context, viewer, event, thread string, q discussions.Query) ([]discussions.Post, int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args, countSQL, countArgs := discussionQueries(viewer, event, thread, q)
	var count int
	if err := r.db.QueryRowContext(ctx, countSQL, countArgs...).Scan(&count); err != nil {
		return nil, 0, safeError("discussion count", err)
	}
	items, ids, snapshots := []discussions.Post{}, []string{}, [][]byte{}
	var scanErr error
	err := queryRowsTx(ctx, r.db, query, args, func(rows *sql.Rows) error {
		var p discussions.Post
		data, err := scanPostRow(rows, &p)
		scanErr = err
		items, ids, snapshots = append(items, p), append(ids, p.EventID), append(snapshots, data)
		return err
	})
	if scanErr != nil {
		return nil, 0, scanErr
	}
	if err != nil {
		return nil, 0, safeError("discussion collection", err)
	}
	events, ok := pageEvents(ids, snapshots)
	if !ok {
		return nil, 0, discussions.ErrUnavailable
	}
	for i := range items {
		items[i].Event = events[items[i].EventID]
	}
	return items, count, nil
}

// venueTipCandidates bounds how many of a venue's most recent eligible
// contributions are ranked by Helpful, so ranking work stays fixed however
// long the venue's history grows. Older posts beyond it are not considered.
const venueTipCandidates = 200

// venueTipTimeout is the optional section's own budget; the page doesn't wait
// the full query timeout for it.
const venueTipTimeout = 750 * time.Millisecond

// VenueTips reads visible contributions on past events at the given venues
// (through the venue search facets), other than event itself: among the
// venueTipCandidates newest, most Helpful first, then newest. Events with an
// unannounced date never count as past. A date-only event is past once its
// local day ends; without a usable time zone, a day later than UTC's, as for
// open questions.
func (r *DiscussionRepository) VenueTips(ctx context.Context, viewer, event string, venues []string, limit int) ([]discussions.Post, error) {
	if len(venues) == 0 || limit < 1 {
		return []discussions.Post{}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, min(r.timeout, venueTipTimeout))
	defer cancel()
	args := []any{event}
	for _, v := range venues {
		args = append(args, v)
	}
	args = append(args, venueTipCandidates, limit)
	ids := []any{}
	err := queryRowsTx(ctx, r.db, `SELECT c.post_id FROM (SELECT p.post_id,p.created_at FROM discussion_posts p JOIN events ev ON ev.event_id=p.event_id
 WHERE p.removed_at IS NULL AND p.hidden_at IS NULL AND p.event_id<>?
 AND p.event_id IN (SELECT f.event_id FROM event_search_facets f WHERE f.kind='venue' AND f.value IN (`+placeholders(len(venues))+`))
 AND NOT ev.date_tba AND NOT ev.date_tbd
 AND (ev.start_utc<UTC_TIMESTAMP(6) OR (ev.start_utc IS NULL AND ev.local_date IS NOT NULL
  AND ev.local_date<COALESCE(DATE(CONVERT_TZ(UTC_TIMESTAMP(),'+00:00',ev.timezone)),UTC_DATE()-INTERVAL 1 DAY)))
 ORDER BY p.created_at DESC,p.post_id DESC LIMIT ?) c
 ORDER BY (SELECT COUNT(*) FROM post_helpful h WHERE h.post_id=c.post_id) DESC,c.created_at DESC,c.post_id DESC LIMIT ?`, args, func(rows *sql.Rows) error {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
		return nil
	})
	if err != nil {
		return nil, safeError("venue tips", err)
	}
	posts, err := readPosts(ctx, r.db, viewer, ids)
	if err != nil {
		return nil, err
	}
	tips := make([]discussions.Post, 0, len(ids))
	for _, id := range ids {
		tips = append(tips, posts[id.(string)])
	}
	return tips, nil
}

// NewReplies lists other people's visible replies in a root thread: up to
// MaxNewReplies created after since, and up to MaxNewReplies in the overlap
// before it. The overlap is the longer of LiveReplyOverlap and this
// repository's query timeout plus a second; every reply is written within
// that timeout, so none can commit too late for both the page and the check.
// The first statement checks the thread and window without touching replies;
// the second is two range reads on discussion_replies (root_id, created_at,
// post_id) in one statement, so the lists are one consistent set.
func (r *DiscussionRepository) NewReplies(ctx context.Context, viewer, event, thread string, since time.Time) (discussions.NewReplies, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	out := discussions.NewReplies{Replies: []discussions.NewReply{}, Recent: []discussions.NewReply{}}
	var root bool
	err := r.db.QueryRowContext(ctx, `SELECT UTC_TIMESTAMP(6),EXISTS(SELECT 1 FROM discussion_posts t WHERE t.post_id=? AND t.event_id=? AND t.root_id IS NULL)`, thread, event).Scan(&out.AsOf, &root)
	if err != nil {
		return out, safeError("live reply check", err)
	}
	if !root {
		return discussions.NewReplies{}, discussions.ErrNotFound
	}
	if since.After(out.AsOf.Add(time.Minute)) || since.Before(out.AsOf.Add(-discussions.LiveReplyWindow)) {
		return discussions.NewReplies{}, &accounts.ValidationError{Field: "since", Message: "reload the conversation to check for new replies"}
	}
	overlap := max(discussions.LiveReplyOverlap, r.timeout+time.Second)
	const visible = `FROM discussion_posts WHERE root_id=? AND removed_at IS NULL AND hidden_at IS NULL AND account_id<>?`
	err = queryRowsTx(ctx, r.db, `(SELECT 1 AS after_since,post_id,created_at `+visible+` AND created_at>? ORDER BY created_at,post_id LIMIT ?)
 UNION ALL (SELECT 0,post_id,created_at `+visible+` AND created_at>? AND created_at<=? ORDER BY created_at,post_id LIMIT ?)`,
		[]any{thread, viewer, since, discussions.MaxNewReplies + 1, thread, viewer, since.Add(-overlap), since, discussions.MaxNewReplies}, func(rows *sql.Rows) error {
			var after bool
			var reply discussions.NewReply
			if err := rows.Scan(&after, &reply.ID, &reply.CreatedAt); err != nil {
				return err
			}
			reply.CreatedAt = reply.CreatedAt.UTC()
			if after {
				out.Replies = append(out.Replies, reply)
			} else {
				out.Recent = append(out.Recent, reply)
			}
			return nil
		})
	if err != nil {
		return discussions.NewReplies{}, safeError("live reply check", err)
	}
	byCreation := func(a, b discussions.NewReply) int {
		return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), strings.Compare(a.ID, b.ID))
	}
	slices.SortFunc(out.Replies, byCreation)
	slices.SortFunc(out.Recent, byCreation)
	if len(out.Replies) > discussions.MaxNewReplies {
		out.Replies, out.More = out.Replies[:discussions.MaxNewReplies], true
	}
	return out, nil
}
