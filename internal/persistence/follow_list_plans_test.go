package persistence

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/follows"
)

func TestMariaDBFollowListBoundedBranches(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	owner, _ := loginAccount(t, accounts.New(p.Accounts(), accountProviderFixture{}), "follow-list-plans")
	db, err := connect(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	plans := os.Getenv("MARIADB_FOLLOW_PLANS") == "1"
	n := 120
	if plans {
		n = 5000
	}
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	for _, kind := range []string{"artist", "venue"} {
		catalog, _, column, collection, err := followTables(kind)
		if err != nil {
			t.Fatal(err)
		}
		seq := fmt.Sprintf("seq_1_to_%d", n)
		execSQL(t, db, `INSERT INTO `+catalog+` (`+column+`,snapshot,first_seen,last_seen)
 SELECT CONCAT('ticketmaster:',LPAD(seq,5,'0')),JSON_OBJECT('id',CONCAT('ticketmaster:',LPAD(seq,5,'0')),'name',REPEAT('Followed target ',64)),?,? FROM `+seq, at, at)
		execSQL(t, db, `INSERT INTO `+collection+` (account_id,`+column+`,followed_at)
 SELECT ?,`+column+`,TIMESTAMPADD(SECOND,FLOOR(RIGHT(`+column+`,5)/100),?) FROM `+catalog, owner.ID, at)
	}
	cursorAt := at.Add(time.Duration(n/100-1) * time.Second)
	id := fmt.Sprintf("ticketmaster:%05d", n-50)
	for _, tc := range []struct {
		name string
		q    follows.Query
	}{
		{"merged_first", follows.Query{Limit: 20}},
		{"merged_after_artist", follows.Query{Limit: 20, Cursor: &follows.Cursor{Before: cursorAt, Kind: "artist", ID: id}}},
		{"merged_after_venue", follows.Query{Limit: 20, Cursor: &follows.Cursor{Before: cursorAt, Kind: "venue", ID: id}}},
		{"artist_only", follows.Query{Limit: 20, Kind: "artist", Cursor: &follows.Cursor{Before: cursorAt, Kind: "artist", ID: id}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			old, oldArgs := unboundedFollowList(t, owner.ID, tc.q)
			current, args, err := followListQuery(owner.ID, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			read := func(ctx context.Context, query string, bindings []any) []string {
				t.Helper()
				out := []string{}
				err := queryRowsTx(ctx, p.db, query, bindings, func(rows *sql.Rows) error {
					var kind, snapshot string
					var asOf, followed time.Time
					if err := rows.Scan(&kind, &snapshot, &asOf, &followed); err != nil {
						return err
					}
					out = append(out, kind+" "+snapshot+" "+asOf.String()+" "+followed.String())
					return nil
				})
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
			if want, got := read(t.Context(), old, oldArgs), read(t.Context(), current, args); len(want) != 21 || !slices.Equal(want, got) {
				t.Fatalf("pages differ: old length %d, new length %d", len(want), len(got))
			}
			if !plans {
				return
			}
			for i, probe := range []struct {
				sql  string
				args []any
			}{{old, oldArgs}, {current, args}} {
				var times []float64
				var raw []byte
				for n := range 12 {
					if err := p.db.QueryRowContext(t.Context(), `ANALYZE FORMAT=JSON `+probe.sql, probe.args...).Scan(&raw); err != nil {
						t.Fatal(err)
					}
					var plan struct {
						Block struct {
							MS float64 `json:"r_total_time_ms"`
						} `json:"query_block"`
					}
					if err := json.Unmarshal(raw, &plan); err != nil {
						t.Fatal(err)
					}
					if n > 0 {
						times = append(times, plan.Block.MS)
					}
				}
				slices.Sort(times)
				t.Logf("variant %d median %.3f ms\n%s", i, times[len(times)/2], raw)
			}
		})
	}
}

func unboundedFollowList(t *testing.T, owner string, q follows.Query) (string, []any) {
	t.Helper()
	kinds := []string{"artist", "venue"}
	if q.Kind != "" {
		kinds = []string{q.Kind}
	}
	var parts []string
	var args []any
	for _, kind := range kinds {
		catalog, _, column, collection, err := followTables(kind)
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, `SELECT '`+kind+`' AS kind,c.snapshot,c.last_seen AS data_as_of,f.followed_at,f.`+column+` AS target_id FROM `+collection+` f JOIN `+catalog+` c ON c.`+column+`=f.`+column+` WHERE f.account_id=?`)
		args = append(args, owner)
	}
	qry := `SELECT kind,snapshot,data_as_of,followed_at FROM (` + strings.Join(parts, ` UNION ALL `) + `) entries`
	if c := q.Cursor; c != nil {
		qry += ` WHERE followed_at<? OR (followed_at=? AND (kind<? OR (kind=? AND target_id<?)))`
		args = append(args, c.Before.UTC(), c.Before.UTC(), c.Kind, c.Kind, c.ID)
	}
	return qry + ` ORDER BY followed_at DESC,kind DESC,target_id DESC LIMIT ?`, append(args, q.Limit+1)
}
