package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/sonastea/ticketopia/internal/drafts"
	"github.com/sonastea/ticketopia/internal/models"
)

var _ drafts.Repository = (*DraftRepository)(nil)

// DraftRepository stores agents' private drafts awaiting the person's review.
type DraftRepository struct {
	db      *sql.DB
	timeout time.Duration
}

func (p *Pool) Drafts() *DraftRepository { return &DraftRepository{p.db, p.config.QueryTimeout} }

const draftColumns = `d.draft_id,d.event_id,e.snapshot,d.kind,COALESCE(d.thread_id,''),COALESCE(d.parent_id,''),d.body,d.client_name,d.client_host,COALESCE(d.grant_id,''),d.created_at,d.expires_at`
const draftJoin = ` FROM agent_drafts d JOIN event_snapshots e ON e.event_id=d.event_id`

func scanDraft(row rowScanner) (drafts.Draft, error) {
	var d drafts.Draft
	var snapshot []byte
	if err := row.Scan(&d.ID, &d.EventID, &snapshot, &d.Kind, &d.ThreadID, &d.ParentID, &d.Text, &d.ClientName, &d.ClientHost, &d.GrantID, &d.CreatedAt, &d.ExpiresAt); err != nil {
		return d, err
	}
	if json.Unmarshal(snapshot, &d.Event) != nil {
		return d, drafts.ErrUnavailable
	}
	return d, nil
}

// Create stores the draft and its event snapshot under the account's row
// lock, so concurrent drafts cannot exceed drafts.MaxPending. Expired drafts
// are removed first, everywhere in a bounded batch and for this account in
// full, as OAuth codes are. A nil detail uses the event's stored snapshot,
// which its foreign key requires.
func (r *DraftRepository) Create(ctx context.Context, detail *models.EventDetail, d drafts.Draft, account string, now time.Time) (drafts.Draft, error) {
	var args []any
	var data []byte
	if detail != nil {
		var err error
		if args, data, err = prepareSnapshot(*detail); err != nil {
			return drafts.Draft{}, drafts.ErrUnavailable
		}
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if err := (&AccountRepository{r.db, r.timeout}).cleanupExpired(ctx, "agent_drafts", "draft_id", now); err != nil {
		return drafts.Draft{}, safeError("expired draft cleanup", err)
	}
	for attempt := range 3 {
		created, retry, err := r.createOnce(ctx, detail, d, account, now, args, data)
		if err == nil {
			return created, nil
		}
		if errors.Is(err, drafts.ErrLimit) || !retry || attempt == 2 {
			if errors.Is(err, drafts.ErrLimit) {
				return drafts.Draft{}, err
			}
			return drafts.Draft{}, safeError("draft create", err)
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return drafts.Draft{}, safeError("draft create", ctx.Err())
		case <-timer.C:
		}
	}
	panic("unreachable")
}

// createOnce returns an identical pending draft (same event, kind, thread,
// parent, text and app) instead of adding another, so retries do not fill the
// account's cap.
func (r *DraftRepository) createOnce(ctx context.Context, detail *models.EventDetail, d drafts.Draft, account string, now time.Time, args []any, data []byte) (drafts.Draft, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return d, retryable(err), err
	}
	defer tx.Rollback()
	// Snapshot before account, as saves and recommendations lock the event first.
	if detail != nil {
		if err := upsertSnapshotTx(ctx, tx, *detail, args, data); err != nil {
			return d, retryable(err), err
		}
	}
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT account_id FROM accounts WHERE account_id=? FOR UPDATE`, account).Scan(&id); err != nil {
		return d, retryable(err), err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_drafts WHERE account_id=? AND expires_at<=?`, account, now); err != nil {
		return d, retryable(err), err
	}
	existing, err := scanDraft(tx.QueryRowContext(ctx, `SELECT `+draftColumns+draftJoin+` WHERE d.account_id=? AND d.event_id=? AND d.kind=? AND d.thread_id<=>? AND d.parent_id<=>? AND BINARY d.body=BINARY ? AND BINARY d.client_name=BINARY ? AND d.client_host=? AND d.grant_id<=>? LIMIT 1`,
		account, d.EventID, d.Kind, nullable(d.ThreadID), nullable(d.ParentID), d.Text, d.ClientName, d.ClientHost, nullable(d.GrantID)))
	if err == nil {
		return existing, false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return d, retryable(err), err
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_drafts WHERE account_id=?`, account).Scan(&count); err != nil {
		return d, retryable(err), err
	}
	if count >= drafts.MaxPending {
		return d, false, drafts.ErrLimit
	}
	// A connection removed meanwhile fails its foreign key: its drafts go with it.
	_, err = tx.ExecContext(ctx, `INSERT INTO agent_drafts (draft_id,account_id,event_id,kind,thread_id,parent_id,body,client_name,client_host,grant_id,created_at,expires_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.ID, account, d.EventID, d.Kind, nullable(d.ThreadID), nullable(d.ParentID), d.Text, d.ClientName, d.ClientHost, nullable(d.GrantID), d.CreatedAt, d.ExpiresAt)
	if err != nil {
		return d, retryable(err), err
	}
	return d, false, tx.Commit()
}

func (r *DraftRepository) List(ctx context.Context, account string, now time.Time) ([]drafts.Draft, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, `SELECT `+draftColumns+draftJoin+` WHERE d.account_id=? AND d.expires_at>? ORDER BY d.created_at DESC,d.draft_id DESC LIMIT ?`, account, now, drafts.MaxPending)
	if err != nil {
		return nil, safeError("draft list", err)
	}
	defer rows.Close()
	result := []drafts.Draft{}
	for rows.Next() {
		d, err := scanDraft(rows)
		if errors.Is(err, drafts.ErrUnavailable) {
			return nil, err
		}
		if err != nil {
			return nil, safeError("draft list", err)
		}
		result = append(result, d)
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("draft list", err)
	}
	return result, nil
}

func (r *DraftRepository) Get(ctx context.Context, account, id string, now time.Time) (drafts.Draft, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	d, err := scanDraft(r.db.QueryRowContext(ctx, `SELECT `+draftColumns+draftJoin+` WHERE d.account_id=? AND d.draft_id=? AND d.expires_at>?`, account, id, now))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return drafts.Draft{}, drafts.ErrNotFound
	case errors.Is(err, drafts.ErrUnavailable):
		return drafts.Draft{}, err
	case err != nil:
		return drafts.Draft{}, safeError("draft read", err)
	}
	return d, nil
}

// Delete removes one draft of the account, reporting whether a row went.
func (r *DraftRepository) Delete(ctx context.Context, account, id string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	result, err := r.db.ExecContext(ctx, `DELETE FROM agent_drafts WHERE account_id=? AND draft_id=?`, account, id)
	if err != nil {
		return false, safeError("draft delete", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, safeError("draft delete", err)
	}
	return n > 0, nil
}

func (r *DraftRepository) Count(ctx context.Context, account string, now time.Time) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_drafts WHERE account_id=? AND expires_at>?`, account, now).Scan(&count); err != nil {
		return 0, safeError("draft count", err)
	}
	return count, nil
}

// nullable stores an empty grant (an API-token draft) as NULL.
func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}
