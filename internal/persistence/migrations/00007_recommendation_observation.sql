-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE event_recommendations ADD COLUMN withdrawn_at DATETIME(6) NULL,
    ADD INDEX recommendations_active_event (event_id, withdrawn_at, recommended_at, account_id);

CREATE TABLE recommendation_feed_events (
    event_id VARBINARY(300) NOT NULL PRIMARY KEY,
    first_recommended_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    INDEX recommendation_feed_recent (first_recommended_at, event_id),
    CONSTRAINT recommendation_feed_event_fk FOREIGN KEY (event_id) REFERENCES event_snapshots(event_id)
) ENGINE=InnoDB;
INSERT INTO recommendation_feed_events (event_id, first_recommended_at)
    SELECT event_id, MIN(recommended_at) FROM event_recommendations GROUP BY event_id;

-- One row/account; expired windows reset lazily on the next transition.
-- No IP addresses, text, sanctions, or unbounded per-request history.
CREATE TABLE recommendation_activity (
    account_id VARBINARY(32) NOT NULL PRIMARY KEY,
    window_start DATETIME(6) NOT NULL,
    new_publications BIGINT UNSIGNED NOT NULL DEFAULT 0,
    reactivations BIGINT UNSIGNED NOT NULL DEFAULT 0,
    total_new_publications BIGINT UNSIGNED NOT NULL DEFAULT 0,
    total_reactivations BIGINT UNSIGNED NOT NULL DEFAULT 0,
    last_published_at DATETIME(6) NOT NULL,
    INDEX recommendation_activity_recent (last_published_at),
    CONSTRAINT recommendation_activity_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE recommendation_activity;
DROP TABLE recommendation_feed_events;
DELETE FROM event_recommendations WHERE withdrawn_at IS NOT NULL;
ALTER TABLE event_recommendations DROP INDEX recommendations_active_event, DROP COLUMN withdrawn_at;
