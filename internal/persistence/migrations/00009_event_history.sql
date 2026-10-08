-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE artists (
    artist_id VARBINARY(300) NOT NULL PRIMARY KEY,
    snapshot JSON NOT NULL,
    first_seen DATETIME(6) NOT NULL,
    last_seen DATETIME(6) NOT NULL
) ENGINE=InnoDB;

CREATE TABLE artist_providers (
    provider VARBINARY(32) NOT NULL,
    source_id VARBINARY(255) NOT NULL,
    artist_id VARBINARY(300) NOT NULL,
    PRIMARY KEY (provider, source_id),
    INDEX (artist_id),
    FOREIGN KEY (artist_id) REFERENCES artists(artist_id)
) ENGINE=InnoDB;

CREATE TABLE venues (
    venue_id VARBINARY(300) NOT NULL PRIMARY KEY,
    snapshot JSON NOT NULL,
    first_seen DATETIME(6) NOT NULL,
    last_seen DATETIME(6) NOT NULL
) ENGINE=InnoDB;

CREATE TABLE venue_providers (
    provider VARBINARY(32) NOT NULL,
    source_id VARBINARY(255) NOT NULL,
    venue_id VARBINARY(300) NOT NULL,
    PRIMARY KEY (provider, source_id),
    INDEX (venue_id),
    FOREIGN KEY (venue_id) REFERENCES venues(venue_id)
) ENGINE=InnoDB;

CREATE TABLE event_history_state (
    event_id VARBINARY(300) NOT NULL PRIMARY KEY,
    first_seen DATETIME(6) NOT NULL,
    last_seen DATETIME(6) NOT NULL,
    last_changed DATETIME(6) NULL,
    snapshot JSON NOT NULL,
    FOREIGN KEY (event_id) REFERENCES events(event_id)
) ENGINE=InnoDB;

CREATE TABLE event_observations (
    event_id VARBINARY(300) NOT NULL,
    observed_at DATETIME(6) NOT NULL,
    snapshot JSON NOT NULL,
    changes JSON NOT NULL,
    PRIMARY KEY (event_id, observed_at),
    FOREIGN KEY (event_id) REFERENCES events(event_id)
) ENGINE=InnoDB;

CREATE TABLE collection_tasks (
    task_id BINARY(64) NOT NULL PRIMARY KEY,
    city VARCHAR(120) NOT NULL,
    country CHAR(2) NOT NULL,
    local_date CHAR(10) NOT NULL,
    next_refresh DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    lease_until DATETIME(6) NULL,
    run_id VARBINARY(64) NULL,
    last_attempt DATETIME(6) NULL,
    last_success DATETIME(6) NULL,
    last_failure VARCHAR(64) NOT NULL DEFAULT '',
    INDEX collection_due (next_refresh, task_id),
    INDEX collection_expired (lease_until, task_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE collection_runs (
    run_id VARBINARY(64) NOT NULL PRIMARY KEY,
    task_id BINARY(64) NOT NULL,
    started_at DATETIME(6) NOT NULL,
    finished_at DATETIME(6) NULL,
    status VARCHAR(16) NOT NULL,
    failure VARCHAR(64) NOT NULL DEFAULT '',
    INDEX collection_history (task_id, started_at),
    FOREIGN KEY (task_id) REFERENCES collection_tasks(task_id)
) ENGINE=InnoDB;

CREATE TABLE collection_pages (
    run_id VARBINARY(64) NOT NULL,
    page_number INT NOT NULL,
    collected_at DATETIME(6) NOT NULL,
    event_count INT NOT NULL,
    reported_total INT NOT NULL,
    limited BOOLEAN NOT NULL,
    PRIMARY KEY (run_id, page_number),
    FOREIGN KEY (run_id) REFERENCES collection_runs(run_id)
) ENGINE=InnoDB;

CREATE TABLE collection_run_events (
    run_id VARBINARY(64) NOT NULL,
    event_id VARBINARY(300) NOT NULL,
    observed_at DATETIME(6) NOT NULL,
    PRIMARY KEY (run_id, event_id),
    FOREIGN KEY (run_id) REFERENCES collection_runs(run_id),
    FOREIGN KEY (event_id, observed_at) REFERENCES event_observations(event_id, observed_at)
) ENGINE=InnoDB;

-- All resources using one key share conservative 24-hour window accounting,
-- pacing and cooldowns. Only a SHA-256 key fingerprint is stored, never the key.
CREATE TABLE provider_budgets (
    key_hash BINARY(32) NOT NULL PRIMARY KEY,
    window_start DATETIME(6) NOT NULL,
    used INT NOT NULL,
    budget INT NOT NULL,
    next_request DATETIME(6) NOT NULL,
    blocked_until DATETIME(6) NOT NULL
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE provider_budgets;
DROP TABLE collection_run_events;
DROP TABLE collection_pages;
DROP TABLE collection_runs;
DROP TABLE collection_tasks;
DROP TABLE event_observations;
DROP TABLE event_history_state;
DROP TABLE venue_providers;
DROP TABLE venues;
DROP TABLE artist_providers;
DROP TABLE artists;
