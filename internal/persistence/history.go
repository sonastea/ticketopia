package persistence

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/models"
)

type HistoryRepository struct {
	db      *sql.DB
	timeout time.Duration
}

var _ history.Repository = (*HistoryRepository)(nil)

func (p *Pool) History() *HistoryRepository { return &HistoryRepository{p.db, p.config.QueryTimeout} }

// historyTransaction retries only known pre-commit deadlocks. A transport error
// during commit remains uncertain; durable keys/fences resolve explicit retries.
func historyTransaction(ctx context.Context, db *sql.DB, timeout time.Duration, operation string, fn func(context.Context, *sql.Tx) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for attempt := range 3 {
		tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
		if err == nil {
			err = fn(ctx, tx)
			if err == nil {
				err = tx.Commit()
				if err != nil {
					return safeError(operation, err)
				}
				return nil
			}
			_ = tx.Rollback()
		}
		if errors.Is(err, history.ErrNoTask) || errors.Is(err, history.ErrLeaseLost) {
			return err
		}
		if !retryable(err) || attempt == 2 {
			return safeError(operation, err)
		}
		timer := time.NewTimer(time.Duration(attempt+1) * 10 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return safeError(operation, ctx.Err())
		case <-timer.C:
		}
	}
	panic("unreachable")
}

// observeTx runs under the event's lock. Backdated records are retained, but do
// not replace the current baseline or generate retrospective notifications.
func observeTx(ctx context.Context, tx *sql.Tx, detail models.EventDetail, data []byte) error {
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

func (r *HistoryRepository) Schedule(ctx context.Context, tasks []history.Task) error {
	// Bounded chunks keep even the maximum horizon inside MariaDB's parameter
	// limit and the configured query deadline. Partial seeding is safe to retry.
	for start := 0; start < len(tasks); start += 100 {
		end := min(start+100, len(tasks))
		ctx, cancel := context.WithTimeout(ctx, r.timeout)
		args := []any{}
		for _, t := range tasks[start:end] {
			if t.ID != history.NewTask(t.Scope, t.Date).ID {
				cancel()
				return fmt.Errorf("invalid collection task identity")
			}
			args = append(args, t.ID, t.City, t.Country, t.Date)
		}
		_, err := r.db.ExecContext(ctx, `INSERT INTO collection_tasks (task_id,city,country,local_date) VALUES `+strings.TrimSuffix(strings.Repeat("(?,?,?,?),", end-start), ",")+` ON DUPLICATE KEY UPDATE task_id=task_id`, args...)
		cancel()
		if err != nil {
			return safeError("collection scheduling", err)
		}
	}
	return nil
}

func (r *HistoryRepository) Claim(ctx context.Context, ids []string) (history.Claim, error) {
	var claim history.Claim
	// Recover abandoned attempts even when their date/city has left the active
	// horizon. Otherwise an old receipt could remain "running" forever.
	if err := r.recoverExpired(ctx); err != nil {
		return claim, err
	}
	if len(ids) == 0 {
		return claim, history.ErrNoTask
	}
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return claim, fmt.Errorf("collection ownership unavailable")
	}
	claim.RunID = hex.EncodeToString(entropy[:])
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	err := historyTransaction(ctx, r.db, r.timeout, "collection claim", func(ctx context.Context, tx *sql.Tx) error {
		var old sql.NullString
		err := tx.QueryRowContext(ctx, `SELECT task_id,city,country,local_date,run_id FROM collection_tasks
WHERE task_id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`) AND next_refresh<=UTC_TIMESTAMP(6) AND (lease_until IS NULL OR lease_until<=UTC_TIMESTAMP(6))
ORDER BY next_refresh,local_date,task_id LIMIT 1 FOR UPDATE SKIP LOCKED`, args...).Scan(&claim.ID, &claim.City, &claim.Country, &claim.Date, &old)
		if errors.Is(err, sql.ErrNoRows) {
			return history.ErrNoTask
		}
		if err != nil {
			return err
		}
		if old.Valid {
			if _, err := tx.ExecContext(ctx, `UPDATE collection_runs SET status=IF(EXISTS(SELECT 1 FROM collection_pages WHERE run_id=?),'partial','failed'),failure='interrupted',finished_at=UTC_TIMESTAMP(6) WHERE run_id=? AND status='running'`, old.String, old.String); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO collection_runs (run_id,task_id,started_at,status) VALUES (?,?,UTC_TIMESTAMP(6),'running')`, claim.RunID, claim.ID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE collection_tasks SET run_id=?,lease_until=TIMESTAMPADD(SECOND,120,UTC_TIMESTAMP(6)),last_attempt=UTC_TIMESTAMP(6),last_failure=IF(? IS NULL,last_failure,'interrupted') WHERE task_id=?`, claim.RunID, old, claim.ID)
		return err
	})
	return claim, err
}

func (r *HistoryRepository) recoverExpired(ctx context.Context) error {
	return historyTransaction(ctx, r.db, r.timeout, "collection recovery", func(ctx context.Context, tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, `SELECT task_id,run_id FROM collection_tasks WHERE lease_until<=UTC_TIMESTAMP(6) AND run_id IS NOT NULL ORDER BY lease_until,task_id LIMIT 10 FOR UPDATE SKIP LOCKED`)
		if err != nil {
			return err
		}
		type abandoned struct{ task, run string }
		items := []abandoned{}
		for rows.Next() {
			var item abandoned
			if err := rows.Scan(&item.task, &item.run); err != nil {
				rows.Close()
				return err
			}
			items = append(items, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		for _, item := range items {
			if _, err := tx.ExecContext(ctx, `UPDATE collection_runs SET status=IF(EXISTS(SELECT 1 FROM collection_pages WHERE run_id=?),'partial','failed'),failure='interrupted',finished_at=UTC_TIMESTAMP(6) WHERE run_id=? AND status='running'`, item.run, item.run); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE collection_tasks SET run_id=NULL,lease_until=NULL,last_failure='interrupted' WHERE task_id=?`, item.task); err != nil {
				return err
			}
		}
		return nil
	})
}

func lockClaim(ctx context.Context, tx *sql.Tx, claim history.Claim) error {
	var owned string
	err := tx.QueryRowContext(ctx, `SELECT run_id FROM collection_tasks WHERE task_id=? AND run_id=? AND lease_until>UTC_TIMESTAMP(6) FOR UPDATE`, claim.ID, claim.RunID).Scan(&owned)
	if errors.Is(err, sql.ErrNoRows) {
		return history.ErrLeaseLost
	}
	return err
}

func (r *HistoryRepository) Page(ctx context.Context, claim history.Claim, page int, list models.EventList) error {
	if page < 0 || page >= 10 || list.Meta.Stale || list.Meta.DataAsOf.IsZero() || len(list.Items) > 100 || list.Total < 0 {
		return fmt.Errorf("invalid collection page")
	}
	return historyTransaction(ctx, r.db, max(r.timeout, 10*time.Second), "collection page", func(ctx context.Context, tx *sql.Tx) error {
		if err := lockClaim(ctx, tx, claim); err != nil {
			return err
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM collection_pages WHERE run_id=? AND page_number=?`, claim.RunID, page).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return nil
		}
		for _, e := range list.Items {
			detail := models.EventDetail{Item: e, Meta: list.Meta}
			args, data, err := prepareSnapshot(detail)
			if err != nil {
				return err
			}
			if err := upsertSnapshotTx(ctx, tx, detail, args, data); err != nil {
				return err
			}
			var exists int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM collection_run_events WHERE run_id=? AND event_id=?`, claim.RunID, e.ID).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				if _, err := tx.ExecContext(ctx, `INSERT INTO collection_run_events (run_id,event_id,observed_at) VALUES (?,?,?)`, claim.RunID, e.ID, list.Meta.DataAsOf.UTC().Truncate(time.Microsecond)); err != nil {
					return err
				}
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO collection_pages (run_id,page_number,collected_at,event_count,reported_total,limited) VALUES (?,?,?,?,?,?)`, claim.RunID, page, list.Meta.DataAsOf.UTC(), len(list.Items), list.Total, list.Limited); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE collection_tasks SET lease_until=TIMESTAMPADD(SECOND,120,UTC_TIMESTAMP(6)) WHERE task_id=?`, claim.ID)
		return err
	})
}

func (r *HistoryRepository) Finish(ctx context.Context, claim history.Claim, status, failure string, delay time.Duration) (history.Outcome, error) {
	if (status != "complete" && status != "limited" && status != "failed" && status != "partial") || (failure != "" && failure != "provider_unavailable" && failure != "provider_result_cap" && failure != "interrupted") || delay < time.Second || delay > 7*24*time.Hour {
		return history.Outcome{}, fmt.Errorf("invalid collection outcome")
	}
	err := historyTransaction(ctx, r.db, r.timeout, "collection completion", func(ctx context.Context, tx *sql.Tx) error {
		if err := lockClaim(ctx, tx, claim); err != nil {
			return err
		}
		if status == "complete" {
			var pages, first, last, low, high, distinct int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*),COALESCE(MIN(page_number),0),COALESCE(MAX(page_number),0),COALESCE(MIN(reported_total),0),COALESCE(MAX(reported_total),0) FROM collection_pages WHERE run_id=?`, claim.RunID).Scan(&pages, &first, &last, &low, &high); err != nil {
				return err
			}
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM collection_run_events WHERE run_id=?`, claim.RunID).Scan(&distinct); err != nil {
				return err
			}
			if pages == 0 || first != 0 || last+1 != pages || low != high || distinct != high {
				status, failure = "partial", "result_set_changed"
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE collection_runs SET status=?,failure=?,finished_at=UTC_TIMESTAMP(6) WHERE run_id=?`, status, failure, claim.RunID); err != nil {
			return err
		}
		if status == "complete" {
			if _, err := tx.ExecContext(ctx, `INSERT IGNORE INTO event_detail_tasks (event_id)
SELECT DISTINCT old.event_id FROM collection_run_events old JOIN collection_runs r ON r.run_id=old.run_id
WHERE r.task_id=? AND old.run_id<>? AND NOT EXISTS (SELECT 1 FROM collection_run_events current WHERE current.run_id=? AND current.event_id=old.event_id)`, claim.ID, claim.RunID, claim.RunID); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, `UPDATE collection_tasks SET run_id=NULL,lease_until=NULL,next_refresh=TIMESTAMPADD(MICROSECOND,?,UTC_TIMESTAMP(6)),last_success=IF(?='complete',UTC_TIMESTAMP(6),last_success),last_failure=? WHERE task_id=?`, delay.Microseconds(), status, failure, claim.ID)
		return err
	})
	return history.Outcome{Status: status, Failure: failure}, err
}

func (r *HistoryRepository) Freshness(ctx context.Context, id string) (history.Freshness, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var f history.Freshness
	var changed sql.NullTime
	err := r.db.QueryRowContext(ctx, `SELECT first_seen,last_seen,last_changed FROM event_history_state WHERE event_id=?`, id).Scan(&f.FirstSeen, &f.LastSeen, &changed)
	if err != nil {
		return f, safeError("event freshness", err)
	}
	if changed.Valid {
		f.LastChanged = &changed.Time
	}
	return f, nil
}

// Observations is a bounded keyset read, independent of cache/provider state.
func (r *HistoryRepository) Observations(ctx context.Context, id string, before time.Time, limit int) ([]history.Observation, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("observation limit must be from 1 to 100")
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query, args := `SELECT observed_at,snapshot,changes FROM event_observations WHERE event_id=?`, []any{id}
	if !before.IsZero() {
		query += ` AND observed_at<?`
		args = append(args, before.UTC())
	}
	query += ` ORDER BY observed_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, safeError("event observations", err)
	}
	defer rows.Close()
	items := []history.Observation{}
	for rows.Next() {
		var item history.Observation
		var snapshot, changes []byte
		if err := rows.Scan(&item.ObservedAt, &snapshot, &changes); err != nil {
			return nil, safeError("event observations", err)
		}
		if json.Unmarshal(snapshot, &item.Event) != nil || json.Unmarshal(changes, &item.Changes) != nil {
			return nil, fmt.Errorf("invalid event observation")
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("event observations", err)
	}
	return items, nil
}
