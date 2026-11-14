-- +goose NO TRANSACTION
-- +goose Up
-- The connected app a recommendation's current content came through: its
-- self-declared name and the verified host of its metadata document. Set
-- when an agent published it, or when the person published an agent's draft
-- on the web. NULL for everything a person writes on the web themselves, and
-- for every existing row; a person's later change of reason clears both.
ALTER TABLE event_recommendations ADD COLUMN via_client_name VARCHAR(80) NULL,
    ADD COLUMN via_client_host VARCHAR(253) NULL;

-- Private drafts of free text an agent wrote for the person to review. They
-- become public only when the person publishes them on /me/drafts; until then
-- nobody else can read them. Rows expire after seven days and are deleted
-- opportunistically.
CREATE TABLE agent_drafts (
    draft_id VARBINARY(32) NOT NULL PRIMARY KEY,
    account_id VARBINARY(32) NOT NULL,
    event_id VARBINARY(300) NOT NULL,
    kind VARCHAR(32) NOT NULL,
    body TEXT NOT NULL,
    client_name VARCHAR(80) NOT NULL,
    -- Empty for drafts made with an API token, which identifies no app.
    client_host VARCHAR(253) NOT NULL,
    -- The OAuth connection that wrote it: disconnecting the app, signing out
    -- everywhere, token revocation and connection eviction delete its drafts.
    -- NULL for API-token drafts.
    grant_id VARBINARY(32) NULL,
    created_at DATETIME(6) NOT NULL,
    expires_at DATETIME(6) NOT NULL,
    INDEX agent_drafts_owner (account_id, expires_at, created_at),
    INDEX agent_drafts_expiry (expires_at),
    INDEX agent_drafts_grant (grant_id),
    CONSTRAINT agent_drafts_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    CONSTRAINT agent_drafts_event_fk FOREIGN KEY (event_id) REFERENCES event_snapshots(event_id),
    CONSTRAINT agent_drafts_grant_fk FOREIGN KEY (grant_id) REFERENCES oauth_grants(grant_id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE agent_drafts;
ALTER TABLE event_recommendations DROP COLUMN via_client_host, DROP COLUMN via_client_name;
