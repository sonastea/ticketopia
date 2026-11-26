package persistence

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/discussionfollows"
	"github.com/sonastea/ticketopia/internal/discussions"
	"github.com/sonastea/ticketopia/internal/drafts"
	"github.com/sonastea/ticketopia/internal/oauth"
)

// localTestProvider signs in subject "email|subject" with that email, so
// tests can mark accounts by subject, email, or both.
type localTestProvider struct{}

func (localTestProvider) AuthorizationURL(l accounts.Login) string {
	return "https://accounts.example/?state=" + l.State
}
func (localTestProvider) Verify(_ context.Context, code string, _ accounts.Flow) (accounts.Identity, error) {
	email, subject, _ := strings.Cut(code, "|")
	return accounts.Identity{Issuer: accounts.GoogleIssuer, Subject: subject, Email: email}, nil
}

// Every table that names an account is either cleared by account or handled
// explicitly, so a new account-owned table cannot be left dangling.
func TestMariaDBLocalTestAccountTablesAreComplete(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	rows, err := f.admin.QueryContext(t.Context(), `SELECT DISTINCT table_name FROM information_schema.columns WHERE table_schema=? AND column_name='account_id'
		UNION SELECT DISTINCT table_name FROM information_schema.key_column_usage WHERE table_schema=? AND referenced_table_name='accounts'`, f.runtime.Database, f.runtime.Database)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	handled := append(slices.Clone(localTestAccountTables), "accounts", "discussion_posts", "discussion_follows", "discussion_notifications",
		"post_helpful", "discussion_same_question", "event_reminders", "moderation_reports", "moderation_decisions", "account_roles", "account_role_events")
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(handled, table) {
			t.Errorf("RemoveLocalTestAccounts does not handle %s", table)
		}
	}
}

// Only accounts marked by both subject and email are removed, with all they
// made, including another account's reply in their thread; a real account's
// own activity is untouched. Moderation records stop the removal.
func TestMariaDBRemoveLocalTestAccounts(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	// Removal deletes accounts, which only operator (migration) access may.
	operator, err := Open(t.Context(), f.migration)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { operator.Close() })
	ctx := t.Context()
	auth := accounts.New(p.Accounts(), localTestProvider{})
	login := func(email, subject string) accounts.Account {
		a, _ := loginAccount(t, auth, email+"|"+subject)
		return a
	}
	test := login("owner"+LocalTestEmailDomain, LocalTestSubjectPrefix+"owner")
	real := login("person@example.com", "1234567890")
	// Marked by subject alone, or email alone, is not a test account.
	halfSubject := login("half@example.com", LocalTestSubjectPrefix+"half")
	halfEmail := login("half"+LocalTestEmailDomain, "2345678901")
	if _, _, err := auth.CreateToken(ctx, test.ID, "local", readWrite); err != nil {
		t.Fatal(err)
	}

	detail := savedDetail("LocalTestAccounts")
	event := detail.Item.ID
	posts := discussions.New(p.Discussions(), discussionDetailFixture{detail})
	follows := discussionfollows.New(p.DiscussionFollows())
	testRoot, _, err := posts.Create(ctx, test.ID, event, "", "", "Test question", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	realRoot, _, err := posts.Create(ctx, real.ID, event, "", "", "Real question", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := follows.Set(ctx, real.ID, event, realRoot.ID, "immediate"); err != nil {
		t.Fatal(err)
	}
	if _, err := follows.Set(ctx, real.ID, event, testRoot.ID, "immediate"); err != nil {
		t.Fatal(err)
	}
	testReply, _, err := posts.Create(ctx, test.ID, event, realRoot.ID, "", "Test reply in a real thread", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	realReply, _, err := posts.Create(ctx, real.ID, event, realRoot.ID, testReply.ID, "Real reply to the test reply", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := posts.Create(ctx, real.ID, event, testRoot.ID, "", "Real reply in the test thread", accounts.ID()); err != nil {
		t.Fatal(err)
	}
	keptReply, _, err := posts.Create(ctx, real.ID, event, realRoot.ID, "", "Real reply kept", accounts.ID())
	if err != nil {
		t.Fatal(err)
	}
	for _, mark := range [][2]string{{real.ID, testRoot.ID}, {test.ID, realRoot.ID}} {
		if _, err := posts.React(ctx, mark[0], mark[1], true); err != nil {
			t.Fatal(err)
		}
	}
	for _, who := range []string{test.ID, real.ID} {
		if _, _, err := p.Saved().Save(ctx, who, detail); err != nil {
			t.Fatal(err)
		}
		if _, _, err := p.Interests().Set(ctx, who, detail, "public"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := p.Recommendations().Set(ctx, who, detail, "Go"); err != nil {
			t.Fatal(err)
		}
		if _, err := p.Reminders().Set(ctx, who, event, 60); err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now().UTC().Truncate(time.Microsecond)
	grant := oauth.Grant{ID: accounts.ID(), AccountID: test.ID, ClientID: "https://agent.example/client.json", ClientName: "Agent", Scopes: []string{"read", "write"},
		CreatedAt: now, ExpiresAt: now.Add(time.Hour), AccessHash: oauth.Hash(accounts.ID()), AccessUntil: now.Add(time.Hour)}
	if err := p.OAuth().CreateGrant(ctx, grant, now); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Drafts().Create(ctx, &detail, drafts.Draft{ID: accounts.ID(), EventID: event, Kind: drafts.KindQuestion, Text: "Draft",
		ClientName: "Agent", ClientHost: "agent.example", GrantID: grant.ID, CreatedAt: now, ExpiresAt: now.Add(drafts.Lifetime)}, test.ID, now); err != nil {
		t.Fatal(err)
	}
	count := func(query string, args ...any) int {
		t.Helper()
		var n int
		if err := f.admin.QueryRowContext(ctx, strings.ReplaceAll(query, "{db}", f.runtime.Database), args...).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	realRows := func() int {
		return count(`SELECT (SELECT COUNT(*) FROM {db}.saved_events WHERE account_id=?)+(SELECT COUNT(*) FROM {db}.event_interests WHERE account_id=?)+
			(SELECT COUNT(*) FROM {db}.event_recommendations WHERE account_id=?)+(SELECT COUNT(*) FROM {db}.event_reminders WHERE account_id=?)`, real.ID, real.ID, real.ID, real.ID)
	}
	before := realRows()

	// A moderation record is an audit trail: nothing is removed.
	if _, _, err := p.Moderation().Report(ctx, test.ID, realRoot.ID, "spam", "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := operator.RemoveLocalTestAccounts(ctx); !errors.Is(err, ErrLocalTestAudited) {
		t.Fatalf("audited removal: %v", err)
	}
	if count(`SELECT COUNT(*) FROM {db}.accounts WHERE account_id=?`, test.ID) != 1 {
		t.Fatal("refused removal deleted the account")
	}
	if _, err := f.admin.ExecContext(ctx, `DELETE FROM `+f.runtime.Database+`.moderation_reports WHERE reporter_id=?`, test.ID); err != nil {
		t.Fatal(err)
	}

	if n, err := operator.RemoveLocalTestAccounts(ctx); err != nil || n != 1 {
		t.Fatalf("removed %d: %v", n, err)
	}
	for _, table := range append(slices.Clone(localTestAccountTables), "accounts", "discussion_posts", "discussion_follows", "post_helpful", "event_reminders") {
		if n := count(`SELECT COUNT(*) FROM {db}.`+table+` WHERE account_id=?`, test.ID); n != 0 {
			t.Errorf("%s keeps %d test rows", table, n)
		}
	}
	// The test thread went with its replies; replies to the test reply too.
	if n := count(`SELECT COUNT(*) FROM {db}.discussion_posts WHERE post_id IN (?,?) OR root_id=?`, testRoot.ID, realReply.ID, testRoot.ID); n != 0 {
		t.Errorf("%d posts in or under test posts remain", n)
	}
	if n := count(`SELECT COUNT(*) FROM {db}.discussion_posts WHERE post_id IN (?,?)`, realRoot.ID, keptReply.ID); n != 2 {
		t.Errorf("real posts removed: %d left", n)
	}
	if n := count(`SELECT COUNT(*) FROM {db}.discussion_follows WHERE account_id=?`, real.ID); n != 1 {
		t.Errorf("real follows: %d", n)
	}
	if n := count(`SELECT COUNT(*) FROM {db}.discussion_notifications n WHERE NOT EXISTS (SELECT 1 FROM {db}.discussion_notification_posts x WHERE x.notification_id=n.notification_id)`); n != 0 {
		t.Errorf("%d empty notification envelopes", n)
	}
	if after := realRows(); after != before {
		t.Errorf("real account rows %d -> %d", before, after)
	}
	for _, kept := range []string{real.ID, halfSubject.ID, halfEmail.ID} {
		if count(`SELECT COUNT(*) FROM {db}.accounts WHERE account_id=?`, kept) != 1 {
			t.Errorf("account %s removed", kept)
		}
	}
	if n, err := operator.RemoveLocalTestAccounts(ctx); err != nil || n != 0 {
		t.Fatalf("second removal: %d %v", n, err)
	}
}
