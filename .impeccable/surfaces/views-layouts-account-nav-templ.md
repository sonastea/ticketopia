---
version: 1
slug: "views-layouts-account-nav-templ"
primary_target: "views/layouts/account_nav.templ"
related_targets: ["views/account/settings.templ", "views/account/reminders.templ", "views/home/follows.templ", "views/home/discussion_follows.templ", "views/home/recommendations.templ", "views/home/discussions.templ", "views/home/interests.templ", "views/home/moderation.templ", "views/home/radar.templ"]
---

# Neighboring-screen polish

Mode: Operate. Phase 3 of the [app-polish plan](../../docs/design/app-polish-and-event-section-navigation.md).
Preserve the incumbent Manrope/plum/light-stone world, not a redesign.

## Surface contract

- Shared presentation-only account navigation: current family and location visible,
  other Account/Your events/Updates/enabled Safety families in native More in your
  space. Routes remain document navigation; destination handlers retain permissions.
- Profile preview stays beside editing when it fits and stacks on phones/enlarged
  text. Security/API access stays secondary; short expanded navigation can scroll
  without overlapping its footer. Shell thresholds stay unchanged.
- Community/public-profile artwork and titles open Overview together with return
  context. Event/date/venue precede author/reason/conversation text; named section/
  thread links remain direct. No per-person Tickets/Save or privacy expansion.
- Artist/venue search opens on first use, submissions and errors, compacting on
  populated revisits. Native GET searches/cursors and independent follows remain.
- Followed/inbox records expose actual counts/frequency/UTC availability and read
  labels. Open conversation leads, Mark read supports; no invented excerpts or
  event-wide collapsing. Public composers retain one action and nearby audience
  guidance, scoped drafts/keys, placeholders, reply targets and moderation visibility.

## Verification and scoped self-review — 2026-10-12

| Slice/harness | Passing Chromium checks | Zero-violation axe views |
| --- | ---: | ---: |
| Account navigation | 37 | 8 |
| Artists & venues | 60 | 8 |
| Recommendations and public profiles | 124 | 22 |
| Discussions and community conversations | 69 | 14 |
| Followed discussions and reply inbox | 87 | 14 |

Batched ignored `.impeccable/review/` captures cover desktop, rail, tablet,
390/320px, short viewports and root 200%-text, long titles/bio/follow names,
native/no-JS/touch controls, errors and public/private states. Keyboard/forced-
colors focus and reduced-motion checks pass. Fixed the short-sidebar overlap
and enlarged-text notification action overflow found in those passes.

Rendering/route regressions cover family/current/role gates, fail-closed role
lookup, shared Overview identity/return context, search disclosure states,
distinct notifications/actual availability/read controls and withheld contributions.
Saved (88), Interests (87), Radar (43), reminders (100) and stable event sections
(212) also pass their isolated browser regressions. Full real-MariaDB Go race
tests, vet, embedded production build and publishing-context `make check` pass.
The mechanical detector reports only pre-existing documented provider-brand,
type/radius notices, not new phase-3 findings.

Scoped self-review: implementation and synthetic evidence agree with the brief;
not independent reviewer approval or a whole-app/public-pilot certification.
Manual screen-reader, physical-device, live OAuth/mail and comprehensive cross-
browser verification remain separate. No new palette/type/motion/raster assets;
provider imagery/fallback provenance and `.impeccable/design.json` remain unchanged.
