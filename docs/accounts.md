# Google sign-in and accounts

Implemented, opt-in: direct Google authorization-code sign-in, durable accounts,
browser sessions, public profiles/private preferences, personal API tokens, and
[private event saves](saved-events.md), [private artist/venue follows](follows.md), [Interested](event-interest.md), and
[public recommendations](event-recommendations.md).
Accounts require MariaDB; there is no memory fallback. Discussions,
personalization, and notification delivery remain planned.

## Enable accounts

First follow the [MariaDB setup](persistence.md#repeatable-local-development), apply
schema version **10**, and reapply the runtime grants. Discovery remains usable with
`AUTH_ENABLED` unset/false. When enabled, missing/invalid configuration or SQL
startup failures stop the server before it listens.

1. In [Google Auth Platform](https://console.cloud.google.com/auth/overview), create
   a project/application and configure its audience/branding/consent screen.
2. Create an OAuth client of type **Web application**. Register the exact callback:
   `https://YOUR_HOST/auth/google/callback` (local: `http://localhost:8080/auth/google/callback`).
   The scheme, host, port, and path must match; use separate clients for development
   and production. Testing-mode apps require configured test users; publish the
   consent configuration before a public pilot, following Google's requirements.
3. Supply runtime settings through your secret store or an uncommitted local `.env`:

   ```sh
   export AUTH_ENABLED=true
   export AUTH_BASE_URL=http://localhost:8080
   export GOOGLE_CLIENT_ID=YOUR_WEB_CLIENT_ID
   export GOOGLE_CLIENT_SECRET=YOUR_WEB_CLIENT_SECRET
   ```

   Retain the MariaDB runtime variables and `PERSISTENCE_MODE=mariadb`. Production
   `AUTH_BASE_URL` must be an HTTPS origin without path/query/credentials. Only
   localhost/loopback HTTP is accepted for development. Redirects and cookie
   security use this configured origin, never untrusted Host/forwarded headers.
4. Rebuild/restart and open `/me`. Google sign-in creates an account on first use
   and signs into that same account on later visits. No SMTP setup is required.

OAuth2 uses pinned `golang.org/x/oauth2`; signed Google ID tokens are verified with
pinned `coreos/go-oidc`. No managed auth broker, Google passwords, Google access
tokens, or refresh tokens are stored. Only `openid email` scopes are requested.

## Profiles, privacy, and preferences

- `/me`: edit display name/bio, set the visibility default for **new** event interest,
  preview/open the public profile, sign out, and manage API access.
- `/users/{id}`: public ID, display name, bio, and explicitly public event interest.
  The profile JSON resource still contains only ID/name/bio; activity has its own API.
  A new account starts as
  **Event explorer**; Google real names/photos are not imported. Your email,
  location, categories, notifications, and credentials never enter this projection.
- `/me/preferences`: private city/country, IANA time zone, and future notification
  choices. Notifications start paused with email/digest choices off; no delivery
  occurs in this release. Venue-local event times are unchanged.
- `/me/interests`: paginated own event interest with per-event visibility/removal,
  followed by separate private category choices. Catalog failures retain selected
  IDs rather than erasing choices. Categories do not filter or rank discovery automatically.
- `/follows`: artist/venue search, follow/unfollow and a private, paginated collection.
  Follows never enter public profile activity. See [follows](follows.md).
- Discovery location precedence is explicit search, remembered browser city,
  authenticated account default city, then IP hint. An explicit empty city still
  asks for a location. Sidebar customization remains browser-local.

Forms work without JavaScript, retain invalid values, and report validation errors.
Successful profile/preferences writes redirect back to their section. Sign-in
resumes an allowlisted local `return_to` page, including event/filter context;
cancelled sign-ins preserve it for retry. It never resumes an external/auth URL.

The sign-in CTA uses Google's current gradient G, Google Sans Medium, and the
[blue-filled button treatment](https://developers.google.com/identity/gsi/web/reference/js-reference#theme)
documented by Google Identity: blue fill, white text, and a white logo tile.
Logo and font are embedded local assets, not external requests or an additional
Google SDK. The ordinary OAuth link works without JavaScript, retains return
context, and keeps a 44px target and visible keyboard focus. Disabled deployments
do not show a Google sign-in CTA.

## Sessions and API clients

Browser sessions and API tokens expire **30 days** after creation, without sliding
renewal. Credentials are opaque random 256-bit values; MariaDB stores only hashes.
Production cookies are host-only `__Host-` cookies with Secure, HttpOnly, Path `/`,
and SameSite=Lax. Local HTTP uses unprefixed development cookies. At most ten
browser sessions are retained per account; a new sign-in evicts the oldest when
necessary. At most ten active API tokens may exist.

Open **API access** under `/me` to create a named token. Copy its one-time secret
response, then use `Authorization: Bearer TOKEN` for own account/profile/preferences
API reads/writes. A later GET/list never reveals the secret. Re-submitting a token
creation POST creates another token, so don't refresh/resubmit its response.
API tokens cannot manage credentials. Revoke individually or use **Sign out
everywhere & revoke API tokens**; ordinary Sign out revokes only the current session
and does not sign out of Google. Tokens can revoke themselves through
`DELETE /api/v1/me/session`.

The [OpenAPI contract](openapi.md) documents `/api/v1/me`, own profile/preferences,
public `/api/v1/users/{id}`, token management, and current-credential revocation.
Cookie writes require a same-origin request and `X-CSRF-Token` from `GET /api/v1/me`
(HTML forms carry a hidden token). Bearer writes do not use ambient cookies/CSRF.
Private responses use `private, no-store`; cross-origin CORS access is not enabled.

Personal tokens also manage private bookmarks through `/api/v1/me/saved-events`.
The web UI offers Save/Remove in discovery/previews/event pages and `/saved` for
the owner-only collection. See [saving and verification](saved-events.md).
They also manage independent interest through `/api/v1/me/event-interests`;
see [interest APIs and verification](event-interest.md).
Personal tokens also search artists/venues and manage private follows through
`/api/v1/me/follows`; see [follow APIs and verification](follows.md#api).

## Security and verification

Identity is Google's verified **issuer + subject**, never email. Changed/reused
email addresses do not merge accounts. Sign-in uses state, S256 PKCE, nonce,
signature/issuer/audience/expiry checks, and verified email claims. Ten-minute,
browser-bound flows are consumed atomically before code exchange, including
cancellation. Shared MariaDB counters limit starts/callbacks to 20 each per IP per
ten minutes and token creation to 20 per account per ten minutes. IP bucket keys
are hashed; expired flows/counters are removed in bounded batches. Configure
[trusted proxies](location.md) correctly; shared counters are not full DDoS protection.

Run `MARIADB_TEST_ADDR=127.0.0.1:3307 go test -race ./...`, `go vet ./...`,
`npm run build`, and the [OpenAPI lint](openapi.md#viewing-validating-and-updating).
Tests use signed local Google-token fixtures and real MariaDB for concurrency,
ownership, restart, replay, expiration, revocation, limits, and least privileges.
Browser checks cover desktop/tablet/320–390px, 200% text, ordinary no-JS forms,
validation, public privacy, token creation/revocation, and automated accessibility.
Real Google consent/sign-in still requires your OAuth credentials and callback
configuration; no live Google account or production deployment is claimed.
