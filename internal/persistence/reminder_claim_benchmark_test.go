package persistence

import (
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
)

// TestMariaDBReminderClaimPlans compares candidate indexes in a disposable
// schema. The locking read runs in a rolled-back transaction; no mail is sent.
func TestMariaDBReminderClaimPlans(t *testing.T) {
	if os.Getenv("MARIADB_QUEUE_PLANS") != "1" {
		t.Skip("set MARIADB_QUEUE_PLANS=1 to compare queue plans")
	}
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	owner, _ := loginAccount(t, accounts.New(p.Accounts(), accountProviderFixture{}), "claim-plans")
	event := savedDetail("claim-plans")
	if _, _, err := p.Saved().Save(t.Context(), owner.ID, event); err != nil {
		t.Fatal(err)
	}
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	execSQL(t, db, `INSERT INTO accounts (account_id,email,created_at,updated_at)
 SELECT LPAD(seq,32,'0'),CONCAT('queue-',seq,'@example.test'),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6)
 FROM seq_1_to_10000`)
	execSQL(t, db, `INSERT INTO event_reminders (reminder_id,account_id,event_id,lead_minutes,next_check)
 SELECT LPAD(seq,32,'0'),LPAD(seq,32,'0'),?,30,TIMESTAMPADD(SECOND,seq,UTC_TIMESTAMP(6)-INTERVAL 1 DAY)
 FROM seq_1_to_10000`, event.Item.ID)
	execSQL(t, db, `ALTER TABLE event_reminders
 ADD INDEX probe_ordered_cover (active,next_check,reminder_id,lease_until,account_id,event_id,lead_minutes),
 ADD INDEX probe_lease_first (active,lease_until,next_check,reminder_id)`)

	for _, tc := range []struct {
		name string
		sql  string
		want string
	}{
		{"all_ready", `UPDATE event_reminders SET lease_until=NULL`, "1"},
		{"mixed", `UPDATE event_reminders SET lease_until=IF(reminder_id<LPAD(21,32,'0'),UTC_TIMESTAMP(6)+INTERVAL 1 DAY,NULL)`, "21"},
		{"mostly_leased", `UPDATE event_reminders SET lease_until=IF(reminder_id<LPAD(9991,32,'0'),UTC_TIMESTAMP(6)+INTERVAL 1 DAY,NULL)`, "9991"},
		{"expired_and_unleased", `UPDATE event_reminders SET lease_until=IF(reminder_id=LPAD(1,32,'0'),UTC_TIMESTAMP(6)-INTERVAL 1 DAY,NULL)`, "1"},
		{"none_ready", `UPDATE event_reminders SET lease_until=UTC_TIMESTAMP(6)+INTERVAL 1 DAY`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			execSQL(t, db, tc.sql)
			execSQL(t, db, `ANALYZE TABLE event_reminders`)
			for _, idx := range []string{"reminder_due", "probe_ordered_cover", "probe_lease_first"} {
				q := `SELECT reminder_id,account_id,event_id,lead_minutes FROM event_reminders FORCE INDEX (` + idx + `) WHERE active=TRUE AND next_check<=UTC_TIMESTAMP(6) AND (lease_until IS NULL OR lease_until<=UTC_TIMESTAMP(6)) ORDER BY next_check,reminder_id LIMIT 1 FOR UPDATE SKIP LOCKED`
				var plan []byte
				tx, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
				if err != nil {
					t.Fatal(err)
				}
				err = tx.QueryRowContext(t.Context(), `ANALYZE FORMAT=JSON `+q).Scan(&plan)
				_ = tx.Rollback()
				if err != nil || !json.Valid(plan) {
					t.Fatalf("%s: %v", idx, err)
				}
				t.Logf("%s plan: %s", idx, plan)
				results := make(chan time.Duration, 100)
				errs := make(chan error, 4)
				var workers sync.WaitGroup
				for range 4 {
					workers.Go(func() {
						for range 25 {
							st := time.Now()
							tx, e := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
							if e != nil {
								errs <- e
								return
							}
							var id, user, event string
							var lead int
							e = tx.QueryRowContext(t.Context(), q).Scan(&id, &user, &event, &lead)
							_ = tx.Rollback()
							if !errors.Is(e, sql.ErrNoRows) && e != nil {
								errs <- e
								return
							}
							results <- time.Since(st)
						}
					})
				}
				workers.Wait()
				close(errs)
				close(results)
				for e := range errs {
					t.Fatal(e)
				}
				var times []time.Duration
				for elapsed := range results {
					times = append(times, elapsed)
				}
				slices.Sort(times)
				t.Logf("%s concurrent p95=%s p99=%s", idx, times[94], times[98])
				var id, user, event string
				var lead int
				err = db.QueryRowContext(t.Context(), q).Scan(&id, &user, &event, &lead)
				want := tc.want
				if want != "" {
					for len(want) < 32 {
						want = "0" + want
					}
				}
				if want == "" && !errors.Is(err, sql.ErrNoRows) || want != "" && (err != nil || id != want) {
					t.Fatalf("index %s picked %q, want %q: %v", idx, id, want, err)
				}
			}
		})
	}

	execSQL(t, db, `INSERT INTO reminder_jobs (job_id,reminder_id,sale_at,scheduled_at,state,attempted_at)
 SELECT LPAD(seq,32,'0'),LPAD(seq,32,'0'),UTC_TIMESTAMP(6),UTC_TIMESTAMP(6),'accepted',UTC_TIMESTAMP(6)
 FROM seq_1_to_10000`)
	const recovery = `UPDATE reminder_jobs SET state='uncertain' WHERE state='sending' AND attempted_at<UTC_TIMESTAMP(6)-INTERVAL 2 MINUTE LIMIT 100`
	for _, tc := range []struct {
		name    string
		pending int
	}{
		{"idle", 0},
		{"interrupted", 10},
	} {
		t.Run("recovery_"+tc.name, func(t *testing.T) {
			if tc.pending > 0 {
				execSQL(t, db, `UPDATE reminder_jobs SET state='sending',attempted_at=UTC_TIMESTAMP(6)-INTERVAL 1 HOUR ORDER BY job_id LIMIT ?`, tc.pending)
			}
			var times []time.Duration
			for i := range 51 {
				tx, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
				if err != nil {
					t.Fatal(err)
				}
				start := time.Now()
				if i == 0 {
					var plan string
					err = tx.QueryRowContext(t.Context(), `ANALYZE FORMAT=JSON `+recovery).Scan(&plan)
					t.Logf("recovery plan: %s", plan)
				} else {
					var result sql.Result
					result, err = tx.ExecContext(t.Context(), recovery)
					if err == nil {
						if n, affectedErr := result.RowsAffected(); affectedErr != nil || n != int64(tc.pending) {
							_ = tx.Rollback()
							t.Fatalf("affected %d, want %d: %v", n, tc.pending, affectedErr)
						}
					}
					times = append(times, time.Since(start))
				}
				_ = tx.Rollback()
				if err != nil {
					t.Fatal(err)
				}
			}
			slices.Sort(times)
			t.Logf("recovery median=%s p95=%s", times[25], times[47])
		})
	}
}
