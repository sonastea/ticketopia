package persistence

import (
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
)

// Upgrading to v28 keeps the private changes existing API tokens could make
// and gives browser sessions no scopes.
func TestMariaDBTokenScopesUpgrade(t *testing.T) {
	f := newMaria(t)
	ctx := t.Context()
	old := fstest.MapFS{}
	entries, err := migrations.ReadDir("migrations")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() >= "00028" {
			continue
		}
		data, err := migrations.ReadFile("migrations/" + e.Name())
		if err != nil {
			t.Fatal(err)
		}
		old[e.Name()] = &fstest.MapFile{Data: data}
	}
	if err := migrateFS(ctx, f.migration, old); err != nil {
		t.Fatal(err)
	}
	db, err := connect(ctx, f.migration)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	owner := accounts.ID()
	execSQL(t, db, `INSERT INTO accounts (account_id,email) VALUES (?,?)`, owner, "legacy@example.com")
	now, expires := time.Now().UTC(), time.Now().UTC().Add(time.Hour)
	token, session := "legacy-api-token", "legacy-session"
	for raw, kind := range map[string]string{token: "api", session: "session"} {
		hash := accounts.Hash(raw)
		execSQL(t, db, `INSERT INTO account_credentials (credential_id,account_id,token_hash,kind,name,created_at,expires_at) VALUES (?,?,?,?,?,?,?)`,
			accounts.ID(), owner, hash[:], kind, kind, now, expires)
	}

	f.migrate(t)
	repo := f.open(t).Accounts()
	_, c, err := repo.Authenticate(ctx, accounts.Hash(token), "api", now)
	if err != nil || !slices.Equal(c.Scopes, []string{accounts.ScopeRead, accounts.ScopeWrite}) {
		t.Fatal("legacy API token scopes", c.Scopes, err)
	}
	if _, c, err = repo.Authenticate(ctx, accounts.Hash(session), "session", now); err != nil || len(c.Scopes) != 0 {
		t.Fatal("browser session gained scopes", c.Scopes, err)
	}
	list, err := repo.ListTokens(ctx, owner, now)
	if err != nil || len(list) != 1 || !list[0].Can(accounts.ScopeWrite) {
		t.Fatal("listed legacy token scopes", list, err)
	}
	// New read-only tokens stay read-only across reads.
	s := accounts.New(repo, accountProviderFixture{})
	_, raw, err := s.CreateToken(ctx, owner, "Reader", nil)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.Authenticate(ctx, raw, "api")
	if err != nil || !slices.Equal(p.Credential.Scopes, []string{accounts.ScopeRead}) {
		t.Fatal("new read-only token", p.Credential.Scopes, err)
	}
}
