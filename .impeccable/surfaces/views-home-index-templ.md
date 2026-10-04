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

**OWN-WORLD:** Cobalt navigation, cool white reading surfaces, apricot selection,
ink text, Manrope sans, substantial event imagery, compact date markers, and
ordinary labeled controls. People plan in daylight and on phones outdoors;
the main reading surface is light.

**STORY:** Choose a city, refine discovery across event categories, compare dates and venues,
select an event, inspect its real details, and return to the same results.
Unavailable participation is explicit, never fabricated.

**FIRST VIEWPORT:** A 13.5rem labeled left rail, dominant results column with
location/search and disclosed filters, and a 21rem selected-event context area.
Images and aligned date markers lead rows; search is the primary action.
Mobile uses a compact header, single-column image-led events, full event views,
and four-item bottom navigation. Selection highlights a row in apricot and
updates its matching context without displacing the list.

**FORM:** Community-radio identity, seed `6b2b9e52`, with user-pinned record-shop
imagery and mixtape intimacy. Code-led. The current implementation uses themed
shadcn-templ primitives; DESIGN.md owns visual rules and the UX guide owns behavior.

**FINISH:** unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Boundaries

Keep GET search and no-JavaScript pagination. Use the shared event detail cache.
No invented engagement, sign-in, persistence, sorting choices, or event categories.
Event section links and selected-event URLs survive direct entry and resize.
Provider images remain sourced from normalized Ticketmaster data; no new raster
assets are authored. Account/community destinations explain their current state.

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
