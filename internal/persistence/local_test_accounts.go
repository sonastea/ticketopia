package persistence

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/sonastea/ticketopia/internal/accounts"
)

// Throwaway accounts for trying agent tools against a local server are marked
// so no real account can match both: Google subjects are numeric, and
// .invalid (RFC 2606) is never a real mail domain.
const (
	LocalTestSubjectPrefix = "ticketopia-local-test-"
	LocalTestEmailDomain   = "@local-test.invalid"
)

// ErrLocalTestAudited is a throwaway account with moderation or role records,
// which form an audit trail that is never erased.
var ErrLocalTestAudited = errors.New("a local test account has moderation or role records; remove them by hand if this database is disposable")

// localTestAccountTables are deleted by account, in foreign-key order, after
// posts and their dependents. A test keeps the list complete.
var localTestAccountTables = []string{
	"live_recommendation_activity", "event_recommendations", "recommendation_activity",
	"event_interests", "saved_events", "artist_follows", "venue_follows",
	"agent_drafts", "oauth_codes", "oauth_grants", "account_credentials", "account_identities",
}

// RemoveLocalTestAccounts deletes every throwaway account and everything it
// owns in one transaction: its posts with replies in their threads (even
// other accounts'), Helpful marks, follows and notifications on them, and its
// saves, interests, recommendations, reminders, follows, drafts, OAuth
// connections and credentials. It reports how many accounts it removed. Use
// it only on a local database; it needs operator (migration) credentials.
func (p *Pool) RemoveLocalTestAccounts(ctx context.Context) (int, error) {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, safeError("local test account removal", err)
	}
	defer tx.Rollback()
	ids, err := queryIDs(ctx, tx, `SELECT a.account_id FROM accounts a JOIN account_identities i ON i.account_id=a.account_id
		WHERE i.issuer=? AND i.subject LIKE ? AND a.email LIKE ? FOR UPDATE`, accounts.GoogleIssuer, LocalTestSubjectPrefix+"%", "%"+LocalTestEmailDomain)
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	in, args := inList(ids)
	// The account's posts, then replies in or to them until none are added.
	posts, err := queryIDs(ctx, tx, `SELECT post_id FROM discussion_posts WHERE account_id IN `+in, args...)
	if err != nil {
		return 0, err
	}
	for len(posts) > 0 {
		pin, pargs := inList(posts)
		more, err := queryIDs(ctx, tx, `SELECT post_id FROM discussion_posts WHERE (root_id IN `+pin+` OR parent_id IN `+pin+`) AND post_id NOT IN `+pin, append(append(pargs, pargs...), pargs...)...)
		if err != nil {
			return 0, err
		}
		if len(more) == 0 {
			break
		}
		posts = append(posts, more...)
	}
	pin, pargs := inList(append([]string{""}, posts...))
	var audited int
	if err := tx.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM moderation_reports WHERE reporter_id IN `+in+` OR post_id IN `+pin+`)+
		(SELECT COUNT(*) FROM moderation_decisions WHERE moderator_id IN `+in+` OR post_id IN `+pin+`)+
		(SELECT COUNT(*) FROM account_roles WHERE account_id IN `+in+`)+
		(SELECT COUNT(*) FROM account_role_events WHERE account_id IN `+in+`)`,
		concat(args, pargs, args, pargs, args, args)...).Scan(&audited); err != nil {
		return 0, safeError("local test account removal", err)
	}
	if audited > 0 {
		return 0, ErrLocalTestAudited
	}
	// Envelopes that held these posts are removed once nothing else is in them.
	envelopes, err := queryIDs(ctx, tx, `SELECT DISTINCT notification_id FROM discussion_notification_posts WHERE post_id IN `+pin, pargs...)
	if err != nil {
		return 0, err
	}
	nin, nargs := inList(append([]string{""}, envelopes...))
	steps := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM discussion_notification_posts WHERE post_id IN ` + pin, pargs},
		{`DELETE n FROM discussion_notifications n WHERE n.notification_id IN ` + nin + ` AND NOT EXISTS (SELECT 1 FROM discussion_notification_posts x WHERE x.notification_id=n.notification_id)`, nargs},
		// Notifications cascade from their follows.
		{`DELETE FROM discussion_follows WHERE account_id IN ` + in + ` OR thread_id IN ` + pin, concat(args, pargs)},
		{`DELETE FROM post_helpful WHERE account_id IN ` + in + ` OR post_id IN ` + pin, concat(args, pargs)},
		{`DELETE FROM discussion_same_question WHERE account_id IN ` + in + ` OR post_id IN ` + pin, concat(args, pargs)},
		// Newest first, so replies go before what they answer.
		{`DELETE FROM discussion_posts WHERE post_id IN ` + pin + ` ORDER BY created_at DESC, post_id DESC`, pargs},
		{`DELETE j FROM reminder_jobs j JOIN event_reminders r ON r.reminder_id=j.reminder_id WHERE r.account_id IN ` + in, args},
		{`DELETE FROM event_reminders WHERE account_id IN ` + in, args},
	}
	for _, table := range localTestAccountTables {
		steps = append(steps, struct {
			query string
			args  []any
		}{`DELETE FROM ` + table + ` WHERE account_id IN ` + in, args})
	}
	steps = append(steps, struct {
		query string
		args  []any
	}{`DELETE FROM accounts WHERE account_id IN ` + in, args})
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step.query, step.args...); err != nil {
			return 0, safeError("local test account removal", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, safeError("local test account removal", err)
	}
	invalidateDiscoverCounts()
	return len(ids), nil
}

func queryIDs(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]string, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, safeError("local test account removal", err)
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, safeError("local test account removal", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, safeError("local test account removal", err)
	}
	return ids, nil
}

// inList is "(?,?,…)" and its arguments; callers never pass an empty list.
func inList(ids []string) (string, []any) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	return "(" + strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",") + ")", args
}

func concat(lists ...[]any) []any {
	var all []any
	for _, l := range lists {
		all = append(all, l...)
	}
	return all
}
