# Event community changes

[Older history](archive/event-communities/).

## Unreleased

### 2026-10-29 — Batched moderation and community page reads

- The moderator review queue reads a page's public post projections in one query
  instead of one per row (default page: 22 statements -> 2, about 23 ms -> 3.5 ms
  locally). Order, cursors, pending counts, own-case exclusion and redaction are
  unchanged. See [moderation](../moderation.md).
- Discussion and recommendation pages send and decode each event snapshot once
  per page instead of once per row, still in one statement. With 64 KB snapshots
  a 21-row page fell by 43–56% (thread 3.8 -> 2.2 ms, community 5.8 -> 2.5 ms,
  recommendations 3.3 -> 1.5 ms; 3.0 MB -> 0.2 MB). At the 2.6 KB fixture size,
  community pages are 37% faster and recommendations 4% faster, while thread
  pages are about 5% (0.09 ms) slower. All allocate about half the bytes.

### 2026-10-29 — Optional category filters wait at most one second

- Community, discussions, Happening Now and interest pages give the optional
  category catalog Discover's one-second budget instead of the ten-second refresh.

### 2026-10-28 — Concurrent interest and recommendation reads

- A PUT that omits visibility applies the account default only when it inserts
  the choice. An explicit choice committed while that request fetched event
  details keeps its visibility, so a delayed default can no longer publish it.
- An event's recommendation page and total count share one read snapshot, so a
  concurrent withdrawal can no longer report fewer recommendations than it lists.

### 2026-10-23 — Community cards and typographic empty states

- Community recommendations and conversations are flat bordered cards in an
  intrinsic grid. Each has artwork with the ticket-stub date chip, event identity,
  each person's words as a pull quote with an initials circle, then event facts.
  City, country and category share one compact filter bar.
- Happening Now groups are compact cards. Artwork and dates come from one bounded,
  provider-free read of stored snapshots on the server render (live-pushed cards
  use fallback art; the live DTO is unchanged and no reason is shown beside
  "Someone"). Scope, filters and Refresh share one bar.
- Empty Community and Happening Now states lead with a large display line and the
  main action as a primary button. See
  [recommendations](../event-recommendations.md#public-browsing) and
  [live activity](../live-community-activity.md#attribution-and-controls).

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
