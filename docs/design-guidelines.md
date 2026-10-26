# Responsive UX and interaction guidelines

The implemented UI supports all-category discovery, read-only event details, and
opt-in [Google accounts/profile/privacy/preferences](accounts.md) and
[private event saves](saved-events.md), [Interested](event-interest.md), and
[public recommendations](event-recommendations.md), and
[event discussions/Helpful](event-discussions.md) and basic
[private reporting/moderation](moderation.md).

## Sources of truth

- [Product context](../PRODUCT.md): audience, purpose, and product principles.
- [Visual system](../DESIGN.md): colors, typography, spacing, and responsive layout.
- [UI components](ui-components.md): shadcn-templ primitives, theme, and native-form adaptations.
- [Discovery](discovery.md): shipped routes, search, previews, and data behavior.
- [Delivery checklist](goals/delivery.md): implementation status and future scope.

This guide owns interaction semantics. Component styling and build instructions
belong in the linked guides rather than a parallel prototype specification.

## Navigation and responsive behavior

Use **Discover** as the primary destination. `/` is its canonical route.

| Destination | Route | Current behavior |
| --- | --- | --- |
| Discover | `/` | City, keyword, dates, category/genre filters, event results, pagination |
| Event | `/events/{event_id}` | Details, Save/Interested and per-event privacy; Community shows public recommendations and public-opt-in participants; Discussion shows recent questions/tips and a public composer |
| Saved | `/saved` | Owner-only newest-saved-first collection, last-known details, and pagination; sign-in or configuration explanation when unavailable |
| Community | `/community` | One entry per recommended event, active counts and named reasons; separate `/community/discussions` collection; both have city/country/category filters and pagination. A **Happening Now** tab (`/happening-now`) holds recent live activity. Empty states name the next action (find an event, reset filters) and say "on this page" only on later pages |
| Thread | `/events/{event_id}/discussions/{discussion_id}` | Shareable root/replies, public attribution, owner edit/remove, positive Helpful and native composing |
| Interests | `/me/interests` | Paginated own event interest and per-event visibility, followed by separate private category preferences |
| Profile | `/me` | Google sign-in, own/public profile preview, privacy default, sign-out/API access |
| Preferences | `/me/preferences` | Separate location/time-zone and notification groups; short email/pause explanations, in-app reply links and optional email-delivery details; no out-of-scope digest promotion |
| On-sale reminders | `/me/reminders` | Private event-named rows with recorded targets/time zones and readiness; event settings offer preset/custom lead times, a non-guaranteed preview and native Save/Stop |
| Followed discussions | `/me/discussions` | Private conversation collection and per-thread immediate/daily/weekly or muted reply frequency |
| Reply notifications | `/me/notifications` | Private due reply updates, read state and exact visible-reply links |

The shell uses a labeled sidebar from **72rem**, a compact rail and list/context
layout from **62rem**, and a single-column layout with **Discover / Saved /
Community / Profile** bottom navigation below 62rem. Full event pages pair
facts/actions with artwork when space permits, stacking facts first otherwise.
Exact dimensions and image transitions live in
[DESIGN.md](../DESIGN.md#layout).

Compact navigation reflows into additional rows when enlarged text needs more
space. Full destination labels remain inside their targets; a sticky, normal-flow
shell row reserves the actual navigation/safe-area height without a fixed spacer
or JavaScript measurement. Document scrolling and destination order are unchanged.

### Sidebar preferences

From 72rem, the light sidebar groups **Discover / Community** under Browse and
**Saved / Interests** under Your space, with Profile fixed at the bottom. The
collapse button switches between labels and a compact icon rail; destination
labels remain accessible and appear on hover or keyboard focus. Expanded is the
default. Sidebar ordering never changes the homepage or event/filter state.

**Customize sidebar** reveals handles for dragging whole sections or links within
their section, plus keyboard-accessible Move up / Move down buttons. Section
membership stays fixed. Done saves the draft, Cancel (or Escape) restores the
previous order, and Reset to default changes the draft without committing it.
Collapse is unavailable during editing; resizing below the desktop threshold
cancels an unfinished draft rather than leaving hidden editing controls active.

Collapse and committed ordering are remembered **only in this browser**, without
an account or cross-device sync. Invalid preferences fall back to default;
new destinations append to their section while preserving compatible choices.
When browser storage is unavailable, controls still work for the current page
and feedback explains that the order cannot be remembered. Without JavaScript,
the default expanded sidebar remains usable and enhancement-only controls hide.
The intermediate rail and mobile bottom navigation retain their fixed order.
Expanded desktop navigation scrolls within its own region in short viewports,
instead of overlapping the fixed Profile/footer controls.

### Account families and neighboring screens

Use shared presentation-only `layouts.AccountNav` for personal destinations.
The current **Account**, **Your events**, **Updates**, or enabled **Safety** family
is visible directly, with ordinary links and `aria-current`; other families sit in
native **More in your space**. Reports/outcomes retain their enablement gate and
review tools their moderator gate. Do not background-swap unrelated destinations.
Profile preview remains beside editing when it fits, stacked otherwise; security
and API access stay secondary. See [accounts](accounts.md#your-space-navigation).

Community and public-profile rows share linked Overview artwork/title, full event
date and venue before authors/reasons or conversation text. Named section/thread
links retain direct entry, sorting and pagination. Do not add per-person event
toolbars. Follow search opens on first use/submission/errors and can compact on
collection revisits; existing artist/venue management remains visible and private.
Conversation records expose real frequency/counts, UTC availability and textual
read state. Open conversation is primary, Mark read secondary. Publication has one
clear action with its public audience nearby; owner/report controls remain quieter.

### Discovery controls

Search filters use a native disclosure on every layout. Category changes reset
genre selection and offer only compatible options. Results default to soonest date;
native sorting also offers latest date and name A–Z. There is no filter-chip strip
or map view. Explicit
**Load more events** pagination also works as ordinary navigation without JavaScript.

Keep normal document scrolling. The bounded desktop event preview can scroll
independently, with keyboard access and visible focus. Reserve bottom-navigation
space and safe-area insets on compact screens.

## Event presentation

Account sections use ordinary links and native forms within the same shell.
Profile editing leads with an adjacent wide-screen public preview, stacked on
compact screens. Email and preferences stay private. Invalid writes retain input
and explain that changes were not saved; settings for future participation and
external notifications do not claim those features are available. In-app followed-
discussion updates honor global pause and recheck visibility; see
[followed discussions](followed-discussions.md). Disabled accounts show
truthful configuration/recovery states. See [accounts](accounts.md).

Event dates, titles, venues, sale status, known prices, and source freshness lead.
Unknown prices never mean free. Distinguish event dates from ticket-sale dates.
Provider images retain a fixed-ratio fallback when missing or broken.

Discovery rows extend below their identity/image pair with supplied performers,
separate sale information, and a short description. Public activity adds visible
root-thread and active recommendation count links; no activity means no extra
community block. **Questions & tips** selects Discussion in context;
recommendation counts open Community. Full excerpts and attribution live there.
Reads are page-batched,
escape public text, exclude withheld/withdrawn content, and never name private
interest or saves. Optional failures leave search usable and explain that previews
could not load. See [discovery](discovery.md#web-ui).

Event titles and images are real links to the same Overview destination. Enhanced
selection highlights the matching row and opens Overview alongside results from
62rem; below that threshold it becomes the primary single-column view.
**Open event page** expands to a dedicated page. Without JavaScript the
links open dedicated event pages. Save/Remove work through private, CSRF-protected forms with row/preview
state synchronization; signed-out visitors resume through Google sign-in. Interested
has a separate selected state, visible Private/Public disclosure, aggregate count,
and native per-event visibility control; only public identities appear in participant
lists and profile activity. Changing the profile default affects new choices only.
Interest editing stays behind **Change visibility** beside the count, with only one
editor open at a time. Saving closes the editor. Confirmed controls provide success
feedback rather than persistent status paragraphs; pending/success announcements
remain available to assistive technology, and errors are single, dismissible, and
cleared by retry or a new action. Secondary **Recommend** opens a native disclosure
composer in Community and explains public attribution before publishing. Authors
can edit or withdraw without changing Save/Interested; reasons are optional and
bounded. Confirmed writes close the editor and restore focus; failed writes retain
drafts, including native session/CSRF recovery. See [recommendations](event-recommendations.md).
Do not fabricate engagement counts or successful actions.

External ticket links name Ticketmaster and do not imply ticket ownership or
guaranteed availability. Event sections use a light-stone segmented group with a
white current segment, not curved underline buttons. Discover, Saved, Interests,
and Radar rows expose **Tickets on Ticketmaster** directly and keep Save secondary.
Missing ticket URLs have an explanation. Interested and reminders live in the
event view; Interests retains its independent removal control. Saving never opts
into reminders. The former information disclosure is removed. Zero-interest counts are visually hidden
and revealed when positive. The preview has a labeled **Back to results** action
and a single container boundary. See the [design philosophy](../DESIGN.md#design-philosophy).
Dedicated pages and discovery context share one set of real section links:
**Overview**, **Questions & tips**, and **Recommendations**, with ordinary
navigation semantics rather than partial ARIA tabs. Overview exposes the supplied
description, venue address, and Before you go notices. Tickets and private Save
precede artwork and long content; Interested, Recommend, and eligible reminder
settings form a separate **Your plans & community** group below the content.
**Event source & history** is a secondary native disclosure. Stale-data warnings
remain visible beside price and sale status. On wide workspaces, selected context
takes the larger share of horizontal space. Selected threads retain community
beside or below their conversation; other selections show the requested section.
Collapsing navigation can free space for the wider layout.
From the full-sidebar threshold, selected context fills the usable viewport height
even with little activity; longer content scrolls inside it. At enlarged text sizes,
the default context width can shrink to preserve results, and narrow rows stack
their image/facts instead of leaving a cramped text column. Phone widths at
default text size keep a small thumbnail beside the facts instead.
Discussion and Community preserve the event identity.
Community lists explicit endorsements separately from opted-in interest identities;
Discussion loads recent questions. **Open conversation** selects its root/replies
in the same context, with community still available beside or below it;
**Open full conversation** opens the shareable thread page.
Compact event identity, date, venue, price and freshness remain above discussion.
Essential content no longer requires an information disclosure. The standalone
header pairs facts/actions with artwork when there is room and stacks facts/actions
first on compact screens. Selecting a community section in context omits the
Overview artwork and prose so explicit community entry stays direct.

## Interaction semantics and hierarchy

The following participation semantics are product requirements. Private Save
and Interested, Recommend, Discuss, and Helpful are implemented:

| Interaction | Meaning and visibility | Feedback and aggregate information |
| --- | --- | --- |
| Save | Private collection, independent of attendance | Reversible Save/Saved; no public counts or saver lists |
| Interested | Interest, not attendance or ticket ownership; private profile visibility by default | Separate visibility control; lists expose only opted-in identities |
| Recommend | Public endorsement with optional reason, independent of interest | Disclose attribution; edit or withdraw; count explicit endorsements |
| Discuss | Public root posts and replies about one event | Write/Reply, edit/remove own content; discussion counts mean visible root threads |
| Helpful | Positive reaction to one post/reply, not an event vote | Reversible state and post-level count; no downvote score |

Counts and current-user states are separate. Unknown counts are unavailable,
never fabricated zeroes. Private identities must not leak through lists, API
fields, feeds, or count drill-downs; aggregate counts do not guarantee anonymity
in small groups. Recommending must not implicitly save, mark interest, or post.

Keep decision-critical event facts ahead of participation. Future event controls
keep Save and Interested compact, Discuss contextual, and Recommend
secondary. Within conversations, Reply leads, Helpful is secondary, and editing/
removal use labeled native disclosures. Report privately opens an event-contextual
native form; private receipts/outcomes and authorized review live in the account area.

## Discovery-to-discussion journeys and shared state

Discovery encodes filters, `selected_event`, `section`, `selected_thread`,
`reply_to`, and the independent `discussion_cursor` in URLs. Event pages and
sections use stable IDs, `section`, and a validated `return_to` context. Direct
entry without return context provides a Discover link.

- Explicit selection creates navigable history. Back restores selection/results,
   loaded pages where browser state is available, scroll, and focus.
- Same-event sections switch in place on Discover and standalone event pages.
  Identity, ticket/save controls, navigation, participation, and source/history
  remain mounted; only the section body changes. Overview artwork stays inside
  the context section, while standalone identity artwork stays in its header.
  Loading keeps the previous body visibly labeled. Success updates the URL and
  active link together, keeps link focus, and announces completion without moving
  to the event title. Current-section clicks are no-ops; Recommend opens its composer.
  Modified clicks and JavaScript-disabled links still use their native destinations.
- Failed switches keep the previous body and URL with local Retry and an ordinary
  section link. Failed Back/Forward reads show an error for the requested section,
  not old content with a new label. Requests are aborted/superseded and time out
  after 12 seconds. Section HTML is never stored in browser history or storage.
  Fresh reads reconcile facts, warnings, permissions, action return targets and
  CSRF. Changed sessions withhold previously loaded private result controls until
  a fresh ordinary results read; cross-document/BFCache revalidation still applies.
- Reload and shared URLs reconstruct the selected event, section, thread, reply
  target and discussion page. Filter
  changes reset pagination and selection.
- Resize preserves event/thread selection and drafts without adding history.
  Compact selection stays in-shell with bottom navigation and document scrolling;
  intermediate widths use either readable list/context columns or that primary view.
- Compact context is already the full event journey and omits the desktop-only
  **Open event page** expansion. Failed reads still expose standalone recovery.
- Saved, Interests, Radar, Community and public profiles retain their original
  collection cursor, scroll, and focused event across standalone event-section navigation. Without browser
  history/storage, the normal return link retains the validated collection URL.
- Cancel obsolete requests and reject stale responses. Never display event A's
  details under event B's identity.
- Keep loading and errors local to the preview; offer Retry, Open event page, and
  return navigation while preserving usable results.
- Ordinary links/forms provide the no-JavaScript search and pagination baseline.
- Recommendation links preserve filtered community/profile pages and event
  pagination origins. Explicit Recommend intent opens the composer without a
  second click; generic Community navigation keeps the disclosure secondary.

Discussion/thread routes retain the same event and return context.
Drafts survive validation/network failures and responsive transitions. Load only
the selected conversation's initial page; keep public metadata separate from
viewer-specific state and permission-filtered responses. See the
[API plan](design/radar-api.md#contextual-loading-and-client-state).
Confirmed discussion actions refresh only the selected section. Helpful, editing
and removal preserve action focus/reading position without adding navigation entries;
publication selects the thread. Native forms and canonical thread links remain
available without enhancement. Scoped drafts use this tab's session storage;
blocked storage retains in-page drafts, not reload/cross-device persistence.

### Drill-down return hierarchy

Standalone public participants, recommendations, conversation lists and threads
share one return component. A muted, regular-weight collection link sits on its
own row above **Event destinations** navigation. The named section return and
**Event Overview** stay together, before event identity, on every screen width.
Intrinsic wrapping preserves complete labels and 44px targets at enlarged text;
existing focus, touch and forced-colors behavior applies. Collection returns keep
`data-return-results`; event destinations retain normal links and validated origins.
No history or restoration logic changes.

The priority-4 fixture checks these surfaces at 390px, 320px/200% text and desktop,
filtered Discover cursors, collection-origin parameters, rejected external origins,
native Overview links and keyboard focus. Run against isolated local MariaDB:

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 \
DISCUSSION_BROWSER_SCRIPT="$PWD/scripts/drilldown-return-browser.cjs" \
DRILLDOWN_BROWSER_ENGINE=chromium \
PLAYWRIGHT_MODULE=/path/to/node_modules/playwright \
AXE_SCRIPT=/path/to/axe-core/axe.min.js \
go test -count=1 -run '^TestMariaDBDiscussionBrowserReview$' ./internal/persistence
```

Repeat with `DRILLDOWN_BROWSER_ENGINE=webkit`. The 2026-10-12 run passed 162
checks and 12 clean axe views per engine. The existing `scripts/responsive-browser.cjs`
journey also passed, including appended results, selected threads, return focus and
scroll restoration. These fixtures and inspected captures do not establish reader
comprehension on real phones or manual screen-reader usability; those checks remain open.

## Community requirements and retained later work

Detailed scope lives in [community goals](goals/event-communities.md) and the
[delivery checklist](goals/delivery.md#mvp-release-boundary).

- Discussions are asynchronous. Root posts start newest-first, replies
  oldest-first, with stable ordering and at most one visual nesting level.
  Replies to replies name their target within the same root thread.
- Long conversations get a shareable thread route. Removed content leaves a
  content-free placeholder; no typing indicators or live-chat dependency.
  These behaviors are implemented in both selected context and dedicated threads,
  with independent root/reply pagination and local retry.
- Use one active composer. Keep it reachable above the keyboard and safe area,
  preserve drafts on errors, and warn before deliberate draft discard.
- Use sheets for short actions rather than whole nested conversations; avoid
  stacked sheets and duplicate composers.
- Saved collections remain private, retain past/canceled event identities, and
  provide clear unsave/undo feedback. Public profiles never infer saved activity.
- Category preferences are private by default and do not mark events Interested.
  Hiding interest does not hide separately published posts or recommendations.
- Community discovery uses city/category scopes with distinct recommended-event
  and event-conversation collections. Use deterministic recent ordering rather
  than claiming personalization. No group membership is required for the MVP.
- Sign-in returns to the originating event/action. Confirm mutations only after
  server success; pending/failed states must be explicit and optimistic state
  must roll back. Reporting and moderation precede a public participation pilot.

## Accessibility and verification

Collection interest removal visibly includes Private/Public. Community and profile
recommendation freshness precedes price/sale facts, with explicit stale warnings.
Standalone conversation/recommendation/participant pages name their section return
and offer Event Overview. Native Save/Interested failures retain a validated task
destination and sign-in continuity instead of redirecting recovery to Profile.

Target WCAG 2.2 AA: semantic landmarks/headings, a skip link, labeled fields and
navigation, visible focus, meaningful reading order, reduced motion, and keyboard
access. Preserve 44px action targets, readable contrast, wrapping text, and 320px
reflow. Section links stay links; future menus/dialogs must support Escape and
focus return. Avoid hover-only controls and horizontal page scrolling.

Announce preview loading/completion briefly. Selection keeps a useful focus
target and restores focus when closed. Show stale,
empty, unavailable, and error states explicitly, with appropriate recovery.

Verify search, dependent filters, pagination, selection, retry, direct URLs,
Back/reload, resize, keyboard focus, and no-JavaScript behavior across sidebar,
rail, and compact layouts. Check long content, zoom, and image failures.
Verify sidebar collapse, drag and keyboard reordering, Done/Cancel/Reset,
reload/navigation persistence, unavailable/corrupt storage, new destinations,
focus retention, and a short viewport while customizing.
Participation, privacy, authentication return, and keyboard-safe composition
need separate verification when those features are implemented.
