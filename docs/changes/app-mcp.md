# Agent tools (MCP) changes

## Unreleased

### 2026-11-15 — Public community read tools

- Add public, read-only tools for community reads that had only an HTTP
  route, each calling the route's service method: `event_recommendations`.
  Recommendation authors' profile IDs feed `profile_activity`. See
  [tools](../app-mcp.md#tools).

### 2026-11-14 — Event community and profile activity tools

- Add public, read-only `event_community` and `profile_activity`, sharing the
  code behind `GET /api/v1/events/{id}/community` and `/users/{id}/activity`,
  so agents and API clients get identical results. Signed in with `read`,
  `event_community` adds the caller's own state, and `profile_activity` without
  `user_id` reads the caller's own timeline. See [tools](../app-mcp.md#tools).

### 2026-11-14 — Agent publishing, phase 1

- Add a third OAuth scope, `publish`: never implied by `write`, never requested
  when a client names no scope, and never carried by API tokens (now an explicit
  `read write` list). The consent page offers **Also allow posting publicly as
  *name***, unchecked, only when an app asks; its "can't post anything publicly"
  promise now matches the checkbox. Connected apps label such connections
  **Can post publicly**. Existing connections keep their scopes; a refresh
  cannot add `publish`.
- With `publish`: `make_interest_public`, `recommend_event` (no reason; an
  existing recommendation is returned unchanged) and `withdraw_recommendation`
  (only what an app published without a reason: agents never erase the
  person's words), at most 20 an hour per account per replica, each logged as `Agent published`
  with only the tool and app host. With `write`: `make_interest_private`,
  `draft_recommendation` and `discard_draft`; `my_recommendations` and
  `my_drafts` read. `SignedInTools` maps each tool to its scope; the boundary
  answers 403 `insufficient_scope` naming it, and tools check again.
- Agents never publish words: a drafted reason waits privately at `/me/drafts`
  (seven days, 20 per account, deleted with its connection) beside the reason it
  would replace, until the person publishes it with a CSRF-protected form that
  refuses a stale review and consumes the draft in the same transaction.
  Recommendations an agent published, or whose reason it drafted, show
  "via *app* (*host*)" on the event, profile and Community, and carry `via` and
  `via_host` in the API (contract 1.24.0); API-token drafts say "via an API
  client". Client names naming Ticketopia are refused. Schema 22 adds
  `event_recommendations.via_client_name`/`via_client_host` and `agent_drafts`,
  with runtime grants in every provisioning path. Discussions and Helpful are phase 2; see
  the [guide](../app-mcp.md#publishing-in-your-name).

### 2026-11-13 — Radar, follow and Interested tools

- Add signed-in tools: `my_radar`; `find_artists`, `find_venues`, `my_follows`,
  `follow_artist_or_venue` and `unfollow_artist_or_venue`; `my_interested_events`,
  `mark_interested` and `unmark_interested`. They wrap the same services as the
  account API, act only on the token's account, and their writes need the
  `write` scope. Agents create only private Interested choices, keeping the
  consent page's promise that a connected app cannot post anything publicly.
- Signed in, `find_events` without city, country or keyword uses the account's
  saved city and reports it as `default_location`.
- The consent page and Connected apps list the new abilities. The review
  fixture's provider stand-in answers venue searches, so the tools can be tried
  locally. Tests cover the tools, scope refusals and the location default, plus
  venue search, following, radar picks and private Interested against MariaDB;
  see the [guide](../app-mcp.md#tools).

### 2026-11-13 — Token revocation endpoint (RFC 7009)

- Agents disconnect themselves at `POST /oauth/revoke`, advertised as
  `revocation_endpoint` in server metadata: either of a connection's current
  tokens with the client's `client_id` ends the whole connection, as
  **Disconnect** on the account page does; a refresh token already rotated away
  does too, like a replay at the token endpoint. Unknown, expired or other
  clients' tokens get the same empty 200 and change nothing. It shares the token
  endpoint's public-client, form-body, CORS and per-IP rules; storage failures
  get 503 with `Retry-After`. Each revocation is logged with its outcome and
  the client's metadata host, never the token or account. No schema change.
  Tests cover the service rules, the HTTP endpoint and logs, and cross-replica
  revocation, including racing a refresh, against MariaDB; see the
  [guide](../app-mcp.md#oauth-sign-in).

### 2026-11-13 — Accept ChatGPT's client metadata

- Client ID Metadata Documents that list `token_endpoint_auth_methods_supported`
  (MCP SEP-3149) are accepted when the list includes `none`, whatever the legacy
  `token_endpoint_auth_method` preference says. ChatGPT's document prefers
  `private_key_jwt` but lists `none`, and was refused; it now signs in as a
  public client. Verified against the live ChatGPT, OpenCode and Claude Code
  documents on a local server; see the [guide](../app-mcp.md#oauth-sign-in).

Older entries: [agent tools archive](archive/app-mcp/).
