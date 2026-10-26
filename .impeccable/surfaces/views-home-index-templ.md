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
The 2026-10-10 refinement makes that identity quieter: event-first hierarchy,
transparent participation actions, segmented event sections, and one preview
boundary. The 2026-10-11 list redesign makes title/artwork open Overview and exposes
Tickets with private Save; activity becomes compact count links. Zero
interest counts stay hidden until positive. Back to results replaces the close
icon. See DESIGN.md's “Local frequency, less interface” philosophy.

**OWN-WORLD:** User-approved deep plum actions and selection on light stone
navigation and near-white reading surfaces. Dusty rose belongs to date indexing.
Keep Manrope, real event imagery, outline icons, and ordinary labeled controls.
People plan in daylight and on phones outdoors; the reading surface stays light.

**STORY:** Choose a city, refine discovery across event categories, compare dates and venues,
select an event, inspect its real details, and return to the same results.
Save privately or mark Interested independently; public identity is opt-in.
Unavailable participation is explicit, never fabricated.

**FIRST VIEWPORT:** A 14rem grouped, labeled sidebar, dominant results column with
location/search and disclosed filters, and a 21rem default event context area.
Discovery uses available space up to a 120rem shell. With selection and at least
64rem of actual workspace, results/context divide 2:3 with a 24rem results minimum;
collapsing navigation can free this space too.
Images and aligned date markers lead rows; search is the primary action.
At 72rem and wider the sidebar can collapse to 4.5rem; its sections and links
within each section are reorderable in an explicit customization mode. The
62rem rail and compact four-item bottom navigation keep their existing order.
The contextual extension selects Overview from titles/artwork. Tickets and Save
precede artwork and extended content. Overview description and practical notices
are visible without disclosure; Interested, Recommend, and reminders form a
secondary group below content. Panels and pages share one section navigation:
Overview, Questions & tips, Recommendations. Selected threads retain community
beside or below them using intrinsic 18rem columns. Source/history is secondary;
stale warnings remain beside essential facts. Standalone identity pairs facts/actions
with imagery in an intrinsic 20rem-minimum grid, stacking facts first when compact.
Open event page opens a dedicated event on desktop; compact context is the complete
event view and omits that expansion during successful reads. Failures retain it
for recovery. Conversations keep their shareable expansion.
Mobile uses a light header
and a single primary event/thread view with bottom navigation. Selection is pale plum and
updates matching context without displacing the list.

Discover rows extend vertically with supplied performer/team and sale facts,
short descriptions, and compact real public question/recommendation count links.
The extra content spans the row width beneath image/identity; activity wraps
intrinsically after Tickets and Save. Count links select Discussion or Community
without separate navigation when enhanced. Events without activity add no empty
social block. Default context widths may shrink to 45% of the content region when
enlarged text consumes the workspace; phone-width result containers keep a small
thumbnail beside the facts, and narrower (enlarged-text) ones stack image/facts.
At 72rem+, selected context fills the usable viewport height even when activity
is sparse, and scrolls internally for longer content. Page-batched public reads
exclude withheld/withdrawn content and private identities, with local failure
feedback; the JSON discovery contract remains unchanged.

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

## Richer discovery rows / verification — 2026-10-10

Superseded for list entry points by the 2026-10-11 redesign slice: title/artwork
now open Overview, Tickets is direct, and public activity is count links only.
Chromium fixture checks passed at 320, 390, 768, 1024, and 1440px, including return
focus and explicit Discussion navigation, plus 200% text, forced-colors focus,
reduced-motion mode, and no-JavaScript event navigation. Mobile/desktop captures
were inspected. Live OAuth/writes, physical devices, and other browsers were not
retested. The mechanical detector reports only existing token/provider-font notes.
The subsequent overview slice removes the information disclosure and shares
section navigation between panel/page. Chromium fixture checks at the same five
widths cover exposed practical content, separate participation, section changes,
draft retention, browser Back, result/focus return, missing ticket URLs, canceled/
stale facts, and failed-save recovery. 200% text, forced colors, reduced-motion
mode, and native section links pass; mobile/desktop captures were reviewed, with
one correction to intrinsic section-label wrapping. No browser runtime errors.
Production build, targeted view/API race tests, and Docker `make check` (full race
suite and vet) pass. At this intermediate slice, live account writes,
physical devices, other browsers, and full responsive acceptance remained open;
the completion record below supersedes that scope.

### Redesign completion — 2026-10-11

- All redesign acceptance behaviors are implemented. Collection returns include
  Saved, Interests, and Radar cursor/scroll/focus across event sections. Interests
  names its independent removal action. Reminder settings track confirmed saved
  state without implicit enrollment. Compact success has no competing expansion.
- Production-template matrices pass in Chromium (164 checks, eight clean axe
  scans) and WebKit (156 checks), with desktop/mobile captures inspected. Evidence:
  `../review/redesign/event-discovery-checks.json` and
  `../review/redesign-webkit/event-discovery-checks.json`.
- Real isolated-MariaDB suites pass: Saved 88, Interests 87, recommendations 83,
  responsive discussion 145, Radar 43, reminders 28 checks, with clean automated
  accessibility captures. They exercise actual private/public writes, explicit
  reminder consent, native search/pagination, return position, drafts/reply targets,
  browser history, touch/resize, blocked storage, and read/write failures.
- Production build, full race suite against local MariaDB, host vet, and Docker
  `make check` pass. Only existing detector token/provider-font notes remain.
  This is browser/device emulation and synthetic-account evidence; physical-device,
  manual screen-reader, live Google consent, and external usability-study claims
  are not made. No new palette, fonts, or authored raster assets ship.

- In-thread layout review confirms full-width detail/activity excerpts, conditional
  public previews, steady desktop context height, native links, and preserved
  dedicated-page navigation. No new tokens, authored imagery, or JSON API fields.
- [event-context-space-checks.json](../review/event-context-space-checks.json)
  records 125 passing Chromium checks across desktop/short desktop/ultrawide,
  tablet, phones, and 200% root text, including row-to-thread/community selection,
  focus return, long text, forced colors/reduced motion, and no-JavaScript rendering.
  Fixtures are synthetic and read-only; live OAuth/mutation journeys were not rerun.
- Full Go race tests with isolated local-MariaDB schemas, production asset build,
  Docker `make check` (race/vet/build), and the scoped layout detector pass.
  Enlarged result-copy width improves; inherited sidebar crowding remains outside
  this refinement. This record is not new independent approval or a WCAG claim.

## In-place event sections — 2026-10-11

- Part 1 of the [app polish spec](../../docs/design/app-polish-and-event-section-navigation.md)
  keeps event identity/actions, real section navigation, participation, and native
  provenance disclosure mounted. Context Overview artwork moves with the section;
  standalone identity artwork stays in place. No visual-world/token changes.
- Pending reads label retained content; completion uses an assistive announcement.
  Success commits one history entry, Back restores reading position, and local
  Retry/native links recover failures without mislabeling old content. Fresh facts,
  warnings, permissions and CSRF reconcile; private section HTML is never stored.
- [event-discovery-checks.json](../review/event-sections/event-discovery-checks.json)
  records 210 Chromium checks and eight clean axe scans across the existing
  desktop/rail/tablet/390px/320px/short/enlarged-text matrix. Stable DOM/document
  counts, failed clicks/Back, rapid requests, changed sessions, native links,
  forced-colors focus and reduced motion pass. Desktop/mobile captures were inspected.
- [Final validation](../review/event-sections/final-validation.json) extends that
  matrix to 218 checks with mismatched-fragment and reading-position regressions;
  all eight axe scans remain clean. No extra visual-polish loop was needed.
- Final responsive discussions pass 137 isolated checks, including late-write
  rejection and keyboard conversation entry without completion reclaiming focus.
  Final Docker `make check` passes generation, embedded builds, race tests and vet.
- Isolated MariaDB browser suites pass for Saved, Interests, Radar, recommendations,
  reminders, responsive discussions (including guest Reply and blocked-storage
  discussion/recommendation drafts), and followed discussions. Asset/binary build,
  full local-MariaDB race suite and host vet pass. Detector findings are unchanged
  token/provider-font advisories. Browser emulation is not physical-device,
  manual screen-reader, live OAuth, or independent usability-study evidence.

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
