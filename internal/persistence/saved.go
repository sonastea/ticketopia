package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/saved"
)

var _ saved.Repository = (*SavedRepository)(nil)

type SavedRepository struct {
	db      *sql.DB
	timeout time.Duration
}

func (p *Pool) Saved() *SavedRepository { return &SavedRepository{p.db, p.config.QueryTimeout} }

// Saved reads are always last-known snapshots, not a freshness claim or an
// ingestion scheduler. They require no cache or upstream calls.
func scanSaved(row interface{ Scan(...any) error }) (saved.Item, error) {
	var item saved.Item
	var snapshot []byte
	err := row.Scan(&snapshot, &item.Meta.DataAsOf, &item.SavedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, saved.ErrNotFound
	}
	if err != nil {
		return item, safeError("saved event read", err)
	}
	if json.Unmarshal(snapshot, &item.Event) != nil {
		return item, saved.ErrUnavailable
	}
	item.Meta.Stale = true
	return item, nil
}
func (r *SavedRepository) Get(ctx context.Context, owner, id string) (saved.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return scanSaved(r.db.QueryRowContext(ctx, `SELECT e.snapshot, e.data_as_of, s.saved_at FROM saved_events s JOIN event_snapshots e ON e.event_id=s.event_id WHERE s.account_id=? AND s.event_id=?`, owner, id))
}
func (r *SavedRepository) Snapshot(ctx context.Context, id string) (models.EventDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var detail models.EventDetail
	var snapshot []byte
	err := r.db.QueryRowContext(ctx, `SELECT snapshot, data_as_of FROM event_snapshots WHERE event_id=?`, id).Scan(&snapshot, &detail.Meta.DataAsOf)
	if errors.Is(err, sql.ErrNoRows) {
		return detail, saved.ErrNotFound
	}
	if err != nil {
		return detail, safeError("event snapshot read", err)
	}
	if json.Unmarshal(snapshot, &detail.Item) != nil {
		return detail, saved.ErrUnavailable
	}
	detail.Meta.Stale = true
	return detail, nil
}
func (r *SavedRepository) Save(ctx context.Context, owner string, detail models.EventDetail) (saved.Item, bool, error) {
	args, data, err := prepareSnapshot(detail)
	if err != nil {
		return saved.Item{}, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	for attempt := range 3 {
		item, created, retry, err := r.saveOnce(ctx, owner, detail, args, data)
		if err == nil {
			return item, created, nil
		}
		if !retry || attempt == 2 {
			return saved.Item{}, false, safeError("saved event write", err)
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return saved.Item{}, false, safeError("saved event write", ctx.Err())
		case <-timer.C:
		}
	}
	panic("unreachable")
}

// Saves and interest share durable event metadata, never personal activity.
func prepareSnapshot(detail models.EventDetail) ([]any, []byte, error) {
	e := detail.Item
	if err := validateEvent(e); err != nil {
		return nil, nil, err
	}
	if detail.Meta.DataAsOf.IsZero() || detail.Meta.DataAsOf.Year() < 1000 || detail.Meta.DataAsOf.Year() > 9999 {
		return nil, nil, saved.ErrUnavailable
	}
	data, err := json.Marshal(e)
	if err != nil || len(data) > 1024*1024 {
		return nil, nil, saved.ErrUnavailable
	}
	args := []any{e.ID, e.Name, e.Source.URL, e.Start.DateTime, e.Start.LocalDate, e.Start.LocalTime, e.Start.Timezone, e.Start.DateTBA, e.Start.DateTBD, e.Start.TimeTBA, e.Start.NoSpecificTime, e.Status}
	for _, value := range []any{e.Venues, e.Artists, e.Classifications, e.Place} {
		v, err := json.Marshal(value)
		if err != nil {
			return nil, nil, saved.ErrUnavailable
		}
		args = append(args, string(v))
	}
	return args, data, nil
}
func upsertSnapshotTx(ctx context.Context, tx *sql.Tx, detail models.EventDetail, args []any, data []byte) error {
	// Consistent event-first locking coordinates independent activity writers.
	if err := writeEventTx(ctx, tx, detail.Item, args, false); err != nil {
		return err
	}
	var last time.Time
	err := tx.QueryRowContext(ctx, `SELECT data_as_of FROM event_snapshots WHERE event_id=? FOR UPDATE`, detail.Item.ID).Scan(&last)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if errors.Is(err, sql.ErrNoRows) || detail.Meta.DataAsOf.UTC().Truncate(time.Microsecond).After(last) {
		if err := upsertEventTx(ctx, tx, detail.Item, args); err != nil {
			return err
		}
	}
	if !detail.Meta.Stale {
		if err := observeTx(ctx, tx, detail, data); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO event_snapshots (event_id,snapshot,data_as_of) VALUES (?,?,?) ON DUPLICATE KEY UPDATE snapshot=IF(VALUES(data_as_of)>data_as_of,VALUES(snapshot),snapshot), data_as_of=GREATEST(data_as_of,VALUES(data_as_of))`, detail.Item.ID, string(data), detail.Meta.DataAsOf.UTC().Truncate(time.Microsecond))
	return err
}
func (r *SavedRepository) saveOnce(ctx context.Context, owner string, detail models.EventDetail, args []any, data []byte) (saved.Item, bool, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return saved.Item{}, false, retryable(err), err
	}
	defer tx.Rollback()
	if err = upsertSnapshotTx(ctx, tx, detail, args, data); err != nil {
		return saved.Item{}, false, retryable(err), err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM saved_events WHERE account_id=? AND event_id=?`, owner, detail.Item.ID).Scan(&count); err != nil {
		return saved.Item{}, false, retryable(err), err
	}
	if count == 0 {
		_, err = tx.ExecContext(ctx, `INSERT INTO saved_events (account_id,event_id) VALUES (?,?)`, owner, detail.Item.ID)
		if err != nil {
			return saved.Item{}, false, retryable(err), err
		}
	}
	item, err := scanSaved(tx.QueryRowContext(ctx, `SELECT e.snapshot,e.data_as_of,s.saved_at FROM saved_events s JOIN event_snapshots e ON e.event_id=s.event_id WHERE s.account_id=? AND s.event_id=?`, owner, detail.Item.ID))
	if err != nil {
		return saved.Item{}, false, false, err
	}
	// Commit failures are uncertain; retrying PUT resolves the existing bookmark.
	if err = tx.Commit(); err != nil {
		return saved.Item{}, false, false, err
	}
	return item, count == 0, false, nil
}
func (r *SavedRepository) Remove(ctx context.Context, owner, id string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, `DELETE FROM saved_events WHERE account_id=? AND event_id=?`, owner, id)
	if err != nil {
		return safeError("saved event removal", err)
	}
	return nil
}
func (r *SavedRepository) List(ctx context.Context, owner string, q saved.Query) ([]saved.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query := `SELECT e.snapshot,e.data_as_of,s.saved_at FROM saved_events s JOIN event_snapshots e ON e.event_id=s.event_id WHERE s.account_id=?`
	args := []any{owner}
	if q.Cursor != nil {
		query += ` AND (s.saved_at<? OR (s.saved_at=? AND s.event_id<?))`
		args = append(args, q.Cursor.Before.UTC(), q.Cursor.Before.UTC(), q.Cursor.ID)
	}
	query += ` ORDER BY s.saved_at DESC,s.event_id DESC LIMIT ?`
	args = append(args, q.Limit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, safeError("saved collection read", err)
	}
	defer rows.Close()
	items := []saved.Item{}
	for rows.Next() {
		item, err := scanSaved(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("saved collection read", err)
	}
	return items, nil
}
func (r *SavedRepository) States(ctx context.Context, owner string, ids []string) (map[string]bool, error) {
	result := make(map[string]bool)
	if len(ids) == 0 {
		return result, nil
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	args := []any{owner}
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT event_id FROM saved_events WHERE account_id=? AND event_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...)
	if err != nil {
		return nil, safeError("saved state read", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, safeError("saved state read", err)
		}
		result[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("saved state read", err)
	}
	return result, nil
}
