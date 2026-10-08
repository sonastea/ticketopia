// Package moderation owns private reports, review decisions, and author appeals.
// Public discussion projections never contain reports or private review notes.
package moderation

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussions"
)

var (
	ErrNotFound    = errors.New("report or moderation record not found")
	ErrForbidden   = errors.New("moderator access required")
	ErrConflict    = errors.New("review changed; reload before deciding")
	ErrUnavailable = errors.New("moderation unavailable")
)

const MaxText = 2000

// Outcome is the affected author's allowlisted decision/appeal projection.
type Outcome struct {
	ID             string    `json:"id"`
	PostID         string    `json:"post_id"`
	EventID        string    `json:"event_id"`
	EventName      string    `json:"event_name"`
	ThreadID       string    `json:"thread_id"`
	PostCreatedAt  time.Time `json:"post_created_at"`
	Action         string    `json:"action"`
	Reason         string    `json:"reason"`
	CreatedAt      time.Time `json:"created_at"`
	Appealed       bool      `json:"appealed"`
	AppealReviewed bool      `json:"appeal_reviewed"`
	CanAppeal      bool      `json:"can_appeal"`
}
type Report struct {
	ID            string      `json:"id"`
	PostID        string      `json:"post_id"`
	EventID       string      `json:"event_id"`
	EventName     string      `json:"event_name"`
	ThreadID      string      `json:"thread_id"`
	PostCreatedAt time.Time   `json:"post_created_at"`
	Reason        string      `json:"reason"`
	Context       string      `json:"context"`
	CreatedAt     time.Time   `json:"created_at"`
	Outcome       *Resolution `json:"outcome"`
}

// Reporters receive the resolution, not private author appeal metadata.
type Resolution struct {
	Action    string    `json:"action"`
	Reason    string    `json:"reason"`
	CreatedAt time.Time `json:"created_at"`
}
type ReviewReport struct {
	Report
	ReporterID   string `json:"reporter_id"`
	ReportedBody string `json:"reported_body"`
	Reviewed     bool   `json:"reviewed"`
}
type Decision struct {
	Outcome
	ModeratorID string `json:"moderator_id"`
	Notes       string `json:"notes"`
}
type Appeal struct {
	DecisionID string    `json:"decision_id"`
	Context    string    `json:"context"`
	CreatedAt  time.Time `json:"created_at"`
	Reviewed   bool      `json:"reviewed"`
}
type Case struct {
	Post                discussions.Post `json:"post"`
	AuthorID            string           `json:"author_id"`
	CurrentBody         string           `json:"current_body"`
	Version             int64            `json:"version"`
	Reports             []ReviewReport   `json:"reports"`
	Decisions           []Decision       `json:"decisions"`
	Appeals             []Appeal         `json:"appeals"`
	NextReportsCursor   *string          `json:"next_reports_cursor"`
	NextDecisionsCursor *string          `json:"next_decisions_cursor"`
	NextAppealsCursor   *string          `json:"next_appeals_cursor"`
}
type QueueItem struct {
	Post           discussions.Post `json:"post"`
	PendingReports int              `json:"pending_reports"`
	PendingAppeals int              `json:"pending_appeals"`
}
type List[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}
type Query struct {
	Limit        int
	After, State string
	At           time.Time
}
type CaseQuery struct{ Reports, Decisions, Appeals Query }
type Review struct {
	Action            string    `json:"action"`
	Reason            string    `json:"reason"`
	Notes             string    `json:"notes"`
	ExpectedVersion   *int64    `json:"expected_version"`
	ExpectedUpdatedAt time.Time `json:"expected_updated_at"`
}
type Repository interface {
	IsModerator(context.Context, string) (bool, error)
	Report(context.Context, string, string, string, string) (Report, bool, error)
	Reports(context.Context, string, Query) ([]Report, error)
	Outcomes(context.Context, string, Query) ([]Outcome, error)
	Queue(context.Context, string, Query) ([]QueueItem, error)
	Case(context.Context, string, string, CaseQuery) (Case, error)
	Decide(context.Context, string, string, Review) (Outcome, error)
	Appeal(context.Context, string, string, string) error
}
type Service struct{ repo Repository }

func New(repo Repository) *Service { return &Service{repo: repo} }
func invalid(field, message string) error {
	return &accounts.ValidationError{Field: field, Message: message}
}
func identity(id string) error {
	if !accounts.ValidID(id) {
		return accounts.ErrUnauthenticated
	}
	return nil
}
func RecordID(id string) error {
	if !accounts.ValidID(id) {
		return invalid("id", "use a valid moderation record ID")
	}
	return nil
}
func Text(field, value string, required bool) (string, error) {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxText || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) {
		return "", invalid(field, "use plain text up to 2000 characters")
	}
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n"))
	if required && value == "" {
		return "", invalid(field, "explain the concern or decision")
	}
	return value, nil
}
func (s *Service) IsModerator(ctx context.Context, who string) (bool, error) {
	if err := identity(who); err != nil {
		return false, err
	}
	return s.repo.IsModerator(ctx, who)
}
func (s *Service) RequireModerator(ctx context.Context, who string) error {
	ok, err := s.IsModerator(ctx, who)
	if err != nil {
		return err
	}
	if !ok {
		return ErrForbidden
	}
	return nil
}
func (s *Service) Report(ctx context.Context, who, post, reason, text string) (Report, bool, error) {
	if err := identity(who); err != nil {
		return Report{}, false, err
	}
	if err := discussions.PostID(post); err != nil {
		return Report{}, false, err
	}
	switch reason {
	case "spam", "abuse", "private_information", "other":
	default:
		return Report{}, false, invalid("reason", "choose spam, abuse, private_information, or other")
	}
	text, err := Text("context", text, false)
	if err != nil {
		return Report{}, false, err
	}
	return s.repo.Report(ctx, who, post, reason, text)
}

// Cursors bind the private collection, viewer and state, with a chronological
// boundary and identity tie-breaker. These are live lists, not frozen snapshots.
type cursor struct {
	Scope, ID string
	At        time.Time
}

func ParseQuery(values url.Values, scope string, queue bool) (Query, error) {
	q := Query{Limit: 20, State: "pending"}
	for k, v := range values {
		if len(v) != 1 || (k != "limit" && k != "cursor" && !(queue && k == "state")) {
			return q, invalid("query", "use one supported collection parameter")
		}
	}
	if v, ok := values["limit"]; ok {
		n, err := strconv.Atoi(v[0])
		if err != nil || n < 1 || n > 100 {
			return q, invalid("limit", "use 1–100")
		}
		q.Limit = n
	}
	if queue && values.Get("state") != "" {
		q.State = values.Get("state")
		if q.State != "pending" && q.State != "all" {
			return q, invalid("state", "choose pending or all")
		}
	}
	if v, ok := values["cursor"]; ok {
		var c cursor
		data, err := base64.RawURLEncoding.Strict().DecodeString(v[0])
		if len(v[0]) > 512 || err != nil || json.Unmarshal(data, &c) != nil || c.Scope != scope+":"+q.State || !accounts.ValidID(c.ID) || c.At.Year() < 1000 || c.At.Year() > 9999 {
			return q, invalid("cursor", "use the next cursor from this collection")
		}
		q.After = c.ID
		q.At = c.At
	}
	return q, nil
}
func page[T any](items []T, q Query, scope string, id func(T) string, at func(T) time.Time) List[T] {
	if items == nil {
		items = []T{}
	}
	result := List[T]{Items: items}
	if len(items) > q.Limit {
		result.Items = items[:q.Limit]
		data, _ := json.Marshal(cursor{scope + ":" + q.State, id(result.Items[q.Limit-1]), at(result.Items[q.Limit-1]).UTC()})
		next := base64.RawURLEncoding.EncodeToString(data)
		result.NextCursor = &next
	}
	return result
}
func (s *Service) Reports(ctx context.Context, who string, values url.Values) (List[Report], error) {
	if err := identity(who); err != nil {
		return List[Report]{}, err
	}
	scope := "reports:" + who
	q, err := ParseQuery(values, scope, false)
	if err != nil {
		return List[Report]{}, err
	}
	items, err := s.repo.Reports(ctx, who, q)
	return page(items, q, scope, func(r Report) string { return r.ID }, func(r Report) time.Time { return r.CreatedAt }), err
}
func (s *Service) Outcomes(ctx context.Context, who string, values url.Values) (List[Outcome], error) {
	if err := identity(who); err != nil {
		return List[Outcome]{}, err
	}
	scope := "outcomes:" + who
	q, err := ParseQuery(values, scope, false)
	if err != nil {
		return List[Outcome]{}, err
	}
	items, err := s.repo.Outcomes(ctx, who, q)
	return page(items, q, scope, func(r Outcome) string { return r.ID }, func(r Outcome) time.Time { return r.CreatedAt }), err
}
func (s *Service) Queue(ctx context.Context, who string, values url.Values) (List[QueueItem], error) {
	if err := s.RequireModerator(ctx, who); err != nil {
		return List[QueueItem]{}, err
	}
	scope := "queue:" + who
	q, err := ParseQuery(values, scope, true)
	if err != nil {
		return List[QueueItem]{}, err
	}
	items, err := s.repo.Queue(ctx, who, q)
	return page(items, q, scope, func(r QueueItem) string { return r.Post.ID }, func(r QueueItem) time.Time { return r.Post.CreatedAt }), err
}
func (s *Service) Case(ctx context.Context, who, post string, values url.Values) (Case, error) {
	if err := s.RequireModerator(ctx, who); err != nil {
		return Case{}, err
	}
	if err := discussions.PostID(post); err != nil {
		return Case{}, err
	}
	for key, v := range values {
		if len(v) != 1 || (key != "limit" && key != "reports_cursor" && key != "decisions_cursor" && key != "appeals_cursor") {
			return Case{}, invalid("query", "use one supported case collection parameter")
		}
	}
	queries := []Query{}
	scope := "case:" + who + ":" + post
	for _, kind := range []string{"reports", "decisions", "appeals"} {
		v := url.Values{}
		if limit, ok := values["limit"]; ok {
			v["limit"] = limit
		}
		if c, ok := values[kind+"_cursor"]; ok {
			v["cursor"] = c
		}
		q, err := ParseQuery(v, scope+":"+kind, false)
		if err != nil {
			return Case{}, err
		}
		queries = append(queries, q)
	}
	c, err := s.repo.Case(ctx, who, post, CaseQuery{queries[0], queries[1], queries[2]})
	if err != nil {
		return c, err
	}
	r := page(c.Reports, queries[0], scope+":reports", func(r ReviewReport) string { return r.ID }, func(r ReviewReport) time.Time { return r.CreatedAt })
	c.Reports, c.NextReportsCursor = r.Items, r.NextCursor
	d := page(c.Decisions, queries[1], scope+":decisions", func(d Decision) string { return d.ID }, func(d Decision) time.Time { return d.CreatedAt })
	c.Decisions, c.NextDecisionsCursor = d.Items, d.NextCursor
	ap := page(c.Appeals, queries[2], scope+":appeals", func(a Appeal) string { return a.DecisionID }, func(a Appeal) time.Time { return a.CreatedAt })
	c.Appeals, c.NextAppealsCursor = ap.Items, ap.NextCursor
	return c, nil
}
func (s *Service) Decide(ctx context.Context, who, post string, r Review) (Outcome, error) {
	if err := s.RequireModerator(ctx, who); err != nil {
		return Outcome{}, err
	}
	if err := discussions.PostID(post); err != nil {
		return Outcome{}, err
	}
	switch r.Action {
	case "keep", "hide", "restore":
	default:
		return Outcome{}, invalid("action", "choose keep, hide, or restore")
	}
	var err error
	r.Reason, err = Text("reason", r.Reason, true)
	if err != nil {
		return Outcome{}, err
	}
	r.Notes, err = Text("notes", r.Notes, false)
	if err != nil {
		return Outcome{}, err
	}
	if r.ExpectedVersion == nil || *r.ExpectedVersion < 0 || r.ExpectedUpdatedAt.IsZero() {
		return Outcome{}, invalid("review", "send the case version and contribution update time you reviewed")
	}
	return s.repo.Decide(ctx, who, post, r)
}
func (s *Service) Appeal(ctx context.Context, who, decision, text string) error {
	if err := identity(who); err != nil {
		return err
	}
	if err := RecordID(decision); err != nil {
		return err
	}
	text, err := Text("context", text, true)
	if err != nil {
		return err
	}
	return s.repo.Appeal(ctx, who, decision, text)
}
