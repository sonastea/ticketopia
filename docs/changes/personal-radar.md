# Personal radar and reminder changes

## Unreleased

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

Saves, follows and personalized matches are now delivered; reminders and human
pilot checkpoints remain planned. Runtime discovery changes are recorded in its
[feature history](discovery.md).
