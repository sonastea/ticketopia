---
version: 1
slug: "views-home-follows-templ"
primary_target: "views/home/follows.templ"
related_targets: ["views/home/follows.go"]
---

# Private artist and venue follows

Mode: Operate. Extend the existing account/collection experience; preserve the
incumbent shell, typography, palette, controls and compact navigation.

## Direction contract

**THESIS:** Search by name and keep ongoing interests in an owner-only collection,
without conflating follows with event interest or promising reminders.

**OWN-WORLD:** Inherit Ticketopia's flat plum/light-stone world, Manrope, native
fields, outline focus and quiet rule-separated lists. No new tokens or imagery.

**STORY:** Choose artists or venues, search, distinguish venues by location,
follow a result, and manage or remove it on this visit or another device.

**FIRST VIEWPORT:** Account navigation, a labeled type/name search with an ordinary
submit, results with named Follow/Unfollow actions, then the private collection.
Venue searches disclose city/country fields. Intrinsic field layout and wrapped
rows preserve the same task on compact screens; Profile links to Follows there.

**FORM:** Local extension of the user-pinned incumbent account and collection
composition, code-led. Inherited world seed `6b2b9e52`; no concept tournament.

**FINISH:** unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Boundaries

GET search/pagination and CSRF-protected POST actions work without JavaScript.
Follows stay private, with no public counts/profile activity or notification jobs.
Collection reads and removals do not require Ticketmaster; retained metadata is
last-known, not an availability claim. Search uses the shared provider budget.

## Finish — 2026-10-08

Ordinary Operate extension preserves incumbent Local frequency, seed `6b2b9e52`;
no design-system changes. The documenter checked source and all six authoritative
desktop/tablet/mobile/320px/enlarged-text/outage captures, preserving `DESIGN.md`
and `.impeccable/design.json`.

Reviewer disposition: **ship**, scoped to the sole missing-location fallback fix
scored resolved after recapture, not a new whole-feature/backend audit. Rendering
regressions cover nil, empty, partial and full venue locations without inventing
artist location guidance. Production-handler/MariaDB browser evidence records
49 checks, six zero-violation axe scans and no runtime errors. The final `make check`
passes Docker generation, production builds, race tests and vet.

No shipping rasters were added; screenshots remain ignored test evidence.
Pre-existing sidecar participation examples and ungated-hover documentation drift
were reported without repair. Enlarged text uses root-font scaling, not browser
zoom. Manual screen-reader, physical-device, live OAuth and comprehensive
cross-browser verification remain separate.
