// Package persistence owns MariaDB connections, migrations, and repositories.
// Driver errors never escape this boundary: server messages can contain secrets.
package persistence

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
)

type Config struct {
	Enabled                                                      bool
	Host, Port, Database, User                                   string
	Password                                                     string `json:"-"`
	TLSMode, CAFile                                              string
	MaxOpen, MaxIdle                                             int
	Lifetime, IdleTime, DialTimeout, ReadTimeout, WriteTimeout   time.Duration
	StartupTimeout, QueryTimeout, ReadyTimeout, MigrationTimeout time.Duration
}

func (Config) String() string     { return "MariaDB configuration (credentials redacted)" }
func (c Config) GoString() string { return c.String() }

// ConfigFromEnv deliberately accepts no DSN or arbitrary driver parameters.
// Migration credentials are never read by the application's runtime path.
func ConfigFromEnv(migration bool) (Config, error) {
	c := Config{Port: "3306", TLSMode: "verify-full", MaxOpen: 10, MaxIdle: 2,
		Lifetime: 5 * time.Minute, IdleTime: time.Minute, DialTimeout: 3 * time.Second,
		ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
		StartupTimeout: 10 * time.Second, QueryTimeout: 3 * time.Second,
		ReadyTimeout: 500 * time.Millisecond, MigrationTimeout: 2 * time.Minute}
	mode := os.Getenv("PERSISTENCE_MODE")
	switch mode {
	case "", "disabled":
		if migration {
			return c, fmt.Errorf("migrations require PERSISTENCE_MODE=mariadb")
		}
		return c, nil
	case "mariadb":
		c.Enabled = true
	default:
		return c, fmt.Errorf("PERSISTENCE_MODE must be disabled or mariadb")
	}
	c.Host, c.Database, c.CAFile = os.Getenv("DB_HOST"), os.Getenv("DB_NAME"), os.Getenv("DB_TLS_CA_FILE")
	prefix := "DB_"
	if migration {
		prefix = "DB_MIGRATION_"
	}
	c.User, c.Password = os.Getenv(prefix+"USER"), os.Getenv(prefix+"PASSWORD")
	if other := os.Getenv("DB_MIGRATION_USER"); !migration && other != "" && c.User == other {
		return c, fmt.Errorf("runtime and migration users must be separate")
	}
	if migration && c.User == os.Getenv("DB_USER") && c.User != "" {
		return c, fmt.Errorf("runtime and migration users must be separate")
	}
	if v := os.Getenv("DB_PORT"); v != "" {
		c.Port = v
	}
	if v := os.Getenv("DB_TLS_MODE"); v != "" {
		c.TLSMode = v
	}
	for _, setting := range []struct {
		name   string
		target *int
	}{
		{"DB_MAX_OPEN", &c.MaxOpen}, {"DB_MAX_IDLE", &c.MaxIdle},
	} {
		if v := os.Getenv(setting.name); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return c, fmt.Errorf("%s must be an integer", setting.name)
			}
			*setting.target = n
		}
	}
	for _, setting := range []struct {
		name   string
		target *time.Duration
	}{
		{"DB_CONN_LIFETIME", &c.Lifetime}, {"DB_CONN_IDLE_TIME", &c.IdleTime},
		{"DB_DIAL_TIMEOUT", &c.DialTimeout}, {"DB_READ_TIMEOUT", &c.ReadTimeout},
		{"DB_WRITE_TIMEOUT", &c.WriteTimeout}, {"DB_STARTUP_TIMEOUT", &c.StartupTimeout},
		{"DB_QUERY_TIMEOUT", &c.QueryTimeout}, {"DB_READY_TIMEOUT", &c.ReadyTimeout},
		{"DB_MIGRATION_TIMEOUT", &c.MigrationTimeout},
	} {
		if v := os.Getenv(setting.name); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				return c, fmt.Errorf("%s must be a duration", setting.name)
			}
			*setting.target = d
		}
	}
	return c, c.validate()
}

var databaseName = regexp.MustCompile(`^[a-zA-Z0-9_]{1,64}$`)

func (c Config) validate() error {
	if !c.Enabled {
		return fmt.Errorf("persistence is disabled")
	}
	if c.Host == "" || strings.ContainsAny(c.Host, " /@?#\t\r\n") {
		return fmt.Errorf("DB_HOST must be a database hostname or IP address")
	}
	port, err := strconv.Atoi(c.Port)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("DB_PORT must be between 1 and 65535")
	}
	if !databaseName.MatchString(c.Database) {
		return fmt.Errorf("DB_NAME must contain 1–64 ASCII letters, digits, or underscores")
	}
	if c.User == "" || c.Password == "" {
		return fmt.Errorf("database user and password are required")
	}
	if c.TLSMode != "verify-full" && c.TLSMode != "disabled" {
		return fmt.Errorf("DB_TLS_MODE must be verify-full or disabled (local development only)")
	}
	if c.TLSMode == "verify-full" && c.CAFile == "" {
		return fmt.Errorf("DB_TLS_CA_FILE is required for verified TLS")
	}
	if c.TLSMode == "disabled" && c.CAFile != "" {
		return fmt.Errorf("DB_TLS_CA_FILE requires DB_TLS_MODE=verify-full")
	}
	if c.MaxOpen < 1 || c.MaxOpen > 100 || c.MaxIdle < 0 || c.MaxIdle > c.MaxOpen {
		return fmt.Errorf("DB_MAX_OPEN must be 1–100 and DB_MAX_IDLE must be 0–DB_MAX_OPEN")
	}
	for _, setting := range []struct {
		name       string
		value, max time.Duration
	}{
		{"DB_CONN_LIFETIME", c.Lifetime, time.Hour}, {"DB_CONN_IDLE_TIME", c.IdleTime, time.Hour},
		{"DB_DIAL_TIMEOUT", c.DialTimeout, 30 * time.Second}, {"DB_READ_TIMEOUT", c.ReadTimeout, 30 * time.Second},
		{"DB_WRITE_TIMEOUT", c.WriteTimeout, 30 * time.Second}, {"DB_STARTUP_TIMEOUT", c.StartupTimeout, time.Minute},
		{"DB_QUERY_TIMEOUT", c.QueryTimeout, 30 * time.Second}, {"DB_READY_TIMEOUT", c.ReadyTimeout, 2 * time.Second},
		{"DB_MIGRATION_TIMEOUT", c.MigrationTimeout, 10 * time.Minute},
	} {
		if setting.value < time.Millisecond || setting.value > setting.max {
			return fmt.Errorf("%s must be between 1ms and %s", setting.name, setting.max)
		}
	}
	return nil
}

func (c Config) driverConfig() (*mysql.Config, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	d := mysql.NewConfig()
	d.Net, d.Addr = "tcp", net.JoinHostPort(c.Host, c.Port)
	d.User, d.Passwd, d.DBName = c.User, c.Password, c.Database
	d.ParseTime, d.Loc = true, time.UTC
	d.Timeout, d.ReadTimeout, d.WriteTimeout = c.DialTimeout, c.ReadTimeout, c.WriteTimeout
	d.RejectReadOnly = true
	// Silence the driver's unstructured diagnostics; callers emit sanitized errors.
	d.Logger = discardDriverLog{}
	d.Params = map[string]string{
		"time_zone":              "'+00:00'", // loc=UTC above does NOT set this session variable.
		"sql_mode":               "'STRICT_ALL_TABLES,ERROR_FOR_DIVISION_BY_ZERO,NO_ZERO_DATE,NO_ZERO_IN_DATE,NO_ENGINE_SUBSTITUTION'",
		"default_storage_engine": "'InnoDB'",
	}
	if err := d.Apply(mysql.Charset("utf8mb4", "utf8mb4_unicode_ci"), mysql.TimeTruncate(time.Microsecond)); err != nil {
		return nil, fmt.Errorf("database driver configuration failed")
	}
	if c.TLSMode == "verify-full" {
		pem, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, fmt.Errorf("cannot read DB_TLS_CA_FILE")
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("DB_TLS_CA_FILE contains no CA certificates")
		}
		d.TLS = &tls.Config{RootCAs: roots, ServerName: c.Host, MinVersion: tls.VersionTLS12}
	}
	return d, nil
}

type discardDriverLog struct{}

func (discardDriverLog) Print(...any) {}
