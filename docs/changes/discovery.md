# Event discovery changes

## Unreleased

### 2026-11-21 — Agenda Discover, one Tickets button and shared event cards

- Date-sorted Discover reads as an agenda: visual headings open each stretch
  (Today, This week, Next week, Later in {month}, then each month) in the
  browsed city's calendar, and the soonest event on the first page leads as a
  wide **Up next** card (known, non-past date; full-width artwork in narrower
  lists, with its own image sizes). Pages share the city's day. Load more drops a
  repeated heading and focuses the first new event. Name sort stays one list.
- Result cards (Discover, Saved, Interests, Radar) share the Community cards'
  anatomy: full-bleed artwork, raised surface, a hairline action bar. Tickets is
  the event page's gradient button everywhere (rows rest without the coloured
  lift); Save carries the same ring as on the event page. In narrow rows with
  enlarged text, Tickets drops its icon rather than breaking the word.
- Row activity links became count chips (questions, recommendations); live
  count updates rewrite only the chip's label. The preview's Open event page
  and Happening Now's toast/Refresh controls are ghost actions with icons.
- Cards lead their facts with an ink price tag (From USD 25.00), shorten the
  visible date under agenda headings (full date kept for screen readers), and
  ease the photo in on hover with motion allowed. The event's Tickets shows the
  price where the bar has room. Events without artwork get a tinted placeholder
  (stored tint, or one of seven hues chosen from the ID) with a perforation dot
  grid. See [indexed event rows](../../DESIGN.md#indexed-event-rows).

### 2026-11-20 — Depth layer across Discover and the event page

- Discover's result cards (Saved, Interests and Radar stay flat), the preview
  panel, the event identity card, and Community and Happening Now cards are raised
  (soft plum-tinted elevation, a corner glow, a 2px hover lift with motion allowed).
  Tickets and the current date chip take the brand gradient.
- Quieter controls (list rows when lit, command/filter bars, chips, Save and plans,
  section tabs) get a hairline ring and soft shadow. CSS only; no behaviour change.
- Pressing an event title (iOS Safari touch-down included) no longer paints a box
  behind the text; its row or card takes the press wash and the title darkens.
  See the [depth layer](../../DESIGN.md) and the [community entry](event-communities.md).
- Chromium journeys pass with zero axe violations: recommendations, discussions,
  responsive, moderation, saved, interests and action feedback. Conversation
  ergonomics passes apart from an intermittent view-transition abort that also
  occurs on unchanged code. WebKit: action feedback, sign-in, drill-down pass.

### 2026-10-29 — Shared event-detail refreshes

- Concurrent readers of an expired or missing stored event share one provider
  read and one durable observation instead of each writing it (16 readers: 16
  write transactions -> 1). A write failure reaches every waiting reader and is
  never replayed. See [discovery](../discovery.md#loading-performance).

### 2026-10-27 — Touch-safe preview prefetch and clearer partial results

- A touch press starts its preview request only after a 100ms hold or on
  release (still before the click), so scrolls that begin on a result no longer
  reach the server: 12 of 12 sampled touch scrolls started one before, none
  after. Mouse and pen presses still start it at once.
- Uncollected dates summarize as **Results may be incomplete** rather than
  **Not collected yet**, which read as "nothing here" above shown results.
  See [discovery](../discovery.md#loading-performance).

### 2026-10-27 — Calendar downloads keep announced local times

- Calendar downloads preserve announced local times when the provider omits the
  UTC instant but supplies a valid local date, time and zone; invalid time/zone
  values keep the all-day fallback. See [discovery](../discovery.md#responsive-navigation-and-event-details).

### 2026-10-26 — Right-sized artwork, pressed prefetch and lighter long lists

- Artwork lists one crop's renditions in `srcset` with layout-derived `sizes`,
  so 2x phones take 305px thumbnails instead of 640px.
- Pressing a result prefetches its event page (speculation rules) or, on
  Discover, starts its preview request.
- Off-screen rows skip rendering once they have rendered at the current layout,
  keeping scroll restores and the selected-row correction exact.
  See [discovery](../discovery.md#loading-performance).

### 2026-10-25 — Server-rebuilt result history

- Stop storing result HTML in history. Load more mirrors the last page's cursor
  into the Discover URL (with an implicit city and dates spelled out, kept in
  step on same-document Back) as HTML-only `through=<cursor>`; the server renders
  every page through it, with current saved/interest state and CSRF, so reload
  and Back restore at once. Restores take whole pages up to 10 pages or 200
  events, drop overlapping rows, use the oldest page's time and batch private
  reads. A stale `through` on a whole page falls back to the requested page.
  See [discovery](../discovery.md#restoring-loaded-pages).

### 2026-10-24 — Selected row stays in view

- Opening the desktop preview centers the chosen row before paint, without motion,
  after cards reflow into rows. Closing or widening moves only an out-of-view row;
  visible rows never shift under the pointer. See [discovery](../discovery.md#responsive-navigation-and-event-details).

Older entries: [discovery archive](archive/discovery/).

See [discovery](../discovery.md) and [cache behavior](../cache.md) for current details.
