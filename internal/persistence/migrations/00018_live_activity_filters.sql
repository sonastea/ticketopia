-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE accounts ADD COLUMN live_activity_filters LONGTEXT NOT NULL DEFAULT '{"location":{"mode":"all"},"category_ids":[],"artist_ids":[],"venue_ids":[],"connections":{"mode":"all_events","sources":[]}}' CHECK (JSON_VALID(live_activity_filters));
-- Stored facts follow the authoritative winning snapshot and backfill without
-- provider requests. Community location retains its existing per-field fallback.
ALTER TABLE event_snapshots ADD COLUMN activity_title LONGTEXT
    GENERATED ALWAYS AS (CASE WHEN JSON_TYPE(JSON_EXTRACT(snapshot,'$.name'))='STRING'
        THEN JSON_UNQUOTE(JSON_EXTRACT(snapshot,'$.name')) ELSE '' END) PERSISTENT;

-- +goose Down
ALTER TABLE event_snapshots DROP COLUMN activity_title;
ALTER TABLE accounts DROP COLUMN live_activity_filters;
