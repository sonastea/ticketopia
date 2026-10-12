package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/eventactivity"
	"github.com/sonastea/ticketopia/internal/kv"
)

type eventActivityStub struct {
	batches [][]string
	fail    bool
}

func (r *eventActivityStub) Previews(_ context.Context, ids []string) (map[string]eventactivity.Preview, error) {
	r.batches = append(r.batches, ids)
	if r.fail {
		return nil, eventactivity.ErrUnavailable
	}
	return map[string]eventactivity.Preview{ids[0]: {
		Questions: 2, Question: &eventactivity.Contribution{ID: "question", Body: "<script>A public question</script>", AuthorID: "author", AuthorName: "Public author"},
		Recommendations: 1, Recommendation: &eventactivity.Contribution{Body: "A public recommendation", AuthorID: "author", AuthorName: "Public author"},
	}}, nil
}

func TestDiscoveryActivityBatchesRowsAndPartialPagination(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "classifications") {
			_, _ = w.Write([]byte(`{"_embedded":{"classifications":[]}}`))
			return
		}
		_, _ = w.Write([]byte(`{"page":{"number":0,"size":20,"totalElements":2,"totalPages":1},"_embedded":{"events":[{"id":"One","name":"First event","info":"Useful event description"},{"id":"Two","name":"Second event"}]}}`))
	}))
	defer upstream.Close()
	cache := kv.NewMemory()
	defer cache.Close()
	a := &api{events: discovery.New(t.Context(), cache, zerolog.Nop(), discovery.Config{APIKey: "test", BaseURL: upstream.URL})}
	repo := &eventActivityStub{}
	WithEventActivity(repo)(a)
	for _, partial := range []bool{false, true} {
		r := httptest.NewRequest("GET", "/?city=Berlin", nil)
		if partial {
			r.Header.Set("HX-Request", "true")
		}
		w := httptest.NewRecorder()
		a.Routes().ServeHTTP(w, r)
		body := w.Body.String()
		for _, want := range []string{"Useful event description", "2 questions &amp; tips", "1 public recommendation", "section=discussion", "section=community"} {
			if w.Code != 200 || !strings.Contains(body, want) {
				t.Fatal("initial/partial discovery lost row detail or public activity", partial, w.Code, want)
			}
		}
		if strings.Contains(body, "A public question") || strings.Contains(body, "A public recommendation") || !reflect.DeepEqual(repo.batches[len(repo.batches)-1], []string{"ticketmaster:One", "ticketmaster:Two"}) {
			t.Fatal("row includes excerpts or activity was not one page-sized batch", repo.batches)
		}
	}
	w := httptest.NewRecorder()
	a.Routes().ServeHTTP(w, httptest.NewRequest("GET", "/api/v1/events?city=Berlin", nil))
	if len(repo.batches) != 2 || strings.Contains(w.Body.String(), "Public author") {
		t.Fatal("HTML enrichment changed the public events API")
	}
	repo.fail = true
	for _, partial := range []bool{false, true} {
		w = httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/?city=Berlin", nil)
		if partial {
			r.Header.Set("HX-Request", "true")
		}
		a.Routes().ServeHTTP(w, r)
		body := w.Body.String()
		message := strings.Index(body, "Community previews couldn&#39;t load")
		pagination := strings.Index(body, `id="pagination"`)
		if w.Code != 200 || !strings.Contains(body, "First event") || message < 0 || strings.Contains(body, "0 questions") {
			t.Fatal("optional failure blocked discovery or fabricated empty activity", partial, w.Code)
		}
		if pagination < 0 || message < pagination {
			t.Fatal("activity error sits outside the replaceable pagination region and can accumulate on later loads", partial)
		}
	}
}
