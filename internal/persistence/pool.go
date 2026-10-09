package persistence

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/go-sql-driver/mysql"
)

const SupportedSchemaVersion = 12

var ErrUnavailable = errors.New("database unavailable")

// safeError intentionally does not wrap the driver error, even via Unwrap.
func safeError(operation string, err error) error {
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("database %s: %w", operation, context.Canceled)
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("database %s: %w", operation, context.DeadlineExceeded)
	}
	var server *mysql.MySQLError
	if errors.As(err, &server) {
		return fmt.Errorf("database %s: %w (MariaDB code %d)", operation, ErrUnavailable, server.Number)
	}
	return fmt.Errorf("database %s: %w", operation, ErrUnavailable)
}

type Pool struct {
	db     *sql.DB
	config Config
	closed atomic.Bool
}

func connect(ctx context.Context, c Config) (*sql.DB, error) {
	d, err := c.driverConfig()
	if err != nil {
		return nil, err
	}
	connector, err := mysql.NewConnector(d)
	if err != nil {
		return nil, safeError("configuration", err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(c.MaxOpen)
	db.SetMaxIdleConns(c.MaxIdle)
	db.SetConnMaxLifetime(c.Lifetime)
	db.SetConnMaxIdleTime(c.IdleTime)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, safeError("connect", err)
	}
	return db, nil
}

// Open validates an existing schema and runtime write permissions. It never
// creates, migrates, or grants anything, and its write probes affect zero rows.
func Open(ctx context.Context, c Config) (*Pool, error) {
	if err := c.validate(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.StartupTimeout)
	defer cancel()
	db, err := connect(ctx, c)
	if err != nil {
		return nil, err
	}
	p := &Pool{db: db, config: c}
	if err := p.validateSchema(ctx); err != nil {
		db.Close()
		return nil, err
	}
	if err := p.validateRuntimePermissions(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return p, nil
}

func (p *Pool) Close() error {
	p.closed.Store(true)
	if err := p.db.Close(); err != nil {
		return safeError("close", err)
	}
	return nil
}

// Ready uses a short, fresh check so outages recover without restarting the app.
func (p *Pool) Ready(ctx context.Context) error {
	if p.closed.Load() {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, p.config.ReadyTimeout)
	defer cancel()
	if err := p.checkVersion(ctx); err != nil {
		return err
	}
	if p.closed.Load() {
		return ErrUnavailable
	}
	return nil
}

func (p *Pool) checkVersion(ctx context.Context) error {
	var dirty bool
	if err := p.db.QueryRowContext(ctx, `SELECT dirty FROM schema_state WHERE id = 1`).Scan(&dirty); err != nil {
		return safeError("schema validation", err)
	}
	if dirty {
		return fmt.Errorf("database schema is dirty; inspect and repair the failed migration")
	}
	var version int
	if err := p.db.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_id), 0) FROM goose_db_version WHERE is_applied = 1`).Scan(&version); err != nil {
		return safeError("schema validation", err)
	}
	if version != SupportedSchemaVersion {
		return fmt.Errorf("database schema version %d is unsupported; required version %d", version, SupportedSchemaVersion)
	}
	return nil
}

func (p *Pool) validateSchema(ctx context.Context) error {
	if err := p.checkVersion(ctx); err != nil {
		return err
	}
	for _, query := range []string{
		`SELECT event_id,city,country FROM event_search_places LIMIT 0`,
		`SELECT event_id,kind,value FROM event_search_facets LIMIT 0`,
		`SELECT scope_id,token,status,last_success FROM discovery_scopes LIMIT 0`,
		`SELECT scope_id,event_id,token FROM discovery_scope_events LIMIT 0`,
		`SELECT event_id,token,next_refresh FROM event_detail_tasks LIMIT 0`,
		`SELECT ` + eventColumns + ` FROM events LIMIT 0`,
		`SELECT provider, source_id, event_id FROM event_providers LIMIT 0`,
		`SELECT ` + accountColumns + ` FROM accounts LIMIT 0`,
		`SELECT issuer, subject, account_id FROM account_identities LIMIT 0`,
		`SELECT ` + credentialColumns + ` FROM account_credentials LIMIT 0`,
		`SELECT state_hash, browser_hash, verifier, nonce, return_to, expires_at FROM auth_flows LIMIT 0`,
		`SELECT bucket_hash, attempts, expires_at FROM auth_rate_limits LIMIT 0`,
		`SELECT event_id, snapshot, data_as_of FROM event_snapshots LIMIT 0`,
		`SELECT account_id, event_id, saved_at FROM saved_events LIMIT 0`,
		`SELECT account_id, event_id, interested_at, visibility FROM event_interests LIMIT 0`,
		`SELECT account_id, event_id, reason, recommended_at, updated_at, withdrawn_at FROM event_recommendations LIMIT 0`,
		`SELECT event_id, first_recommended_at FROM recommendation_feed_events LIMIT 0`,
		`SELECT account_id, window_start, new_publications, reactivations, total_new_publications, total_reactivations, last_published_at FROM recommendation_activity LIMIT 0`,
		`SELECT post_id, account_id, event_id, root_id, parent_id, body, idempotency_key, request_hash, created_at, updated_at, removed_at, hidden_at, review_version FROM discussion_posts LIMIT 0`,
		`SELECT account_id, post_id FROM post_helpful LIMIT 0`,
		`SELECT account_id,thread_id,frequency,created_at FROM discussion_follows LIMIT 0`,
		`SELECT notification_id,account_id,thread_id,batch_key,available_at,read_at FROM discussion_notifications LIMIT 0`,
		`SELECT notification_id,post_id FROM discussion_notification_posts LIMIT 0`,
		`SELECT account_id, role FROM account_roles LIMIT 0`,
		`SELECT role_event_id, account_id, role, action, operator_label, database_user, created_at FROM account_role_events LIMIT 0`,
		`SELECT report_id, post_id, reporter_id, reason, context, reported_body, created_at, decision_id FROM moderation_reports LIMIT 0`,
		`SELECT decision_id, post_id, moderator_id, action, reason, notes, created_at FROM moderation_decisions LIMIT 0`,
		`SELECT decision_id, context, created_at, reviewed_by FROM moderation_appeals LIMIT 0`,
		`SELECT artist_id,snapshot,first_seen,last_seen FROM artists LIMIT 0`,
		`SELECT provider,source_id,artist_id FROM artist_providers LIMIT 0`,
		`SELECT venue_id,snapshot,first_seen,last_seen FROM venues LIMIT 0`,
		`SELECT account_id,artist_id,followed_at FROM artist_follows LIMIT 0`,
		`SELECT account_id,venue_id,followed_at FROM venue_follows LIMIT 0`,
		`SELECT provider,source_id,venue_id FROM venue_providers LIMIT 0`,
		`SELECT event_id,first_seen,last_seen,last_changed,snapshot FROM event_history_state LIMIT 0`,
		`SELECT event_id,observed_at,snapshot,changes FROM event_observations LIMIT 0`,
		`SELECT task_id,city,country,local_date,next_refresh,lease_until,run_id,last_attempt,last_success,last_failure FROM collection_tasks LIMIT 0`,
		`SELECT run_id,task_id,started_at,finished_at,status,failure FROM collection_runs LIMIT 0`,
		`SELECT run_id,page_number,collected_at,event_count,reported_total,limited FROM collection_pages LIMIT 0`,
		`SELECT run_id,event_id,observed_at FROM collection_run_events LIMIT 0`,
		`SELECT key_hash,window_start,used,budget,next_request,blocked_until FROM provider_budgets LIMIT 0`,
	} {
		rows, err := p.db.QueryContext(ctx, query)
		if err != nil {
			return safeError("schema validation", err)
		}
		if err := rows.Close(); err != nil {
			return safeError("schema validation", err)
		}
	}
	var count int
	err := p.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name IN ('schema_state','goose_db_version','events','event_providers','accounts','account_identities','account_credentials','auth_flows','auth_rate_limits','event_snapshots','saved_events','event_interests','event_recommendations','discussion_posts','post_helpful','discussion_follows','discussion_notifications','discussion_notification_posts','recommendation_feed_events','recommendation_activity','account_roles','account_role_events','moderation_reports','moderation_decisions','moderation_appeals','artists','artist_providers','venues','venue_providers','artist_follows','venue_follows','event_history_state','event_observations','collection_tasks','collection_runs','collection_pages','collection_run_events','provider_budgets') AND engine = 'InnoDB'`).Scan(&count)
	if err != nil {
		return safeError("schema validation", err)
	}
	if count != 38 {
		return fmt.Errorf("database schema requires InnoDB tables")
	}
	return nil
}

func (p *Pool) Events() *EventRepository {
	return &EventRepository{db: p.db, timeout: p.config.QueryTimeout}
}
