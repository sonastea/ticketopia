-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE accounts ADD COLUMN activity_first_name VARCHAR(40) NULL;
CREATE TABLE live_recommendation_activity (
    account_id VARBINARY(32) NOT NULL,
    event_id VARBINARY(300) NOT NULL,
    published_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    expires_at DATETIME(6) NOT NULL,
    PRIMARY KEY (account_id, event_id),
    INDEX live_activity_expiry (expires_at, event_id, published_at),
    INDEX live_activity_publication (published_at, event_id),
    FOREIGN KEY (account_id, event_id) REFERENCES event_recommendations(account_id, event_id),
    FOREIGN KEY (event_id) REFERENCES event_snapshots(event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE live_recommendation_activity;
ALTER TABLE accounts DROP COLUMN activity_first_name;
