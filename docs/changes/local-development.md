# Local development service changes

## Unreleased

### 2026-11-15 — Throwaway accounts for local agent testing

- `make db-setup` ends by creating throwaway test accounts, skipping them with a
  message (setup still succeeds) when `AUTH_BASE_URL` isn't a localhost address;
  `make local-test-accounts` does the same on its own.
- `make local-test-accounts` creates `owner` and `other` in the local database
  through the app's sign-in, token and OAuth services, writing sessions, API
  tokens and OAuth access tokens (`read write publish` and `read`, as "Local
  Test Agent") to `.local/local-test-accounts.json`; rerunning replaces them.
  `make local-test-accounts-clean` removes them and everything they made in one
  transaction. The separate `cmd/ticketopia-local-accounts` tool is not in the
  image, refuses non-loopback `AUTH_BASE_URL`/`DB_HOST`, matches only accounts
  marked by both a non-numeric subject and an `@local-test.invalid` email, and
  refuses accounts with moderation or role records. MariaDB tests cover
  credentials, replacement, complete removal, untouched real accounts and every
  account-owned table. See [agent tools](../app-mcp.md#throwaway-accounts-on-your-local-database).

### 2026-11-08 — Review scenarios, shared fixtures and MCP access

- Select review fixtures with `make review-server scenario=<name>`:
  `event-edge-cases`, `event-data-gaps` and `reminder-states` add to the baseline.
  Signed-out review uses the baseline without storage state.
- Move the review server into `internal/reviewfixture` and disposable schema,
  user and grant setup into `internal/persistence/persistencetest`, shared with
  the opt-in MariaDB suite. Review servers refuse non-loopback database addresses.
- Add `make review-mcp`, an MCP stdio server with `scenario_list`,
  `scenario_apply` and `manifest`. It cleans up on client disconnect or signal.
- `discover-last-known` shows the stored-Discover omitted-event labeling: fresh
  complete coverage, one removed event labeled on its row, no list banner.
- `reminder-states` now covers every reminder state, and `reminder-email` sends
  one reminder through MiniStack, readable with the MCP `ses_messages` tool.
- `make review-clean` drops marked fixture schemas left by killed processes;
  running fixtures hold a kept-alive MariaDB named lock and are kept, and
  unmarked look-alikes are only reported. A test confirms fixtures make
  no non-loopback requests even with provider settings exported.
- `make db-setup` reissues local MariaDB TLS fixtures before they expire and
  reloads them in the running server.
  See [testing](../testing.md#review-fixtures-over-mcp).

### 2026-10-27 — Signed-in manual review server

- `make review-server` serves production handlers over an isolated MariaDB
  fixture schema with three accounts signed in through the real login service,
  seeded saves, interest, reminders, discussions, notifications and a report,
  plus Playwright storage states. Ctrl-C drops the schema.
  See [testing](../testing.md#signed-in-manual-review-server).

### 2026-10-10 — Token-free MiniStack SESv2 capture

- Verify LocalStack Community 4.14.0 rejects SESv2 and the active free-account
  license on the newer image also excludes it. Replace local SES setup with pinned
  MiniStack 1.5.24 without changing the production SESv2 client.
- Retain loopback-only access, dummy credentials, idempotent identity setup and
  sequential failure handling; disable SMTP relay and emulator persistence.
- Update captured-message checks and [local settings](../local-development.md),
  removing unnecessary token/credential/default variables from the local example.
- Rename the sender variable to `REMINDERS_EMAIL_FROM` to match `REMINDERS_ENABLED`;
  reuse the existing auth base URL, leaving three reminder-specific local settings.
- Verify real sender bootstrap and the Go SESv2 send/capture smoke test against
  MiniStack without real AWS credentials or delivered email.

### 2026-10-09 — Combined database and SES emulator setup

- Add `make ses-setup` for pinned LocalStack SESv2 capture, seeded test identity,
  loopback networking and explicit token readiness; retain standalone `db-setup`.
- Add sequential, fail-fast `make local-setup` without modifying `.env`, resetting
  data, forwarding AWS credentials or mounting the Docker socket into LocalStack.
- Document [local settings](../local-development.md), captured email inspection,
  required external API/OAuth credentials and the production boundary.
- Cover workflow isolation/failures/idempotent identity setup and local SDK requests
  with dummy credentials; include fixture inputs in Docker validation.
