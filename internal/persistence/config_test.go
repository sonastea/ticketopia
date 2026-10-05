package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
)

func envConfig(t *testing.T) Config {
	t.Helper()
	for key, value := range map[string]string{
		"PERSISTENCE_MODE": "mariadb", "DB_HOST": "localhost", "DB_NAME": "ticketopia",
		"DB_USER": "runtime", "DB_PASSWORD": "password-that-must-not-leak", "DB_TLS_MODE": "disabled",
		"DB_TLS_CA_FILE": "", "DB_MIGRATION_USER": "migrator", "DB_MIGRATION_PASSWORD": "migration-secret",
	} {
		t.Setenv(key, value)
	}
	c, err := ConfigFromEnv(false)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestExplicitModesAndCredentials(t *testing.T) {
	t.Setenv("PERSISTENCE_MODE", "disabled")
	c, err := ConfigFromEnv(false)
	if err != nil || c.Enabled {
		t.Fatalf("disabled: %v", err)
	}
	if _, err := ConfigFromEnv(true); err == nil {
		t.Fatal("migration accepted disabled persistence")
	}
	_ = envConfig(t)
	m, err := ConfigFromEnv(true)
	if err != nil || m.User != "migrator" || m.Password != "migration-secret" {
		t.Fatal("wrong migration credentials")
	}
	t.Setenv("DB_MIGRATION_USER", "runtime")
	if _, err := ConfigFromEnv(false); err == nil {
		t.Fatal("runtime accepted migration identity")
	}
	if _, err := ConfigFromEnv(true); err == nil {
		t.Fatal("migration accepted runtime identity")
	}
}

func TestInvalidConfigDoesNotEchoValues(t *testing.T) {
	for _, key := range []string{"PERSISTENCE_MODE", "DB_HOST", "DB_NAME", "DB_PORT", "DB_TLS_MODE", "DB_MAX_OPEN", "DB_MAX_IDLE", "DB_DIAL_TIMEOUT", "DB_QUERY_TIMEOUT"} {
		t.Run(key, func(t *testing.T) {
			envConfig(t)
			t.Setenv(key, "password-that-must-not-leak@tcp(host)/db")
			_, err := ConfigFromEnv(false)
			if err == nil || strings.Contains(err.Error(), "must-not-leak") {
				t.Fatalf("unsafe validation: %v", err)
			}
		})
	}
	for key, value := range map[string]string{"DB_MAX_OPEN": "0", "DB_MAX_IDLE": "11", "DB_READY_TIMEOUT": "3s", "DB_STARTUP_TIMEOUT": "0s", "DB_CONN_LIFETIME": "2h"} {
		t.Run(key+value, func(t *testing.T) {
			envConfig(t)
			t.Setenv(key, value)
			if _, err := ConfigFromEnv(false); err == nil {
				t.Fatal("unbounded setting accepted")
			}
		})
	}
}

func TestDriverUTCAndTLS(t *testing.T) {
	c := envConfig(t)
	d, err := c.driverConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !d.ParseTime || d.Loc != time.UTC || d.Params["time_zone"] != "'+00:00'" || d.MultiStatements || d.AllowFallbackToPlaintext {
		t.Fatal("unsafe driver defaults")
	}
	if d.Timeout <= 0 || d.ReadTimeout <= 0 || d.WriteTimeout <= 0 {
		t.Fatal("missing I/O limits")
	}
	if !strings.Contains(d.Params["sql_mode"], "STRICT_ALL_TABLES") {
		t.Fatal("missing strict session mode")
	}
	c.TLSMode, c.CAFile = "verify-full", filepath.Join(t.TempDir(), "ca.crt")
	if _, err := c.driverConfig(); err == nil {
		t.Fatal("missing CA accepted")
	}
	if err := os.WriteFile(c.CAFile, []byte("invalid secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := c.driverConfig(); err == nil || strings.Contains(err.Error(), "invalid secret") {
		t.Fatal("invalid CA accepted or leaked")
	}
	// Real CA and hostname verification are exercised by the MariaDB suite.
}

func TestCredentialRedaction(t *testing.T) {
	c := envConfig(t)
	data, _ := json.Marshal(c)
	for _, text := range []string{fmt.Sprintf("%v %+v %#v", c, c, c), string(data), safeError("connect", &mysql.MySQLError{Number: 1045, Message: c.Password + " runtime:secret@tcp(host)/db"}).Error()} {
		if strings.Contains(text, c.Password) || strings.Contains(text, "@tcp(") {
			t.Fatal("credentials leaked")
		}
	}
	err := safeError("query", context.DeadlineExceeded)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("lost context classification")
	}
}

func TestStartupDeadlineDuringHandshake(t *testing.T) {
	c := envConfig(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			var b [1]byte
			_, _ = conn.Read(b[:])
		}
	}()
	c.Host, c.Port, _ = net.SplitHostPort(listener.Addr().String())
	c.StartupTimeout = 50 * time.Millisecond
	start := time.Now()
	if _, err := Open(t.Context(), c); err == nil {
		t.Fatal("incomplete handshake accepted")
	}
	if time.Since(start) > time.Second {
		t.Fatal("startup was not bounded by its context")
	}
	<-done
}
