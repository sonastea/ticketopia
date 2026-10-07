-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE event_interests (
    account_id VARBINARY(32) NOT NULL,
    event_id VARBINARY(300) NOT NULL,
    visibility ENUM('private', 'public') NOT NULL DEFAULT 'private',
    interested_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    PRIMARY KEY (account_id, event_id),
    INDEX event_interests_owner_order (account_id, interested_at, event_id),
    INDEX event_interests_public_owner (account_id, visibility, interested_at, event_id),
    INDEX event_interests_public_event (event_id, visibility, interested_at, account_id),
    CONSTRAINT event_interests_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    CONSTRAINT event_interests_event_fk FOREIGN KEY (event_id) REFERENCES event_snapshots(event_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE event_interests;
