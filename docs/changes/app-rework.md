# App-wide rework changes

Phases of the [one job per screen](../design/app-rework.md) rework.

## Unreleased

### 2026-11-25 — Flat, event-first discovery and community surfaces

- Canceled and postponed events direct people to event updates instead of
  selling tickets. Rescheduled and last-known events ask people to verify
  ticket details; last-known sale status is explicitly labeled.
- App consent starts read-only. Private changes and public actions are
  independent opt-ins and the summary next to Connect names the chosen access.
  See [agent access](../app-mcp.md).
- Discover uses a compact thumbnail when the lead has no provider image.
  Date presets and event sections wrap rather than clipping, and empty keyword
  searches can clear the keyword without losing the other filters.
- Community filters start collapsed with an active-filter summary so the
  recommendations come into view sooner. Radar now names its results section.
- Account pages use a compact tab grid and add jumps to profile editing,
  notification preferences, and security controls. Reminders explain the
  actual pause setting without changing the saved timing.
- Moderation puts the submission and review state ahead of event metadata.
  Case identifiers are disclosed separately, and privacy guidance is shown
  immediately before the shared decision reason.
- Temporary event lookup failures offer retry without leaving the event.
  An invalid reminder link explains that nothing changed and points to reminders.

- Discover, Saved, Interests, Radar, event details and previews, Community,
  and Happening Now use borders and flat fills instead of decorative shadows,
  gradients, and hover lifts. Event composers, notes, and quotes follow the
  same treatment; focus and reorder markers remain. Linked and newly arrived
  posts keep their fill and forced-colors outline without a shadow.
- Tickets stays solid plum in event details and previews and becomes outlined in
  lists, beside quiet Save. Publish is solid plum; the current date shortcut
  has a pale-plum fill and plum border.
- Discover's lead uses Bricolage Grotesque and a wide 2:1 crop in narrow lists;
  regular rows retain Manrope. Desktop date groups stick on opaque paper; the
  idle preview has one border and a compact ticket mark. Pagination skeletons
  stay still in all motion modes. Missing artwork uses a
  flat event tint, perforation dots, and a solid icon tile; provider images,
  breakpoints, and interactions are unchanged. See [DESIGN.md](../../DESIGN.md).
- A tighter Discover filter strip leaves more room for the agenda. Shared date
  markup keeps venue-local times and unknowns consistent across event surfaces.
- Community uses visible filter labels and compact thumbnail-and-identity rows;
  Community and personal collections share quieter heading and list metadata.
- Phone discussions use full-width rows and dividers instead of nested cards.
  Threads lead with the post before follow settings, keep Reply and Helpful
  visible, and group edit, remove, and report under a native More disclosure.
  See [conversation ergonomics](../event-discussions.md#conversation-ergonomics-and-draft-safety).

### 2026-11-20 — Follow-up: error pages, consent, and the browser journeys

- One error layout, `layouts.ErrorPage`, for the 404/405 page and the account,
  event task, app connection and follow failures; the offline page repeats its
  shape. The moderation case page uses `PageHeader`.
- The consent page states what an app can and can't do in a "What it can do"
  card; the publishing line still follows its checkbox. Tests hold consent to
  the notes budget.
- Fixes found by the journeys: removing a Saved or Interested row keeps the
  next action above the phone navigation (stale row placeholder sizes, and a
  WebKit scroll shift as rows land, unless the person scrolls first); closing a
  compact preview brings the focused title clear of the header and navigation
  when its old offset no longer fits; Recommend focuses the composer once its
  disclosure opens, unless focus has moved on; the phone navigation's columns fit
  "Community" whole at enlarged text (wrapping to more rows instead).
- Journeys updated for current behavior: Sign in to save completes the Save;
  accounts no longer start with notifications paused; the service worker is
  blocked where `page.route` must see navigations; checks wait for disclosures,
  transitions and focus instead of sampling at once; and axe scans share
  `scripts/axe-scan.cjs`, which waits for animations and re-checks a target
  covered by the fixed navigation after scrolling it clear, dropping it only if
  it then passes. A paused account stays paused through a reminder save. Playwright's WebKit with
  JavaScript off leaves some collection loads at "interactive" (cached font
  preloads), so those loads wait for the DOM.
- `TestCappedRebuildHoldsQueuedAccounts` lengthens the rebuild hold for its run:
  convergence takes several 2s sweeps, which a loaded race build can push past
  the 10s production hold.
- Discover's height target stays a deliberate departure: the cards keep their
  artwork.

### 2026-11-19 — Phase 7: moderation

- A moderation case's status, author and reply target are one metadata line;
  reporter and moderator account IDs are metadata; a failed decision shows one
  alert with the problem and, as its own sentence, that the case shown is
  current and should be reviewed before deciding again. A test holds the case
  page to the notes budget (after a failure and once the author removed the
  reply) and checks that alert, and the moderation journey passes.

Older entries: [app rework archive](archive/app-rework/).
