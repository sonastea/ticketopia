package localaccounts

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/sonastea/ticketopia/internal/accounts"
	"github.com/sonastea/ticketopia/internal/oauth"
	"github.com/sonastea/ticketopia/internal/persistence"
	"github.com/sonastea/ticketopia/internal/persistence/persistencetest"
)

// Only loopback sites and databases are accepted.
func TestGuard(t *testing.T) {
	for _, c := range []struct {
		base, host string
		ok         bool
	}{
		{"http://localhost:3000", "localhost", true},
		{"http://127.0.0.1:8080", "127.0.0.1", true},
		{"http://[::1]:8080", "::1", true},
		{"https://topia.sonastea.com", "localhost", false},
		{"http://localhost:3000", "db.internal", false},
		{"http://localhost.example.com", "localhost", false},
		{"", "localhost", false},
		{"http://localhost:3000", "", false},
	} {
		if err := Guard(c.base, c.host); (err == nil) != c.ok {
			t.Errorf("Guard(%q, %q) = %v", c.base, c.host, err)
		}
	}
}

// Created accounts sign in with every credential they are given, with the
// scopes promised; creating again replaces them.
func TestMariaDBCreate(t *testing.T) {
	options, ok := persistencetest.OptionsFromEnv(filepath.Join("..", "..", ".local", "mariadb", "certs", "ca.crt"))
	if !ok {
		t.Skip("set MARIADB_TEST_ADDR to run real MariaDB tests")
	}
	ctx := t.Context()
	db, err := persistencetest.Create(ctx, options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	config := persistence.Config{Enabled: true, Host: db.Host, Port: db.Port, Database: db.Name, User: db.MigrationUser, Password: db.Password,
		TLSMode: "verify-full", CAFile: db.CAFile, MaxOpen: 4, MaxIdle: 2, Lifetime: time.Minute, IdleTime: time.Minute, DialTimeout: 3 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, StartupTimeout: 10 * time.Second, QueryTimeout: 3 * time.Second,
		ReadyTimeout: time.Second, MigrationTimeout: 2 * time.Minute}
	if err := persistence.Migrate(ctx, config); err != nil {
		t.Fatal(err)
	}
	if err := db.GrantRuntime(ctx); err != nil {
		t.Fatal(err)
	}
	p, err := persistence.Open(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })

	first, err := Create(ctx, p, "http://localhost:3000/")
	if err != nil {
		t.Fatal(err)
	}
	if first.MCPURL != "http://localhost:3000/mcp" || first.SessionCookie != "ticketopia_session" || len(first.Accounts) != 2 {
		t.Fatalf("result: %+v", first)
	}
	auth := accounts.New(p.Accounts(), nil)
	grants := oauth.New(p.OAuth(), nil, "http://localhost:3000")
	for key, a := range first.Accounts {
		for kind, raw := range map[string]string{"session": a.Session, "api": a.APIToken} {
			if principal, err := auth.Authenticate(ctx, raw, kind); err != nil || principal.Account.ID != a.ID {
				t.Fatalf("%s %s: %v", key, kind, err)
			}
		}
		for raw, scopes := range map[string][]string{a.OAuthPublish: {"read", "write", "publish"}, a.OAuthReadOnly: {"read"}} {
			access, err := grants.Authenticate(ctx, raw)
			if err != nil || access.AccountID != a.ID || !slices.Equal(access.Scopes, scopes) || access.ClientName != ClientName {
				t.Fatalf("%s OAuth %v: %+v %v", key, scopes, access, err)
			}
		}
	}
	if owner, _ := auth.Account(ctx, first.Accounts["owner"].ID); owner.Preferences.City != "Chicago" {
		t.Fatalf("owner city: %+v", owner.Preferences)
	}

	second, err := Create(ctx, p, "http://localhost:3000")
	if err != nil {
		t.Fatal(err)
	}
	for key, a := range first.Accounts {
		if second.Accounts[key].ID == a.ID {
			t.Fatalf("%s was not replaced", key)
		}
		if _, err := auth.Authenticate(ctx, a.APIToken, "api"); err == nil {
			t.Fatalf("replaced %s's token still works", key)
		}
	}
	if n, err := p.RemoveLocalTestAccounts(ctx); err != nil || n != 2 {
		t.Fatalf("removed %d: %v", n, err)
	}
}
