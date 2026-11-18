# Agent tools (MCP) changes

## Unreleased

### 2026-11-17 — In-app agents and API guide

- `/agents` shows setup for enabled features, with a configured MCP origin
  or instructions to use the browser’s address. **Connected apps** and **API
  access** link to it. The guide and token form spell out the token’s broader
  JSON API access, and the guide and consent page explain that OAuth connection
  expiry depends on refresh. See [Agent tools](../app-mcp.md).

### 2026-11-15 — Clearer argument and not-found errors

- Arguments that don't fit a tool (unknown, missing or of the wrong type) are
  refused in the API's `field: message` wording, naming every problem, instead
  of the SDK's schema messages. `limit` errors read `use an integer from 1 to
  100` and malformed event IDs `use a ticketmaster: prefixed ID` everywhere,
  in tools and `/api/v1` alike. `event_community` and its route report an ID
  that names no event as not found (contract 1.28.0), not as zeros; this
  costs a cached event lookup for events without activity. See
  [tools](../app-mcp.md#tools).

### 2026-11-15 — Agent publishing, phase 2

- `draft_question` and `draft_reply` (`write`) store private question, tip and
  reply drafts beside recommendation reasons; the person publishes or discards
  them at `/me/drafts`, which shows what each reply answers and offers only
  Discard once its conversation is gone. Publishing applies the discussion
  rules and rate limit and consumes the draft in the same transaction, keyed by
  the draft, so a draft becomes at most one post. `mark_helpful` and
  `unmark_helpful` (`publish`) set the person's Helpful mark, limited and
  logged like the other publish actions. Agents still never edit or remove
  posts. Published posts show "via *app* (*host*)" in threads, Community and
  moderation views and carry `via`/`via_host` (contract 1.27.0); the person's
  edit or removal clears them. Schema 24. See the
  [guide](../app-mcp.md#publishing-in-your-name).
- Write tools refused for lacking `write` say the connection "can't make
  changes" rather than "read-only", since it may still post publicly.
  `discard_draft` reports `discarded: false` when nothing was pending and names
  a malformed `draft_id`; on `/me/drafts`, discarding a draft that isn't pending
  answers 404, as publishing does.

### 2026-11-15 — Signed-in discussion and preference tools

- Add signed-in tools for account routes that had none, each calling the
  route's service method: `my_followed_discussions`, `my_notifications` and
  `my_preferences` (`read`), and `mark_notification_read`, `follow_discussion`
  and `unfollow_discussion` (`write`). See
  [tools](../app-mcp.md#tools).

### 2026-11-15 — Public community read tools

- Add public, read-only tools for community reads that had only an HTTP
  route, each calling the route's service method: `event_recommendations`,
  `event_interested_people`, `event_discussions` and `discussion_thread`
  (a thread with a page of its replies), `community_recommendations`,
  `community_discussions`, `user_profile` and `happening_now`. Their profile IDs feed `profile_activity`. See
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

Older entries: [agent tools archive](archive/app-mcp/).
