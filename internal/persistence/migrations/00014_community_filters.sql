-- +goose NO TRANSACTION
-- +goose Up
-- Keep the exact snapshot fallback semantics (including literal 'null'). Stored
-- columns backfill existing rows and follow the winning snapshot automatically.
-- Prefix indexes bound index size without truncating the compared values.
ALTER TABLE event_snapshots
    ADD COLUMN community_city LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci
        GENERATED ALWAYS AS (COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(snapshot,'$.venues[0].city')),'null'),NULLIF(JSON_UNQUOTE(JSON_EXTRACT(snapshot,'$.place.city')),'null'),'')) PERSISTENT,
    ADD COLUMN community_country LONGTEXT CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci
        GENERATED ALWAYS AS (COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(snapshot,'$.venues[0].country_code')),'null'),NULLIF(JSON_UNQUOTE(JSON_EXTRACT(snapshot,'$.place.country_code')),'null'),'')) PERSISTENT,
    ADD INDEX community_city (community_city(120),community_country(2),event_id),
    ADD INDEX community_country (community_country(2),event_id);

-- v11 originally backfilled from identity metadata, which can differ from the
-- authoritative snapshot. Align category facets for existing snapshots too.
DELETE f FROM event_search_facets f JOIN event_snapshots e ON e.event_id=f.event_id WHERE f.kind='category';
INSERT IGNORE INTO event_search_facets (event_id,kind,value)
SELECT e.event_id,'category',j.id FROM event_snapshots e,
    JSON_TABLE(e.snapshot,'$.classifications[*]' COLUMNS(id LONGTEXT PATH '$.segment.id')) j
    WHERE j.id IS NOT NULL AND j.id<>'' AND OCTET_LENGTH(j.id)<=300;

-- +goose Down
ALTER TABLE event_snapshots DROP INDEX community_city, DROP INDEX community_country,
    DROP COLUMN community_city, DROP COLUMN community_country;
