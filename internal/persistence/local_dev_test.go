package persistence

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Exercise the real Makefile/wrapper with external tools replaced by fixtures.
// No local or shared database is contacted by these workflow tests.
func TestLocalMigrateWorkflow(t *testing.T) {
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make is required for local workflow tests")
	}
	source, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	steps := []string{"tls", "database", "users", "migrations", "grants", "runtime-check"}
	for _, failure := range append([]string{""}, steps...) {
		name := failure
		if name == "" {
			name = "success"
		}
		t.Run(name, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "local project")
			write := func(name, content string, mode os.FileMode) {
				t.Helper()
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), mode); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{"Makefile", "scripts/mariadb-migrate.sh"} {
				data, err := os.ReadFile(filepath.Join(source, name))
				if err != nil {
					t.Fatal(err)
				}
				write(name, string(data), 0644)
			}
			write("scripts/mariadb-certs.sh", "#!/bin/sh\nprintf '%s\\n' tls >> \"$WORKFLOW_LOG\"\nif [ \"$FAIL_STEP\" = tls ]; then exit 9; fi\n", 0644)
			write("deploy/mariadb/compose.yaml", "fixture", 0644)
			write("deploy/mariadb/init.sql", "users", 0644)
			write("deploy/mariadb/runtime-grants.sql", "grants", 0644)
			write("deploy/mariadb/runtime-check.sql", "runtime-check", 0644)
			write("tools/openssl", "#!/bin/sh\nexit 0\n", 0755)
			write("tools/docker", `#!/bin/sh
set -eu
case "$*" in
  *"up -d --wait --wait-timeout 60 mariadb") step=database ;;
  *"exec -T mariadb sh -c"*) step=$(cat) ;;
  *"--host=localhost --protocol=tcp --user=ticketopia_runtime"*) step=$(cat) ;;
  *) echo "unexpected Docker invocation" >&2; exit 1 ;;
esac
printf '%s\n' "$step" >> "$WORKFLOW_LOG"
if [ "$FAIL_STEP" = "$step" ]; then exit 9; fi
`, 0755)
			write("tools/go", `#!/bin/sh
set -eu
test "$*" = 'run ./cmd/ticketopia migrate'
test "$PERSISTENCE_MODE" = mariadb
test "$DB_HOST" = localhost
test "$DB_PORT" = 3307
test "$DB_NAME" = ticketopia
test "$DB_USER" = ticketopia_runtime
test "$DB_PASSWORD" = local-runtime-only
test "$DB_MIGRATION_USER" = ticketopia_migration
test "$DB_MIGRATION_PASSWORD" = local-migration-only
test "$DB_TLS_MODE" = verify-full
test "$DB_TLS_CA_FILE" = "$PWD/.local/mariadb/certs/ca.crt"
printf '%s\n' migrations >> "$WORKFLOW_LOG"
if [ "$FAIL_STEP" = migrations ]; then exit 9; fi
`, 0755)
			log := filepath.Join(root, "workflow.log")
			command := exec.Command("make", "migrate")
			command.Dir = root
			command.Env = append(os.Environ(),
				"PATH="+filepath.Join(root, "tools")+string(os.PathListSeparator)+os.Getenv("PATH"),
				"WORKFLOW_LOG="+log, "FAIL_STEP="+failure,
				"PERSISTENCE_MODE=disabled", "DB_HOST=production.invalid", "DB_PORT=3306", "DB_NAME=production",
				"DB_USER=production", "DB_PASSWORD=production-password-canary",
				"DB_MIGRATION_USER=production", "DB_MIGRATION_PASSWORD=production-password-canary",
				"DB_TLS_MODE=disabled", "DB_TLS_CA_FILE=/production/ca.crt")
			output, err := command.CombinedOutput()
			if (err != nil) != (failure != "") {
				t.Fatalf("workflow failure=%q: %v\n%s", failure, err, output)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			want := steps
			for i, step := range steps {
				if step == failure {
					want = steps[:i+1]
				}
			}
			if string(data) != strings.Join(want, "\n")+"\n" {
				t.Fatalf("wrong order or workflow continued after failure: %q", data)
			}
			if strings.Contains(string(output), "production-password-canary") {
				t.Fatal("inherited credentials appeared in output")
			}
			if strings.Contains(string(output), "Local migrations and runtime permissions are ready.") != (failure == "") {
				t.Fatal("workflow reported success without completing verification")
			}
		})
	}
}

func TestMariaDBLocalRuntimeCheckRequiresGrants(t *testing.T) {
	f := newMaria(t)
	f.migrate(t)
	p := f.open(t)
	script, err := os.ReadFile("../../deploy/mariadb/runtime-check.sql")
	if err != nil {
		t.Fatal(err)
	}
	lines := []string{}
	for _, line := range strings.Split(string(script), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			lines = append(lines, line)
		}
	}
	statements := strings.Split(strings.Join(lines, "\n"), ";")
	check := func() error {
		tx, err := p.db.BeginTx(t.Context(), nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		// The first and last statements are the script's transaction boundaries;
		// use database/sql's transaction so failures always roll back here too.
		for _, statement := range statements[1:] {
			statement = strings.TrimSpace(statement)
			if statement == "" || statement == "ROLLBACK" {
				continue
			}
			if _, err := tx.ExecContext(t.Context(), statement); err != nil {
				return err
			}
		}
		return nil
	}
	if err := check(); err != nil {
		t.Fatal("runtime check failed with required grants", err)
	}
	execSQL(t, f.admin, "REVOKE INSERT, UPDATE ON "+f.runtime.Database+".event_snapshots FROM '"+f.runtime.User+"'@'%'")
	if err := check(); err == nil {
		t.Fatal("runtime check missed the permissions that caused Save to fail")
	}
	execSQL(t, f.admin, "GRANT INSERT, UPDATE ON "+f.runtime.Database+".event_snapshots TO '"+f.runtime.User+"'@'%'")
	if err := check(); err != nil {
		t.Fatal("runtime check did not recover after grants", err)
	}
	var count int
	if err := p.db.QueryRowContext(t.Context(), `SELECT (SELECT COUNT(*) FROM events)+(SELECT COUNT(*) FROM accounts)+(SELECT COUNT(*) FROM event_snapshots)+(SELECT COUNT(*) FROM saved_events)`).Scan(&count); err != nil || count != 0 {
		t.Fatal("runtime check changed domain data", count, err)
	}
}
