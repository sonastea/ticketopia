-- Local development verification, executed as ticketopia_runtime after grants.
-- Every DML statement affects zero rows; rollback also protects future edits.
-- Keep this list aligned with runtime-grants.sql when adding domain tables.
START TRANSACTION;

SELECT id, dirty, target_version FROM schema_state WHERE id = 1;
SELECT version_id FROM goose_db_version ORDER BY id DESC LIMIT 1;

INSERT INTO events SELECT * FROM events WHERE FALSE;
UPDATE events SET name = name WHERE FALSE;
INSERT INTO event_providers SELECT * FROM event_providers WHERE FALSE;
UPDATE event_providers SET event_id = event_id WHERE FALSE;
INSERT INTO accounts SELECT * FROM accounts WHERE FALSE;
UPDATE accounts SET display_name = display_name WHERE FALSE;
INSERT INTO account_identities SELECT * FROM account_identities WHERE FALSE;
INSERT INTO account_credentials SELECT * FROM account_credentials WHERE FALSE;
DELETE FROM account_credentials WHERE FALSE;
INSERT INTO auth_flows SELECT * FROM auth_flows WHERE FALSE;
DELETE FROM auth_flows WHERE FALSE;
INSERT INTO auth_rate_limits SELECT * FROM auth_rate_limits WHERE FALSE;
UPDATE auth_rate_limits SET attempts = attempts WHERE FALSE;
DELETE FROM auth_rate_limits WHERE FALSE;
INSERT INTO event_snapshots SELECT * FROM event_snapshots WHERE FALSE;
UPDATE event_snapshots SET snapshot = snapshot WHERE FALSE;
INSERT INTO saved_events SELECT * FROM saved_events WHERE FALSE;
DELETE FROM saved_events WHERE FALSE;

ROLLBACK;
