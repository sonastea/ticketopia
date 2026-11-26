# Agent tools (MCP) changes

[Older history](archive/app-mcp/).

## Unreleased

### 2026-11-25 — API token scopes on `/mcp`

- An API token keeps its own scopes on `/mcp`: a read-only token's change tools
  get 403 `insufficient_scope` with a message to create a token that allows
  private changes. Tokens still never hold `publish`. Scripts draft through
  `/api/v1/me/drafts`, sharing the drafting tools' service. See
  [Signing in](../app-mcp.md#signing-in).

### 2026-11-22 — Open questions and Same question tools

- `open_questions` (public) lists questions nobody has answered yet on upcoming
  events, with Community's filters. Signed in with `read`, `mine` keeps the
  caller's saved, Interested and followed events. `mark_same_question` and
  `unmark_same_question` (`publish`) mark someone else's question as the
  caller's too. Posts now carry `same_question_count`, `same_question` and
  `first_post`. See [Agent tools](../app-mcp.md).

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
