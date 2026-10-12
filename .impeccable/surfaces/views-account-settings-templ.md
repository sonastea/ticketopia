---
version: 1
slug: "views-account-settings-templ"
primary_target: "views/account/settings.templ"
related_targets: ["views/account/presentation.go"]
---

# Account destinations

## Notification copy refinement — 2026-10-12

- Keep the existing Operate surface, palette, native forms and preference semantics.
  Use **Email me on-sale reminders**, short control-local help, **Replies in the app**
  links, and optional **About email delivery** details for server/expiry caveats.
- Group labels/help in one native checkbox target, align descriptions beneath titles,
  use .875rem/1.5 help and parent-owned gaps, and put 44px links on separate rows.
  A quiet rule separates reply settings; keep first-line checkbox alignment on wraps.
- Hover/press tints the title only, without a rectangular row fill. The full native
  label/help target and keyboard focus remain unchanged.
- Remove digest/change-email availability notes: those notification kinds are now
  out of scope, not future promises. HTML still preserves the legacy stored digest
  value by omitting its patch; the JSON compatibility contract is unchanged.
- Everyday app use and bounded nuisance/flow fixes replace another broad cohesion
  pass. See [verification priorities](../../docs/goals/delivery.md#current-focus-use-refine-and-verify).
- Verify 112 synthetic Chromium checks and 21 clean axe views with isolated MariaDB;
  inspect desktop, 390px, 320px and enlarged-text captures, including focused settings.
  Keyboard disclosure and native/touch checkbox grouping work. The detector has no
  findings in the new rules; the full stylesheet flags existing documented Google
  branding and type/radius values outside this change.
  Styling is scoped to Notifications; no tokens, notification behavior or shipping raster assets change.

## Neighboring-screen extension — 2026-10-12

Shared account-family navigation and intrinsic profile-preview stacking now ship;
security/API controls remain secondary. The [phase-3 record](views-layouts-account-nav-templ.md)
owns this slice's rendering/role/native navigation checks and eight clean axe views.
Existing public/private boundaries and the incumbent tokens remain unchanged.

Mode: Operate. Extend the existing Profile/Interests destinations and established
visual world; no redesign. The user selected direct Google OAuth2 sign-in.

## Direction contract

**THESIS:** A usable personal space with unmistakable public/private boundaries,
not a promotional sign-up page or a simulation of undelivered saved/community work.

**OWN-WORLD:** Inherit DESIGN.md's Manrope, plum actions, light-stone navigation,
flat surfaces, labeled native forms, responsive shell, and visible focus.

**STORY:** Sign in with Google, return to the original local task, choose a public
display name/bio, inspect the public projection, save private preferences, and
manage event interest above private category preferences on `/me/interests`.
Set visibility for new choices or change an individual choice; sign out or revoke
credentials. No Google real name is imported by default.

**FIRST VIEWPORT:** Existing global navigation around a clear heading and real
Profile/Preferences/Interests links. Profile editing leads, with a distinct public
preview alongside on wide screens and beneath on mobile. Security follows;
technical API controls stay disclosed until needed. Forms work without JavaScript.

**FORM:** Narrow extension of the incumbent account destinations; user-pinned
Google-only implementation, inherited seed `6b2b9e52`, code-led.

**FINISH:** unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Boundaries

MariaDB owns accounts, hashed credentials, and independent durable event interest.
Interest identity starts private; the profile default affects NEW choices only.
Each selected event has a native visibility disclosure. Private category preferences
do not create event interest, personalization, or delivered reminders. Public
profiles expose the local ID, display name, bio, and explicitly public event
interest; email, saves, private interest, and preferences remain private. No new
global design tokens ship. Google sign-in is a scoped provider-brand exception using
Google's unmodified gradient G raster and self-hosted Google Sans Medium; the
surrounding account UI retains Manrope and the incumbent world.

## Interested extension / finish record — 2026-10-06

- The own paginated collection now leads `/me/interests`, above unchanged private
  category preferences. Profile privacy explains NEW-choice defaults; public
  profiles list only opt-in public event activity. Native and enhanced forms remain.
- The [shared Interested finish record](views-home-index-templ.md#interested-extension--finish-record--2026-10-06)
  records 54 fixture checks/seven zero-violation axe captures and the handoff's
  race/vet/build/OpenAPI results. Reviewer **ship** applies only to visible-label
  accessible names and participant return continuity, not whole-surface approval.
- Account/home templates, CSS, and desktop/compact collection captures retain
  Manrope, existing account links/forms, image/date rows, flat borders, plum states,
  and visible focus. DESIGN.md frontmatter and all aesthetics remain unchanged;
  no new tokens or raster assets, and `.impeccable/design.json` is not regenerated.

## Reminder/preferences extension — 2026-10-11

- Preferences keeps this Operate surface and its existing account navigation.
  Location & time zone and Notifications are distinct; permission/global pause
  explain their scope beside controls and link reminders/reply frequency.
- Digest is a non-interactive availability note. HTML omits the stored-value
  patch rather than trusting a hidden field; JSON is unchanged.
- The [reminder finish record](views-account-reminders-templ.md) covers the shared
  100 browser checks, 21 zero-violation axe views and desktop/compact/error/text
  captures. No new palette/type/motion/raster assets; neighboring account-family
  navigation remains part 3, not silently included in this slice.
