---
version: 1
slug: "views-home-index-templ"
primary_target: "views/home/index.templ"
related_targets: ["views/home/event.templ","views/layouts/base.templ","views/home/destination.templ"]
---

# Discovery and event views

Mode: Operate. Scope: responsive web UI on the existing read-only discovery backend.
The user confirmed docs/design-guidelines.md, current product truth, and truthful
unavailable states for accounts and community features.

## Direction contract

**THESIS:** Local frequency makes nearby discovery feel personally assembled:
record-sleeve imagery and a mixtape's clearly indexed dates in a practical app.

**OWN-WORLD:** User-approved deep plum actions and selection on light stone
navigation and near-white reading surfaces. Dusty rose belongs to date indexing.
Keep Manrope, real event imagery, outline icons, and ordinary labeled controls.
People plan in daylight and on phones outdoors; the reading surface stays light.

**STORY:** Choose a city, refine discovery across event categories, compare dates and venues,
select an event, inspect its real details, and return to the same results.
Unavailable participation is explicit, never fabricated.

**FIRST VIEWPORT:** A 14rem grouped, labeled sidebar, dominant results column with
location/search and disclosed filters, and a 21rem selected-event context area.
Images and aligned date markers lead rows; search is the primary action.
At 72rem and wider the sidebar can collapse to 4.5rem; its sections and links
within each section are reorderable in an explicit customization mode. The
62rem rail and compact four-item bottom navigation keep their existing order.
Mobile uses a light header and full event views. Selection is pale plum and
updates matching context without displacing the list.

**FORM:** User-pinned plum/light-stone refresh with Notion-inspired grouped
icon-and-text navigation, explicit collapse, and browser-local rearrangement.
Retain the community-radio imagery and date indexing of seed `6b2b9e52`.
Code-led; DESIGN.md owns visuals and the UX guide owns behavior.

**FINISH:** unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Boundaries

Keep GET search and no-JavaScript pagination. Use the shared event detail cache.
No invented engagement, sign-in, account persistence, sorting, or event categories.
Sidebar preferences alone persist locally in this browser; they do not sync
across devices or change the default homepage. Done commits edits, Cancel
restores the previous order, and Reset restores a cancellable default draft.
Keyboard move controls complement drag handles. Profile stays fixed.
Event section links and selected-event URLs survive direct entry and resize.
Provider images remain sourced from normalized Ticketmaster data; no new raster
assets are authored. Account/community destinations explain their current state.

## Finish record — 2026-10-04

The independent read-only finish review returned **ship** for the user-approved
plum/light-stone refresh and customizable sidebar, with no material findings.
Local `nav-*` captures and `nav-checks.json` cover 68 passing browser checks and
11 zero-violation automated accessibility views, including native drag, keyboard
edits, short-height scrolling, preference failures, mobile, and no-JavaScript
search. Production asset build, Go race tests, and vet pass. Detector notices
were advisory only. This is not a comprehensive cross-browser or WCAG claim.
The documenter refreshed `DESIGN.md` and the schema-v2 sidecar; their 18 color
tokens match the source, and component references and tonal ramps validate.
Runtime imagery retains provider URLs; no new authored raster ships.
The user's date-color refinement replaces apricot with dusty-rose `#eadfe5`.
Its scoped independent review returned **ship** after 17 targeted browser checks
and four zero-violation automated accessibility views; date ink, geometry, and
behavior are unchanged. The documenter validated the updated token and sidecar.

The supporting-palette refinement separates pale-plum selection (`#ebdde8`),
neutral stone hover (`#e6e0e5`), and warmer dusty-rose dates (`#e8d2db`). The
selection wash is one RGB step lighter than the proposed value to keep muted
text at 4.54:1; loading actions explicitly use plum text on pale plum. In-thread
visual review and `palette-checks.json` cover 35 passing Chromium checks and
seven zero-violation automated accessibility views, including selected rows,
focus/hover/loading, the intermediate rail, mobile, and 320px. The production
build, race tests, and vet pass. Only pre-existing advisory detector findings
remain; this is not a comprehensive cross-browser or WCAG claim. The token
documentation and sidecar match the source; no new authored raster ships.

## Finish record — 2026-10-03

The independent finish review requested two material fixes: preserve discovery
return state across full-page event sections, and lead event identity with price
and sale status. Its verdict pass scored both resolved and returned **ship** for
those fixes. Final evidence lives in the local `.impeccable/review/` captures and
browser report: 43 checks, six zero-violation automated accessibility views, and
no horizontal overflow. Go race tests and vet pass; the mechanical detector
returned no findings. The documenter generated token-bearing `DESIGN.md` and the
schema-v2 `.impeccable/design.json` from the implemented system. Runtime imagery
retains Ticketmaster source URLs; no local raster assets ship.
