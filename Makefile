.PHONY: help check migrate db-setup ses-setup local-setup review-server

REVIEW_SERVER_ADDR ?= 127.0.0.1:8090

help:
	@printf '%s\n' \
		'make check     Build assets and run Go race tests/vet in Docker.' \
		'make migrate   Apply schema migrations to the configured database only.' \
		'make db-setup  Prepare local MariaDB, apply migrations and grants, and verify runtime access.' \
		'make ses-setup Start local SES emulation in MiniStack and seed the test sender.' \
		'make local-setup Prepare MiniStack SES and MariaDB for local development.' \
		'make review-server Serve signed-in fixture accounts on an isolated schema until Ctrl-C.'

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

# Local manual review only: isolated fixture schema, dropped on Ctrl-C.
review-server:
	@MARIADB_TEST_ADDR=$${MARIADB_TEST_ADDR:-127.0.0.1:3307} REVIEW_SERVER_ADDR=$(REVIEW_SERVER_ADDR) \
		go test -count=1 -timeout 0 -run '^TestMariaDBReviewServer$$' -v ./internal/persistence
