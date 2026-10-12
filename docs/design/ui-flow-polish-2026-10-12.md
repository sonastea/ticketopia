# UI flow polish priorities — 2026-10-12

Status: the confirmed enlarged-text navigation defect is fixed. The remaining
items are prioritized follow-up checks, not confirmed defects or shipped changes.
Preserve the existing visual system; fix observed friction rather than start
another redesign. See the [current delivery focus](../goals/delivery.md#current-focus-use-refine-and-verify).

## Completed: enlarged-text compact navigation

- Compact navigation reflows intrinsically from four destinations in one row to
  two rows when text needs more space. Full labels and 44px targets remain visible.
- The sticky navigation occupies a normal-flow shell row, reserving its actual
  height, including the bottom safe area, without JavaScript measurement.
- Keep ordinary document scrolling, link/current-page semantics, native no-JS
  navigation, and the existing 62rem rail/72rem desktop thresholds.
- Regression checks cover label containment and content clearance at 320px/390px
  with 200% text, in addition to the existing shared-CSS browser matrix.
- Verified with 267 Chromium and 263 WebKit checks, six clean automated axe views,
  and inspected compact/enlarged/rail captures. Physical devices and manual screen
  readers were not tested; safe-area padding is retained, not a hardware claim.

## Next choices, in priority order

### 1. Sign-in and task continuity

**Why first:** authentication interrupts the main discovery-to-participation flow.

- Walk Discover → event → sign in → Save, Interested, or Recommend → return.
- Exercise canceled sign-in, expired sessions, filtered/paginated origins and native
  links/forms. Use test accounts; live Google consent needs a separate manual check.
- Fix only observed lost context, unclear next actions or redundant steps. Sign-in
  must not implicitly publish, change interest visibility or configure reminders.
- **Done when:** the event/section and origin remain clear; the intended explicit
  action is easy to complete and its confirmed result is obvious.

### 2. Phone conversation ergonomics and draft safety

**Why next:** an awkward composer or lost draft costs more than visual inconsistency.

- Walk question → reply → Helpful on a real phone with the keyboard open.
- Try long replies, reply-to-reply targets, Back, resize and interrupted submissions.
- Keep one active composer reachable above the keyboard; distinguish pending,
  failed and confirmed posts without shifting the reader away from the conversation.
- **Done when:** drafts and reply targets survive supported failures/transitions,
  deliberate discard is clear, and retry cannot create duplicate contributions.

### 3. Action feedback and collection removal

**Why:** users need to trust that private choices changed exactly as requested.

- Review pending, success and failed Save/Interested writes; verify Private/Public
  remains readable and independently published activity is unaffected.
- Remove first, middle and last entries from paginated Saved/Interests collections.
  Check remaining-item/empty-state focus, reading position and recovery guidance.
- Improve wording, grouping or recovery only where a journey demonstrates friction;
  do not add a new undo mechanism unless existing recovery proves insufficient.
- **Done when:** confirmed state is unambiguous, errors retain prior state, and
  removal never strands keyboard or screen-reader users.

### 4. Drill-down return hierarchy

**Why:** collection, event section and Overview are different navigation levels.

- Compare participant, recommendation and conversation pages at phone widths.
- If readers hesitate, make the collection return visually secondary and group
  event-level destinations consistently. Preserve named links and safe origins.
- **Done when:** readers can predict where each return goes without trial clicks;
  filtered cursors, focus and scroll restoration still work.

### 5. Bounded reporting/moderation polish

**Why later:** this is a specialized safety task, not everyday event browsing.

- Walk report → receipt and authorized review → decision → author outcome.
- Check event/contribution identity, retained validation drafts, confirmation and
  recovery. Keep intentional case-first links unless moderator use exposes a problem.
- **Done when:** actions and outcomes are clear without leaking private evidence.
  Any privacy/access defect takes precedence over this polish ordering and must be
  resolved before a public pilot.

## How to choose the next fix

Start with priority 1 and include two or three people in a well-covered city.
Record the route, device, expected action and observed friction. Fix blockers,
data/draft loss and misleading state before spacing or wording preferences.
Validate affected journeys on real phones, Safari/Chromium, keyboard and a screen
reader; browser fixtures supplement rather than replace those checks. Keep palette,
typography and overall layout unchanged. This document does not claim those manual
journeys, live OAuth or public-pilot verification have been completed.
