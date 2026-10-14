// Package radar owns explainable, private discovery. Only explicit preferences
// and artist/venue follows are signals; social activity and saves are not.
package radar

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discovery"
	"github.com/sonastea/ticketopia/internal/models"
)

var ErrUnavailable = errors.New("radar unavailable")
var ErrChanged = errors.New("radar changed; refresh from the first page")

const HorizonDays = 90
const CursorLifetime = 24 * time.Hour

type Signal struct{ Kind, ID string }
type Candidate struct {
	Event       models.Event
	DataAsOf    time.Time
	Unconfirmed bool
}
type Dataset struct {
	Preferences accounts.Preferences
	Follows     []Signal
	Candidates  []Candidate
	Meta        models.Freshness
	Refresh     bool
	// Revision covers all eligible records and explicit signals, not just Candidates.
	// Candidates contains only the requested, SQL-ranked page.
	Revision string
	HasMore  bool
	// Oldest is the oldest eligible snapshot, independently of coverage evidence.
	Oldest time.Time
}

// Read takes one consistent database snapshot of signals, revision, coverage
// and a ranked page. Meta/Oldest describe the entire eligible set. It validates
// cursor revision and anchor before loading page snapshots. at pins the date
// window and elapsed-event cutoff across pages.
type Repository interface {
	Read(context.Context, string, time.Time, time.Duration, Query) (Dataset, error)
}
type Discovery interface {
	Events(context.Context, discovery.Query) (models.EventList, error)
}
type Reason struct {
	Code     string `json:"code"`
	Text     string `json:"text"`
	TargetID string `json:"target_id,omitempty"`
}
type Item struct {
	Event   models.Event     `json:"event"`
	Reasons []Reason         `json:"reasons"`
	Meta    models.Freshness `json:"meta"`
}
type Scope struct {
	City      string `json:"city"`
	Country   string `json:"country"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}
type List struct {
	Items      []Item           `json:"items"`
	NextCursor *string          `json:"next_cursor"`
	Mode       string           `json:"mode"`
	Scope      Scope            `json:"scope"`
	Meta       models.Freshness `json:"meta"`
}
type Cursor struct {
	Version int       `json:"v"`
	Owner   string    `json:"owner"`
	Limit   int       `json:"limit"`
	At      time.Time `json:"at"`
	Digest  string    `json:"digest"`
	After   string    `json:"after"`
}
type Query struct {
	Limit  int
	Cursor *Cursor
}
type Service struct {
	repo      Repository
	discovery Discovery
	freshFor  time.Duration
	now       func() time.Time
}

func New(repo Repository, discovery Discovery, freshFor time.Duration) *Service {
	if freshFor <= 0 {
		freshFor = 6 * time.Hour
	}
	return &Service{repo, discovery, freshFor, func() time.Time { return time.Now().UTC() }}
}

// InvalidCursor reports malformed cursor input or an absent pagination anchor.
func InvalidCursor() error {
	return &accounts.ValidationError{Field: "cursor", Message: "use your radar's next cursor with the same limit, or refresh Radar"}
}
func ParseQuery(values url.Values, owner string, now time.Time) (Query, error) {
	q := Query{Limit: 20}
	for k, v := range values {
		if len(v) != 1 || (k != "limit" && k != "cursor") {
			return q, &accounts.ValidationError{Field: k, Message: "use one value for limit or cursor"}
		}
	}
	if v, ok := values["limit"]; ok {
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 1 || n > 100 {
			return q, &accounts.ValidationError{Field: "limit", Message: "use an integer from 1 to 100"}
		}
		q.Limit = n
	}
	if v, ok := values["cursor"]; ok {
		if len(v[0]) > 1024 {
			return q, InvalidCursor()
		}
		var c Cursor
		data, err := base64.RawURLEncoding.Strict().DecodeString(v[0])
		if err != nil || json.Unmarshal(data, &c) != nil || c.Version != 1 || c.Owner != owner || c.Limit != q.Limit || c.At.IsZero() || c.At.After(now.Add(time.Minute)) || now.Sub(c.At) > CursorLifetime || len(c.Digest) != 64 || c.After == "" || len(c.After) > 300 {
			return q, InvalidCursor()
		}
		if _, err := hex.DecodeString(c.Digest); err != nil {
			return q, InvalidCursor()
		}
		q.Cursor = &c
	}
	return q, nil
}
func DateScope(p accounts.Preferences, at time.Time) Scope {
	loc, err := time.LoadLocation(p.Timezone)
	if err != nil {
		loc = time.UTC
	}
	date := at.In(loc).Format(time.DateOnly)
	start, _ := time.Parse(time.DateOnly, date)
	return Scope{p.City, p.Country, date, start.AddDate(0, 0, HorizonDays-1).Format(time.DateOnly)}
}
func (s *Service) List(ctx context.Context, owner string, values url.Values) (List, error) {
	if !accounts.ValidID(owner) {
		return List{}, accounts.ErrUnauthenticated
	}
	now := s.now()
	q, err := ParseQuery(values, owner, now)
	if err != nil {
		return List{}, err
	}
	if s.repo == nil {
		return List{}, ErrUnavailable
	}
	at := now
	if q.Cursor != nil {
		at = q.Cursor.At
	}
	d, err := s.repo.Read(ctx, owner, at, s.freshFor, q)
	if err != nil {
		return List{}, readError(err)
	}
	scope := DateScope(d.Preferences, at)
	if q.Cursor == nil && d.Refresh && scope.City != "" && scope.Country != "" && s.discovery != nil {
		// Broad public collection is shared across users, not one provider call per
		// follow or per event. Failure preserves the last successful local records.
		_, _ = s.discovery.Events(ctx, discovery.Query{City: scope.City, Country: scope.Country, StartDate: scope.StartDate, EndDate: scope.EndDate, Limit: 1, Sort: "date_asc"})
		d, err = s.repo.Read(ctx, owner, at, s.freshFor, q)
		if err != nil {
			return List{}, ErrUnavailable
		}
		scope = DateScope(d.Preferences, at)
	}
	list := List{Items: []Item{}, Scope: scope, Meta: d.Meta, Mode: "personalized"}
	if scope.City == "" || scope.Country == "" {
		if q.Cursor != nil {
			return List{}, ErrChanged
		}
		list.Mode = "needs_location"
		return list, nil
	}
	if len(d.Follows) == 0 && len(d.Preferences.CategoryIDs) == 0 {
		list.Mode = "nearby"
	}
	if q.Cursor != nil && q.Cursor.Digest != d.Revision {
		return List{}, ErrChanged
	}
	if !d.Oldest.IsZero() && now.Sub(d.Oldest) >= s.freshFor {
		list.Meta.Stale = true
	}
	followed := map[string]bool{}
	for _, f := range d.Follows {
		followed[f.Kind+":"+f.ID] = true
	}
	preferred := map[string]bool{}
	for _, id := range d.Preferences.CategoryIDs {
		preferred[id] = true
	}
	for _, c := range d.Candidates {
		e := c.Event
		reasons := match(e, followed, preferred)
		if list.Mode == "nearby" {
			reasons = []Reason{{Code: "nearby", Text: "In your chosen city: " + scope.City}}
		}
		meta := models.Freshness{DataAsOf: c.DataAsOf, Stale: c.Unconfirmed || c.DataAsOf.IsZero() || now.Sub(c.DataAsOf) >= s.freshFor}
		list.Items = append(list.Items, Item{e, reasons, meta})
		if list.Meta.DataAsOf.IsZero() || (!c.DataAsOf.IsZero() && c.DataAsOf.Before(list.Meta.DataAsOf)) {
			list.Meta.DataAsOf = c.DataAsOf
		}
		list.Meta.Stale = list.Meta.Stale || meta.Stale
	}
	if d.HasMore && len(list.Items) > 0 {
		data, _ := json.Marshal(Cursor{1, owner, q.Limit, at, d.Revision, list.Items[len(list.Items)-1].Event.ID})
		next := base64.RawURLEncoding.EncodeToString(data)
		list.NextCursor = &next
	}
	return list, nil
}

func readError(err error) error {
	var validation *accounts.ValidationError
	if errors.Is(err, ErrChanged) || errors.As(err, &validation) {
		return err
	}
	return ErrUnavailable
}
func match(e models.Event, followed, preferred map[string]bool) []Reason {
	reasons := []Reason{}
	seen := map[string]bool{}
	add := func(code, id, text string) {
		key := code + ":" + id
		if !seen[key] {
			reasons = append(reasons, Reason{code, text, id})
			seen[key] = true
		}
	}
	for _, a := range e.Artists {
		if followed["artist:"+a.ID] {
			text := "Features an artist you follow"
			if strings.TrimSpace(a.Name) != "" {
				text = "You follow " + a.Name
			}
			add("followed_artist", a.ID, text)
		}
	}
	for _, v := range e.Venues {
		if followed["venue:"+v.ID] {
			text := "At a venue you follow"
			if strings.TrimSpace(v.Name) != "" {
				text = "You follow " + v.Name
			}
			add("followed_venue", v.ID, text)
		}
	}
	for _, c := range e.Classifications {
		if c.Segment != nil && preferred[c.Segment.ID] {
			text := "Matches one of your preferred categories"
			if strings.TrimSpace(c.Segment.Name) != "" {
				text = "You chose " + c.Segment.Name
			}
			add("preferred_category", c.Segment.ID, text)
		}
	}
	slices.SortFunc(reasons, func(a, b Reason) int {
		if n := strings.Compare(a.Code, b.Code); n != 0 {
			return n
		}
		return strings.Compare(a.TargetID, b.TargetID)
	})
	return reasons
}
