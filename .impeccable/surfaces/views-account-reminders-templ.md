---
version: 1
slug: "views-account-reminders-templ"
primary_target: "views/account/reminders.templ"
related_targets: ["views/account/reminders.go", "views/account/settings.templ"]
---

# On-sale reminders and preferences

## Neighboring-screen extension — 2026-10-12

Reminder pages now use shared Updates navigation; Saved stays directly reachable
and preferences/help links remain local. Timing/delivery/privacy behavior is
unchanged. The [phase-3 record](views-layouts-account-nav-templ.md) records shared
navigation and the rerun 100-check/21-clean-axe reminder regression harness.

Mode: Operate. Part 2 of the [app-polish plan](../../docs/design/app-polish-and-event-section-navigation.md),
extending the incumbent account surfaces, not replacing their visual world.

## Surface contract

- Preserve Manrope, plum actions, light-stone shell, flat separators, native
  controls, logical spacing, 44px targets and visible outline focus.
- Settings lead with event identity/Overview, public-sale time (not event date),
  saved timing, readiness and the worker's recorded target. The rule-separated
  timing preview is a proposal, not a delivery promise. Presets normalize to the
  unchanged minutes service; Custom retains saved/non-preset and invalid values.
- Configuration never grants email consent or unpauses notifications. Unknown
  times are unscheduled, future elapsed leads can be eligible immediately, past
  sales never get late emails. Explain each send state; SES accepted never means
  inbox delivery. Stop and unsave semantics, private pagination and JSON stay intact.
- Preferences groups location/time-zone and notifications, with local permission,
  pause and reply-frequency guidance. Digest is unavailable and has no HTML patch
  or hidden preservation field. Account navigation remains unchanged in this slice.

## Verification record — 2026-10-11

- Go handler/template coverage: preset/custom bounds, repeated fields, local
  errors/retained choices, spring/fall DST, owner/guest/changed-session privacy,
  pagination, unchanged JSON digest/lead contracts and digest-preserving rerenders.
- Isolated MariaDB/Chromium fixture: 100 checks and 21 zero-violation axe views.
  Batched captures in `.impeccable/review/` cover reminders and preferences at
  desktop/rail/tablet, 390/320px, short viewport and 200%-text, plus known/unknown
  sale, collection, empty and validation states. Native/touch Save/Stop, keyboard
  and forced-colors outlines, reduced motion and every delivery state are checked.
- Corrected the reminder empty-state CTA's inherited dark-on-plum contrast and
  restored event-title hierarchy in collection links. No new tokens, animation
  or authored raster assets. Mechanical detector findings are confined to
  pre-existing, documented provider-brand/type/radius exceptions in shared CSS.
- Full MariaDB-backed Go race suite, vet, embedded asset/binary build and Docker
  `make check` pass. Isolated Saved, Interests, Radar, recommendations, discussions
  and followed-discussion browser regressions pass; no phase-3 redesign is included.
- These are synthetic presentation fixtures: no sender runs and no live mail is
  sent. Real SES/inbox evidence remains an operator verification, not claimed here.
