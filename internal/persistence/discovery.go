package persistence

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/models"
)

type DiscoveryRepository struct {
	db      *sql.DB
	timeout time.Duration
}

func (p *Pool) Discovery() *DiscoveryRepository {
	return &DiscoveryRepository{p.db, p.config.QueryTimeout}
}

var _ discovery.Catalog = (*DiscoveryRepository)(nil)

// Projections are replaced only with the winning public snapshot, in the same
// transaction. Activity tables are deliberately absent from discovery queries.
func projectSearchTx(ctx context.Context, tx *sql.Tx, e models.Event) error {
	for _, table := range []string{"event_search_places", "event_search_facets"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE event_id=?`, e.ID); err != nil {
			return err
		}
	}
	places := []models.Place{}
	if e.Place != nil {
		places = append(places, *e.Place)
	}
	for _, v := range e.Venues {
		places = append(places, v.Place)
	}
	for _, p := range places {
		if len(p.City) > 120 || len(p.CountryCode) > 2 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO event_search_places (event_id,city,country) VALUES (?,?,?)`, e.ID, p.City, strings.ToUpper(p.CountryCode)); err != nil {
			return err
		}
	}
	type facet struct{ kind, value string }
	facets := []facet{}
	for _, a := range e.Artists {
		facets = append(facets, facet{"artist", a.ID})
	}
	for _, v := range e.Venues {
		facets = append(facets, facet{"venue", v.ID})
	}
	for _, c := range e.Classifications {
		if c.Segment != nil {
			facets = append(facets, facet{"category", c.Segment.ID})
		}
		if c.Genre != nil {
			facets = append(facets, facet{"genre", c.Genre.ID})
		}
	}
	for _, f := range facets {
		if f.value == "" || len(f.value) > 300 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO event_search_facets (event_id,kind,value) VALUES (?,?,?)`, e.ID, f.kind, f.value); err != nil {
			return err
		}
	}
	return nil
}

func searchWhere(q discovery.Query) (string, []any) {
	where, args := `e.local_date BETWEEN ? AND ? AND e.date_tba=0 AND e.date_tbd=0`, []any{q.StartDate, q.EndDate}
	if q.City != "" || q.Country != "" {
		where += ` AND EXISTS (SELECT 1 FROM event_search_places p WHERE p.event_id=e.event_id`
		if q.City != "" {
			where += ` AND p.city=?`
			args = append(args, q.City)
		}
		if q.Country != "" {
			where += ` AND p.country=?`
			args = append(args, q.Country)
		}
		where += `)`
	}
	for _, f := range []struct{ kind, value string }{{"category", q.CategoryID}, {"genre", q.GenreID}, {"artist", q.ArtistID}, {"venue", q.VenueID}} {
		if f.value != "" {
			where += ` AND EXISTS (SELECT 1 FROM event_search_facets f WHERE f.event_id=e.event_id AND f.kind=? AND f.value=?)`
			args = append(args, f.kind, f.value)
		}
	}
	// Literal, case-insensitive substring matching includes short artist/venue
	// names that full-text stopword/minimum-word rules would silently discard.
	if q.Keyword != "" {
		pattern := "%" + strings.NewReplacer("=", "==", "%", "=%", "_", "=_").Replace(q.Keyword) + "%"
		where += ` AND (e.name LIKE ? ESCAPE '=' OR EXISTS (SELECT 1 FROM JSON_TABLE(e.artists,'$[*]' COLUMNS(name VARCHAR(1024) PATH '$.name')) a WHERE a.name LIKE ? ESCAPE '=') OR EXISTS (SELECT 1 FROM JSON_TABLE(e.venues,'$[*]' COLUMNS(name VARCHAR(1024) PATH '$.name')) v WHERE v.name LIKE ? ESCAPE '='))`
		args = append(args, pattern, pattern, pattern)
	}
	return where, args
}

func (r *DiscoveryRepository) Search(ctx context.Context, q discovery.Query, freshFor time.Duration) (discovery.CatalogRead, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	read := discovery.CatalogRead{List: models.EventList{Items: []models.Event{}}}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return read, safeError("discover read", err)
	}
	defer tx.Rollback()
	meta, refresh, err := coverageTx(ctx, tx, q, freshFor)
	if err != nil {
		return read, safeError("discover coverage", err)
	}
	read.List.Meta, read.Refresh = meta, refresh
	where, args := searchWhere(q)
	from := ` FROM events e JOIN event_snapshots s ON s.event_id=e.event_id WHERE ` + where
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*)`+from, args...).Scan(&read.List.Total); err != nil {
		return read, safeError("discover count", err)
	}
	order := `e.local_date,e.local_time,e.name,e.event_id`
	if q.Sort == "date_desc" {
		order = `e.local_date DESC,e.local_time DESC,e.name,e.event_id`
	}
	if q.Sort == "name_asc" {
		order = `e.name,e.local_date,e.local_time,e.event_id`
	}
	args = append(args, q.Limit, q.Page*q.Limit)
	rows, err := tx.QueryContext(ctx, `SELECT s.snapshot,s.data_as_of,EXISTS(SELECT 1 FROM event_detail_tasks d WHERE d.event_id=e.event_id)`+from+` ORDER BY `+order+` LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return read, safeError("discover page", err)
	}
	for rows.Next() {
		var data []byte
		var at time.Time
		var unconfirmed bool
		var event models.Event
		if err = rows.Scan(&data, &at, &unconfirmed); err != nil {
			break
		}
		if err = json.Unmarshal(data, &event); err != nil {
			break
		}
		read.List.Items = append(read.List.Items, event)
		if read.List.Meta.DataAsOf.IsZero() || at.Before(read.List.Meta.DataAsOf) {
			read.List.Meta.DataAsOf = at
		}
		if time.Since(at) > freshFor || unconfirmed {
			read.List.Meta.Stale = true
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return read, safeError("discover page", err)
	}
	if (q.Page+1)*q.Limit < read.List.Total && (q.Page+1)*q.Limit <= 1000000 {
		cursor := q.NextCursor()
		read.List.NextCursor = &cursor
	}
	read.List.Limited = read.List.Meta.Coverage.Status == "partial"
	return read, nil
}

func coverageTx(ctx context.Context, tx *sql.Tx, q discovery.Query, freshFor time.Duration) (models.Freshness, bool, error) {
	start, _ := time.Parse(time.DateOnly, q.StartDate)
	end, _ := time.Parse(time.DateOnly, q.EndDate)
	days := int(end.Sub(start)/(24*time.Hour)) + 1
	c := &models.Coverage{Status: "not_collected", TotalDays: days}
	meta := models.Freshness{Coverage: c}
	scope := q.CollectionScope()
	var count, fresh int
	var oldest sql.NullTime
	if q.City != "" && q.Country != "" {
		err := tx.QueryRowContext(ctx, `SELECT COUNT(last_success),COALESCE(SUM(last_success>=TIMESTAMPADD(MICROSECOND,-?,UTC_TIMESTAMP(6)) AND last_failure=''),0),MIN(last_success) FROM collection_tasks WHERE city=? AND country=? AND local_date BETWEEN ? AND ?`, freshFor.Microseconds(), q.City, q.Country, q.StartDate, q.EndDate).Scan(&count, &fresh, &oldest)
		if err != nil {
			return meta, false, err
		}
		c.CollectedDays = count
		if count > 0 {
			meta.DataAsOf = oldest.Time
			c.Status = "partial"
		}
		if count == days {
			c.Status = "complete"
			meta.Stale = fresh < days
			if meta.Stale {
				c.Status = "stale"
			}
		}
		if fresh == days {
			return meta, false, nil
		}
	}
	// A successfully collected enclosing on-demand range covers narrowed dates
	// and all local filters. Broader/other-country queries need their own evidence.
	rows, err := tx.QueryContext(ctx, `SELECT last_success,data_as_of,status,last_success<TIMESTAMPADD(MICROSECOND,-?,UTC_TIMESTAMP(6)),(next_refresh>UTC_TIMESTAMP(6) OR COALESCE(lease_until>UTC_TIMESTAMP(6),0)) FROM discovery_scopes WHERE city=? AND country=? AND start_date<=? AND end_date>=? AND artist_id=? AND venue_id=? ORDER BY last_success DESC`, freshFor.Microseconds(), scope.City, scope.Country, scope.StartDate, scope.EndDate, scope.ArtistID, scope.VenueID)
	if err != nil {
		return meta, false, err
	}
	defer rows.Close()
	refresh := true
	for rows.Next() {
		var success, at sql.NullTime
		var status string
		var old sql.NullBool
		var waiting bool
		if err := rows.Scan(&success, &at, &status, &old, &waiting); err != nil {
			return meta, false, err
		}
		if success.Valid {
			if !old.Bool && status == "complete" {
				c.CollectedDays, c.Status = days, "complete"
				meta.DataAsOf, meta.Stale = success.Time, false
				return meta, false, nil
			}
			if c.CollectedDays < days || success.Time.After(meta.DataAsOf) {
				c.CollectedDays, c.Status = days, "stale"
				meta.DataAsOf, meta.Stale = success.Time, true
			}
		} else if at.Valid && c.CollectedDays < days {
			c.Status = "partial"
			meta.DataAsOf = at.Time
			meta.Stale = time.Since(at.Time) >= freshFor || status == "failed"
		}
		if waiting {
			refresh = false
		}
	}
	if err := rows.Err(); err != nil {
		return meta, false, err
	}
	if c.Status == "not_collected" {
		// Scheduled partial pages are evidence even before a complete day.
		var at sql.NullTime
		if err := tx.QueryRowContext(ctx, `SELECT MAX(p.collected_at) FROM collection_tasks t JOIN collection_runs r ON r.task_id=t.task_id JOIN collection_pages p ON p.run_id=r.run_id WHERE t.city=? AND t.country=? AND t.local_date BETWEEN ? AND ?`, q.City, q.Country, q.StartDate, q.EndDate).Scan(&at); err != nil {
			return meta, false, err
		}
		if at.Valid {
			c.Status = "partial"
			meta.DataAsOf = at.Time
			meta.Stale = true
		}
	}
	return meta, refresh, nil
}

func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("collection ownership unavailable")
	}
	return hex.EncodeToString(b[:]), nil
}

func (r *DiscoveryRepository) ClaimSearch(ctx context.Context, q discovery.Query) (string, error) {
	q = q.CollectionScope()
	token, err := newToken()
	if err != nil {
		return "", err
	}
	err = historyTransaction(ctx, r.db, r.timeout, "discover claim", func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO discovery_scopes (scope_id,city,country,start_date,end_date,artist_id,venue_id) VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE scope_id=scope_id`, q.ScopeID(), q.City, q.Country, q.StartDate, q.EndDate, q.ArtistID, q.VenueID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `UPDATE discovery_scopes SET token=?,lease_until=TIMESTAMPADD(SECOND,30,UTC_TIMESTAMP(6)),last_attempt=UTC_TIMESTAMP(6),pages=0,low_total=0,high_total=0,status='running' WHERE scope_id=? AND next_refresh<=UTC_TIMESTAMP(6) AND (lease_until IS NULL OR lease_until<=UTC_TIMESTAMP(6))`, token, q.ScopeID())
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if n == 0 {
			token = ""
		}
		return err
	})
	return token, err
}

func lockSearch(ctx context.Context, tx *sql.Tx, q discovery.Query, token string) (int, error) {
	var pages int
	err := tx.QueryRowContext(ctx, `SELECT pages FROM discovery_scopes WHERE scope_id=? AND token=? AND lease_until>UTC_TIMESTAMP(6) FOR UPDATE`, q.ScopeID(), token).Scan(&pages)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, history.ErrLeaseLost
	}
	return pages, err
}

func (r *DiscoveryRepository) SearchPage(ctx context.Context, q discovery.Query, token string, page int, list models.EventList) error {
	if page < 0 || page >= 10 || list.Meta.Stale || list.Meta.DataAsOf.IsZero() || len(list.Items) > 100 || list.Total < 0 {
		return fmt.Errorf("invalid discover collection page")
	}
	// A page is an atomic batch of up to 100 observations/catalog projections,
	// not one ordinary read. Its deadline is also clipped by the caller's bounded
	// collection context; never retry an uncertain commit.
	return historyTransaction(ctx, r.db, max(r.timeout, 10*time.Second), "discover ingestion", func(ctx context.Context, tx *sql.Tx) error {
		pages, err := lockSearch(ctx, tx, q, token)
		if err != nil {
			return err
		}
		if page < pages {
			return nil
		}
		if page != pages {
			return fmt.Errorf("noncontiguous discover page")
		}
		for _, e := range list.Items {
			d := models.EventDetail{Item: e, Meta: list.Meta}
			args, data, err := prepareSnapshot(d)
			if err != nil {
				return err
			}
			if err := upsertSnapshotTx(ctx, tx, d, args, data); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO discovery_scope_events (scope_id,event_id,token) VALUES (?,?,?) ON DUPLICATE KEY UPDATE token=VALUES(token)`, q.ScopeID(), e.ID, token); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE discovery_scopes SET data_as_of=?,low_total=IF(pages=0,?,LEAST(low_total,?)),high_total=GREATEST(high_total,?),pages=pages+1,lease_until=TIMESTAMPADD(SECOND,30,UTC_TIMESTAMP(6)) WHERE scope_id=?`, list.Meta.DataAsOf.UTC(), list.Total, list.Total, list.Total, q.ScopeID())
		return err
	})
}

func (r *DiscoveryRepository) FinishSearch(ctx context.Context, q discovery.Query, token, status string, delay time.Duration) (string, error) {
	if (status != "complete" && status != "limited" && status != "partial" && status != "failed") || delay < time.Second || delay > 7*24*time.Hour {
		return "", fmt.Errorf("invalid discover outcome")
	}
	err := historyTransaction(ctx, r.db, r.timeout, "discover completion", func(ctx context.Context, tx *sql.Tx) error {
		pages, err := lockSearch(ctx, tx, q, token)
		if err != nil {
			return err
		}
		if status == "complete" {
			var low, high, distinct int
			if err := tx.QueryRowContext(ctx, `SELECT low_total,high_total,(SELECT COUNT(*) FROM discovery_scope_events WHERE scope_id=? AND token=?) FROM discovery_scopes WHERE scope_id=?`, q.ScopeID(), token, q.ScopeID()).Scan(&low, &high, &distinct); err != nil {
				return err
			}
			if pages == 0 || low != high || distinct != high {
				status = "partial"
			}
		}
		// Revisit only known events missing from this scope. No deletion/status
		// inference; detail calls are separately leased, paced and quota-accounted.
		if status == "complete" {
			if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO event_detail_tasks (event_id) SELECT event_id FROM discovery_scope_events WHERE scope_id=? AND token<>?`, q.ScopeID(), token); err != nil {
				return err
			}
			where, args := searchWhere(q.CollectionScope())
			args = append(args, q.ScopeID(), token)
			if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO event_detail_tasks (event_id) SELECT e.event_id FROM events e JOIN event_snapshots s ON s.event_id=e.event_id WHERE `+where+` AND NOT EXISTS (SELECT 1 FROM discovery_scope_events current WHERE current.scope_id=? AND current.token=? AND current.event_id=e.event_id)`, args...); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `UPDATE discovery_scopes SET token=NULL,lease_until=NULL,status=?,last_success=IF(?='complete',data_as_of,last_success),next_refresh=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)) WHERE scope_id=?`, status, status, delay.Microseconds(), q.ScopeID())
		return err
	})
	return status, err
}

func (r *DiscoveryRepository) Snapshot(ctx context.Context, id string) (models.EventDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	detail, err := (&SavedRepository{r.db, r.timeout}).Snapshot(ctx, id)
	if err != nil {
		return detail, err
	}
	// Omitted events awaiting detail verification are last-known facts even
	// when the previous observation is still inside the usual freshness TTL.
	err = r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM event_detail_tasks WHERE event_id=?)`, id).Scan(&detail.Meta.Stale)
	if err != nil {
		return models.EventDetail{}, safeError("event confirmation", err)
	}
	return detail, nil
}
func (r *DiscoveryRepository) Observe(ctx context.Context, d models.EventDetail) error {
	if d.Meta.Stale {
		return nil
	}
	args, data, err := prepareSnapshot(d)
	if err != nil {
		return err
	}
	return historyTransaction(ctx, r.db, r.timeout, "event observation", func(ctx context.Context, tx *sql.Tx) error { return upsertSnapshotTx(ctx, tx, d, args, data) })
}

func (r *DiscoveryRepository) History(ctx context.Context, id string, before time.Time, limit int) (*models.EventHistory, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("history limit must be from 1 to 100")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	h := &models.EventHistory{Changes: []models.EventChange{}}
	var changed sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT first_seen,last_seen,last_changed FROM event_history_state WHERE event_id=?`, id).Scan(&h.FirstSeen, &h.LastSeen, &changed)
	if errors.Is(err, sql.ErrNoRows) {
		var exists int
		if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE event_id=?`, id).Scan(&exists); err != nil {
			return nil, safeError("history read", err)
		}
		if exists == 0 {
			return nil, discovery.ErrNotFound
		}
		return nil, nil
	}
	if err != nil {
		return nil, safeError("history read", err)
	}
	if changed.Valid {
		h.LastChanged = &changed.Time
	}
	query, args := `SELECT observed_at,changes FROM event_observations WHERE event_id=? AND JSON_LENGTH(changes)>0`, []any{id}
	if !before.IsZero() {
		query += ` AND observed_at<?`
		args = append(args, before.UTC())
	}
	query += ` ORDER BY observed_at DESC LIMIT ?`
	args = append(args, limit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, safeError("history changes", err)
	}
	defer rows.Close()
	n := 0
	var last time.Time
	for rows.Next() {
		var at time.Time
		var data []byte
		var changes []history.Change
		if err := rows.Scan(&at, &data); err != nil {
			return nil, safeError("history changes", err)
		}
		if n == limit {
			h.NextBefore = &last
			break
		}
		n++
		last = at
		if err := json.Unmarshal(data, &changes); err != nil {
			return nil, fmt.Errorf("invalid history changes")
		}
		for _, c := range changes {
			summary := history.ChangeSummary(c)
			h.Changes = append(h.Changes, models.EventChange{ObservedAt: at, Kind: c.Kind, Summary: summary, Before: c.Before, After: c.After})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("history changes", err)
	}
	rows.Close()
	ids := map[string]bool{}
	for _, c := range h.Changes {
		if c.Kind == "venue" {
			var before []string
			if json.Unmarshal(c.Before, &before) == nil {
				for _, id := range before {
					ids[id] = true
				}
			}
		}
	}
	if len(ids) > 0 {
		args := []any{}
		for id := range ids {
			args = append(args, id)
		}
		names := map[string]string{}
		rows, err := r.db.QueryContext(ctx, `SELECT venue_id,JSON_UNQUOTE(JSON_EXTRACT(snapshot,'$.name')) FROM venues WHERE venue_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(args)), ",")+`)`, args...)
		if err != nil {
			return nil, safeError("history venues", err)
		}
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return nil, safeError("history venues", err)
			}
			names[id] = name
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, safeError("history venues", err)
		}
		for i, c := range h.Changes {
			if c.Kind != "venue" {
				continue
			}
			var before []string
			if json.Unmarshal(c.Before, &before) != nil || len(before) == 0 {
				continue
			}
			for j, id := range before {
				if names[id] != "" {
					before[j] = names[id]
				}
			}
			h.Changes[i].Summary = "Moved from " + strings.Join(before, ", ")
		}
	}
	return h, nil
}

// OperationalStatus is an operator-only aggregate. It exposes no key hashes,
// account identities, search keywords, or raw provider errors.
func (r *DiscoveryRepository) OperationalStatus(ctx context.Context, freshFor time.Duration) (map[string]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	result := map[string]int64{}
	for _, item := range []struct {
		name, query string
		args        []any
	}{
		{"collected_days", `SELECT COUNT(*) FROM collection_tasks WHERE last_success IS NOT NULL`, nil},
		{"stale_days", `SELECT COUNT(*) FROM collection_tasks WHERE last_success IS NULL OR last_success<TIMESTAMPADD(MICROSECOND,-?,UTC_TIMESTAMP(6))`, []any{freshFor.Microseconds()}},
		{"failed_days", `SELECT COUNT(*) FROM collection_tasks WHERE last_failure<>''`, nil},
		{"on_demand_scopes", `SELECT COUNT(*) FROM discovery_scopes`, nil},
		{"stale_scopes", `SELECT COUNT(*) FROM discovery_scopes WHERE last_success IS NULL OR last_success<TIMESTAMPADD(MICROSECOND,-?,UTC_TIMESTAMP(6))`, []any{freshFor.Microseconds()}},
		{"partial_scopes", `SELECT COUNT(*) FROM discovery_scopes WHERE status IN ('limited','partial') OR (status='running' AND lease_until<UTC_TIMESTAMP(6))`, nil},
		{"failed_scopes", `SELECT COUNT(*) FROM discovery_scopes WHERE status='failed'`, nil},
		{"pending_details", `SELECT COUNT(*) FROM event_detail_tasks`, nil},
		{"failed_details", `SELECT COUNT(*) FROM event_detail_tasks WHERE last_failure<>''`, nil},
		{"quota_used", `SELECT COALESCE(SUM(used),0) FROM provider_budgets WHERE window_start>TIMESTAMPADD(HOUR,-24,UTC_TIMESTAMP(6))`, nil},
		{"quota_budget", `SELECT COALESCE(SUM(budget),0) FROM provider_budgets WHERE window_start>TIMESTAMPADD(HOUR,-24,UTC_TIMESTAMP(6))`, nil},
		{"provider_cooldowns", `SELECT COUNT(*) FROM provider_budgets WHERE blocked_until>UTC_TIMESTAMP(6)`, nil},
	} {
		var n int64
		if err := r.db.QueryRowContext(ctx, item.query, item.args...).Scan(&n); err != nil {
			return nil, safeError("collection status", err)
		}
		result[item.name] = n
	}
	return result, nil
}
