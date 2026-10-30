package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/models"
)

// Multi-row statements stay well below MariaDB's 65,535-placeholder limit and
// default packet size; one row may still carry a full 1 MiB snapshot.
const (
	maxBatchBytes  = 512 * 1024
	maxBatchParams = 16000
)

func placeholders(n int) string { return strings.TrimSuffix(strings.Repeat("?,", n), ",") }

func insertRowsTx(ctx context.Context, tx *sql.Tx, prefix, suffix string, rows [][]any) error {
	for len(rows) > 0 {
		n, size, params := 0, 0, 0
		for ; n < len(rows); n++ {
			cost := 128
			for _, v := range rows[n] {
				if s, ok := v.(string); ok {
					cost += 2 * len(s)
				} else {
					cost += 32
				}
			}
			if n > 0 && (size+cost > maxBatchBytes || params+len(rows[n]) > maxBatchParams) {
				break
			}
			size += cost
			params += len(rows[n])
		}
		args, values := make([]any, 0, params), make([]string, 0, n)
		for _, row := range rows[:n] {
			args = append(args, row...)
			values = append(values, "("+placeholders(len(row))+")")
		}
		if _, err := tx.ExecContext(ctx, prefix+strings.Join(values, ",")+suffix, args...); err != nil {
			return err
		}
		rows = rows[n:]
	}
	return nil
}

func queryRowsTx(ctx context.Context, tx *sql.Tx, query string, args []any, scan func(*sql.Rows) error) error {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

type pageEvent struct {
	models.Event
	args []any
	data []byte
}

// writePageTx is the set-based equivalent of applying upsertSnapshotTx and a
// run receipt to each event in page order: repeated events are first-writer-wins
// at one data_as_of. Parent rows are locked in event-ID order before dependent
// tables, keeping the shared event-first ordering deadlock-safe across pages.
func writePageTx(ctx context.Context, tx *sql.Tx, runID string, list models.EventList) error {
	invalidateDiscoverCounts()
	at := list.Meta.DataAsOf.UTC().Truncate(time.Microsecond)
	page, seen := []pageEvent{}, map[string]bool{}
	for _, e := range list.Items {
		args, data, err := prepareSnapshot(models.EventDetail{Item: e, Meta: list.Meta})
		if err != nil {
			return err
		}
		if !seen[e.ID] {
			seen[e.ID] = true
			page = append(page, pageEvent{e, args, data})
		}
	}
	if len(page) == 0 {
		return nil
	}
	sorted := slices.Clone(page)
	slices.SortFunc(sorted, func(a, b pageEvent) int { return strings.Compare(a.ID, b.ID) })
	ids, events, providers, keys := make([]any, len(sorted)), [][]any{}, [][]any{}, []any{}
	for i, e := range sorted {
		ids[i] = e.ID
		events = append(events, e.args)
		providers = append(providers, []any{e.Source.Provider, e.Source.ID, e.ID})
		keys = append(keys, e.Source.Provider, e.Source.ID)
	}
	if err := insertRowsTx(ctx, tx, `INSERT INTO events (`+eventColumns+`) VALUES `, ` ON DUPLICATE KEY UPDATE event_id=event_id`, events); err != nil {
		return err
	}
	if err := insertRowsTx(ctx, tx, `INSERT INTO event_providers (provider,source_id,event_id) VALUES `, ` ON DUPLICATE KEY UPDATE event_id=event_id`, providers); err != nil {
		return err
	}
	mapped := map[string]string{}
	err := queryRowsTx(ctx, tx, `SELECT provider,source_id,event_id FROM event_providers WHERE (provider,source_id) IN (`+strings.TrimSuffix(strings.Repeat("(?,?),", len(sorted)), ",")+`)`, keys, func(rows *sql.Rows) error {
		var provider, source, id string
		err := rows.Scan(&provider, &source, &id)
		mapped[provider+":"+source] = id
		return err
	})
	if err != nil {
		return err
	}
	for _, e := range sorted {
		if mapped[e.Source.Provider+":"+e.Source.ID] != e.ID {
			return fmt.Errorf("provider mapping cannot change identity")
		}
	}
	last := map[string]time.Time{}
	err = queryRowsTx(ctx, tx, `SELECT event_id,data_as_of FROM event_snapshots WHERE event_id IN (`+placeholders(len(ids))+`) ORDER BY event_id FOR UPDATE`, ids, func(rows *sql.Rows) error {
		var id string
		var t time.Time
		err := rows.Scan(&id, &t)
		last[id] = t
		return err
	})
	if err != nil {
		return err
	}
	// Only a newer public snapshot replaces event metadata and its projection.
	winners, winnerIDs, places, facets := [][]any{}, []any{}, [][]any{}, [][]any{}
	for _, e := range sorted {
		if !at.After(last[e.ID]) {
			continue
		}
		winners = append(winners, e.args)
		winnerIDs = append(winnerIDs, e.ID)
		for _, p := range searchPlaces(e.Event) {
			if len(p.City) <= 120 && len(p.CountryCode) <= 2 {
				places = append(places, []any{e.ID, p.City, strings.ToUpper(p.CountryCode)})
			}
		}
		for _, f := range searchFacets(e.Event) {
			if f.value != "" && len(f.value) <= 300 {
				facets = append(facets, []any{e.ID, f.kind, f.value})
			}
		}
	}
	if err := insertRowsTx(ctx, tx, `INSERT INTO events (`+eventColumns+`) VALUES `, ` ON DUPLICATE KEY UPDATE `+eventUpdate, winners); err != nil {
		return err
	}
	if len(winnerIDs) > 0 {
		for _, table := range []string{"event_search_places", "event_search_facets"} {
			if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE event_id IN (`+placeholders(len(winnerIDs))+`)`, winnerIDs...); err != nil {
				return err
			}
		}
	}
	if err := insertRowsTx(ctx, tx, `INSERT IGNORE INTO event_search_places (event_id,city,country) VALUES `, "", places); err != nil {
		return err
	}
	if err := insertRowsTx(ctx, tx, `INSERT IGNORE INTO event_search_facets (event_id,kind,value) VALUES `, "", facets); err != nil {
		return err
	}
	if err := observePageTx(ctx, tx, at, page, ids); err != nil {
		return err
	}
	snapshots := [][]any{}
	for _, e := range sorted {
		snapshots = append(snapshots, []any{e.ID, string(e.data), at})
	}
	if err := insertRowsTx(ctx, tx, `INSERT INTO event_snapshots (event_id,snapshot,data_as_of) VALUES `, ` ON DUPLICATE KEY UPDATE snapshot=IF(VALUES(data_as_of)>data_as_of,VALUES(snapshot),snapshot), data_as_of=GREATEST(data_as_of,VALUES(data_as_of))`, snapshots); err != nil {
		return err
	}
	// Receipts are INSERT-only and keep their first observation; the claim lock
	// serializes writers of one run, so read-then-insert cannot race.
	received := map[string]bool{}
	err = queryRowsTx(ctx, tx, `SELECT event_id FROM collection_run_events WHERE run_id=? AND event_id IN (`+placeholders(len(ids))+`)`, append([]any{runID}, ids...), func(rows *sql.Rows) error {
		var id string
		err := rows.Scan(&id)
		received[id] = true
		return err
	})
	if err != nil {
		return err
	}
	receipts := [][]any{}
	for _, e := range sorted {
		if !received[e.ID] {
			receipts = append(receipts, []any{runID, e.ID, at})
		}
	}
	return insertRowsTx(ctx, tx, `INSERT INTO collection_run_events (run_id,event_id,observed_at) VALUES `, "", receipts)
}

// observePageTx is the set-based observeTx for distinct page events (page order)
// whose parent rows are already locked; ids are the same events in ID order.
func observePageTx(ctx context.Context, tx *sql.Tx, at time.Time, page []pageEvent, ids []any) error {
	type state struct {
		last     time.Time
		snapshot []byte
	}
	states := map[string]state{}
	err := queryRowsTx(ctx, tx, `SELECT event_id,last_seen,snapshot FROM event_history_state WHERE event_id IN (`+placeholders(len(ids))+`) ORDER BY event_id FOR UPDATE`, ids, func(rows *sql.Rows) error {
		var id string
		var s state
		err := rows.Scan(&id, &s.last, &s.snapshot)
		states[id] = s
		return err
	})
	if err != nil {
		return err
	}
	// INSERT-only permissions keep observations immutable, even on retries.
	exists := map[string]bool{}
	err = queryRowsTx(ctx, tx, `SELECT event_id FROM event_observations WHERE event_id IN (`+placeholders(len(ids))+`) AND observed_at=?`, append(slices.Clone(ids), at), func(rows *sql.Rows) error {
		var id string
		err := rows.Scan(&id)
		exists[id] = true
		return err
	})
	if err != nil {
		return err
	}
	observations, baselines, accepted := [][]any{}, [][]any{}, []pageEvent{}
	for _, e := range page {
		if exists[e.ID] {
			continue
		}
		changes, baseline := []history.Change{}, e.data
		if s, ok := states[e.ID]; ok && at.After(s.last) {
			var before models.Event
			if err := json.Unmarshal(s.snapshot, &before); err != nil {
				return err
			}
			changes = history.Changes(before, e.Event)
			if baseline, err = json.Marshal(history.KnownFacts(before, e.Event)); err != nil {
				return err
			}
		}
		encoded, err := json.Marshal(changes)
		if err != nil {
			return err
		}
		var changed any
		if len(changes) > 0 {
			changed = at
		}
		observations = append(observations, []any{e.ID, at, string(e.data), string(encoded)})
		baselines = append(baselines, []any{e.ID, at, at, changed, string(baseline)})
		accepted = append(accepted, e)
	}
	if err := insertRowsTx(ctx, tx, `INSERT INTO event_observations (event_id,observed_at,snapshot,changes) VALUES `, "", observations); err != nil {
		return err
	}
	if err := insertRowsTx(ctx, tx, `INSERT INTO event_history_state (event_id,first_seen,last_seen,last_changed,snapshot) VALUES `, ` ON DUPLICATE KEY UPDATE first_seen=LEAST(first_seen,VALUES(first_seen)),
snapshot=IF(VALUES(last_seen)>last_seen,VALUES(snapshot),snapshot),
last_changed=IF(VALUES(last_seen)>last_seen AND VALUES(last_changed) IS NOT NULL,VALUES(last_changed),last_changed),
last_seen=GREATEST(last_seen,VALUES(last_seen))`, baselines); err != nil {
		return err
	}
	return referencePageTx(ctx, tx, at, accepted)
}

// referencePageTx upserts catalog references for newly observed events. Equal
// last_seen keeps the first snapshot in page order, as sequential upserts did;
// rows are then written in stable kind:id order to avoid cross-page lock cycles.
func referencePageTx(ctx context.Context, tx *sql.Tx, at time.Time, accepted []pageEvent) error {
	type reference struct {
		id, provider, source string
		data                 []byte
	}
	refs := map[string]map[string]reference{"artist": {}, "venue": {}}
	add := func(kind, id string, src models.Source, value any) error {
		// Older local snapshots may contain only the public ID. The ID itself
		// still supplies a case-sensitive provider mapping; unnamed places with
		// no provider identity remain in the observation, not the catalog.
		provider, source, ok := strings.Cut(id, ":")
		if !ok || !providerIdentity.MatchString(provider) || !sourceIdentity.MatchString(source) {
			return nil
		}
		if src.Provider != "" && (src.Provider != provider || src.ID != source) {
			return fmt.Errorf("reference provider identity does not match")
		}
		if _, ok := refs[kind][id]; ok {
			return nil
		}
		data, err := json.Marshal(value)
		refs[kind][id] = reference{id, provider, source, data}
		return err
	}
	for _, e := range accepted {
		for _, a := range e.Artists {
			if err := add("artist", a.ID, a.Source, a); err != nil {
				return err
			}
		}
		for _, v := range e.Venues {
			if err := add("venue", v.ID, v.Source, v); err != nil {
				return err
			}
		}
	}
	for _, kind := range []string{"artist", "venue"} {
		if len(refs[kind]) == 0 {
			continue
		}
		table, mapping, column, _, err := followTables(kind)
		if err != nil {
			return err
		}
		ordered := make([]reference, 0, len(refs[kind]))
		for _, ref := range refs[kind] {
			ordered = append(ordered, ref)
		}
		slices.SortFunc(ordered, func(a, b reference) int { return strings.Compare(a.id, b.id) })
		catalog, mappings := [][]any{}, [][]any{}
		for _, ref := range ordered {
			catalog = append(catalog, []any{ref.id, string(ref.data), at, at})
			mappings = append(mappings, []any{ref.provider, ref.source, ref.id})
		}
		if err := insertRowsTx(ctx, tx, `INSERT INTO `+table+` (`+column+`,snapshot,first_seen,last_seen) VALUES `, ` ON DUPLICATE KEY UPDATE snapshot=IF(VALUES(last_seen)>last_seen,VALUES(snapshot),snapshot),first_seen=LEAST(first_seen,VALUES(first_seen)),last_seen=GREATEST(last_seen,VALUES(last_seen))`, catalog); err != nil {
			return err
		}
		if err := insertRowsTx(ctx, tx, `INSERT INTO `+mapping+` (provider,source_id,`+column+`) VALUES `, ` ON DUPLICATE KEY UPDATE `+column+`=`+column, mappings); err != nil {
			return err
		}
		for start := 0; start < len(ordered); start += 100 {
			chunk := ordered[start:min(start+100, len(ordered))]
			keys := make([]any, 0, 2*len(chunk))
			for _, ref := range chunk {
				keys = append(keys, ref.provider, ref.source)
			}
			found := 0
			err := queryRowsTx(ctx, tx, `SELECT provider,source_id,`+column+` FROM `+mapping+` WHERE (provider,source_id) IN (`+strings.TrimSuffix(strings.Repeat("(?,?),", len(chunk)), ",")+`)`, keys, func(rows *sql.Rows) error {
				var provider, source, id string
				if err := rows.Scan(&provider, &source, &id); err != nil {
					return err
				}
				if id != provider+":"+source {
					return fmt.Errorf("reference mapping cannot change identity")
				}
				found++
				return nil
			})
			if err != nil {
				return err
			}
			if found != len(chunk) {
				return fmt.Errorf("missing reference mapping")
			}
		}
	}
	return nil
}
