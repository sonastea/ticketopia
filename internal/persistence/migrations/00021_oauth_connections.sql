-- +goose NO TRANSACTION
-- +goose Up
-- Single-use OAuth authorization codes for agent connections (/mcp). Only
-- hashes are stored; codes expire within minutes.
CREATE TABLE oauth_codes (
    code_hash BINARY(32) NOT NULL PRIMARY KEY,
    account_id VARBINARY(32) NOT NULL,
    client_id VARCHAR(2048) NOT NULL,
    client_name VARCHAR(80) NOT NULL,
    redirect_uri VARCHAR(2048) NOT NULL,
    code_challenge VARBINARY(128) NOT NULL,
    scopes VARCHAR(64) NOT NULL,
    refresh BOOLEAN NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    INDEX oauth_codes_account (account_id),
    INDEX oauth_codes_expiry (expires_at),
    CONSTRAINT oauth_codes_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- One row per connected agent: the person's consent and its current token
-- pair. Refreshing replaces both hashes; deleting the row revokes the agent.
CREATE TABLE oauth_grants (
    grant_id VARBINARY(32) NOT NULL PRIMARY KEY,
    account_id VARBINARY(32) NOT NULL,
    client_id VARCHAR(2048) NOT NULL,
    client_name VARCHAR(80) NOT NULL,
    scopes VARCHAR(64) NOT NULL,
    access_hash BINARY(32) NOT NULL UNIQUE,
    access_expires_at DATETIME(6) NOT NULL,
    refresh_hash BINARY(32) NULL UNIQUE,
    created_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    INDEX oauth_grants_owner (account_id, created_at, grant_id),
    INDEX oauth_grants_expiry (expires_at),
    CONSTRAINT oauth_grants_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Refresh tokens already rotated away. Presenting one again revokes its grant
-- (it leaked, or the client raced itself); rows go with the grant.
CREATE TABLE oauth_refresh_history (
    refresh_hash BINARY(32) NOT NULL PRIMARY KEY,
    grant_id VARBINARY(32) NOT NULL,
    used_at DATETIME(6) NOT NULL,
    INDEX oauth_refresh_history_grant (grant_id, used_at),
    CONSTRAINT oauth_refresh_history_grant_fk FOREIGN KEY (grant_id) REFERENCES oauth_grants(grant_id) ON DELETE CASCADE
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE oauth_refresh_history;
DROP TABLE oauth_grants;
DROP TABLE oauth_codes;
