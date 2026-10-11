.PHONY: help check migrate db-setup ses-setup local-setup

help:
	@printf '%s\n' \
		'make check     Build assets and run Go race tests/vet in Docker.' \
		'make migrate   Apply schema migrations to the configured database only.' \
		'make db-setup  Prepare local MariaDB, apply migrations and grants, and verify runtime access.' \
		'make ses-setup Start local SES emulation in MiniStack and seed the test sender.' \
		'make local-setup Prepare MiniStack SES and MariaDB for local development.'

# Validate the current working tree before committing, using the publishing checks.
check:
	@docker buildx build --target validate --no-cache-filter validate .

# Use existing environment/.env database settings; no container or user setup.
migrate:
	@go run ./cmd/ticketopia migrate

# Local development only; do not redirect this workflow to a shared database.
db-setup:
	@sh scripts/mariadb-setup.sh

ses-setup:
	@sh scripts/ses-setup.sh

# Sequential even with make -j; fail before DB work if SES prerequisites are missing.
local-setup:
	@$(MAKE) ses-setup
	@$(MAKE) db-setup
