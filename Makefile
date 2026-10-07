.PHONY: help migrate db-setup

help:
	@printf '%s\n' \
		'make migrate   Apply schema migrations to the configured database only.' \
		'make db-setup  Prepare local MariaDB, apply migrations and grants, and verify runtime access.'

# Use existing environment/.env database settings; no container or user setup.
migrate:
	@go run ./cmd/ticketopia migrate

# Local development only; do not redirect this workflow to a shared database.
db-setup:
	@sh scripts/mariadb-setup.sh
