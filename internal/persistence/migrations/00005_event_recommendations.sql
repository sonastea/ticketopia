-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE event_recommendations (
    account_id VARBINARY(32) NOT NULL,
    event_id VARBINARY(300) NOT NULL,
    reason VARCHAR(500) NOT NULL DEFAULT '',
    recommended_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    PRIMARY KEY (account_id, event_id),
    INDEX recommendations_recent (recommended_at, event_id, account_id),
    INDEX recommendations_owner (account_id, recommended_at, event_id),
    INDEX recommendations_event (event_id, recommended_at, account_id),
    CONSTRAINT recommendations_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    CONSTRAINT recommendations_event_fk FOREIGN KEY (event_id) REFERENCES event_snapshots(event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE event_recommendations;
