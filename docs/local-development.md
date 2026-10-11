# Local development services

Run the Go app on your host with optional local MariaDB and MiniStack SES capture.
Neither target deploys the app, changes `.env`, provisions Google OAuth/Ticketmaster
credentials, resets database data, or configures production services.

## Setup targets

```sh
make local-setup
```

The setup pins [MiniStack](https://github.com/ministackorg/ministack) **1.5.24**,
an MIT-licensed AWS emulator with SESv2 support and no account/auth-token requirement.
It uses the same SESv2 client as production, but captures messages locally.

- `make ses-setup`: start/wait for the pinned MiniStack container on loopback port
  **4566**, then create/check `notifications-dev@ticketopia.sonastea.com` in
  **us-east-1** using dummy credentials. No AWS CLI installation is needed on the
  host; the container supplies `awslocal`.
- `make db-setup`: existing [local MariaDB setup](persistence.md#repeatable-local-development),
  including TLS, users, current migrations, runtime grants and permission checks.
- `make local-setup`: SES setup first, then database setup, sequentially even with
  `make -j`. A failure stops later work. Missing emulator prerequisites therefore
  fail before touching the database. Services already started remain available.

Docker with Compose is required; database setup also needs Go and OpenSSL.
Stop the app before applying schema-changing migrations. Existing database volumes
and certificates are retained. Targets can be rerun; an existing SES identity is
checked rather than recreated. MiniStack captures are intentionally ephemeral:
container recreation loses captured messages/identities; rerun `make ses-setup`.

The setup ignores the app's `.env` for MiniStack Compose and pins all setup AWS
calls to the container's loopback endpoint, region and dummy credentials. It never
forwards your host AWS credentials, mounts AWS files or the Docker socket, or
configures an SMTP relay. Compose explicitly leaves MiniStack's `SMTP_HOST` empty
and `PERSIST_STATE=0`; neither requires a host environment variable.

If you used the previous LocalStack setup, stop its container before starting
MiniStack, so it does not occupy port 4566:

```sh
docker stop ticketopia-ses-localstack-1
make ses-setup
```

Stopping retains the old container; do not remove it if you need its captures.
Compose may warn about that stopped "orphan" container; this is expected and does
not prevent MiniStack startup. Setup intentionally does not remove it automatically.

## App environment

Merge these settings into your existing uncommitted `.env` or shell environment;
do **not** overwrite your current file or copy production database credentials.
These settings assume the app runs on the host, not in a separate container.

```dotenv
PERSISTENCE_MODE=mariadb
DB_HOST=localhost
DB_PORT=3307
DB_NAME=ticketopia
DB_USER=ticketopia_runtime
DB_PASSWORD=local-runtime-only
DB_TLS_MODE=verify-full
DB_TLS_CA_FILE=.local/mariadb/certs/ca.crt

AUTH_ENABLED=true
AUTH_BASE_URL=http://localhost:8080
# Use your own Google Web OAuth client, with this exact callback registered:
# http://localhost:8080/auth/google/callback
GOOGLE_CLIENT_ID=your-google-client-id
GOOGLE_CLIENT_SECRET=your-google-client-secret
TICKETMASTER_KEY=your-ticketmaster-key

REMINDERS_ENABLED=true
SES_ENDPOINT_URL=http://localhost:4566
REMINDERS_EMAIL_FROM='Ticketopia DEV <notifications-dev@ticketopia.sonastea.com>'
```

With `SES_ENDPOINT_URL` set, the app uses **dummy `test` AWS credentials**, regardless
of your credential profiles/environment. You do not need `AWS_ACCESS_KEY_ID`,
`AWS_SECRET_ACCESS_KEY`, `AWS_PROFILE`, or real SES/DNS verification locally.
Only HTTP(S) origins at `localhost`, `127.0.0.1`, or `::1` are accepted as emulator
endpoints. This explicit mode also permits HTTP loopback `AUTH_BASE_URL` links.
Remote HTTP app URLs remain rejected. Keep `SES_CONFIGURATION_SET` unset or empty locally.

### Remove unnecessary local email settings

When moving from LocalStack, remove these from the app's local `.env`/shell if
they were added only for email emulation (preserve credentials other tools need):

- `LOCALSTACK_AUTH_TOKEN`: MiniStack does not use it.
- `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`, `AWS_PROFILE`:
  local email uses fixed dummy credentials without reading the AWS credential chain.
- `AWS_ENDPOINT_URL`, `AWS_ENDPOINT_URL_SESV2`: use only `SES_ENDPOINT_URL`.
- `AWS_REGION=us-east-1`, `REMINDER_SEND_INTERVAL=1s`: already the app defaults;
  a non-default value remains a deliberate override. Setup seeds `us-east-1` only.
- `SES_CONFIGURATION_SET`: omit the empty setting; a nonempty production value
  must not be inherited into local mode.
- `LOCALSTACK_TEST_ENDPOINT`: replaced by the one-command `SES_TEST_ENDPOINT`
  smoke-test variable below; it is not an app setting.

Only three reminder-specific settings are needed locally: `REMINDERS_ENABLED`,
`SES_ENDPOINT_URL`, and `REMINDERS_EMAIL_FROM`. Reminders reuse the existing
`AUTH_BASE_URL` from sign-in; no second base URL is needed. Keep the
database/OAuth/provider settings your enabled features need. Environment variables
override `.env`, so removing a line from the file
does not unset an exported shell value. Keep production credentials/configuration
separate; this list is not advice to remove required production settings.

Use **`SES_ENDPOINT_URL`**, not `AWS_ENDPOINT_URL` or a shared AWS profile endpoint:
local mode ignores implicit AWS SDK endpoint overrides. Production mode rejects
them at startup rather than silently redirecting requests or falling back to real
SES when a developer expected an emulator.

Start the app with the normal command (or Air) from the repository root:

```sh
go run ./cmd/ticketopia
```

To exercise delivery, sign in, allow email reminders, unpause notifications, save
an event with a known upcoming public sale and explicitly configure its reminder.
Real Ticketmaster refreshes still require the provider key and consume its budget;
MiniStack is used only for the email API here. No mail leaves the emulator.

After setup, verify the actual SESv2 SDK request and captured message without
Google sign-in, a browser, database changes or a Ticketmaster call:

```sh
SES_TEST_ENDPOINT=http://localhost:4566 \
  go test ./internal/reminders -run TestMiniStackSESSendAndCapture -count=1 -v
```

This opt-in check sends one synthetic message to the emulator. Without the
variable, the check skips and ordinary tests use HTTP fixtures, not real SES.

## Inspect and stop

```sh
curl --fail http://localhost:4566/_ministack/ses/messages
docker compose --env-file /dev/null -f deploy/ministack/compose.yaml stop
docker compose -f deploy/mariadb/compose.yaml stop
```

The capture endpoint returns JSON grouped by account under `messages`; dummy
credentials use account `000000000000`. Records include `MessageId`, `To`,
`Subject` and `BodyText`. Treat captures as private development data. Do not expose
port 4566 beyond loopback or use real customer records for local testing. The service does not
provide a production inbox or reproduce real delivery/reputation, quotas, DNS
authentication, or all SES errors. Unit tests cover rejection/uncertain outcomes;
live production smoke tests remain separate.

If setup waits or fails, inspect the SES readiness check rather than assuming a
running container is ready:

```sh
docker compose --env-file /dev/null -f deploy/ministack/compose.yaml ps
docker compose --env-file /dev/null -f deploy/ministack/compose.yaml logs --tail 40 ministack
```

Check that the old emulator is stopped and nothing else occupies port 4566.

## Production boundary

**Unset `SES_ENDPOINT_URL` in production**, use the public HTTPS `AUTH_BASE_URL`,
the production sender, and restricted real AWS credentials. The production region
must contain your verified identity and SES production approval.
See [reminders](reminders.md#operator-configuration) for deployment requirements.
