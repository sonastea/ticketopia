-- +goose NO TRANSACTION
-- +goose Up
-- What an API token may do, as space-separated scopes: 'read' or
-- 'read write'. API tokens never hold publish. Existing tokens keep the
-- private changes they could already make; browser sessions carry ''.
ALTER TABLE account_credentials ADD COLUMN scopes VARCHAR(32) NOT NULL DEFAULT '';
UPDATE account_credentials SET scopes='read write' WHERE kind='api';

-- +goose Down
ALTER TABLE account_credentials DROP COLUMN scopes;
