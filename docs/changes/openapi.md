# OpenAPI contract changes

## Unreleased

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

### 2026-11-08 — Per-event freshness on stored lists, contract v1.21.0

- Add optional `Event.meta` (`Freshness`) to stored (MariaDB) event list items;
  `stale` marks older or omitted events awaiting verification. Fresh complete
  coverage no longer makes the list `meta` stale for them. See
  [event history](../event-history.md#stored-discover-and-coverage-aware-reads).

### 2026-10-26 — Event calendar file, contract v1.20.0

- Add `GET /api/v1/events/{event_id}/calendar.ics`: one public `text/calendar`
  VEVENT with event-detail cache semantics and no query parameters. A date that is
  still TBA/TBD returns 404 with the new `date_not_announced` problem code. See
  [discovery](../discovery.md#json-api).

### 2026-10-23 — Artwork tint on events, contract v1.19.0

- Add optional `Event.tint` (`#rrggbb`), an artwork-derived header colour
  normalized to a fixed OKLCH lightness and computed once when the event is cached
  or stored. Clients may ignore it. See [discovery](../discovery.md#responsive-navigation-and-event-details).

### 2026-10-07 — Private reporting and moderation contract v1.8.0

- Document owner-only receipts/outcomes, private author review requests, authorized
  queue/case reads, and atomic keep/hide/restore with stale-context rejection.
- Add safe hidden-post projections and independent bounded history cursors;
  distinguish shared reasons from moderator-only evidence/notes and author appeals.
  See [moderation](../moderation.md#api).
- Validate the contract with only the two existing health-probe warnings.

Older entries: [OpenAPI contract archive](archive/openapi/).
