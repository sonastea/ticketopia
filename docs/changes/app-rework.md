# App-wide rework changes

Phases of the [one job per screen](../design/app-rework.md) rework.

## Unreleased

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
