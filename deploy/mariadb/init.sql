-- These credentials are deliberately public fixtures, never production secrets.
CREATE DATABASE IF NOT EXISTS ticketopia CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
CREATE USER IF NOT EXISTS 'ticketopia_runtime'@'%' IDENTIFIED BY 'local-runtime-only' REQUIRE SSL;
CREATE USER IF NOT EXISTS 'ticketopia_migration'@'%' IDENTIFIED BY 'local-migration-only' REQUIRE SSL;
GRANT SELECT ON ticketopia.* TO 'ticketopia_runtime'@'%';
GRANT SELECT, INSERT, UPDATE, DELETE, CREATE, ALTER, DROP, INDEX, REFERENCES ON ticketopia.* TO 'ticketopia_migration'@'%';
