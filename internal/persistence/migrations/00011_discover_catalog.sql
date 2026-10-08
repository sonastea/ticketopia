-- +goose NO TRANSACTION
-- +goose Up
-- Search projections contain only public event facts, never account activity.
CREATE TABLE event_search_places (
    event_id VARBINARY(300) NOT NULL,
    city VARCHAR(120) NOT NULL,
    country CHAR(2) NOT NULL,
    PRIMARY KEY (event_id, city, country),
    INDEX search_city (city, country, event_id),
    INDEX search_country (country, event_id),
    FOREIGN KEY (event_id) REFERENCES events(event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE event_search_facets (
    event_id VARBINARY(300) NOT NULL,
    kind VARCHAR(16) NOT NULL,
    value VARBINARY(300) NOT NULL,
    PRIMARY KEY (event_id, kind, value),
    INDEX search_facet (kind, value, event_id),
    FOREIGN KEY (event_id) REFERENCES events(event_id)
) ENGINE=InnoDB;

ALTER TABLE events ADD INDEX search_date (local_date, name(120), event_id),
    ADD INDEX search_name (name(120), event_id);
ALTER TABLE collection_tasks ADD INDEX collection_scope (city, country, local_date);

-- Backfill existing public metadata without inventing observation/coverage times.
INSERT IGNORE INTO event_search_places (event_id,city,country)
SELECT e.event_id, LEFT(j.city,120), LEFT(COALESCE(j.country,''),2)
FROM events e, JSON_TABLE(e.venues,'$[*]' COLUMNS(city VARCHAR(120) PATH '$.city', country CHAR(2) PATH '$.country_code')) j WHERE j.city IS NOT NULL;
INSERT IGNORE INTO event_search_places (event_id,city,country)
SELECT event_id, LEFT(JSON_UNQUOTE(JSON_EXTRACT(place,'$.city')),120), LEFT(COALESCE(JSON_UNQUOTE(JSON_EXTRACT(place,'$.country_code')),''),2)
FROM events WHERE JSON_EXTRACT(place,'$.city') IS NOT NULL AND JSON_UNQUOTE(JSON_EXTRACT(place,'$.city')) <> 'null';

INSERT IGNORE INTO event_search_facets (event_id,kind,value)
SELECT e.event_id,'artist',j.id FROM events e, JSON_TABLE(e.artists,'$[*]' COLUMNS(id VARCHAR(300) PATH '$.id')) j WHERE j.id IS NOT NULL AND j.id<>'';
INSERT IGNORE INTO event_search_facets (event_id,kind,value)
SELECT e.event_id,'venue',j.id FROM events e, JSON_TABLE(e.venues,'$[*]' COLUMNS(id VARCHAR(300) PATH '$.id')) j WHERE j.id IS NOT NULL AND j.id<>'';
INSERT IGNORE INTO event_search_facets (event_id,kind,value)
SELECT e.event_id,'category',j.id FROM events e, JSON_TABLE(e.classifications,'$[*]' COLUMNS(id VARCHAR(300) PATH '$.segment.id')) j WHERE j.id IS NOT NULL AND j.id<>'';
INSERT IGNORE INTO event_search_facets (event_id,kind,value)
SELECT e.event_id,'genre',j.id FROM events e, JSON_TABLE(e.classifications,'$[*]' COLUMNS(id VARCHAR(300) PATH '$.genre.id')) j WHERE j.id IS NOT NULL AND j.id<>'';

CREATE TABLE discovery_scopes (
    scope_id BINARY(64) NOT NULL PRIMARY KEY,
    city VARCHAR(120) NOT NULL,
    country CHAR(2) NOT NULL,
    start_date CHAR(10) NOT NULL,
    end_date CHAR(10) NOT NULL,
    artist_id VARBINARY(300) NOT NULL,
    venue_id VARBINARY(300) NOT NULL,
    token VARBINARY(32) NULL,
    lease_until DATETIME(6) NULL,
    next_refresh DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    last_attempt DATETIME(6) NULL,
    last_success DATETIME(6) NULL,
    data_as_of DATETIME(6) NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'not_collected',
    pages INT NOT NULL DEFAULT 0,
    low_total INT NOT NULL DEFAULT 0,
    high_total INT NOT NULL DEFAULT 0,
    INDEX discovery_coverage (city,country,start_date,end_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE discovery_scope_events (
    scope_id BINARY(64) NOT NULL,
    event_id VARBINARY(300) NOT NULL,
    token VARBINARY(32) NOT NULL,
    PRIMARY KEY (scope_id,event_id),
    INDEX discovery_run (scope_id,token),
    FOREIGN KEY (scope_id) REFERENCES discovery_scopes(scope_id),
    FOREIGN KEY (event_id) REFERENCES events(event_id)
) ENGINE=InnoDB;

CREATE TABLE event_detail_tasks (
    event_id VARBINARY(300) NOT NULL PRIMARY KEY,
    next_refresh DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    token VARBINARY(32) NULL,
    lease_until DATETIME(6) NULL,
    last_attempt DATETIME(6) NULL,
    last_failure VARCHAR(64) NOT NULL DEFAULT '',
    INDEX detail_due (next_refresh,lease_until),
    FOREIGN KEY (event_id) REFERENCES events(event_id)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE event_detail_tasks;
DROP TABLE discovery_scope_events;
DROP TABLE discovery_scopes;
ALTER TABLE collection_tasks DROP INDEX collection_scope;
ALTER TABLE events DROP INDEX search_date, DROP INDEX search_name;
DROP TABLE event_search_facets;
DROP TABLE event_search_places;
