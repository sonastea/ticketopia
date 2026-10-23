package persistence

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
)

// activityProjectionAlternatives are diagnostic forms compared against the
// repository's projection. They are evidence only: adopt one in the repository
// only when its rows are identical and its plan does strictly less work.
var activityProjectionAlternatives = map[string]string{
	// The form before 2026-10-22: the grouped join only re-proved account
	// existence (MariaDB eliminates only outer joins), costing one eq_ref
	// lookup per live contribution.
	"group_account_join": strings.Replace(activityProjectionSQL, " AND r.withdrawn_at IS NULL\n", " AND r.withdrawn_at IS NULL\n JOIN accounts a ON a.account_id=l.account_id\n", 1),
}

// TestMariaDBActivityProjectionPlans records ANALYZE FORMAT=JSON for the live
// projection over rich windows. Plans are evidence, never optimizer assertions,
// and depend on the data shape (LIVE_ACTIVITY_PLAN_SHAPES, default both):
//   - history: two contributions per group from 16 contributors, plus 4,096
//     non-contributing accounts with 32,768 past recommendations;
//   - soak: the soak fixture's shape, one contribution per group from 8
//     contributors whose own past (no longer live) recommendations are four
//     times the window.
func TestMariaDBActivityProjectionPlans(t *testing.T) {
	sizes := capacitySizes(t, "LIVE_ACTIVITY_PLAN_GROUPS")
	shapes := []string{"history", "soak"}
	if raw := os.Getenv("LIVE_ACTIVITY_PLAN_SHAPES"); raw != "" {
		shapes = strings.Split(raw, ",")
	}
	for _, shape := range shapes {
		for _, n := range sizes {
			t.Run(fmt.Sprintf("%s/groups=%d", shape, n), func(t *testing.T) { activityProjectionPlans(t, shape, n) })
		}
	}
}

func activityProjectionPlans(t *testing.T, shape string, n int) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	auth := accounts.New(p.Accounts(), accountProviderFixture{})
	contributors := []string{}
	for i := range 16 {
		account, _ := loginAccount(t, auth, fmt.Sprintf("plan-contributor-%d", i))
		contributors = append(contributors, account.ID)
	}
	schema := f.runtime.Database
	switch shape {
	case "history":
		seedCapacityWindow(t, f, p, contributors[:8], n)
		seedCapacityWindow(t, f, p, contributors[8:], n)
		// Production holds far more accounts and past recommendations than
		// live contributions; without them every plan sees a live-only table.
		execSQL(t, f.admin, `INSERT INTO `+schema+`.accounts (account_id,email) SELECT CONCAT('plan-history-',seq),CONCAT('history-',seq,'@example.test') FROM seq_1_to_4096`)
		execSQL(t, f.admin, `INSERT INTO `+schema+`.event_recommendations (account_id,event_id,recommended_at,updated_at) SELECT CONCAT('plan-history-',a.seq),CONCAT('ticketmaster:LiveCap_',MOD(a.seq*8+k.seq,`+fmt.Sprint(n)+`)),UTC_TIMESTAMP(6)-INTERVAL 2 DAY,UTC_TIMESTAMP(6)-INTERVAL 2 DAY FROM seq_1_to_4096 a JOIN seq_0_to_7 k`)
	case "soak":
		// Publish 5n groups, then retire the oldest 4n contributions as
		// expiry cleanup would; their recommendations remain.
		seedCapacityWindow(t, f, p, contributors[:8], 5*n)
		execSQL(t, f.admin, `DELETE FROM `+schema+`.live_recommendation_activity WHERE CAST(SUBSTRING_INDEX(event_id,'_',-1) AS UNSIGNED)<`+fmt.Sprint(4*n))
	default:
		t.Fatal("LIVE_ACTIVITY_PLAN_SHAPES lists history and/or soak")
	}
	execSQL(t, f.admin, "ANALYZE TABLE "+f.runtime.Database+".live_recommendation_activity, "+f.runtime.Database+".event_recommendations, "+f.runtime.Database+".accounts, "+f.runtime.Database+".event_snapshots")
	var asOf time.Time
	if err := f.admin.QueryRowContext(t.Context(), `SELECT UTC_TIMESTAMP(6)`).Scan(&asOf); err != nil {
		t.Fatal(err)
	}
	db := p.db
	want := activityProjectionRows(t, db, activityProjectionSQL, asOf)
	if len(want) != n {
		t.Fatalf("projection returned %d groups, want %d", len(want), n)
	}
	dir := os.Getenv("LIVE_ACTIVITY_PLAN_DIR")
	if dir == "" {
		dir = t.TempDir()
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{"current": activityProjectionSQL}
	for name, query := range activityProjectionAlternatives {
		if query == activityProjectionSQL {
			t.Fatalf("%s did not rewrite the projection", name)
		}
		if got := activityProjectionRows(t, db, query, asOf); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s rows differ from the projection", name)
		}
		cases[name] = query
	}
	for name, query := range cases {
		var plan string
		times := []float64{}
		for range 6 {
			if err := db.QueryRowContext(t.Context(), "ANALYZE FORMAT=JSON "+query, asOf, asOf).Scan(&plan); err != nil {
				t.Fatal(name, err)
			}
			var parsed struct {
				QueryOptimization struct {
					Time float64 `json:"r_total_time_ms"`
				} `json:"query_optimization"`
				QueryBlock struct {
					Time float64 `json:"r_total_time_ms"`
				} `json:"query_block"`
			}
			if err := json.Unmarshal([]byte(plan), &parsed); err != nil {
				t.Fatal(err)
			}
			times = append(times, parsed.QueryBlock.Time)
		}
		// The first execution warms the plan and buffers; report the rest.
		t.Logf("projection plan shape=%s groups=%d %s r_total_time_ms=%v", shape, n, name, times[1:])
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("activity-projection-%s-%d-%s.json", shape, n, name)), []byte(plan), 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func activityProjectionRows(t *testing.T, db *sql.DB, query string, asOf time.Time) [][]any {
	t.Helper()
	rows, err := db.QueryContext(t.Context(), query, asOf, asOf)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	result := [][]any{}
	for rows.Next() {
		var id, title, city, key string
		var n int
		var latest, expiry, revision time.Time
		var actor sql.NullString
		if err := rows.Scan(&id, &n, &latest, &expiry, &actor, &title, &city, &key, &revision); err != nil {
			t.Fatal(err)
		}
		result = append(result, []any{id, n, latest, expiry, actor, title, city, key, revision})
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}
