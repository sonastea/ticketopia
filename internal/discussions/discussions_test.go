package discussions

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
)

func TestPlainTextAndRetryKeyValidation(t *testing.T) {
	for _, body := range []string{"", " \n\t ", string([]byte{0xff}), "hidden\x00text", strings.Repeat("界", 4001)} {
		if _, err := Body(body); err == nil {
			t.Fatalf("accepted invalid body %q", body)
		}
	}
	if body, err := Body(" \nQuestion\nwith a tip\t "); err != nil || body != "Question\nwith a tip" {
		t.Fatal(body, err)
	}
	if _, err := Body(strings.Repeat("界", 4000)); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"", "short", strings.Repeat("x", 129), "valid_length_but space"} {
		if Key(key) == nil {
			t.Fatal("invalid key", key)
		}
	}
	if err := Key(accounts.ID()); err != nil {
		t.Fatal(err)
	}
}
func TestCursorAudienceFiltersAndSnapshot(t *testing.T) {
	q, err := ParseQuery(url.Values{"city": {" Berlin "}, "country": {"de"}, "limit": {"1"}}, "community:viewer", true)
	if err != nil {
		t.Fatal(err)
	}
	c := Cursor{queryScope("community:viewer", q), time.Now().UTC().Add(-time.Hour), accounts.ID(), q.Snapshot}
	encode := func(c Cursor) string { data, _ := json.Marshal(c); return base64.RawURLEncoding.EncodeToString(data) }
	values := url.Values{"city": {"berlin"}, "country": {"DE"}, "cursor": {encode(c)}}
	parsed, err := ParseQuery(values, "community:viewer", true)
	if err != nil || !parsed.Snapshot.Equal(c.Snapshot) {
		t.Fatal(parsed, err)
	}
	for _, scope := range []string{"community:other", "event:ticketmaster:show:viewer", "community:"} {
		if _, err := ParseQuery(values, scope, true); err == nil {
			t.Fatal("cursor crossed audience", scope)
		}
	}
	values.Set("city", "Chicago")
	if _, err := ParseQuery(values, "community:viewer", true); err == nil {
		t.Fatal("cursor crossed city")
	}
	for _, values := range []url.Values{{"owner": {"x"}}, {"limit": {"0"}}, {"cursor": {"x", "y"}}, {"category_id": {"a", "b"}}, {"city": {"Berlin"}}} {
		if _, err := ParseQuery(values, "event", false); err == nil {
			t.Fatal("accepted invalid query", values)
		}
	}
	c.At = c.Snapshot.Add(time.Hour)
	values = url.Values{"city": {"Berlin"}, "country": {"DE"}, "cursor": {encode(c)}}
	if _, err := ParseQuery(values, "community:viewer", true); err == nil {
		t.Fatal("position beyond snapshot")
	}
}

type retryRepository struct {
	Repository
	result Post
	err    error
	calls  int
}

func (r *retryRepository) Retry(context.Context, string, string, [32]byte) (Post, error) {
	r.calls++
	return r.result, r.err
}
func TestOfflineRetryAndThreadBoundaries(t *testing.T) {
	id, owner := accounts.ID(), accounts.ID()
	repo := &retryRepository{result: Post{ID: id, EventID: "ticketmaster:show", ThreadID: id}}
	s := New(repo, nil)
	p, created, err := s.Create(t.Context(), owner, "ticketmaster:show", "", "", "Question", accounts.ID())
	if err != nil || created || p.ID != id {
		t.Fatal("retry required provider", p, err)
	}
	repo.err = ErrConflict
	if _, _, err := s.Create(t.Context(), owner, "ticketmaster:show", "", "", "Question", accounts.ID()); !errors.Is(err, ErrConflict) {
		t.Fatal(err)
	}
	calls := repo.calls
	if _, _, err := s.Create(t.Context(), owner, "ticketmaster:show", "", accounts.ID(), "Question", accounts.ID()); err == nil || repo.calls != calls {
		t.Fatal("accepted parent on root")
	}
}

type boundaryRepository struct {
	Repository
	at   time.Time
	seen Query
}

func (r *boundaryRepository) Snapshot(context.Context) (time.Time, error) { return r.at, nil }
func (r *boundaryRepository) List(_ context.Context, _, _, _ string, q Query) ([]Post, int, error) {
	r.seen = q
	return []Post{}, 0, nil
}
func TestCreationBoundaryUsesRepositoryClock(t *testing.T) {
	r := &boundaryRepository{at: time.Now().UTC().Add(time.Hour)}
	s := New(r, nil)
	if _, err := s.List(t.Context(), "", "", "", nil); err != nil || !r.seen.Snapshot.Equal(r.at) {
		t.Fatal("used application clock", r.seen, err)
	}
	c := Cursor{queryScope("community:", Query{}), r.at, accounts.ID(), r.at.Add(time.Hour)}
	data, _ := json.Marshal(c)
	if _, err := s.List(t.Context(), "", "", "", url.Values{"cursor": {base64.RawURLEncoding.EncodeToString(data)}}); err == nil {
		t.Fatal("accepted future database boundary")
	}
}
