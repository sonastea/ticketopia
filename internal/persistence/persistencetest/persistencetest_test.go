package persistencetest

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestMariaDBDropStaleKeepsLiveFixtures(t *testing.T) {
	o, ok := OptionsFromEnv(filepath.Join("..", "..", "..", ".local", "mariadb", "certs", "ca.crt"))
	if !ok {
		t.Skip("set MARIADB_TEST_ADDR to run real MariaDB tests")
	}
	live, err := Create(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { live.Close() })
	stale, err := Create(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stale.Close() })
	// A killed process loses its connections, which releases the lock.
	var id int64
	if err := stale.lock.QueryRowContext(t.Context(), "SELECT CONNECTION_ID()").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := live.Exec(t.Context(), "KILL ?", id); err != nil {
		t.Fatal(err)
	}
	stale.lock.Close()
	stale.lock = nil

	// A look-alike schema without the fixture marker is reported, never dropped.
	lookalike := "ticketopia_test_" + strings.Repeat("0", 15) + "1"
	if err := live.Exec(t.Context(), "CREATE DATABASE "+lookalike); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		d, err := adminOnly(context.Background(), o)
		if err == nil {
			d.Exec(context.Background(), "DROP DATABASE IF EXISTS "+lookalike)
			d.admin.Close()
		}
	})

	c, err := DropStale(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(c.Dropped, stale.Name) || slices.Contains(c.Dropped, live.Name) || slices.Contains(c.Dropped, lookalike) {
		t.Fatalf("dropped %v; want %s but not %s or %s", c.Dropped, stale.Name, live.Name, lookalike)
	}
	if !slices.Contains(c.Unmarked, lookalike) {
		t.Fatalf("unmarked %v; want %s", c.Unmarked, lookalike)
	}
	exists := func(schema string) bool {
		var n int
		if err := live.Admin().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name=?`, schema).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n == 1
	}
	if exists(stale.Name) || !exists(live.Name) || !exists(lookalike) {
		t.Fatalf("stale exists=%v live exists=%v", exists(stale.Name), exists(live.Name))
	}
	var users int
	if err := live.Admin().QueryRowContext(t.Context(), `SELECT COUNT(*) FROM mysql.user WHERE user IN (?,?)`, stale.RuntimeUser, stale.MigrationUser).Scan(&users); err != nil || users != 0 {
		t.Fatalf("stale users remain: %d %v", users, err)
	}
	stale.drops = nil // already dropped
	if err := live.Close(); err != nil {
		t.Fatal(err)
	}
	if exists := func() bool {
		d, err := adminOnly(t.Context(), o)
		if err != nil {
			t.Fatal(err)
		}
		defer d.admin.Close()
		var n int
		_ = d.admin.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name=?`, live.Name).Scan(&n)
		return n == 1
	}(); exists {
		t.Fatal("Close left the live schema behind")
	}
}

// The keepalive holds the lock across the server's idle-session timeout.
func TestMariaDBFixtureLockSurvivesIdleTimeout(t *testing.T) {
	o, ok := OptionsFromEnv(filepath.Join("..", "..", "..", ".local", "mariadb", "certs", "ca.crt"))
	if !ok {
		t.Skip("set MARIADB_TEST_ADDR to run real MariaDB tests")
	}
	keepaliveInterval = 200 * time.Millisecond
	t.Cleanup(func() { keepaliveInterval = time.Minute })
	d, err := Create(t.Context(), o)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.lock.ExecContext(t.Context(), "SET SESSION wait_timeout=1"); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2500 * time.Millisecond)
	var holder *int64
	if err := d.Admin().QueryRowContext(t.Context(), "SELECT IS_USED_LOCK(?)", lockName(strings.TrimPrefix(d.Name, "ticketopia_test_"))).Scan(&holder); err != nil {
		t.Fatal(err)
	}
	if holder == nil {
		t.Fatal("fixture lock released while the fixture was in use")
	}
}
