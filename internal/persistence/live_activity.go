package persistence

import (
	"context"
	"database/sql"
	"strings"
	"sync/atomic"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
	"github.com/sonastea/ticketopia/internal/saved"
)

type ActivityRepository struct {
	db         *sql.DB
	timeout    time.Duration
	statements atomic.Uint64
}

func (p *Pool) Activity() *ActivityRepository {
	return &ActivityRepository{db: p.db, timeout: p.config.QueryTimeout}
}

// QueryCount counts attempted activity SELECT/DML statements, excluding
// transaction control. No SQL text or parameters enter telemetry.
func (r *ActivityRepository) QueryCount() uint64 { return r.statements.Load() }
func (r *ActivityRepository) Snapshot(ctx context.Context) (activity.Snapshot, error) {
	data, err := r.Candidates(ctx)
	result := activity.Snapshot{AsOf: data.AsOf, Items: []activity.Item{}}
	if err != nil {
		return result, err
	}
	for _, c := range data.Items[:min(20, len(data.Items))] {
		result.Items = append(result.Items, c.Item)
	}
	return result, nil
}

// Weight strings preserve MariaDB's literal Unicode collation, including accent
// and case equivalence. RTRIM implements the collation's PAD SPACE equality.
const activityLocationKey = `CONCAT(HEX(WEIGHT_STRING(RTRIM(community_city))),':',HEX(WEIGHT_STRING(RTRIM(community_country))))`

func (r *ActivityRepository) LocationKey(ctx context.Context, l activity.Location) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var key string
	r.statements.Add(1)
	err := r.db.QueryRowContext(ctx, `SELECT CONCAT(HEX(WEIGHT_STRING(RTRIM(CONVERT(? USING utf8mb4)) COLLATE utf8mb4_unicode_ci)),':',HEX(WEIGHT_STRING(RTRIM(CONVERT(? USING utf8mb4)) COLLATE utf8mb4_unicode_ci)))`, l.City, l.Country).Scan(&key)
	if err != nil {
		return "", activity.ErrUnavailable
	}
	return key, nil
}

func (r *ActivityRepository) Candidates(ctx context.Context) (activity.Candidates, error) {
	return r.RefreshCandidates(ctx, activity.Candidates{})
}

func (r *ActivityRepository) RefreshCandidates(ctx context.Context, previous activity.Candidates) (activity.Candidates, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if len(previous.Items) > activity.MaxCandidates {
		return activity.Candidates{}, activity.ErrCandidateLimit
	}
	prior := make(map[string]int, len(previous.Items))
	for i, c := range previous.Items {
		prior[c.Item.EventID] = i
	}
	changed := []int{}
	// Size from the previous generation and scan rows in place: regrowing and
	// copying ~10,000 escaping candidates dominated projection allocation.
	result := activity.Candidates{Items: make([]activity.Candidate, 0, min(max(len(previous.Items), 64), activity.MaxCandidates))}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return result, activity.ErrUnavailable
	}
	defer tx.Rollback()
	r.statements.Add(1)
	if err = tx.QueryRowContext(ctx, `SELECT UTC_TIMESTAMP(6)`).Scan(&result.AsOf); err != nil {
		return result, activity.ErrUnavailable
	}
	r.statements.Add(1)
	rows, err := tx.QueryContext(ctx, `SELECT g.event_id,g.n,g.latest,g.expiry,IF(g.n=1,a.activity_first_name,NULL),LEFT(e.activity_title,65537),LEFT(e.community_city,65537),IF(OCTET_LENGTH(e.activity_title)+OCTET_LENGTH(e.community_city)+OCTET_LENGTH(e.community_country)>65536,'',`+activityLocationKey+`),e.data_as_of
 FROM (SELECT l.event_id,COUNT(*) n,MAX(l.published_at) latest,MIN(l.expires_at) expiry,MIN(l.account_id) actor
 FROM live_recommendation_activity l JOIN event_recommendations r ON r.account_id=l.account_id AND r.event_id=l.event_id AND r.withdrawn_at IS NULL
 JOIN accounts a ON a.account_id=l.account_id
 WHERE l.expires_at>? AND l.published_at<=? GROUP BY l.event_id) g
 JOIN accounts a ON a.account_id=g.actor JOIN event_snapshots e ON e.event_id=g.event_id
	 ORDER BY g.latest DESC,g.event_id DESC LIMIT 10001`, result.AsOf, result.AsOf)
	if err != nil {
		return result, activity.ErrUnavailable
	}
	defer rows.Close()
	bytes := 0
	groups := 0
	var actor sql.NullString
	for rows.Next() {
		groups++
		if groups > activity.MaxCandidates {
			return activity.Candidates{}, activity.ErrCandidateLimit
		}
		result.Items = append(result.Items, activity.Candidate{})
		candidate := &result.Items[len(result.Items)-1]
		item := &candidate.Item
		if rows.Scan(&item.EventID, &item.RecentRecommendations, &item.LatestRecommendedAt, &item.ExpiresAt, &actor, &item.EventTitle, &item.City, &candidate.CityKey, &candidate.Revision) != nil {
			return result, activity.ErrUnavailable
		}
		// The empty key is an oversized-fact sentinel, never a truncated view.
		if candidate.CityKey == "" || len(item.EventTitle)+len(item.City)+len(candidate.CityKey) > activity.MaxFactBytes {
			return activity.Candidates{}, activity.ErrMemoryLimit
		}
		bytes += 1024 + 4*(len(item.EventID)+len(item.EventTitle)+len(item.City)+len(candidate.CityKey))
		if bytes > activity.MemoryBudget/4 {
			return activity.Candidates{}, activity.ErrMemoryLimit
		}
		if saved.ValidateID(item.EventID) != nil || strings.TrimSpace(item.EventTitle) == "" {
			result.Items = result.Items[:len(result.Items)-1]
			continue
		}
		if actor.Valid {
			name, err := accounts.ActivityFirstName(actor.String)
			if err == nil && name != "" {
				item.ActorFirstName = &name
			}
		}
		if i, ok := prior[item.EventID]; ok && previous.Items[i].Revision.Equal(candidate.Revision) {
			// Facets are projected only with a strictly newer accepted snapshot,
			// atomically with data_as_of. Never reuse the name-bearing Item.
			old := previous.Items[i]
			candidate.CategoryIDs, candidate.ArtistIDs, candidate.VenueIDs = old.CategoryIDs, old.ArtistIDs, old.VenueIDs
			for _, ids := range [][]string{candidate.CategoryIDs, candidate.ArtistIDs, candidate.VenueIDs} {
				for _, id := range ids {
					bytes += 128 + 4*len(id)
				}
			}
			if bytes > activity.MemoryBudget/4 {
				return activity.Candidates{}, activity.ErrMemoryLimit
			}
		} else {
			changed = append(changed, len(result.Items)-1)
		}
	}
	if rows.Err() != nil {
		return result, activity.ErrUnavailable
	}
	rows.Close()
	for start := 0; start < len(changed); start += 256 {
		end := min(start+256, len(changed))
		args := []any{}
		byID := map[string]int{}
		for _, i := range changed[start:end] {
			args = append(args, result.Items[i].Item.EventID)
			byID[result.Items[i].Item.EventID] = i
		}
		r.statements.Add(1)
		facets, err := tx.QueryContext(ctx, `SELECT event_id,kind,value FROM event_search_facets WHERE kind IN ('category','artist','venue') AND event_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+`) ORDER BY event_id,kind,value`, args...)
		if err != nil {
			return activity.Candidates{}, activity.ErrUnavailable
		}
		for facets.Next() {
			var id, kind, value string
			if facets.Scan(&id, &kind, &value) != nil {
				facets.Close()
				return activity.Candidates{}, activity.ErrUnavailable
			}
			bytes += 128 + 4*len(value)
			if bytes > activity.MemoryBudget/4 {
				facets.Close()
				return activity.Candidates{}, activity.ErrMemoryLimit
			}
			i := byID[id]
			switch kind {
			case "category":
				result.Items[i].CategoryIDs = append(result.Items[i].CategoryIDs, value)
			case "artist":
				result.Items[i].ArtistIDs = append(result.Items[i].ArtistIDs, value)
			case "venue":
				result.Items[i].VenueIDs = append(result.Items[i].VenueIDs, value)
			}
		}
		err = facets.Err()
		facets.Close()
		if err != nil {
			return activity.Candidates{}, activity.ErrUnavailable
		}
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
	r.statements.Add(1)
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
		r.statements.Add(1)
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
	r.statements.Add(1)
	_, err := r.db.ExecContext(ctx, `DELETE FROM live_recommendation_activity WHERE expires_at<=UTC_TIMESTAMP(6) ORDER BY expires_at LIMIT 500`)
	if err != nil {
		return activity.ErrUnavailable
	}
	return nil
}
