.PHONY: help check migrate db-setup

help:
	@printf '%s\n' \
		'make check     Build assets and run Go race tests/vet in Docker.' \
		'make migrate   Apply schema migrations to the configured database only.' \
		'make db-setup  Prepare local MariaDB, apply migrations and grants, and verify runtime access.'

# Validate the current working tree before committing, using the publishing checks.
check:
	@docker buildx build --target validate --no-cache-filter validate .

# Use existing environment/.env database settings; no container or user setup.
migrate:
	@go run ./cmd/ticketopia migrate

# Local development only; do not redirect this workflow to a shared database.
db-setup:
	@sh scripts/mariadb-setup.sh
