package persistence

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

// Adding a table grant must also add a startup probe and a local setup check;
// these contracts run in ordinary CI, without a MariaDB server.
func TestRuntimePermissionChecksMatchProvisioning(t *testing.T) {
	grants, err := os.ReadFile("../../deploy/mariadb/runtime-grants.sql")
	if err != nil {
		t.Fatal(err)
	}
	grantPattern := regexp.MustCompile(`^GRANT (.+) ON ticketopia\.([a-z_][a-z0-9_]*) TO 'ticketopia_runtime'@'%';$`)
	required := map[string]bool{}
	for _, line := range strings.Split(string(grants), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "--") {
			continue
		}
		match := grantPattern.FindStringSubmatch(line)
		if match == nil {
			t.Fatalf("unrecognized runtime grant: %s", line)
		}
		for _, privilege := range strings.Split(match[1], ", ") {
			required[match[2]+"/"+privilege] = true
		}
	}
	script, err := os.ReadFile("../../deploy/mariadb/runtime-check.sql")
	if err != nil {
		t.Fatal(err)
	}
	localChecks := map[string]bool{}
	for _, line := range strings.Split(string(script), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "INSERT ") || strings.HasPrefix(line, "UPDATE ") || strings.HasPrefix(line, "DELETE ") {
			// Ignore formatting differences only; the zero-row SQL must match.
			localChecks[strings.Join(strings.Fields(strings.ReplaceAll(strings.TrimSuffix(line, ";"), "=", " = ")), " ")] = true
		}
	}
	for _, check := range runtimeWriteChecks() {
		key := check.table + "/" + check.operation
		if !required[key] {
			t.Errorf("startup probe has no matching runtime grant: %s", key)
		}
		delete(required, key)
		query := strings.Join(strings.Fields(strings.ReplaceAll(check.query, "=", " = ")), " ")
		if !localChecks[query] {
			t.Errorf("startup probe has no matching local setup check: %s", key)
		}
		delete(localChecks, query)
	}
	for key := range required {
		t.Errorf("runtime grant has no startup probe: %s", key)
	}
	for query := range localChecks {
		t.Errorf("local setup check has no startup probe: %s", query)
	}
}

func TestMariaDBStartupRequiresRuntimePermissions(t *testing.T) {
	f := newMaria(t)
	// A successful migration alone must not allow an underprivileged app to start.
	if err := Migrate(t.Context(), f.migration); err != nil {
		t.Fatal(err)
	}
	if p, err := Open(t.Context(), f.runtime); err == nil {
		p.Close()
		t.Fatal("schema-only migration allowed startup without runtime grants")
	} else if !strings.Contains(err.Error(), "runtime permission validation (INSERT events)") {
		t.Fatal("missing runtime grant was not identified", err)
	}
	f.migrate(t)
	p := f.open(t)
	for _, check := range runtimeWriteChecks() {
		t.Run(check.table+"/"+check.operation, func(t *testing.T) {
			execSQL(t, f.admin, "REVOKE "+check.operation+" ON "+f.runtime.Database+"."+check.table+" FROM '"+f.runtime.User+"'@'%'")
			opened, err := Open(t.Context(), f.runtime)
			execSQL(t, f.admin, "GRANT "+check.operation+" ON "+f.runtime.Database+"."+check.table+" TO '"+f.runtime.User+"'@'%'")
			if opened != nil {
				opened.Close()
				t.Fatal("startup accepted a missing runtime permission")
			}
			if !errors.Is(err, ErrUnavailable) || !strings.Contains(err.Error(), check.operation+" "+check.table) || !strings.Contains(err.Error(), "verify runtime grants") {
				t.Fatal("startup did not identify the missing permission and recovery", err)
			}
			for _, secret := range []string{f.runtime.User, f.runtime.Password, f.runtime.Database} {
				if strings.Contains(err.Error(), secret) {
					t.Fatal("startup permission error leaked connection details")
				}
			}
		})
	}
	f.open(t)
	for _, permission := range runtimeTablePermissions {
		var count int
		if err := p.db.QueryRowContext(t.Context(), "SELECT COUNT(*) FROM "+permission.table).Scan(&count); err != nil || count != 0 {
			t.Fatal("startup permission validation changed application data", permission.table, count, err)
		}
	}
}

func TestMariaDBRuntimePermissionValidationDeadline(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	conn, err := f.admin.Conn(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(t.Context(), "LOCK TABLE "+f.runtime.Database+".discovery_scopes WRITE"); err != nil {
		t.Fatal(err)
	}
	defer conn.ExecContext(context.Background(), "UNLOCK TABLES")
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := p.validateRuntimePermissions(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("permission validation did not respect the caller deadline", err)
	} else if strings.Contains(err.Error(), "verify runtime grants") {
		t.Fatal("permission validation misdiagnosed a deadline as a grant denial", err)
	}
	if time.Since(start) > time.Second {
		t.Fatal("permission validation exceeded its deadline")
	}
	if _, err := conn.ExecContext(t.Context(), "UNLOCK TABLES"); err != nil {
		t.Fatal(err)
	}
	if err := p.validateRuntimePermissions(t.Context()); err != nil {
		t.Fatal("permission validation did not recover after the lock", err)
	}
}
