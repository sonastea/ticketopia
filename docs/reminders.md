# Public on-sale email reminders

Implemented with opt-in accounts, Amazon SES, and MariaDB **schema 13**. A Go
worker runs independently of browser visits. Saving, Interested, recommending,
posting, and following do **not** subscribe anyone. Weekly discovery digests and
additional sale/price/event-change emails are out of scope. Ticket-provider
transactional notifications are not Ticketopia's responsibility; existing on-sale
delivery and dated in-app event history remain supported.

## Choose, change, pause, or stop

Save an event, then choose the bell-marked **On-sale reminder** button in the event
view's **Your plans & community** group. Lists keep Tickets and Save; settings
appear only after a confirmed save when reminders are supported. Choose **30
minutes**, **1 hour**, **1 day**, or **Custom** in a native select. Custom accepts
1–10080 whole minutes (up to seven days), retaining previously saved non-preset
values. The default is 30 minutes, not an automatic subscription. Manage settings at
`/me/reminders`; each event has its own form and stop control.

Settings lead with the event name and an **Event Overview** link, public ticket-sale
time (not the event date), saved timing, delivery readiness and the worker's
recorded target. Times name the account's IANA zone and local abbreviation, with
DST applied to the target instant. A separate **Timing preview** shows the proposed
target; it is not a delivery promise or a new schedule. Unknown sale times stay
unscheduled; elapsed lead times for a future sale explain immediate eligibility.
The custom field is visible without JavaScript; enhancement reveals it only for
Custom and updates the preview. The server validates and normalizes every HTML
choice into `lead_minutes`; repeated fields remain invalid. Invalid submissions
retain the choice and custom value with a nearby error; successful saves/stops
return a named confirmation. Saving timing never changes email consent or pause.

In [Preferences](accounts.md), enable email reminders and turn off
Pause all notifications to permit delivery. Configuring a reminder does not
change those preferences. Account email stays private; dates in the form and
email use the account's time zone. Global pause also retains its existing effect
on in-app discussion notifications.

Preferences separates **Location & time zone** from **Notifications**, with email
permission and pause grouped with short descriptions beneath their labels.
**Choose events & timing** is a separate action, not a tall inline prose link.
**Replies in the app** links **Choose reply frequency** and **Read reply updates**;
**About email delivery** discloses
server support and expiry of missed reminders. Digest/change-email roadmap notes
are removed. HTML saves (including validation rerenders) do not patch the stored
digest choice; its existing JSON preference contract remains for compatibility,
not a delivery promise.

Unknown or TBD sale times stay pending. Known future sales whose lead time has
already elapsed can be reminded immediately after enrollment, including after
changing the lead time or re-enabling a stopped reminder. Already-on-sale
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

Reminder rows lead with event name, recorded target/time zone and a concise state
plus its explanation. Settings distinguish pending date, scheduled, paused,
missing fresh evidence, sending, SES rejection awaiting retry, **SES accepted**,
uncertain, failed, expired and stopped. SES acceptance is never inbox confirmation.
Changing timing does not replay a terminal outcome for the same sale timestamp.

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
sending live mail. The phase-2 fixture covers presets/custom errors, consent,
digest retention, all readiness states, owner/guest/changed-session privacy,
native/touch controls, keyboard/forced-colors focus and compact/enlarged-text
reflow. Back/Forward BFCache restores revalidate private forms rather than retaining
another session's settings. A real SES recipient/inbox smoke test remains an operator step.
