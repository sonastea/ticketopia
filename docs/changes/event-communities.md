# Event community changes

[Older history](archive/event-communities/).

## Unreleased

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
