# Event discovery and event-view redesign

**Status:** Implemented; browser and isolated account-backed validation complete.
**Date:** 2026-10-11.
**Scope:** Event lists and the connected event overview, ticket, and community
journeys on mobile, tablet, and desktop.

## Purpose

Make browsing an event, understanding it, and finding tickets feel like one
predictable journey. Users should not need to distinguish between several
similarly named information links before reaching useful content.

The central rule is: **open the event to understand it; use an explicitly named
ticket action to visit the seller; use a named community link to join a discussion.**

This document records the agreed redesign and delivery progress. The current
[UX guide](../design-guidelines.md) and [visual system](../../DESIGN.md) remain the
implementation references until the corresponding changes are delivered.

## Problems that motivated the redesign

- Event titles select Discussion rather than the event overview.
- In Discover, “More event information” expands navigation links rather than
  additional event information.
- “View event details” selects a preview whose overview and ticket action are
  behind another disclosure.
- “Open full event details” and “Event details & actions” introduce additional,
  similarly named choices inside that preview.
- Save, Interested, reminders, activity links, and information links compete for
  attention in repeated rows. On phones, wrapping and vertical stacking amplify
  the number of decisions between events.

These findings come from the templates and navigation code, not a new live-device
usability study. Implementation must include visual and interaction validation.

## Design direction

### Event-first, with community in context

Lead with event identity, date, venue, known price, and sale status. Make opening
the event predictable and make the external ticket destination explicit. Community
remains part of deciding whether an event fits, without becoming the implicit
destination of every event title.

Borrow familiar event-first browsing and separate save/participation patterns from
providers such as Eventbrite and Meetup. These are interaction references, not
layouts or branding to reproduce. Ticketopia retains Manrope, plum actions,
light-stone surfaces, dusty-rose dates, real provider photography, and flat,
rule-separated content.

### One meaning per interaction

| Control | Intended result |
| --- | --- |
| Event title or image | Open the event overview inside Ticketopia |
| Tickets on Ticketmaster ↗ | Navigate directly to the supplied external ticket URL |
| Save / Saved | Change the private saved state, with confirmed feedback |
| Questions & tips | Open that event's discussion content |
| Recommendations | Open that event's public recommendations |
| Back to results | Return to the originating list and browsing position |

The event title and image must share a destination. Keep separate ticket and Save
controls outside their link; do not make a whole interactive row a nested link.

## Event list redesign

Each row should answer: **What is it? When and where? What is known about tickets?
Do I want to open it, get tickets, or save it?**

### Content hierarchy

1. Provider image and readable event title.
2. Venue-local event date/time and venue/city.
3. Known price or an explicit unknown, plus honest sale status.
4. Concise supporting information when useful: category, distinctive performers,
   or a short description. Avoid repeating the title as another fact.
5. A visible ticket action and a quiet Save control.
6. Compact public activity links, only when activity exists.

Illustrative content structure; dates and counts are examples, not shipping data:

```text
[Image]  Event title
         Sat 24 Oct · 7:30 PM
         Venue · City
         Known price · Sale status

         Tickets on Ticketmaster ↗     Save

         3 questions & tips
```

Remove “More event information” and its nested “View event details” link from
list rows. The title/image supplies the internal event-opening path; tickets are
available directly. Use “Tickets on Ticketmaster” consistently in every surface.

Keep Save as the only routine participation control in the list. Move Interested,
Recommend, and on-sale reminder settings into the event view. Saving never
enrolls someone in reminders or expresses public interest.

Replace multi-link activity excerpts with a compact activity summary linking to
the named sections. Full excerpts, author links, and conversation actions belong
in those sections. Omit empty activity blocks and zero-count clutter.

Apply the same navigation/action vocabulary to Discover, Saved, Interests, and
Radar. Preserve collection-specific context: removal from Saved, independent
interest state, and Radar's “Why this event” reasons. The Interests collection
may retain an explicit removal action because managing that collection is its
primary task; it must not look like removing a private save.

## Event overview redesign

Opening an event lands on Overview. Present event identity, essential facts, and
the ticket action before participation tools and extended community content.

### Reading order

1. Labeled return navigation.
2. Event identity, imagery, date/time, venue, price, and sale status.
3. “Tickets on Ticketmaster ↗” and private Save.
4. About the event and important practical information, including supplied venue
   details and “Before you go” notices.
5. Clearly grouped Interested, Recommend, and reminder settings.
6. Questions & tips and recommendations, with public interest kept distinct.
7. Secondary provenance and event history.

Show the description and ticket action without opening “Event details & actions.”
Use disclosures for genuinely secondary material such as observation history,
not for the information needed to decide whether to attend. Stale-data warnings
remain adjacent to affected facts rather than hidden with secondary provenance.

Keep section navigation where it helps readers reach long discussion or community
content. Overview is the default; explicit activity links can still open the
requested section or exact conversation directly. Avoid creating two competing
sets of event-section controls.

Private Save, Interested visibility, public recommendations, and reminder consent
retain their existing semantics. Moving a control does not merge these actions.
Reminder settings remain conditional on supported configuration and eligibility.

## Responsive journeys

### Mobile and compact tablet

- Title/image opens one primary event overview, not a choice between a preview
  and a full-details screen.
- Place the ticket action near the essential facts, before long descriptions,
  participation controls, and discussions. Large artwork must not bury it.
- Use a comfortable full-width ticket action in the event view when appropriate;
  keep Save visually secondary. Allow labels and facts to wrap naturally.
- Keep one clear Back to results action, or the named originating collection.
- Restore filters, loaded results, scroll position, and focus when returning.
- Keep community direct links useful without forcing overview-first navigation
  for someone who explicitly chose a question or recommendation.
- Account for the existing bottom navigation and safe areas. A second fixed
  bottom action bar is not required for this redesign.

### Desktop and wide tablet

- Retain side-by-side results and event context when there is sufficient space.
- Selecting the title/image shows the same overview and primary actions as mobile.
- Keep selected-row feedback and the labeled return control.
- If standalone expansion is needed, use one secondary “Open event page” link.
  The panel must already contain the useful overview and ticket action; expansion
  cannot be a required intermediate step.
- Activity links select the requested community section or conversation directly.
- Preserve the shared responsive thresholds and selection across resizing.

## States and accessibility

- Use real links for navigation and native forms/buttons for state changes.
  Search, event access, ticket links, and pagination work without JavaScript.
- Retain visible keyboard focus, labeled controls, at least 44px action targets,
  readable contrast, text selection, and reduced-motion support.
- Do not replace ambiguous text with unexplained icons. The external arrow
  supplements the named ticket provider; it is not the only destination cue.
- Preserve essential text at narrow widths and enlarged text sizes.
- When a provider URL is missing, show a concise explanation rather than a fake
  ticket button. Canceled, postponed, sold-out, unknown, and stale states remain
  explicit; a provider link is not a guarantee of ticket availability.
- Signed-out Save can initiate sign-in with return continuity. Disabled-account
  deployments explain unavailability without repeating a toolbar of inert actions.
- Failed saves retain prior confirmed state and offer nearby recovery. Preview
  failures retain return navigation and access to a standalone event page.
- Direct event and thread URLs, browser Back/Forward, and existing links continue
  to resolve. Preserve discussion drafts and reply targets during navigation.

## Delivery plan

1. **Simplify list entry points:** unify title/image destinations, remove the
   information disclosure, expose tickets, and reduce repeated participation tools.
2. **Recompose the event overview:** expose practical content and tickets, group
   secondary participation, and simplify community entry points.
3. **Unify responsive behavior:** make compact selection a complete event view,
   retain desktop context, and verify return/deep-link continuity.
4. **Align collections and states:** carry the vocabulary through Saved, Interests,
   and Radar; validate account, provider, stale, and failure states.
5. **Validate and document delivery:** regenerate affected assets, run relevant
   rendering/navigation regressions, and update the current UX guide, visual
   system, surface brief, and feature history for implemented changes.

Primary implementation surfaces are `views/home/more_events.templ`,
`views/home/event.templ`, `views/home/index.templ`, shared participation components,
the discovery navigation logic in `views/assets/app.js`, and app-owned styles in
`views/styles/app.css`. Use source templates and regenerate committed artifacts.

## Acceptance criteria

- [x] Title and image open Overview consistently across supported event lists.
- [x] A supplied ticket URL is reachable from the list in one activation without
  opening a disclosure or event preview first.
- [x] “More event information,” “View event details,” and “Event details & actions”
  no longer form a nested navigation chain.
- [x] Opening an event exposes its description, essential facts, and ticket action
  without an additional information disclosure.
- [x] Default discovery rows expose Tickets and Save, with compact activity links
  only when relevant; secondary participation lives in the event view.
- [x] Mobile has one coherent event view; desktop retains useful side-by-side context.
- [x] Return navigation restores list state, and direct community/thread links,
  drafts, browser history, and resize continuity remain functional.
- [x] Save, interest visibility, recommendations, and reminder consent remain distinct.
- [x] Keyboard, touch, narrow phones, tablet, desktop, long titles, enlarged text,
  reduced motion, forced colors, and no-JavaScript journeys are verified.
- [x] Missing ticket URLs, unavailable accounts, stale/unknown facts, canceled
  events, and failed reads/writes retain clear, truthful recovery paths.

Success means fewer decisions to reach event information or tickets, more events
that can be comfortably compared on a phone, and community that enriches the event
journey without obscuring it. Validate these outcomes through representative task
walkthroughs rather than treating fewer buttons alone as proof of usability.

## Delivery verification — 2026-10-11

- `scripts/event-context-browser.cjs` verifies production templates in Chromium
  and WebKit at eight sizes/conditions, including 320px, compact tablet, short
  desktop, long content, and 200% text. Keyboard/touch emulation, forced-colors
  focus, reduced motion, native links, provider/account states, and save failures
  pass. Chromium's eight automated accessibility scans report no violations.
- The isolated MariaDB browser harnesses verify actual save/interest/recommendation
  writes, reminder consent, collection cursors/focus, private/public projections,
  independent thread pagination, reply targets/drafts, Back/Forward/reload,
  resizing, failed reads/writes, and no-JavaScript search/pagination/publication.
  Saved, Interests, Radar, recommendations, reminders, and responsive journeys pass.
- Compact context is the primary event view. Only desktop offers the secondary
  expansion link during successful reads; failures retain standalone recovery.
  Return continuity now includes Saved, Interests, and Radar across event sections.
- [Reproduction guidance](../ui-components.md#verification) documents browser tools;
  local reports/captures live in ignored `.impeccable/review/`, including
  `redesign/event-discovery-checks.json` and `redesign-webkit/event-discovery-checks.json`.
  Provider fixtures and synthetic accounts are isolated; no live email is sent.
- This is browser-based verification, including emulated touch, not a physical-device
  usability study, manual screen-reader audit, or live Google consent test.
- Production asset/binary generation, full local-MariaDB race tests, host vet,
  and Docker `make check` pass. Generated artifacts are retained with their source.
