package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/sonastea/ticketopia/internal/useractivity"
)

var _ useractivity.Repository = (*UserActivityRepository)(nil)

type UserActivityRepository struct {
	db      *sql.DB
	timeout time.Duration
}

func (p *Pool) UserActivity() *UserActivityRepository {
	return &UserActivityRepository{p.db, p.config.QueryTimeout}
}

// userActivitySource is one public contribution table. Each is read through an
// owner-leading index in its own newest-first order and limited before the
// merge, so a page reads at most three bounded index ranges.
type userActivitySource struct {
	kind, at, key, sql string
}

var userActivitySources = []userActivitySource{
	{useractivity.TypeDiscussionPost, "p.created_at", "p.post_id",
		`SELECT 1 AS item_rank,p.created_at AS at,p.post_id AS item_key,p.event_id,p.body,COALESCE(p.root_id,p.post_id) AS thread_id,COALESCE(p.parent_id,'') AS parent_id,p.updated_at,COALESCE(p.via_client_name,'') AS via,COALESCE(p.via_client_host,'') AS via_host
		 FROM discussion_posts p WHERE p.account_id=? AND p.removed_at IS NULL AND p.hidden_at IS NULL`},
	{useractivity.TypeRecommendation, "r.recommended_at", "r.event_id",
		`SELECT 2,r.recommended_at,r.event_id,r.event_id,r.reason,'','',r.updated_at,COALESCE(r.via_client_name,''),COALESCE(r.via_client_host,'')
		 FROM event_recommendations r WHERE r.account_id=? AND r.withdrawn_at IS NULL`},
	{useractivity.TypeInterest, "i.interested_at", "i.event_id",
		`SELECT 0,i.interested_at,i.event_id,i.event_id,'','','',i.interested_at,'',''
		 FROM event_interests i WHERE i.account_id=? AND i.visibility='public'`},
}

// List merges the account's public sources by (time, type rank, key). Removed
// or hidden posts, withdrawn recommendations and private interest never enter
// a page; a reply stays listed when its thread's root is removed or hidden,
// matching the thread view. Reads need no provider or snapshot refresh.
func (r *UserActivityRepository) List(ctx context.Context, owner string, q useractivity.Query) ([]useractivity.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query := `SELECT a.item_rank,a.at,a.item_key,a.body,a.thread_id,a.parent_id,a.updated_at,a.via,a.via_host,e.snapshot,e.data_as_of FROM (`
	var args []any
	for i, source := range userActivitySources {
		if i > 0 {
			query += ` UNION ALL `
		}
		query += `(` + source.sql
		args = append(args, owner)
		if c := q.Cursor; c != nil {
			at := c.At.UTC()
			switch rank := useractivity.Rank(source.kind); {
			case rank < c.Rank:
				query += ` AND ` + source.at + `<=?`
				args = append(args, at)
			case rank == c.Rank:
				query += ` AND (` + source.at + `<? OR (` + source.at + `=? AND ` + source.key + `<?))`
				args = append(args, at, at, c.ID)
			default:
				query += ` AND ` + source.at + `<?`
				args = append(args, at)
			}
		}
		query += ` ORDER BY ` + source.at + ` DESC,` + source.key + ` DESC LIMIT ?)`
		args = append(args, q.Limit+1)
	}
	// STRAIGHT_JOIN keeps the merged page as the driving table, so each row
	// reads its snapshot by primary key instead of scanning snapshots.
	query += `) a STRAIGHT_JOIN event_snapshots e ON e.event_id=a.event_id ORDER BY a.at DESC,a.item_rank DESC,a.item_key DESC LIMIT ?`
	args = append(args, q.Limit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, safeError("public activity", err)
	}
	defer rows.Close()
	items := []useractivity.Item{}
	for rows.Next() {
		var item useractivity.Item
		var rank int
		var key, body, thread, parent, via, viaHost string
		var updated time.Time
		var snapshot []byte
		if err := rows.Scan(&rank, &item.At, &key, &body, &thread, &parent, &updated, &via, &viaHost, &snapshot, &item.Meta.DataAsOf); err != nil {
			return nil, safeError("public activity", err)
		}
		if json.Unmarshal(snapshot, &item.Event) != nil {
			return nil, useractivity.ErrUnavailable
		}
		item.Meta.Stale = true
		switch rank {
		case useractivity.Rank(useractivity.TypeDiscussionPost):
			item.Type = useractivity.TypeDiscussionPost
			item.Post = &useractivity.Post{ID: key, ThreadID: thread, ParentID: parent, Body: body, Via: via, ViaHost: viaHost, CreatedAt: item.At, UpdatedAt: updated}
		case useractivity.Rank(useractivity.TypeRecommendation):
			item.Type = useractivity.TypeRecommendation
			item.Recommendation = &useractivity.Recommendation{Reason: body, Via: via, ViaHost: viaHost, RecommendedAt: item.At, UpdatedAt: updated}
		default:
			item.Type = useractivity.TypeInterest
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("public activity", err)
	}
	return items, nil
}
