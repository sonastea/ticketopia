package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

type ActivityRepository struct {
	db      *sql.DB
	timeout time.Duration
}

func (p *Pool) Activity() *ActivityRepository {
	return &ActivityRepository{p.db, p.config.QueryTimeout}
}
func (r *ActivityRepository) Snapshot(ctx context.Context) (activity.Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	result := activity.Snapshot{Items: []activity.Item{}}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return result, activity.ErrUnavailable
	}
	defer tx.Rollback()
	if err = tx.QueryRowContext(ctx, `SELECT UTC_TIMESTAMP(6)`).Scan(&result.AsOf); err != nil {
		return result, activity.ErrUnavailable
	}
	rows, err := tx.QueryContext(ctx, `SELECT g.event_id,g.n,g.latest,g.expiry,IF(g.n=1,a.activity_first_name,NULL),e.snapshot
 FROM (SELECT l.event_id,COUNT(*) n,MAX(l.published_at) latest,MIN(l.expires_at) expiry,MIN(l.account_id) actor
 FROM live_recommendation_activity l JOIN event_recommendations r ON r.account_id=l.account_id AND r.event_id=l.event_id AND r.withdrawn_at IS NULL
 JOIN accounts a ON a.account_id=l.account_id
 WHERE l.expires_at>? AND l.published_at<=? GROUP BY l.event_id) g
 JOIN accounts a ON a.account_id=g.actor JOIN event_snapshots e ON e.event_id=g.event_id
 ORDER BY g.latest DESC,g.event_id DESC LIMIT 20`, result.AsOf, result.AsOf)
	if err != nil {
		return result, activity.ErrUnavailable
	}
	defer rows.Close()
	for rows.Next() {
		var item activity.Item
		var raw []byte
		if rows.Scan(&item.EventID, &item.RecentRecommendations, &item.LatestRecommendedAt, &item.ExpiresAt, &item.ActorFirstName, &raw) != nil {
			return result, activity.ErrUnavailable
		}
		var event models.Event
		if json.Unmarshal(raw, &event) != nil || event.ID != item.EventID || saved.ValidateID(item.EventID) != nil || strings.TrimSpace(event.Name) == "" {
			continue
		}
		item.EventTitle = event.Name
		if len(event.Venues) > 0 {
			item.City = event.Venues[0].City
		} else if event.Place != nil {
			item.City = event.Place.City
		}
		if item.ActorFirstName != nil {
			name, err := accounts.ActivityFirstName(*item.ActorFirstName)
			item.ActorFirstName = nil
			if err == nil && name != "" {
				item.ActorFirstName = &name
			}
		}
		result.Items = append(result.Items, item)
	}
	if rows.Err() != nil {
		return result, activity.ErrUnavailable
	}
	if tx.Commit() != nil {
		return result, activity.ErrUnavailable
	}
	return result, nil
}
func (r *ActivityRepository) Counts(ctx context.Context, ids []string) (activity.Counts, error) {
	result := activity.Counts{Items: []activity.Count{}}
	if len(ids) > 101 {
		return result, activity.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return result, activity.ErrUnavailable
	}
	defer tx.Rollback()
	if tx.QueryRowContext(ctx, `SELECT UTC_TIMESTAMP(6)`).Scan(&result.AsOf) != nil {
		return result, activity.ErrUnavailable
	}
	if len(ids) == 0 {
		return result, nil
	}
	args := make([]any, len(ids))
	slots := make([]string, len(ids))
	byID := map[string]*activity.Count{}
	for i, id := range ids {
		args[i] = id
		slots[i] = "?"
		result.Items = append(result.Items, activity.Count{EventID: id})
	}
	for i := range result.Items {
		byID[result.Items[i].EventID] = &result.Items[i]
	}
	for _, table := range []string{"event_recommendations", "event_interests"} {
		query := `SELECT event_id,COUNT(*) FROM ` + table + ` WHERE event_id IN (` + strings.Join(slots, ",") + `)`
		if table == "event_recommendations" {
			query += ` AND withdrawn_at IS NULL`
		}
		query += ` GROUP BY event_id`
		rows, err := tx.QueryContext(ctx, query, args...)
		if err != nil {
			return result, activity.ErrUnavailable
		}
		for rows.Next() {
			var id string
			var n int
			if rows.Scan(&id, &n) != nil {
				rows.Close()
				return result, activity.ErrUnavailable
			}
			if c := byID[id]; c != nil {
				if table == "event_recommendations" {
					c.RecommendationCount = n
				} else {
					c.InterestedCount = n
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return result, activity.ErrUnavailable
		}
	}
	if tx.Commit() != nil {
		return result, activity.ErrUnavailable
	}
	return result, nil
}
func (r *ActivityRepository) Cleanup(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, `DELETE FROM live_recommendation_activity WHERE expires_at<=UTC_TIMESTAMP(6) ORDER BY expires_at LIMIT 500`)
	if err != nil {
		return activity.ErrUnavailable
	}
	return nil
}
