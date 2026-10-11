package persistence

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/reminders"
)

func TestMariaDBOnSaleReminderLifecycle(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "reminder-owner")
	other, _ := loginAccount(t, auth, "reminder-other")
	if _, err := auth.UpdatePreferences(t.Context(), owner.ID, accounts.PreferencesPatch{EmailReminders: pointer(true), NotificationsPaused: pointer(false)}); err != nil {
		t.Fatal(err)
	}
	r := p.Reminders()
	s := reminders.New(r)
	makeEvent := func(id string, sale *time.Time) models.EventDetail {
		d := savedDetail(id)
		d.Item.PublicSale.Start = sale
		d.Meta.DataAsOf = time.Now().UTC()
		if _, _, err := p.Saved().Save(t.Context(), owner.ID, d); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Set(t.Context(), owner.ID, d.Item.ID, 30); err != nil {
			t.Fatal(err)
		}
		return d
	}
	wake := func(event string) {
		execSQL(t, p.db, `UPDATE reminder_dispatch SET next_send=UTC_TIMESTAMP(6) WHERE id=1`)
		execSQL(t, p.db, `UPDATE event_reminders SET next_check=UTC_TIMESTAMP(6),lease_until=NULL WHERE event_id=?`, event)
	}
	claim := func() reminders.Claim {
		c, err := r.Claim(t.Context())
		if err != nil || c.ID == "" {
			t.Fatal("claim", c, err)
		}
		return c
	}
	prepare := func(c reminders.Claim, d *models.EventDetail) *reminders.Delivery {
		job, err := r.Prepare(t.Context(), c, d)
		if err != nil {
			t.Fatal("prepare", err)
		}
		return job
	}
	read := func(event string) reminders.Item {
		i, err := s.Get(t.Context(), owner.ID, event)
		if err != nil {
			t.Fatal(err)
		}
		return i
	}

	unknown := makeEvent("ReminderUnknown", nil)
	if prepare(claim(), &unknown) != nil || read(unknown.Item.ID).State != "pending_date" {
		t.Fatal("guessed unknown sale")
	}
	execSQL(t, p.db, `UPDATE event_reminders SET next_check='9999-01-01' WHERE event_id=?`, unknown.Item.ID)
	sale := time.Now().UTC().Add(time.Hour)
	d := makeEvent("ReminderKnown", &sale)
	if _, err := s.Set(t.Context(), other.ID, d.Item.ID, 30); err == nil {
		t.Fatal("configured another owner's save")
	}
	if _, err := s.Get(t.Context(), other.ID, d.Item.ID); !errors.Is(err, reminders.ErrNotFound) {
		t.Fatal("foreign reminder read", err)
	}
	c := claim()
	if prepare(c, &d) != nil {
		t.Fatal("sent early")
	}
	i := read(d.Item.ID)
	if i.State != "scheduled" || i.ScheduledAt == nil || !i.ScheduledAt.Equal(sale.Add(-30*time.Minute)) {
		t.Fatal(i)
	}
	if _, err := s.Set(t.Context(), owner.ID, d.Item.ID, 30); err != nil {
		t.Fatal(err)
	}
	if got := read(d.Item.ID); !got.CreatedAt.Equal(i.CreatedAt) {
		t.Fatal("PUT changed original creation")
	}
	// Source changes reconcile the same setting and suppress the old occurrence.
	moved := sale.Add(time.Hour)
	d.Item.PublicSale.Start = &moved
	d.Meta.DataAsOf = time.Now().UTC()
	wake(d.Item.ID)
	if prepare(claim(), &d) != nil {
		t.Fatal("sent on date change")
	}
	if got := read(d.Item.ID); got.SaleAt == nil || !got.SaleAt.Equal(moved) {
		t.Fatal("date not reconciled", got)
	}
	var cancelled int
	if err := p.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM reminder_jobs WHERE state='cancelled'`).Scan(&cancelled); err != nil || cancelled != 1 {
		t.Fatal("old job not cancelled", cancelled, err)
	}
	// Pause is checked against the current account, not the publication snapshot.
	wake(d.Item.ID)
	c = claim()
	if _, err := auth.UpdatePreferences(t.Context(), owner.ID, accounts.PreferencesPatch{NotificationsPaused: pointer(true)}); err != nil {
		t.Fatal(err)
	}
	if prepare(c, &d) != nil || read(d.Item.ID).State != "paused" {
		t.Fatal("ignored pause")
	}
	if _, err := auth.UpdatePreferences(t.Context(), owner.ID, accounts.PreferencesPatch{NotificationsPaused: pointer(false)}); err != nil {
		t.Fatal(err)
	}
	// Three independent pools can claim a due setting only once.
	wake(d.Item.ID)
	pools := []*Pool{p, f.open(t), f.open(t)}
	var wg sync.WaitGroup
	claims := make(chan reminders.Claim, 3)
	for _, pool := range pools {
		wg.Go(func() {
			c, err := pool.Reminders().Claim(t.Context())
			if err != nil {
				t.Error(err)
			}
			if c.ID != "" {
				claims <- c
			}
		})
	}
	wg.Wait()
	close(claims)
	count := 0
	for got := range claims {
		count++
		c = got
	}
	if count != 1 {
		t.Fatal("multiple claims", count)
	}
	// Stop invalidates a fetched claim, and unsave prevents reactivation by resave.
	if err := s.Remove(t.Context(), owner.ID, d.Item.ID); err != nil {
		t.Fatal(err)
	}
	if prepare(c, &d) != nil {
		t.Fatal("stopped claim sent")
	}
	if _, err := s.Set(t.Context(), owner.ID, d.Item.ID, 30); err != nil {
		t.Fatal(err)
	}
	if err := p.Saved().Remove(t.Context(), owner.ID, d.Item.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := p.Saved().Save(t.Context(), owner.ID, d); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(t.Context(), owner.ID, d.Item.ID); !errors.Is(err, reminders.ErrNotFound) {
		t.Fatal("resave reactivated reminder")
	}
	// Late enrollment before a known future sale is immediate, but never after sale.
	soon := time.Now().UTC().Add(10 * time.Minute)
	d = makeEvent("ReminderSend", &soon)
	job := prepare(claim(), &d)
	if job == nil {
		t.Fatal("due email not claimed")
	}
	if err := r.Finish(t.Context(), job.JobID, "retry", ""); err != nil {
		t.Fatal(err)
	}
	wake(d.Item.ID)
	job = prepare(claim(), &d)
	if job == nil {
		t.Fatal("explicit SES rejection not retried")
	}
	if err := r.Finish(t.Context(), job.JobID, "accepted", "ses-receipt"); err != nil {
		t.Fatal(err)
	}
	restarted := f.open(t).Reminders()
	if got, err := restarted.Get(t.Context(), owner.ID, d.Item.ID); err != nil || got.State != "accepted" {
		t.Fatal("receipt lost", got, err)
	}
	wake(d.Item.ID)
	if prepare(claim(), &d) != nil {
		t.Fatal("accepted occurrence resent")
	}
	if err := s.Unsubscribe(t.Context(), job.UnsubscribeToken); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(t.Context(), owner.ID, d.Item.ID); !errors.Is(err, reminders.ErrNotFound) {
		t.Fatal("unsubscribe ineffective")
	}
	if err := s.Unsubscribe(t.Context(), accounts.ID()); !errors.Is(err, reminders.ErrNotFound) {
		t.Fatal("unknown unsubscribe token accepted")
	}
	// A killed sender's in-flight attempt is uncertain, not automatically replayed.
	d = makeEvent("ReminderInterrupted", &soon)
	job = prepare(claim(), &d)
	if job == nil {
		t.Fatal("missing interrupted job")
	}
	execSQL(t, p.db, `UPDATE reminder_jobs SET attempted_at=UTC_TIMESTAMP(6)-INTERVAL 3 MINUTE WHERE job_id=?`, job.JobID)
	wake(d.Item.ID)
	if prepare(claim(), &d) != nil || read(d.Item.ID).State != "uncertain" {
		t.Fatal("replayed uncertain email")
	}
	// No stale send on provider failure; no obsolete send after a long outage.
	d = makeEvent("ReminderStale", &soon)
	if prepare(claim(), nil) != nil || read(d.Item.ID).State != "refresh_failed" {
		t.Fatal("sent without fresh data")
	}
	execSQL(t, p.db, `UPDATE event_reminders SET created_at=UTC_TIMESTAMP(6)-INTERVAL 1 HOUR WHERE event_id=?`, d.Item.ID)
	wake(d.Item.ID)
	if prepare(claim(), &d) != nil || read(d.Item.ID).State != "expired" {
		t.Fatal("late reminder delivered")
	}
}

type reminderSenderFixture struct{ calls int }

func (s *reminderSenderFixture) Send(_ context.Context, _ reminders.Delivery) (string, string) {
	s.calls++
	return "accepted", "fixture-receipt"
}

func TestMariaDBOnSaleBackgroundDeliveryAndSharedGate(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	owner, _ := loginAccount(t, auth, "reminder-worker")
	if _, err := auth.UpdatePreferences(t.Context(), owner.ID, accounts.PreferencesPatch{EmailReminders: pointer(true), NotificationsPaused: pointer(false)}); err != nil {
		t.Fatal(err)
	}
	sale := time.Now().UTC().Add(10 * time.Minute)
	for _, id := range []string{"GateOne", "GateTwo"} {
		d := savedDetail(id)
		d.Item.PublicSale.Start = &sale
		d.Meta.DataAsOf = time.Now().UTC()
		if _, _, err := p.Saved().Save(t.Context(), owner.ID, d); err != nil {
			t.Fatal(err)
		}
		if _, err := reminders.New(p.Reminders()).Set(t.Context(), owner.ID, d.Item.ID, 30); err != nil {
			t.Fatal(err)
		}
	}
	// No HTTP server or browser exists here. Retained fresh evidence suffices;
	// a nil provider would panic if the worker tried a needless refresh.
	sender := &reminderSenderFixture{}
	worker := reminders.NewWorker(p.Reminders(), nil, sender, zerolog.Nop(), time.Second)
	if err := worker.Step(t.Context()); err != nil || sender.calls != 1 {
		t.Fatal("background send", sender.calls, err)
	}
	other := f.open(t).Reminders()
	if err := reminders.NewWorker(other, nil, sender, zerolog.Nop(), time.Second).Step(t.Context()); err != nil || sender.calls != 1 {
		t.Fatal("replica bypassed dispatch gate", sender.calls, err)
	}
	var pending string
	if err := p.db.QueryRowContext(t.Context(), `SELECT job_id FROM reminder_jobs WHERE state='pending'`).Scan(&pending); err != nil {
		t.Fatal("gate discarded pending job", err)
	}
	execSQL(t, p.db, `UPDATE reminder_dispatch SET next_send=UTC_TIMESTAMP(6) WHERE id=1`)
	execSQL(t, p.db, `UPDATE event_reminders SET next_check=UTC_TIMESTAMP(6) WHERE state='scheduled'`)
	c, err := other.Claim(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	job, err := other.Prepare(t.Context(), c, &c.Detail)
	if err != nil || job == nil {
		t.Fatal("gated job lost", err)
	}
	if err := other.Finish(t.Context(), job.JobID, "retry", ""); err != nil {
		t.Fatal(err)
	}
	var seconds float64
	if err := p.db.QueryRowContext(t.Context(), `SELECT TIMESTAMPDIFF(MICROSECOND,UTC_TIMESTAMP(6),next_send)/1000000 FROM reminder_dispatch WHERE id=1`).Scan(&seconds); err != nil || seconds < 59 {
		t.Fatal("quota rejection did not cool all replicas", seconds, err)
	}
}

func TestMariaDBOnSaleReminderBrowserReview(t *testing.T) {
	script := os.Getenv("REMINDER_BROWSER_SCRIPT")
	if script == "" {
		t.Skip("set REMINDER_BROWSER_SCRIPT for optional Chromium verification")
	}
	runActivityBrowser(t, script)
}
