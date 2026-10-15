package persistence

import (
	"fmt"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/history"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
)

func benchmarkOwner(i int) string   { return fmt.Sprintf("%032d", i+1) }
func benchmarkPost(i, j int) string { return fmt.Sprintf("%016d%016d", i, j) }

func (f queryFixture) seedCommunity(t testing.TB) {
	t.Helper()
	for i := range 32 {
		execSQL(t, f.db, `INSERT INTO accounts (account_id,email,city,country,category_ids,created_at,updated_at) VALUES (?,?,'Berlin','DE','["music"]',?,?)`, benchmarkOwner(i), fmt.Sprintf("bench%d@example.test", i), benchmarkAt, benchmarkAt)
	}
	for i := range f.volume {
		// Half the events have no community activity; hot events have 32 authors
		// and 64 replies. A non-periodic city correlation is intentional.
		if (i/10)%2 != 0 {
			continue
		}
		e := benchmarkEvent(i)
		at := benchmarkAt.Add(-time.Duration(i%90) * time.Minute)
		n := 2
		if i%100 == 0 {
			n = 32
		}
		tx, err := f.db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		exec := func(query string, args ...any) {
			if _, err := tx.ExecContext(t.Context(), query, args...); err != nil {
				_ = tx.Rollback()
				t.Fatal(err)
			}
		}
		exec(`INSERT INTO recommendation_feed_events VALUES (?,?)`, e.ID, at)
		for j := range n {
			var withdrawn any
			if j%4 == 1 {
				withdrawn = at
			}
			exec(`INSERT INTO event_recommendations (account_id,event_id,reason,recommended_at,updated_at,withdrawn_at) VALUES (?,?,'A deterministic recommendation',?,?,?)`, benchmarkOwner(j), e.ID, at, at, withdrawn)
		}
		replies := 2
		if n == 32 {
			replies = 64
		}
		for j := 0; j <= replies; j++ {
			var root, parent, hidden, removed any
			if j > 0 {
				root, parent = benchmarkPost(i, 0), benchmarkPost(i, 0)
			}
			if (i+j)%7 == 0 {
				hidden = at
			}
			if (i+j)%11 == 0 {
				removed = at
			}
			exec(`INSERT INTO discussion_posts (post_id,account_id,event_id,root_id,parent_id,body,idempotency_key,request_hash,created_at,updated_at,hidden_at,removed_at) VALUES (?,?,?,?,?,'Fixture discussion',?,?, ?,?,?,?)`, benchmarkPost(i, j), benchmarkOwner(j%32), e.ID, root, parent, benchmarkPost(i, j), make([]byte, 32), at, at, hidden, removed)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
}

func communityBenchmarkQueries() []struct {
	name string
	q    recommendations.Query
} {
	return []struct {
		name string
		q    recommendations.Query
	}{
		{"all", recommendations.Query{Limit: 20}},
		{"city", recommendations.Query{City: "Paris", Limit: 20}},
		{"country", recommendations.Query{Country: "FR", Limit: 20}},
		{"category", recommendations.Query{CategoryID: "theatre", Limit: 20}},
		{"combined", recommendations.Query{City: "Paris", Country: "FR", CategoryID: "theatre", Limit: 20}},
	}
}

func BenchmarkMariaDBCommunity(b *testing.B) {
	for _, volume := range benchmarkVolumes(b) {
		b.Run(fmt.Sprint(volume), func(b *testing.B) {
			f := newQueryFixture(b, volume)
			for _, tc := range communityBenchmarkQueries() {
				b.Run("recommendations/"+tc.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						if _, err := f.p.Recommendations().Community(b.Context(), tc.q); err != nil {
							b.Fatal(err)
						}
					}
				})
				b.Run("discussions/"+tc.name, func(b *testing.B) {
					q := discussions.Query{City: tc.q.City, Country: tc.q.Country, CategoryID: tc.q.CategoryID, Limit: 20, Snapshot: benchmarkAt}
					b.ReportAllocs()
					for b.Loop() {
						if _, _, err := f.p.Discussions().List(b.Context(), "", "", "", q); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
			for _, thread := range []bool{false, true} {
				name, root := "event", ""
				if thread {
					name, root = "thread", benchmarkPost(0, 0)
				}
				b.Run("discussions/"+name, func(b *testing.B) {
					q := discussions.Query{Limit: 20, Snapshot: benchmarkAt}
					b.ReportAllocs()
					for b.Loop() {
						if _, _, err := f.p.Discussions().List(b.Context(), "", benchmarkEvent(0).ID, root, q); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}

func BenchmarkMariaDBWrites(b *testing.B) {
	for _, volume := range benchmarkVolumes(b) {
		b.Run(fmt.Sprint(volume), func(b *testing.B) {
			f := newQueryFixture(b, volume)
			b.Run("coverage_scope_claim", func(b *testing.B) {
				q := benchmarkDiscoverQuery()
				q.City = "Write coverage"
				b.ReportAllocs()
				for b.Loop() {
					token, err := f.p.Discovery().ClaimSearch(b.Context(), q)
					if err != nil || token == "" {
						b.Fatal("scope claim", err)
					}
					b.StopTimer()
					execSQL(b, f.db, `DELETE FROM discovery_scopes WHERE scope_id=?`, q.ScopeID())
					b.StartTimer()
				}
			})
			b.Run("discussion_insert", func(b *testing.B) {
				c := discussions.Create{Owner: benchmarkOwner(0), EventID: benchmarkEvent(0).ID, ThreadID: benchmarkPost(0, 0), Body: "Benchmark reply"}
				i := 0
				b.ReportAllocs()
				for b.Loop() {
					b.StopTimer()
					c.Key = fmt.Sprintf("write-%d", i)
					i++
					b.StartTimer()
					post, fresh, err := f.p.Discussions().Create(b.Context(), c, nil)
					if err != nil || !fresh {
						b.Fatal("insert", err)
					}
					b.StopTimer()
					execSQL(b, f.db, `DELETE FROM discussion_posts WHERE post_id=?`, post.ID)
					b.StartTimer()
				}
			})
			b.Run("collection_batch_100", func(b *testing.B) {
				r := f.p.History()
				task := history.NewTask(history.Scope{City: "Write fixture", Country: "DE"}, "2026-10-08")
				if err := r.Schedule(b.Context(), []history.Task{task}); err != nil {
					b.Fatal(err)
				}
				list := models.EventList{Total: 100, Meta: models.Freshness{DataAsOf: benchmarkAt}}
				for i := range 100 {
					list.Items = append(list.Items, benchmarkEvent(i))
				}
				i := 0
				b.ReportAllocs()
				for b.Loop() {
					b.StopTimer()
					execSQL(b, f.db, `UPDATE collection_tasks SET next_refresh='2000-01-01',lease_until=NULL WHERE task_id=?`, task.ID)
					claim, err := r.Claim(b.Context(), []string{task.ID})
					if err != nil {
						b.Fatal(err)
					}
					i++
					list.Meta.DataAsOf = benchmarkAt.Add(time.Duration(i) * time.Second)
					b.StartTimer()
					if err := r.Page(b.Context(), claim, 0, list); err != nil {
						b.Fatal(err)
					}
					b.StopTimer()
					if _, err := r.Finish(b.Context(), claim, "complete", "", time.Hour); err != nil {
						b.Fatal(err)
					}
					b.StartTimer()
				}
			})
		})
	}
}
