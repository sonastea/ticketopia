package persistence

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/reminders"
	"github.com/sonastea/ticketopia/internal/saved"
)

type ReminderRepository struct {
	db           *sql.DB
	timeout      time.Duration
	sendInterval time.Duration
}

func (p *Pool) Reminders() *ReminderRepository {
	return &ReminderRepository{db: p.db, timeout: p.config.QueryTimeout, sendInterval: time.Second}
}

// SetSendInterval configures the shared dispatch gate before starting workers.
func (r *ReminderRepository) SetSendInterval(interval time.Duration) { r.sendInterval = interval }

var _ reminders.Repository = (*ReminderRepository)(nil)

const reminderColumns = `r.event_id,e.name,r.lead_minutes,r.state,r.sale_at,r.scheduled_at,r.created_at`

func scanReminder(row rowScanner) (reminders.Item, error) {
	var i reminders.Item
	err := row.Scan(&i.EventID, &i.Name, &i.LeadMinutes, &i.State, &i.SaleAt, &i.ScheduledAt, &i.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return i, reminders.ErrNotFound
	}
	if err != nil {
		return i, safeError("reminder read", err)
	}
	return i, nil
}
func (r *ReminderRepository) Get(ctx context.Context, owner, event string) (reminders.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	return scanReminder(r.db.QueryRowContext(ctx, `SELECT `+reminderColumns+` FROM event_reminders r JOIN events e ON e.event_id=r.event_id WHERE r.account_id=? AND r.event_id=? AND r.active=TRUE`, owner, event))
}
func (r *ReminderRepository) Set(ctx context.Context, owner, event string, lead int) (reminders.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return reminders.Item{}, safeError("reminder write", err)
	}
	defer tx.Rollback()
	var who string
	if err = tx.QueryRowContext(ctx, `SELECT account_id FROM accounts WHERE account_id=? FOR UPDATE`, owner).Scan(&who); err != nil {
		return reminders.Item{}, safeError("reminder owner", err)
	}
	if err = tx.QueryRowContext(ctx, `SELECT account_id FROM saved_events WHERE account_id=? AND event_id=? LOCK IN SHARE MODE`, owner, event).Scan(&who); errors.Is(err, sql.ErrNoRows) {
		return reminders.Item{}, saved.ErrNotFound
	} else if err != nil {
		return reminders.Item{}, safeError("reminder saved event", err)
	}
	// Identical PUT preserves schedule and in-flight ownership. Changed settings
	// invalidate pre-send claims but never reset accepted/uncertain occurrences.
	_, err = tx.ExecContext(ctx, `INSERT INTO event_reminders (reminder_id,account_id,event_id,lead_minutes) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE token=IF(active=TRUE AND lead_minutes=VALUES(lead_minutes),token,NULL),lease_until=IF(active=TRUE AND lead_minutes=VALUES(lead_minutes),lease_until,NULL),next_check=IF(active=TRUE AND lead_minutes=VALUES(lead_minutes),next_check,UTC_TIMESTAMP(6)),lead_minutes=VALUES(lead_minutes),active=TRUE`, accounts.ID(), owner, event, lead)
	if err != nil {
		return reminders.Item{}, safeError("reminder write", err)
	}
	i, err := scanReminder(tx.QueryRowContext(ctx, `SELECT `+reminderColumns+` FROM event_reminders r JOIN events e ON e.event_id=r.event_id WHERE r.account_id=? AND r.event_id=?`, owner, event))
	if err != nil {
		return i, err
	}
	if err = tx.Commit(); err != nil {
		return i, safeError("reminder commit", err)
	}
	return i, nil
}
func (r *ReminderRepository) Remove(ctx context.Context, owner, event string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError("reminder stop", err)
	}
	defer tx.Rollback()
	err = stopRemindersTx(ctx, tx, owner, event)
	if err != nil {
		return safeError("reminder stop", err)
	}
	if err = tx.Commit(); err != nil {
		return safeError("reminder stop", err)
	}
	return nil
}

func stopRemindersTx(ctx context.Context, tx *sql.Tx, owner, event string) error {
	if _, err := tx.ExecContext(ctx, `UPDATE event_reminders SET active=FALSE,state='cancelled',token=NULL,lease_until=NULL WHERE account_id=? AND event_id=?`, owner, event); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE reminder_jobs j JOIN event_reminders r ON r.reminder_id=j.reminder_id SET j.state='cancelled' WHERE r.account_id=? AND r.event_id=? AND j.state='pending'`, owner, event)
	return err
}
func (r *ReminderRepository) List(ctx context.Context, owner string, q saved.Query) ([]reminders.Item, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	query := `SELECT ` + reminderColumns + ` FROM event_reminders r JOIN events e ON e.event_id=r.event_id WHERE r.account_id=? AND r.active=TRUE`
	args := []any{owner}
	if q.Cursor != nil {
		query += ` AND (r.created_at<? OR (r.created_at=? AND r.event_id<?))`
		args = append(args, q.Cursor.Before.UTC(), q.Cursor.Before.UTC(), q.Cursor.ID)
	}
	query += ` ORDER BY r.created_at DESC,r.event_id DESC LIMIT ?`
	args = append(args, q.Limit+1)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, safeError("reminder collection", err)
	}
	defer rows.Close()
	items := []reminders.Item{}
	for rows.Next() {
		i, err := scanReminder(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, i)
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("reminder collection", err)
	}
	return items, nil
}
func (r *ReminderRepository) Claim(ctx context.Context) (reminders.Claim, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	// A process killed after entering SendEmail must not cause automatic replay.
	if _, err := r.db.ExecContext(ctx, `UPDATE reminder_jobs SET state='uncertain' WHERE state='sending' AND attempted_at<UTC_TIMESTAMP(6)-INTERVAL 2 MINUTE LIMIT 100`); err != nil {
		return reminders.Claim{}, safeError("reminder recovery", err)
	}
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return reminders.Claim{}, safeError("reminder claim", err)
	}
	defer tx.Rollback()
	var c reminders.Claim
	err = tx.QueryRowContext(ctx, `SELECT reminder_id,account_id,event_id,lead_minutes FROM event_reminders WHERE active=TRUE AND next_check<=UTC_TIMESTAMP(6) AND (lease_until IS NULL OR lease_until<=UTC_TIMESTAMP(6)) ORDER BY next_check,reminder_id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&c.ID, &c.Owner, &c.EventID, &c.LeadMinutes)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, safeError("reminder claim", err)
	}
	c.Token = accounts.ID()
	if _, err = tx.ExecContext(ctx, `UPDATE event_reminders SET token=?,lease_until=UTC_TIMESTAMP(6)+INTERVAL 2 MINUTE WHERE reminder_id=?`, c.Token, c.ID); err != nil {
		return c, safeError("reminder claim", err)
	}
	var snapshot []byte
	if err = tx.QueryRowContext(ctx, `SELECT snapshot,data_as_of FROM event_snapshots WHERE event_id=?`, c.EventID).Scan(&snapshot, &c.Detail.Meta.DataAsOf); err != nil {
		return c, safeError("reminder snapshot", err)
	}
	if json.Unmarshal(snapshot, &c.Detail.Item) != nil {
		return c, reminders.ErrUnavailable
	}
	if err = tx.Commit(); err != nil {
		return reminders.Claim{}, safeError("reminder claim", err)
	}
	if err = r.db.QueryRowContext(ctx, `SELECT notifications_paused OR NOT email_reminders FROM accounts WHERE account_id=?`, c.Owner).Scan(&c.Paused); err != nil {
		return reminders.Claim{}, safeError("reminder preferences", err)
	}
	return c, nil
}
func (r *ReminderRepository) Prepare(ctx context.Context, c reminders.Claim, fresh *models.EventDetail) (*reminders.Delivery, error) {
	ctx, cancel := context.WithTimeout(ctx, max(r.timeout, 10*time.Second))
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, safeError("reminder planning", err)
	}
	defer tx.Rollback()
	if fresh != nil {
		args, data, err := prepareSnapshot(*fresh)
		if err != nil {
			return nil, err
		}
		if err = upsertSnapshotTx(ctx, tx, *fresh, args, data); err != nil {
			return nil, safeError("reminder observation", err)
		}
	}
	a, err := scanAccount(tx.QueryRowContext(ctx, `SELECT `+accountColumns+` FROM accounts WHERE account_id=? FOR UPDATE`, c.Owner))
	if err != nil {
		return nil, err
	}
	var active bool
	var token sql.NullString
	var lead int
	var created, now time.Time
	var lease sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT active,token,lead_minutes,created_at,UTC_TIMESTAMP(6),lease_until FROM event_reminders WHERE reminder_id=? FOR UPDATE`, c.ID).Scan(&active, &token, &lead, &created, &now, &lease)
	if err != nil {
		return nil, safeError("reminder planning", err)
	}
	if !active || token.String != c.Token || !lease.Valid || !lease.Time.After(now) {
		return nil, nil
	}
	state := "refresh_failed"
	var sale, due *time.Time
	next := now.Add(time.Minute)
	var e models.Event
	var snapshot []byte
	var asOf time.Time
	if err = tx.QueryRowContext(ctx, `SELECT snapshot,data_as_of FROM event_snapshots WHERE event_id=?`, c.EventID).Scan(&snapshot, &asOf); err != nil {
		return nil, safeError("reminder snapshot", err)
	}
	if json.Unmarshal(snapshot, &e) != nil {
		return nil, reminders.ErrUnavailable
	}
	if fresh != nil && now.Sub(asOf) <= 5*time.Minute {
		state, sale, due = reminders.Schedule(e, lead, now)
		next = now.Add(time.Hour)
		if due != nil {
			v := maxTime(*due, created)
			due = &v
			next = minTime(next, maxTime(v, now.Add(time.Second)))
		}
	}
	var savedCount int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM saved_events WHERE account_id=? AND event_id=?`, c.Owner, c.EventID).Scan(&savedCount); err != nil {
		return nil, safeError("reminder saved state", err)
	}
	if savedCount == 0 {
		state = "cancelled"
		active = false
	}
	if active && (a.Preferences.NotificationsPaused || !a.Preferences.EmailReminders) {
		state = "paused"
		next = now.Add(time.Minute)
	}
	var delivery *reminders.Delivery
	// Suppress superseded pending jobs, without discarding delivery receipts.
	if _, err = tx.ExecContext(ctx, `UPDATE reminder_jobs SET state=IF(?='expired' AND sale_at=?,'expired','cancelled') WHERE reminder_id=? AND state='pending' AND (? IS NULL OR sale_at<>? OR ?<>'scheduled')`, state, sale, c.ID, sale, sale, state); err != nil {
		return nil, safeError("reminder reconciliation", err)
	}
	if state == "scheduled" {
		jobID := accounts.ID()
		_, err = tx.ExecContext(ctx, `INSERT INTO reminder_jobs (job_id,reminder_id,sale_at,scheduled_at,state) VALUES (?,?,?,?,'pending') ON DUPLICATE KEY UPDATE scheduled_at=IF(state IN ('pending','cancelled'),VALUES(scheduled_at),scheduled_at),state=IF(state='cancelled','pending',state)`, jobID, c.ID, *sale, *due)
		if err != nil {
			return nil, safeError("reminder job", err)
		}
		var jobState string
		if err = tx.QueryRowContext(ctx, `SELECT job_id,state FROM reminder_jobs WHERE reminder_id=? AND sale_at=? FOR UPDATE`, c.ID, *sale).Scan(&jobID, &jobState); err != nil {
			return nil, safeError("reminder job", err)
		}
		if jobState != "pending" {
			state = jobState
		} else if !due.After(now) {
			if now.Sub(*due) > 5*time.Minute {
				state = "expired"
				_, err = tx.ExecContext(ctx, `UPDATE reminder_jobs SET state='expired' WHERE job_id=?`, jobID)
			} else {
				var available time.Time
				if err = tx.QueryRowContext(ctx, `SELECT next_send FROM reminder_dispatch WHERE id=1 FOR UPDATE`).Scan(&available); err != nil {
					return nil, safeError("reminder dispatch gate", err)
				}
				if available.After(now) {
					next = available
				} else {
					state = "sending"
					unsubscribe := accounts.ID()
					hash := sha256.Sum256([]byte(unsubscribe))
					_, err = tx.ExecContext(ctx, `UPDATE reminder_jobs SET state='sending',attempts=attempts+1,attempted_at=?,unsubscribe_hash=? WHERE job_id=?`, now, hash[:], jobID)
					delivery = &reminders.Delivery{JobID: jobID, To: a.Email, Name: e.Name, URL: e.Source.URL, Timezone: a.Preferences.Timezone, SaleAt: *sale, UnsubscribeToken: unsubscribe}
					if err == nil {
						_, err = tx.ExecContext(ctx, `UPDATE reminder_dispatch SET next_send=? WHERE id=1`, now.Add(r.sendInterval))
					}
				}
			}
			if err != nil {
				return nil, safeError("reminder attempt", err)
			}
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE event_reminders SET active=?,state=?,sale_at=?,scheduled_at=?,next_check=?,token=NULL,lease_until=NULL WHERE reminder_id=?`, active, state, sale, due, next, c.ID)
	if err != nil {
		return nil, safeError("reminder schedule", err)
	}
	if err = tx.Commit(); err != nil {
		return nil, safeError("reminder planning commit", err)
	}
	return delivery, nil
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}
func (r *ReminderRepository) Finish(ctx context.Context, id, state, messageID string) error {
	if state != "accepted" && state != "failed" && state != "uncertain" && state != "retry" {
		return reminders.ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	if state == "retry" {
		state = "pending"
	}
	// Lock settings before jobs, matching Prepare, avoiding reversed lock order.
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError("reminder receipt", err)
	}
	defer tx.Rollback()
	var setting string
	var sale time.Time
	if err = tx.QueryRowContext(ctx, `SELECT reminder_id,sale_at FROM reminder_jobs WHERE job_id=?`, id).Scan(&setting, &sale); err != nil {
		return safeError("reminder receipt", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE event_reminders SET state=?,next_check=IF(?='pending',UTC_TIMESTAMP(6)+INTERVAL 1 MINUTE,UTC_TIMESTAMP(6)+INTERVAL 1 HOUR) WHERE reminder_id=? AND active=TRUE AND sale_at=?`, state, state, setting, sale); err != nil {
		return safeError("reminder receipt", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE reminder_jobs SET state=?,message_id=? WHERE job_id=? AND state='sending'`, state, messageID, id); err != nil {
		return safeError("reminder receipt", err)
	}
	if state == "pending" {
		if _, err = tx.ExecContext(ctx, `UPDATE reminder_dispatch SET next_send=GREATEST(next_send,UTC_TIMESTAMP(6)+INTERVAL 1 MINUTE) WHERE id=1`); err != nil {
			return safeError("reminder throttle cooldown", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return safeError("reminder receipt", err)
	}
	return nil
}
func (r *ReminderRepository) Unsubscribe(ctx context.Context, token string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	hash := sha256.Sum256([]byte(token))
	var owner, event string
	err := r.db.QueryRowContext(ctx, `SELECT r.account_id,r.event_id FROM reminder_jobs j JOIN event_reminders r ON r.reminder_id=j.reminder_id WHERE j.unsubscribe_hash=?`, hash[:]).Scan(&owner, &event)
	if errors.Is(err, sql.ErrNoRows) {
		return reminders.ErrNotFound
	}
	if err != nil {
		return safeError("reminder unsubscribe", err)
	}
	return r.Remove(ctx, owner, event)
}
