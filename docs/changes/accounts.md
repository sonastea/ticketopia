# Authentication and account changes

## Unreleased

### 2026-10-23 — Sign in to save completes the save

- **Sign in to save** now saves the event after Google sign-in instead of only
  returning to it. The intent rides a flow-lifetime HttpOnly cookie replaced by
  every start, applies only to the event the validated destination selects, and
  survives cancellation for retry; save failures never block sign-in. The sign-in
  page says the event will be saved. Other actions still need an explicit choice.
  See [task continuity](../accounts.md#sign-in-task-continuity).

### 2026-10-12 — Event journey consistency

- Fix priority-1 sign-in continuity: row Save selects the event, event/section
  identity and guest/canceled/start-failure returns remain clear, and Recommend
  resumes its composer. Preserve filtered origins and Saved guest pagination.
- Offer contextual sign-in after expired native/enhanced writes; retain confirmed
  state and native draft-copy guidance. Sign-in performs no participation writes,
  visibility changes or reminder configuration. See
  [task continuity](../accounts.md#sign-in-task-continuity); real-device, manual
  screen-reader, live Google and small-group city checks remain open.
- Verify 152 Chromium/WebKit journey checks and ten clean axe views, with native
  forms, keyboard guest returns, 200% text, isolated MariaDB and local OAuth fixtures.
- Keep native Save/Interested failures and direct community-read failures in their
  validated event/collection journey, including sign-in return continuity.
- Show Private/Public beside collection removal; restore public-list scroll/focus
  across event sections; name drill-down section returns and provide Overview.
- Put stale recommendation caveats before availability facts; align Artists & venues
  labels and remove reminder copy advertising out-of-scope notification kinds.
- Correct stale sorting, ticket-layout and quiet-control guidance. See the
  [interaction guide](../design-guidelines.md). Moderation case-first links are unchanged.
- Verify public-list section returns with isolated Chromium fixtures and 16
  desktop/compact/enlarged-text layouts. Existing bottom-nav labels overlap at 200%
  text; that shared-shell issue remains a separate follow-up.

### 2026-10-12 — Shorter notification preferences

- Replace permission jargon with **Email me on-sale reminders**, short local
  explanations for permission/pause, and named links for in-app reply frequency/inbox.
- Disclose server-delivery/expiry caveats under **About email delivery** and remove
  out-of-scope digest/change-email notes. Preserve checkbox behavior, accessible
  descriptions, native forms and the existing JSON/digest compatibility contract.
- Refine the visual grouping with Good CSS: align help under checkbox labels,
  strengthen label/help hierarchy and use parent-owned gaps. Move 44px action
  links out of prose so they no longer stretch line boxes; separate in-app replies
  with a quiet rule. Keep native controls, the existing font/palette and focus/press feedback.
- Keep checkbox-row hover/press feedback to a plum title tint, not a full-width
  rectangular background; preserve the full clickable label/help area.
- See [preferences](../accounts.md#profiles-privacy-and-preferences) and
  [release priorities](../goals/delivery.md#current-focus-use-refine-and-verify).
- Verify the built app with 112 isolated Chromium checks/21 clean axe views,
  including compact/enlarged-text preferences, keyboard disclosure, native/touch
  checkbox grouping and retained legacy digest values. Inspect focused desktop,
  390px and 320px captures; retain 44px action targets without stretching prose.

### 2026-10-12 — Family-local account navigation

- Share presentation-only navigation across account, Radar, follows, reminders,
  conversations and Safety destinations. Show the current family directly and
  disclose other grouped families; preserve all routes and role gates.
- Keep profile preview beside editing when it fits and stacked on phones/text
  enlargement; leave security/API tools secondary. Fix expanded sidebar/footer
  overlap in short viewports without changing shell thresholds.
- Verify 37 Chromium checks/eight clean axe views plus route/permission tests;
  see [accounts](../accounts.md#your-space-navigation) and the
  [phase-3 record](../../.impeccable/surfaces/views-layouts-account-nav-templ.md).

### 2026-10-11 — Clear, digest-preserving notification preferences

- Group Location & time zone separately from Notifications; explain email
  permission/global pause locally and link on-sale settings and reply frequency.
- Replace weekly digest's checkbox with **Not available yet**. HTML never patches
  `WeeklyDigest`, including validation rerenders; forged fields are rejected and
  the JSON contract is unchanged. See [accounts](../accounts.md).
- Cover both stored digest values, invalid-zone rerenders and category-only saves;
  the [reminder polish verification](personal-radar.md) includes responsive,
  accessibility and changed-session preference checks.

Older entries: [accounts archive](archive/accounts/).
