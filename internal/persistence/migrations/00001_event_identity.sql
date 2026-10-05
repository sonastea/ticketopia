-- +goose NO TRANSACTION
-- +goose Up
-- MariaDB DDL implicitly commits. schema_state guards partial application.
CREATE TABLE events (
    event_id VARBINARY(300) NOT NULL PRIMARY KEY,
    name VARCHAR(1024) NOT NULL,
    source_url TEXT NOT NULL,
    start_utc DATETIME(6) NULL,
    local_date CHAR(10) NULL,
    local_time VARCHAR(16) NULL,
    timezone VARCHAR(64) NULL,
    date_tba BOOLEAN NOT NULL,
    date_tbd BOOLEAN NOT NULL,
    time_tba BOOLEAN NOT NULL,
    no_specific_time BOOLEAN NOT NULL,
    status VARCHAR(64) NOT NULL,
    venues JSON NOT NULL,
    artists JSON NOT NULL,
    classifications JSON NOT NULL,
    place JSON NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE event_providers (
    provider VARBINARY(32) NOT NULL,
    source_id VARBINARY(255) NOT NULL,
    event_id VARBINARY(300) NOT NULL,
    PRIMARY KEY (provider, source_id),
    INDEX event_providers_event (event_id),
    CONSTRAINT event_providers_event_fk FOREIGN KEY (event_id) REFERENCES events(event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
-- Destructive rollback is deliberately not exposed by the application command.
DROP TABLE event_providers;
DROP TABLE events;
