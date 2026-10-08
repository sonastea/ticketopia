# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Users

People looking for worthwhile nearby events and practical advice from others
who share their interests. Discovery spans Ticketmaster event categories. The
responsive web experience serves phones, tablets, and desktop browsers through a shared backend
that will also support other clients.

## Capabilities and Constraints

The Go/Echo, templ, htmx, shadcn-templ, and Tailwind web app provides all-category
discovery, city/date/category/genre search, and cached event list/detail reads.
The [responsive UX guide](docs/design-guidelines.md) describes the implemented
navigation and interaction contract.
Opt-in Google accounts provide MariaDB-backed sessions, profiles/privacy defaults,
private preferences, API credentials, and durable private event saves with
last-known metadata. Independent Interested choices provide private-by-default
identities, public counts, and explicitly public participant/profile activity.
Public recommendations provide optional short reasons, edit/withdrawal, event/profile
attribution, and grouped city/category browsing independently of private interest.
Community shows each event once at its original first-recommendation position;
withdrawal/republication cannot bump it. Publication/reactivation measurements are
observe-only, without recommendation quotas or automatic account restrictions.
Event discussions provide public questions, replies, owner edit/removal, Helpful
acknowledgments, direct threads, and city/category browsing. Reporting/moderation
remain planned and gate a public community pilot.

## Product Purpose

Help someone discover an event, save it privately, express interest, recommend
it, and join useful event-specific conversations. Events are the central objects
connecting local discovery and community participation. Useful reminders remain
an agreed subsequent outcome. The discovery foundation uses efficiently cached
Ticketmaster data; see the [delivery checklist](docs/goals/delivery.md) for scope
and implementation status.

## Brand Personality

Friendly, clear, practical. Combine the local discovery clarity of Yelp/Google
Maps, the contextual conversation organization of Slack/Teams, and the shared
interests and contributions of Reddit/Meetup. Keep accurate dates, venues, known
prices, and ticket links central. These are interaction references, not skins to
copy. [Design guidelines](docs/design-guidelines.md) own navigation, screen
structure, responsive behavior, and interaction semantics.

## Anti-references

Avoid promotional clutter, manufactured urgency, and browsing patterns that hide
the information needed to decide whether an event fits. Avoid a Ticketmaster
clone with appended comments, an undifferentiated social feed, or a workplace
messenger. Do not present asynchronous discussions as live chat.

## Design Principles

- Discovery first, community throughout: conversations enrich event decisions.
- Keep every discussion visibly associated with its event.
- Use progressive disclosure and predictable navigation with low-friction actions.
- Design desktop, tablet, and mobile independently with essential feature parity.
- Keep private saves, social interest, event endorsements, and post reactions distinct.
- Prefer asynchronous threads over advanced messaging complexity.
- Start with a relevant city and make the location visible and easy to change.
- Keep event dates distinct from ticket-sale dates.
- Make unknown information and data freshness explicit.
- Carry the same event identity and behavior across web and API clients.
- Deliver and verify small user outcomes before expanding.

## Accessibility & Inclusion

Target WCAG AA: keyboard navigation, visible focus, readable contrast, labeled
forms, understandable error/empty states, and reduced-motion support. Preserve
core search and pagination behavior without JavaScript.
