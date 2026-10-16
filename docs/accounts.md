# Google sign-in and accounts

Implemented, opt-in: direct Google authorization-code sign-in, durable accounts,
browser sessions, public profiles/private preferences, personal API tokens, and
[private event saves](saved-events.md), [private artist/venue follows](follows.md), [Interested](event-interest.md), and
[public recommendations](event-recommendations.md).
Accounts require MariaDB; there is no memory fallback. [Personalized radar](radar.md)
uses private follows/category/location preferences. Explicit [on-sale emails](reminders.md)
and [in-app reply updates](followed-discussions.md) are implemented; event-change
emails and weekly discovery digests are out of scope.

## Enable accounts

Profiles also store an owner-editable optional activity first name, used only by
[live community activity](live-community-activity.md). It is never inferred from
display name/email/Google claims and is omitted from public profiles. Blank clears
it to Someone; omitted profile PATCH fields preserve it. The existing account-wide
notification pause also suppresses automatic live activity display/streaming.

First follow the [MariaDB setup](persistence.md#repeatable-local-development), apply
the current required schema version **13**, and reapply the runtime grants. Discovery
remains usable with `AUTH_ENABLED` unset/false. When enabled, missing/invalid configuration or SQL
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
- `/me/preferences`: separate Location & time zone and Notifications groups for
  private city/country, IANA time zone, account-wide pause and on-sale email permission.
  Notifications and live activity start enabled, with
  email/digest choices off. Receive in-app replies for explicitly followed
  conversations. [On-sale emails](reminders.md) additionally require an explicitly
  configured saved-event reminder and SES enabled on the server. Venue-local times are unchanged.
  Notifications uses short labels and local help: **Email me on-sale reminders**
  permits email, **Pause all notifications** stops live activity and both channels without erasing
  settings, and **Replies in the app** links **Choose reply frequency** and
  **Read reply updates**. Control labels and help form one clickable row, with
  descriptions aligned beneath the label and action links separate from prose.
  Help uses readable .875rem type and compact line spacing rather than oversized
  inline link boxes; parent-owned gaps separate choices and a quiet rule separates replies.
  Optional **About email delivery** details explain server support and missed reminders.
  Out-of-scope digests/change emails are not advertised. No HTML digest field is
  submitted: unrelated saves and validation rerenders retain its stored value by
  omitting that patch. The JSON contract still accepts `weekly_digest` for compatibility;
  it does not enable digest delivery.
  A separate **Enable live activity** native form controls only that feed, with
  revision-based conflict detection. Schema 17 enables notifications once for all
  existing accounts; later pause/disable choices persist. Channel permissions,
  subscriptions and delivery history stay independent. See [live preferences](live-community-activity.md#default-on-preferences-part-1).
- `/me/discussions` and `/me/notifications`: private followed conversations and
  frequency-controlled reply updates. See [followed discussions](followed-discussions.md).
- `/me/interests`: paginated own event interest with per-event visibility/removal,
  followed by separate private category choices. Catalog failures retain selected
  IDs rather than erasing choices. Categories help rank private Radar; they do not
  filter or rank public Discover automatically.
- `/follows`: artist/venue search, follow/unfollow and a private, paginated collection.
  Follows never enter public profile activity. See [follows](follows.md).
- Discovery location precedence is explicit search, remembered browser city,
  authenticated account default city, then IP hint. An explicit empty city still
  asks for a location. Sidebar customization remains browser-local.

Forms work without JavaScript, retain invalid values, and report validation errors.
Successful profile/preferences writes redirect back to their section. Sign-in
resumes an allowlisted local `return_to` page, including event/filter context;
cancelled sign-ins preserve it for retry. It never resumes an external/auth URL.

### Sign-in task continuity

- Save from a discovery row resumes that event's Overview with its filters and
  pagination intact, rather than returning to an unidentified row. An already
  selected event retains its section/conversation. Interested uses the same context.
- Event sign-in names the event when details are available, explains the section
  you'll return to, and provides a native **Return to event without signing in**
  link. Leaving/canceling sign-in keeps the public journey; private destinations
  still require authentication. Start failures retain context and retry guidance.
- Recommend resumes the open composer, not just the recommendations list.
  Signing in never saves, marks interest, publishes, changes visibility, or
  configures reminders; each action still needs an explicit form submission.
- Expired-session writes retain confirmed state and provide a contextual sign-in
  link in both enhanced and native recovery. Enhanced recommendation drafts retain
  the existing same-account, tab-scoped storage; native recovery keeps escaped
  text on the failure page and asks you to copy it **before** leaving to sign in.
- The Saved guest redirect and sign-in allowlist preserve its collection cursor.

Run the isolated journey fixture with `MARIADB_TEST_ADDR=127.0.0.1:3307`,
`SIGN_IN_BROWSER_SCRIPT="$PWD/scripts/sign-in-continuity-browser.cjs"`,
`PLAYWRIGHT_MODULE=/path/to/playwright`, and optional `AXE_SCRIPT=/path/to/axe.min.js`:
`go test -run TestMariaDBSignInContinuityBrowserReview -v ./internal/persistence`.
It uses Chromium/WebKit, desktop/phone/native forms, real one-time OAuth flows and
local provider/test-account fixtures—not live Google consent. Physical phones,
manual screen readers, and the two-or-three-person city journeys remain separate
[verification work](design/ui-flow-polish-2026-10-12.md#1-sign-in-and-task-continuity).
The 2026-10-12 run passed 152 journey checks and ten clean automated axe views,
including keyboard guest returns and 200% text. Browser engines use separate
schemas so the production sign-in rate limit remains enabled.

### Your space navigation

Account screens show their current family's links directly, with the current
location labeled. Native **More in your space** groups other destinations:
**Account** (Profile & privacy, Preferences), **Your events** (Radar, Interests,
Artists & venues), **Updates** (On-sale reminders, Followed discussions, Reply
notifications), and **Safety** (reports/outcomes when enabled; review queue only
for moderators). Links use normal document navigation and work without JavaScript.
Profile editing and its public preview sit beside each other when space allows,
stack on phones/enlarged text, and keep security/API tools secondary.

Public event interest and recommendations use shared image/date/title/venue rows.
Artwork and title open Overview with the profile's pagination return context;
named Recommendations links still open that section directly. No per-person
Tickets/Save toolbar or private activity is added.

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

The isolated account-navigation pass uses `ACCOUNT_BROWSER_SCRIPT="$PWD/scripts/account-navigation-browser.cjs"`
with `TestMariaDBAccountBrowserReview`, `MARIADB_TEST_ADDR`, `PLAYWRIGHT_MODULE`,
and optional `AXE_SCRIPT`/`CHROMIUM_EXECUTABLE`. It verifies family navigation,
long public content, preview stacking, desktop/rail/tablet/phone/short/text layouts,
keyboard/forced-colors focus and native touch links. See the
[phase-3 surface record](../.impeccable/surfaces/views-layouts-account-nav-templ.md).
