package persistence

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"slices"
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

func (r *RadarRepository) Read(ctx context.Context, owner string, at time.Time, freshFor time.Duration, page radar.Query) (radar.Dataset, error) {
	return r.readAt(ctx, owner, at, freshFor, page, time.Time{})
}

func (r *RadarRepository) readAt(ctx context.Context, owner string, at time.Time, freshFor time.Duration, page radar.Query, now time.Time) (radar.Dataset, error) {
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
	rows, err := tx.QueryContext(ctx, `SELECT 'artist' AS kind,artist_id AS id FROM artist_follows WHERE account_id=? UNION ALL SELECT 'venue',venue_id FROM venue_follows WHERE account_id=? ORDER BY kind,id`, owner, owner)
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
	d.Meta, d.Refresh, _, err = coverageStampAtTx(ctx, tx, q, freshFor, now)
	if err != nil {
		return d, safeError("radar coverage", err)
	}
	where, args := searchWhere(q)
	where += ` AND BINARY e.status NOT IN ('cancelled','canceled','postponed') AND (e.start_utc IS NULL OR e.start_utc>=?)`
	args = append(args, at)
	// EXISTS scores signal types once, regardless of lineup/classification size.
	score := `0`
	var scoreArgs []any
	personalized := len(d.Follows) > 0 || len(d.Preferences.CategoryIDs) > 0
	if personalized {
		// Signals were read in this same snapshot; avoid per-event probes for
		// signal types the account does not have (especially category-only Radar).
		artist, venue := false, false
		for _, follow := range d.Follows {
			artist = artist || follow.Kind == "artist"
			venue = venue || follow.Kind == "venue"
		}
		if artist {
			score += ` + 4*EXISTS(SELECT 1 FROM event_search_facets f JOIN artist_follows a ON a.artist_id=f.value AND a.account_id=? WHERE f.event_id=e.event_id AND f.kind='artist')`
			scoreArgs = append(scoreArgs, owner)
		}
		if venue {
			score += ` + 2*EXISTS(SELECT 1 FROM event_search_facets f JOIN venue_follows v ON v.venue_id=f.value AND v.account_id=? WHERE f.event_id=e.event_id AND f.kind='venue')`
			scoreArgs = append(scoreArgs, owner)
		}
		if len(d.Preferences.CategoryIDs) > 0 {
			score += ` + EXISTS(SELECT 1 FROM event_search_facets f WHERE f.event_id=e.event_id AND f.kind='category' AND f.value IN (` + strings.TrimSuffix(strings.Repeat("?,", len(d.Preferences.CategoryIDs)), ",") + `))`
			for _, id := range d.Preferences.CategoryIDs {
				scoreArgs = append(scoreArgs, id)
			}
		}
	}
	// The non-restrictive LIMIT is a derived-merge barrier: evaluate correlated
	// signal scores once per event, rather than inlining them into every revision
	// word, eligibility predicate and ordering expression. The revision subquery
	// uses the same barrier to compute each SHA-256 only once for its four words.
	cte := `WITH ranked AS (SELECT e.event_id,e.local_date,
CASE WHEN e.local_time IS NOT NULL AND e.local_time<>'' AND e.time_tba=0 AND e.no_specific_time=0 THEN e.local_time ELSE '24:00:00' END AS clock,
(` + score + `) AS score,s.data_as_of,
EXISTS(SELECT 1 FROM event_detail_tasks d WHERE d.event_id=e.event_id) AS unconfirmed
FROM events e JOIN event_snapshots s ON s.event_id=e.event_id WHERE ` + where + ` LIMIT 18446744073709551615), eligible AS (SELECT * FROM ranked`
	if personalized {
		cte += ` WHERE score>0`
	}
	cte += `) `
	args = append(scoreArgs, args...)
	// data_as_of is the snapshot's strictly monotonic revision: equal/backdated
	// writes cannot replace its JSON. XOR all four SHA-256 words plus count gives
	// an order-independent fingerprint without transferring the dataset or using
	// a truncatable GROUP_CONCAT. Include omission state and ordering keys, but
	// never wall-clock staleness or coverage receipts.
	var words [4]uint64
	var count int
	var oldest sql.NullTime
	var unconfirmed int
	anchor := ""
	if page.Cursor != nil {
		anchor = page.Cursor.After
	}
	var anchorScore sql.NullInt64
	var anchorDate, anchorClock sql.NullString
	revisionSQL := cte + `SELECT COUNT(*),MIN(data_as_of),COALESCE(MAX(unconfirmed),0),
BIT_XOR(CAST(CONV(SUBSTRING(revision,1,16),16,10) AS UNSIGNED)),
BIT_XOR(CAST(CONV(SUBSTRING(revision,17,16),16,10) AS UNSIGNED)),
BIT_XOR(CAST(CONV(SUBSTRING(revision,33,16),16,10) AS UNSIGNED)),
BIT_XOR(CAST(CONV(SUBSTRING(revision,49,16),16,10) AS UNSIGNED)),
MAX(IF(event_id=?,score,NULL)),MAX(IF(event_id=?,local_date,NULL)),MAX(IF(event_id=?,clock,NULL))
FROM (SELECT eligible.*,SHA2(CONCAT_WS(CHAR(0),event_id,data_as_of,unconfirmed,score,local_date,clock),256) AS revision FROM eligible LIMIT 18446744073709551615) revisions`
	revisionArgs := append(slices.Clone(args), anchor, anchor, anchor)
	if err = tx.QueryRowContext(ctx, revisionSQL, revisionArgs...).Scan(&count, &oldest, &unconfirmed, &words[0], &words[1], &words[2], &words[3], &anchorScore, &anchorDate, &anchorClock); err != nil {
		return d, safeError("radar revision", err)
	}
	categories := slices.Clone(d.Preferences.CategoryIDs)
	slices.Sort(categories)
	version, _ := json.Marshal(struct {
		Version    int
		Scope      radar.Scope
		Timezone   string
		Categories []string
		Follows    []radar.Signal
		Count      int
		Words      [4]uint64
	}{2, scope, d.Preferences.Timezone, categories, d.Follows, count, words})
	hash := sha256.Sum256(version)
	d.Revision = hex.EncodeToString(hash[:])
	if page.Cursor != nil {
		if page.Cursor.Digest != d.Revision {
			return d, radar.ErrChanged
		}
		if !anchorScore.Valid {
			return d, radar.InvalidCursor()
		}
	}
	if oldest.Valid && (d.Meta.DataAsOf.IsZero() || oldest.Time.Before(d.Meta.DataAsOf)) {
		d.Meta.DataAsOf = oldest.Time
	}
	d.Oldest = oldest.Time
	d.Meta.Stale = d.Meta.Stale || unconfirmed != 0
	// Select only lightweight keys with LIMIT+1. Fetch JSON separately so even
	// the lookahead record and discarded sort rows never load full snapshots.
	pageSQL := cte + `SELECT event_id FROM eligible`
	pageArgs := slices.Clone(args)
	if page.Cursor != nil {
		pageSQL += ` WHERE score<? OR (score=? AND (local_date,clock,event_id)>(?,?,?))`
		pageArgs = append(pageArgs, anchorScore.Int64, anchorScore.Int64, anchorDate.String, anchorClock.String, anchor)
	}
	pageSQL += ` ORDER BY score DESC,local_date,clock,event_id LIMIT ?`
	pageArgs = append(pageArgs, page.Limit+1)
	rows, err = tx.QueryContext(ctx, pageSQL, pageArgs...)
	if err != nil {
		return d, safeError("radar page", err)
	}
	ids := make([]string, 0, page.Limit+1)
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		ids = append(ids, id)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return d, safeError("radar page", err)
	}
	d.HasMore = len(ids) > page.Limit
	if d.HasMore {
		ids = ids[:page.Limit]
	}
	if len(ids) == 0 {
		return d, nil
	}
	params := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	snapshotArgs := make([]any, 0, len(ids)*2)
	for _, id := range ids {
		snapshotArgs = append(snapshotArgs, id)
	}
	snapshotArgs = append(snapshotArgs, snapshotArgs...)
	rows, err = tx.QueryContext(ctx, `SELECT s.snapshot,s.data_as_of,EXISTS(SELECT 1 FROM event_detail_tasks d WHERE d.event_id=s.event_id) FROM event_snapshots s WHERE s.event_id IN (`+params+`) ORDER BY FIELD(s.event_id,`+params+`)`, snapshotArgs...)
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
