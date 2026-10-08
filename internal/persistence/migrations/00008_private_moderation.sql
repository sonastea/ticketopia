-- +goose NO TRANSACTION
-- +goose Up
ALTER TABLE discussion_posts
    ADD COLUMN hidden_at DATETIME(6) NULL,
    ADD COLUMN review_version BIGINT NOT NULL DEFAULT 0;

CREATE TABLE account_roles (
    account_id VARBINARY(32) NOT NULL,
    role VARCHAR(32) NOT NULL,
    PRIMARY KEY (account_id, role),
    CONSTRAINT role_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    CHECK (role = 'moderator')
) ENGINE=InnoDB;

CREATE TABLE account_role_events (
    role_event_id VARBINARY(32) NOT NULL PRIMARY KEY,
    account_id VARBINARY(32) NOT NULL,
    role VARCHAR(32) NOT NULL,
    action VARCHAR(16) NOT NULL,
    operator_label VARCHAR(120) NOT NULL,
    database_user VARCHAR(320) NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    INDEX role_history (account_id, created_at),
    CONSTRAINT role_event_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    CHECK (role = 'moderator'),
    CHECK (action IN ('grant', 'revoke'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE moderation_decisions (
    decision_id VARBINARY(32) NOT NULL PRIMARY KEY,
    post_id VARBINARY(32) NOT NULL,
    moderator_id VARBINARY(32) NOT NULL,
    action VARCHAR(16) NOT NULL,
    reason TEXT NOT NULL,
    notes TEXT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    INDEX decision_post (post_id, created_at, decision_id),
    CONSTRAINT decision_post_fk FOREIGN KEY (post_id) REFERENCES discussion_posts(post_id),
    CONSTRAINT decision_moderator_fk FOREIGN KEY (moderator_id) REFERENCES accounts(account_id),
    CHECK (action IN ('keep', 'hide', 'restore'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE moderation_reports (
    report_id VARBINARY(32) NOT NULL PRIMARY KEY,
    post_id VARBINARY(32) NOT NULL,
    reporter_id VARBINARY(32) NOT NULL,
    reason VARCHAR(32) NOT NULL,
    context TEXT NOT NULL,
    reported_body TEXT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    decision_id VARBINARY(32) NULL,
    UNIQUE KEY report_retry (reporter_id, post_id),
    INDEX report_queue (post_id, decision_id),
    INDEX report_owner (reporter_id, created_at, report_id),
    INDEX report_history (post_id, created_at, report_id),
    CONSTRAINT report_post_fk FOREIGN KEY (post_id) REFERENCES discussion_posts(post_id),
    CONSTRAINT report_reporter_fk FOREIGN KEY (reporter_id) REFERENCES accounts(account_id),
    CONSTRAINT report_decision_fk FOREIGN KEY (decision_id) REFERENCES moderation_decisions(decision_id),
    CHECK (reason IN ('spam', 'abuse', 'private_information', 'other'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE moderation_appeals (
    decision_id VARBINARY(32) NOT NULL PRIMARY KEY,
    context TEXT NOT NULL,
    created_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    reviewed_by VARBINARY(32) NULL,
    CONSTRAINT appeal_decision_fk FOREIGN KEY (decision_id) REFERENCES moderation_decisions(decision_id),
    CONSTRAINT appeal_review_fk FOREIGN KEY (reviewed_by) REFERENCES moderation_decisions(decision_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- +goose Down
DROP TABLE moderation_appeals;
DROP TABLE moderation_reports;
DROP TABLE moderation_decisions;
DROP TABLE account_role_events;
DROP TABLE account_roles;
ALTER TABLE discussion_posts DROP COLUMN hidden_at, DROP COLUMN review_version;
