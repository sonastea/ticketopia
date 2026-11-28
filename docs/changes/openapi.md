# OpenAPI contract changes

[Older history](archive/openapi/).

## Unreleased

### 2026-11-28 — Helpful updates in the inbox, contract v1.34.0

- `DiscussionNotification` adds `kind` (`replies` or `helpful`), `helpful_count`,
  `helpful_names` and `reply`. `reply_count` may now be 0, for Helpful updates.
  See [Helpful updates](../followed-discussions.md#helpful-updates).
- **Breaking for strict clients:** every item now has `kind`, which the old
  schema's `additionalProperties: false` rejected, and Helpful items carry
  `reply_count: 0`. Clients should accept `kind`, treat only `replies` items as
  reply counts, and skip kinds they don't know.

### 2026-11-28 — Moderation queue triage fields, contract v1.33.0

- `ModerationQueueItem` adds `pending_reasons` (reason codes with counts),
  `waiting_since` and `last_action`. See [moderation](../moderation.md).

### 2026-11-27 — Metro areas, runs and event dates, contract v1.32.0

- `GET /api/v1/events` accepts opt-in `area` (`metro`, `city`) and `fold`
  (`run`); lists add `area`, and folded items add `run` (`EventRun`). Event
  detail adds `dates` (`EventDate`, at most 100), `dates_more` and
  `Classification.type`; Radar's `scope` adds `area`. MariaDB mode no longer
  lists Upsell add-ons once tagged. See
  [metro areas and runs](../discovery.md#metro-areas-and-runs).

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
