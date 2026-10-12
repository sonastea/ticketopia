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
