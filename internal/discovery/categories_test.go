package discovery

import (
	_ "embed"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

//go:embed testdata/categories.json
var categoriesFixture []byte

func TestCategoriesCachePaginationAndFallback(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if r.URL.Path != "/classifications.json" || r.URL.Query().Get("size") != "100" {
			t.Errorf("unexpected catalog request: %s", r.URL.Path)
		}
		if call > 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if r.URL.Query().Get("page") == "0" {
			_, _ = fmt.Fprint(w, strings.Replace(string(categoriesFixture), `"totalPages": 1`, `"totalPages": 2`, 1))
		} else {
			_, _ = fmt.Fprint(w, `{"page":{"number":1,"totalPages":2},"_embedded":{"classifications":[{"segment":{"id":"new","name":"New category"}},{"segment":{"id":"KZFzniwnSyZfZ7v7nE","name":"Sports"}}]}}`)
		}
	})
	now := time.Now()
	s.now = func() time.Time { return now }
	first, err := s.Categories(t.Context())
	if err != nil || len(first.Items) != 6 || first.Items[0].Name != "Arts & Theatre" || first.Items[2].Name != "Miscellaneous" {
		t.Fatalf("catalog pagination/sorting failed: %+v %v", first, err)
	}
	if first.Items[3].Genres[0].Name != "Jazz" || first.Items[3].Genres[0].Subgenres == nil || first.Items[5].Genres[0].Subgenres[0].Name != "NBA" || first.Items[1].Genres == nil {
		t.Fatalf("genre metadata missing: %+v", first)
	}
	now = now.Add(23 * time.Hour)
	_, _ = s.Categories(t.Context())
	if calls.Load() != 2 {
		t.Fatal("catalog cache expired too soon")
	}
	now = now.Add(2 * time.Hour)
	stale, err := s.Categories(t.Context())
	if err != nil || !stale.Meta.Stale || len(stale.Items) != 6 || !stale.Meta.DataAsOf.Equal(first.Meta.DataAsOf) {
		t.Fatalf("catalog fallback lost metadata: %+v %v", stale, err)
	}
	now = now.Add(7 * 24 * time.Hour)
	if _, err := s.Categories(t.Context()); err == nil {
		t.Fatal("expired catalog survived retention")
	}
}

func TestCategoriesRejectIncompleteCatalog(t *testing.T) {
	for _, body := range []string{
		`null`,
		`{"page":{"number":0,"totalPages":11}}`,
		`{"page":{"number":0,"totalPages":1},"_embedded":{"classifications":[{"type":{"id":"x"}}]}}`,
		`{"page":{"number":0,"totalPages":1},"_embedded":{"classifications":[{"segment":{"id":"../bad","name":"Bad"}}]}}`,
	} {
		t.Run(body, func(t *testing.T) {
			s := newTestService(t, func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, body) })
			if _, err := s.Categories(t.Context()); err == nil {
				t.Fatal("invalid catalog accepted")
			}
		})
	}
}

func TestCategorySearchIsolationAndNonMusicDetails(t *testing.T) {
	var calls atomic.Int32
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		category := r.URL.Query().Get("segmentId")
		if category == "KZFzniwnSyZfZ7v7nn" {
			_, _ = fmt.Fprint(w, `{"page":{"number":0,"totalElements":0,"totalPages":0}}`)
			return
		}
		if category == "" {
			category = "all"
		}
		_, _ = fmt.Fprintf(w, `{"page":{"number":0,"totalElements":1,"totalPages":1},"_embedded":{"events":[{"id":%q,"name":"Event","url":"https://www.ticketmaster.com/event/game","classifications":[{"primary":true,"segment":{"id":%q,"name":"Sports"},"genre":{"id":"basketball","name":"Basketball"}}]}]}}`, category, category)
	})
	for _, category := range []string{"all", musicSegment, "KZFzniwnSyZfZ7v7nE", "KZFzniwnSyZfZ7v7nn", "all", "KZFzniwnSyZfZ7v7nn"} {
		list, err := s.Events(t.Context(), testQuery(t, "category_id="+category))
		if err != nil {
			t.Fatal(err)
		}
		if category == "KZFzniwnSyZfZ7v7nn" {
			if len(list.Items) != 0 || list.Items == nil || list.NextCursor != nil {
				t.Fatalf("invalid empty category result: %+v", list)
			}
			continue
		}
		if len(list.Items) != 1 || list.Items[0].ID != "ticketmaster:"+category {
			t.Fatalf("category cache contamination: %+v", list)
		}
		before := calls.Load()
		detail, err := s.Event(t.Context(), list.Items[0].ID)
		if err != nil || calls.Load() != before || detail.Item.Classifications[0].Genre.Name != "Basketball" || detail.Item.Artists == nil || len(detail.Item.Artists) != 0 || detail.Item.PriceRanges != nil || detail.Item.Start.DateTime != nil || detail.Item.Source.URL == "" {
			t.Fatalf("non-music metadata/cache failed: %+v %v", detail, err)
		}
	}
	if calls.Load() != 4 {
		t.Fatalf("expected four category-specific fetches, got %d", calls.Load())
	}
}
