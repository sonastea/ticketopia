.PHONY: help check migrate db-setup ses-setup local-setup review-server review-mcp review-mcp-config review-clean

REVIEW_SERVER_ADDR ?= 127.0.0.1:8090
scenario ?= baseline

help:
	@printf '%s\n' \
		'make check     Build assets and run Go race tests/vet in Docker.' \
		'make migrate   Apply schema migrations to the configured database only.' \
		'make db-setup  Prepare local MariaDB, apply migrations and grants, and verify runtime access.' \
		'make ses-setup Start local SES emulation in MiniStack and seed the test sender.' \
		'make local-setup Prepare MiniStack SES and MariaDB for local development.' \
		'make review-server Serve signed-in fixture accounts on an isolated schema until Ctrl-C.' \
		'                   Add scenario=<name> (event-edge-cases, event-data-gaps, discover-last-known, reminder-states, reminder-email).' \
		'make review-mcp    Serve the review fixtures to an MCP client over stdio (local only).' \
		'make review-mcp-config Print MCP client setup for this checkout.' \
		'make review-clean  Drop fixture schemas left by killed review servers or tests (local only).'

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
		go test -count=1 -timeout 0 -run '^TestMariaDBReviewServer$$' -v ./internal/persistence \
		-args '-review.scenario=$(scenario)'

# Local manual review only: MCP stdio server for review scenarios. Stdout is the
# protocol, so the build stays quiet and the binary replaces this process.
review-mcp:
	@go build -o bin/ticketopia-review-mcp ./cmd/ticketopia-review-mcp
	@exec bin/ticketopia-review-mcp -addr $(REVIEW_SERVER_ADDR)

# Print MCP client setup with this checkout's path filled in; changes nothing.
review-mcp-config:
	@printf '%s\n' \
		'OpenCode: opencode.json in this checkout already registers ticketopia-review.' \
		'' \
		'Claude Code:' \
		'  claude mcp add ticketopia-review -- make -s -C "$(CURDIR)" review-mcp' \
		'' \
		'Other MCP clients (stdio server JSON):' \
		'  "ticketopia-review": {"command": "make", "args": ["-s", "-C", "$(CURDIR)", "review-mcp"]}'

# Local only: running fixtures hold a lock and are kept.
review-clean:
	@go run ./cmd/ticketopia-review-mcp -clean-stale
