# Personal radar and reminder changes

## Unreleased

### 2026-10-11 — Reminder timing and readiness polish

- Implement [part 2 of app polish](../design/app-polish-and-event-section-navigation.md):
  native preset/custom timing, retained invalid input, inline errors and clear
  Save/Stop confirmations, without changing global consent or the JSON API.
- Lead settings/rows with event identity, distinct public-sale/target times and
  account time zones; explain unknown/past/elapsed timing and each delivery state
  without promising delivery or treating SES acceptance as inbox confirmation.
- Separate preference groups and retire the interactive digest field without
  modifying stored digest values; keep native forms and owner-only pagination.
- Verify spring/fall DST, strict fields, consent and session ownership in Go;
  100 isolated browser checks and 21 zero-violation axe views cover timing,
  readiness, privacy, touch/native forms and responsive/error/empty layouts.
  See [reminders](../reminders.md) and the
  [surface record](../../.impeccable/surfaces/views-account-reminders-templ.md).
- Regenerate templates/CSS and build embedded assets; pass full MariaDB-backed
  race tests, vet, Docker `make check` and the neighboring collection/conversation
  browser regressions. Live SES/inbox confirmation remains an operator step.

### 2026-10-09 — Explicit public on-sale email reminders

- Add [on-sale reminders](../reminders.md) through shared HTML/API settings,
  owner-only saved-event opt-in, lead times, status, global pause and stop links.
- Persist token-fenced settings, occurrence-unique jobs, receipts and shared SES
  pacing in schema 13; recheck fresh sale evidence and preferences before sending.
- Disable delivery by default; classify SES rejection separately from uncertain
  acceptance and never blindly replay abandoned or ambiguous sends.
- Default the SES region to `us-east-1` (N. Virginia), with an explicit
  `AWS_REGION` override for identities and production approval in other regions.
- Add [local development](../local-development.md) with LocalStack `ses-setup`
  and combined `local-setup`, loopback-only endpoints, forced dummy credentials,
  HTTP localhost links, captured messages and fail-fast workflow regression tests.
- Cover sale changes, stale/unknown/past times, pause/stop/unsave, replica claims,
  restart recovery and quota outcomes. Live provider setup remains an operator step.

### 2026-10-08 — Explainable personalized radar

- Deliver [Radar](../radar.md) on web and API through one service, using private
  city/category preferences and exact artist/venue follows, not social inference.
- Explain each match and rank distinct signals deterministically, with location
  setup, nearby starter suggestions, no-match guidance, unknown metadata and
  coverage-aware last-known results during upstream failures.
- Add owner/limit/window/result-version-bound cursors with explicit changed-set
  recovery across replicas/restarts; update OpenAPI, navigation and category copy.
- Cover ranking, privacy, stable paging, freshness and failure behavior with unit,
  handler, isolated MariaDB and optional Chromium regression fixtures.

### 2026-10-03 — All-category MVP and private saved collections

- Expand the planned audience beyond music in the [radar goals](../goals/personal-radar.md)
  while retaining the delivered music-only baseline and later follow/reminder goals.
- Clarify private saves versus social signals, revise pilot sequencing, and align
  [delivery](../goals/delivery.md) with responsive discovery/community scope.

### 2026-10-02 — Product goal, reusable API, and discovery experiments

- Document the [personal radar and reminder goal](../goals/personal-radar.md),
  with a small pilot, incremental user-outcome goals, and success signals.
- Draft a [reusable API](../design/radar-api.md) for web, mobile, and other
  clients, backed by shared services and durable event/notification state.
- Record optional [venue analysis and discovery-game experiments](../ideas/venue-insights-and-discovery.md),
  including the observations needed and how to interpret advertised price data.
- Explore [discovery games and social motivations](../ideas/discovery-games.md),
  with a passport and one-to-one pick swap as the first suggested experiment.
- Connect the implemented [discovery foundation](../discovery.md) to the first
  radar outcome and map remaining backend/frontend work in the
  [delivery checklist](../goals/delivery.md).
- Make the first discovery visit location-aware, with an editable approximate
  IP city, remembered manual choices, and a city-entry fallback. See the
  [location guide](../location.md).

Saves, follows, personalized matches and explicit on-sale reminders are delivered;
event-change emails, digests and human pilot checkpoints remain planned. Runtime discovery changes are recorded in its
[feature history](discovery.md).
