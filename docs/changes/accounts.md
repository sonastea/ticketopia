# Authentication and account changes

## Unreleased

### 2026-10-12 — Event journey consistency

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

### 2026-10-06 — Direct Google sign-in, durable accounts, and private saves

- Deliver direct Google code/PKCE/OIDC sign-in using pinned `x/oauth2` and
  `go-oidc`, with browser-bound, atomic one-time flows and verified identity claims.
- Store issuer/subject identities, hashed expiring sessions/API credentials,
  profiles/privacy defaults, and private preferences in MariaDB schema v2.
- Add profile/preferences/interests/public-profile screens, no-JavaScript forms,
  owner-only JSON endpoints, CSRF/origin checks, shared limits, and revocation.
- Brand the Google sign-in CTA with Google's current gradient logo, locally hosted
  Google Sans, and blue-filled button treatment with a white logo tile; retain
  accessible no-JS OAuth navigation and disabled-account behavior. See
  [accounts](../accounts.md).
- Keep discovery public/database-optional and other milestones truthful; default
  event-interest visibility private, notifications paused, and public projection
  limited to ID/name/bio. Use account city only after URL/browser preferences.
- Document setup, Google consent/callback requirements, grants, and the v1-to-v2
  drain/migrate/start boundary; see [accounts](../accounts.md) and
  [persistence](../persistence.md#rolling-update-compatibility).
- Verify signed local provider fixtures, real-MariaDB ownership/restart/
  concurrency/replay/limits, plus 42 browser checks and 13 zero-violation automated
  accessibility views. Real Google consent requires operator-supplied credentials.
- Pass the production asset/application build, full real-MariaDB race suite, vet,
  and OpenAPI lint (only the two existing probe warnings). The independent UI
  reviewer's account-status documentation fix was scored resolved; preserve the
  incumbent design tokens and update its prose/sidecar guardrail consistently.
- Extend the account phase with private Save/Remove, durable bookmarks and
  `/saved`; see [save behavior](../saved-events.md) and its
  [feature history](saved-events.md). Public interest and reminder delivery stay planned.
