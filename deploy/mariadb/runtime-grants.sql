-- Run as the local admin AFTER migrations. Repeatable and no runtime DDL.
GRANT INSERT, UPDATE ON ticketopia.events TO 'ticketopia_runtime'@'%';
GRANT INSERT, UPDATE ON ticketopia.event_providers TO 'ticketopia_runtime'@'%';
GRANT INSERT, UPDATE ON ticketopia.accounts TO 'ticketopia_runtime'@'%';
GRANT INSERT ON ticketopia.account_identities TO 'ticketopia_runtime'@'%';
GRANT INSERT, DELETE ON ticketopia.account_credentials TO 'ticketopia_runtime'@'%';
GRANT INSERT, DELETE ON ticketopia.auth_flows TO 'ticketopia_runtime'@'%';
GRANT INSERT, UPDATE, DELETE ON ticketopia.auth_rate_limits TO 'ticketopia_runtime'@'%';
GRANT INSERT, UPDATE ON ticketopia.event_snapshots TO 'ticketopia_runtime'@'%';
GRANT INSERT, DELETE ON ticketopia.saved_events TO 'ticketopia_runtime'@'%';
GRANT INSERT, UPDATE, DELETE ON ticketopia.event_interests TO 'ticketopia_runtime'@'%';
GRANT INSERT, UPDATE, DELETE ON ticketopia.event_recommendations TO 'ticketopia_runtime'@'%';
GRANT INSERT, UPDATE ON ticketopia.recommendation_feed_events TO 'ticketopia_runtime'@'%';
GRANT INSERT, UPDATE ON ticketopia.recommendation_activity TO 'ticketopia_runtime'@'%';
GRANT INSERT, UPDATE ON ticketopia.discussion_posts TO 'ticketopia_runtime'@'%';
GRANT INSERT, UPDATE, DELETE ON ticketopia.post_helpful TO 'ticketopia_runtime'@'%';
