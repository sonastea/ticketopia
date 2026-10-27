# Local development service changes

## Unreleased

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
