package discovery

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"testing"
	"time"
)

func TestQueryRejectsInvalidInputs(t *testing.T) {
	for _, raw := range []string{
		"page=-1", "limit=0", "limit=101", "limit=nope", "country=USA",
		"start_date=2026-02-30", "start_date=2026-10-02&end_date=2026-10-01",
		"start_date=2026-10-02&end_date=2028-10-02", "city=Boston&city=Chicago",
		"venue_id=../secrets", "artist_id=unscoped", "genre_id=../x",
		"cursor=bad", "sort=random", "apikey=anything",
		"category_id=../x", "category_id=music&category_id=sports", "category_id=all&genre_id=rock",
	} {
		t.Run(raw, func(t *testing.T) {
			values, _ := url.ParseQuery(raw)
			if _, err := ParseQuery(values, time.Now()); err == nil {
				t.Fatal("invalid query accepted")
			}
		})
	}
}

func TestCategoryQueryCompatibilityAndCursorIsolation(t *testing.T) {
	for _, tt := range []struct{ query, segment, genre string }{
		{"", "", ""},
		{"category_id=all", "", ""},
		{"category_id=", "", ""},
		{"category_id=" + musicSegment, musicSegment, ""},
		{"genre_id=rock", musicSegment, "rock"},
		{"category_id=KZFzniwnSyZfZ7v7nE&genre_id=basketball", "KZFzniwnSyZfZ7v7nE", "basketball"},
	} {
		t.Run(tt.query, func(t *testing.T) {
			q := testQuery(t, tt.query)
			if q.upstream().Get("segmentId") != tt.segment || q.upstream().Get("genreId") != tt.genre {
				t.Fatalf("incorrect category/genre mapping: %v", q.upstream())
			}
			values := q.Values()
			values.Set("cursor", q.nextCursor())
			next, err := ParseQuery(values, time.Now())
			if err != nil || next.Page != 1 || next.CategoryID != q.CategoryID {
				t.Fatalf("category pagination failed: %+v %v", next, err)
			}
			values.Set("category_id", "another-category")
			if _, err := ParseQuery(values, time.Now()); err == nil {
				t.Fatal("cursor accepted a changed category")
			}
		})
	}
	legacy := testQuery(t, "genre_id=rock")
	explicit := testQuery(t, "category_id="+musicSegment+"&genre_id=rock")
	if legacy.cacheKey() != explicit.cacheKey() {
		t.Fatal("legacy music link did not canonicalize to the explicit category")
	}
	q := testQuery(t, "")
	if q.cacheKey() != testQuery(t, "category_id=all").cacheKey() || q.cacheKey() == testQuery(t, "category_id="+musicSegment).cacheKey() {
		t.Fatal("all-category cache scope is incorrect")
	}
	// Old unfiltered queries were implicitly Music. Their cursors must not be
	// resumed into an all-category result set after this upgrade.
	old := q.Values()
	old.Del("category_id")
	sum := sha256.Sum256([]byte(old.Encode()))
	data, _ := json.Marshal(pageCursor{1, 1, hex.EncodeToString(sum[:])})
	old.Set("cursor", base64.RawURLEncoding.EncodeToString(data))
	if _, err := ParseQuery(old, time.Now()); err == nil {
		t.Fatal("old music cursor was accepted into the expanded search")
	}
}

func TestPaginationBindsFiltersAndStopsAtProviderLimit(t *testing.T) {
	s := newTestService(t, func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		_, _ = fmt.Fprintf(w, `{"page":{"number":%d,"size":100,"totalElements":1500,"totalPages":15},"_embedded":{"events":[{"id":"page-%d","name":"Show"}]}}`, page, page)
	})
	q := testQuery(t, "city=Boston&limit=100")
	for page := range 10 {
		list, err := s.Events(t.Context(), q)
		if err != nil {
			t.Fatal(err)
		}
		if page == 9 {
			if list.NextCursor != nil || !list.Limited {
				t.Fatal("pagination exceeded the provider cap")
			}
			break
		}
		if list.NextCursor == nil {
			t.Fatal("pagination stopped early")
		}
		values := q.Values()
		values.Set("cursor", *list.NextCursor)
		q, err = ParseQuery(values, time.Now())
		if err != nil || q.Page != page+1 {
			t.Fatalf("next cursor failed: %v", err)
		}
		values.Set("city", "Chicago")
		if _, err := ParseQuery(values, time.Now()); err == nil {
			t.Fatal("cursor accepted different filters")
		}
	}
	for _, page := range []int{-1, 0, 10, 1000000000} {
		data, _ := json.Marshal(pageCursor{1, page, q.fingerprint()})
		values := q.Values()
		values.Set("cursor", base64.RawURLEncoding.EncodeToString(data))
		if _, err := ParseQuery(values, time.Now()); err == nil {
			t.Fatalf("invalid cursor page %d accepted", page)
		}
	}
}
