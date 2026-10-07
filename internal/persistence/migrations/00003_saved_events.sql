-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE event_snapshots (
    event_id VARBINARY(300) NOT NULL PRIMARY KEY,
    snapshot JSON NOT NULL,
    data_as_of DATETIME(6) NOT NULL,
    CONSTRAINT event_snapshots_event_fk FOREIGN KEY (event_id) REFERENCES events(event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE saved_events (
    account_id VARBINARY(32) NOT NULL,
    event_id VARBINARY(300) NOT NULL,
    saved_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    PRIMARY KEY (account_id, event_id),
    INDEX saved_events_owner_order (account_id, saved_at, event_id),
    CONSTRAINT saved_events_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    CONSTRAINT saved_events_event_fk FOREIGN KEY (event_id) REFERENCES event_snapshots(event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE saved_events;
DROP TABLE event_snapshots;
