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
	queries := []string{
		`SELECT event_id,post_id,body,account_id,display_name,total FROM (
		 SELECT p.event_id,p.post_id,p.body,a.account_id,a.display_name,
		 COUNT(*) OVER (PARTITION BY p.event_id) AS total,
		 ROW_NUMBER() OVER (PARTITION BY p.event_id ORDER BY p.created_at DESC,p.post_id DESC) AS row_position
		 FROM discussion_posts p JOIN accounts a ON a.account_id=p.account_id
		 WHERE p.event_id IN (` + placeholders + `) AND p.root_id IS NULL AND p.removed_at IS NULL AND p.hidden_at IS NULL
		) visible WHERE row_position=1`,
		`SELECT event_id,'',reason,account_id,display_name,total FROM (
		 SELECT r.event_id,r.reason,a.account_id,a.display_name,
		 COUNT(*) OVER (PARTITION BY r.event_id) AS total,
		 ROW_NUMBER() OVER (PARTITION BY r.event_id ORDER BY r.recommended_at DESC,r.account_id DESC) AS row_position
		 FROM event_recommendations r JOIN accounts a ON a.account_id=r.account_id
		 WHERE r.event_id IN (` + placeholders + `) AND r.withdrawn_at IS NULL
		) active WHERE row_position=1`,
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
			if err := rows.Scan(&id, &contribution.ID, &contribution.Body, &contribution.AuthorID, &contribution.AuthorName, &count); err != nil {
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
