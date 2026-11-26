# Authentication and account changes

## Unreleased

### 2026-11-26 — API token scopes and posting as you

- New tokens are read-only unless **Allow private changes** is checked, and
  post only when **Allow posting as *name*** is checked too; existing tokens
  keep `read write`. The token list shows each token's access.
- Without posting, tokens draft reasons, questions, tips and replies through
  `/api/v1/me/drafts` for the person to publish. With it, they post, recommend,
  react and publish drafts, labelled **Agent** "via an API client", within 20
  public actions an hour shared with agents. No token changes the profile,
  edits or removes posts, or records moderation decisions. See
  [sessions and API clients](../accounts.md#sessions-and-api-clients).

### 2026-11-18 — One job per account page

- Split the cluttered profile into **Profile** (`/me`), **Privacy**
  (`/me/privacy`), **Preferences** and **Security & Apps** (`/me/security`),
  each with one form per section and one Save. Profile now leads with a profile
  card and the three most recent public actions; Privacy uses Private/Public
  radios, a live-activity name preview and a "Who sees what" summary. Event
  category preferences moved from Interests to Preferences.
- **Sign out of all devices** replaces the button beside Sign out: it comes last
  on Security & Apps, behind a native disclosure listing its effects. Token and
  app confirmations appear in their own section; old `/me#api-access` links now
  point to `/me/security#api-access`.
- Every personal page shares `layouts.AccountHeader`. Family tabs wrap instead
  of hiding behind **More in your space**; other families are one link each, and
  pending agent drafts are counted on every account page. See
  [your space navigation](../accounts.md#your-space-navigation).

### 2026-11-14 — API token creation survives a refresh

- Creating an API token on `/me` now redirects back to the **API access** section
  instead of rendering on the POST, so a refresh no longer mints a second token or
  re-shows the secret. The secret is handed over once through a short-lived
  HttpOnly, SameSite=Strict cookie and gains a **Copy** button. See
  [sessions and API clients](../accounts.md#sessions-and-api-clients).

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
  desktop/compact/enlarged-text layouts. Bottom-nav labels that broke at 200%
  text were later fixed in the shared shell (2026-11-20: a 6em minimum column).

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

Older entries: [accounts archive](archive/accounts/).
