# Private reporting and moderation

Implemented for [discussion questions and replies](event-discussions.md), with
opt-in [accounts](accounts.md) and clean MariaDB schema **8**. Reports request
review, not downvotes. Neither report volume nor a report itself hides content.
Recommendations/profile reporting, notifications, account warnings, and
participation restrictions are not part of this release.

## Report, review, and close the loop

- Use **Report privately** on another person's contribution in a conversation.
  Choose spam/scam, abusive behaviour, private information, or another concern;
  optional context allows up to 2000 Unicode characters of plain text.
- **Profile → Your reports** (`/me/reports`) acknowledges received reports and
  shows pending/reviewed status with a shared decision reason. One immutable report
  per reporter/contribution is retained; retries return it unchanged, even after
  hiding or author removal.
- Moderators use **Profile → Moderator review queue** (`/moderation`). Pending
  cases have unresolved reports or author requests; **All cases** includes resolved
  cases. Ordering is newest contribution creation, never report volume or replies.
- A case includes event/current contribution context, private reported text as it
  appeared at reporting time, concerns, author requests, and decision history.
  Follow **Read the full conversation** before deciding. Each history is independently
  paginated, default 20/max 100. Decisions close all currently pending reports/requests
  for that contribution, including those on other history pages.
- **Keep**, **Hide**, or **Restore** requires a reason shared with reporters and the
  affected author. Optional notes stay moderator-only. Never put reporter identities
  or sensitive evidence in the shared reason. Moderators cannot read or decide their
  own cases, which are excluded from their queue; another moderator must handle them.
- **Profile → Moderation outcomes** (`/me/moderation`) shows hide/restore reasons
  on your contributions. **Request another review** accepts private context against
  the latest hiding decision while the post remains hidden and not author-removed.
  One request per decision is kept, with safe retries. Requests return the case to
  the pending queue; another review does not promise a different moderator.

Ordinary forms work without JavaScript. Rejected writes retain escaped drafts;
expired-session recovery advises copying them before signing in. Decisions carry
the reviewed case version and contribution update time. New reports, appeals,
author edits/removal, or decisions invalidate stale submissions with 409 instead
of silently resolving unseen activity. Repeated decision submits also return 409;
check history before deciding again after an uncertain response.

Outcomes are in-app only: no email, push, polling, or notification jobs. Reporters
see their own concern and resolution, not other reports, moderator identity/notes,
or private author appeal metadata. Authors never see reporter identity/context or
moderator notes. Moderator views contain account IDs, not emails, preferences,
credentials, private saves, or private interest identities. All reporting/moderation
responses are `private, no-store`.

Personal records identify the event and contribution type/creation time without
quoting hidden text. Direct links select the exact contribution or placeholder,
including replies outside the current conversation page.

## Hiding, restoration, and retention

Moderator hiding is separate from irreversible **author removal**. Public web/API
projections withhold a hidden post's body, attribution, Helpful state and counts.
Visible counts exclude hidden posts; roots with visible replies remain navigable
as placeholders. Withheld reply targets say **Hidden contribution** or **Removed
contribution**. Remaining replies stay readable.

Authors cannot edit hidden text to bypass review; setting Helpful is rejected, but
removing your Helpful choice is permitted. Hiding retains body/reactions for
restoration and does not change author publication/edit times. An author can still
remove their own hidden post through the existing API; removal clears text/reactions
and cannot be undone by restoration.

Reports retain a private original-text snapshot, including after author removal.
Decisions retain shared reasons, private notes, moderator identity and UTC time.
These records and role audit events are durable, with no runtime DELETE grants;
this is not a complete legal retention/deletion policy. Restrict operator/database
access and protect backups as private community data.

## Bootstrap and manage moderator access

No account starts as a moderator. There is no first-signup promotion, default
administrator/password, email-based authorization, or role-management web API.
`account_roles` keys the named `moderator` role by stable Ticketopia account ID;
ordinary authenticated membership is not a stored role. Other roles are later work.
Email does not establish or link identity in this app.

1. Enable accounts normally and sign in with the intended moderator's Google account.
2. Copy its 32-character ID from the public-profile link or authenticated
   `GET /api/v1/me` response. Do not use email or a Google subject as the ID.
3. In an operator environment configured for the intended database and TLS, use
   the release binary with **separate `DB_MIGRATION_USER` /
   `DB_MIGRATION_PASSWORD` credentials**, as for migrations:

   ```sh
   ticketopia moderator grant --account-id YOUR_32_CHARACTER_ACCOUNT_ID --operator "owner / bootstrap"
   ticketopia moderator revoke --account-id YOUR_32_CHARACTER_ACCOUNT_ID --operator "owner / revoke access"
   ```

   Development can use `go run ./cmd/ticketopia moderator ...`. Arguments contain
   IDs/an audit label, never passwords. The command validates an already-migrated
   schema and existing account; it does not migrate, create users, change credentials,
   or issue sessions/tokens.

Changes atomically record account/role, grant/revoke action, the supplied operator
label, authenticated MariaDB `CURRENT_USER()`, and UTC time in `account_role_events`.
The label is descriptive, not a separately verified human identity. No-op retries
create no extra events. Prefer this CLI to ad hoc SQL so validation and auditing
stay together.

Runtime has SELECT-only access to role membership/audit tables. Moderators cannot
appoint moderators. Every privileged request reads current membership, with another
transactional check before decisions. Revocation needs no restart; a transaction
already holding authorization may finish before revoke commits. Existing account
credentials remain valid for ordinary, nonmoderator use.

In Kubernetes, run the current-image command in an approved short-lived operator
Job/shell with the operator secret and CA mount. Keep migration credentials out of
normal application pods/logs, and restrict who can create that Job/read its secret.
This is a documented procedure, not an exercised cluster rollout.

## Schema upgrade and runtime grants

This binary supports **clean schema 8 only**. Drain old traffic, preserve a backup,
run `ticketopia migrate` with migration credentials, reconcile runtime grants, then
start schema-8 apps. No startup migration or zero-downtime cross-version support is
claimed; see [persistence](persistence.md#rolling-update-compatibility).

Schema 8 adds `hidden_at` / `review_version` to posts, roles/role audits, reports,
append-only decisions, and author requests. Prior posts, Helpful, retry identities,
sessions, and other personal/community state are retained. Runtime needs SELECT
on all tables, INSERT/UPDATE on `moderation_reports` / `moderation_appeals`, and
INSERT-only on `moderation_decisions`. It needs **no role table writes**. Local/operator
source grants and zero-row probes are updated; the running database changes only
when an operator explicitly applies them.

## API

The [OpenAPI contract](openapi.md) is version **1.8.0**.

| Method | Route | Purpose |
| --- | --- | --- |
| POST | `/api/v1/posts/{post_id}/reports` | Private report/unchanged retry receipt |
| GET | `/api/v1/me/reports` | Own receipts and resolutions |
| GET | `/api/v1/me/moderation` | Author-only actions/appeal status |
| POST | `/api/v1/me/moderation/{decision_id}/appeal` | Author request for another review |
| GET | `/api/v1/moderation` | Moderator queue, `state=pending` or `all` |
| GET | `/api/v1/moderation/posts/{post_id}` | Private case/evidence/histories |
| POST | `/api/v1/moderation/posts/{post_id}/decisions` | Authorized keep/hide/restore |

Cookie/bearer credentials share current role checks. Cookie writes require CSRF
and same-origin checks; bearer identity takes precedence. Strict JSON rejects
unknown/repeated/null fields and bodies over 16 KiB. Writes accept no query parameters.
Report and appeal attempts each allow 20/account/ten minutes, shared across web/API
through durable account counters. Native report/appeal forms allow 32 KiB; decisions
allow 64 KiB for both Unicode text fields.

Report JSON is `{"reason":"spam","context":"Optional explanation"}`; creation
returns 201/retry 200. Appeals take `{"context":"Please reconsider ..."}` and return
204 on success/retry. Decisions take `action`, required `reason`, optional `notes`,
and required `expected_version` / `expected_updated_at` from the case; success is
201. Keep requires a nonhidden post; removed posts permit keep/no-further-action
only. Missing/unowned records return 404, unauthorized review 403, stale context 409.

Collections return `items` / nullable `next_cursor`, newest first with identity
tie-breakers. Cursors bind to collection/viewer/queue state. Case histories return
`next_reports_cursor`, `next_decisions_cursor`, and `next_appeals_cursor`; send the
corresponding `reports_cursor`, `decisions_cursor`, or `appeals_cursor`. These are
live histories, not frozen snapshots.

## Verification and remaining scope

SQL tests cover schema-7 preservation, role audit/runtime write denial, immediate
revocation, duplicate report races, stale/competing decisions, rollback, scoped
pagination, withheld attribution/counts, removal precedence, privacy, appeals,
restoration, and fresh-pool/provider-free reads. HTTP tests cover credentials/roles,
strict JSON, CSRF, unavailable state, escaped drafts, and safe sign-in returns.

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 go test -race ./...
go vet ./...
npm run build
npx --yes @redocly/cli lint internal/api/openapi.yaml
```

Optional Chromium/axe verification uses `scripts/moderation-browser.cjs` and
`TestMariaDBModerationBrowserReview`, with `MODERATION_BROWSER_SCRIPT`,
`PLAYWRIGHT_MODULE`, and `AXE_SCRIPT` configured like the
[recommendation fixture](event-recommendations.md#verification). Use `go test -count=1`
after changing the external script. The synthetic isolated journey passed 51 checks
and 13 zero-violation accessibility captures across desktop/tablet/390px/320px/200%
text, with native writes, stale review/drafts, privacy, hiding/restoration, appeals,
and forced-colors focus/reduced-motion checks.

The independent finish-review verdict is **ship** for the three scored fixes:
moderator-author evidence isolation, recognizable personal records/direct targets,
and accurate delivered-feature documentation. It is not a new whole-surface review.

This completes the basic loop, not public-pilot approval, whole-application
accessibility certification, operational moderation coverage, notification delivery,
production backup/restore, or multi-replica deployment verification.
