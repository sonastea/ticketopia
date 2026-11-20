# Event community changes

[Older history](archive/event-communities/).

## Unreleased

### 2026-11-20 — Readable event community notes and composer cards

- On the event, recommendations, questions and replies share one note shape:
  initials avatar, linked name and date on one 44px line, then the person's words
  at body size in ink. Your own recommendation is marked **You** on a pale-plum wash;
  public interest shows as name chips.
- Composers are cards whose summary invites the action; publishing is the primary
  action and spans phone-width cards. Each post has one compact action row (Reply,
  Helpful, Edit, Remove, Report) with full accessible names. Publishing, privacy,
  public-audience guidance and the API are unchanged. See [DESIGN.md](../../DESIGN.md#public-recommendations).
- Chromium journeys pass with zero axe violations: recommendations (126 checks/22
  views), responsive (150/5), discussions (69/14), plus moderation. Browser scripts
  now use the stable `[data-recommendation-editor]` hook and the note's body path.
  Real phones, WebKit and manual screen readers remain unchecked.

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
