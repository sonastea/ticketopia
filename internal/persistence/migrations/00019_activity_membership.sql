-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE accounts ADD COLUMN activity_membership_revision BIGINT UNSIGNED NOT NULL DEFAULT 1;

-- +goose Down
-- Retain revisions and later notification choices on rollback.
SELECT 1;
