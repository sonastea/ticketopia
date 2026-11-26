package persistence

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/eventactivity"
)

type EventActivityRepository struct {
	db      *sql.DB
	timeout time.Duration
}

var _ eventactivity.Repository = (*EventActivityRepository)(nil)

func (p *Pool) EventActivity() *EventActivityRepository {
	return &EventActivityRepository{p.db, p.config.QueryTimeout}
}

// Two bounded page queries, not per-card lookups. Rank only publicly visible
// roots and active endorsements; hidden/removed text and withdrawn reasons never
// enter either preview or its count. Reads require no provider/snapshot lookup.
func (r *EventActivityRepository) Previews(ctx context.Context, ids []string) (map[string]eventactivity.Preview, error) {
	result := map[string]eventactivity.Preview{}
	if len(ids) == 0 {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	// Rank keys only, then look up text and authors for each event's winner:
	// joining every matching row's body and account before ranking widened the
	// sort and probed accounts for rows the preview discards. The recommendation
	// ranking reads only recommendations_active_event (a covering index).
	queries := []string{
		`SELECT k.event_id,p.post_id,p.body,a.account_id,a.display_name,IFNULL(p.via_client_name,''),k.total FROM (
		 SELECT p.event_id,p.post_id,
		 COUNT(*) OVER (PARTITION BY p.event_id) AS total,
		 ROW_NUMBER() OVER (PARTITION BY p.event_id ORDER BY p.created_at DESC,p.post_id DESC) AS row_position
		 FROM discussion_posts p
		 WHERE p.event_id IN (` + placeholders + `) AND p.root_id IS NULL AND p.removed_at IS NULL AND p.hidden_at IS NULL
		) k JOIN discussion_posts p ON p.post_id=k.post_id JOIN accounts a ON a.account_id=p.account_id WHERE k.row_position=1`,
		`SELECT k.event_id,'',r.reason,a.account_id,a.display_name,IFNULL(r.via_client_name,''),k.total FROM (
		 SELECT r.event_id,r.account_id,
		 COUNT(*) OVER (PARTITION BY r.event_id) AS total,
		 ROW_NUMBER() OVER (PARTITION BY r.event_id ORDER BY r.recommended_at DESC,r.account_id DESC) AS row_position
		 FROM event_recommendations r
		 WHERE r.event_id IN (` + placeholders + `) AND r.withdrawn_at IS NULL
		) k JOIN event_recommendations r ON r.event_id=k.event_id AND r.account_id=k.account_id JOIN accounts a ON a.account_id=r.account_id WHERE k.row_position=1`,
	}
	for kind, query := range queries {
		rows, err := r.db.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, safeError("event activity previews", err)
		}
		for rows.Next() {
			var id string
			var contribution eventactivity.Contribution
			var count int
			if err := rows.Scan(&id, &contribution.ID, &contribution.Body, &contribution.AuthorID, &contribution.AuthorName, &contribution.Via, &count); err != nil {
				rows.Close()
				return nil, safeError("event activity previews", err)
			}
			preview := result[id]
			if kind == 0 {
				preview.Questions, preview.Question = count, &contribution
			} else {
				preview.Recommendations, preview.Recommendation = count, &contribution
			}
			result[id] = preview
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, safeError("event activity previews", err)
		}
	}
	return result, nil
}
