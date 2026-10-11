# Public on-sale email reminders

Implemented with opt-in accounts, Amazon SES, and MariaDB **schema 13**. A Go
worker runs independently of browser visits. Saving, Interested, recommending,
posting, and following do **not** subscribe anyone. Meaningful event-change emails
and weekly discovery digests remain planned.

## Choose, change, pause, or stop

Save an event, then choose the bell-marked **On-sale reminder** button beside
Save and Interested (in event lists, previews, or the full event page). Configure
1–10080 minutes (up to seven days) before public tickets go on sale. The default
form value is 30 minutes, not an automatic subscription. Manage settings at
`/me/reminders`; each event has its own form and stop control.

In [Preferences](accounts.md), enable email reminders and turn off
Pause all notifications to permit delivery. Configuring a reminder does not
change those preferences. Account email stays private; dates in the form and
email use the account's time zone. Global pause also retains its existing effect
on in-app discussion notifications.

Unknown or TBD sale times stay pending. Known future sales whose lead time has
already elapsed can be reminded immediately after enrollment. Already-on-sale
events never produce a late reminder. Messages more than five minutes past their
scheduled time expire rather than catching up after an outage or long pause.
Observed time changes reconcile pending jobs; explicit cancellation/postponement
withholds delivery. Failed refreshes, missing results, and 404s never establish
cancellation: sending waits for fresh evidence instead.

Stop a reminder without removing the save, or remove the saved event to stop it
atomically. Saving it again does not reactivate the reminder. Each email includes
a reminder-specific stop link: GET shows confirmation, POST stops it without
sign-in. Its random capability is stored only as a hash. A message already being
sent or accepted cannot be recalled.

## Operator configuration

Delivery is **disabled by default**. Drain older binaries, apply migrations with
the separate migration credentials, and reapply runtime grants before starting
schema-13 apps; see [persistence](persistence.md). Never use `make db-setup` against
a shared service. The app does not provision AWS resources or change credentials.

For local SES API capture without real email, use `make ses-setup` (or
`make local-setup` for SES plus MariaDB) and the explicit loopback
`SES_ENDPOINT_URL` mode. See [local development](local-development.md) for the
token-free MiniStack setup, dummy credentials, localhost links and capture API.

Set these in the runtime environment only after approving live delivery:

```dotenv
REMINDERS_ENABLED=true
AWS_REGION=us-east-1 # Optional override; defaults to us-east-1 (N. Virginia)
REMINDERS_EMAIL_FROM=Ticketopia <notifications@your-verified-domain.example>
AUTH_BASE_URL=https://your-ticketopia.example
REMINDER_SEND_INTERVAL=1s
# Optional: a preconfigured SES event/reputation configuration set
SES_CONFIGURATION_SET=ticketopia-reminders
```

`AWS_REGION` defaults to `us-east-1` when unset or empty. Override it if your
SES verification and production approval are in another region.

Use your **production-approved SES region**, verified sender/domain and a restricted
AWS SDK credential chain (workload role, profile, or injected AWS credentials).
Grant only `ses:SendEmail` for the intended identity/region and necessary
configuration-set access. Do not commit credentials. Startup validates local
configuration and credential availability, not live SES acceptance. Accounts,
MariaDB and `TICKETMASTER_KEY` must also be enabled; refreshes share the existing
Ticketmaster request/budget coordinator.

The durable dispatch gate spaces attempts across replicas by the configured
interval (1s–1m); configure replicas consistently. Your 50,000-recipient rolling
24-hour SES allowance is shared with other projects in that AWS account/region;
it is not reserved for Ticketopia. Check the separate approved sending rate.
Explicit SES quota/throttle rejection defers the message and applies a shared
one-minute cooldown. Retries still expire at the original deadline. Capacity and
provider outages can therefore result in a missed reminder, not a late email.

Configure SES domain authentication, suppression, bounce/complaint monitoring and
reputation alerts before a real pilot. Optional configuration-set event publishing
can provide delivery evidence externally; Ticketopia does not ingest those events
or label provider acceptance as confirmed inbox delivery.

## Durability and outcomes

`event_reminders` stores private intent, schedule and token-fenced leases;
`reminder_jobs` stores one intended message per reminder/public-sale timestamp,
attempts and provider receipts; `reminder_dispatch` holds the shared pacing gate.
At send preparation the worker rechecks the current save, settings, account
preferences and public event snapshot. Fresh observations feed existing history.
Checks recur hourly (paused/failed-refresh settings retry after a minute).

SES SendEmail has **no idempotency key**. SDK automatic retries are disabled.
Explicit rejection can retry; transport errors, ambiguous server failures and
abandoned in-flight attempts become `uncertain` and are **not automatically
resent**. Changing lead times or stop/re-enable never replays accepted, failed,
expired or uncertain messages for the same sale timestamp. A genuinely changed
sale timestamp is a distinct occurrence. This avoids duplicate automatic sends,
but cannot guarantee exactly-once email delivery. Operators must review uncertain
receipts using SES evidence; there is no blind resend control.

## API and verification

- `GET /api/v1/me/reminders?limit=20&cursor=...`
- `GET /api/v1/me/reminders/{event_id}/public-on-sale`
- `PUT /api/v1/me/reminders/{event_id}/public-on-sale` with `{"lead_minutes":30}`
- `DELETE /api/v1/me/reminders/{event_id}/public-on-sale` with an empty body

Owner-only, private/no-store; strict inputs, owner/collection-bound pagination,
cookie CSRF/same-origin checks or personal bearer tokens. PUT requires the owner's
saved event. No recipient override or implicit global preference changes. See
[OpenAPI](openapi.md) for schemas and errors. HTML and JSON share one service.

Unit, handler and isolated MariaDB tests cover unknown/past/changed times,
pause/stop/unsave, leases across independent pools, restart receipts, stale refresh
withholding, expiration, explicit SES rejection and uncertain acceptance.
Synthetic browser fixtures verify native forms and responsive states without
sending live mail. A real SES recipient/inbox smoke test remains an operator step.
