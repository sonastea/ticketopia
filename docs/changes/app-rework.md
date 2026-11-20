# App-wide rework changes

Phases of the [one job per screen](../design/app-rework.md) rework.

## Unreleased

### 2026-11-19 — Phase 7: moderation

- A moderation case's status, author and reply target are one metadata line;
  reporter and moderator account IDs are metadata; a failed decision shows one
  alert with the problem and, as its own sentence, that the case shown is
  current and should be reviewed before deciding again. A test holds the case
  page to the notes budget (after a failure and once the author removed the
  reply) and checks that alert, and the moderation journey passes.

### 2026-11-19 — Phase 6: public profile, sign-in and the agents guide

- A public profile (`/users/{id}`) leads with the same card as `/me`: initial,
  name, bio, its privacy line and a way back to Discover.
- Sign-in's Google and privacy notes are one note; when sign-in continues an
  event, that event's context is its own short section.
- The agents guide uses the shared page header, with its "On this page" links as
  local navigation.
- On profiles, a recommendation row's date stub stays with its artwork instead
  of drifting to the bottom of a tall row.
- A test holds the public profile and sign-in to the notes budget.

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

Older entries: [app rework archive](archive/app-rework/).
