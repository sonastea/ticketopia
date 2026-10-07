-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE accounts (
    account_id VARBINARY(32) NOT NULL PRIMARY KEY,
    email VARCHAR(320) NOT NULL,
    display_name VARCHAR(80) NOT NULL DEFAULT 'Event explorer',
    bio VARCHAR(500) NOT NULL DEFAULT '',
    interest_visibility ENUM('private','public') NOT NULL DEFAULT 'private',
    city VARCHAR(120) NOT NULL DEFAULT '',
    country VARCHAR(2) NOT NULL DEFAULT '',
    timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
    category_ids JSON NOT NULL DEFAULT ('[]'),
    email_reminders BOOLEAN NOT NULL DEFAULT FALSE,
    weekly_digest BOOLEAN NOT NULL DEFAULT FALSE,
    notifications_paused BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Identity is issuer + subject. Email is deliberately not unique or a login key.
CREATE TABLE account_identities (
    issuer VARBINARY(255) NOT NULL,
    subject VARBINARY(255) NOT NULL,
    account_id VARBINARY(32) NOT NULL,
    PRIMARY KEY (issuer, subject),
    INDEX account_identities_account (account_id),
    CONSTRAINT account_identities_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id)
) ENGINE=InnoDB;

CREATE TABLE account_credentials (
    credential_id VARBINARY(32) NOT NULL PRIMARY KEY,
    account_id VARBINARY(32) NOT NULL,
    token_hash BINARY(32) NOT NULL UNIQUE,
    kind ENUM('session','api') NOT NULL,
    name VARCHAR(80) NOT NULL,
    created_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    INDEX account_credentials_owner (account_id, kind, created_at, credential_id),
    INDEX account_credentials_expiry (expires_at),
    CONSTRAINT account_credentials_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE auth_flows (
    state_hash BINARY(32) NOT NULL PRIMARY KEY,
    browser_hash BINARY(32) NOT NULL,
    verifier VARBINARY(128) NOT NULL,
    nonce VARBINARY(128) NOT NULL,
    return_to VARCHAR(4096) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    INDEX auth_flows_expiry (expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE auth_rate_limits (
    bucket_hash BINARY(32) NOT NULL PRIMARY KEY,
    attempts INT NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    INDEX auth_rate_limits_expiry (expires_at)
) ENGINE=InnoDB;

-- +goose Down
DROP TABLE auth_rate_limits;
DROP TABLE auth_flows;
DROP TABLE account_credentials;
DROP TABLE account_identities;
DROP TABLE accounts;
