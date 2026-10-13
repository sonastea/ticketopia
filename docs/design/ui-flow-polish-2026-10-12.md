# UI flow polish priorities — 2026-10-12

Status: the confirmed enlarged-text navigation defect is fixed. Priorities 1–4
now have bounded continuity/draft-safety/action-feedback/return-hierarchy fixes and automated fixture coverage;
their manual journey checks remain open. Other items are prioritized follow-up
checks, not confirmed defects.
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

**Implemented follow-up:** row Save now resumes the selected event with its origin;
sign-in names the event/section and preserves guest/canceled/start-failure returns.
Recommend resumes its open composer; expired writes expose contextual sign-in
without changing confirmed choices. Saved pagination survives guest sign-in.
Native recommendation recovery asks for copying the retained draft before leaving.
See [sign-in task continuity](../accounts.md#sign-in-task-continuity) for the contract
and fixture command. The run passed 152 Chromium/WebKit journey checks and ten
clean axe views. This is not completion of the people/device/live-consent checks below.

- Walk Discover → event → sign in → Save, Interested, or Recommend → return.
- Exercise canceled sign-in, expired sessions, filtered/paginated origins and native
  links/forms. Use test accounts; live Google consent needs a separate manual check.
- Fix only observed lost context, unclear next actions or redundant steps. Sign-in
  must not implicitly publish, change interest visibility or configure reminders.
- **Done when:** the event/section and origin remain clear; the intended explicit
  action is easy to complete and its confirmed result is obvious.

### 2. Phone conversation ergonomics and draft safety

**Why next:** an awkward composer or lost draft costs more than visual inconsistency.

**Implemented follow-up:** standalone conversations refresh in place, retaining
Helpful/edit/remove focus, reading position and unrelated drafts. Reply targets
keep separate drafts through Back/reload/resize; explicit discard clears only the
chosen draft. Pending fields stay selectable, and long fields adapt to the visual
viewport while ordinary scrolling remains available. Interrupted publication keeps
its body, target and retry key immutable until confirmed; key conflicts never
silently become new posts. Confirmed publication selects the exact contribution.
See [conversation ergonomics and draft safety](../event-discussions.md#conversation-ergonomics-and-draft-safety)
for the contract and fixture command. Chromium/WebKit passed 80 journey checks and
eight clean axe views. Reduced-height fixtures are not real-phone keyboard tests;
the physical-device and manual screen-reader checks below remain open.

- Walk question → reply → Helpful on a real phone with the keyboard open.
- Try long replies, reply-to-reply targets, Back, resize and interrupted submissions.
- Keep one active composer reachable above the keyboard; distinguish pending,
  failed and confirmed posts without shifting the reader away from the conversation.
- **Done when:** drafts and reply targets survive supported failures/transitions,
  deliberate discard is clear, and retry cannot create duplicate contributions.

### 3. Action feedback and collection removal

**Why:** users need to trust that private choices changed exactly as requested.

**Implemented follow-up:** confirmed collection removals keep surviving rows mounted,
retain reading position and focus, and name the removed event. An emptied later page
explains that newer choices may remain and focuses its newest-page recovery link;
whole-collection empty states lead to Discover. Transport/unreadable-response failures
retain confirmed controls and explain safe retry, including a committed write whose
response was lost. Private/Public stays explicit; independent recommendations remain
published. Their disclosure label now wraps at 320px/200% text. No undo, palette,
type or navigation-threshold change was added. See
[action feedback and collection removal](../saved-events.md#action-feedback-and-collection-removal)
for the fixture command. Chromium/WebKit passed 548 journey checks and 22 clean axe
views. Physical-device and manual screen-reader checks remain open.

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

**Implemented follow-up:** participant, recommendation and conversation pages share
a quiet, named collection return above a labeled group of event-section and Overview
links. This fixes the participant page's adjacent collection/section links and
separated Overview destination. Links retain safe origins and wrap with 44px targets.
See [drill-down return hierarchy](../design-guidelines.md#drill-down-return-hierarchy)
for the contract and fixture command. Chromium/WebKit passed 324 checks and 24 clean
axe views; the existing responsive restoration journey also passed. People/device
and manual screen-reader checks below remain open.

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
