package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/follows"
	"github.com/sonastea/ticketopia/internal/models"
)

type FollowRepository struct {
	db      *sql.DB
	timeout time.Duration
}

var _ follows.Repository = (*FollowRepository)(nil)

func (p *Pool) Follows() *FollowRepository { return &FollowRepository{p.db, p.config.QueryTimeout} }

// Names are selected only from these constants, never interpolated client input.
func followTables(kind string) (catalog, mapping, column, collection string, err error) {
	switch kind {
	case "artist":
		return "artists", "artist_providers", "artist_id", "artist_follows", nil
	case "venue":
		return "venues", "venue_providers", "venue_id", "venue_follows", nil
	default:
		return "", "", "", "", follows.ErrUnavailable
	}
}

// Shared reference catalogs are updated by both event observations and follows.
func upsertReferenceTx(ctx context.Context, tx *sql.Tx, kind, id, provider, source string, data []byte, at time.Time) error {
	table, mapping, column, _, err := followTables(kind)
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+table+` (`+column+`,snapshot,first_seen,last_seen) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE snapshot=IF(VALUES(last_seen)>last_seen,VALUES(snapshot),snapshot),first_seen=LEAST(first_seen,VALUES(first_seen)),last_seen=GREATEST(last_seen,VALUES(last_seen))`, id, string(data), at, at); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO `+mapping+` (provider,source_id,`+column+`) VALUES (?,?,?) ON DUPLICATE KEY UPDATE `+column+`=`+column, provider, source, id); err != nil {
		return err
	}
	var mapped string
	if err := tx.QueryRowContext(ctx, `SELECT `+column+` FROM `+mapping+` WHERE provider=? AND source_id=?`, provider, source).Scan(&mapped); err != nil {
		return err
	}
	if mapped != id {
		return fmt.Errorf("reference mapping cannot change identity")
	}
	return nil
}
func decodeFollowTarget(kind string, data []byte) (models.FollowTarget, error) {
	if kind == "artist" {
		var a models.Artist
		if json.Unmarshal(data, &a) != nil {
			return models.FollowTarget{}, follows.ErrUnavailable
		}
		if a.Source.Provider == "" {
			a.Source.Provider, a.Source.ID, _ = strings.Cut(a.ID, ":")
		}
		return models.ArtistTarget(a), nil
	}
	var v models.Venue
	if json.Unmarshal(data, &v) != nil {
		return models.FollowTarget{}, follows.ErrUnavailable
	}
	if v.Source.Provider == "" {
		v.Source.Provider, v.Source.ID, _ = strings.Cut(v.ID, ":")
	}
	return models.VenueTarget(v), nil
}
func scanFollow(row interface{ Scan(...any) error }) (follows.Item, error) {
	var item follows.Item
	var kind string
	var data []byte
	err := row.Scan(&kind, &data, &item.Meta.DataAsOf, &item.FollowedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, follows.ErrNotFound
	}
	if err != nil {
		return item, safeError("follow read", err)
	}
	item.Target, err = decodeFollowTarget(kind, data)
	item.Meta.Stale = true
	return item, err
}
func followSelect(kind, catalog, column, collection string) string {
	return `SELECT '` + kind + `' AS kind,c.snapshot,c.last_seen AS data_as_of,f.followed_at,f.` + column + ` AS target_id FROM ` + collection + ` f JOIN ` + catalog + ` c ON c.` + column + `=f.` + column + ` WHERE f.account_id=?`
}
func (r *FollowRepository) Get(ctx context.Context, owner, kind, id string) (follows.Item, error) {
	catalog, _, column, collection, err := followTables(kind)
	if err != nil {
		return follows.Item{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return scanFollow(r.db.QueryRowContext(ctx, `SELECT '`+kind+`',c.snapshot,c.last_seen,f.followed_at FROM `+collection+` f JOIN `+catalog+` c ON c.`+column+`=f.`+column+` WHERE f.account_id=? AND f.`+column+`=?`, owner, id))
}
func (r *FollowRepository) Snapshot(ctx context.Context, kind, id string) (models.CatalogDetail, error) {
	catalog, _, column, _, err := followTables(kind)
	if err != nil {
		return models.CatalogDetail{}, err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var detail models.CatalogDetail
	var data []byte
	err = r.db.QueryRowContext(ctx, `SELECT snapshot,last_seen FROM `+catalog+` WHERE `+column+`=?`, id).Scan(&data, &detail.Meta.DataAsOf)
	if errors.Is(err, sql.ErrNoRows) {
		return detail, follows.ErrNotFound
	}
	if err != nil {
		return detail, safeError("follow target read", err)
	}
	detail.Item, err = decodeFollowTarget(kind, data)
	detail.Meta.Stale = true
	return detail, err
}
func (r *FollowRepository) Follow(ctx context.Context, owner string, detail models.CatalogDetail) (follows.Item, bool, error) {
	t := detail.Item
	if follows.Validate(t.Kind, t.ID) != nil || strings.TrimSpace(t.Name) == "" || len(t.Name) > 1000 || t.Source.Provider != "ticketmaster" || t.ID != "ticketmaster:"+t.Source.ID || detail.Meta.DataAsOf.IsZero() || detail.Meta.DataAsOf.Year() < 1000 || detail.Meta.DataAsOf.Year() > 9999 {
		return follows.Item{}, false, follows.ErrUnavailable
	}
	var value any = models.Artist{ID: t.ID, Name: t.Name, Source: t.Source}
	if t.Kind == "venue" {
		place := models.Place{Name: t.Name}
		if t.Location != nil {
			place = *t.Location
			place.Name = t.Name
		}
		value = models.Venue{ID: t.ID, Source: t.Source, Timezone: t.Timezone, Place: place}
	}
	data, err := json.Marshal(value)
	if err != nil || len(data) > 64*1024 {
		return follows.Item{}, false, follows.ErrUnavailable
	}
	catalog, _, column, collection, _ := followTables(t.Kind)
	var item follows.Item
	var created bool
	err = historyTransaction(ctx, r.db, r.timeout, "follow write", func(ctx context.Context, tx *sql.Tx) error {
		if err := upsertReferenceTx(ctx, tx, t.Kind, t.ID, t.Source.Provider, t.Source.ID, data, detail.Meta.DataAsOf.UTC().Truncate(time.Microsecond)); err != nil {
			return err
		}
		// The catalog row lock serializes duplicate follows across app pools.
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+collection+` WHERE account_id=? AND `+column+`=?`, owner, t.ID).Scan(&count); err != nil {
			return err
		}
		created = count == 0
		if created {
			if _, err := tx.ExecContext(ctx, `INSERT INTO `+collection+` (account_id,`+column+`) VALUES (?,?)`, owner, t.ID); err != nil {
				return err
			}
		}
		var err error
		item, err = scanFollow(tx.QueryRowContext(ctx, `SELECT '`+t.Kind+`',c.snapshot,c.last_seen,f.followed_at FROM `+collection+` f JOIN `+catalog+` c ON c.`+column+`=f.`+column+` WHERE f.account_id=? AND f.`+column+`=?`, owner, t.ID))
		return err
	})
	if err != nil {
		return follows.Item{}, false, err
	}
	return item, created, nil
}
func (r *FollowRepository) Remove(ctx context.Context, owner, kind, id string) error {
	_, _, column, collection, err := followTables(kind)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	_, err = r.db.ExecContext(ctx, `DELETE FROM `+collection+` WHERE account_id=? AND `+column+`=?`, owner, id)
	if err != nil {
		return safeError("follow removal", err)
	}
	return nil
}
func (r *FollowRepository) List(ctx context.Context, owner string, q follows.Query) ([]follows.Item, error) {
	kinds := []string{"artist", "venue"}
	if q.Kind != "" {
		kinds = []string{q.Kind}
	}
	parts := []string{}
	args := []any{}
	for _, kind := range kinds {
		catalog, _, column, collection, err := followTables(kind)
		if err != nil {
			return nil, err
		}
		parts = append(parts, followSelect(kind, catalog, column, collection))
		args = append(args, owner)
	}
	query := `SELECT kind,snapshot,data_as_of,followed_at FROM (` + strings.Join(parts, ` UNION ALL `) + `) entries`
	if q.Cursor != nil {
		query += ` WHERE followed_at<? OR (followed_at=? AND (kind<? OR (kind=? AND target_id<?)))`
		args = append(args, q.Cursor.Before.UTC(), q.Cursor.Before.UTC(), q.Cursor.Kind, q.Cursor.Kind, q.Cursor.ID)
	}
	query += ` ORDER BY followed_at DESC,kind DESC,target_id DESC LIMIT ?`
	args = append(args, q.Limit+1)
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, safeError("follow collection read", err)
	}
	defer rows.Close()
	items := []follows.Item{}
	for rows.Next() {
		item, err := scanFollow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("follow collection read", err)
	}
	return items, nil
}
func (r *FollowRepository) States(ctx context.Context, owner, kind string, ids []string) (map[string]bool, error) {
	_, _, column, collection, err := followTables(kind)
	if err != nil {
		return nil, err
	}
	result := map[string]bool{}
	if len(ids) == 0 {
		return result, nil
	}
	args := []any{owner}
	for _, id := range ids {
		args = append(args, id)
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	rows, err := r.db.QueryContext(ctx, `SELECT `+column+` FROM `+collection+` WHERE account_id=? AND `+column+` IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`)`, args...)
	if err != nil {
		return nil, safeError("follow state read", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, safeError("follow state read", err)
		}
		result[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("follow state read", err)
	}
	return result, nil
}
