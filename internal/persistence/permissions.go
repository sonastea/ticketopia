package persistence

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-sql-driver/mysql"
)

// These are the table-level writes required by the current schema. Contract
// tests keep them aligned with local grants and the setup SQL checks. Runtime
// must never gain permission to grant itself access or change the schema.
var runtimeTablePermissions = []struct {
	table        string
	updateColumn string
	delete       bool
}{
	{"events", "name", false},
	{"event_providers", "event_id", false},
	{"accounts", "display_name", false},
	{"account_identities", "", false},
	{"account_credentials", "", true},
	{"auth_flows", "", true},
	{"auth_rate_limits", "attempts", true},
	{"event_snapshots", "snapshot", false},
	{"saved_events", "", true},
	{"event_interests", "visibility", true},
	{"event_recommendations", "reason", true},
	{"recommendation_feed_events", "first_recommended_at", false},
	{"recommendation_activity", "new_publications", false},
	{"discussion_posts", "body", false},
	{"post_helpful", "post_id", true},
	{"moderation_reports", "decision_id", false},
	{"moderation_decisions", "", false},
	{"moderation_appeals", "reviewed_by", false},
	{"artists", "snapshot", false},
	{"artist_providers", "artist_id", false},
	{"venues", "snapshot", false},
	{"venue_providers", "venue_id", false},
	{"event_history_state", "last_seen", false},
	{"event_observations", "", false},
	{"collection_tasks", "next_refresh", false},
	{"collection_runs", "status", false},
	{"collection_pages", "", false},
	{"collection_run_events", "", false},
	{"provider_budgets", "used", false},
	{"artist_follows", "", true},
	{"venue_follows", "", true},
	{"event_search_places", "", true},
	{"event_search_facets", "", true},
	{"discovery_scopes", "scope_id", false},
	{"discovery_scope_events", "token", false},
	{"event_detail_tasks", "token", true},
}

type runtimeWriteCheck struct {
	table, operation, query string
}

func runtimeWriteChecks() []runtimeWriteCheck {
	checks := make([]runtimeWriteCheck, 0, len(runtimeTablePermissions)*3)
	for _, permission := range runtimeTablePermissions {
		table := permission.table
		checks = append(checks, runtimeWriteCheck{table, "INSERT", "INSERT INTO " + table + " SELECT * FROM " + table + " WHERE FALSE"})
		if column := permission.updateColumn; column != "" {
			checks = append(checks, runtimeWriteCheck{table, "UPDATE", "UPDATE " + table + " SET " + column + "=" + column + " WHERE FALSE"})
		}
		if permission.delete {
			checks = append(checks, runtimeWriteCheck{table, "DELETE", "DELETE FROM " + table + " WHERE FALSE"})
		}
	}
	return checks
}

// Probe actual access instead of interpreting SHOW GRANTS (including roles and
// database-wide grants). Every statement affects zero rows, and the transaction
// is always rolled back. Use the caller's startup deadline, not per-query ones.
func (p *Pool) validateRuntimePermissions(ctx context.Context) error {
	tx, err := p.db.BeginTx(ctx, nil)
	if err != nil {
		return safeError("runtime permission validation", err)
	}
	defer tx.Rollback()
	for _, check := range runtimeWriteChecks() {
		if _, err := tx.ExecContext(ctx, check.query); err != nil {
			failure := safeError("runtime permission validation ("+check.operation+" "+check.table+")", err)
			var server *mysql.MySQLError
			if errors.As(err, &server) && (server.Number == 1142 || server.Number == 1143) {
				return fmt.Errorf("%w; verify runtime grants after migrations", failure)
			}
			return failure
		}
	}
	if err := tx.Rollback(); err != nil {
		return safeError("runtime permission validation rollback", err)
	}
	return nil
}
