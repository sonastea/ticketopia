---
version: 1
slug: "views-home-recommendations-templ"
primary_target: "views/home/recommendations.templ"
related_targets: ["views/home/event.templ", "views/account/settings.templ"]
---

# Public recommendations

Mode: Operate. The user confirmed publishing an optional 500-character reason,
edit/withdraw, public event/profile attribution, and city/category browsing.
Extend the incumbent system; no new identity, global tokens, or raster assets.

## Direction contract

**THESIS:** Event endorsements explain who recommends an event and why, without
turning private saves or interest into public activity or a generic social feed.

**OWN-WORLD:** Inherit Manrope, plum quiet actions, flat borders, light-stone
navigation, provider imagery, and dusty-rose date indexes from DESIGN.md.

**STORY:** Open an event, disclose the public audience before publishing, add an
optional reason, and return later to edit or withdraw. Browse real endorsements.

**FIRST VIEWPORT:** Community leads with a heading, ordinary city/country/category
filters, then image/date-indexed event recommendations and named authors. Event
Community leads with a native disclosure composer, followed by public reasons;
private-by-default interest identities remain a separate section. Mobile stacks.

**FORM:** User-confirmed narrow extension of the existing Operate surface,
code-led, inheriting seed `6b2b9e52`. Drafts survive failed writes; publication
requires explicit submission. Edits never bump publication ordering.

**FINISH:** unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Grouped discovery extension — 2026-10-07

User confirmed permissive participation: one Community entry per event, immutable
first-recommendation ordering, and observe-only publication/reactivation signals.
No quotas, sanctions, trust roles, IP grouping, or new visual identity in this slice.
Inherit the direction contract above. Community entries carry one image/date/title,
an explicit active endorsement count, up to three named reasons, and a full-list
link. Filters and ordinary pagination/return context remain unchanged.

## Earlier scoped finish — 2026-10-07

The independent review's four material findings were fixed in one batch and
confirmed resolved by the verdict continuation: Recommend directly opens the
composer; native auth/CSRF failures retain escaped drafts and safe recovery
context without weakening checks; preview availability copy is current; and
PRODUCT.md/DESIGN.md describe implemented recommendations. Disposition: ship
only at that fix-list scope, not a broader accessibility or public-pilot approval.

At that earlier review, `../review/recommendation-checks.json` recorded 62 browser
checks, eight zero-violation axe captures with same-name screenshots, and no runtime
errors; that report has since been superseded by the grouped-extension evidence below.
Documentation compared the finished templates, CSS, and enhancement
scripts with DESIGN.md and its sidecar; the composer and shared collection
guidance extend the incumbent system without new tokens or identity.

No authored shipping raster was introduced: event images retain Ticketmaster
provenance and the existing SVG fallback; review screenshots are evidence, not
shipping assets. Pre-existing ticket CTA clipping at 200% text and stale sidecar
availability examples/narrative remain outside scope, unrepaired and not
canonized. This documentation pass consumes the completed review evidence;
it does not claim another live browser run. Moderation still gates a public pilot.

## Grouped discovery scoped finish — 2026-10-07

The fresh independent finish review returned **ship**, with no material fixes,
for the grouped Community extension only—not whole-app, whole-Good-CSS, or
public-pilot approval.
Scope remains one entry per event, explicit active count, up to three named
recommendations and a full-list link, immutable first-recommendation position
even after everyone withdraws, and observe-only ten-minute publication/reactivation
counters. No quotas, cooldowns, trust roles, IP grouping, or sanctions were added.

Documentation compared `views/home/recommendations.templ`, the grouping styles in
`views/styles/app.css`, and `views/home/presentation.go` against PRODUCT.md,
DESIGN.md, and `.impeccable/design.json`; the initial documentation pass inspected
all 11 supplied captures.
The extension retains Manrope's event-title/body/metadata roles, plum links,
flat rule-separated image/date-indexed rows, the 70ch wrapping reason measure,
and incumbent responsive stacking. Named reasons retain the existing 1rem rhythm
with logical block/inline margins, explicit list-item block-margin resets, and
sibling `margin-block-start`. Tabular numerals apply only to Community count/date
metadata, not author/reason prose; that metadata remains secondary to event facts.
Full-list and event links retain filtered return context. No new global tokens
or reusable system change requires rewriting DESIGN.md or its sidecar.

The current `../review/recommendation-checks.json` records 82 passing Chromium
outcomes, 11 recaptured screenshots with zero-violation axe scans, and no runtime
errors. The 44px assertion now covers Community event/title, profile, and full-list
text-link actions alongside composer controls. Earlier supplied validation passed
the real-MariaDB Go race suite, vet, production
npm build, and OpenAPI lint (two pre-existing health warnings); the supplied
detector pass found no new grouping findings. After the final logical-spacing and
tabular-metadata delta, npm build passed and the fresh reviewer continuation
confirmed the scoped **ship** with no material fixes. This documentation recheck
consumed the updated report and supplied verdict without rerunning browsers,
detectors, builds, or tests.

Limits: SVG fallback fixtures do not exercise provider photographs; the
three-author cap is backed by backend tests, not screenshots. Event imagery remains
provider URLs or the incumbent SVG fallback; no authored shipping raster was
introduced, and screenshots are evidence only. Existing sidecar availability-copy
drift, enlarged-text ticket CTA clipping, and unrelated detector notices remain
unrepaired and not canonized. Moderation still gates a public pilot.
