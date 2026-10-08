package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/radar"
)

type RadarRepository struct {
	db      *sql.DB
	timeout time.Duration
}

func (p *Pool) Radar() *RadarRepository { return &RadarRepository{p.db, p.config.QueryTimeout} }

var _ radar.Repository = (*RadarRepository)(nil)

func (r *RadarRepository) Read(ctx context.Context, owner string, at time.Time, freshFor time.Duration) (radar.Dataset, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	d := radar.Dataset{}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return d, safeError("radar read", err)
	}
	defer tx.Rollback()
	a, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE account_id=?`, owner))
	if err != nil {
		return d, err
	}
	d.Preferences = a.Preferences
	rows, err := tx.QueryContext(ctx, `SELECT 'artist',artist_id FROM artist_follows WHERE account_id=? UNION ALL SELECT 'venue',venue_id FROM venue_follows WHERE account_id=?`, owner, owner)
	if err != nil {
		return d, safeError("radar follows", err)
	}
	for rows.Next() {
		var f radar.Signal
		if err = rows.Scan(&f.Kind, &f.ID); err != nil {
			break
		}
		d.Follows = append(d.Follows, f)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return d, safeError("radar follows", err)
	}
	scope := radar.DateScope(d.Preferences, at)
	if scope.City == "" || scope.Country == "" {
		return d, nil
	}
	q := discovery.Query{City: scope.City, Country: scope.Country, StartDate: scope.StartDate, EndDate: scope.EndDate}
	d.Meta, d.Refresh, err = coverageTx(ctx, tx, q, freshFor)
	if err != nil {
		return d, safeError("radar coverage", err)
	}
	where, args := searchWhere(q)
	if len(d.Follows) > 0 || len(d.Preferences.CategoryIDs) > 0 {
		// Match by case-sensitive provider identities, never names. OR keeps follows
		// useful across categories; no joins duplicate multi-artist/venue events.
		clauses := []string{`(f.kind='artist' AND EXISTS (SELECT 1 FROM artist_follows a WHERE a.account_id=? AND a.artist_id=f.value))`, `(f.kind='venue' AND EXISTS (SELECT 1 FROM venue_follows v WHERE v.account_id=? AND v.venue_id=f.value))`}
		args = append(args, owner, owner)
		if len(d.Preferences.CategoryIDs) > 0 {
			clauses = append(clauses, `(f.kind='category' AND f.value IN (`+strings.TrimSuffix(strings.Repeat("?,", len(d.Preferences.CategoryIDs)), ",")+`))`)
			for _, id := range d.Preferences.CategoryIDs {
				args = append(args, id)
			}
		}
		where += ` AND EXISTS (SELECT 1 FROM event_search_facets f WHERE f.event_id=e.event_id AND (` + strings.Join(clauses, ` OR `) + `))`
	}
	rows, err = tx.QueryContext(ctx, `SELECT s.snapshot,s.data_as_of,EXISTS(SELECT 1 FROM event_detail_tasks d WHERE d.event_id=e.event_id) FROM events e JOIN event_snapshots s ON s.event_id=e.event_id WHERE `+where, args...)
	if err != nil {
		return d, safeError("radar events", err)
	}
	for rows.Next() {
		var c radar.Candidate
		var data []byte
		if err = rows.Scan(&data, &c.DataAsOf, &c.Unconfirmed); err != nil {
			break
		}
		if err = json.Unmarshal(data, &c.Event); err != nil {
			break
		}
		d.Candidates = append(d.Candidates, c)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return d, safeError("radar events", err)
	}
	return d, nil
}
