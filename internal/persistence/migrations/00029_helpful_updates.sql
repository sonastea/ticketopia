-- +goose NO TRANSACTION
-- +goose Up
-- When each Helpful mark was made, so a day's update counts only that day's
-- marks that still stand. Marks from before this version stay NULL and never
-- count or get named; the application stamps every new mark. created_day is
-- the mark's UTC day, so an update finds its day's marks by equality on
-- (post_id, created_day) and reads names newest first from the same index,
-- never the post's whole history.
ALTER TABLE post_helpful ADD COLUMN created_at DATETIME(6) NULL,
    ADD COLUMN created_day DATE AS (DATE(created_at)) PERSISTENT,
    ADD INDEX helpful_post_day (post_id, created_day, created_at, account_id),
    DROP INDEX helpful_post;

-- One private "your answer helped" envelope per post per UTC day for its
-- author. It holds only IDs and the delivery window, never who marked it:
-- counts and opted-in first names are read from current marks at delivery.
-- Both foreign keys cascade, so removing a post or account removes its
-- envelopes without a separate delete.
CREATE TABLE helpful_notifications (
    notification_id VARBINARY(32) NOT NULL PRIMARY KEY,
    account_id VARBINARY(32) NOT NULL,
    post_id VARBINARY(32) NOT NULL,
    window_start DATETIME(6) NOT NULL,
    available_at DATETIME(6) NOT NULL,
    read_at DATETIME(6) NULL,
    UNIQUE KEY helpful_notification_batch (account_id,post_id,available_at),
    INDEX helpful_notification_inbox (account_id,available_at,notification_id),
    INDEX helpful_notification_post (post_id),
    CONSTRAINT helpful_notification_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id) ON DELETE CASCADE,
    CONSTRAINT helpful_notification_post_fk FOREIGN KEY (post_id) REFERENCES discussion_posts(post_id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE helpful_notifications;
ALTER TABLE post_helpful ADD INDEX helpful_post (post_id), DROP INDEX helpful_post_day, DROP COLUMN created_day, DROP COLUMN created_at;
