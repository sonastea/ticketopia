-- +goose NO TRANSACTION
-- +goose Up
-- When the current lead time took effect; NULL means unchanged since created_at.
ALTER TABLE event_reminders ADD COLUMN enrolled_at DATETIME(6) NULL;

-- +goose Down
-- Retain later enrollment instants on rollback.
SELECT 1;
