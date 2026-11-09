# Behavior-focused testing

Choose tests by the regression they prevent, not a coverage percentage or a
target test count. Keep a test when failure could expose private data, bypass
authorization, lose or duplicate durable state, send an unwanted email, or
misrepresent event details. A short helper test is not automatically unnecessary.

## Test at the boundary that owns the behavior

- **Service unit tests:** visibility defaults and explicit opt-in, retained/offline
  behavior, scheduling instants, and handling unavailable dependencies. Use narrow
  stubs that return outcomes; do not recreate SQL authorization or transactions.
- **HTTP tests:** authenticated ownership derived from credentials, CSRF, moderator
  decisions, safe returns, error statuses, and `429` with `Retry-After`. Checking
  that an arbitrary unknown query parameter returns `400` does not prove ownership;
  retain actual cross-account read/write checks instead.
- **Rendering tests:** visible facts, honest unavailable/TBA states, privacy-safe
  content and usable native controls. Assert text and semantic contracts rather
  than full HTML snapshots or incidental CSS classes. Cover price formatting at
  its rendered use instead of duplicating the same table against a helper.
- **Real MariaDB tests:** SQL ownership, concurrent reminder claims, moderation
  version races, literal keyword matching, migrations and atomic writes. A fake
  repository cannot prove these invariants. Keep fixtures that distinguish correct
  behavior from the likely bug: literal `%`/`_` searches need matching decoys that
  unescaped wildcards would wrongly include.

Use complementary tests across boundaries, not a blanket "one layer only" rule.
For example, a service test proves interest policy; an HTTP test proves the
handler supplies the authenticated account's profile default. A service test
that stubs ownership rejection is not a replacement for the SQL ownership test.

Avoid new exhaustive schema-constant boundary matrices, thin-wrapper success
tests, trivial empty-input tests, and repeated harmless unknown-query rejection
cases unless they protect an identified behavioral risk. Keep security-sensitive
input ambiguity, cursor audience/filter binding, and safe return validation.
Never delete useful tests merely because an integration test exercises the same
function somewhere in a larger journey.

## Run the relevant checks

During a local change, run the affected packages, for example:

```sh
go test ./internal/interests ./internal/reminders ./internal/api ./views/home
```

For final validation across the suite:

```sh
go test -race ./...
go vet ./...
make check
```

`make check` validates the Docker publishing context, assets, race tests and vet.
It does **not** automatically enable the opt-in real database or browser tests.
On the isolated [local MariaDB service](persistence.md#repeatable-local-development):

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 go test -race ./internal/persistence
```

Database fixtures create and clean up random schemas/users. Never use a shared
or production database. Browser reviews and emulator checks have additional
opt-in variables documented in the corresponding feature guides. Report skipped
checks explicitly; a passing default suite is not evidence they ran.

## Signed-in manual review server

For manual or agent-driven review of signed-in screens (phone widths, enlarged
text, forced colors, keyboard), serve the production handlers over an isolated
fixture schema:

```sh
make review-server
```

It runs `TestMariaDBReviewServer` with `MARIADB_TEST_ADDR=127.0.0.1:3307` on
`REVIEW_SERVER_ADDR` (default `127.0.0.1:8090`; loopback only). Three accounts
(`owner`, `other`, `moderator`) sign in through the real login service with the
fixture provider. There is no runtime auth bypass, and CSRF checks still apply.
Seeded state covers six Chicago events (including long names), saves, public and
private interest, recommendations, on-sale reminders, a followed discussion
with replies, Helpful reactions and unread reply notifications, and a reported
post in the moderator queue. Ticketmaster and IP lookup stay off; events come
from cache fixtures. `MARIADB_TEST_ADDR` must be a loopback address because the
server creates a schema and users.

Add a named scenario to seed extra states on top of that baseline:

```sh
make review-server scenario=event-edge-cases
```

- `baseline` (default): the state above.
- `event-edge-cases`: two more saved events, which also appear in Discover. One is
  rescheduled, with a long title and no price or presale details. The other is
  canceled. The rescheduled event has a busy discussion that the owner follows:
  nested replies, Helpful reactions, unread replies and a long post.
- `event-data-gaps`: a saved event with no image or venue, last refreshed a week
  ago. Its detail cache entry is omitted, so the page falls back to the durable
  snapshot and shows its age. `unavailable_event` is neither cached nor saved and
  returns 503 while the provider is off.
- `reminder-states`: the owner's reminders page shows every state the reminder
  worker can reach: scheduled (due in about 30 minutes), sending, waiting to
  retry, SES accepted, failed, uncertain, expired, awaiting a sale date, stopped
  for a canceled event, and refresh failed. One more reminder is stopped, so its
  event page shows none. `other` has notifications paused, so its reminder shows
  Paused. The real worker produces these states without a clock override; a
  fixture sender returns each outcome, and no email is sent.
- `reminder-email`: sends one real on-sale reminder for the owner through the
  production SES sender in local emulator mode (MiniStack from `make ses-setup`;
  dummy credentials, never the AWS credential chain). The reminder shows SES
  accepted. Fixture email addresses contain the manifest's `run` token, so this
  run's message can be told apart from others in the capture endpoint. Override
  the emulator with `-args -review.ses-endpoint=...` on `go test`; the default is
  `http://localhost:4566`.

For the signed-out view, open any path in a browser context without storage
state; use `paths.discover`, since `/` without an account asks for a location.

An unknown scenario name fails before any schema is created. Scenarios are fixed;
to start over, stop the server and run it again.

`REVIEW_SERVER_STATE_DIR` (default `$TMPDIR/ticketopia-review`) receives
`manifest.json` (base URL, account IDs and emails, routes, event and thread IDs,
plus `scenario`, `run` and the `scenario_paths`) and one
Playwright storage-state file per account, for example
`browser.newContext({ storageState: manifest.accounts.owner.storage_state })`.
Writes go to the throwaway schema, so reviewers may save, post and report. Stop
with Ctrl-C to drop the schema and users and remove the state files. A killed
process leaves its `ticketopia_test_*` schema and users behind; remove them with:

```sh
make review-clean
```

Fixture schemas carry a marker comment, and each running fixture (review
server, MCP server or opt-in test) holds a MariaDB named lock on a dedicated
connection, pinged every minute so the server's idle timeout cannot release it.
Cleanup drops only marked schemas, with their users, whose lock has been
released, so running fixtures are safe. It reports look-alike
`ticketopia_test_*` schemas without the marker (from older code or other tools)
and leaves them for you to drop manually.

The fixtures live in `internal/reviewfixture`; disposable schemas, users and
runtime grants come from `internal/persistence/persistencetest`, shared with the
opt-in MariaDB suite. Neither is imported by `cmd/ticketopia`.
`MARIADB_TEST_ADDR=127.0.0.1:3307 go test ./internal/reviewfixture` starts every
scenario and checks its paths for each account and signed out. It also checks
that every reminder state renders, and that no request leaves loopback even with
`TICKETMASTER_KEY` and IP lookup enabled. Add `SES_TEST_ENDPOINT=http://localhost:4566`
to include `reminder-email`.

### Review fixtures over MCP

`make review-mcp` builds `bin/ticketopia-review-mcp` and serves the same
fixtures to an MCP client over stdio. Register it with the repository root as the
working directory, for example:

```json
{ "command": "make", "args": ["-s", "review-mcp"] }
```

- `scenario_list`: scenario names and descriptions.
- `scenario_apply {"name": ...}`: starts the review server on a fresh schema
  seeded with that scenario and returns the manifest. It replaces any running
  server, so applying the same name resets it. Earlier sessions, storage states
  and IDs become invalid.
- `manifest`: the running server's manifest, or `running: false`.
- `ses_messages`: emails MiniStack captured for the running scenario's accounts,
  oldest first. Only `reminder-email` sends one.

The server starts only on `scenario_apply`. Flags (defaults in parentheses):
`-addr` (`127.0.0.1:8090`; `make review-mcp` passes `REVIEW_SERVER_ADDR`), `-state-dir`
(`$TMPDIR/ticketopia-review-mcp`), `-db-addr` (`127.0.0.1:3307`), `-db-ca`
(`.local/mariadb/certs/ca.crt`) and `-ses-endpoint` (`http://localhost:4566`).
The listener and database addresses must be loopback IPs, and the SES endpoint
a loopback origin. `-clean-stale` runs the `make review-clean` cleanup and exits.
`MARIADB_TEST_ADMIN_USER`/`_PASSWORD` override the local administrator as in the
opt-in suite. Stdout carries only the protocol; diagnostics go to stderr. Closing
the client (stdin EOF), SIGINT or SIGTERM stops the review server, drops its
schema and users and removes the state files; a killed process leaves its schema
behind for `make review-clean`. `MARIADB_TEST_ADDR=127.0.0.1:3307 go test ./cmd/ticketopia-review-mcp`
covers apply, reset and disconnect cleanup.
