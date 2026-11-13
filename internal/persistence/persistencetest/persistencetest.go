// Package persistencetest creates disposable MariaDB schemas and users for the
// opt-in integration suite and local review fixtures. It deliberately does not
// import persistence, so in-package persistence tests can use it. Never point
// its administrator credentials at a shared or production server.
package persistencetest

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
)

// Options selects the administrator connection. Defaults match make db-setup.
type Options struct {
	Addr, CAFile, AdminUser, AdminPassword string
}

// OptionsFromEnv reads the MARIADB_TEST_* settings used by the opt-in suite;
// ok is false when MARIADB_TEST_ADDR is unset. defaultCA applies when
// MARIADB_TEST_CA is unset.
func OptionsFromEnv(defaultCA string) (o Options, ok bool) {
	o = Options{Addr: os.Getenv("MARIADB_TEST_ADDR"), CAFile: os.Getenv("MARIADB_TEST_CA"),
		AdminUser: os.Getenv("MARIADB_TEST_ADMIN_USER"), AdminPassword: os.Getenv("MARIADB_TEST_ADMIN_PASSWORD")}
	if o.CAFile == "" {
		o.CAFile = defaultCA
	}
	return o, o.Addr != ""
}

// Database is one random schema with a read-only runtime user (until
// GrantRuntime) and a migration user. Close drops all three.
type Database struct {
	Host, Port, Name, CAFile   string
	RuntimeUser, MigrationUser string
	Password                   string
	admin                      *sql.DB
	lock                       *sql.Conn
	stopKeepalive              context.CancelFunc
	keepaliveDone              chan struct{}
	closeOnce                  sync.Once
	drops                      []string
	closeErr                   error
}

// Create connects with verified TLS and provisions a fresh schema and users.
// Every created object is dropped again if a later step fails.
func Create(ctx context.Context, o Options) (_ *Database, err error) {
	var entropy [8]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return nil, err
	}
	d, err := adminOnly(ctx, o)
	if err != nil {
		return nil, err
	}
	suffix := hex.EncodeToString(entropy[:])
	d.Name, d.RuntimeUser, d.MigrationUser, d.Password = "ticketopia_test_"+suffix, "runtime_"+suffix, "migration_"+suffix, "fixture-"+suffix
	defer func() {
		if err != nil {
			err = errors.Join(err, d.Close())
		}
	}()
	// Hold a named lock for the fixture's lifetime, taken before any object
	// exists, so DropStale can tell live fixtures from ones left by killed
	// processes: the server releases the lock when its connection drops.
	if d.lock, err = d.admin.Conn(ctx); err != nil {
		return nil, fmt.Errorf("fixture lock: %w", sanitize(err))
	}
	var held sql.NullInt64
	if err := d.lock.QueryRowContext(ctx, "SELECT GET_LOCK(?, 0)", lockName(suffix)).Scan(&held); err != nil || held.Int64 != 1 {
		return nil, fmt.Errorf("fixture lock unavailable")
	}
	// The server closes idle sessions after wait_timeout, which would release
	// the lock while the fixture is still in use; keep the session active.
	keepaliveCtx, stop := context.WithCancel(context.WithoutCancel(ctx))
	d.stopKeepalive, d.keepaliveDone = stop, make(chan struct{})
	go d.keepalive(keepaliveCtx)
	// The comment marks the schema as this package's, so DropStale never
	// removes look-alike schemas created by anything else.
	if err := d.Exec(ctx, "CREATE DATABASE "+d.Name+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci COMMENT '"+fixtureMarker+"'"); err != nil {
		return nil, err
	}
	d.drops = append(d.drops, "DROP DATABASE "+d.Name)
	for _, user := range []string{d.RuntimeUser, d.MigrationUser} {
		if err := d.Exec(ctx, fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY '%s' REQUIRE SSL", user, d.Password)); err != nil {
			return nil, err
		}
		d.drops = append(d.drops, fmt.Sprintf("DROP USER '%s'@'%%'", user))
	}
	if err := d.Exec(ctx, fmt.Sprintf("GRANT SELECT ON %s.* TO '%s'@'%%'", d.Name, d.RuntimeUser)); err != nil {
		return nil, err
	}
	if err := d.Exec(ctx, fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, DROP, INDEX, REFERENCES ON %s.* TO '%s'@'%%'", d.Name, d.MigrationUser)); err != nil {
		return nil, err
	}
	return d, nil
}

func lockName(suffix string) string { return "ticketopia_fixture_" + suffix }

const fixtureMarker = "ticketopia persistencetest fixture"

// keepaliveInterval stays well below MariaDB's default 8-hour wait_timeout.
var keepaliveInterval = time.Minute

func (d *Database) keepalive(ctx context.Context) {
	defer close(d.keepaliveDone)
	ticker := time.NewTicker(keepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			ping, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = d.lock.PingContext(ping)
			cancel()
		}
	}
}

var fixtureSuffix = regexp.MustCompile(`^ticketopia_test_([0-9a-f]{16})$`)

// Cleanup reports what DropStale removed and which look-alike schemas it left
// because they lack this package's marker (created by older code or other
// tools); drop those manually if they are yours.
type Cleanup struct {
	Dropped, Unmarked []string
}

// DropStale drops marked fixture schemas, with their users, whose creating
// process is gone, identified by a released fixture lock. Live fixtures,
// including idle review servers, keep their lock and are skipped.
func DropStale(ctx context.Context, o Options) (Cleanup, error) {
	var c Cleanup
	d, err := adminOnly(ctx, o)
	if err != nil {
		return c, err
	}
	defer d.admin.Close()
	rows, err := d.admin.QueryContext(ctx, `SELECT schema_name, schema_comment FROM information_schema.schemata WHERE schema_name LIKE 'ticketopia\_test\_%' ORDER BY schema_name`)
	if err != nil {
		return c, fmt.Errorf("listing fixtures: %w", sanitize(err))
	}
	var suffixes []string
	for rows.Next() {
		var name, comment string
		if err := rows.Scan(&name, &comment); err != nil {
			rows.Close()
			return c, fmt.Errorf("listing fixtures: %w", sanitize(err))
		}
		if m := fixtureSuffix.FindStringSubmatch(name); m != nil && comment == fixtureMarker {
			suffixes = append(suffixes, m[1])
		} else {
			c.Unmarked = append(c.Unmarked, name)
		}
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return c, fmt.Errorf("listing fixtures: %w", sanitize(err))
	}
	for _, suffix := range suffixes {
		var holder sql.NullInt64
		if err := d.admin.QueryRowContext(ctx, "SELECT IS_USED_LOCK(?)", lockName(suffix)).Scan(&holder); err != nil {
			return c, fmt.Errorf("checking fixture lock: %w", sanitize(err))
		}
		if holder.Valid {
			continue
		}
		name := "ticketopia_test_" + suffix
		for _, q := range []string{"DROP DATABASE IF EXISTS " + name,
			fmt.Sprintf("DROP USER IF EXISTS 'runtime_%s'@'%%', 'migration_%s'@'%%'", suffix, suffix)} {
			if err := d.Exec(ctx, q); err != nil {
				return c, err
			}
		}
		c.Dropped = append(c.Dropped, name)
	}
	return c, nil
}

func adminOnly(ctx context.Context, o Options) (*Database, error) {
	if o.AdminUser == "" {
		o.AdminUser = "root"
	}
	if o.AdminPassword == "" {
		o.AdminPassword = "local-root-only"
	}
	host, port, err := net.SplitHostPort(o.Addr)
	if err != nil {
		return nil, fmt.Errorf("invalid MariaDB address %q", o.Addr)
	}
	admin, err := connect(ctx, host, port, o.CAFile, o.AdminUser, o.AdminPassword)
	if err != nil {
		return nil, err
	}
	return &Database{Host: host, Port: port, CAFile: o.CAFile, admin: admin}, nil
}

func connect(ctx context.Context, host, port, caFile, user, password string) (*sql.DB, error) {
	pem, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read MariaDB CA file %q", caFile)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("MariaDB CA file %q contains no certificates", caFile)
	}
	c := mysql.NewConfig()
	c.Net, c.Addr = "tcp", net.JoinHostPort(host, port)
	c.User, c.Passwd, c.DBName = user, password, "mysql"
	c.ParseTime, c.Loc = true, time.UTC
	c.Timeout, c.ReadTimeout, c.WriteTimeout = 3*time.Second, 5*time.Second, 5*time.Second
	c.TLS = &tls.Config{RootCAs: roots, ServerName: host, MinVersion: tls.VersionTLS12}
	connector, err := mysql.NewConnector(c)
	if err != nil {
		return nil, fmt.Errorf("MariaDB administrator configuration failed")
	}
	db := sql.OpenDB(connector)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("MariaDB administrator connect: %w", sanitize(err))
	}
	return db, nil
}

// sanitize keeps the server error number but never driver text or credentials.
func sanitize(err error) error {
	var server *mysql.MySQLError
	if errors.As(err, &server) {
		return fmt.Errorf("MariaDB code %d", server.Number)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return errors.New("database unavailable")
}

// Admin exposes the administrator pool for tests that seed through SQL.
func (d *Database) Admin() *sql.DB { return d.admin }

// Exec runs one administrator statement with a bounded timeout.
func (d *Database) Exec(ctx context.Context, query string, args ...any) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if _, err := d.admin.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("fixture SQL: %w", sanitize(err))
	}
	return nil
}

// GrantRuntime applies the runtime table grants once migrations have created
// the tables; MariaDB refuses table-level grants on missing tables.
func (d *Database) GrantRuntime(ctx context.Context) error {
	grants := []struct{ privileges, table string }{}
	add := func(privileges string, tables ...string) {
		for _, table := range tables {
			grants = append(grants, struct{ privileges, table string }{privileges, table})
		}
	}
	add("INSERT, UPDATE", "events", "event_providers", "event_reminders", "reminder_jobs", "reminder_dispatch",
		"accounts", "event_snapshots", "discussion_posts", "recommendation_feed_events", "recommendation_activity",
		"moderation_reports", "moderation_appeals", "artists", "artist_providers", "venues", "venue_providers",
		"event_history_state", "collection_tasks", "collection_runs", "provider_budgets", "discovery_scopes", "discovery_scope_events")
	add("INSERT, DELETE", "live_recommendation_activity", "account_credentials", "auth_flows", "oauth_codes", "oauth_refresh_history", "saved_events",
		"artist_follows", "venue_follows", "event_search_places", "event_search_facets")
	add("INSERT", "account_identities", "moderation_decisions", "discussion_notification_posts", "event_observations",
		"collection_pages", "collection_run_events")
	add("INSERT, UPDATE, DELETE", "auth_rate_limits", "oauth_grants", "event_interests", "event_recommendations", "post_helpful",
		"discussion_follows", "discussion_notifications", "event_detail_tasks")
	for _, g := range grants {
		if err := d.Exec(ctx, fmt.Sprintf("GRANT %s ON %s.%s TO '%s'@'%%'", g.privileges, d.Name, g.table, d.RuntimeUser)); err != nil {
			return err
		}
	}
	return nil
}

// Close drops the schema and users with an independent bounded context, then
// closes the administrator pool. It is safe to call more than once.
func (d *Database) Close() error {
	d.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var errs []error
		for _, q := range d.drops {
			if _, err := d.admin.ExecContext(ctx, q); err != nil {
				errs = append(errs, fmt.Errorf("fixture cleanup: %w", sanitize(err)))
			}
		}
		if d.stopKeepalive != nil {
			d.stopKeepalive()
			<-d.keepaliveDone
		}
		if d.lock != nil {
			errs = append(errs, d.lock.Close())
		}
		errs = append(errs, d.admin.Close())
		d.closeErr = errors.Join(errs...)
	})
	return d.closeErr
}
