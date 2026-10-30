package persistence

import (
	"reflect"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
)

// Pages send and decode each event snapshot once, yet every item must equal
// its single-row read, whether a page repeats one event or spans many. Paging
// over tied timestamps must neither skip nor repeat items.
func TestMariaDBCommunityPagesMatchSingleReads(t *testing.T) {
	p, who, shared, root, _ := discussionReadsFixture(t, 30, 1600)
	for i := 1; i <= 12; i++ {
		e := benchmarkEvent(i)
		if err := p.Discovery().Observe(t.Context(), models.EventDetail{Item: e, Meta: models.Freshness{DataAsOf: benchmarkAt}}); err != nil {
			t.Fatal(err)
		}
		at := benchmarkAt.Add(time.Duration(i%3) * time.Second)
		id := benchmarkPost(94, i)
		var removed any
		if i%5 == 0 { // Removed roots without visible replies leave the community list.
			removed = at
		}
		execSQL(t, p.db, `INSERT INTO discussion_posts (post_id,account_id,event_id,body,idempotency_key,request_hash,created_at,updated_at,removed_at) VALUES (?,?,?,'Other event',?,?,?,?,?)`, id, benchmarkOwner(i), e.ID, id, make([]byte, 32), at, at, removed)
		execSQL(t, p.db, `INSERT INTO event_recommendations (account_id,event_id,reason,recommended_at,updated_at) VALUES (?,?,'Other event',?,?)`, benchmarkOwner(i), e.ID, at, at)
	}
	snapshot := benchmarkAt.Add(time.Hour)
	posts := func(t *testing.T, event, thread string, limit int, cursor *discussions.Cursor) []discussions.Post {
		t.Helper()
		items, _, err := p.Discussions().List(t.Context(), who, event, thread, discussions.Query{Limit: limit, Snapshot: snapshot, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	for _, tc := range []struct{ name, event, thread string }{
		{"one event thread", shared, root},
		{"many events", "", ""},
	} {
		t.Run("discussions/"+tc.name, func(t *testing.T) {
			count := statementCounter(t, p)
			all := posts(t, tc.event, tc.thread, 100, nil)
			if got := count(); got != 2 {
				t.Errorf("list ran %d statements; want count and page", got)
			}
			events := map[string]int{}
			for _, item := range all {
				events[item.EventID]++
				want, err := readPost(t.Context(), p.db, who, item.ID)
				if err != nil || !reflect.DeepEqual(item, want) {
					t.Fatalf("%s differs from its single read: %v", item.ID, err)
				}
			}
			if len(all) < 20 || (tc.event == "" && len(events) < 8) {
				t.Fatalf("fixture too small: %d posts over %d events", len(all), len(events))
			}
			var paged []string
			var cursor *discussions.Cursor
			for {
				page := posts(t, tc.event, tc.thread, 7, cursor)
				for _, item := range page[:min(7, len(page))] {
					paged = append(paged, item.ID)
				}
				if len(page) <= 7 {
					break
				}
				cursor = &discussions.Cursor{At: page[6].CreatedAt, ID: page[6].ID}
			}
			if want := pageKeys(all, func(p discussions.Post) string { return p.ID }); !reflect.DeepEqual(paged, want) {
				t.Fatalf("paged order:\n%v\nwant\n%v", paged, want)
			}
		})
	}
	recs := func(t *testing.T, limit int, cursor *recommendations.Cursor) []recommendations.Item {
		t.Helper()
		items, err := p.Recommendations().List(t.Context(), "", "", recommendations.Query{Limit: limit, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	t.Run("recommendations/many events", func(t *testing.T) {
		all := recs(t, 100, nil)
		events := map[string]bool{}
		for _, item := range all {
			events[item.Event.ID] = true
			want, err := p.Recommendations().Get(t.Context(), item.Profile.ID, item.Event.ID)
			if err != nil || !reflect.DeepEqual(item, want) {
				t.Fatalf("%s/%s differs from its single read: %v", item.Profile.ID, item.Event.ID, err)
			}
		}
		if len(all) < 40 || len(events) != 13 {
			t.Fatalf("fixture too small: %d recommendations over %d events", len(all), len(events))
		}
		var paged []string
		var cursor *recommendations.Cursor
		for {
			page := recs(t, 7, cursor)
			for _, item := range page[:min(7, len(page))] {
				paged = append(paged, item.Profile.ID+"/"+item.Event.ID)
			}
			if len(page) <= 7 {
				break
			}
			cursor = &recommendations.Cursor{Before: page[6].RecommendedAt, EventID: page[6].Event.ID, AccountID: page[6].Profile.ID}
		}
		if want := pageKeys(all, func(r recommendations.Item) string { return r.Profile.ID + "/" + r.Event.ID }); !reflect.DeepEqual(paged, want) {
			t.Fatalf("paged order:\n%v\nwant\n%v", paged, want)
		}
	})
	t.Run("recommendations/one event", func(t *testing.T) {
		count := statementCounter(t, p)
		items, total, err := p.Recommendations().EventList(t.Context(), shared, recommendations.Query{Limit: 20})
		if got := count(); err != nil || got != 2 || len(items) != 21 || total != 30 {
			t.Fatalf("event list: rows=%d total=%d statements=%d err=%v", len(items), total, got, err)
		}
		for _, item := range items {
			if want, err := p.Recommendations().Get(t.Context(), item.Profile.ID, shared); err != nil || !reflect.DeepEqual(item, want) {
				t.Fatalf("%s differs from its single read: %v", item.Profile.ID, err)
			}
		}
	})
}

func pageKeys[T any](items []T, id func(T) string) []string {
	out := make([]string, len(items))
	for i, item := range items {
		out[i] = id(item)
	}
	return out
}
