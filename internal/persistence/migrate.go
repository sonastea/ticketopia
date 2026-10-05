package persistence

import (
	"context"
	"database/sql/driver"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"time"

	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Migrate is an explicit deployment operation, never an application startup hook.
func Migrate(ctx context.Context, c Config) error {
	source, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return fmt.Errorf("embedded migrations unavailable")
	}
	return migrateFS(ctx, c, source)
}

func migrateFS(ctx context.Context, c Config, source fs.FS) error {
	if err := c.validate(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, c.MigrationTimeout)
	defer cancel()
	// One connection holds the advisory lock, one is used by Goose. Never share
	// this pool with runtime traffic or expose DDL credentials to app pods.
	c.MaxOpen, c.MaxIdle = 2, 2
	db, err := connect(ctx, c)
	if err != nil {
		return err
	}
	defer db.Close()
	conn, err := db.Conn(ctx)
	if err != nil {
		return safeError("migration connection", err)
	}
	defer conn.Close()
	// GET_LOCK is connection-scoped and survives MariaDB's implicit DDL commits.
	var acquired int
	lockName := "ticketopia:migrate:" + c.Database
	for acquired != 1 {
		// Poll instead of exceeding the socket read timeout inside GET_LOCK.
		if err := conn.QueryRowContext(ctx, `SELECT GET_LOCK(?, 0)`, lockName).Scan(&acquired); err != nil {
			return safeError("migration lock", err)
		}
		if acquired == 1 {
			break
		}
		timer := time.NewTimer(50 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return safeError("migration lock", ctx.Err())
		case <-timer.C:
		}
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if _, err := conn.ExecContext(cleanup, `SELECT RELEASE_LOCK(?)`, lockName); err != nil {
			// sql.Conn.Close alone returns a session to the pool; discard it on
			// failed unlock so a lock cannot remain on a reusable connection.
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
	}()
	if _, err := conn.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_state (id TINYINT NOT NULL PRIMARY KEY, dirty BOOLEAN NOT NULL, target_version BIGINT NOT NULL, CHECK (id = 1)) ENGINE=InnoDB`); err != nil {
		return safeError("migration state", err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT IGNORE INTO schema_state (id, dirty, target_version) VALUES (1, FALSE, 0)`); err != nil {
		return safeError("migration state", err)
	}
	var dirty bool
	if err := conn.QueryRowContext(ctx, `SELECT dirty FROM schema_state WHERE id = 1`).Scan(&dirty); err != nil {
		return safeError("migration state", err)
	}
	if dirty {
		return fmt.Errorf("database schema is dirty; inspect and repair before retrying migrations")
	}
	provider, err := goose.NewProvider(goose.DialectMySQL, db, source,
		goose.WithDisableGlobalRegistry(true), goose.WithSlog(slog.New(slog.NewTextHandler(io.Discard, nil))))
	if err != nil {
		return fmt.Errorf("embedded migration configuration failed")
	}
	current, target, err := provider.GetVersions(ctx)
	if err != nil {
		return safeError("migration version", err)
	}
	if current > target {
		return fmt.Errorf("database schema is newer than this migration executable")
	}
	if current == target {
		return nil
	}
	// Goose does not track failed nontransactional DDL. Commit this guard before
	// any migration; leave it set on failure, timeout, or process interruption.
	if _, err := conn.ExecContext(ctx, `UPDATE schema_state SET dirty = TRUE, target_version = ? WHERE id = 1`, target); err != nil {
		return safeError("migration state", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return safeError("migration (schema left dirty; inspect before recovery)", err)
	}
	if _, err := conn.ExecContext(ctx, `UPDATE schema_state SET dirty = FALSE WHERE id = 1`); err != nil {
		return safeError("migration completion (schema left dirty)", err)
	}
	return nil
}
