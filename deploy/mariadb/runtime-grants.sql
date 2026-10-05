-- Run as the local admin AFTER migrations. Repeatable and no runtime DDL.
GRANT INSERT, UPDATE ON ticketopia.events TO 'ticketopia_runtime'@'%';
GRANT INSERT, UPDATE ON ticketopia.event_providers TO 'ticketopia_runtime'@'%';
