-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE discussion_posts (
    post_id VARBINARY(32) NOT NULL PRIMARY KEY,
    account_id VARBINARY(32) NOT NULL,
    event_id VARBINARY(300) NOT NULL,
    root_id VARBINARY(32) NULL,
    parent_id VARBINARY(32) NULL,
    body TEXT NOT NULL,
    idempotency_key VARBINARY(128) NOT NULL,
    request_hash BINARY(32) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    removed_at DATETIME(6) NULL,
    UNIQUE KEY discussion_retry (account_id, idempotency_key),
    INDEX discussion_event (event_id, root_id, created_at, post_id),
    INDEX discussion_replies (root_id, created_at, post_id),
    INDEX discussion_recent (root_id, created_at, post_id),
    CONSTRAINT discussion_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    CONSTRAINT discussion_event_fk FOREIGN KEY (event_id) REFERENCES event_snapshots(event_id),
    CONSTRAINT discussion_root_fk FOREIGN KEY (root_id) REFERENCES discussion_posts(post_id),
    CONSTRAINT discussion_parent_fk FOREIGN KEY (parent_id) REFERENCES discussion_posts(post_id),
    CHECK (parent_id IS NULL OR root_id IS NOT NULL)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE post_helpful (
    account_id VARBINARY(32) NOT NULL,
    post_id VARBINARY(32) NOT NULL,
    PRIMARY KEY (account_id, post_id),
    INDEX helpful_post (post_id),
    CONSTRAINT helpful_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    CONSTRAINT helpful_post_fk FOREIGN KEY (post_id) REFERENCES discussion_posts(post_id)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE post_helpful;
DROP TABLE discussion_posts;
