-- +goose NO TRANSACTION
-- +goose Up
-- Additive read indexes; no grant changes and no data rewrite.

-- Event-card previews and community lists rank visible root posts per event,
-- newest first. Covering the visibility columns lets that ranking read only
-- this index instead of every post row (discussion_event lacks them).
ALTER TABLE discussion_posts
    ADD INDEX discussion_visible_roots (event_id, root_id, removed_at, hidden_at, created_at, post_id);

-- Event history lists only observations that changed something. Filtering on
-- JSON_LENGTH(changes) evaluated every observation of the event; a virtual
-- column with an index finds the changed ones directly, newest first.
ALTER TABLE event_observations
    ADD COLUMN has_changes TINYINT(1) AS (JSON_LENGTH(changes) > 0) VIRTUAL,
    ADD INDEX observation_changes (event_id, has_changes, observed_at);

-- The detail queue claims by ORDER BY next_refresh, event_id. The old key
-- (next_refresh, lease_until) could not supply that order, so each claim
-- sorted the whole due backlog; this key keeps it and checks the lease inline.
ALTER TABLE event_detail_tasks
    DROP INDEX detail_due,
    ADD INDEX detail_due (next_refresh, event_id, lease_until);

-- +goose Down
ALTER TABLE event_detail_tasks
    DROP INDEX detail_due,
    ADD INDEX detail_due (next_refresh, lease_until);
ALTER TABLE event_observations
    DROP INDEX observation_changes,
    DROP COLUMN has_changes;
ALTER TABLE discussion_posts
    DROP INDEX discussion_visible_roots;
