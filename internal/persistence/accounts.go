package persistence

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/sonastea/ticketopia/internal/accounts"
)

var _ accounts.Repository = (*AccountRepository)(nil)

type AccountRepository struct {
	db      *sql.DB
	timeout time.Duration
}

func (p *Pool) Accounts() *AccountRepository { return &AccountRepository{p.db, p.config.QueryTimeout} }

const accountColumns = `account_id, email, display_name, bio, interest_visibility, city, country, timezone, category_ids, email_reminders, weekly_digest, notifications_paused, created_at`
const credentialColumns = `credential_id, account_id, token_hash, kind, name, created_at, expires_at`

type rowScanner interface{ Scan(...any) error }

func scanAccount(row rowScanner) (accounts.Account, error) {
	var a accounts.Account
	var categories []byte
	err := row.Scan(&a.ID, &a.Email, &a.DisplayName, &a.Bio, &a.InterestVisibility, &a.Preferences.City, &a.Preferences.Country, &a.Preferences.Timezone, &categories, &a.Preferences.EmailReminders, &a.Preferences.WeeklyDigest, &a.Preferences.NotificationsPaused, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return a, accounts.ErrNotFound
	}
	if err != nil {
		return a, safeError("account read", err)
	}
	if err := json.Unmarshal(categories, &a.Preferences.CategoryIDs); err != nil {
		return a, safeError("account preferences", err)
	}
	if a.Preferences.CategoryIDs == nil {
		a.Preferences.CategoryIDs = []string{}
	}
	return a, nil
}
func scanCredential(row rowScanner) (accounts.Credential, error) {
	var c accounts.Credential
	var hash []byte
	err := row.Scan(&c.ID, &c.AccountID, &hash, &c.Kind, &c.Name, &c.CreatedAt, &c.ExpiresAt)
	if err != nil {
		return c, err
	}
	copy(c.Hash[:], hash)
	return c, nil
}
func (r *AccountRepository) Get(ctx context.Context, id string) (accounts.Account, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return scanAccount(r.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE account_id=?`, id))
}
func (r *AccountRepository) PutFlow(ctx context.Context, f accounts.Flow) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if err := r.cleanupExpired(ctx, "auth_flows", "state_hash", time.Now().UTC()); err != nil {
		return safeError("expired sign-in cleanup", err)
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO auth_flows (state_hash,browser_hash,verifier,nonce,return_to,expires_at) VALUES (?,?,?,?,?,?)`, f.StateHash[:], f.BrowserHash[:], f.Verifier, f.Nonce, f.ReturnTo, f.ExpiresAt)
	if err != nil {
		return safeError("sign-in start", err)
	}
	return nil
}
func (r *AccountRepository) ConsumeFlow(ctx context.Context, state, browser [32]byte, now time.Time) (accounts.Flow, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return accounts.Flow{}, safeError("sign-in transaction", err)
	}
	defer tx.Rollback()
	f := accounts.Flow{StateHash: state}
	var storedBrowser []byte
	err = tx.QueryRowContext(ctx, `SELECT browser_hash,verifier,nonce,return_to,expires_at FROM auth_flows WHERE state_hash=? FOR UPDATE`, state[:]).Scan(&storedBrowser, &f.Verifier, &f.Nonce, &f.ReturnTo, &f.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return f, accounts.ErrFlow
	}
	if err != nil {
		return f, safeError("sign-in read", err)
	}
	if subtle.ConstantTimeCompare(storedBrowser, browser[:]) != 1 || !now.Before(f.ExpiresAt) {
		return f, accounts.ErrFlow
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_flows WHERE state_hash=?`, state[:]); err != nil {
		return f, safeError("sign-in consume", err)
	}
	if err := tx.Commit(); err != nil {
		return f, safeError("sign-in commit", err)
	}
	return f, nil
}
func (r *AccountRepository) Login(ctx context.Context, identity accounts.Identity, c accounts.Credential) (accounts.Account, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	for attempt := range 3 {
		a, retry, err := r.loginOnce(ctx, identity, c)
		if err == nil {
			return a, nil
		}
		if !retry || attempt == 2 {
			return accounts.Account{}, safeError("account sign-in", err)
		}
	}
	panic("unreachable")
}
func (r *AccountRepository) loginOnce(ctx context.Context, identity accounts.Identity, c accounts.Credential) (accounts.Account, bool, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return accounts.Account{}, retryable(err), err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT account_id FROM account_identities WHERE issuer=? AND subject=?`, identity.Issuer, identity.Subject).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		id = accounts.ID()
		_, err = tx.ExecContext(ctx, `INSERT INTO accounts (account_id,email) VALUES (?,?)`, id, identity.Email)
		if err == nil {
			_, err = tx.ExecContext(ctx, `INSERT INTO account_identities (issuer,subject,account_id) VALUES (?,?,?)`, identity.Issuer, identity.Subject, id)
		}
		if err != nil {
			var server *mysql.MySQLError
			return accounts.Account{}, retryable(err) || (errors.As(err, &server) && server.Number == 1062), err
		}
	} else if err != nil {
		return accounts.Account{}, retryable(err), err
	}
	// The account row serializes credential limits and sign-ins across replicas.
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET email=? WHERE account_id=?`, identity.Email, id)
	if err != nil {
		return accounts.Account{}, retryable(err), err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM account_credentials WHERE account_id=? AND expires_at<=?`, id, c.CreatedAt); err != nil {
		return accounts.Account{}, retryable(err), err
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_credentials WHERE account_id=? AND kind='session'`, id).Scan(&count); err != nil {
		return accounts.Account{}, retryable(err), err
	}
	if count >= 10 {
		if _, err = tx.ExecContext(ctx, `DELETE FROM account_credentials WHERE account_id=? AND kind='session' ORDER BY created_at,credential_id LIMIT ?`, id, count-9); err != nil {
			return accounts.Account{}, retryable(err), err
		}
	}
	c.AccountID = id
	if err = insertCredential(ctx, tx, c); err != nil {
		return accounts.Account{}, retryable(err), err
	}
	a, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE account_id=?`, id))
	if err != nil {
		return accounts.Account{}, false, err
	}
	// Do not replay an uncertain COMMIT. A new provider sign-in safely resolves identity.
	return a, false, tx.Commit()
}
func insertCredential(ctx context.Context, tx *sql.Tx, c accounts.Credential) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO account_credentials (`+credentialColumns+`) VALUES (?,?,?,?,?,?,?)`, c.ID, c.AccountID, c.Hash[:], c.Kind, c.Name, c.CreatedAt, c.ExpiresAt)
	return err
}
func (r *AccountRepository) Authenticate(ctx context.Context, hash [32]byte, kind string, now time.Time) (accounts.Account, accounts.Credential, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	c, err := scanCredential(r.db.QueryRowContext(ctx, `SELECT `+credentialColumns+` FROM account_credentials WHERE token_hash=? AND kind=? AND expires_at>?`, hash[:], kind, now))
	if errors.Is(err, sql.ErrNoRows) {
		return accounts.Account{}, c, accounts.ErrUnauthenticated
	}
	if err != nil {
		return accounts.Account{}, c, safeError("credential read", err)
	}
	a, err := scanAccount(r.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE account_id=?`, c.AccountID))
	return a, c, err
}
func (r *AccountRepository) update(ctx context.Context, id string, sets []string, args []any) (accounts.Account, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	sets = append(sets, "updated_at=UTC_TIMESTAMP(6)")
	args = append(args, id)
	// Column names come only from the allowlisted methods below, never client input.
	if _, err := r.db.ExecContext(ctx, `UPDATE accounts SET `+strings.Join(sets, ",")+` WHERE account_id=?`, args...); err != nil {
		return accounts.Account{}, safeError("account update", err)
	}
	return scanAccount(r.db.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE account_id=?`, id))
}
func (r *AccountRepository) UpdateProfile(ctx context.Context, id string, p accounts.ProfilePatch) (accounts.Account, error) {
	var sets []string
	var args []any
	for _, field := range []struct {
		column string
		value  *string
	}{{"display_name", p.DisplayName}, {"bio", p.Bio}, {"interest_visibility", p.InterestVisibility}} {
		if field.value != nil {
			sets = append(sets, field.column+"=?")
			args = append(args, *field.value)
		}
	}
	return r.update(ctx, id, sets, args)
}
func (r *AccountRepository) UpdatePreferences(ctx context.Context, id string, p accounts.PreferencesPatch) (accounts.Account, error) {
	var sets []string
	var args []any
	for _, field := range []struct {
		column string
		value  *string
	}{{"city", p.City}, {"country", p.Country}, {"timezone", p.Timezone}} {
		if field.value != nil {
			sets = append(sets, field.column+"=?")
			args = append(args, *field.value)
		}
	}
	if p.CategoryIDs != nil {
		data, err := json.Marshal(*p.CategoryIDs)
		if err != nil {
			return accounts.Account{}, safeError("preferences encoding", err)
		}
		sets = append(sets, "category_ids=?")
		args = append(args, string(data))
	}
	for _, field := range []struct {
		column string
		value  *bool
	}{{"email_reminders", p.EmailReminders}, {"weekly_digest", p.WeeklyDigest}, {"notifications_paused", p.NotificationsPaused}} {
		if field.value != nil {
			sets = append(sets, field.column+"=?")
			args = append(args, *field.value)
		}
	}
	return r.update(ctx, id, sets, args)
}
func (r *AccountRepository) ListTokens(ctx context.Context, id string, now time.Time) ([]accounts.Credential, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, `SELECT `+credentialColumns+` FROM account_credentials WHERE account_id=? AND kind='api' AND expires_at>? ORDER BY created_at DESC,credential_id LIMIT 10`, id, now)
	if err != nil {
		return nil, safeError("API token list", err)
	}
	defer rows.Close()
	result := []accounts.Credential{}
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, safeError("API token list", err)
		}
		result = append(result, c)
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("API token list", err)
	}
	return result, nil
}
func (r *AccountRepository) CreateToken(ctx context.Context, c accounts.Credential, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return safeError("API token transaction", err)
	}
	defer tx.Rollback()
	var id string
	if err = tx.QueryRowContext(ctx, `SELECT account_id FROM accounts WHERE account_id=? FOR UPDATE`, c.AccountID).Scan(&id); err != nil {
		return safeError("API token owner", err)
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_credentials WHERE account_id=? AND kind='api' AND expires_at>?`, id, now).Scan(&count); err != nil {
		return safeError("API token count", err)
	}
	if count >= 10 {
		return accounts.ErrCredentialLimit
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM account_credentials WHERE account_id=? AND expires_at<=?`, id, now); err != nil {
		return safeError("API token cleanup", err)
	}
	if err = insertCredential(ctx, tx, c); err != nil {
		return safeError("API token create", err)
	}
	if err = tx.Commit(); err != nil {
		return safeError("API token commit", err)
	}
	return nil
}
func (r *AccountRepository) Revoke(ctx context.Context, owner, id string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, `DELETE FROM account_credentials WHERE account_id=? AND credential_id=?`, owner, id)
	if err != nil {
		return safeError("credential revoke", err)
	}
	return nil
}
func (r *AccountRepository) RevokeAll(ctx context.Context, owner string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	_, err := r.db.ExecContext(ctx, `DELETE FROM account_credentials WHERE account_id=?`, owner)
	if err != nil {
		return safeError("credentials revoke", err)
	}
	return nil
}
func (r *AccountRepository) Allow(ctx context.Context, key [32]byte, now time.Time, window time.Duration, limit int) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if err := r.cleanupExpired(ctx, "auth_rate_limits", "bucket_hash", now); err != nil {
		return false, safeError("auth rate cleanup", err)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, safeError("auth rate transaction", err)
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `INSERT INTO auth_rate_limits (bucket_hash,attempts,expires_at) VALUES (?,0,?) ON DUPLICATE KEY UPDATE bucket_hash=bucket_hash`, key[:], now.Add(window)); err != nil {
		return false, safeError("auth rate initialization", err)
	}
	var count int
	var expires time.Time
	if err = tx.QueryRowContext(ctx, `SELECT attempts,expires_at FROM auth_rate_limits WHERE bucket_hash=? FOR UPDATE`, key[:]).Scan(&count, &expires); err != nil {
		return false, safeError("auth rate read", err)
	}
	if !now.Before(expires) {
		count = 0
		expires = now.Add(window)
	}
	if count >= limit {
		return false, nil
	}
	if _, err = tx.ExecContext(ctx, `UPDATE auth_rate_limits SET attempts=?,expires_at=? WHERE bucket_hash=?`, count+1, expires, key[:]); err != nil {
		return false, safeError("auth rate update", err)
	}
	if err = tx.Commit(); err != nil {
		return false, safeError("auth rate commit", err)
	}
	return true, nil
}

// Select expired primary keys without write locks before deleting. An empty
// range DELETE under MariaDB's default isolation can gap-lock concurrent inserts
// and deadlock first sign-ins. Names here are internal constants, never input.
func (r *AccountRepository) cleanupExpired(ctx context.Context, table, key string, now time.Time) error {
	rows, err := r.db.QueryContext(ctx, `SELECT `+key+` FROM `+table+` WHERE expires_at<=? ORDER BY expires_at,`+key+` LIMIT 100`, now)
	if err != nil {
		return err
	}
	var args []any
	for rows.Next() {
		var value []byte
		if err := rows.Scan(&value); err != nil {
			rows.Close()
			return err
		}
		args = append(args, value)
	}
	err = rows.Err()
	if closeErr := rows.Close(); err == nil {
		err = closeErr
	}
	if err != nil || len(args) == 0 {
		return err
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")
	args = append(args, now)
	_, err = r.db.ExecContext(ctx, `DELETE FROM `+table+` WHERE `+key+` IN (`+placeholders+`) AND expires_at<=?`, args...)
	return err
}
