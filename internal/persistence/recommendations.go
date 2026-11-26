package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
)

var _ recommendations.Repository = (*RecommendationRepository)(nil)

type RecommendationRepository struct {
	db           *sql.DB
	timeout      time.Duration
	liveActivity bool
}

func (p *Pool) Recommendations() *RecommendationRepository {
	return &RecommendationRepository{db: p.db, timeout: p.config.QueryTimeout}
}

func (r *RecommendationRepository) EnableLiveActivity(enabled bool) { r.liveActivity = enabled }

const recommendationColumns = `e.snapshot,` + recommendationFields

// recommendationPageColumns read page rows carried through the limited keys,
// plus the event ID. Only one row per event looks up its snapshot (see
// firstOfEvent), so neither the sort nor the result repeats it.
const recommendationPageColumns = `IF(page.first,(SELECT s.snapshot FROM event_snapshots s WHERE s.event_id=page.event_id),NULL),page.data_as_of,page.account_id,page.display_name,page.bio,page.reason,page.recommended_at,page.updated_at,page.via,page.via_host,page.event_id`
const recommendationFields = `e.data_as_of,a.account_id,a.display_name,a.bio,r.reason,r.recommended_at,r.updated_at,COALESCE(r.via_client_name,'') AS via,COALESCE(r.via_client_host,'') AS via_host`
const recommendationJoin = ` FROM event_recommendations r JOIN event_snapshots e ON e.event_id=r.event_id JOIN accounts a ON a.account_id=r.account_id`

func scanRecommendation(row interface{ Scan(...any) error }) (recommendations.Item, error) {
	var item recommendations.Item
	snapshot, err := scanRecommendationRow(row, &item)
	if err != nil {
		return item, err
	}
	if json.Unmarshal(snapshot, &item.Event) != nil {
		return item, recommendations.ErrUnavailable
	}
	return item, nil
}

// scanRecommendationRow scans into item, returning the snapshot undecoded, and
// scans any columns after recommendationFields into extra.
func scanRecommendationRow(row interface{ Scan(...any) error }, item *recommendations.Item, extra ...any) ([]byte, error) {
	var snapshot []byte
	err := row.Scan(append([]any{&snapshot, &item.Meta.DataAsOf, &item.Profile.ID, &item.Profile.DisplayName, &item.Profile.Bio, &item.Reason, &item.RecommendedAt, &item.UpdatedAt, &item.Via, &item.ViaHost}, extra...)...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, recommendations.ErrNotFound
	}
	if err != nil {
		return nil, safeError("recommendation read", err)
	}
	item.Meta.Stale = true
	return snapshot, nil
}
func (r *RecommendationRepository) Get(ctx context.Context, owner, id string) (recommendations.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return scanRecommendation(r.db.QueryRowContext(ctx, `SELECT `+recommendationColumns+recommendationJoin+` WHERE r.account_id=? AND r.event_id=? AND r.withdrawn_at IS NULL`, owner, id))
}

// Set publishes or edits the account's own words, without app attribution.
func (r *RecommendationRepository) Set(ctx context.Context, owner string, detail models.EventDetail, reason string) (recommendations.Item, bool, error) {
	return r.Write(ctx, owner, detail, recommendations.Write{Reason: reason})
}

func (r *RecommendationRepository) Write(ctx context.Context, owner string, detail models.EventDetail, w recommendations.Write) (recommendations.Item, bool, error) {
	reason, err := recommendations.Reason(w.Reason)
	if err != nil {
		return recommendations.Item{}, false, err
	}
	w.Reason = reason
	args, data, err := prepareSnapshot(detail)
	if err != nil {
		return recommendations.Item{}, false, recommendations.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	for attempt := range 3 {
		item, created, retry, err := r.setOnce(ctx, owner, detail, w, args, data)
		if err == nil {
			return item, created, nil
		}
		if errors.Is(err, recommendations.ErrChanged) || errors.Is(err, recommendations.ErrDraftGone) || errors.Is(err, recommendations.ErrPersonal) {
			return recommendations.Item{}, false, err
		}
		if !retry || attempt == 2 {
			return recommendations.Item{}, false, safeError("recommendation write", err)
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return recommendations.Item{}, false, safeError("recommendation write", ctx.Err())
		case <-timer.C:
		}
	}
	panic("unreachable")
}

// viaValue stores an empty app name as NULL: a person's own writing.
func viaValue(via string) any {
	if via == "" {
		return nil
	}
	return via
}

func (r *RecommendationRepository) setOnce(ctx context.Context, owner string, detail models.EventDetail, w recommendations.Write, args []any, data []byte) (recommendations.Item, bool, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return recommendations.Item{}, false, retryable(err), err
	}
	defer tx.Rollback()
	// Lock the stable event/snapshot before its endorsement, matching save and
	// interest transactions. This serializes new account/event rows without an
	// account-wide lock that could deadlock with those independent actions.
	if err := upsertSnapshotTx(ctx, tx, detail, args, data); err != nil {
		return recommendations.Item{}, false, retryable(err), err
	}
	var active, personal bool
	var current string
	err = tx.QueryRowContext(ctx, `SELECT withdrawn_at IS NULL,reason,via_client_name IS NULL FROM event_recommendations WHERE account_id=? AND event_id=? FOR UPDATE`, owner, detail.Item.ID).Scan(&active, &current, &personal)
	first := errors.Is(err, sql.ErrNoRows)
	if err != nil && !first {
		return recommendations.Item{}, false, retryable(err), err
	}
	created := first || !active
	// The person's review must still describe the row, checked under its lock.
	if e := w.Expect; e != nil && !e.Matches(!created, current) {
		return recommendations.Item{}, false, false, recommendations.ErrChanged
	}
	if w.KeepPersonal && !created && personal && current != "" {
		return recommendations.Item{}, false, false, recommendations.ErrPersonal
	}
	// The draft is consumed with the publication, or nothing happens. Lock the
	// account first, as sign-out-everywhere does before cascading into drafts,
	// so the two never wait on each other in opposite orders.
	if w.Draft != "" {
		var account string
		if err := tx.QueryRowContext(ctx, `SELECT account_id FROM accounts WHERE account_id=? FOR UPDATE`, owner).Scan(&account); err != nil {
			return recommendations.Item{}, false, retryable(err), err
		}
		result, err := tx.ExecContext(ctx, `DELETE FROM agent_drafts WHERE account_id=? AND draft_id=? AND expires_at>UTC_TIMESTAMP(6)`, owner, w.Draft)
		if err != nil {
			return recommendations.Item{}, false, retryable(err), err
		}
		if n, err := result.RowsAffected(); err != nil || n != 1 {
			if err == nil {
				err = recommendations.ErrDraftGone
			}
			return recommendations.Item{}, false, false, err
		}
	}
	if !created && w.KeepExisting {
		// Under the row lock: a concurrent edit is never overwritten.
		item, err := scanRecommendation(tx.QueryRowContext(ctx, `SELECT `+recommendationColumns+recommendationJoin+` WHERE r.account_id=? AND r.event_id=?`, owner, detail.Item.ID))
		if err != nil {
			return recommendations.Item{}, false, false, err
		}
		return item, false, false, tx.Commit()
	}
	// Binary comparison detects case-only edits and ignores idempotent retries.
	// Attribution changes with the content it describes, so it is assigned
	// first: later assignments see the updated reason and withdrawn_at.
	_, err = tx.ExecContext(ctx, `INSERT INTO event_recommendations (account_id,event_id,reason,via_client_name,via_client_host) VALUES (?,?,?,?,?) ON DUPLICATE KEY UPDATE via_client_name=IF(withdrawn_at IS NOT NULL OR BINARY reason<>BINARY VALUES(reason),VALUES(via_client_name),via_client_name),via_client_host=IF(withdrawn_at IS NOT NULL OR BINARY reason<>BINARY VALUES(reason),VALUES(via_client_host),via_client_host),updated_at=IF(withdrawn_at IS NOT NULL OR BINARY reason<>BINARY VALUES(reason),UTC_TIMESTAMP(6),updated_at),reason=VALUES(reason),withdrawn_at=NULL`, owner, detail.Item.ID, w.Reason, viaValue(w.Via), viaValue(w.ViaHost))
	if err != nil {
		return recommendations.Item{}, false, retryable(err), err
	}
	item, err := scanRecommendation(tx.QueryRowContext(ctx, `SELECT `+recommendationColumns+recommendationJoin+` WHERE r.account_id=? AND r.event_id=?`, owner, detail.Item.ID))
	if err != nil {
		return recommendations.Item{}, false, false, err
	}
	if first && r.liveActivity {
		if _, err := tx.ExecContext(ctx, `INSERT INTO live_recommendation_activity (account_id,event_id,published_at,expires_at) SELECT account_id,event_id,recommended_at,recommended_at+INTERVAL 10 MINUTE FROM event_recommendations WHERE account_id=? AND event_id=?`, owner, detail.Item.ID); err != nil {
			return recommendations.Item{}, false, retryable(err), err
		}
	}
	if created {
		if _, err := tx.ExecContext(ctx, `INSERT INTO recommendation_feed_events (event_id,first_recommended_at) VALUES (?,?) ON DUPLICATE KEY UPDATE event_id=VALUES(event_id)`, detail.Item.ID, item.RecommendedAt); err != nil {
			return recommendations.Item{}, false, retryable(err), err
		}
		observation, err := recordRecommendationActivity(ctx, tx, owner, first)
		if err != nil {
			return recommendations.Item{}, false, retryable(err), err
		}
		item.Observation = &observation
	}
	if err := tx.Commit(); err != nil {
		return recommendations.Item{}, false, false, err
	}
	invalidateDiscoverCounts()
	return item, created, false, nil
}
func recordRecommendationActivity(ctx context.Context, tx *sql.Tx, owner string, first bool) (recommendations.Observation, error) {
	newCount, reactivationCount := 0, 1
	if first {
		newCount, reactivationCount = 1, 0
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO recommendation_activity (account_id,window_start,new_publications,reactivations,total_new_publications,total_reactivations,last_published_at)
 VALUES (?,FROM_UNIXTIME(FLOOR(UNIX_TIMESTAMP(UTC_TIMESTAMP(6))/600)*600),?,?,?,?,UTC_TIMESTAMP(6))
 ON DUPLICATE KEY UPDATE
 new_publications=IF(window_start=VALUES(window_start),new_publications+VALUES(new_publications),VALUES(new_publications)),
 reactivations=IF(window_start=VALUES(window_start),reactivations+VALUES(reactivations),VALUES(reactivations)),
 total_new_publications=total_new_publications+VALUES(total_new_publications),
 total_reactivations=total_reactivations+VALUES(total_reactivations),window_start=VALUES(window_start),last_published_at=VALUES(last_published_at)`, owner, newCount, reactivationCount, newCount, reactivationCount)
	if err != nil {
		return recommendations.Observation{}, err
	}
	var o recommendations.Observation
	err = tx.QueryRowContext(ctx, `SELECT window_start,new_publications,reactivations,total_new_publications,total_reactivations FROM recommendation_activity WHERE account_id=?`, owner).Scan(&o.WindowStart, &o.NewPublications, &o.Reactivations, &o.TotalNewPublications, &o.TotalReactivations)
	return o, err
}
func (r *RecommendationRepository) Remove(ctx context.Context, owner, id string) error {
	_, err := r.withdraw(ctx, owner, id, false)
	return err
}

// WithdrawAttributed withdraws the recommendation only while it is
// attributed to an app and has no reason, in the same statement, so a
// concurrent edit by the person is never erased. It reports whether it
// withdrew one.
func (r *RecommendationRepository) WithdrawAttributed(ctx context.Context, owner, id string) (bool, error) {
	return r.withdraw(ctx, owner, id, true)
}

func (r *RecommendationRepository) withdraw(ctx context.Context, owner, id string, attributedOnly bool) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, safeError("recommendation withdrawal", err)
	}
	defer tx.Rollback()
	query := `UPDATE event_recommendations SET reason='',via_client_name=NULL,via_client_host=NULL,updated_at=UTC_TIMESTAMP(6),withdrawn_at=UTC_TIMESTAMP(6) WHERE account_id=? AND event_id=? AND withdrawn_at IS NULL`
	if attributedOnly {
		query += ` AND via_client_name IS NOT NULL AND reason=''`
	}
	result, err := tx.ExecContext(ctx, query, owner, id)
	if err != nil {
		return false, safeError("recommendation withdrawal", err)
	}
	withdrawn, err := result.RowsAffected()
	if err != nil {
		return false, safeError("recommendation withdrawal", err)
	}
	if withdrawn > 0 || !attributedOnly {
		if _, err := tx.ExecContext(ctx, `DELETE FROM live_recommendation_activity WHERE account_id=? AND event_id=?`, owner, id); err != nil {
			return false, safeError("recommendation withdrawal", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return false, safeError("recommendation withdrawal", err)
	}
	invalidateDiscoverCounts()
	return withdrawn > 0, nil
}
func (r *RecommendationRepository) Count(ctx context.Context, id string) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var count int
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_recommendations WHERE event_id=? AND withdrawn_at IS NULL`, id).Scan(&count)
	if err != nil {
		return 0, safeError("recommendation count", err)
	}
	return count, nil
}
func (r *RecommendationRepository) List(ctx context.Context, owner, event string, q recommendations.Query) ([]recommendations.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return listRecommendations(ctx, r.db, owner, event, q)
}

// EventList reads an event's page and its total from one committed state, so
// a concurrent withdrawal cannot leave the total smaller than the page.
func (r *RecommendationRepository) EventList(ctx context.Context, event string, q recommendations.Query) ([]recommendations.Item, int, error) {
	return r.eventList(ctx, event, q, nil)
}

// afterPage, when set, runs between the page and count reads.
func (r *RecommendationRepository) eventList(ctx context.Context, event string, q recommendations.Query, afterPage func(context.Context) error) ([]recommendations.Item, int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, 0, safeError("recommendation collection", err)
	}
	defer tx.Rollback()
	items, err := listRecommendations(ctx, tx, "", event, q)
	if err != nil {
		return nil, 0, err
	}
	if afterPage != nil {
		if err := afterPage(ctx); err != nil {
			return nil, 0, err
		}
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_recommendations WHERE event_id=? AND withdrawn_at IS NULL`, event).Scan(&count); err != nil {
		return nil, 0, safeError("recommendation count", err)
	}
	return items, count, nil
}

func listRecommendations(ctx context.Context, db interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, owner, event string, q recommendations.Query) ([]recommendations.Item, error) {
	query := `SELECT ` + recommendationFields + `,r.event_id` + recommendationJoin + ` WHERE r.withdrawn_at IS NULL`
	args := []any{}
	if owner != "" {
		query += ` AND r.account_id=?`
		args = append(args, owner)
	}
	if event != "" {
		query += ` AND r.event_id=?`
		args = append(args, event)
	}
	filters, filterArgs := communityFilters(q.City, q.Country, q.CategoryID)
	query += filters
	args = append(args, filterArgs...)
	if q.Cursor != nil {
		query += ` AND (r.recommended_at<? OR (r.recommended_at=? AND (r.event_id<? OR (r.event_id=? AND r.account_id<?))))`
		args = append(args, q.Cursor.Before.UTC(), q.Cursor.Before.UTC(), q.Cursor.EventID, q.Cursor.EventID, q.Cursor.AccountID)
	}
	query = `SELECT ` + recommendationPageColumns + ` FROM (` + firstOfEvent(query+` ORDER BY r.recommended_at DESC,r.event_id DESC,r.account_id DESC LIMIT ?`, "k.*") + `) page ORDER BY page.recommended_at DESC,page.event_id DESC,page.account_id DESC`
	args = append(args, q.Limit+1)
	items, ids, snapshots := []recommendations.Item{}, []string{}, [][]byte{}
	var scanErr error
	err := queryRowsTx(ctx, db, query, args, func(rows *sql.Rows) error {
		var id string
		var item recommendations.Item
		data, err := scanRecommendationRow(rows, &item, &id)
		scanErr = err
		items, ids, snapshots = append(items, item), append(ids, id), append(snapshots, data)
		return err
	})
	if scanErr != nil {
		return nil, scanErr
	}
	if err != nil {
		return nil, safeError("recommendation collection", err)
	}
	events, ok := pageEvents(ids, snapshots)
	if !ok {
		return nil, recommendations.ErrUnavailable
	}
	for i := range items {
		items[i].Event = events[ids[i]]
	}
	return items, nil
}
