package persistence

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/activity"
)

const livePreferenceColumns = `live_activity_version,live_activity_enabled,live_activity_revision,notifications_paused`

func scanLive(row rowScanner) (accounts.LivePreferences, error) {
	p := accounts.LivePreferences{Preferences: activity.DefaultPreferences()}
	err := row.Scan(&p.Version, &p.Enabled, &p.Revision, &p.Paused)
	if errors.Is(err, sql.ErrNoRows) {
		return p, accounts.ErrNotFound
	}
	if err != nil {
		return p, safeError("live preference read", err)
	}
	return p, nil
}
func (r *AccountRepository) LivePreferences(ctx context.Context, owner string) (accounts.LivePreferences, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return scanLive(r.db.QueryRowContext(ctx, `SELECT `+livePreferenceColumns+` FROM accounts WHERE account_id=?`, owner))
}
func (r *AccountRepository) ReplaceLivePreferences(ctx context.Context, owner string, revision uint64, enabled bool) (accounts.LivePreferences, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return accounts.LivePreferences{}, safeError("live preference transaction", err)
	}
	defer tx.Rollback()
	p, err := scanLive(tx.QueryRowContext(ctx, `SELECT `+livePreferenceColumns+` FROM accounts WHERE account_id=? FOR UPDATE`, owner))
	if err != nil {
		return p, err
	}
	if p.Revision != revision {
		return p, accounts.ErrLiveConflict
	}
	if p.Enabled != enabled {
		_, err = tx.ExecContext(ctx, `UPDATE accounts SET live_activity_enabled=?,live_activity_revision=live_activity_revision+1,updated_at=UTC_TIMESTAMP(6) WHERE account_id=?`, enabled, owner)
		if err != nil {
			return p, safeError("live preference update", err)
		}
		p.Enabled = enabled
		p.Revision++
	}
	if err = tx.Commit(); err != nil {
		return p, safeError("live preference commit", err)
	}
	return p, nil
}
func (r *AccountRepository) LiveScopes(ctx context.Context, ids []string) (map[string]accounts.LivePreferences, error) {
	result := map[string]accounts.LivePreferences{}
	if len(ids) == 0 {
		return result, nil
	}
	if len(ids) > 128 {
		return nil, activity.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := r.db.QueryContext(ctx, `SELECT c.credential_id,a.live_activity_version,a.live_activity_enabled,a.live_activity_revision,a.notifications_paused FROM account_credentials c JOIN accounts a ON a.account_id=c.account_id WHERE c.expires_at>UTC_TIMESTAMP(6) AND c.credential_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...)
	if err != nil {
		return nil, safeError("live scope read", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		p := accounts.LivePreferences{Preferences: activity.DefaultPreferences()}
		if err = rows.Scan(&id, &p.Version, &p.Enabled, &p.Revision, &p.Paused); err != nil {
			return nil, safeError("live scope read", err)
		}
		result[id] = p
	}
	if err = rows.Err(); err != nil {
		return nil, safeError("live scope read", err)
	}
	return result, nil
}
