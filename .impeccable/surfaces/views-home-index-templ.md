---
version: 1
slug: "views-home-index-templ"
primary_target: "views/home/index.templ"
related_targets: ["views/home/event.templ","views/layouts/base.templ","views/home/destination.templ"]
---

# Discovery and event views

Mode: Operate. Scope: responsive web UI on the existing read-only discovery backend,
with opt-in accounts, private saves, and independent durable Interested choices.
The user confirmed public opt-in participant lists/profile event activity and
preservation of the incumbent UI. Remaining unavailable capabilities stay explicit.

## Direction contract

**THESIS:** Local frequency makes nearby discovery feel personally assembled:
record-sleeve imagery and a mixtape's clearly indexed dates in a practical app.

**OWN-WORLD:** User-approved deep plum actions and selection on light stone
navigation and near-white reading surfaces. Dusty rose belongs to date indexing.
Keep Manrope, real event imagery, outline icons, and ordinary labeled controls.
People plan in daylight and on phones outdoors; the reading surface stays light.

**STORY:** Choose a city, refine discovery across event categories, compare dates and venues,
select an event, inspect its real details, and return to the same results.
Save privately or mark Interested independently; public identity is opt-in.
Unavailable participation is explicit, never fabricated.

**FIRST VIEWPORT:** A 14rem grouped, labeled sidebar, dominant results column with
location/search and disclosed filters, and a 21rem selected-event context area.
Images and aligned date markers lead rows; search is the primary action.
At 72rem and wider the sidebar can collapse to 4.5rem; its sections and links
within each section are reorderable in an explicit customization mode. The
62rem rail and compact four-item bottom navigation keep their existing order.
The contextual extension selects questions first, with compact event identity,
native event details/actions, and the selected root/replies in the same panel.
Expansion opens a dedicated event or shareable thread. Mobile uses a light header
and a single primary event/thread view with bottom navigation. Selection is pale plum and
updates matching context without displacing the list.

**FORM:** User-pinned plum/light-stone refresh with Notion-inspired grouped
icon-and-text navigation, explicit collapse, and browser-local rearrangement.
Retain the community-radio imagery and date indexing of seed `6b2b9e52`.
Code-led; DESIGN.md owns visuals and the UX guide owns behavior.

**FINISH:** unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance

## Boundaries

Keep GET search and no-JavaScript pagination. Use the shared event detail cache.
Provider discovery remains read-only; opt-in accounts persist saves and event
interest separately. Public identities appear only for choices marked Public;
private choices contribute to public counts without naming anyone. No invented
engagement, sorting, or event categories. Sidebar preferences persist locally in
this browser; they do not sync
across devices or change the default homepage. Done commits edits, Cancel
restores the previous order, and Reset restores a cancellable default draft.
Keyboard move controls complement drag handles. Profile stays fixed.
Event section links and selected-event URLs survive direct entry and resize.
Provider images remain sourced from normalized Ticketmaster data; no new raster
assets are authored. Account/community destinations explain their current state.

## Responsive event/discussion extension / finish record — 2026-10-07

- Completed the connected discovery-to-participation milestone without replacing
  the incumbent visual system. Event titles select Discussion; Details selects
  Overview. Questions and selected roots/replies share the context panel from
  62rem; compact screens use a single primary view. Native event details/actions
  and explicit dedicated-page expansion keep event identity close to discussion.
- Filters, loaded results, selected event/thread/target, separate reply pagination,
  scoped drafts, and return/focus continuity survive resize, Back, and reload.
  Drafts remain browser/tab-local, not cross-device synchronization. Thread reads
  reuse permission-filtered retained context without requiring the provider.
- [responsive-checks.json](../review/responsive-checks.json) records 134 passing
  Chromium checks and five zero-violation axe scans across desktop, tablet, mobile,
  320px, and 200% text, including keyboard/touch journeys and failure recovery.
  Saved/Interested/Recommendation/Discussion regressions pass 66/44/49/24 checks;
  the real-MariaDB full race suite, vet, production build, and localized
  post-generation race checks also pass.
- The independent reviewer scored compact selection-failure focus and guest Reply
  focus resolved: **ship for those two fixes only**. This is not whole-application
  approval or public-pilot certification. Manual screen-reader, physical-device,
  live OAuth, and cross-browser checks remain unverified.
- The documenter updated descriptive Event Discussions prose in `DESIGN.md` only.
  Frontmatter tokens and `.impeccable/design.json` remain unchanged; pre-existing
  availability-copy drift is untouched. No new authored raster assets ship.

## Interested extension / finish record — 2026-10-06

- Subsequent user-requested refinement removes redundant state paragraphs and
  disclosure labels. Keep Private/Public on the action only, with an emphasized
  count and compact **Change visibility** beside it. One native editor opens at a
  time and closes after save; pending/success use assistive announcements, while
  one dismissible error is replaced/cleared rather than accumulating. No new
  palette, typography tokens, assets, or layout system; Save remains independent.
- The refinement's in-thread inspection covers compact/open layouts, an actionable
  error, and desktop/tablet/mobile/200% text. The updated browser report records
  84 passing checks and ten zero-violation axe captures; targeted view/API race
  tests, vet, and production build pass. Fixed the desktop open-editor wrap so the
  form sits below, not alongside, the count. Prior independent verdicts below keep
  their original scope; no new whole-application or certification claim is made.
- Ordinary extension, not a redesign: independent Interested, visible Private/Public
  action disclosure, separate `aria-pressed`/count, native per-event visibility,
  and public opt-in participants/profile activity. Existing Save stays independent.
- Reviewer `ses_eeb5cd683ffeGH4yR1PRlcHuHM` returned **ship for the two scored fixes
  only**: accessible names include visible action labels, and participant pagination
  plus Back to event preserves the originating collection cursor. Earlier verdict
  scopes below are unchanged; this is not whole-application approval or certification.
- [interest-checks.json](../review/interest-checks.json) records 54 checks, seven
  zero-violation axe captures (desktop, tablet, 390px, 320px, 200% text, own collection),
  and no browser runtime errors. These use isolated SQL/provider fixtures, not
  production or live Google consent. The implementation handoff reports the full
  real-MariaDB Go race suite, vet/build, and OpenAPI lint passing, with two existing
  health-probe warnings; this documentation pass did not rerun them.
- Templates, `views/styles/app.css`, and recorded desktop/320px/own-collection
  captures retain Manrope, plum quiet controls, 44px targets, existing corners/focus,
  flat borders, and image/date-indexed rows. `DESIGN.md` changes availability prose
  only; all frontmatter values and aesthetics remain intact. No new tokens, fonts,
  palette, shape/spacing system, or raster assets; sidecar drift stays untouched.

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
