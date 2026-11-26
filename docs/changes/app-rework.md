# App-wide rework changes

Phases of the [one job per screen](../design/app-rework.md) rework.

## Unreleased

### 2026-11-25 — Reading-first recommendations, reordering and conversation feeds

- Event Recommendations lead with people's reasons; writing or editing your own
  is a compact action beside the list heading. A short public notice sits beside
  Publish, the full visibility note is on demand, and withdrawal asks first.
- On phones, Tickets/Save stick above the navigation on Overview only. In the
  desktop split view the selected row no longer repeats them beside the preview.
- Reordering the sidebar widens it, names the mode, keeps each row on one line
  and pairs Done with Cancel beneath the list.
- Community Conversations and Open questions lead with the question, then a
  compact event row; phones use flat, full-width rows.

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
- Happening Now distinguishes a quiet feed from a filtered empty state and keeps
  its filters to a readable width. Followed discussions lead with the visible
  question, unread replies stand out, and Saved and Interests point to their
  respective next actions.
- Artist and venue search returns directly to labeled results. Sign-in leads
  with the task, the reminder editor pairs sale time with the saved target, and
  event history separates timestamps from changes.
- First-time Discover puts city and search before optional filters.

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

### 2026-11-19 — Phase 7: moderation

- A moderation case's status, author and reply target are one metadata line;
  reporter and moderator account IDs are metadata; a failed decision shows one
  alert with the problem and, as its own sentence, that the case shown is
  current and should be reviewed before deciding again. A test holds the case
  page to the notes budget (after a failure and once the author removed the
  reply) and checks that alert, and the moderation journey passes.

Older entries: [app rework archive](archive/app-rework/).
