package persistence

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/oauth"
)

var _ oauth.Repository = (*OAuthRepository)(nil)

// OAuthRepository stores agent authorization codes and connections.
type OAuthRepository struct {
	db      *sql.DB
	timeout time.Duration
}

func (p *Pool) OAuth() *OAuthRepository { return &OAuthRepository{p.db, p.config.QueryTimeout} }

const grantColumns = `grant_id, account_id, client_id, client_name, scopes, access_hash, access_expires_at, refresh_hash, created_at, expires_at`

func scanGrant(row rowScanner) (oauth.Grant, error) {
	var g oauth.Grant
	var scopes string
	var access, refresh []byte
	if err := row.Scan(&g.ID, &g.AccountID, &g.ClientID, &g.ClientName, &scopes, &access, &g.AccessUntil, &refresh, &g.CreatedAt, &g.ExpiresAt); err != nil {
		return g, err
	}
	g.Scopes = strings.Fields(scopes)
	copy(g.AccessHash[:], access)
	copy(g.RefreshHash[:], refresh)
	return g, nil
}

// nullableHash stores an unset refresh token as NULL so the UNIQUE index
// ignores clients without refresh tokens.
func nullableHash(h [32]byte) any {
	if h == ([32]byte{}) {
		return nil
	}
	return h[:]
}

func (r *OAuthRepository) cleanup(ctx context.Context, table, key string, now time.Time) error {
	return (&AccountRepository{r.db, r.timeout}).cleanupExpired(ctx, table, key, now)
}

func (r *OAuthRepository) PutCode(ctx context.Context, c oauth.Code) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if err := r.cleanup(ctx, "oauth_codes", "code_hash", time.Now().UTC()); err != nil {
		return safeError("expired authorization code cleanup", err)
	}
	_, err := r.db.ExecContext(ctx, `INSERT INTO oauth_codes (code_hash,account_id,client_id,client_name,redirect_uri,code_challenge,scopes,refresh,expires_at) VALUES (?,?,?,?,?,?,?,?,?)`,
		c.Hash[:], c.AccountID, c.ClientID, c.ClientName, c.RedirectURI, c.Challenge, strings.Join(c.Scopes, " "), c.Refresh, c.ExpiresAt)
	if err != nil {
		return safeError("authorization code create", err)
	}
	return nil
}

// Redeem consumes the code and stores its grant in one transaction. The
// account row is locked first, as RevokeAll locks it, so a redemption either
// finishes before "sign out everywhere" (which then removes its grant) or
// finds the code already gone.
func (r *OAuthRepository) Redeem(ctx context.Context, hash [32]byte, now time.Time, grant func(oauth.Code) (oauth.Grant, error)) (oauth.Grant, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if err := r.cleanup(ctx, "oauth_grants", "grant_id", now); err != nil {
		return oauth.Grant{}, safeError("expired connection cleanup", err)
	}
	var owner string
	err := r.db.QueryRowContext(ctx, `SELECT account_id FROM oauth_codes WHERE code_hash=?`, hash[:]).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return oauth.Grant{}, oauth.ErrInvalidGrant
	}
	if err != nil {
		return oauth.Grant{}, safeError("authorization code read", err)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return oauth.Grant{}, safeError("authorization code transaction", err)
	}
	defer tx.Rollback()
	if err = lockAccount(ctx, tx, owner); err != nil {
		return oauth.Grant{}, err
	}
	c := oauth.Code{Hash: hash}
	var scopes string
	err = tx.QueryRowContext(ctx, `SELECT account_id,client_id,client_name,redirect_uri,code_challenge,scopes,refresh,expires_at FROM oauth_codes WHERE code_hash=? FOR UPDATE`, hash[:]).
		Scan(&c.AccountID, &c.ClientID, &c.ClientName, &c.RedirectURI, &c.Challenge, &scopes, &c.Refresh, &c.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return oauth.Grant{}, oauth.ErrInvalidGrant
	}
	if err != nil {
		return oauth.Grant{}, safeError("authorization code read", err)
	}
	c.Scopes = strings.Fields(scopes)
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_codes WHERE code_hash=?`, hash[:]); err != nil {
		return oauth.Grant{}, safeError("authorization code consume", err)
	}
	var g oauth.Grant
	if !now.Before(c.ExpiresAt) {
		err = oauth.ErrInvalidGrant
	} else {
		g, err = grant(c)
	}
	if err != nil {
		// A failed exchange still consumes the code.
		if commitErr := tx.Commit(); commitErr != nil {
			return oauth.Grant{}, safeError("authorization code commit", commitErr)
		}
		return oauth.Grant{}, err
	}
	if err = insertGrant(ctx, tx, g, now); err != nil {
		return oauth.Grant{}, err
	}
	if err = tx.Commit(); err != nil {
		return oauth.Grant{}, safeError("connection commit", err)
	}
	return g, nil
}

func lockAccount(ctx context.Context, tx *sql.Tx, account string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT account_id FROM accounts WHERE account_id=? FOR UPDATE`, account).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return oauth.ErrInvalidGrant
	}
	if err != nil {
		return safeError("connection owner", err)
	}
	return nil
}

// insertGrant adds a connection under the caller's account lock, removing
// expired ones and, beyond oauth.MaxGrants, the oldest.
func insertGrant(ctx context.Context, tx *sql.Tx, g oauth.Grant, now time.Time) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_grants WHERE account_id=? AND expires_at<=?`, g.AccountID, now); err != nil {
		return safeError("connection cleanup", err)
	}
	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM oauth_grants WHERE account_id=?`, g.AccountID).Scan(&count); err != nil {
		return safeError("connection count", err)
	}
	if count >= oauth.MaxGrants {
		if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_grants WHERE account_id=? ORDER BY created_at,grant_id LIMIT ?`, g.AccountID, count-oauth.MaxGrants+1); err != nil {
			return safeError("connection limit", err)
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO oauth_grants (`+grantColumns+`) VALUES (?,?,?,?,?,?,?,?,?,?)`,
		g.ID, g.AccountID, g.ClientID, g.ClientName, strings.Join(g.Scopes, " "), g.AccessHash[:], g.AccessUntil, nullableHash(g.RefreshHash), g.CreatedAt, g.ExpiresAt)
	if err != nil {
		return safeError("connection create", err)
	}
	return nil
}

// CreateGrant stores a connection directly; review fixtures seed with it.
func (r *OAuthRepository) CreateGrant(ctx context.Context, g oauth.Grant, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return safeError("connection transaction", err)
	}
	defer tx.Rollback()
	if err = lockAccount(ctx, tx, g.AccountID); err != nil {
		return err
	}
	if err = insertGrant(ctx, tx, g, now); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return safeError("connection commit", err)
	}
	return nil
}

// Refresh rotates the grant's tokens and records the old refresh token, so
// presenting it again later revokes the grant.
func (r *OAuthRepository) Refresh(ctx context.Context, old [32]byte, clientID string, scopes []string, next oauth.Grant, now time.Time) (oauth.Grant, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return oauth.Grant{}, safeError("refresh transaction", err)
	}
	defer tx.Rollback()
	g, err := scanGrant(tx.QueryRowContext(ctx, `SELECT `+grantColumns+` FROM oauth_grants WHERE refresh_hash=? FOR UPDATE`, old[:]))
	if errors.Is(err, sql.ErrNoRows) {
		return oauth.Grant{}, r.replayed(ctx, tx, old, now)
	}
	if err != nil {
		return oauth.Grant{}, safeError("refresh read", err)
	}
	if g.ClientID != clientID || !now.Before(g.ExpiresAt) {
		return oauth.Grant{}, oauth.ErrInvalidGrant
	}
	if !oauth.Subset(scopes, g.Scopes) {
		return oauth.Grant{}, oauth.ErrInvalidScope
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO oauth_refresh_history (refresh_hash,grant_id,used_at) VALUES (?,?,?)`, old[:], g.ID, now); err != nil {
		return oauth.Grant{}, safeError("refresh history", err)
	}
	// A refresh token unused for a full grant lifetime would have expired anyway.
	if _, err = tx.ExecContext(ctx, `DELETE FROM oauth_refresh_history WHERE grant_id=? AND used_at<?`, g.ID, now.Add(-oauth.GrantLifetime)); err != nil {
		return oauth.Grant{}, safeError("refresh history cleanup", err)
	}
	_, err = tx.ExecContext(ctx, `UPDATE oauth_grants SET access_hash=?,access_expires_at=?,refresh_hash=?,expires_at=? WHERE grant_id=?`,
		next.AccessHash[:], next.AccessUntil, next.RefreshHash[:], next.ExpiresAt, g.ID)
	if err != nil {
		return oauth.Grant{}, safeError("refresh update", err)
	}
	if err = tx.Commit(); err != nil {
		return oauth.Grant{}, safeError("refresh commit", err)
	}
	g.AccessHash, g.AccessUntil, g.RefreshHash, g.ExpiresAt = next.AccessHash, next.AccessUntil, next.RefreshHash, next.ExpiresAt
	return g, nil
}

// replayed handles a refresh token that is not current: a rotated one used
// again revokes its grant, whoever refreshed first.
func (r *OAuthRepository) replayed(ctx context.Context, tx *sql.Tx, old [32]byte, now time.Time) error {
	var grant string
	err := tx.QueryRowContext(ctx, `SELECT grant_id FROM oauth_refresh_history WHERE refresh_hash=?`, old[:]).Scan(&grant)
	if errors.Is(err, sql.ErrNoRows) {
		return oauth.ErrInvalidGrant
	}
	if err != nil {
		return safeError("refresh history read", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM oauth_grants WHERE grant_id=?`, grant); err != nil {
		return safeError("replayed refresh revoke", err)
	}
	if err := tx.Commit(); err != nil {
		return safeError("replayed refresh commit", err)
	}
	return oauth.ErrInvalidGrant
}

func (r *OAuthRepository) Authenticate(ctx context.Context, access [32]byte, now time.Time) (oauth.Grant, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	g, err := scanGrant(r.db.QueryRowContext(ctx, `SELECT `+grantColumns+` FROM oauth_grants WHERE access_hash=? AND access_expires_at>? AND expires_at>?`, access[:], now, now))
	if errors.Is(err, sql.ErrNoRows) {
		return oauth.Grant{}, oauth.ErrUnauthenticated
	}
	if err != nil {
		return oauth.Grant{}, safeError("access token read", err)
	}
	return g, nil
}

func (r *OAuthRepository) ListGrants(ctx context.Context, account string, now time.Time) ([]oauth.Grant, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, `SELECT `+grantColumns+` FROM oauth_grants WHERE account_id=? AND expires_at>? ORDER BY created_at DESC,grant_id LIMIT ?`, account, now, oauth.MaxGrants)
	if err != nil {
		return nil, safeError("connection list", err)
	}
	defer rows.Close()
	result := []oauth.Grant{}
	for rows.Next() {
		g, err := scanGrant(rows)
		if err != nil {
			return nil, safeError("connection list", err)
		}
		result = append(result, g)
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("connection list", err)
	}
	return result, nil
}

func (r *OAuthRepository) RevokeGrant(ctx context.Context, account, id string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if _, err := r.db.ExecContext(ctx, `DELETE FROM oauth_grants WHERE account_id=? AND grant_id=?`, account, id); err != nil {
		return safeError("connection revoke", err)
	}
	return nil
}

// RevokeAll removes every connection and pending code under the account lock
// that Redeem also takes, so no redemption in flight can outlive it.
func (r *OAuthRepository) RevokeAll(ctx context.Context, account string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return safeError("connections transaction", err)
	}
	defer tx.Rollback()
	if err = lockAccount(ctx, tx, account); err != nil {
		if errors.Is(err, oauth.ErrInvalidGrant) {
			return nil
		}
		return err
	}
	for _, query := range []string{`DELETE FROM oauth_grants WHERE account_id=?`, `DELETE FROM oauth_codes WHERE account_id=?`} {
		if _, err := tx.ExecContext(ctx, query, account); err != nil {
			return safeError("connections revoke", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return safeError("connections commit", err)
	}
	return nil
}
