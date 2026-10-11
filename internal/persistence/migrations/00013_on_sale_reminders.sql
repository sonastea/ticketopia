-- +goose NO TRANSACTION
-- +goose Up
CREATE TABLE event_reminders (
    reminder_id VARBINARY(32) NOT NULL PRIMARY KEY,
    account_id VARBINARY(32) NOT NULL,
    event_id VARBINARY(300) NOT NULL,
    lead_minutes INT NOT NULL,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    state VARCHAR(24) NOT NULL DEFAULT 'pending_date',
    sale_at DATETIME(6) NULL,
    scheduled_at DATETIME(6) NULL,
    next_check DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    lease_until DATETIME(6) NULL,
    token VARBINARY(32) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT UTC_TIMESTAMP(6),
    UNIQUE KEY reminder_owner_event (account_id,event_id),
    INDEX reminder_due (active,next_check),
    CONSTRAINT reminder_account_fk FOREIGN KEY (account_id) REFERENCES accounts(account_id),
    CONSTRAINT reminder_event_fk FOREIGN KEY (event_id) REFERENCES events(event_id),
    CONSTRAINT reminder_lead_check CHECK (lead_minutes BETWEEN 1 AND 10080)
) ENGINE=InnoDB;

CREATE TABLE reminder_jobs (
    job_id VARBINARY(32) NOT NULL PRIMARY KEY,
    reminder_id VARBINARY(32) NOT NULL,
    sale_at DATETIME(6) NOT NULL,
    scheduled_at DATETIME(6) NOT NULL,
    state ENUM('pending','sending','accepted','cancelled','expired','failed','uncertain') NOT NULL,
    attempts INT NOT NULL DEFAULT 0,
    attempted_at DATETIME(6) NULL,
    message_id VARCHAR(256) NOT NULL DEFAULT '',
    unsubscribe_hash BINARY(32) NULL,
    UNIQUE KEY reminder_occurrence (reminder_id,sale_at),
    UNIQUE KEY reminder_unsubscribe (unsubscribe_hash),
    INDEX reminder_inflight (state,attempted_at),
    CONSTRAINT reminder_job_setting_fk FOREIGN KEY (reminder_id) REFERENCES event_reminders(reminder_id)
) ENGINE=InnoDB;

CREATE TABLE reminder_dispatch (
    id TINYINT NOT NULL PRIMARY KEY,
    next_send DATETIME(6) NOT NULL,
    CONSTRAINT reminder_dispatch_singleton CHECK (id=1)
) ENGINE=InnoDB;
INSERT INTO reminder_dispatch (id,next_send) VALUES (1,UTC_TIMESTAMP(6));

-- +goose Down
DROP TABLE reminder_dispatch;
DROP TABLE reminder_jobs;
DROP TABLE event_reminders;
