// Package discussions owns public, asynchronous event conversations and Helpful
// reactions. Saves, interest, and recommendations remain independent resources.
package discussions

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/models"
	"github.com/sonastea/ticketopia/internal/recommendations"
	"github.com/sonastea/ticketopia/internal/saved"
)

var (
	ErrNotFound    = errors.New("discussion post not found")
	ErrUnavailable = errors.New("discussions unavailable")
	ErrConflict    = errors.New("idempotency key already used for another contribution")
	// ErrDraftGone is an Attribution.Draft already published, discarded or expired.
	ErrDraftGone = errors.New("draft already published, discarded or expired")
	// ErrOwnQuestion is a Same question mark on the person's own post.
	ErrOwnQuestion error = &accounts.ValidationError{Field: "post_id", Message: "mark Same question on someone else's question or tip"}
)

const MaxBody = 4000

// MaxVia bounds a stored app name, matching OAuth client names and
// recommendation attribution.
const MaxVia = recommendations.MaxVia

type Post struct {
	ID         string                  `json:"id"`
	EventID    string                  `json:"event_id"`
	ThreadID   string                  `json:"thread_id"`
	ParentID   string                  `json:"parent_id,omitempty"`
	ParentName string                  `json:"parent_name,omitempty"`
	Profile    *accounts.PublicProfile `json:"profile,omitempty"`
	// Via names the connected app whose draft the person published as this
	// post; empty for a person's own writing. Agents never post directly.
	Via string `json:"via,omitempty"`
	// ViaHost is that app's verified metadata host, empty for API-token drafts.
	ViaHost      string    `json:"via_host,omitempty"`
	Body         string    `json:"body"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	Removed      bool      `json:"removed"`
	Hidden       bool      `json:"hidden"`
	ReplyCount   int       `json:"reply_count"`
	HelpfulCount int       `json:"helpful_count"`
	Helpful      bool      `json:"helpful"`
	// SameQuestionCount is how many people marked this question or tip as
	// theirs too; SameQuestion is the viewer's own mark. Both are withheld,
	// like Helpful, for removed and hidden posts.
	SameQuestionCount int  `json:"same_question_count"`
	SameQuestion      bool `json:"same_question"`
	// FirstPost marks a person's first visible question or tip while they
	// have fewer than three visible contributions, so others can welcome them.
	// It is derived from post history, never stored.
	FirstPost bool             `json:"first_post"`
	Event     models.Event     `json:"event"`
	Meta      models.Freshness `json:"meta"`
}
type List struct {
	Items      []Post  `json:"items"`
	NextCursor *string `json:"next_cursor"`
	Count      int     `json:"count"`
	// AsOf is the database creation boundary this page was read at; live
	// reply checks count replies created after it.
	AsOf time.Time `json:"-"`
}

// NewReplies is what a participant's open conversation may not show yet:
// other people's visible replies, oldest first. Replies lists those created
// after since, which the page can't hold. Recent lists those created in the
// overlap just before since: a reply stamped before the page's read whose
// transaction committed after it. The page may hold those, so the browser
// drops the ones it shows by ID.
type NewReplies struct {
	Replies []NewReply `json:"replies"`
	// More is true when more than MaxNewReplies were created after since.
	More   bool       `json:"more"`
	Recent []NewReply `json:"recent"`
	// AsOf is the database time of this check.
	AsOf time.Time `json:"as_of"`
}

// NewReply identifies one reply; the conversation refresh shows its text.
type NewReply struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
}

const (
	// LiveReplyWindow bounds how far back a live check may look; an older
	// page should reload instead.
	LiveReplyWindow = 24 * time.Hour
	// LiveReplyOverlap is the least a check looks before since. The
	// repository widens it to its query timeout, which bounds every reply's
	// write transaction, so a reply can't commit too late to be found.
	LiveReplyOverlap = 5 * time.Second
	// MaxNewReplies bounds one check's answer.
	MaxNewReplies = 50
)

type Cursor struct {
	Scope    string    `json:"scope"`
	At       time.Time `json:"at"`
	ID       string    `json:"id"`
	Snapshot time.Time `json:"snapshot"`
}
type Query struct {
	Limit                     int
	City, Country, CategoryID string
	Cursor                    *Cursor
	Snapshot                  time.Time
	// Open keeps visible roots with no visible reply on upcoming events.
	Open bool
	// Connected limits roots to events this account saved, marked
	// Interested, or whose artist or venue it follows.
	Connected string
}
type Create struct {
	Owner, EventID, ThreadID, ParentID, Body, Key string
	Hash                                          [32]byte
	Attribution
}

// Attribution is the connected app whose draft the person publishes on the
// web, and that draft, consumed in the same transaction as the post. It is
// empty for a person's own writing.
type Attribution struct {
	// Via is the app's self-declared name, ViaHost its verified metadata host
	// (empty for API-token drafts).
	Via, ViaHost string
	// Draft is the person's agent draft deleted with the publication, or
	// ErrDraftGone if it no longer exists, so a draft publishes at most once.
	Draft string
}
type Repository interface {
	Snapshot(context.Context) (time.Time, error)
	Retry(context.Context, string, string, [32]byte) (Post, error)
	Create(context.Context, Create, *models.EventDetail) (Post, bool, error)
	Get(context.Context, string, string) (Post, error)
	List(context.Context, string, string, string, Query) ([]Post, int, error)
	Edit(context.Context, string, string, string) (Post, error)
	Remove(context.Context, string, string) error
	React(context.Context, string, string, bool) (Post, error)
	SameQuestion(context.Context, string, string, bool) (Post, error)
	NewReplies(ctx context.Context, viewer, event, thread string, since time.Time) (NewReplies, error)
}
type Details interface {
	Detail(context.Context, string) (models.EventDetail, error)
}
type Service struct {
	repo    Repository
	details Details
}

func New(repo Repository, details Details) *Service { return &Service{repo, details} }
func invalid(field, message string) error {
	return &accounts.ValidationError{Field: field, Message: message}
}
func Body(value string) (string, error) {
	if !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxBody || strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) {
		return "", invalid("body", "use plain text from 1 to 4000 characters")
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", invalid("body", "write a question, tip, or reply")
	}
	return value, nil
}

var keyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,128}$`)

func Key(key string) error {
	if !keyPattern.MatchString(key) {
		return invalid("idempotency_key", "use a unique 16–128 character key containing letters, digits, hyphens, or underscores")
	}
	return nil
}
func PostID(id string) error {
	if !accounts.ValidID(id) {
		return invalid("post_id", "use a valid discussion post ID")
	}
	return nil
}
func owner(id string) error {
	if !accounts.ValidID(id) {
		return accounts.ErrUnauthenticated
	}
	return nil
}
func (s *Service) Create(ctx context.Context, who, event, thread, parent, body, key string) (Post, bool, error) {
	return s.Publish(ctx, who, event, thread, parent, body, key, Attribution{})
}

// Publish is Create for an agent's draft the person reviewed and chose to
// publish: the post carries the app's attribution and consumes the draft.
func (s *Service) Publish(ctx context.Context, who, event, thread, parent, body, key string, a Attribution) (Post, bool, error) {
	if utf8.RuneCountInString(a.Via) > MaxVia || !utf8.ValidString(a.Via) || len(a.ViaHost) > 253 || (a.Via == "" && (a.ViaHost != "" || a.Draft != "")) {
		return Post{}, false, invalid("via", "use an app name up to 80 characters")
	}
	if err := owner(who); err != nil {
		return Post{}, false, err
	}
	if err := saved.ValidateID(event); err != nil {
		return Post{}, false, err
	}
	if thread != "" {
		if err := PostID(thread); err != nil {
			return Post{}, false, err
		}
	}
	if parent != "" {
		if thread == "" {
			return Post{}, false, invalid("parent_id", "replies require a root thread")
		}
		if err := PostID(parent); err != nil {
			return Post{}, false, err
		}
	}
	if err := Key(key); err != nil {
		return Post{}, false, err
	}
	body, err := Body(body)
	if err != nil {
		return Post{}, false, err
	}
	data, _ := json.Marshal([]string{event, thread, parent, body})
	hash := sha256.Sum256(data)
	post, err := s.repo.Retry(ctx, who, key, hash)
	if err == nil {
		return post, false, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return Post{}, false, err
	}
	var detail *models.EventDetail
	if thread == "" {
		if s.details == nil {
			return Post{}, false, ErrUnavailable
		}
		d, err := s.details.Detail(ctx, event)
		if err != nil {
			return Post{}, false, err
		}
		if d.Item.ID != event {
			return Post{}, false, ErrUnavailable
		}
		detail = &d
	}
	return s.repo.Create(ctx, Create{Owner: who, EventID: event, ThreadID: thread, ParentID: parent, Body: body, Key: key, Hash: hash, Attribution: a}, detail)
}
func (s *Service) Thread(ctx context.Context, viewer, event, id string) (Post, error) {
	if err := saved.ValidateID(event); err != nil {
		return Post{}, err
	}
	if err := PostID(id); err != nil {
		return Post{}, err
	}
	p, err := s.repo.Get(ctx, viewer, id)
	if err == nil && (p.EventID != event || p.ThreadID != p.ID) {
		return Post{}, ErrNotFound
	}
	return p, err
}
func (s *Service) Post(ctx context.Context, viewer, id string) (Post, error) {
	if err := PostID(id); err != nil {
		return Post{}, err
	}
	return s.repo.Get(ctx, viewer, id)
}
func (s *Service) Edit(ctx context.Context, who, id, body string) (Post, error) {
	if err := owner(who); err != nil {
		return Post{}, err
	}
	if err := PostID(id); err != nil {
		return Post{}, err
	}
	body, err := Body(body)
	if err != nil {
		return Post{}, err
	}
	return s.repo.Edit(ctx, who, id, body)
}
func (s *Service) Parent(ctx context.Context, viewer, event, thread, id string) (Post, error) {
	if err := saved.ValidateID(event); err != nil {
		return Post{}, err
	}
	if err := PostID(thread); err != nil {
		return Post{}, err
	}
	if err := PostID(id); err != nil {
		return Post{}, err
	}
	p, err := s.repo.Get(ctx, viewer, id)
	if err == nil && (p.EventID != event || p.ThreadID != thread) {
		return Post{}, ErrNotFound
	}
	return p, err
}
func (s *Service) Remove(ctx context.Context, who, id string) error {
	if err := owner(who); err != nil {
		return err
	}
	if err := PostID(id); err != nil {
		return err
	}
	return s.repo.Remove(ctx, who, id)
}
func (s *Service) React(ctx context.Context, who, id string, set bool) (Post, error) {
	if err := owner(who); err != nil {
		return Post{}, err
	}
	if err := PostID(id); err != nil {
		return Post{}, err
	}
	return s.repo.React(ctx, who, id, set)
}

// SameQuestion sets or clears the person's "Same question" mark on someone
// else's visible question or tip.
func (s *Service) SameQuestion(ctx context.Context, who, id string, set bool) (Post, error) {
	if err := owner(who); err != nil {
		return Post{}, err
	}
	if err := PostID(id); err != nil {
		return Post{}, err
	}
	return s.repo.SameQuestion(ctx, who, id, set)
}

// NewReplies counts other people's visible replies in a thread created after
// since, the AsOf of the page or previous check. It is the live conversation
// check, so it stays one indexed read in the common case of nothing new.
func (s *Service) NewReplies(ctx context.Context, viewer, event, thread string, since time.Time) (NewReplies, error) {
	if err := owner(viewer); err != nil {
		return NewReplies{}, err
	}
	if err := saved.ValidateID(event); err != nil {
		return NewReplies{}, err
	}
	if err := PostID(thread); err != nil {
		return NewReplies{}, err
	}
	if since.IsZero() || since.Year() < 2000 {
		return NewReplies{}, invalid("since", "use the as_of time from the conversation or the previous check")
	}
	return s.repo.NewReplies(ctx, viewer, event, thread, since.UTC())
}

// ConnectionsMine is the connections value that limits open questions to the
// signed-in person's own events.
const ConnectionsMine = "mine"

// OpenQuestions lists visible questions and tips that have no visible reply
// yet, on upcoming events, newest started first like every community list.
// connections=mine, for a signed-in viewer, keeps only events they saved,
// marked Interested, or whose artist or venue they follow.
func (s *Service) OpenQuestions(ctx context.Context, viewer string, values url.Values) (List, error) {
	values, connected, err := openQuestionScope(viewer, values)
	if err != nil {
		return List{}, err
	}
	return s.list(ctx, viewer, "", "", values, &Query{Open: true, Connected: connected})
}

// openQuestionScope separates connections from the shared community filters.
func openQuestionScope(viewer string, values url.Values) (url.Values, string, error) {
	raw, ok := values["connections"]
	if !ok {
		return values, "", nil
	}
	copy := url.Values{}
	for k, v := range values {
		copy[k] = v
	}
	copy.Del("connections")
	if len(raw) != 1 || (raw[0] != ConnectionsMine && raw[0] != "all") {
		return copy, "", invalid("connections", "use mine or all")
	}
	if raw[0] != ConnectionsMine {
		return copy, "", nil
	}
	if err := owner(viewer); err != nil {
		return copy, "", err
	}
	return copy, viewer, nil
}

// ParseOpenQuery is ParseQuery for the open-questions collection, reporting
// whether it is limited to the viewer's own events.
func ParseOpenQuery(viewer string, values url.Values) (Query, bool, error) {
	rest, connected, err := openQuestionScope(viewer, values)
	q, parseErr := ParseQuery(rest, openScope(viewer, connected), true)
	if err == nil {
		err = parseErr
	}
	return q, connected != "", err
}

func openScope(viewer, connected string) string {
	scope := "questions:" + viewer
	if connected != "" {
		scope += ":mine"
	}
	return scope
}

func queryScope(scope string, q Query) string {
	data, _ := json.Marshal([]string{scope, strings.ToLower(q.City), q.Country, q.CategoryID})
	return fmt.Sprintf("%x", sha256.Sum256(data))
}
func ParseQuery(values url.Values, scope string, community bool) (Query, error) {
	copy := url.Values{}
	for k, v := range values {
		copy[k] = v
	}
	copy.Del("cursor")
	base, err := recommendations.ParseQuery(copy, scope, community)
	q := Query{Limit: base.Limit, City: base.City, Country: base.Country, CategoryID: base.CategoryID, Snapshot: time.Now().UTC()}
	if err != nil {
		var validation *accounts.ValidationError
		if errors.As(err, &validation) {
			return q, invalid(validation.Field, strings.ReplaceAll(validation.Message, "recommendation", "discussion"))
		}
		return q, err
	}
	if raw, ok := values["cursor"]; ok {
		if len(raw) != 1 || len(raw[0]) > 1024 {
			return q, invalid("cursor", "use the next cursor from this discussion collection")
		}
		var c Cursor
		data, err := base64.RawURLEncoding.Strict().DecodeString(raw[0])
		if err != nil || json.Unmarshal(data, &c) != nil || c.Scope != queryScope(scope, q) || !accounts.ValidID(c.ID) || c.At.IsZero() || c.Snapshot.IsZero() || c.At.After(c.Snapshot) || c.At.Year() < 1000 || c.Snapshot.Year() > 9999 {
			return q, invalid("cursor", "use the next cursor from this discussion collection and filters")
		}
		q.Cursor = &c
		q.Snapshot = c.Snapshot
	}
	return q, nil
}
func (s *Service) List(ctx context.Context, viewer, event, thread string, values url.Values) (List, error) {
	return s.list(ctx, viewer, event, thread, values, nil)
}

// RepliesFrom is a thread's reply page starting at one of its replies, so the
// first of several new replies opens a page holding it and those after it.
// It takes the same values as List, without a cursor.
func (s *Service) RepliesFrom(ctx context.Context, viewer, event, thread, from string, values url.Values) (List, error) {
	if err := PostID(from); err != nil {
		return List{}, invalid("from", "start from a reply in this conversation")
	}
	if _, ok := values["cursor"]; ok {
		return List{}, invalid("from", "use from or cursor, not both")
	}
	start, err := s.Parent(ctx, viewer, event, thread, from)
	if err != nil {
		return List{}, err
	}
	if start.ID == thread {
		return List{}, invalid("from", "start from a reply in this conversation")
	}
	// Just before the reply in reply order: same time, the lowest ID.
	return s.list(ctx, viewer, event, thread, values, &Query{Cursor: &Cursor{At: start.CreatedAt, ID: ""}})
}

// list reads one collection. open, when set, selects open questions and
// their own cursor scope, or (with only Cursor) starts a thread's replies at
// a reply.
func (s *Service) list(ctx context.Context, viewer, event, thread string, values url.Values, open *Query) (List, error) {
	if event != "" {
		if err := saved.ValidateID(event); err != nil {
			return List{}, err
		}
	}
	if thread != "" {
		if _, err := s.Thread(ctx, viewer, event, thread); err != nil {
			return List{}, err
		}
	}
	scope := "community:" + viewer
	if event != "" {
		scope = "event:" + event + ":" + viewer
	}
	if thread != "" {
		scope += "/thread:" + thread
	}
	if open != nil && open.Open {
		scope = openScope(viewer, open.Connected)
	}
	q, err := ParseQuery(values, scope, event == "")
	if err != nil {
		return List{}, err
	}
	if open != nil {
		q.Open, q.Connected = open.Open, open.Connected
		if open.Cursor != nil {
			q.Cursor = open.Cursor
		}
	}
	// MariaDB owns creation timestamps. Use the same clock for pagination rather
	// than temporarily hiding read-after-write results when app/DB clocks differ.
	boundary, err := s.repo.Snapshot(ctx)
	if err != nil {
		return List{}, err
	}
	if q.Cursor == nil || (open != nil && open.Cursor != nil) {
		// A first page, or one starting at a reply: read as of now.
		q.Snapshot = boundary.UTC()
	} else if q.Snapshot.After(boundary) {
		return List{}, invalid("cursor", "use the next cursor from this discussion collection")
	}
	items, count, err := s.repo.List(ctx, viewer, event, thread, q)
	if err != nil {
		return List{}, err
	}
	// AsOf is this read's own boundary, never an older cursor's, so a live
	// check from a later page doesn't re-offer replies created since then.
	result := List{Items: items, Count: count, AsOf: boundary.UTC()}
	if items == nil {
		result.Items = []Post{}
	}
	if len(items) > q.Limit {
		result.Items = items[:q.Limit]
		last := result.Items[q.Limit-1]
		data, _ := json.Marshal(Cursor{queryScope(scope, q), last.CreatedAt.UTC(), last.ID, q.Snapshot})
		next := base64.RawURLEncoding.EncodeToString(data)
		result.NextCursor = &next
	}
	return result, nil
}
