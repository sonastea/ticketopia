# Event discovery changes

## Unreleased

### 2026-10-23 — Quieter rows and screen-reader fixes

- Drop the provider-description excerpt from rows and hide Featuring performers
  already named in the title (rows and Overview).
- Make the row title the only tab stop, focus the preview heading after choosing
  an event on desktop as on compact, and stop announcing Radar coverage notes.
- Align the search label with venue matching, shorten its placeholder, say
  **Enter a city** before one is chosen, and replace "Country code" with
  "Country (optional, 2-letter code)" across forms.
  See [discovery](../discovery.md#responsive-navigation-and-event-details).

### 2026-10-22 — Compact Discover first screen

- Merge freshness, stale fallback and coverage into one non-live results line,
  keeping `data-coverage`; fix the repeated "Showing … Showing" stale copy.
- Put a shorter sort label ("Soonest first") on the results heading line and
  move Save privacy from above every result list into the first enhanced Save's
  confirmation in each browser (page memory if storage is blocked).
- At 390×844 the first event starts at 475px with a stale, partial-coverage
  fixture; the four-item bottom bar stays on one row at 320px.
  See [discovery](../discovery.md#responsive-navigation-and-event-details).

### 2026-10-22 — Shared links with tracking parameters

- Discover, Community and event pages redirect (`302`) away from unsupported query
  keys such as `utm_*`/`fbclid` instead of returning 400; `/api/v1` stays strict.
  See [discovery](../discovery.md#json-api).

### 2026-10-11 — In-place event-section navigation

- Deliver part 1 of [app polish](../design/app-polish-and-event-section-navigation.md)
  on Discover and standalone event pages. Preserve header/action/navigation and
  participation/provenance nodes; replace only the requested section body.
- Add private, validated section fragments, canonical action returns, selected-only
  optional reads, and fresh fact/permission/CSRF reconciliation. Keep native links,
  conversation-plus-community context, and the existing JSON API.
- Commit history only after confirmed switches; restore reading position on Back,
  preserve collection return journeys and scoped drafts (including blocked storage),
  reject obsolete/redirected/mismatched responses, and offer local failure recovery.
- Verify 218 Chromium fixture checks, eight clean automated accessibility scans,
  stable DOM/document-request assertions, failure/rapid-switch/session-change cases,
  native navigation, and batched desktop/mobile captures. Physical-device, manual
  screen-reader and live OAuth checks remain outside this evidence.
- Pass isolated MariaDB browser regressions for Saved (78), Interests (77),
  recommendations (72), responsive discussions (137), Radar (38), reminders (23),
  and followed discussions (46). Build assets/binary, run the full local-MariaDB
  race suite, host vet and final Docker `make check`; only pre-existing
  design-detector advisories remain.
- See the [UX guide](../design-guidelines.md) and [fragment contract](../discovery.md#in-place-event-sections).

### 2026-10-11 — Complete event-first discovery redesign

- Begin the [approved redesign](../design/event-discovery-redesign.md): title and
  artwork open Overview across Discover, Saved, Interests, and Radar. Ticket links
  are directly visible with consistent provider naming and missing-URL feedback.
- Remove list information disclosures, repeated Interested/reminder tools, and
  activity excerpts. Preserve private Save feedback and independent interest
  management in Interests; activity count links open their named event sections.
- Expose tickets/Save, supplied descriptions, venue details, and practical notices
  without an information disclosure. Group secondary participation separately;
  retain source/history disclosure and visible stale-data warnings. Panels and
  standalone pages share Overview, Questions & tips, and Recommendations links.
- Keep selected threads and community together, native section navigation, local
  save feedback, and tab-local discussion drafts. Rename expansion to Open event
  page and add the named Radar return. See the [current UX guide](../design-guidelines.md)
  and [discovery guide](../discovery.md).
- Verify shared-row rendering/API regressions and Chromium fixture journeys at
  phone, tablet, and desktop sizes, enlarged text, forced-colors focus, and native
  event links, section selection, Back/return focus, retained drafts, and failed-save
  recovery. Check stale/canceled/missing-provider fixtures and 200% text.
- Complete compact single-view behavior, collection cursor/position/focus return
  across event sections, explicit Remove interest wording, and reminder-setting
  visibility synchronized only after confirmed saves. Supplied sold-out states
  remain explicit rather than being presented as unknown availability.
- Pass Chromium/WebKit responsive matrices and isolated MariaDB browser suites for
  Saved, Interests, recommendations, Radar, reminders, and complete discovery/thread
  journeys, including native search/pagination and consent. Automated accessibility
  scans are clean; physical-device, manual screen-reader, and live OAuth checks
  remain outside this browser-based verification.
- Production assets/binaries, targeted view/API race tests, and Docker `make check`
  (full race suite and vet) pass. Final validation also passes the full race suite
  against isolated local-MariaDB databases and host vet.

Older entries: [discovery archive](archive/discovery/).

See [discovery](../discovery.md) and [cache behavior](../cache.md) for current details.
