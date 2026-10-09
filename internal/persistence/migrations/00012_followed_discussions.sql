-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE discussion_follows (
    account_id VARBINARY(32) NOT NULL,
    thread_id VARBINARY(32) NOT NULL,
    frequency ENUM('immediate','daily','weekly','muted') NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    PRIMARY KEY (account_id,thread_id),
    INDEX discussion_follow_thread (thread_id),
    CONSTRAINT discussion_follow_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    CONSTRAINT discussion_follow_thread_fk FOREIGN KEY (thread_id) REFERENCES discussion_posts(post_id)
) ENGINE=InnoDB;

-- An envelope coalesces replies for one UTC daily/weekly window. Immediate
-- envelopes have one reply. No body, author, report, or moderation evidence is copied.
CREATE TABLE discussion_notifications (
    notification_id VARBINARY(32) NOT NULL PRIMARY KEY,
    account_id VARBINARY(32) NOT NULL,
    thread_id VARBINARY(32) NOT NULL,
    batch_key VARBINARY(64) NOT NULL,
    available_at DATETIME(6) NOT NULL,
    read_at DATETIME(6) NULL,
    UNIQUE KEY discussion_notification_batch (account_id,thread_id,batch_key),
    INDEX discussion_notification_inbox (account_id,available_at,notification_id),
    CONSTRAINT discussion_notification_follow_fk FOREIGN KEY (account_id,thread_id) REFERENCES discussion_follows(account_id,thread_id) ON DELETE CASCADE
) ENGINE=InnoDB;

CREATE TABLE discussion_notification_posts (
    notification_id VARBINARY(32) NOT NULL,
    post_id VARBINARY(32) NOT NULL,
    PRIMARY KEY (notification_id,post_id),
    CONSTRAINT discussion_notification_envelope_fk FOREIGN KEY (notification_id) REFERENCES discussion_notifications(notification_id) ON DELETE CASCADE,
    CONSTRAINT discussion_notification_post_fk FOREIGN KEY (post_id) REFERENCES discussion_posts(post_id)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE discussion_notification_posts;
DROP TABLE discussion_notifications;
DROP TABLE discussion_follows;
