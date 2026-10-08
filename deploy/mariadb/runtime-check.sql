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
INSERT INTO event_interests SELECT * FROM event_interests WHERE FALSE;
UPDATE event_interests SET visibility = visibility WHERE FALSE;
DELETE FROM event_interests WHERE FALSE;
INSERT INTO event_recommendations SELECT * FROM event_recommendations WHERE FALSE;
UPDATE event_recommendations SET reason = reason WHERE FALSE;
DELETE FROM event_recommendations WHERE FALSE;
INSERT INTO recommendation_feed_events SELECT * FROM recommendation_feed_events WHERE FALSE;
UPDATE recommendation_feed_events SET first_recommended_at=first_recommended_at WHERE FALSE;
INSERT INTO recommendation_activity SELECT * FROM recommendation_activity WHERE FALSE;
UPDATE recommendation_activity SET new_publications=new_publications WHERE FALSE;
INSERT INTO discussion_posts SELECT * FROM discussion_posts WHERE FALSE;
UPDATE discussion_posts SET body = body WHERE FALSE;
INSERT INTO post_helpful SELECT * FROM post_helpful WHERE FALSE;
UPDATE post_helpful SET post_id = post_id WHERE FALSE;
DELETE FROM post_helpful WHERE FALSE;
SELECT account_id, role FROM account_roles LIMIT 0;
SELECT role_event_id FROM account_role_events LIMIT 0;
INSERT INTO moderation_reports SELECT * FROM moderation_reports WHERE FALSE;
UPDATE moderation_reports SET decision_id = decision_id WHERE FALSE;
INSERT INTO moderation_decisions SELECT * FROM moderation_decisions WHERE FALSE;
INSERT INTO moderation_appeals SELECT * FROM moderation_appeals WHERE FALSE;
UPDATE moderation_appeals SET reviewed_by = reviewed_by WHERE FALSE;
INSERT INTO artists SELECT * FROM artists WHERE FALSE;
UPDATE artists SET snapshot=snapshot WHERE FALSE;
INSERT INTO artist_providers SELECT * FROM artist_providers WHERE FALSE;
UPDATE artist_providers SET artist_id=artist_id WHERE FALSE;
INSERT INTO venues SELECT * FROM venues WHERE FALSE;
UPDATE venues SET snapshot=snapshot WHERE FALSE;
INSERT INTO venue_providers SELECT * FROM venue_providers WHERE FALSE;
UPDATE venue_providers SET venue_id=venue_id WHERE FALSE;
INSERT INTO event_history_state SELECT * FROM event_history_state WHERE FALSE;
UPDATE event_history_state SET last_seen=last_seen WHERE FALSE;
INSERT INTO event_observations SELECT * FROM event_observations WHERE FALSE;
INSERT INTO collection_tasks SELECT * FROM collection_tasks WHERE FALSE;
UPDATE collection_tasks SET next_refresh=next_refresh WHERE FALSE;
INSERT INTO collection_runs SELECT * FROM collection_runs WHERE FALSE;
UPDATE collection_runs SET status=status WHERE FALSE;
INSERT INTO collection_pages SELECT * FROM collection_pages WHERE FALSE;
INSERT INTO collection_run_events SELECT * FROM collection_run_events WHERE FALSE;
INSERT INTO provider_budgets SELECT * FROM provider_budgets WHERE FALSE;
UPDATE provider_budgets SET used=used WHERE FALSE;

ROLLBACK;
