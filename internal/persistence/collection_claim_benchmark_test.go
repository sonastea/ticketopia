package persistence

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMariaDBCollectionClaimPlans(t *testing.T) {
	if os.Getenv("MARIADB_QUEUE_PLANS") != "1" {
		t.Skip("set MARIADB_QUEUE_PLANS=1 to compare queue plans")
	}
	f := newMaria(t)
	f.migrate(t)
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	execSQL(t, db, `INSERT INTO collection_tasks (task_id,city,country,local_date,next_refresh)
 SELECT SHA2(CAST(seq AS CHAR),256),CONCAT('city-',seq % 20),'DE',
 DATE_FORMAT(DATE_ADD('2026-01-01', INTERVAL (seq % 366) DAY),'%Y-%m-%d'),
 TIMESTAMPADD(SECOND, FLOOR(seq / 1000), UTC_TIMESTAMP(6)-INTERVAL 1 DAY)
 FROM seq_1_to_10000`)

	for _, tc := range []struct {
		active  int
		profile string
		update  string
	}{
		{90, "ready", `UPDATE collection_tasks SET lease_until=NULL`},
		{1800, "ready", `UPDATE collection_tasks SET lease_until=NULL`},
		{7320, "ready", `UPDATE collection_tasks SET lease_until=NULL`},
		{1800, "sparse", `UPDATE collection_tasks SET lease_until=IF(ORD(LEFT(task_id,1))<ORD('f'),UTC_TIMESTAMP(6)+INTERVAL 1 DAY,NULL)`},
		{1800, "idle", `UPDATE collection_tasks SET next_refresh=UTC_TIMESTAMP(6)+INTERVAL 1 DAY`},
	} {
		t.Run(fmt.Sprintf("%s_%d", tc.profile, tc.active), func(t *testing.T) {
			execSQL(t, db, tc.update)
			execSQL(t, db, `ANALYZE TABLE collection_tasks`)
			args := make([]any, tc.active)
			ids := make([]string, tc.active)
			for i := range args {
				// Interleave current tasks with older city/date rows.
				ids[i] = fmt.Sprintf("%x", sha256.Sum256(fmt.Appendf(nil, "%d", 1+i*10000/tc.active)))
				args[i] = ids[i]
			}
			encoded, err := json.Marshal(ids)
			if err != nil {
				t.Fatal(err)
			}
			var want string
			for i := range 4 {
				if i == 2 {
					execSQL(t, db, `ALTER TABLE collection_tasks ADD INDEX probe_collection_order (next_refresh,local_date,task_id)`)
				}
				hint := ""
				if i >= 2 {
					hint = " FORCE INDEX (probe_collection_order)"
				}
				in, bound := strings.TrimSuffix(strings.Repeat("?,", len(args)), ","), args
				if i%2 == 1 {
					in, bound = collectionClaimIDs, []any{string(encoded)}
				}
				q := `SELECT task_id,city,country,local_date,run_id FROM collection_tasks` + hint +
					` WHERE task_id IN (` + in + `)
 AND next_refresh<=UTC_TIMESTAMP(6) AND (lease_until IS NULL OR lease_until<=UTC_TIMESTAMP(6))
 ORDER BY next_refresh,local_date,task_id LIMIT 1 FOR UPDATE SKIP LOCKED`
				measure := func(analyze bool) (string, time.Duration) {
					t.Helper()
					start := time.Now()
					tx, err := db.BeginTx(t.Context(), &sql.TxOptions{Isolation: sql.LevelReadCommitted})
					if err != nil {
						t.Fatal(err)
					}
					defer tx.Rollback()
					var result string
					if analyze {
						err = tx.QueryRowContext(t.Context(), `ANALYZE FORMAT=JSON `+q, bound...).Scan(&result)
					} else {
						var city, country, date string
						var run sql.NullString
						err = tx.QueryRowContext(t.Context(), q, bound...).Scan(&result, &city, &country, &date, &run)
					}
					if err != nil && err != sql.ErrNoRows {
						t.Fatal(err)
					}
					return result, time.Since(start)
				}
				plan, _ := measure(true)
				t.Logf("variant %d plan: %s", i, plan)
				var times []time.Duration
				for range 21 {
					got, dt := measure(false)
					if i == 0 {
						want = got
					} else if got != want {
						t.Fatalf("candidate returned %q, current picked %q", got, want)
					}
					times = append(times, dt)
				}
				slices.Sort(times)
				t.Logf("variant %d median=%s p95=%s", i, times[10], times[19])
			}
			execSQL(t, db, `ALTER TABLE collection_tasks DROP INDEX probe_collection_order`)
		})
	}
}
