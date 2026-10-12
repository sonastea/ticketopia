# App polish and in-place event sections

**Status:** Part 1 implemented; parts 2–3 remain proposed.
**Date:** 2026-10-11.
**Baseline:** `b6c61ad` (event-first redesign) and its parent `a3f34a9`
(on-sale reminders). Extends the [implemented discovery redesign](event-discovery-redesign.md),
not a replacement visual direction.

## Summary

Polish existing journeys before adding event-change emails or digests. Preserve
Manrope, plum actions, light-stone surfaces, dusty-rose dates, provider imagery,
and flat, rule-separated content from [DESIGN.md](../../DESIGN.md).

| Priority | Outcome |
| --- | --- |
| 1 | Overview, Questions & tips, and Recommendations switch without document navigation or replacing the entire event context. |
| 2 | Reminder timing, delivery readiness, and notification preferences are understandable and truthful. |
| 3 | Account, community, follows, and conversation screens follow the same hierarchy and navigation vocabulary. |

Saved, Interests, and Radar already received event-first rows and return continuity;
make regression fixes there, not another wholesale redesign. This is an **Operate**
pass: clearer decisions, predictable controls, and fewer interruptions.

## Baseline gaps and boundaries

The section-navigation gaps below describe the pre-implementation baseline.
Part 1 now uses a validated section fragment and stable event shell; reminder and
neighboring-screen gaps remain open. See the [implemented journey guide](../design-guidelines.md#discovery-to-discussion-journeys-and-shared-state).

- Discover already intercepts section links, but `select()` in
  `views/assets/app.js` replaces all of `#event-context` with loading UI and then
  new HTML. Identity, actions, navigation, and focus are recreated on each switch.
- Standalone event-section links still perform ordinary document navigation.
- `internal/api/pages.go` loads both discussions and community data for every
  full-panel read, even when only Overview is displayed.
- Reminder setup uses raw minutes; its list has little event or timing hierarchy.
- Preferences expose an editable digest choice although delivery is unavailable.
  The HTML handler currently treats an omitted checkbox as false.
- Account navigation repeats long, different destination lists. Community and
  public-profile cards still have non-linked artwork and titles opening community
  sections rather than the Overview used in the redesigned collections.

These findings are from current source and commit review. Redesign captures inform
visual continuity; older account captures are not evidence of current copy or a
new live usability audit.

Keep current routes, Go/service/persistence boundaries, privacy, sorting, cursor
semantics, and shell thresholds. No new notification kind, schema migration,
ranking system, global SPA router, automatic dark mode, or deployment work.

## 1. In-place event-section navigation

### User-facing behavior

- Keep the event identity, Tickets/Save, section navigation, participation group,
  and source/history disclosure mounted when switching sections of the same event.
  Only the section region changes; Overview artwork belongs inside that region
  in context, while standalone identity artwork keeps its current position.
- Retain real links and `aria-current`, not incomplete ARIA tabs. Modified clicks,
  new-tab actions, and JavaScript-disabled navigation use their normal destinations.
- Show a small loading message and `aria-busy` in the section region. Leave the
  existing content visible while a click-triggered request is pending, clearly
  identified as the previous section. Never blank the header or results.
- After success, update the current section and URL. Clicking the already-current
  section is a no-op. Explicit Recommend intent still opens/focuses its composer.
- Keep focus on the activated section link and announce completion briefly. Do
  not reset document/panel scrolling to the event title; keep the controls and
  beginning of the new content reachable. Restore saved reading position on Back.
- On failure, retain the previous section and URL, with local Retry and an ordinary
  link to the requested section. For failed Back/Forward loads, render a local
  error for the URL-requested section rather than mislabeling old content.

### Rendering and request contract

1. Extract a shared `EventSectionRegion` template in `views/home/event.templ`,
   used by full pages and context. Give it event ID, section, and presentation-mode
   markers so a response can be checked before mounting.
2. Reuse `GET /events/{event_id}` with `X-Ticketopia-Section: true` for section
   fragments. With `X-Ticketopia-Panel: true`, render the context variant and its
   selection-aware action return URL; without it, use standalone event semantics.
   Existing full-page and full-panel responses remain available.
3. Include both headers in `Vary`; keep `Cache-Control: private, no-store`, existing
   parameter/return-URL validation, and authentication/ownership checks. Fragment
   errors retain meaningful HTTP status codes. Never mount a redirected sign-in
   document, mismatched event/section, or unrecognized response as section HTML.
4. Load only the chosen section's optional lists plus the minimal viewer/action
   state required by the persistent controls. Separate capability flags from list
   fetching so Overview does not falsely disable Recommend. Preserve the existing
   conversation-plus-community layout for explicitly selected threads.
5. Render canonical action-return metadata with the fragment. Refresh persistent
   form return targets and section-dependent controls without replacing the shell.
   If fresh event facts or viewer/session state changed, explicitly reconcile those
   facts, warnings, CSRF controls, and permissions; never retain another viewer's
   private controls. Recommend must still work after any section transition.

### Client state and integration

- Refactor the existing selection controller rather than layering a second click
  or history handler over it. A different event may still load a full context;
  same-event sections use the narrow fragment path on Discover and standalone pages.
- Keep canonical `overview`, `discussion`, and `community` query values. Discover
  retains its filters and `selected_event`; standalone pages retain their event URL
  and validated `return_to`. Clear incompatible thread/reply/cursor/Recommend intent.
- Commit one `pushState` only after a successful user-triggered switch. `popstate`
  reconstructs the section without pushing another entry. Update the existing
  return-journey step count so Back to Saved/Interests/Radar still reaches the
  original collection after several section changes. Direct entry uses normal
  server return links when no journey exists.
- Save scroll/focus state before leaving each section; preserve discussion drafts,
  idempotency keys, reply targets, and recommendation drafts through their existing
  viewer/event-scoped session-storage and in-memory mechanisms. Switching must not
  clear or publish a draft, even when browser storage is blocked.
- Retain abort controllers, the 12-second timeout, and generation checks. Rapid
  switches and late mutation responses cannot overwrite a newer selection. Wire
  discussion confirmation events into the narrow refresh path; Helpful/edit/remove
  keep reading position and do not add history entries.
- Process inserted htmx content and existing draft initialization. Do not preload
  all sections or persist private section HTML in storage. Existing privacy-driven
  revalidation on cross-document/BFCache returns is not removed by this change.

## 2. Reminders and preferences

**Reminder detail:** event name and an Overview link; public-sale time; delivery
readiness; lead-time choice; scheduled target; Save/Stop controls. Event dates and
ticket-sale dates stay distinct, with account-time-zone labels on reminder times.

- Offer **30 minutes, 1 hour, 1 day, and Custom** through a native select. Custom
  keeps the existing integer range of 1–10080 minutes, including previously saved
  non-preset values. Show the custom field without JavaScript; enhancement may
  conditionally reveal it. Normalize HTML choices server-side into `lead_minutes`,
  leaving the JSON API unchanged and retaining strict repeated-field validation.
- Preview the target time without implying delivery is guaranteed. Unknown sale
  times remain pending; a lead time already elapsed for a future sale explains
  immediate eligibility; already-on-sale events do not acquire late reminders.
- Distinguish configuration from readiness: paused preferences, missing fresh
  evidence, scheduled, sending, SES accepted, uncertain, failed, expired, or stopped.
  SES acceptance must never be relabeled as confirmed inbox delivery.
- Show clear saved confirmation and nearby validation errors; preserve submitted
  timing choices on failure instead of sending users to a generic account error.
  Saving never unpauses delivery or changes global email consent.
- Reminder collection rows lead with event name, target time/time zone, and a concise
  state with explanatory detail. Keep owner-only pagination and existing stop/unsave
  semantics; unknown dates are not presented as scheduled.

**Preferences:** group Location & time zone separately from Notifications. Explain
email permission, global pause, and the links to on-sale settings and in-app reply
frequency at their controls, not in one long introductory paragraph.

Replace the interactive weekly-digest checkbox with a quiet **Not available yet**
note. Preserve its existing stored value by omitting `WeeklyDigest` from this HTML
form's patch, including validation rerenders; do not trust a hidden field to preserve
it. The existing JSON preference contract remains unchanged. Pausing continues to
affect both configured on-sale emails and in-app reply updates.

## 3. Extend the redesign to neighboring screens

| Surface | Bounded change |
| --- | --- |
| Profile/account navigation | Shared family-local navigation with grouped secondary destinations instead of one mixed link strip. Keep every existing destination reachable and current location visible; retain report/moderator role gates. Public profile preview stays beside editing on wide screens and stacks on phones; security/API tools stay secondary. |
| Community and public-profile event cards | Artwork and event title open Overview together. Named Recommendations/Questions & tips/Open conversation links retain direct section/thread entry. Lead with event/date/venue, then attribution and reason or conversation text; do not repeat a Tickets/Save toolbar for every person. Keep grouped-event anti-bumping, publication order, privacy and cursors. |
| Artists & venues | Separate finding a new follow from managing existing ones. Search opens for first use, submitted searches and errors; an ordinary disclosure may compact it on collection revisits. Keep existing GET filters/pagination and independent artist/venue controls; no follow implies reminders or public interest. |
| Followed discussions and reply notifications | Keep event context, follow frequency, reply count, actual availability time and read state scannable. Make Open conversation the primary action and Mark read secondary. Unread is labeled, not color-only; do not collapse distinct conversations into one event notification or invent excerpts/counts. Preserve UTC delivery grouping and exact-reply links. |
| Discussion/recommendation composers | One clear writing action, public-attribution explanation beside it, readable thread spacing, and quieter author/report controls. Preserve single-composer behavior, drafts, independent root/reply pagination, removal placeholders and moderation visibility. |
| Saved, Interests, Radar | Regression-only alignment: existing row vocabulary, Radar reasons, distinct interest/privacy controls, and collection return continuity. Keep category preferences separate from individual event interest. |

Use shared primitives and app-owned CSS, intrinsic wrapping layouts, logical
spacing, current tokens, 44px targets, visible outline focus, touch feedback and
reduced-motion support. Do not turn ordinary navigation among unrelated screens
into background swaps or hide essential information behind new disclosures.

For account navigation, show the current family's links directly and place other
families in a native **More in your space** disclosure: **Account** (Profile &
privacy, Preferences), **Your events** (Radar, Interests, Artists & venues),
**Updates** (On-sale reminders, Followed discussions, Reply notifications), and
**Safety** (existing report/outcome/review destinations, permission-gated). Use a
shared `views/layouts/account_nav.templ` with presentation-only inputs; do not
introduce a circular import between `views/account` and `views/home`.

## Delivery and acceptance

Ship in reviewable slices: **event sections → reminders/preferences → account
navigation → community/follows/conversations**. Each slice gets rendering and
behavior tests plus batched desktop/mobile captures before moving on.

- [x] Section switches on Discover and standalone pages cause no document request;
  header/action/nav DOM stays mounted. URL, active link, body and return targets agree.
- [x] Back/Forward, reload/shared URLs, result/collection return, resizing, thread
  entry, Recommend intent, rapid switching, failed fetches and late writes work.
- [x] Keyboard focus and reading position remain useful; drafts survive switches
  and blocked storage. Native links/forms and modified clicks still work.
- [ ] Preset/custom reminder choices, time-zone/DST cases, unknown/past sale times,
  pause/consent and all delivery states are truthful; errors preserve input.
- [ ] Saving unrelated preferences does not erase the stored digest choice. Owner,
  another user, guest and changed-session cases retain their existing privacy rules.
- [ ] Desktop, rail, tablet, 390/320px, short viewport and 200%-text layouts cover
  long titles/content, empty/error states, touch, reduced motion and forced colors.

Extend `internal/api/pages_test.go`, relevant account/reminder route tests and
template tests. Extend `scripts/event-context-browser.cjs` to assert document-request
counts and stable DOM identity, not just final content. Use the isolated reminders,
saved, interests, Radar, recommendations, discussions and followed-discussion
harnesses for affected behavior; add secondary-screen fixtures as needed.

During implementation, regenerate with `npm run generate` and `npm run css`, build
embedded assets, and run focused checks; broaden to race tests/vet and `make check`
for the shared navigation/assets changes. Update the implemented UX/feature guides,
visual/surface records and feature histories only when the corresponding slice ships.
Parts 2–3 retain their proposed acceptance criteria; the first slice's verification
is recorded in the discovery feature history and surface record.
