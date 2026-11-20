# App-wide rework changes

Phases of the [one job per screen](../design/app-rework.md) rework.

## Unreleased

### 2026-11-19 — Phase 5: personal collections

- Radar's scope reads "Chicago, US · Nov 19, 2026 – Feb 16, 2027". Its
  staleness, coverage state (each with its own wording, such as "not a
  confirmed empty search") and "Updated … Venue-local event times" read as one
  visible line; a result row shows "Updated…" only when its own data is stale.
- Radar's bottom note moves into the shared source footer, which now also says
  that details Ticketmaster hasn't supplied are left out rather than guessed.
- A draft's expiry joins its metadata line ("· not published, expires …").
- On wide screens, Reminders' tabs fit on one row: the page keeps the shared
  account width, with a 70ch measure for its content.
- The `Stamp` children guard now matches only a `{` on the same line as the
  call, which is when templ treats it as children.
- The reminders journey finds the Preferences city field and notification
  group unambiguously, its pause copy check matches the current wording, and it
  ignores Chromium's view-transition abort under emulated reduced motion, so it
  runs to the end again. The radar journey checks the not-collected wording.
  Run with non-fatal checks against Phase 4 with the same scripts, the radar,
  reminders, saved, follows, interests and followed discussions journeys show
  the same findings.

### 2026-11-19 — Phase 4: Community and Happening Now

- Community and its Conversations tab start with `layouts.PageHeader`; Happening
  Now uses the same header markup, keeping its focusable live heading.
- Last-known details are explained once above the Community list; each affected
  card shows "Price and sale status last known …" directly before those facts
  instead of a warning box. Counts and "first recommended" are list metadata.
- A recommender's name and "recommends this event" stay beside their avatar.
- The `empty-display` token is capped below the page heading, so "All quiet
  right now." and Community's empty states no longer outrank the page title.
- Happening Now's scope summary is list metadata and its snapshot time a
  `Stamp`. The live filter disclosure (also on Preferences) groups artists and
  venues in one fieldset whose instructions both pickers reference, and makes
  the city note field help.
- A test fails when a `Stamp` call is given children, which templ silently
  drops (it caught Happening Now's snapshot sentence during development; a
  render test now checks that sentence). A test holds
  Community, Conversations and Happening Now (live and paused) to the notes
  budget.
- In one non-fatal run each against Phase 3 (`aa59229`), the live activity,
  recommendations and discussions journeys showed no new failures; two Phase 3
  findings (recommendations' Community focus restoration and a tablet
  target-size finding measured at one capture position) did not recur. The
  live filter, entity and connection journeys ran as written (their checks
  stop at the first failure) and failed at the same disclosure-timing checks
  on both; later checks in those journeys did not run.

### 2026-11-19 — Phase 3: conversations

- A conversation's return links (where you came from, Event Overview, the
  section) share one line instead of stacking.
- **Follow** and **Your reply notifications** share one row; an open follow
  disclosure takes the full width, with the inbox link after it in reading and
  tab order. The paused-notifications advice joins the follow disclosure's one
  note.
- The composer's reply-target and public-audience notes are one note. The
  owner's Edit and Remove disclosures stay below each post's Reply, Report
  privately and Helpful row, where opening one doesn't move its summary.
- The notes check treats each post (`article`) as its own section. A test holds
  the conversation page to one note per section for owners and guests, in an
  empty thread and one with nested replies.
- The responsive journey now waits for focus to return after closing the
  preview, which on wide screens happens inside a view transition. Run with
  non-fatal checks against Phase 2 (`caff5d8`), the discussions, responsive,
  conversation ergonomics, followed discussions and drill-down journeys show no
  failure that Phase 2 doesn't also show in at least one of three runs:
  responsive scroll restoration on phones, the small-phone target-size finding
  for a link under the floating navigation, Community target-size on tablets,
  and target-size and focus-outline findings in followed discussions. One is
  new: at the tablet capture's scroll position, fixed chrome partially covers
  an "In reply to" link (axe target-size, 17px visible); readers can scroll it
  clear. That link's paragraph is now a flex line so its 44px target no longer
  overflows onto neighbors.

Older entries: [app rework archive](archive/app-rework/).
