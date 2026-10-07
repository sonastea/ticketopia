package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/interests"
	"github.com/sonastea/ticketopia/internal/models"
)

var _ interests.Repository = (*InterestRepository)(nil)

type InterestRepository struct {
	db      *sql.DB
	timeout time.Duration
}

func (p *Pool) Interests() *InterestRepository {
	return &InterestRepository{p.db, p.config.QueryTimeout}
}

const interestColumns = `e.snapshot,e.data_as_of,i.interested_at,i.visibility`

func scanInterest(row interface{ Scan(...any) error }) (interests.Item, error) {
	var item interests.Item
	var snapshot []byte
	err := row.Scan(&snapshot, &item.Meta.DataAsOf, &item.InterestedAt, &item.Visibility)
	if errors.Is(err, sql.ErrNoRows) {
		return item, interests.ErrNotFound
	}
	if err != nil {
		return item, safeError("event interest read", err)
	}
	if json.Unmarshal(snapshot, &item.Event) != nil {
		return item, interests.ErrUnavailable
	}
	item.Meta.Stale = true
	return item, nil
}
func (r *InterestRepository) Get(ctx context.Context, owner, id string) (interests.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return scanInterest(r.db.QueryRowContext(ctx, `SELECT `+interestColumns+` FROM event_interests i JOIN event_snapshots e ON e.event_id=i.event_id WHERE i.account_id=? AND i.event_id=?`, owner, id))
}
func (r *InterestRepository) Set(ctx context.Context, owner string, detail models.EventDetail, visibility string) (interests.Item, bool, error) {
	if err := interests.Visibility(visibility); err != nil {
		return interests.Item{}, false, err
	}
	args, data, err := prepareSnapshot(detail)
	if err != nil {
		return interests.Item{}, false, interests.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	for attempt := range 3 {
		item, created, retry, err := r.setOnce(ctx, owner, detail, visibility, args, data)
		if err == nil {
			return item, created, nil
		}
		if !retry || attempt == 2 {
			return interests.Item{}, false, safeError("event interest write", err)
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return interests.Item{}, false, safeError("event interest write", ctx.Err())
		case <-timer.C:
		}
	}
	panic("unreachable")
}
func (r *InterestRepository) setOnce(ctx context.Context, owner string, detail models.EventDetail, visibility string, args []any, data []byte) (interests.Item, bool, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return interests.Item{}, false, retryable(err), err
	}
	defer tx.Rollback()
	if err := upsertSnapshotTx(ctx, tx, detail, args, data); err != nil {
		return interests.Item{}, false, retryable(err), err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_interests WHERE account_id=? AND event_id=?`, owner, detail.Item.ID).Scan(&count); err != nil {
		return interests.Item{}, false, retryable(err), err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO event_interests (account_id,event_id,visibility) VALUES (?,?,?) ON DUPLICATE KEY UPDATE visibility=VALUES(visibility)`, owner, detail.Item.ID, visibility)
	if err != nil {
		return interests.Item{}, false, retryable(err), err
	}
	item, err := scanInterest(tx.QueryRowContext(ctx, `SELECT `+interestColumns+` FROM event_interests i JOIN event_snapshots e ON e.event_id=i.event_id WHERE i.account_id=? AND i.event_id=?`, owner, detail.Item.ID))
	if err != nil {
		return interests.Item{}, false, false, err
	}
	if err := tx.Commit(); err != nil {
		return interests.Item{}, false, false, err
	}
	return item, count == 0, false, nil
}
func (r *InterestRepository) Remove(ctx context.Context, owner, id string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, `DELETE FROM event_interests WHERE account_id=? AND event_id=?`, owner, id)
	if err != nil {
		return safeError("event interest removal", err)
	}
	return nil
}

// Counts include private interest; viewer fields are selected only for the owner.
// One grouped query batches all card aggregates and personal states.
func (r *InterestRepository) States(ctx context.Context, owner string, ids []string) (map[string]interests.State, error) {
	result := map[string]interests.State{}
	if len(ids) == 0 {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	args := []any{owner, owner}
	for _, id := range ids {
		args = append(args, id)
		result[id] = interests.State{}
	}
	rows, err := r.db.QueryContext(ctx, `SELECT event_id,COUNT(*),MAX(account_id=?),COALESCE(MAX(CASE WHEN account_id=? THEN visibility END),'') FROM event_interests WHERE event_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`) GROUP BY event_id`, args...)
	if err != nil {
		return nil, safeError("event interest states", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var state interests.State
		if err := rows.Scan(&id, &state.Count, &state.Interested, &state.Visibility); err != nil {
			return nil, safeError("event interest states", err)
		}
		result[id] = state
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("event interest states", err)
	}
	return result, nil
}
func (r *InterestRepository) List(ctx context.Context, owner string, public bool, q interests.Query) ([]interests.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query := `SELECT ` + interestColumns + ` FROM event_interests i JOIN event_snapshots e ON e.event_id=i.event_id WHERE i.account_id=?`
	args := []any{owner}
	if public {
		query += ` AND i.visibility='public'`
	}
	if q.Cursor != nil {
		query += ` AND (i.interested_at<? OR (i.interested_at=? AND i.event_id<?))`
		args = append(args, q.Cursor.Before.UTC(), q.Cursor.Before.UTC(), q.Cursor.ID)
	}
	query += ` ORDER BY i.interested_at DESC,i.event_id DESC LIMIT ?`
	args = append(args, q.Limit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, safeError("interest collection", err)
	}
	defer rows.Close()
	items := []interests.Item{}
	for rows.Next() {
		item, err := scanInterest(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("interest collection", err)
	}
	return items, nil
}
func (r *InterestRepository) Participants(ctx context.Context, id string, q interests.Query) ([]interests.Participant, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query := `SELECT a.account_id,a.display_name,a.bio,i.interested_at FROM event_interests i JOIN accounts a ON a.account_id=i.account_id WHERE i.event_id=? AND i.visibility='public'`
	args := []any{id}
	if q.Cursor != nil {
		query += ` AND (i.interested_at<? OR (i.interested_at=? AND i.account_id<?))`
		args = append(args, q.Cursor.Before.UTC(), q.Cursor.Before.UTC(), q.Cursor.ID)
	}
	query += ` ORDER BY i.interested_at DESC,i.account_id DESC LIMIT ?`
	args = append(args, q.Limit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, safeError("public interest participants", err)
	}
	defer rows.Close()
	items := []interests.Participant{}
	for rows.Next() {
		var item interests.Participant
		if err := rows.Scan(&item.Profile.ID, &item.Profile.DisplayName, &item.Profile.Bio, &item.InterestedAt); err != nil {
			return nil, safeError("public interest participants", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("public interest participants", err)
	}
	return items, nil
}
