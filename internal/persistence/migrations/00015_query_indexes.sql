-- +goose NO TRANSACTION
-- +goose Up
-- Identical to discussion_replies, which also supports discussion_root_fk.
ALTER TABLE discussion_posts DROP INDEX discussion_recent;

-- Scope identity predicates precede the overlapping-date range. Sorting by
-- last_success remains explicit rather than sacrificing date filtering.
ALTER TABLE discovery_scopes DROP INDEX discovery_coverage,
    ADD INDEX discovery_coverage (city, country, artist_id, venue_id, start_date, end_date);

-- +goose Down
ALTER TABLE discovery_scopes DROP INDEX discovery_coverage,
    ADD INDEX discovery_coverage (city, country, start_date, end_date);
ALTER TABLE discussion_posts ADD INDEX discussion_recent (root_id, created_at, post_id);
