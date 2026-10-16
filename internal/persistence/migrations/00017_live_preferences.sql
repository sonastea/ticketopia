-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE accounts
    ALTER COLUMN notifications_paused SET DEFAULT FALSE,
    ADD COLUMN notification_pause_source VARCHAR(24) NOT NULL DEFAULT 'default',
    ADD COLUMN live_activity_version INT NOT NULL DEFAULT 1,
    ADD COLUMN live_activity_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    ADD COLUMN live_activity_revision BIGINT UNSIGNED NOT NULL DEFAULT 1;

-- Product policy, not a user action. Goose records this once; application
-- startup/login never resets these choices. Channel/delivery records are untouched.
UPDATE accounts SET notifications_paused=FALSE, notification_pause_source='migration_policy';

-- +goose Down
-- Preference-preserving rollback requires a compatible application image.
SELECT 1;
