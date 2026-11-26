# OpenAPI contract changes

[Older history](archive/openapi/).

## Unreleased

### 2026-11-26 — Publishing API tokens, contract v1.31.0

- `Credential.scopes` and token creation accept `publish` (implies `write`).
  With it, tokens post, reply, recommend (never replacing the person's own),
  react, make Interested public and publish drafts through the new
  `POST /api/v1/me/drafts/{draft_id}/publish`, all carrying `via`. New problem
  codes: `browser_session_required` (profile, post edits and removals,
  moderation), `publish_limited` (429, 20 public actions an hour shared with
  agents) and `recommendation_is_personal` (409).

### 2026-11-25 — API token scopes and drafts, contract v1.30.0

- `Credential` gains required `scopes` (`[read]` or `[read, write]`), and
  `POST /api/v1/me/tokens` accepts `scopes` (default `[read]`; `publish` is 400).
- API tokens never act publicly: profile changes, public Interested,
  recommendations, posts, edits, removals, reactions and moderation decisions
  are session-only and answer tokens 403 `token_cannot_publish`; read-only
  tokens' changes get 403 `insufficient_scope`. Adds `GET`/`POST /api/v1/me/drafts`,
  `DELETE /api/v1/me/drafts/{draft_id}` and `Draft`. See
  [accounts](../accounts.md#sessions-and-api-clients).

### 2026-11-22 — Open questions and Same question, contract v1.29.0

- Adds `GET /api/v1/community/questions` (Community filters plus
  `connections=mine` when signed in) and `PUT`/`DELETE
  /api/v1/posts/{post_id}/reactions/same-question`. `DiscussionPost` gains
  required `same_question_count`, `same_question` and `first_post`.

### 2026-11-15 — Event community not-found, contract v1.28.0

- `GET /api/v1/events/{event_id}/community` answers 404 `event_not_found` for
  an ID that names no event; an event with no activity still reports zeros,
  as does any event while the provider can't be reached. Every collection's
  limit error now reads `limit: use an integer from 1 to 100` (event search,
  notifications and followed discussions said otherwise), and every malformed
  event ID `event_id: use a ticketmaster: prefixed ID`.

### 2026-11-15 — Discussion post attribution, contract v1.27.0

- `DiscussionPost`, and `post` in `ProfileActivityItem`, gain optional `via`
  and `via_host`: the connected app whose drafted question, tip or reply the
  author reviewed and published on `/me/drafts`, and its verified metadata
  host. They are omitted for the author's own writing, after the author edits
  the text, and for removed or hidden posts. Clients should show both beside
  the author as plain text. No route changes; agent tools (`/mcp`) never post
  directly, though API tokens remain full account credentials on `/api/v1`.

### 2026-11-15 — Notification not-found code, contract v1.26.0

- `PUT /api/v1/me/notifications/{notification_id}/read` answers 404
  `notification_not_found` ("This notification couldn't be found.") instead of
  `post_not_found`, and a malformed ID names `notification_id` in `fields`.
  Add the `NotificationNotFound` response and problem code.

### 2026-11-14 — Event community and profile activity reads, contract v1.25.0

- Add `GET /api/v1/events/{event_id}/community` (`EventCommunity`): public
  interest, recommendation and discussion totals plus a separate `viewer`
  object (null when anonymous) with the caller's own save, interest and
  recommendation state.
- Add `GET /api/v1/users/{user_id}/activity` (`ProfileActivityList`): one
  person's visible posts, active recommendations and public interest, newest
  first, with account-bound cursors.
- Add problem code `community_activity_unavailable`. See
  [community activity from other clients](../accounts.md#community-activity-from-other-clients).

### 2026-11-14 — Recommendation attribution and draft errors, contract v1.24.0

- `EventRecommendation` gains optional `via` and `via_host`: the connected app
  that published the recommendation or wrote its reason, and its verified
  metadata host, omitted for a person's own writing. Clients should show both
  beside the author as plain text.
- Add problem codes `draft_not_found`, `draft_limit`, `drafts_unavailable` and
  `recommendation_changed` for agent drafts (reviewed on the web at
  `/me/drafts`, not a JSON route).

### 2026-11-11 — Problem+json for unmatched API routes, contract v1.23.0

- `/api` paths and methods that match no route answer problem+json with
  `route_not_found` (404) or `method_not_allowed` (405, keeping `Allow`)
  instead of Echo's `{"message": ...}`, with `Cache-Control: no-store` and
  `Vary: Accept`.

### 2026-11-11 — Busy and not-saved errors, complete problem codes, contract v1.22.0

- Add `temporarily_unavailable` (503, `Retry-After: 5`) for an overloaded or
  unavailable database, which was reported as `provider_unavailable` ("We
  couldn't refresh the events"); `provider_unavailable` now means the event
  provider only. Pages and agent tools show the same "busy" wording.
- Setting a reminder on an event you have not saved answers **409**
  `event_not_saved` ("Save this event before setting a reminder.") instead of
  404 `event_not_found`.
- The `Problem.code` enum lists every code the API sends (21 were missing); a
  test keeps it in step with the handlers.
