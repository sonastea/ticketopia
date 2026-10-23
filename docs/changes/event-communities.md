# Event community changes

[Older history](archive/event-communities/).

## Unreleased

### 2026-10-22 — Pilot empty states and Community tabs

- Empty states now depend on where they render. Event sections invite the first
  question or recommendation through the action already there. Community offers
  Find an event, or Reset filters when filters matched nothing. "On this page" plus a
  newest-page link appears only on later cursor pages. Zero counts are hidden; live
  counts that rise above zero hide the empty copy.
- Community gains a **Happening Now** tab; see
  [live activity](live-community-activity.md). Browser checks were updated, not rerun.

### 2026-10-12 — Drill-down return hierarchy

- Share a quieter collection return and grouped event-section/Overview links across
  participant, recommendation and conversation pages, fixing the split participant
  hierarchy. Preserve named links, safe origins, intrinsic wrapping and 44px targets.
- Verify 324 Chromium/WebKit checks and 24 clean axe views, plus the existing
  responsive focus/scroll restoration journey. People/device/manual screen-reader
  checks remain open. See [return hierarchy](../design-guidelines.md#drill-down-return-hierarchy).

### 2026-10-12 — Interested action and collection feedback

- Name confirmed removals, preserve surviving collection rows/focus, and distinguish
  empty later pages from an empty whole collection. Retain readable Private/Public
  state through rejected or uncertain writes; retries leave recommendations independent.
- Make the retained recommendation's disclosure label shrink/wrap at 320px/200% text.
  See the shared [action/removal fixture](../saved-events.md#action-feedback-and-collection-removal).
  Verify 548 Chromium/WebKit checks and 22 clean axe views, plus existing behavior regressions.
  Real phones/manual screen readers remain open; no undo or publication change.

### 2026-10-12 — Conversation ergonomics and draft safety

- Refresh standalone threads in place; preserve unrelated drafts and Helpful/edit/remove
  focus/reading position. Add scoped, deliberate discard and viewport-bounded composing.
- Keep interrupted publication payloads/targets/keys immutable through retry/reload;
  never turn conflicts into new posts. Select confirmed contributions explicitly.
- Verify 80 Chromium/WebKit journey checks and eight clean axe views; real-phone
  keyboards/manual screen readers remain open. See [discussions](../event-discussions.md#conversation-ergonomics-and-draft-safety).

### 2026-10-12 — Event-first neighboring screens

- Community and public-profile artwork/title open Overview together; named
  Recommendations, Questions & tips and conversation links retain direct entry.
  Event/date/venue precede attribution/text without per-person Tickets/Save.
- Place public-audience guidance beside writing actions; keep owner/report tools
  quieter without changing drafts, pagination, publication/anti-bumping or privacy.
- Verify recommendations/public-profile and discussion journeys with 193 Chromium
  checks and 36 clean axe views; broaden race/vet/Docker validation and preserve
  stable event-section DOM/document-request and collection-return regressions.
  See [recommendations](../event-recommendations.md), [discussions](../event-discussions.md)
  and the [phase-3 record](../../.impeccable/surfaces/views-layouts-account-nav-templ.md).

### 2026-10-10 — Inline event-context activity

- Show questions/tips, public recommendations, and public interest together below
  event details without preview tabs; retain full-page sections and thread expansion.
- Load activity for every preview entry point and reload, including retained threads;
  keep private interest identities hidden and community failures local.
- Use wide-screen space for a larger selected context and parallel discussion/community;
  stack when space is limited and keep Overview prose in the details disclosure.
  Preserve native forms, navigation thresholds, and scrolling for longer content.
- Extend Discover rows with supplied descriptions/performers/sale facts and compact
  public activity, spanning the row width; keep desktop selection viewport-height.
  Batch visible-root/active-endorsement reads per results page and preserve safe
  attribution, canonical conversation links, empty rows, and local failure handling.
  See [discovery](../discovery.md); the JSON event API is unchanged.
  See [responsive UX](../design-guidelines.md) and [discussions](../event-discussions.md).

### 2026-10-07 — Responsive discovery-to-discussion flow

- Select event/threads beside results or in a compact view; disclose actions and expand details.
- Preserve filters, thread/target/pages, scoped drafts and return state through
  resize/Back/reload; reuse permission-filtered, provider-free thread reads.
- Verify 134 Chromium outcomes/five clean axe scans and real-MariaDB race/vet/build.
  See [responsive UX](../design-guidelines.md) and [discussions](../event-discussions.md).
  Manual screen-reader/physical-device/cross-browser checks remain; no pilot approval.

### 2026-10-07 — Private reporting, review, and durable moderator roles

- Deliver private report receipts/status, moderator queue/context, shared reasons
  with private notes, reversible hiding/restoration, and author second reviews.
  Preserve replies and prevent hidden-text editing or stale/unseen-context decisions.
- Store moderator membership by stable account ID, with audited operator grant/revoke
  commands; deny role-table writes to runtime and make revocation effective without
  restart. No default admin, email authorization, admin web UI, or notifications.
- Add schema 8, OpenAPI 1.8.0, native forms/draft recovery, and bounded independent
  histories. See [moderation](../moderation.md) for bootstrap, retention and upgrade.
- Verify the full race suite against MariaDB, vet/build/lint, schema-7 preservation,
  races/rollback/privacy, and 51 Chromium outcomes with 13 clean axe captures.
  This completes basic reporting/review, not public-pilot or production certification.
- Finish review scores all three fixes resolved: protect case evidence from
  moderator-authors, identify personal records/direct targets, and document delivery.
