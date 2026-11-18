# Agent tools (MCP)

Ticketopia serves event tools, and once signed in (OAuth or an API token) the
caller's own radar, saves, reminders, follows, Interested choices,
recommendations and drafts, to people's AI agents and editors over the
[Model Context Protocol](https://modelcontextprotocol.io/) at **`/mcp`**. It is
off unless `MCP_ENABLED=true`. The [design](design/app-mcp.md) describes later
steps (a separate deployment); this page covers what exists.
[Load testing](load-testing.md) covers `make load-test` and baselines.

On the site, open **Agents and API access** (`/agents`) for a public setup guide
with the deployment’s MCP endpoint, OAuth examples, API tokens, drafts, and
access removal where enabled. **Connected apps** and **API tokens** in Security &
apps (`/me/security`) both link to
it. Even when accounts are off, set `AUTH_BASE_URL` to the public origin to
show the full MCP URL. Otherwise the guide gives `/mcp` and asks people to
use their browser’s address.

## Tools

| Tool | Behavior |
|---|---|
| `find_events` | Search by city, country, dates, category, keyword, sort and limit, with `cursor` paging. Same validation, defaults, caching and results as Discover and `GET /api/v1/events`. Signed in, a search without city, country or keyword uses the account's saved city, as Discover does, and reports it as `default_location`; keyword searches (an artist's tour) stay unrestricted. |
| `list_categories` | Categories and their IDs for `find_events` `category_id`, as `GET /api/v1/categories`. |
| `event_details` | One event by ID, as `GET /api/v1/events/{event_id}`, including the durable last-known fallback. |
| `event_community` | An event's public totals (interested including private choices, active recommendations, visible questions and tips), as `GET /api/v1/events/{event_id}/community`. Signed in with `read`, `viewer` adds the caller's own saved, Interested and recommended state; otherwise it is `null`. An ID that names no event is not found; zeros mean the event is quiet, or that it couldn't be checked while the provider is unavailable. |
| `profile_activity` | One person's public posts, recommendations and public Interested choices, newest first, as `GET /api/v1/users/{user_id}/activity`. `user_id` comes from a profile URL; signed in, omitting it reads the caller's own. |
| `event_recommendations` | An event's public recommendations with each author's public profile (its `id` is a `profile_activity` `user_id`), optional reason, `via` attribution and the event's `count`, as `GET /api/v1/events/{event_id}/recommendations`. |
| `event_interested_people` | People who made their Interested choice public, with public profiles, as `GET /api/v1/events/{event_id}/interested-users`. Private choices are never listed. |
| `event_discussions` | An event's public questions and tips, newest first, with reply and Helpful counts, as `GET /api/v1/events/{event_id}/discussions`; signed in, `helpful` is the caller's own choice. |
| `discussion_thread` | One question or tip with a page of its replies (oldest first), as `GET …/discussions/{discussion_id}` and `…/replies` together; the thread is returned on every page. |
| `community_recommendations` | Recommended events grouped per event, optionally by city, country and `category_id`, as `GET /api/v1/community/recommendations`. |
| `community_discussions` | Recent questions and tips across events with the same filters, as `GET /api/v1/community/discussions`. |
| `user_profile` | One public profile (ID, display name, bio; never email or settings), as `GET /api/v1/users/{user_id}`; signed in, omitting `user_id` reads the caller's own. |
| `happening_now` | The public Happening Now snapshot, optionally by city with country and `category_ids`, as `GET /api/v1/community/activity` without credentials. Listed when live activity is on; it needs no accounts. |

These community reads need accounts and MariaDB, and are listed with them.
They share the HTTP routes' code, so results match those routes exactly.

Signed-in tools act only on the account behind the request's token. The
saved-event tools are listed when accounts and MariaDB are enabled; the reminder
tools also need [email reminders](reminders.md) (`REMINDERS_ENABLED=true`):

| Tool | Behavior |
|---|---|
| `my_saved_events` | The caller's saved events, newest first, with `limit`/`cursor` paging, as `GET /api/v1/me/saved-events`. |
| `save_event` / `unsave_event` | Save or remove one event, idempotently, returning the resulting state. Removing also stops its reminder. |
| `my_reminders` | The caller's on-sale reminders and their delivery states, as `GET /api/v1/me/reminders`. |
| `set_reminder` / `stop_reminder` | Set (1–10080 minutes before the public sale) or stop a reminder; the event must be saved first, and delivery follows the account's email preferences. |
| `my_radar` | The caller's [radar](radar.md): upcoming events near their saved city from their follows and categories, each with its reasons, as `GET /api/v1/me/radar`; nearby events without follows or categories, and empty (`needs_location`) without a saved city. |
| `find_artists` / `find_venues` | Search the provider's artists, or venues optionally within a city and country, for IDs to follow, as `GET /api/v1/artists` and `/venues`, with their error wording plus any retry delay. |
| `my_follows` | The caller's private artist and venue [follows](follows.md), optionally one `kind`, as `GET /api/v1/me/follows`. |
| `follow_artist_or_venue` / `unfollow_artist_or_venue` | Follow (falling back to the stored catalog when the provider is unavailable) or unfollow one artist or venue by `kind` and `target_id`. |
| `my_interested_events` | The caller's [Interested](event-interest.md) choices with their visibility, as `GET /api/v1/me/event-interests`. |
| `mark_interested` / `unmark_interested` | Mark or remove Interested. `mark_interested` never publishes: a new choice is private (counted only in the anonymous total) whatever the account's default, and an existing choice keeps its visibility. |
| `make_interest_private` | Make a choice private, marking Interested first if needed (`write`). |
| `make_interest_public` | Make a choice public, marking Interested first if needed (`publish`): the person's name appears among the event's interested people and on their profile. |
| `my_recommendations` | The caller's public [recommendations](event-recommendations.md), each with its `via` app when an agent published it or wrote its reason, as `GET /api/v1/me/event-recommendations`. |
| `recommend_event` | Publicly recommend an event without a reason (`publish`), attributed to the app and its host. An existing recommendation is returned unchanged, reason included. |
| `withdraw_recommendation` | Withdraw the caller's recommendation only when an app published it without a reason (`publish`); one the person published or that has a reason is refused with a tool error pointing to the web. Idempotent. |
| `draft_recommendation` | Store a private reason for the person to review (`write`), returning `review_url` (`AUTH_BASE_URL/me/drafts`). Nothing is published; see [drafts](#publishing-in-your-name). |
| `draft_question` | Store a private question or tip on an event for the person to review (`write`), returning `review_url`. Nothing is published. |
| `draft_reply` | Store a private reply in a question or tip's conversation (`write`), by `event_id` and `thread_id`, answering the reply `parent_id` when given; the thread must be a question or tip on that event and the parent a post in it. Nothing is published. |
| `my_drafts` / `discard_draft` | List the caller's pending drafts of every kind (`recommendation`, `question`, `reply`), or delete one (`write`). `discarded` is `false` when no pending draft had that id (already gone, or never the caller's, without saying which); a malformed id is an error naming `draft_id`. |
| `mark_helpful` / `unmark_helpful` | Set or remove the caller's Helpful mark on a question, tip or reply by `post_id` (`publish`), as `PUT`/`DELETE /api/v1/posts/{post_id}/reactions/helpful`, returning the post with its public `helpful_count`. Removed and hidden posts can't be marked. |
| `my_followed_discussions` | The questions and tips the caller [follows](followed-discussions.md) privately, with frequency and the question, as `GET /api/v1/me/discussions`. |
| `my_notifications` | New replies in followed discussions, read or unread, with a site path to each reply, as `GET /api/v1/me/notifications`. Listing marks nothing read. |
| `mark_notification_read` | Mark one notification read (`write`), idempotently, as `PUT /api/v1/me/notifications/{notification_id}/read`. |
| `my_preferences` | The caller's private preferences (saved city, timezone, categories, reminder/digest/pause settings), as `GET /api/v1/me/preferences`. Never includes email. |
| `follow_discussion` / `unfollow_discussion` | Follow a question privately with a frequency (immediate, daily, weekly or muted), or stop (`write`), as `PUT`/`DELETE /api/v1/events/{event_id}/discussions/{discussion_id}/follow`. |

Client metadata whose `client_name` names Ticketopia (case-folded letters and
digits, with 0 read as o and 1, l and ! as i) is refused, so no app appears as
the site itself in consent or public "via" labels.

The radar, follow and Interested tools are listed when their services are, with
accounts and MariaDB; the recommendation tools with recommendations, and the
draft tools with drafts too. `draft_question`, `draft_reply` and the Helpful
tools also need discussions. No tool edits or removes posts
(`PATCH`/`DELETE /api/v1/posts/{post_id}`): only the person changes their words. Searching artists and venues needs sign-in, as on
the web and API, because it reaches the provider.

Read tools are annotated read-only. `unsave_event`, `stop_reminder`,
`set_reminder` (changing a reminder replaces its timing), `unfollow_artist_or_venue`,
`unmark_interested`, `make_interest_private`, `withdraw_recommendation`,
`discard_draft`, `unmark_helpful`, `follow_discussion` (changing a follow replaces its frequency)
and `unfollow_discussion` are marked destructive; `save_event`, `follow_artist_or_venue`,
`mark_interested`, `make_interest_public`, `recommend_event`, `mark_helpful`,
`draft_recommendation`, `draft_question`, `draft_reply` and `mark_notification_read` are additive. All writes are idempotent (an identical
pending draft is returned, not repeated) and return the resulting state.
`make_interest_public`, `recommend_event`, `withdraw_recommendation`,
`mark_helpful` and `unmark_helpful` need the `publish` scope; the other writes
need `write`.

`find_events` dates are `YYYY-MM-DD` and match each event's local date, as on
Discover. `start_date` defaults to today in UTC and `end_date` to 90 days later
(at most 366 days). `limit` is 1–100, default 20. For
the next page, pass `next_cursor` as `cursor` with exactly the same other
arguments; a cursor is bound to its filters.

Results keep the API's freshness metadata: list `meta` and, for stored Discover
lists, per-event `meta` with `stale` for last-known events. Each tool publishes
an output schema, and results are validated against it. Errors are tool errors
with the API's wording (for example `limit: use an integer from 1 to 100`),
plus `Retry after N seconds.` when the provider is cooling down, so agents can
correct or pace their requests. Signed-in tool errors use the account API's
wording. Arguments that don't fit a tool are refused before it runs, in the same
`field: message` form, naming every problem in name order and joined by `; `:
unknown arguments are named together with the tool's arguments, a missing one
says `required` with its description, and a wrong type, including a list's
items, says what to send (a mistyped `limit` says `use an integer from 1 to
100`). A failed call carries only its message, with no structured result, so a
zero value (such as `following: false`) is never mistaken for the answer.

## Signing in

With accounts enabled, **every `/mcp` request needs sign-in by default**, public
tools included, so all agent traffic is attributed to an account and limited
per account; requests without a token get a **401** challenge that starts
OAuth sign-in. `MCP_REQUIRE_SIGN_IN=false` lets public tools (and
`initialize`/`tools/list`) work anonymously again, limited per IP. Without
accounts nobody can sign in, so only the public tools are served, anonymously.

Sign in with either:

- **OAuth** (on by default with accounts; `MCP_OAUTH_ENABLED=false` turns it
  off): the agent opens a
  browser, the person signs in with Google if needed and approves the connection,
  and the agent receives tokens. This is how Claude, ChatGPT, VS Code and other
  apps without custom headers connect. See [OAuth sign-in](#oauth-sign-in).
- **An API token**: create one under **API tokens** in Security & Apps (`/me/security`;
  see [accounts](accounts.md#sessions-and-api-clients)) and configure your client
  to send `Authorization: Bearer <token>`. API tokens carry read and write
  access on `/mcp`, never `publish`: an agent using one can draft but not post
  publicly through the agent tools. They are still full account credentials on
  `/api/v1`, where they can publish recommendations with any reason, so give
  agents OAuth instead wherever the client supports it. Revoking the token cuts
  the agent off.

A token that is sent must be valid: invalid, expired or revoked tokens get
**401** with `WWW-Authenticate: Bearer`. Browser session cookies are never
accepted on `/mcp`, so a page cannot act with someone's sign-in. With sign-in
optional and OAuth off, a signed-in tool called without a token returns a tool
error explaining how to connect.

### OAuth sign-in

Ticketopia is its own OAuth 2.1 authorization server, with `AUTH_BASE_URL` as
the issuer and `AUTH_BASE_URL/mcp` as the only resource tokens are issued for.

- **Discovery.** `/.well-known/oauth-protected-resource/mcp` (and the root
  `/.well-known/oauth-protected-resource`) describe `/mcp` (RFC 9728);
  `/.well-known/oauth-authorization-server` describes the server (RFC 8414). Both
  allow any origin, as do the token and revocation endpoints, which never use
  cookies.
- **Challenges.** A request without a token gets **401** with
  `resource_metadata` and `scope="read write"` in `WWW-Authenticate`, so clients
  start sign-in: every request while sign-in is required, otherwise only
  signed-in tool calls. A tool needing a scope the token lacks gets **403**
  `error="insufficient_scope"`, so clients can ask again: changes through a
  read-only connection, and publishing tools through any connection or API
  token without `publish`. Publishing tools ask for `scope="read write publish"`;
  no other challenge mentions `publish`.
- **Clients** identify themselves with a
  [Client ID Metadata Document](https://modelcontextprotocol.io/specification/2026-07-28/basic/authorization/client-registration):
  an HTTPS `client_id` URL serving JSON with the same `client_id`, a
  `client_name` and `redirect_uris`. Dynamic Client Registration and client
  secrets are not supported. Clients sign in as public clients (`none`): a
  document's `token_endpoint_auth_methods_supported` must include `none` when
  it is present (MCP SEP-3149; ChatGPT lists `none` and `private_key_jwt`),
  and otherwise its legacy `token_endpoint_auth_method` must be `none` or
  absent. Documents are fetched over HTTPS on port 443 from
  public addresses only (no redirects, 5 KB, 5 seconds) and cached per replica
  for their `max-age` (1 minute to 24 hours, default 10 minutes).
- **Redirects** must match a registered URI exactly, except loopback `http`
  URIs, which may use any port (RFC 8252). Registered URIs must be `https`,
  loopback `http` or an app scheme such as `cursor://`. A request with an
  untrusted `client_id` or `redirect_uri` shows an error page instead of
  redirecting.
- **Authorization** at `/oauth/authorize` requires `response_type=code` and PKCE
  with `S256`; `resource`, when sent, must be `AUTH_BASE_URL/mcp`. Signed-out
  people sign in first and return to the request. The consent page names the app
  and the host serving its metadata, lists what it can do, and offers **Also
  allow changes** when `write` is requested; leaving it off makes a read-only
  connection. When `publish` is requested it also offers **Also allow posting
  publicly as *display name***, unchecked by default, naming exactly what that
  allows. Responses carry `iss` (RFC 9207). The page cannot be framed.
- **Scopes**: `read` (public tools, radar, saved events, reminders, follows,
  Interested events, recommendations, drafts), `write` (save and unsave, set and
  stop reminders, follow and unfollow, mark Interested privately, draft and
  discard reasons, questions, tips and replies) and `publish` (make Interested
  public, recommend without a reason, withdraw a recommendation, mark or
  unmark Helpful). `publish` implies `read`, not `write`. An
  omitted scope requests `read write`, never `publish`. Unless posting is
  allowed, the consent page promises the app cannot post anything publicly;
  with the box ticked (detected with `:has()`), it says what the app can post.
  Browsers without `:has()` show a statement true either way.
- **Tokens** from `/oauth/token` (form-encoded, public clients only): access
  tokens (`tko_at_…`) last one hour; refresh tokens (`tko_rt_…`), issued when
  the client's document lists the `refresh_token` grant, rotate on every use.
  Presenting a rotated refresh token again revokes the whole connection: it
  leaked, or the client raced its own refreshes, and the two can't be told
  apart, so the person reconnects either way. A refresh may narrow
  but never widen scopes, and a rejected one changes nothing. A connection
  expires after 30 days without a refresh. Codes last five minutes and work
  once, even if the exchange fails; `redirect_uri` may be omitted (PKCE binds
  the code) but must match when sent. Only hashes are stored. Access tokens work
  only on `/mcp`, never on `/api/v1`.
- **Connections.** Each approval creates a connection, listed under **Connected
  apps** on the account page with its access (**Can post publicly** when it
  holds `publish`); existing connections keep exactly their scopes; **Disconnect** stops its tokens
  immediately. An account keeps at most ten; approving another removes the
  oldest. **Sign out of all devices** also disconnects every app, including any whose
  code exchange is in progress.
- **Revocation** (RFC 7009): an agent disconnects itself by posting `token`
  (either current token) and its `client_id` to `/oauth/revoke`, the
  `revocation_endpoint` in server metadata, form-encoded and without client
  authentication. Either token ends the whole connection, as **Disconnect**
  does; so does a refresh token already rotated away, which means the client
  kept a stale token or it leaked, as with a replay at the token endpoint. A
  replaced access token is simply unknown. `token_type_hint` is accepted but
  unneeded, since the prefix identifies the token. The answer is an empty
  **200** whether or not anything was revoked: unknown, expired and other
  clients' tokens change nothing. A missing `token` gets `invalid_request`, a
  missing `client_id` or any `Authorization` header `invalid_client`, and a
  storage failure **503** with `Retry-After`. Each answered request logs an
  `OAuth token revocation` record with its `outcome` (`revoked`,
  `revoked_rotated`, `unknown` or `other_client`, a client bug or a token in
  the wrong hands) and the claimed client's metadata host, never the token or
  account.
- The token and revocation endpoints share one allowance per client IP, like
  `/mcp` (`MCP_RATE_PER_MINUTE`); authorization pages share the sign-in limit of 20
  attempts per IP per ten minutes.

OAuth needs schema version 21 (`oauth_codes`, `oauth_grants`,
`oauth_refresh_history`), publishing schema 22 (`agent_drafts` and
`event_recommendations.via_client_name`/`via_client_host`), and discussion
drafts schema 24 (`agent_drafts.thread_id`/`parent_id` and
`discussion_posts.via_client_name`/`via_client_host`); run migrations and reconcile
runtime grants before starting the new version, as for any schema change.

## Publishing in your name

Connecting an agent never lets it put words in public. It covers Interested
choices, recommendations, discussion questions, tips and replies, and Helpful
(see the [design](design/app-mcp.md)).

- **Structured, reversible actions** publish directly when the connection holds
  `publish`: making an Interested choice public, recommending without a reason,
  withdrawing a recommendation, and marking or unmarking a post Helpful (a
  public count with no text; nobody sees who marked it). Each account may make
  20 such calls an hour per replica; more get a tool error with
  `Retry after N seconds.` Each success logs one `Agent published` record with
  only `tool` and `client_host`.
- **Free text is never published by an agent.** `/mcp` is stateless, so MCP
  elicitation (asking the person inside the call) is impossible, and only a web
  page guarantees a person read the words. `draft_recommendation` (a reason,
  plain text up to 500 characters), `draft_question` and `draft_reply` (plain
  text up to 4000 characters, as on the web) store a private draft and return
  `review_url`. At **Agent drafts** (`/me/drafts`, linked in the account
  navigation with a count while drafts wait) the person reads each draft with
  its event and app and chooses **Publish recommendation**, **Publish question
  or tip** or **Publish reply**, or **Discard**, each a CSRF-protected POST; the
  page never publishes on GET. A reason shows the reason it would replace, and
  the form carries a hash of what was shown: if the recommendation changed
  since, nothing is published and the page asks for a fresh review. A reply
  shows the question, tip or reply it answers; once that is gone from the event,
  the draft can only be discarded. Publishing validates the text and target
  again, applies the discussion rate limit to questions and replies, and
  consumes the draft in the same transaction, so a draft publishes at most once
  (a reply's or question's retry key is derived from the draft) and a failure
  leaves it in place. Published replies notify the conversation's followers as
  any reply does. Drafts are private to their account, at most 20 at once of
  every kind, expire after seven days, and are deleted with their connection
  (Disconnect, **Sign out of all devices**, token revocation or eviction). Drafts
  made with an API token, which identifies no app, say "App unidentified;
  created using an API token".
- **Attribution.** A recommendation an agent published, or whose drafted reason
  the person published, shows "via *app name* (*host*)" beside the author on the
  event, profile and Community, and carries `via` and `via_host` in the API. So
  does a question, tip or reply published from a draft, in its thread, the
  event's questions and tips, Community and moderation views, and in
  `/api/v1` posts and profile activity. The name is the app's own claim; the
  host is its verified metadata host, in ASCII (IDNA) form so lookalike
  characters show. API-token drafts publish as "via an API client", without a
  host. `via` describes the source of the current content, not permanent
  history: the person's change of reason or post text, withdrawal and removal
  clear it. Interested choices and Helpful carry no label.
- **Agents never erase or change the person's words.** `recommend_event`
  returns an existing recommendation unchanged, checked under its row lock, and
  `withdraw_recommendation` only withdraws a recommendation an app published
  without a reason, in the same statement. No tool edits or removes a post
  (`PATCH`/`DELETE /api/v1/posts/{post_id}` have no agent tool), including posts
  published from an agent's draft: once published, they are the person's.

## Protocol and limits

- Streamable HTTP in **stateless** mode with JSON responses: each `POST /mcp`
  stands alone, so any replica can answer and no session is kept. A
  `tools/call` needs no prior `initialize` request.
- Each trusted client IP, and each signed-in account, may make
  `MCP_RATE_PER_MINUTE` requests per minute per replica (default 60, bursts up
  to a minute's allowance). Excess requests get
  **429** with `Retry-After`. Client IPs follow `TRUSTED_PROXY_CIDRS`. At most
  4096 client IPs are tracked per replica; while full, new IPs get 429.
- One request is one call: JSON-RPC batches get **400**.
- At most `MCP_MAX_IN_FLIGHT` requests run at once per replica (default 8, below
  the default database pool of 10); excess gets **503** with `Retry-After: 1`
  immediately rather than queuing.
- Requests carrying an `Origin` header must come from an allowed origin or a
  loopback origin (local tools such as the MCP Inspector); others get **403**.
  `MCP_ALLOWED_ORIGINS` (comma-separated) sets the allowed origins and defaults
  to `AUTH_BASE_URL`, which applies even when accounts are disabled. The request's own `Host` is not trusted, since DNS rebinding controls
  it. Clients without an `Origin` header (agents, servers) are unaffected, and a
  reverse proxy may connect over loopback with the public `Host`. CORS stays
  disabled.
- Requests above 64 KiB get **413**. Responses are `Cache-Control: no-store`.
- Every `tools/call` is logged with its tool, outcome (`ok`, `tool_error`,
  `error`) and duration, never its arguments, including calls rejected for
  invalid arguments. Names that match no tool are logged as `unknown`.
- Each replica logs an `MCP metrics` record every minute in which anything
  changed: per tool, cumulative `calls`, `ok`, `tool_errors`, `errors`,
  `signed_in`, `seconds` and seven disjoint duration buckets (≤1 / ≤10 / ≤50 /
  ≤250 / ≤1000 / ≤5000 ms and more); and `refusals` at the boundary by reason
  (`origin`, `ip_rate`, `account_rate`, `busy`, `unauthorized`,
  `accounts_unavailable`, `batch`, `too_large`, and with OAuth `sign_in_required`
  and `insufficient_scope`). Dimensions are fixed: no
  arguments, accounts or IPs. Use interval deltas, as for
  [live-activity telemetry](live-activity-rollout.md#operational-telemetry).
  With an OTLP endpoint configured, the same counts, a duration histogram and
  revocation outcomes are also exported as metrics; see
  [observability](observability.md#metrics).
- When Ticketopia itself is busy (for example its database does not answer in
  time), tools report "Ticketopia is busy right now… Retry after 5 seconds."
  rather than a provider failure.
- `/mcp` is JSON-RPC, not REST, so it is not part of the
  [OpenAPI contract](openapi.md).

## Connect a client

Enable it locally with `MCP_ENABLED=true` in `.env` and run the app (`air`), then
add the remote server to your client:

```sh
opencode mcp add ticketopia --url http://localhost:8080/mcp \
  --header 'Authorization=Bearer {env:TICKETOPIA_TOKEN}'
claude mcp add --transport http ticketopia http://localhost:8080/mcp \
  --header "Authorization: Bearer $TICKETOPIA_TOKEN"
npx @modelcontextprotocol/inspector   # then connect to http://localhost:8080/mcp
```

Keep the token in an environment variable rather than in committed
configuration. Omitting the header works only for public tools, and only with
`MCP_REQUIRE_SIGN_IN=false` or without accounts.

With OAuth on (the default with accounts), omit the header instead: clients that
support MCP OAuth with Client ID Metadata Documents (Claude Code, OpenCode,
VS Code, Claude and ChatGPT connectors) open the browser to sign in when they
connect (or, with sign-in optional, when a signed-in tool first needs it). The
client app signs in, not the model, so OpenCode or Claude Code connects the same
way with any model (GLM, GPT, Claude). In OpenCode, add the server without a
header (`opencode mcp add ticketopia --url …/mcp`) and sign in from `/mcps`.
Hosted apps (Claude on the web, ChatGPT) can only reach a public HTTPS
deployment, not `localhost`. Clients that only support Dynamic Client
Registration cannot sign in; give them an API token header instead.

The [review server](testing.md#signed-in-manual-review-server) also serves
`/mcp` (manifest `paths.mcp`) over its seeded fixture data, without a provider
key, with a high rate limit for local tooling. It enables OAuth with a fixture
app (`paths.oauth_authorize` opens its consent page) but keeps sign-in optional,
so load scripts can call public tools anonymously. Each fixture account has an API
token in the manifest (`accounts.<name>.api_token`) for the signed-in tools.

### Throwaway accounts on your local database

To try the tools against your own local server and database (real provider
data, your migrations) without signing in with Google, use the throwaway
accounts `make db-setup` creates, or run `make local-test-accounts` on its own
(for example, to refresh expired OAuth tokens). It creates two accounts, `owner` (saved city
Chicago, US) and `other`, through the app's own sign-in, token and OAuth
services, and writes their credentials to `.local/local-test-accounts.json`
(owner-only permissions, gitignored), never to the terminal:

- `session`: the browser session cookie's value, named by `session_cookie`, for
  account pages such as `/me/drafts` (forms also need the `csrf_token` from
  `GET /api/v1/me` and an `Origin` of `base_url`);
- `api_token`: an API token (`read write`);
- `oauth_publish` and `oauth_read_only`: OAuth access tokens as an approved
  agent named "Local Test Agent" (`local-test-agent.invalid`) would hold, with
  `read write publish` and `read`, so publish actions, "via" attribution and
  scope refusals can be tried without a hosted client. They last one hour.

Running it again replaces the accounts with fresh ones (and fresh tokens);
`make local-test-accounts-clean` removes them and everything they made, including
other accounts' replies in their threads. Calls go to `mcp_url`
(`AUTH_BASE_URL/mcp`, the `air` proxy; the app itself also answers on `:8080`).
Without `AUTH_BASE_URL` it assumes the app's own `http://localhost:8080`.
The tool refuses unless `AUTH_BASE_URL` and `DB_HOST` are loopback, uses the
migration credentials, and is not built into the image. Accounts are marked by a
non-numeric Google subject and an `@local-test.invalid` email, which no real
account can have, and removal refuses accounts with moderation or role records,
which are an audit trail.

## Verifying new tools with an agent

Unit tests prove a tool's code. They don't show how an agent actually uses it.
When a change adds or changes tools, an agent (a subagent of the session doing
the work) uses them the way a person's agent would: it starts from
`find_events` or `event_details`, then calls the new tool with what those return
and follows up as a person would ask. It does this twice.

**1. Locally, before committing.** Apply a review scenario through the
`ticketopia-review` MCP server (`scenario_apply`), or run
`make review-server scenario=<name>` and read `manifest.json` from
`REVIEW_SERVER_STATE_DIR` (`baseline` seeds public
activity, while `discover-last-known` or `populated` are needed for provider-backed
tools such as `find_venues` and `my_radar`). The `ticketopia-review` server runs
the binary built when its MCP server started, so restart it after changing code
(check `tools/list` lists the new tools), or use `make review-server` from the
working tree. To use your local database instead, create
[throwaway accounts](#throwaway-accounts-on-your-local-database). Then call
`paths.mcp` over HTTP:

- anonymously, and with the `owner` and `other` tokens from the manifest, so
  results are checked for the right caller and nothing private crosses
  accounts;
- every new tool, with a valid call, an empty or unknown target, a malformed
  argument and a bad `limit`/`cursor`, checking the error wording an agent
  would see;
- writes through to their read-back (for example `mark_interested`, then
  `event_community` and `profile_activity`), including scope refusals with an
  OAuth or read-only connection where the tool needs `write` or `publish`;
- each tool against its `/api/v1` route with the same token, which must match.

Writes are safe here: the schema is thrown away.

**2. Against production, once deployed.** Confirm in Coolify that the commit
finished deploying and `/readyz` answers 200. Then run the same read paths
against `https://topia.sonastea.com/mcp`, with the token from the
repository's ignored `.envrc` (`TICKETOPIA_TOKEN`, loaded with `direnv exec .`).
Never print it. Without a token, `/mcp` must answer 401 with the OAuth
challenge. Use only read-only tools and invalid-input calls. Writes change a
real account and `publish` tools act publicly, so run them only when the person
asks for that run.

The agent reports each call and its outcome, compares results with the API,
and lists anything it couldn't exercise (such as no production data to
paginate).
