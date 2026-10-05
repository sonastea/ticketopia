# Responsive UX and interaction guidelines

The implemented UI supports all-category discovery and read-only event details.
Accounts, saves, interest, recommendations, and discussions remain planned.

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
| Event | `/events/{event_id}` | Overview, Discussion, and Community section links; the latter two explain planned availability |
| Saved | `/saved` | Planned private collection; availability explanation |
| Community | `/community` | Planned recommendations and conversations; availability explanation |
| Interests | `/me/interests` | Planned category preferences; availability explanation |
| Profile | `/me` | Planned accounts and privacy controls; link to Interests |

The shell uses a labeled sidebar from **72rem**, a compact rail and list/context
layout from **62rem**, and a single-column layout with **Discover / Saved /
Community / Profile** bottom navigation below 62rem. Full event pages use a ticket
column when space permits. Exact dimensions and image transitions live in
[DESIGN.md](../DESIGN.md#layout).

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

### Discovery controls

Search filters use a native disclosure on every layout. Category changes reset
genre selection and offer only compatible options. Results are ordered by date;
there is no user-selectable sort, filter-chip strip, or map view. Explicit
**Load more events** pagination also works as ordinary navigation without JavaScript.

Keep normal document scrolling. The bounded desktop event preview can scroll
independently, with keyboard access and visible focus. Reserve bottom-navigation
space and safe-area insets on compact screens.

## Event presentation

Event dates, titles, venues, sale status, known prices, and source freshness lead.
Unknown prices never mean free. Distinguish event dates from ticket-sale dates.
Provider images retain a fixed-ratio fallback when missing or broken.

Event titles and Details are real event links. Desktop selection highlights the
matching row and opens its preview; ordinary compact-screen selection opens the
event page. Save and Interested are disabled with explanatory copy. Recommend
is disabled in Community. Do not fabricate engagement counts or successful actions.

External ticket links name Ticketmaster and do not imply ticket ownership or
guaranteed availability. Section links use ordinary navigation semantics, not
partial ARIA-tab behavior. Discussion and Community preserve the event identity
while explaining their current availability.

## Interaction semantics and hierarchy

The following participation semantics are product requirements for future work,
not implemented mutations:

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
should keep Save and Interested compact, Discuss contextual, and Recommend
secondary. Within conversations, Reply leads, Helpful is secondary, and editing
and reporting use a labeled action menu.

## Discovery-to-discussion journeys and shared state

Current discovery encodes filters and `selected_event` in URLs. Event pages and
sections use stable IDs, `section`, and a validated `return_to` context. Direct
entry without return context provides a Discover link.

- Explicit selection creates navigable history. Back restores selection/results,
  loaded pages where browser state is available, scroll, and focus.
- Reload and shared URLs reconstruct the selected event and section. Filter
  changes reset pagination and selection.
- Resize preserves the selected event and section without adding history. An
  existing selected-preview URL displays context in-shell on compact screens.
- Cancel obsolete requests and reject stale responses. Never display event A's
  details under event B's identity.
- Keep loading and errors local to the preview; offer Retry, Open event, and
  return navigation while preserving usable results.
- Ordinary links/forms provide the no-JavaScript search and pagination baseline.

Future discussion/thread routes must retain the same event and return context.
Drafts survive validation/network failures and responsive transitions. Load only
the selected conversation's initial page; keep public metadata separate from
viewer-specific state and permission-filtered responses. See the
[API plan](design/radar-api.md#contextual-loading-and-client-state).

## Future community requirements

Detailed scope lives in [community goals](goals/event-communities.md) and the
[delivery checklist](goals/delivery.md#mvp-release-boundary).

- Discussions are asynchronous. Root posts start newest-first, replies
  oldest-first, with stable ordering and at most one visual nesting level.
  Replies to replies name their target within the same root thread.
- Long conversations get a shareable thread route. Removed content leaves a
  content-free placeholder; no typing indicators or live-chat dependency.
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

Target WCAG 2.2 AA: semantic landmarks/headings, a skip link, labeled fields and
navigation, visible focus, meaningful reading order, reduced motion, and keyboard
access. Preserve 44px action targets, readable contrast, wrapping text, and 320px
reflow. Section links stay links; future menus/dialogs must support Escape and
focus return. Avoid hover-only controls and horizontal page scrolling.

Announce preview loading/completion briefly. Selection keeps a useful focus
target, offers a jump to the preview, and restores focus when closed. Show stale,
empty, unavailable, and error states explicitly, with appropriate recovery.

Verify search, dependent filters, pagination, selection, retry, direct URLs,
Back/reload, resize, keyboard focus, and no-JavaScript behavior across sidebar,
rail, and compact layouts. Check long content, zoom, and image failures.
Verify sidebar collapse, drag and keyboard reordering, Done/Cancel/Reset,
reload/navigation persistence, unavailable/corrupt storage, new destinations,
focus retention, and a short viewport while customizing.
Participation, privacy, authentication return, and keyboard-safe composition
need separate verification when those features are implemented.
