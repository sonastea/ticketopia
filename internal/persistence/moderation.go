package persistence

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/moderation"
)

type ModerationRepository struct {
	db      *sql.DB
	timeout time.Duration
}

var _ moderation.Repository = (*ModerationRepository)(nil)

func (p *Pool) Moderation() *ModerationRepository {
	return &ModerationRepository{p.db, p.config.QueryTimeout}
}
func (r *ModerationRepository) IsModerator(ctx context.Context, who string) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	var ok bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_roles WHERE account_id=? AND role='moderator')`, who).Scan(&ok)
	if err != nil {
		return false, safeError("moderator access", err)
	}
	return ok, nil
}

const eventNameColumn = `COALESCE(JSON_UNQUOTE(JSON_EXTRACT(e.snapshot,'$.name')),'')`
const outcomeJoin = ` FROM moderation_decisions d JOIN discussion_posts p ON p.post_id=d.post_id JOIN event_snapshots e ON e.event_id=p.event_id`
const outcomeColumns = `d.decision_id,d.post_id,p.event_id,` + eventNameColumn + `,COALESCE(p.root_id,p.post_id),p.created_at,d.action,d.reason,d.created_at,
 EXISTS(SELECT 1 FROM moderation_appeals ap WHERE ap.decision_id=d.decision_id),
 EXISTS(SELECT 1 FROM moderation_appeals ap WHERE ap.decision_id=d.decision_id AND ap.reviewed_by IS NOT NULL),
 (d.action='hide' AND p.hidden_at IS NOT NULL AND p.removed_at IS NULL
 AND NOT EXISTS(SELECT 1 FROM moderation_appeals ap WHERE ap.decision_id=d.decision_id)
 AND NOT EXISTS(SELECT 1 FROM moderation_decisions newer WHERE newer.post_id=d.post_id AND (newer.created_at>d.created_at OR (newer.created_at=d.created_at AND newer.decision_id>d.decision_id))))`

func outcomeTargets(o *moderation.Outcome) []any {
	return []any{&o.ID, &o.PostID, &o.EventID, &o.EventName, &o.ThreadID, &o.PostCreatedAt, &o.Action, &o.Reason, &o.CreatedAt, &o.Appealed, &o.AppealReviewed, &o.CanAppeal}
}

func scanOutcome(row interface{ Scan(...any) error }) (moderation.Outcome, error) {
	var o moderation.Outcome
	err := row.Scan(outcomeTargets(&o)...)
	if errors.Is(err, sql.ErrNoRows) {
		return o, moderation.ErrNotFound
	}
	if err != nil {
		return o, safeError("moderation outcome", err)
	}
	return o, nil
}

const reportColumns = `r.report_id,r.post_id,p.event_id,` + eventNameColumn + `,COALESCE(p.root_id,p.post_id),p.created_at,r.reason,r.context,r.created_at,
 COALESCE(d.decision_id,''),COALESCE(d.action,''),COALESCE(d.reason,''),d.created_at,
 EXISTS(SELECT 1 FROM moderation_appeals ap WHERE ap.decision_id=d.decision_id),
 EXISTS(SELECT 1 FROM moderation_appeals ap WHERE ap.decision_id=d.decision_id AND ap.reviewed_by IS NOT NULL)`
const reportJoin = ` FROM moderation_reports r JOIN discussion_posts p ON p.post_id=r.post_id JOIN event_snapshots e ON e.event_id=p.event_id LEFT JOIN moderation_decisions d ON d.decision_id=r.decision_id`

func scanReport(row interface{ Scan(...any) error }) (moderation.Report, error) {
	var r moderation.Report
	var o moderation.Outcome
	var at sql.NullTime
	err := row.Scan(&r.ID, &r.PostID, &r.EventID, &r.EventName, &r.ThreadID, &r.PostCreatedAt, &r.Reason, &r.Context, &r.CreatedAt, &o.ID, &o.Action, &o.Reason, &at, &o.Appealed, &o.AppealReviewed)
	if errors.Is(err, sql.ErrNoRows) {
		return r, moderation.ErrNotFound
	}
	if err != nil {
		return r, safeError("private report read", err)
	}
	if o.ID != "" {
		o.PostID, o.EventID, o.ThreadID, o.CreatedAt = r.PostID, r.EventID, r.ThreadID, at.Time
		r.Outcome = &moderation.Resolution{Action: o.Action, Reason: o.Reason, CreatedAt: o.CreatedAt}
	}
	return r, nil
}
func (r *ModerationRepository) Report(ctx context.Context, who, post, reason, text string) (moderation.Report, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return moderation.Report{}, false, safeError("report write", err)
	}
	defer tx.Rollback()
	var author, body string
	var unavailable bool
	err = tx.QueryRowContext(ctx, `SELECT account_id,body,(removed_at IS NOT NULL OR hidden_at IS NOT NULL) FROM discussion_posts WHERE post_id=? FOR UPDATE`, post).Scan(&author, &body, &unavailable)
	if errors.Is(err, sql.ErrNoRows) {
		return moderation.Report{}, false, moderation.ErrNotFound
	}
	if err != nil {
		return moderation.Report{}, false, safeError("report write", err)
	}
	previous, err := scanReport(tx.QueryRowContext(ctx, `SELECT `+reportColumns+reportJoin+` WHERE r.reporter_id=? AND r.post_id=?`, who, post))
	if err == nil {
		return previous, false, nil
	}
	if !errors.Is(err, moderation.ErrNotFound) {
		return moderation.Report{}, false, err
	}
	if unavailable {
		return moderation.Report{}, false, moderation.ErrNotFound
	}
	if author == who {
		return moderation.Report{}, false, &accounts.ValidationError{Field: "post_id", Message: "edit or remove your own contribution instead"}
	}
	id := accounts.ID()
	if _, err = tx.ExecContext(ctx, `INSERT INTO moderation_reports (report_id,post_id,reporter_id,reason,context,reported_body) VALUES (?,?,?,?,?,?)`, id, post, who, reason, text, body); err != nil {
		return moderation.Report{}, false, safeError("report write", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE discussion_posts SET review_version=review_version+1 WHERE post_id=?`, post); err != nil {
		return moderation.Report{}, false, safeError("report version", err)
	}
	result, err := scanReport(tx.QueryRowContext(ctx, `SELECT `+reportColumns+reportJoin+` WHERE r.report_id=?`, id))
	if err != nil {
		return moderation.Report{}, false, err
	}
	if err = tx.Commit(); err != nil {
		return moderation.Report{}, false, safeError("report write", err)
	}
	return result, true, nil
}
func (r *ModerationRepository) Reports(ctx context.Context, who string, q moderation.Query) ([]moderation.Report, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	after, args := reviewAfter("r", "report_id", q)
	args = append([]any{who}, args...)
	args = append(args, q.Limit+1)
	rows, err := r.db.QueryContext(ctx, `SELECT `+reportColumns+reportJoin+` WHERE r.reporter_id=?`+after+` ORDER BY r.created_at DESC,r.report_id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, safeError("private report collection", err)
	}
	defer rows.Close()
	items := []moderation.Report{}
	for rows.Next() {
		v, err := scanReport(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError("private report collection", err)
	}
	return items, nil
}
func (r *ModerationRepository) Outcomes(ctx context.Context, who string, q moderation.Query) ([]moderation.Outcome, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	after, args := reviewAfter("d", "decision_id", q)
	args = append([]any{who}, args...)
	args = append(args, q.Limit+1)
	rows, err := r.db.QueryContext(ctx, `SELECT `+outcomeColumns+outcomeJoin+` WHERE p.account_id=? AND d.action<>'keep'`+after+` ORDER BY d.created_at DESC,d.decision_id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, safeError("author outcomes", err)
	}
	defer rows.Close()
	items := []moderation.Outcome{}
	for rows.Next() {
		v, err := scanOutcome(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	if err = rows.Err(); err != nil {
		return nil, safeError("author outcomes", err)
	}
	return items, nil
}

const pendingReports = `(SELECT COUNT(*) FROM moderation_reports r WHERE r.post_id=p.post_id AND r.decision_id IS NULL)`
const pendingAppeals = `(SELECT COUNT(*) FROM moderation_appeals ap JOIN moderation_decisions d ON d.decision_id=ap.decision_id WHERE d.post_id=p.post_id AND ap.reviewed_by IS NULL)`

func (r *ModerationRepository) Queue(ctx context.Context, who string, q moderation.Query) ([]moderation.QueueItem, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	after, args := reviewAfter("p", "post_id", q)
	// Another moderator handles an author's own cases, without exposing private
	// report activity to that author through privileged queue access.
	args = append([]any{who}, args...)
	where := ` WHERE p.account_id<>? AND (EXISTS(SELECT 1 FROM moderation_reports r WHERE r.post_id=p.post_id) OR EXISTS(SELECT 1 FROM moderation_decisions d WHERE d.post_id=p.post_id))` + after
	if q.State == "pending" {
		where += ` AND (` + pendingReports + `>0 OR ` + pendingAppeals + `>0)`
	}
	// Load bounded identities first, then current public projections. No raw body
	// or reporter identity is needed to scan the queue.
	args = append(args, q.Limit+1)
	rows, err := r.db.QueryContext(ctx, `SELECT p.post_id,`+pendingReports+`,`+pendingAppeals+` FROM discussion_posts p`+where+` ORDER BY p.created_at DESC,p.post_id DESC LIMIT ?`, args...)
	if err != nil {
		return nil, safeError("moderation queue", err)
	}
	items := []moderation.QueueItem{}
	for rows.Next() {
		var v moderation.QueueItem
		if err = rows.Scan(&v.Post.ID, &v.PendingReports, &v.PendingAppeals); err != nil {
			rows.Close()
			return nil, safeError("moderation queue", err)
		}
		items = append(items, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, safeError("moderation queue", err)
	}
	for i := range items {
		items[i].Post, err = readPost(ctx, r.db, "", items[i].Post.ID)
		if err != nil {
			return nil, err
		}
	}
	return items, nil
}
func reviewAfter(alias, id string, q moderation.Query) (string, []any) {
	if q.After == "" {
		return "", nil
	}
	return ` AND (` + alias + `.created_at<? OR (` + alias + `.created_at=? AND ` + alias + `.` + id + `<?))`, []any{q.At.UTC(), q.At.UTC(), q.After}
}
func (r *ModerationRepository) Case(ctx context.Context, who, post string, q moderation.CaseQuery) (moderation.Case, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return moderation.Case{}, safeError("moderation case", err)
	}
	defer tx.Rollback()
	// Check immutable authorship before reading any private evidence. Moderator
	// membership never overrides reporter privacy from the affected author.
	var author string
	err = tx.QueryRowContext(ctx, `SELECT account_id FROM discussion_posts WHERE post_id=?`, post).Scan(&author)
	if errors.Is(err, sql.ErrNoRows) {
		return moderation.Case{}, moderation.ErrNotFound
	}
	if err != nil {
		return moderation.Case{}, safeError("moderation case access", err)
	}
	if author == who {
		return moderation.Case{}, moderation.ErrForbidden
	}
	c := moderation.Case{Reports: []moderation.ReviewReport{}, Decisions: []moderation.Decision{}, Appeals: []moderation.Appeal{}}
	c.Post, err = readPost(ctx, tx, "", post)
	if err != nil {
		return c, err
	}
	if err = tx.QueryRowContext(ctx, `SELECT account_id,body,review_version FROM discussion_posts WHERE post_id=?`, post).Scan(&c.AuthorID, &c.CurrentBody, &c.Version); err != nil {
		return c, safeError("moderation case", err)
	}
	after, args := reviewAfter("r", "report_id", q.Reports)
	args = append([]any{post}, args...)
	args = append(args, q.Reports.Limit+1)
	rows, err := tx.QueryContext(ctx, `SELECT report_id,reporter_id,reason,context,reported_body,created_at,decision_id IS NOT NULL FROM moderation_reports r WHERE post_id=?`+after+` ORDER BY created_at DESC,report_id DESC LIMIT ?`, args...)
	if err != nil {
		return c, safeError("moderation case reports", err)
	}
	for rows.Next() {
		var v moderation.ReviewReport
		err = rows.Scan(&v.ID, &v.ReporterID, &v.Reason, &v.Context, &v.ReportedBody, &v.CreatedAt, &v.Reviewed)
		if err != nil {
			break
		}
		v.PostID, v.EventID, v.ThreadID = post, c.Post.EventID, c.Post.ThreadID
		v.EventName, v.PostCreatedAt = c.Post.Event.Name, c.Post.CreatedAt
		c.Reports = append(c.Reports, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return c, safeError("moderation case reports", err)
	}
	after, args = reviewAfter("d", "decision_id", q.Decisions)
	args = append([]any{post}, args...)
	args = append(args, q.Decisions.Limit+1)
	rows, err = tx.QueryContext(ctx, `SELECT `+outcomeColumns+`,d.moderator_id,d.notes`+outcomeJoin+` WHERE d.post_id=?`+after+` ORDER BY d.created_at DESC,d.decision_id DESC LIMIT ?`, args...)
	if err != nil {
		return c, safeError("moderation case decisions", err)
	}
	for rows.Next() {
		var v moderation.Decision
		targets := append(outcomeTargets(&v.Outcome), &v.ModeratorID, &v.Notes)
		err = rows.Scan(targets...)
		if err != nil {
			break
		}
		v.PostID, v.EventID, v.ThreadID = post, c.Post.EventID, c.Post.ThreadID
		c.Decisions = append(c.Decisions, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return c, safeError("moderation case decisions", err)
	}
	after, args = reviewAfter("ap", "decision_id", q.Appeals)
	args = append([]any{post}, args...)
	args = append(args, q.Appeals.Limit+1)
	rows, err = tx.QueryContext(ctx, `SELECT ap.decision_id,ap.context,ap.created_at,ap.reviewed_by IS NOT NULL FROM moderation_appeals ap JOIN moderation_decisions d ON d.decision_id=ap.decision_id WHERE d.post_id=?`+after+` ORDER BY ap.created_at DESC,ap.decision_id DESC LIMIT ?`, args...)
	if err != nil {
		return c, safeError("moderation case appeals", err)
	}
	for rows.Next() {
		var v moderation.Appeal
		err = rows.Scan(&v.DecisionID, &v.Context, &v.CreatedAt, &v.Reviewed)
		if err != nil {
			break
		}
		c.Appeals = append(c.Appeals, v)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return c, safeError("moderation case appeals", err)
	}
	if err = tx.Commit(); err != nil {
		return c, safeError("moderation case", err)
	}
	return c, nil
}
func lockModerator(ctx context.Context, tx *sql.Tx, who string) error {
	// Same lock order as operator role changes. A revoke waits for an already
	// authorized transaction; subsequent decisions cannot use a stale role.
	var id string
	if err := tx.QueryRowContext(ctx, `SELECT account_id FROM accounts WHERE account_id=? FOR UPDATE`, who).Scan(&id); err != nil {
		return safeError("moderator lock", err)
	}
	err := tx.QueryRowContext(ctx, `SELECT account_id FROM account_roles WHERE account_id=? AND role='moderator' FOR UPDATE`, who).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return moderation.ErrForbidden
	}
	if err != nil {
		return safeError("moderator lock", err)
	}
	return nil
}
func (r *ModerationRepository) Decide(ctx context.Context, who, post string, review moderation.Review) (moderation.Outcome, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return moderation.Outcome{}, safeError("moderation decision", err)
	}
	defer tx.Rollback()
	if err = lockModerator(ctx, tx, who); err != nil {
		return moderation.Outcome{}, err
	}
	var author string
	var version int64
	var updated time.Time
	var removed, hidden bool
	err = tx.QueryRowContext(ctx, `SELECT account_id,review_version,updated_at,removed_at IS NOT NULL,hidden_at IS NOT NULL FROM discussion_posts WHERE post_id=? FOR UPDATE`, post).Scan(&author, &version, &updated, &removed, &hidden)
	if errors.Is(err, sql.ErrNoRows) {
		return moderation.Outcome{}, moderation.ErrNotFound
	}
	if err != nil {
		return moderation.Outcome{}, safeError("moderation decision", err)
	}
	if author == who {
		return moderation.Outcome{}, moderation.ErrForbidden
	}
	if version != *review.ExpectedVersion || !updated.Equal(review.ExpectedUpdatedAt) {
		return moderation.Outcome{}, moderation.ErrConflict
	}
	if (review.Action == "restore" && (removed || !hidden)) || (review.Action == "hide" && removed) || (review.Action == "keep" && hidden && !removed) {
		return moderation.Outcome{}, &accounts.ValidationError{Field: "action", Message: "choose an action compatible with the current contribution state"}
	}
	id := accounts.ID()
	if _, err = tx.ExecContext(ctx, `INSERT INTO moderation_decisions (decision_id,post_id,moderator_id,action,reason,notes) VALUES (?,?,?,?,?,?)`, id, post, who, review.Action, review.Reason, review.Notes); err != nil {
		return moderation.Outcome{}, safeError("moderation decision", err)
	}
	state := `hidden_at`
	switch review.Action {
	case "hide":
		state = `COALESCE(hidden_at,UTC_TIMESTAMP(6))`
	case "restore":
		state = `NULL`
	}
	if _, err = tx.ExecContext(ctx, `UPDATE discussion_posts SET hidden_at=`+state+`,review_version=review_version+1 WHERE post_id=?`, post); err != nil {
		return moderation.Outcome{}, safeError("moderation visibility", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE moderation_reports SET decision_id=? WHERE post_id=? AND decision_id IS NULL`, id, post); err != nil {
		return moderation.Outcome{}, safeError("report resolution", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE moderation_appeals ap JOIN moderation_decisions d ON d.decision_id=ap.decision_id SET ap.reviewed_by=? WHERE d.post_id=? AND ap.reviewed_by IS NULL`, id, post); err != nil {
		return moderation.Outcome{}, safeError("appeal resolution", err)
	}
	o, err := scanOutcome(tx.QueryRowContext(ctx, `SELECT `+outcomeColumns+outcomeJoin+` WHERE d.decision_id=?`, id))
	if err != nil {
		return o, err
	}
	if err = tx.Commit(); err != nil {
		return o, safeError("moderation decision", err)
	}
	return o, nil
}
func (r *ModerationRepository) Appeal(ctx context.Context, who, decision, text string) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return safeError("appeal write", err)
	}
	defer tx.Rollback()
	var post string
	err = tx.QueryRowContext(ctx, `SELECT d.post_id FROM moderation_decisions d JOIN discussion_posts p ON p.post_id=d.post_id WHERE d.decision_id=? AND p.account_id=? AND d.action='hide'`, decision, who).Scan(&post)
	if errors.Is(err, sql.ErrNoRows) {
		return moderation.ErrNotFound
	}
	if err != nil {
		return safeError("appeal write", err)
	}
	var hidden, removed bool
	if err = tx.QueryRowContext(ctx, `SELECT hidden_at IS NOT NULL,removed_at IS NOT NULL FROM discussion_posts WHERE post_id=? FOR UPDATE`, post).Scan(&hidden, &removed); err != nil {
		return safeError("appeal write", err)
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM moderation_appeals WHERE decision_id=?)`, decision).Scan(&exists); err != nil {
		return safeError("appeal retry", err)
	}
	if exists {
		return nil
	}
	var latest string
	if err = tx.QueryRowContext(ctx, `SELECT decision_id FROM moderation_decisions WHERE post_id=? ORDER BY created_at DESC,decision_id DESC LIMIT 1`, post).Scan(&latest); err != nil {
		return safeError("appeal write", err)
	}
	if !hidden || removed || latest != decision {
		return moderation.ErrConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO moderation_appeals (decision_id,context) VALUES (?,?)`, decision, text); err != nil {
		return safeError("appeal write", err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE discussion_posts SET review_version=review_version+1 WHERE post_id=?`, post); err != nil {
		return safeError("appeal version", err)
	}
	if err = tx.Commit(); err != nil {
		return safeError("appeal write", err)
	}
	return nil
}

// SetModerator is operator-only. Runtime has no write grants on either role
// table. Use an explicit operator label plus the authenticated database user
// for an atomic, durable audit trail; no-op retries create no extra events.
func (p *Pool) SetModerator(ctx context.Context, account, operator string, grant bool) (bool, error) {
	if !accounts.ValidID(account) {
		return false, &accounts.ValidationError{Field: "account_id", Message: "use the existing account's 32-character ID"}
	}
	label, err := moderation.Text("operator", operator, true)
	if err != nil {
		return false, err
	}
	if len(label) > 120 {
		return false, &accounts.ValidationError{Field: "operator", Message: "use an operator label up to 120 bytes"}
	}
	ctx, cancel := context.WithTimeout(ctx, p.config.QueryTimeout)
	defer cancel()
	tx, err := p.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return false, safeError("role change", err)
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `SELECT account_id FROM accounts WHERE account_id=? FOR UPDATE`, account).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, accounts.ErrNotFound
	}
	if err != nil {
		return false, safeError("role change", err)
	}
	var exists bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM account_roles WHERE account_id=? AND role='moderator')`, account).Scan(&exists); err != nil {
		return false, safeError("role change", err)
	}
	if exists == grant {
		return false, nil
	}
	action := "revoke"
	if grant {
		action = "grant"
		_, err = tx.ExecContext(ctx, `INSERT INTO account_roles (account_id,role) VALUES (?,'moderator')`, account)
	} else {
		_, err = tx.ExecContext(ctx, `DELETE FROM account_roles WHERE account_id=? AND role='moderator'`, account)
	}
	if err != nil {
		return false, safeError("role change", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO account_role_events (role_event_id,account_id,role,action,operator_label,database_user) VALUES (?,?,'moderator',?,?,CURRENT_USER())`, accounts.ID(), account, action, label); err != nil {
		return false, safeError("role audit", err)
	}
	if err = tx.Commit(); err != nil {
		return false, safeError("role change", err)
	}
	return true, nil
}
