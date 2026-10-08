package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/sonastea/ticketopia/internal/recommendations"
)

func (r *RecommendationRepository) Community(ctx context.Context, q recommendations.Query) ([]recommendations.EventGroup, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	// Counts, snapshots, and author previews must describe the same committed
	// state, including when another member withdraws while this page is loading.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return nil, safeError("recommendation feed", err)
	}
	defer tx.Rollback()
	query := `SELECT e.snapshot,e.data_as_of,f.first_recommended_at,
 (SELECT COUNT(*) FROM event_recommendations active WHERE active.event_id=f.event_id AND active.withdrawn_at IS NULL)
 FROM recommendation_feed_events f JOIN event_snapshots e ON e.event_id=f.event_id
 WHERE EXISTS(SELECT 1 FROM event_recommendations active WHERE active.event_id=f.event_id AND active.withdrawn_at IS NULL)`
	args := []any{}
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
	if c := q.FeedCursor; c != nil {
		query += ` AND (f.first_recommended_at<? OR (f.first_recommended_at=? AND f.event_id<?))`
		args = append(args, c.Before.UTC(), c.Before.UTC(), c.EventID)
	}
	query += ` ORDER BY f.first_recommended_at DESC,f.event_id DESC LIMIT ?`
	args = append(args, q.Limit+1)
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, safeError("recommendation feed", err)
	}
	items := []recommendations.EventGroup{}
	positions := map[string]int{}
	ids := []any{}
	for rows.Next() {
		var g recommendations.EventGroup
		var data []byte
		if err = rows.Scan(&data, &g.Meta.DataAsOf, &g.FirstRecommendedAt, &g.Count); err != nil {
			rows.Close()
			return nil, safeError("recommendation feed", err)
		}
		if json.Unmarshal(data, &g.Event) != nil {
			rows.Close()
			return nil, recommendations.ErrUnavailable
		}
		g.Meta.Stale = true
		g.Recommendations = []recommendations.Item{}
		positions[g.Event.ID] = len(items)
		ids = append(ids, g.Event.ID)
		items = append(items, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, safeError("recommendation feed", err)
	}
	if len(items) == 0 {
		if err = tx.Commit(); err != nil {
			return nil, safeError("recommendation feed", err)
		}
		return items, nil
	}
	// Batch three-author previews, never a query per card or an unbounded author list.
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	query = `SELECT ` + recommendationColumns + ` FROM
 (SELECT source.*,ROW_NUMBER() OVER(PARTITION BY source.event_id ORDER BY source.recommended_at DESC,source.account_id DESC) AS position
 FROM event_recommendations source WHERE source.withdrawn_at IS NULL AND source.event_id IN (` + placeholders + `)) r
 JOIN event_snapshots e ON e.event_id=r.event_id JOIN accounts a ON a.account_id=r.account_id WHERE r.position<=? ORDER BY r.event_id,r.position`
	ids = append(ids, recommendations.PreviewLimit)
	rows, err = tx.QueryContext(ctx, query, ids...)
	if err != nil {
		return nil, safeError("recommendation previews", err)
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanRecommendation(rows)
		if err != nil {
			return nil, err
		}
		i := positions[item.Event.ID]
		items[i].Recommendations = append(items[i].Recommendations, item)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError("recommendation previews", err)
	}
	rows.Close()
	if err = tx.Commit(); err != nil {
		return nil, safeError("recommendation feed", err)
	}
	return items, nil
}
