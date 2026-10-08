package persistence

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/rs/zerolog"
	"github.com/sonastea/ticketopia/internal/api"
	"github.com/sonastea/ticketopia/internal/events"
	"github.com/sonastea/ticketopia/internal/kv"
	"github.com/sonastea/ticketopia/internal/models"
)

// Opt-in real MariaDB suite. Each test owns and removes its random schema/users;
// never point these admin credentials at a shared or production database.
type mariaFixture struct {
	admin              *sql.DB
	migration, runtime Config
}

func newMaria(t *testing.T) mariaFixture {
	t.Helper()
	address := os.Getenv("MARIADB_TEST_ADDR")
	if address == "" {
		t.Skip("set MARIADB_TEST_ADDR to run real MariaDB tests")
	}
	c := envConfig(t)
	var err error
	c.Host, c.Port, err = net.SplitHostPort(address)
	if err != nil {
		t.Fatal("invalid MARIADB_TEST_ADDR")
	}
	c.TLSMode = "verify-full"
	c.CAFile = os.Getenv("MARIADB_TEST_CA")
	if c.CAFile == "" {
		c.CAFile, err = filepath.Abs("../../.local/mariadb/certs/ca.crt")
		if err != nil {
			t.Fatal(err)
		}
	}
	c.Database, c.User, c.Password = "mysql", "root", "local-root-only"
	if v := os.Getenv("MARIADB_TEST_ADMIN_USER"); v != "" {
		c.User = v
	}
	if v := os.Getenv("MARIADB_TEST_ADMIN_PASSWORD"); v != "" {
		c.Password = v
	}
	admin, err := connect(t.Context(), c)
	if err != nil {
		t.Fatal(err)
	}
	var entropy [8]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		t.Fatal(err)
	}
	suffix := hex.EncodeToString(entropy[:])
	name, runtimeUser, migrationUser := "ticketopia_test_"+suffix, "runtime_"+suffix, "migration_"+suffix
	execSQL(t, admin, "CREATE DATABASE "+name+" CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci")
	for _, user := range []string{runtimeUser, migrationUser} {
		execSQL(t, admin, fmt.Sprintf("CREATE USER '%s'@'%%' IDENTIFIED BY 'fixture-%s' REQUIRE SSL", user, suffix))
	}
	execSQL(t, admin, fmt.Sprintf("GRANT SELECT ON %s.* TO '%s'@'%%'", name, runtimeUser))
	execSQL(t, admin, fmt.Sprintf("GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, DROP, INDEX, REFERENCES ON %s.* TO '%s'@'%%'", name, migrationUser))
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, q := range []string{"DROP DATABASE " + name, fmt.Sprintf("DROP USER '%s'@'%%', '%s'@'%%'", runtimeUser, migrationUser)} {
			if _, err := admin.ExecContext(ctx, q); err != nil {
				t.Error(safeError("fixture cleanup", err))
			}
		}
		admin.Close()
	})
	c.Database, c.User, c.Password = name, runtimeUser, "fixture-"+suffix
	m := c
	m.User = migrationUser
	return mariaFixture{admin: admin, migration: m, runtime: c}
}

func execSQL(t *testing.T, db *sql.DB, query string, args ...any) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, query, args...); err != nil {
		t.Fatal(safeError("test SQL", err))
	}
}

func (f mariaFixture) migrate(t *testing.T) {
	t.Helper()
	if err := Migrate(t.Context(), f.migration); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"events", "event_providers"} {
		execSQL(t, f.admin, fmt.Sprintf("GRANT INSERT, UPDATE ON %s.%s TO '%s'@'%%'", f.runtime.Database, table, f.runtime.User))
	}
	for table, privileges := range map[string]string{"accounts": "INSERT, UPDATE", "account_identities": "INSERT", "account_credentials": "INSERT, DELETE", "auth_flows": "INSERT, DELETE", "auth_rate_limits": "INSERT, UPDATE, DELETE", "event_snapshots": "INSERT, UPDATE", "saved_events": "INSERT, DELETE", "event_interests": "INSERT, UPDATE, DELETE", "event_recommendations": "INSERT, UPDATE, DELETE", "discussion_posts": "INSERT, UPDATE", "post_helpful": "INSERT, UPDATE, DELETE", "recommendation_feed_events": "INSERT, UPDATE", "recommendation_activity": "INSERT, UPDATE"} {
		execSQL(t, f.admin, fmt.Sprintf("GRANT %s ON %s.%s TO '%s'@'%%'", privileges, f.runtime.Database, table, f.runtime.User))
	}
}

func (f mariaFixture) open(t *testing.T) *Pool {
	t.Helper()
	p, err := Open(t.Context(), f.runtime)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := p.Close(); err != nil {
			t.Error(err)
		}
	})
	return p
}

func TestMariaDBConcurrentMigrations(t *testing.T) {
	f := newMaria(t)
	// All invocations open independent pools, including version-table creation.
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { errs <- Migrate(t.Context(), f.migration) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	p := f.open(t)
	if err := p.Ready(t.Context()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := p.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM goose_db_version WHERE version_id=1`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate migration history: %d %v", count, safeError("version read", err))
	}
	// A held lock must prevent a second command from changing schema, and honor
	// its context deadline instead of waiting for an unbounded database timeout.
	conn, err := f.admin.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	lock := "ticketopia:migrate:" + f.runtime.Database
	var acquired int
	if err := conn.QueryRowContext(t.Context(), `SELECT GET_LOCK(?, 0)`, lock).Scan(&acquired); err != nil || acquired != 1 {
		t.Fatal("could not hold test migration lock")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := Migrate(ctx, f.migration); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("held lock: %v", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("lock wait not bounded")
	}
	if _, err := conn.ExecContext(t.Context(), `SELECT RELEASE_LOCK(?)`, lock); err != nil {
		t.Fatal(err)
	}
	if err := Migrate(t.Context(), f.migration); err != nil {
		t.Fatal(err)
	}
}

func TestMariaDBFailedDDLAndRecovery(t *testing.T) {
	f := newMaria(t)
	broken := fstest.MapFS{"00001_broken.sql": {Data: []byte("-- +goose NO TRANSACTION\n-- +goose Up\nCREATE TABLE partial_ddl (id INT PRIMARY KEY) ENGINE=InnoDB;\nSELECT missing_migration_function();\n-- +goose Down\nDROP TABLE partial_ddl;\n")}}
	if err := migrateFS(t.Context(), f.migration, broken); err == nil {
		t.Fatal("broken migration succeeded")
	}
	var count int
	if err := f.admin.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=? AND table_name='partial_ddl'`, f.runtime.Database).Scan(&count); err != nil || count != 1 {
		t.Fatal("DDL was incorrectly assumed to roll back")
	}
	if err := Migrate(t.Context(), f.migration); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty retry: %v", err)
	}
	if _, err := Open(t.Context(), f.runtime); err == nil || !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("dirty startup: %v", err)
	}
	// Recovery for this disposable fixture: remove the known partial object and
	// clear the guard under the same advisory lock. Production requires inspection.
	conn, err := f.admin.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	lock := "ticketopia:migrate:" + f.runtime.Database
	var acquired int
	if err := conn.QueryRowContext(t.Context(), `SELECT GET_LOCK(?, 0)`, lock).Scan(&acquired); err != nil || acquired != 1 {
		t.Fatal("recovery lock failed")
	}
	for _, q := range []string{"DROP TABLE " + f.runtime.Database + ".partial_ddl", "UPDATE " + f.runtime.Database + ".schema_state SET dirty=FALSE WHERE id=1"} {
		if _, err := conn.ExecContext(t.Context(), q); err != nil {
			t.Fatal(safeError("recovery", err))
		}
	}
	if _, err := conn.ExecContext(t.Context(), `SELECT RELEASE_LOCK(?)`, lock); err != nil {
		t.Fatal(err)
	}
	f.migrate(t)
	f.open(t)
}

func TestMariaDBRuntimePrivilegesAndStartupFailures(t *testing.T) {
	f := newMaria(t)
	if _, err := Open(t.Context(), f.runtime); err == nil {
		t.Fatal("unmigrated startup succeeded")
	}
	f.migrate(t)
	p := f.open(t)
	for _, query := range []string{`CREATE TABLE forbidden (id INT)`, `ALTER TABLE events ADD forbidden INT`, `DROP TABLE event_providers`, `UPDATE schema_state SET dirty=TRUE WHERE id=1`} {
		if _, err := p.db.ExecContext(t.Context(), query); err == nil {
			t.Fatalf("runtime allowed: %s", query)
		}
	}
	if err := Migrate(t.Context(), f.runtime); err == nil {
		t.Fatal("runtime credentials migrated schema")
	}
	var timezone, mode, charset string
	if err := p.db.QueryRowContext(t.Context(), `SELECT @@session.time_zone, @@session.sql_mode, @@character_set_connection`).Scan(&timezone, &mode, &charset); err != nil {
		t.Fatal(err)
	}
	if timezone != "+00:00" || !strings.Contains(mode, "STRICT_ALL_TABLES") || charset != "utf8mb4" {
		t.Fatal("wrong session settings")
	}
	bad := f.runtime
	bad.Password = "runtime-secret@tcp(host)/schema"
	start := time.Now()
	if _, err := Open(t.Context(), bad); err == nil || strings.Contains(err.Error(), bad.Password) || strings.Contains(err.Error(), "@tcp(") {
		t.Fatal("credential failure was not redacted")
	}
	if time.Since(start) > bad.StartupTimeout+time.Second {
		t.Fatal("startup failure was not bounded")
	}
	var versionRow int
	if err := p.db.QueryRowContext(t.Context(), `SELECT id FROM goose_db_version WHERE version_id=?`, SupportedSchemaVersion).Scan(&versionRow); err != nil {
		t.Fatal(err)
	}
	for _, version := range []int{0, SupportedSchemaVersion - 1, SupportedSchemaVersion + 1} {
		execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".goose_db_version SET version_id=? WHERE id=?", version, versionRow)
		if _, err := Open(t.Context(), f.runtime); err == nil {
			t.Fatal("unsupported schema version accepted")
		}
		execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".goose_db_version SET version_id=? WHERE id=?", SupportedSchemaVersion, versionRow)
	}
	// A clean marker cannot hide a missing table (for example a botched repair).
	execSQL(t, f.admin, "RENAME TABLE "+f.runtime.Database+".event_providers TO "+f.runtime.Database+".missing_mapping")
	if _, err := Open(t.Context(), f.runtime); err == nil {
		t.Fatal("partial schema accepted")
	}
	execSQL(t, f.admin, "RENAME TABLE "+f.runtime.Database+".missing_mapping TO "+f.runtime.Database+".event_providers")
}

func TestMariaDBSchemaValidationDeadline(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	conn, err := f.admin.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), "LOCK TABLE "+f.runtime.Database+".schema_state WRITE"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "UNLOCK TABLES")
	c := f.runtime
	c.StartupTimeout = 100 * time.Millisecond
	start := time.Now()
	if _, err := Open(t.Context(), c); err == nil {
		t.Fatal("locked schema accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("schema validation deadline exceeded")
	}
	if _, err := conn.ExecContext(t.Context(), "UNLOCK TABLES"); err != nil {
		t.Fatal(err)
	}
	f.open(t)
}

func sampleEvent(source string) models.Event {
	instant := time.Date(2026, 10, 6, 20, 0, 0, 123456000, time.FixedZone("local", 7200))
	date, clock, zone := "2026-10-06", "20:00:00", "Europe/Berlin"
	return models.Event{ID: "ticketmaster:" + source, Name: "Sport without artists 🏟", Source: models.Source{Provider: "ticketmaster", ID: source, URL: "https://tickets.example/event"},
		Start: models.EventStart{DateTime: &instant, LocalDate: &date, LocalTime: &clock, Timezone: &zone}, Status: "onsale",
		Venues:          []models.Venue{{ID: "ticketmaster:venue", Place: models.Place{Name: "Arena", City: "Berlin"}}},
		Classifications: []models.Classification{{Primary: true, Segment: &models.NamedID{ID: "sports", Name: "Sports"}, Genre: &models.NamedID{ID: "ball", Name: "Ball sports"}}}}
}

func TestMariaDBConcurrentIdentityAcrossPools(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	pools := []*Pool{f.open(t), f.open(t), f.open(t), f.open(t)}
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for worker := range 8 {
		wg.Go(func() {
			for iteration := range 15 {
				e := sampleEvent("Source_ID")
				e.Name = fmt.Sprintf("worker %d iteration %d", worker, iteration)
				if _, err := events.New(pools[worker%len(pools)].Events()).EnsureDurable(t.Context(), e); err != nil {
					errs <- err
					return
				}
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	r := pools[0].Events()
	for _, e := range []models.Event{sampleEvent("source_id"), sampleEvent("Source_ID")} {
		if _, err := r.Upsert(t.Context(), e); err != nil {
			t.Fatal(err)
		}
	}
	otherProvider := sampleEvent("Source_ID")
	otherProvider.Source.Provider = "Ticketmaster"
	otherProvider.ID = "Ticketmaster:Source_ID"
	if _, err := r.Upsert(t.Context(), otherProvider); err != nil {
		t.Fatal(err)
	}
	var eventCount, mappingCount int
	if err := pools[0].db.QueryRowContext(t.Context(), `SELECT (SELECT COUNT(*) FROM events), (SELECT COUNT(*) FROM event_providers)`).Scan(&eventCount, &mappingCount); err != nil || eventCount != 3 || mappingCount != 3 {
		t.Fatalf("identity counts: %d %d", eventCount, mappingCount)
	}
	changed := sampleEvent("Source_ID")
	changed.Name, changed.Status = "Renamed event", "cancelled"
	changed.Start.DateTime = nil
	changed.Start.DateTBD = true
	changed.Venues[0].Name = "New venue"
	if _, err := r.Upsert(t.Context(), changed); err != nil {
		t.Fatal(err)
	}
	got, err := pools[3].Events().GetByProvider(t.Context(), "ticketmaster", "Source_ID")
	if err != nil || got.ID != changed.ID || got.Name != changed.Name || got.Status != "cancelled" || got.Start.DateTime != nil || !got.Start.DateTBD || got.Venues[0].Name != "New venue" || len(got.Artists) != 0 || got.Classifications[0].Segment.ID != "sports" {
		t.Fatalf("metadata update: %v %+v", err, got)
	}
	lower, err := r.Get(t.Context(), "ticketmaster:source_id")
	if err != nil || lower.Name == got.Name || lower.Status != "onsale" || lower.Start.DateTime.Location() != time.UTC || !lower.Start.DateTime.Equal(*sampleEvent("source_id").Start.DateTime) {
		t.Fatal("case identity or UTC instant lost")
	}
}

func TestMariaDBAtomicIdentityAndCacheIndependentRestart(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	service := events.New(p.Events())
	e := sampleEvent("retained")
	if _, err := service.EnsureDurable(t.Context(), e); err != nil {
		t.Fatal(err)
	}
	cache := kv.NewMemory()
	if err := cache.Set(t.Context(), "ticketmaster:v1:event:"+e.ID, []byte("replaceable"), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	cache.Close() // Clears all entries. SQL has no cache eviction/deletion hook.
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	restarted := f.open(t)
	got, err := events.New(restarted.Events()).Get(t.Context(), e.ID)
	if err != nil || got.ID != e.ID || got.Name != e.Name || got.Status != "onsale" {
		t.Fatalf("restart/cache loss: %v", err)
	}
	if _, err := restarted.Events().Get(t.Context(), "ticketmaster:unknown"); !errors.Is(err, events.ErrNotFound) {
		t.Fatal(err)
	}
	// Corrupt a mapping as admin to prove the repository never reassigns it and
	// an unsuccessful mapping operation rolls back the event metadata as well.
	other := sampleEvent("other")
	if _, err := restarted.Events().Upsert(t.Context(), other); err != nil {
		t.Fatal(err)
	}
	execSQL(t, f.admin, "UPDATE "+f.runtime.Database+".event_providers SET event_id=? WHERE source_id=?", other.ID, e.Source.ID)
	e.Name = "must roll back"
	if _, err := restarted.Events().Upsert(t.Context(), e); err == nil {
		t.Fatal("mapping moved")
	}
	var storedName string
	if err := restarted.db.QueryRowContext(t.Context(), `SELECT name FROM events WHERE event_id=?`, e.ID).Scan(&storedName); err != nil || storedName == e.Name {
		t.Fatal("partial event update committed")
	}
}

func TestMariaDBReadinessRecoveryAndTLS(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	cache := kv.NewMemory()
	defer cache.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a, err := api.NewAPI(ctx, zerolog.Nop(), cache, api.WithPersistence(p.Ready, events.New(p.Events())))
	if err != nil {
		t.Fatal(err)
	}
	routes := a.Routes()
	probe := func(path string, want int) {
		t.Helper()
		start := time.Now()
		rec := httptest.NewRecorder()
		routes.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != want || strings.Contains(rec.Body.String(), "fixture-") {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		if time.Since(start) > time.Second {
			t.Fatal("readiness was not bounded")
		}
	}
	probe("/readyz", 200)
	execSQL(t, f.admin, fmt.Sprintf("ALTER USER '%s'@'%%' ACCOUNT LOCK", f.runtime.User))
	rows, err := f.admin.QueryContext(t.Context(), `SELECT id FROM information_schema.processlist WHERE user=?`, f.runtime.User)
	if err != nil {
		t.Fatal(err)
	}
	var connections []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		connections = append(connections, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	for _, id := range connections {
		execSQL(t, f.admin, fmt.Sprintf("KILL %d", id))
	}
	probe("/readyz", 503)
	probe("/healthz", 200)
	execSQL(t, f.admin, fmt.Sprintf("ALTER USER '%s'@'%%' ACCOUNT UNLOCK", f.runtime.User))
	probe("/readyz", 200)
	cancel()
	probe("/readyz", 503)
	probe("/healthz", 200)
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	if err := p.Ready(t.Context()); err == nil {
		t.Fatal("closed pool was ready")
	}
	// Verify TLS is active, and that neither invalid trust nor hostname mismatch
	// can silently downgrade to plaintext.
	q := f.open(t)
	var key, cipher string
	if err := q.db.QueryRowContext(t.Context(), `SHOW SESSION STATUS LIKE 'Ssl_cipher'`).Scan(&key, &cipher); err != nil || cipher == "" {
		t.Fatal("connection is not encrypted")
	}
	bad := f.runtime
	bad.CAFile = filepath.Join(t.TempDir(), "other-ca.pem")
	otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	otherCA := &x509.Certificate{SerialNumber: big.NewInt(1), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, otherCA, otherCA, &otherKey.PublicKey, otherKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bad.CAFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(t.Context(), bad); err == nil {
		t.Fatal("invalid trust accepted")
	}
	d, err := f.runtime.driverConfig()
	if err != nil {
		t.Fatal(err)
	}
	d.TLS.ServerName = "wrong-database.example"
	connector, err := mysql.NewConnector(d)
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	defer db.Close()
	deadline, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	if err := db.PingContext(deadline); err == nil {
		t.Fatal("hostname verification bypassed")
	}
}
