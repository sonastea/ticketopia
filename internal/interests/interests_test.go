package interests

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
)

type visibilityRepository struct {
	Repository
	item   Item
	err    error
	writes int
	detail models.EventDetail
	value  string
}

func (r *visibilityRepository) Get(context.Context, string, string) (Item, error) {
	return r.item, r.err
}
func (r *visibilityRepository) Set(_ context.Context, _ string, detail models.EventDetail, visibility string) (Item, bool, error) {
	r.writes++
	r.detail, r.value = detail, visibility
	return Item{Event: detail.Item, Visibility: visibility}, errors.Is(r.err, ErrNotFound), nil
}
func (r *visibilityRepository) Add(ctx context.Context, owner string, detail models.EventDetail, visibility string) (Item, bool, error) {
	return r.Set(ctx, owner, detail, visibility)
}

type visibilityDetails struct {
	detail models.EventDetail
	calls  int
}

func (d *visibilityDetails) Detail(context.Context, string) (models.EventDetail, error) {
	d.calls++
	return d.detail, nil
}

func TestSetVisibilityRequiresOptInAndPreservesExistingChoices(t *testing.T) {
	private, public := "private", "public"
	id := "ticketmaster:Visibility"
	at := time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name, existing, preference, want string
		explicit                         *string
		writes, fetches                  int
	}{
		{"new private default", "", private, private, nil, 1, 1},
		{"new public profile opt-in", "", public, public, nil, 1, 1},
		{"new explicit public opt-in", "", private, public, &public, 1, 1},
		{"new explicit private overrides profile", "", public, private, &private, 1, 1},
		{"profile change cannot publish private choice", private, public, private, nil, 0, 0},
		{"profile change cannot reset public choice", public, private, public, nil, 0, 0},
		{"explicit publication reuses retained event", private, private, public, &public, 1, 0},
		{"explicit privacy reuses retained event", public, public, private, &private, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &visibilityRepository{err: ErrNotFound}
			detail := models.EventDetail{Item: models.Event{ID: id, Name: "Retained show"}, Meta: models.Freshness{DataAsOf: at}}
			if tc.existing != "" {
				r.item = Item{Event: detail.Item, Meta: detail.Meta, InterestedAt: at, Visibility: tc.existing}
				r.err = nil
			}
			d := &visibilityDetails{detail: detail}
			item, created, err := New(r, d).Set(t.Context(), accounts.ID(), id, tc.explicit, tc.preference)
			if err != nil || item.Visibility != tc.want || created != (tc.existing == "") || r.writes != tc.writes || d.calls != tc.fetches {
				t.Fatalf("item=%+v created=%v writes=%d fetches=%d err=%v", item, created, r.writes, d.calls, err)
			}
			if tc.writes == 0 && !item.InterestedAt.Equal(at) {
				t.Fatal("retry changed the original interest time")
			}
			if tc.writes != 0 && (r.value != tc.want || r.detail.Item.ID != id || !r.detail.Meta.DataAsOf.Equal(at)) {
				t.Fatal("visibility change lost its event snapshot")
			}
		})
	}
}

func TestSetCannotReplaceAnUnreadableChoice(t *testing.T) {
	r := &visibilityRepository{err: ErrUnavailable}
	d := &visibilityDetails{}
	_, _, err := New(r, d).Set(t.Context(), accounts.ID(), "ticketmaster:Visibility", nil, "public")
	if !errors.Is(err, ErrUnavailable) || r.writes != 0 || d.calls != 0 {
		t.Fatal("read failure was treated as a new public choice", err)
	}
}

func TestInterestCursorValidationAndVisibility(t *testing.T) {
	scope := "own:owner"
	cursor := nextCursor(scope, time.Now().UTC(), "ticketmaster:CaseSensitive")
	q, err := parseQuery(url.Values{"limit": {"1"}, "cursor": {*cursor}}, scope, false)
	if err != nil || q.Limit != 1 || q.Cursor.ID != "ticketmaster:CaseSensitive" {
		t.Fatal("valid keyset rejected", err)
	}
	for _, values := range []url.Values{
		{"cursor": {*cursor}}, {"cursor": {"bad"}}, {"cursor": {strings.Repeat("a", 513)}},
		{"limit": {"0"}}, {"limit": {"101"}}, {"limit": {"1", "2"}}, {"visibility": {"public"}},
	} {
		if _, err := parseQuery(values, "public:owner", false); err == nil {
			t.Fatal("invalid/cross-audience query accepted", values)
		}
	}
	for _, value := range []string{"", "friends", "Public"} {
		if Visibility(value) == nil {
			t.Fatal("unsupported visibility", value)
		}
	}
}
