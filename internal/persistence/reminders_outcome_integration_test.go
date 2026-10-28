package persistence

import (
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/reminders"
)

func reminderFixture(t *testing.T, lead int) (*Pool, string, models.EventDetail) {
	t.Helper()
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "reminder-outcome")
	if _, err := auth.UpdatePreferences(t.Context(), owner.ID, accounts.PreferencesPatch{EmailReminders: pointer(true), NotificationsPaused: pointer(false)}); err != nil {
		t.Fatal(err)
	}
	d := savedDetail("ReminderOutcome")
	d.Meta.DataAsOf = time.Now().UTC().Truncate(time.Microsecond)
	d.Item.PublicSale.Start = pointer(d.Meta.DataAsOf.Add(10 * time.Minute))
	if _, _, err := p.Saved().Save(t.Context(), owner.ID, d); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Reminders().Set(t.Context(), owner.ID, d.Item.ID, lead); err != nil {
		t.Fatal(err)
	}
	return p, owner.ID, d
}

func claimReminderFixture(t *testing.T, r *ReminderRepository) reminders.Claim {
	t.Helper()
	c, err := r.Claim(t.Context())
	if err != nil || c.ID == "" {
		t.Fatalf("claim: %+v, error: %v", c, err)
	}
	return c
}

// A receipt describes what already happened. A later provider outage or account
// pause must not replace it with a status claiming that delivery is withheld.
func TestMariaDBReminderTerminalReceiptSurvivesRecheck(t *testing.T) {
	for _, scenario := range []string{"refresh_failure", "paused", "sale_elapsed"} {
		t.Run(scenario, func(t *testing.T) {
			p, owner, d := reminderFixture(t, 30)
			r := p.Reminders()
			job, err := r.Prepare(t.Context(), claimReminderFixture(t, r), &d)
			if err != nil || job == nil {
				t.Fatalf("initial delivery: %+v, error: %v", job, err)
			}
			if err := r.Finish(t.Context(), job.JobID, "accepted", "ses-receipt"); err != nil {
				t.Fatal(err)
			}
			var fresh *models.EventDetail
			if scenario == "paused" {
				if _, err := accounts.New(p.Accounts(), nil).UpdatePreferences(t.Context(), owner, accounts.PreferencesPatch{NotificationsPaused: pointer(true)}); err != nil {
					t.Fatal(err)
				}
				fresh = &d
			}
			if scenario == "sale_elapsed" {
				// Advance this fixture's sole runtime connection, not the server
				// clock or another test's schema, past the normal hourly check.
				p.db.SetMaxOpenConns(1)
				later := time.Now().UTC().Add(61 * time.Minute).Truncate(time.Second)
				execSQL(t, p.db, `SET timestamp=?`, later.Unix())
				d.Meta.DataAsOf = later
				fresh = &d
			}
			execSQL(t, p.db, `UPDATE event_reminders SET next_check=UTC_TIMESTAMP(6) WHERE event_id=?`, d.Item.ID)
			job, err = r.Prepare(t.Context(), claimReminderFixture(t, r), fresh)
			if err != nil || job != nil {
				t.Fatalf("recheck delivery: %+v, error: %v", job, err)
			}
			item, err := r.Get(t.Context(), owner, d.Item.ID)
			if err != nil {
				t.Fatal(err)
			}
			var receipt string
			if err := p.db.QueryRowContext(t.Context(), `SELECT state FROM reminder_jobs WHERE reminder_id=(SELECT reminder_id FROM event_reminders WHERE account_id=? AND event_id=?)`, owner, d.Item.ID).Scan(&receipt); err != nil {
				t.Fatal(err)
			}
			if item.State != "accepted" {
				t.Fatalf("terminal receipt overwritten after %s: public state=%q, durable receipt=%q; want accepted", scenario, item.State, receipt)
			}
		})
	}
}

// A worker may resume with an actual SES acceptance after another worker has
// recovered its abandoned sending job. Public state must not claim acceptance
// while the same occurrence's receipt remains uncertain with no message ID.
func TestMariaDBReminderLateReceiptMatchesDurableOutcome(t *testing.T) {
	p, owner, d := reminderFixture(t, 30)
	r := p.Reminders()
	job, err := r.Prepare(t.Context(), claimReminderFixture(t, r), &d)
	if err != nil || job == nil {
		t.Fatalf("initial delivery: %+v, error: %v", job, err)
	}
	execSQL(t, p.db, `UPDATE reminder_jobs SET attempted_at=UTC_TIMESTAMP(6)-INTERVAL 3 MINUTE WHERE job_id=?`, job.JobID)
	execSQL(t, p.db, `UPDATE event_reminders SET next_check=UTC_TIMESTAMP(6) WHERE event_id=?`, d.Item.ID)
	if delivery, err := r.Prepare(t.Context(), claimReminderFixture(t, r), &d); err != nil || delivery != nil {
		t.Fatalf("recovery must not resend: %+v, error: %v", delivery, err)
	}
	if err := r.Finish(t.Context(), job.JobID, "accepted", "late-ses-receipt"); err != nil {
		t.Fatal(err)
	}
	item, err := r.Get(t.Context(), owner, d.Item.ID)
	if err != nil {
		t.Fatal(err)
	}
	var receiptState, messageID string
	if err := p.db.QueryRowContext(t.Context(), `SELECT state,message_id FROM reminder_jobs WHERE job_id=?`, job.JobID).Scan(&receiptState, &messageID); err != nil {
		t.Fatal(err)
	}
	if item.State != receiptState || (item.State == "accepted" && messageID != "late-ses-receipt") {
		t.Fatalf("late receipt produced contradictory outcomes: public state=%q, durable state=%q, message_id=%q", item.State, receiptState, messageID)
	}
}

// Neither changing a still-pending lead nor explicitly re-enrolling should use
// the old enrollment instant as the deadline for a newly requested lead time.
func TestMariaDBReminderChangedTimingUsesCurrentEnrollment(t *testing.T) {
	for _, scenario := range []string{"change_lead", "stop_reenable"} {
		t.Run(scenario, func(t *testing.T) {
			p, owner, d := reminderFixture(t, 1)
			r := p.Reminders()
			// The reminder was originally enrolled an hour ago with a one-minute
			// lead. Its pending target is still nine minutes in the future.
			execSQL(t, p.db, `UPDATE event_reminders SET created_at=UTC_TIMESTAMP(6)-INTERVAL 1 HOUR WHERE event_id=?`, d.Item.ID)
			job, err := r.Prepare(t.Context(), claimReminderFixture(t, r), &d)
			if err != nil || job != nil {
				t.Fatalf("initial schedule: %+v, error: %v", job, err)
			}
			if scenario == "stop_reenable" {
				if err := r.Remove(t.Context(), owner, d.Item.ID); err != nil {
					t.Fatal(err)
				}
			}
			// The sale remains ten minutes away. A newly requested 30-minute
			// lead is immediately eligible; this occurrence has never been sent.
			if _, err := r.Set(t.Context(), owner, d.Item.ID, 30); err != nil {
				t.Fatal(err)
			}
			job, err = r.Prepare(t.Context(), claimReminderFixture(t, r), &d)
			if err != nil {
				t.Fatal(err)
			}
			item, err := r.Get(t.Context(), owner, d.Item.ID)
			if err != nil {
				t.Fatal(err)
			}
			if job == nil {
				t.Fatalf("newly requested timing expired using original creation after %s: state=%q, scheduled_at=%v, sale_at=%v; want immediate delivery for future sale", scenario, item.State, item.ScheduledAt, item.SaleAt)
			}
		})
	}
}
