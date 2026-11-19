# App-wide rework changes

Phases of the [one job per screen](../design/app-rework.md) rework.

## Unreleased

### 2026-11-19 — Phase 2: Discover and result rows

- Result rows in Discover, Saved, Radar and Interests show a **Tickets** link and
  the Save button on one row. The button reads **Save** or **Saved**, and
  **Remove** in Saved itself; its accessible name still says the action
  ("Remove from Saved — event"), and the Tickets link is still named "Tickets
  on Ticketmaster for" the event. On Interests, Interested follows the pair.
- Each response renders at one time (`layouts.WithNow`, set from the API clock),
  so a row and its preview always agree on whether a sale has started.
- A public sale shows on a row only while it is ahead, as "Public sale starts",
  in the row's metadata style.
- The Discover preview, before an event is chosen, is one short prompt instead of
  a heading and two generic messages.
- Browser journeys updated for the account navigation (Radar, follows and
  moderation reach their pages through family links) and a follows disclosure
  check now waits for the open transition. The saved journey measures action
  layout once running animations settle (at most a second), after an
  in-flight animation intermittently read a 44px Recommend link as shorter.
  Saved (four consecutive runs), interest, radar, follows and moderation
  journeys pass.

### 2026-11-19 — Phase 1: event page

- Interested, Recommend and On-sale reminder now sit directly under Tickets and
  Save, on the event page and in the Discover preview, instead of after the
  section content. One privacy line replaces the separate Save and
  participation notes.
- A public sale shows in the identity facts only while it is still ahead.
  Without artwork, the tinted identity card leads on its own.
- Recommendation and Questions & tips notes fit the one-note budget; question
  counts are list metadata. A test holds every event section and the preview to
  the budget.
- The profile card wraps its monogram above the name at 320px with 200% text,
  where its public-profile link overflowed.
- Saved, interest (now following category preferences into Preferences) and
  discussion journeys pass. Run with non-fatal checks, the recommendation,
  sign-in continuity and event-context journeys fail the same checks on this
  phase and on the previous commit (`b701661`): recommendations' Community and
  Profile title focus and scroll restoration; sign-in's Chromium desktop
  "names event and explicit action" and nine WebKit save checks; and
  event-context's Recommend composer focus and a "result and focus
  restoration" check whose failing viewports vary between runs. The
  action-feedback journey times out on both, and the shared CSS harness's 44px
  check on the preview fails intermittently on both.
- The Event source & history disclosure keeps a public sale that already
  started; an image that failed to load stays collapsed across section
  switches; empty states read as content rather than notes; counts are list
  metadata; and the notes test also covers guest and empty sections.

### 2026-11-19 — Phase 0: shared foundations

- Add `layouts.PageHeader` (heading, one intro line, local navigation);
  `AccountHeader` now builds on it.
- Add `layouts.SectionNotice` and `layouts.ConfirmDisclosure`; account pages use
  both, and **Sign out of all devices** is the first confirm disclosure.
- Name the shared surfaces: `.summary-card` (borderless plum tint) and `.panel`
  (white, bordered), replacing account-only styling.
- Add `layouts.Stamp`/`StampDate` for posted and updated times. Without
  JavaScript they read as exact UTC. With it, `app.js` shows viewer-local
  relative time within the last day (refreshed each minute) and the exact local
  date and time otherwise, with the exact time in the title and for screen
  readers. `StampDate` always shows a date. Every insertion path is covered by
  one observer. Event start times stay venue-local. Discussion posts,
  recommendation dates, drafts, reply notifications and moderation records use
  them.
- Add `internal/htmlcheck.NotesOverBudget`. The account settings pages are held
  to one explanatory note per section, including after a failed save. Field help
  named by a control in the same section, notes inside list items and empty live
  regions don't count.
- A test fails if a generated view prints a component call as text, which templ
  does when a call follows bare text on the same line.
