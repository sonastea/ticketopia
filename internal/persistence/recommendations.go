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
	db      *sql.DB
	timeout time.Duration
}

func (p *Pool) Recommendations() *RecommendationRepository {
	return &RecommendationRepository{p.db, p.config.QueryTimeout}
}

const recommendationColumns = `e.snapshot,e.data_as_of,a.account_id,a.display_name,a.bio,r.reason,r.recommended_at,r.updated_at`
const recommendationJoin = ` FROM event_recommendations r JOIN event_snapshots e ON e.event_id=r.event_id JOIN accounts a ON a.account_id=r.account_id`

func scanRecommendation(row interface{ Scan(...any) error }) (recommendations.Item, error) {
	var item recommendations.Item
	var snapshot []byte
	err := row.Scan(&snapshot, &item.Meta.DataAsOf, &item.Profile.ID, &item.Profile.DisplayName, &item.Profile.Bio, &item.Reason, &item.RecommendedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, recommendations.ErrNotFound
	}
	if err != nil {
		return item, safeError("recommendation read", err)
	}
	if json.Unmarshal(snapshot, &item.Event) != nil {
		return item, recommendations.ErrUnavailable
	}
	item.Meta.Stale = true
	return item, nil
}
func (r *RecommendationRepository) Get(ctx context.Context, owner, id string) (recommendations.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return scanRecommendation(r.db.QueryRowContext(ctx, `SELECT `+recommendationColumns+recommendationJoin+` WHERE r.account_id=? AND r.event_id=? AND r.withdrawn_at IS NULL`, owner, id))
}
func (r *RecommendationRepository) Set(ctx context.Context, owner string, detail models.EventDetail, reason string) (recommendations.Item, bool, error) {
	reason, err := recommendations.Reason(reason)
	if err != nil {
		return recommendations.Item{}, false, err
	}
	args, data, err := prepareSnapshot(detail)
	if err != nil {
		return recommendations.Item{}, false, recommendations.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	for attempt := range 3 {
		item, created, retry, err := r.setOnce(ctx, owner, detail, reason, args, data)
		if err == nil {
			return item, created, nil
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
func (r *RecommendationRepository) setOnce(ctx context.Context, owner string, detail models.EventDetail, reason string, args []any, data []byte) (recommendations.Item, bool, bool, error) {
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
	var active bool
	err = tx.QueryRowContext(ctx, `SELECT withdrawn_at IS NULL FROM event_recommendations WHERE account_id=? AND event_id=? FOR UPDATE`, owner, detail.Item.ID).Scan(&active)
	first := errors.Is(err, sql.ErrNoRows)
	if err != nil && !first {
		return recommendations.Item{}, false, retryable(err), err
	}
	created := first || !active
	// Binary comparison detects case-only edits and ignores idempotent retries.
	_, err = tx.ExecContext(ctx, `INSERT INTO event_recommendations (account_id,event_id,reason) VALUES (?,?,?) ON DUPLICATE KEY UPDATE updated_at=IF(withdrawn_at IS NOT NULL OR BINARY reason<>BINARY VALUES(reason),UTC_TIMESTAMP(6),updated_at),reason=VALUES(reason),withdrawn_at=NULL`, owner, detail.Item.ID, reason)
	if err != nil {
		return recommendations.Item{}, false, retryable(err), err
	}
	item, err := scanRecommendation(tx.QueryRowContext(ctx, `SELECT `+recommendationColumns+recommendationJoin+` WHERE r.account_id=? AND r.event_id=?`, owner, detail.Item.ID))
	if err != nil {
		return recommendations.Item{}, false, false, err
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
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, `UPDATE event_recommendations SET reason='',updated_at=UTC_TIMESTAMP(6),withdrawn_at=UTC_TIMESTAMP(6) WHERE account_id=? AND event_id=? AND withdrawn_at IS NULL`, owner, id)
	if err != nil {
		return safeError("recommendation withdrawal", err)
	}
	return nil
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
	query := `SELECT ` + recommendationColumns + recommendationJoin + ` WHERE r.withdrawn_at IS NULL`
	args := []any{}
	if owner != "" {
		query += ` AND r.account_id=?`
		args = append(args, owner)
	}
	if event != "" {
		query += ` AND r.event_id=?`
		args = append(args, event)
	}
	// Scope reads use the authoritative last-known snapshot, not client-supplied
	// labels or provider calls. '%' and '_' in city names are literal, not wildcards.
	for _, filter := range []struct{ value, path, fallback string }{{q.City, "$.venues[0].city", "$.place.city"}, {q.Country, "$.venues[0].country_code", "$.place.country_code"}} {
		if filter.value != "" {
			query += ` AND COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(e.snapshot,?)),'null'),NULLIF(JSON_UNQUOTE(JSON_EXTRACT(e.snapshot,?)),'null'),'') COLLATE utf8mb4_unicode_ci=?`
			args = append(args, filter.path, filter.fallback, filter.value)
		}
	}
	if q.CategoryID != "" {
		query += ` AND JSON_CONTAINS(e.snapshot,JSON_OBJECT('segment',JSON_OBJECT('id',?)),'$.classifications')=1`
		args = append(args, q.CategoryID)
	}
	if q.Cursor != nil {
		query += ` AND (r.recommended_at<? OR (r.recommended_at=? AND (r.event_id<? OR (r.event_id=? AND r.account_id<?))))`
		args = append(args, q.Cursor.Before.UTC(), q.Cursor.Before.UTC(), q.Cursor.EventID, q.Cursor.EventID, q.Cursor.AccountID)
	}
	query += ` ORDER BY r.recommended_at DESC,r.event_id DESC,r.account_id DESC LIMIT ?`
	args = append(args, q.Limit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, safeError("recommendation collection", err)
	}
	defer rows.Close()
	items := []recommendations.Item{}
	for rows.Next() {
		item, err := scanRecommendation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("recommendation collection", err)
	}
	return items, nil
}
