package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/models"
)

// sequentialSnapshotTx is the per-statement snapshot writer that preceded the
// set-based one. Tests keep it as an independent reference for equivalence.
func sequentialSnapshotTx(ctx context.Context, tx *sql.Tx, detail models.EventDetail, args []any, data []byte) error {
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
		if err := sequentialProjectSearchTx(ctx, tx, detail.Item); err != nil {
			return err
		}
	}
	if !detail.Meta.Stale {
		if err := sequentialObserveTx(ctx, tx, detail, data); err != nil {
			return err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO event_snapshots (event_id,snapshot,data_as_of) VALUES (?,?,?) ON DUPLICATE KEY UPDATE snapshot=IF(VALUES(data_as_of)>data_as_of,VALUES(snapshot),snapshot), data_as_of=GREATEST(data_as_of,VALUES(data_as_of))`, detail.Item.ID, string(data), detail.Meta.DataAsOf.UTC().Truncate(time.Microsecond))
	invalidateDiscoverCounts()
	return err
}

// sequentialObserveTx runs under the event's lock. Backdated records are retained, but do
// not replace the current baseline or generate retrospective notifications.
func sequentialObserveTx(ctx context.Context, tx *sql.Tx, detail models.EventDetail, data []byte) error {
	at := detail.Meta.DataAsOf.UTC().Truncate(time.Microsecond)
	var last time.Time
	var previous []byte
	err := tx.QueryRowContext(ctx, `SELECT last_seen,snapshot FROM event_history_state WHERE event_id=? FOR UPDATE`, detail.Item.ID).Scan(&last, &previous)
	changes := []history.Change{}
	baseline := data
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if err == nil && at.After(last) {
		var before models.Event
		if err := json.Unmarshal(previous, &before); err != nil {
			return err
		}
		changes = history.Changes(before, detail.Item)
		baseline, err = json.Marshal(history.KnownFacts(before, detail.Item))
		if err != nil {
			return err
		}
	}
	encoded, err := json.Marshal(changes)
	if err != nil {
		return err
	}
	// INSERT-only permissions keep observations immutable, even on retries.
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM event_observations WHERE event_id=? AND observed_at=?`, detail.Item.ID, at).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO event_observations (event_id,observed_at,snapshot,changes) VALUES (?,?,?,?)`, detail.Item.ID, at, string(data), string(encoded)); err != nil {
		return err
	}
	var changed any
	if len(changes) > 0 {
		changed = at
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO event_history_state (event_id,first_seen,last_seen,last_changed,snapshot) VALUES (?,?,?,?,?)
ON DUPLICATE KEY UPDATE first_seen=LEAST(first_seen,VALUES(first_seen)),
snapshot=IF(VALUES(last_seen)>last_seen,VALUES(snapshot),snapshot),
last_changed=IF(VALUES(last_seen)>last_seen AND VALUES(last_changed) IS NOT NULL,VALUES(last_changed),last_changed),
last_seen=GREATEST(last_seen,VALUES(last_seen))`, detail.Item.ID, at, at, changed, string(baseline)); err != nil {
		return err
	}
	// Lock reference catalogs in stable order to avoid cross-event lock cycles.
	type reference struct {
		kind, id string
		source   models.Source
		value    any
	}
	references := []reference{}
	for _, a := range detail.Item.Artists {
		references = append(references, reference{"artist", a.ID, a.Source, a})
	}
	for _, v := range detail.Item.Venues {
		references = append(references, reference{"venue", v.ID, v.Source, v})
	}
	slices.SortFunc(references, func(a, b reference) int { return strings.Compare(a.kind+":"+a.id, b.kind+":"+b.id) })
	for _, ref := range references {
		// Older local snapshots may contain only the public ID. The ID itself
		// still supplies a case-sensitive provider mapping; unnamed places with
		// no provider identity remain in the observation, not the catalog.
		provider, source, ok := strings.Cut(ref.id, ":")
		if !ok || !providerIdentity.MatchString(provider) || !sourceIdentity.MatchString(source) {
			continue
		}
		if ref.source.Provider != "" && (ref.source.Provider != provider || ref.source.ID != source) {
			return fmt.Errorf("reference provider identity does not match")
		}
		data, err := json.Marshal(ref.value)
		if err != nil {
			return err
		}
		if err := upsertReferenceTx(ctx, tx, ref.kind, ref.id, provider, source, data, at); err != nil {
			return err
		}
	}
	return nil
}

// Projections are replaced only with the winning public snapshot, in the same
// transaction. Activity tables are deliberately absent from discovery queries.
func sequentialProjectSearchTx(ctx context.Context, tx *sql.Tx, e models.Event) error {
	for _, table := range []string{"event_search_places", "event_search_facets"} {
		if _, err := tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE event_id=?`, e.ID); err != nil {
			return err
		}
	}
	for _, p := range searchPlaces(e) {
		if len(p.City) > 120 || len(p.CountryCode) > 2 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO event_search_places (event_id,city,country) VALUES (?,?,?)`, e.ID, p.City, strings.ToUpper(p.CountryCode)); err != nil {
			return err
		}
	}
	for _, f := range searchFacets(e) {
		if f.value == "" || len(f.value) > 300 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO event_search_facets (event_id,kind,value) VALUES (?,?,?)`, e.ID, f.kind, f.value); err != nil {
			return err
		}
	}
	return nil
}
