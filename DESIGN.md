---
name: Ticketopia
description: "Local frequency: record-shop imagery, mixtape intimacy, and clear everyday event discovery."
colors:
  ink: "#202539"
  muted: "#5d6274"
  cobalt: "#2946c7"
  cobalt-dark: "#2038a5"
  on-cobalt: "#e2e8ff"
  paper: "#f4f5fa"
  surface: "#ffffff"
  line: "#dfe2ed"
  control-line: "#959caf"
  selection: "#ffe1c5"
  selection-ink: "#653613"
  blue-soft: "#e9edff"
  error: "#a22b39"
  error-bg: "#fff0f1"
  warning: "#795318"
  warning-bg: "#fff3d9"
  success: "#286247"
typography:
  display:
    fontFamily: "Manrope, sans-serif"
    fontSize: "2rem"
    fontWeight: 800
    lineHeight: 1.25
    letterSpacing: "-.035em"
  headline:
    fontFamily: "Manrope, sans-serif"
    fontSize: "1.25rem"
    fontWeight: 750
    lineHeight: 1.25
    letterSpacing: "-.02em"
  title:
    fontFamily: "Manrope, sans-serif"
    fontSize: "1.0625rem"
    fontWeight: 750
    lineHeight: 1.25
    letterSpacing: "-.015em"
  event-title:
    fontFamily: "Manrope, sans-serif"
    fontSize: "1.125rem"
    fontWeight: 750
    lineHeight: 1.4
    letterSpacing: "-.015em"
  body:
    fontFamily: "Manrope, sans-serif"
    fontSize: "1rem"
    fontWeight: 400
    lineHeight: 1.6
    letterSpacing: "normal"
  body-small:
    fontFamily: "Manrope, sans-serif"
    fontSize: ".9375rem"
    fontWeight: 400
    lineHeight: 1.6
    letterSpacing: "normal"
  label:
    fontFamily: "Manrope, sans-serif"
    fontSize: ".875rem"
    fontWeight: 700
    lineHeight: 1.6
    letterSpacing: "normal"
  action:
    fontFamily: "Manrope, sans-serif"
    fontSize: ".875rem"
    fontWeight: 750
    lineHeight: 1.4
    letterSpacing: "normal"
  navigation:
    fontFamily: "Manrope, sans-serif"
    fontSize: ".9375rem"
    fontWeight: 650
    lineHeight: 1.6
    letterSpacing: "normal"
  small-label:
    fontFamily: "Manrope, sans-serif"
    fontSize: ".8125rem"
    fontWeight: 650
    lineHeight: 1.6
    letterSpacing: "normal"
  metadata:
    fontFamily: "Manrope, sans-serif"
    fontSize: ".75rem"
    fontWeight: 400
    lineHeight: 1.6
    letterSpacing: "normal"
  date-number:
    fontFamily: "Manrope, sans-serif"
    fontSize: "1.375rem"
    fontWeight: 800
    lineHeight: 1.2
    letterSpacing: "normal"
rounded:
  radius: ".75rem"
  control: ".5rem"
  navigation: ".6rem"
  image: ".65rem"
  date-marker: ".45rem"
  availability: ".4rem"
spacing:
  space-1: ".25rem"
  space-2: ".5rem"
  space-3: ".75rem"
  space-4: "1rem"
  space-5: "1.5rem"
  space-6: "2rem"
  space-7: "3rem"
components:
  button-primary:
    backgroundColor: "{colors.cobalt}"
    textColor: "{colors.surface}"
    typography: "{typography.action}"
    rounded: "{rounded.control}"
    padding: ".65rem 1rem"
  button-primary-hover:
    backgroundColor: "{colors.cobalt-dark}"
  button-primary-active:
    backgroundColor: "#172b87"
  button-secondary:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.cobalt}"
    typography: "{typography.action}"
    rounded: "{rounded.control}"
    padding: ".65rem 1rem"
  button-secondary-hover:
    backgroundColor: "{colors.blue-soft}"
    textColor: "{colors.cobalt-dark}"
  button-quiet-unavailable:
    backgroundColor: "transparent"
    textColor: "{colors.muted}"
    typography: "{typography.small-label}"
    rounded: "{rounded.control}"
    padding: ".5rem .6rem"
  search-field:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.radius}"
    padding: ".45rem .45rem .45rem .9rem"
  field:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.control}"
    padding: ".6rem .7rem"
  field-disabled:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.muted}"
  nav-link:
    textColor: "{colors.on-cobalt}"
    typography: "{typography.navigation}"
    rounded: "{rounded.navigation}"
    padding: ".75rem 1rem"
  nav-link-hover:
    backgroundColor: "{colors.cobalt-dark}"
    textColor: "{colors.surface}"
  nav-link-current:
    backgroundColor: "{colors.selection}"
    textColor: "{colors.ink}"
  bottom-nav-current:
    backgroundColor: "{colors.blue-soft}"
    textColor: "{colors.cobalt}"
  availability-label:
    backgroundColor: "{colors.selection}"
    textColor: "{colors.selection-ink}"
    typography: "{typography.small-label}"
    rounded: "{rounded.availability}"
    padding: ".35rem .65rem"
  event-row:
    textColor: "{colors.ink}"
    padding: "1.5rem .75rem"
  event-row-selected:
    backgroundColor: "{colors.selection}"
    rounded: "{rounded.radius}"
  event-image:
    backgroundColor: "{colors.blue-soft}"
    rounded: "{rounded.image}"
    width: "100%"
  date-marker:
    backgroundColor: "{colors.selection}"
    textColor: "{colors.ink}"
    rounded: "{rounded.date-marker}"
    padding: ".4rem .65rem"
  context-card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.ink}"
    rounded: "{rounded.radius}"
    padding: "1.25rem"
  section-link:
    textColor: "{colors.ink}"
    typography: "{typography.small-label}"
    padding: ".5rem .1rem"
  section-link-current:
    textColor: "{colors.cobalt}"
---

# Design System: Ticketopia

## Overview

**Creative North Star: "Local frequency"**

Local frequency makes event discovery feel personally assembled: the presence of a record shop, the intimacy of a mixtape, and the clarity of an everyday planning tool. Cobalt navigation frames cool light reading surfaces; apricot selection and compact date indexes give the collection a warm, human rhythm.

Bold, readable Manrope and real provider photography carry the personality. Familiar labeled controls keep this an Operate-mode product: useful dates, venues, prices, and clear availability matter more than promotional decoration. Density changes with the available space while the event identity stays recognizable.

**Key Characteristics:**
- Cobalt navigation and actions against cool paper and white surfaces.
- Apricot selection, date indexing, and honest availability labels.
- One expressive, readable Manrope family with substantial heading weights.
- Record-sleeve provider imagery and compact, aligned event facts.
- Border-only depth, restrained state transitions, and visible keyboard focus.

**Implementation authority:** [`views/styles/app.css`](views/styles/app.css) is the unminified token and style source; [`views/assets/app.css`](views/assets/app.css) is its compiled output. Frontmatter records the shipped values, retaining CSS custom-property names where present. `tailwind.config.js` supplies scan paths, not another theme. [`PRODUCT.md`](PRODUCT.md) owns product truth; [`docs/design-guidelines.md`](docs/design-guidelines.md) owns UX; the [surface contract](.impeccable/surfaces/views-home-index-templ.md) retains route strategy and the selected direction's seed. The sidecar's synthesized tonal strips are panel previews, not additional shipping colors.

## Colors

The palette combines confident cobalt, cool near-white reading space, and a small warm apricot vocabulary; dark ink and clearly differentiated borders keep it practical.

### Primary
- **Cobalt** (`cobalt`): navigation backgrounds, primary actions, actionable text, caret, and focus outline on light surfaces.
- **Deep cobalt** (`cobalt-dark`): action/navigation hover and readable event-date text.
- **Pale cobalt ink** (`on-cobalt`): secondary navigation text on cobalt.
- **Soft blue** (`blue-soft`): image fallbacks, empty-preview artwork, secondary-action hover, and the current bottom-nav destination.

### Secondary
- **Apricot** (`selection`): selected event rows, current sidebar/rail destinations, date markers, availability labels, the brand's stop, and browser text selection.
- **Apricot ink** (`selection-ink`): explanatory availability text on apricot. Dates and selected navigation use ordinary ink instead.

### Neutral
- **Ink** (`ink`): headings, body content, and selected navigation text.
- **Muted ink** (`muted`): venues, explanatory copy, source freshness, and unavailable controls.
- **Cool paper** (`paper`): page background and disabled selects.
- **White surface** (`surface`): search/field surfaces, event context, and ticket containers; also light text on filled cobalt actions.
- **Quiet line** (`line`): result separators and container boundaries.
- **Control line** (`control-line`): stronger field and secondary-action boundaries.

### Status
- **Error / error wash** (`error`, `error-bg`): failed reads and canceled-event status.
- **Warning / warning wash** (`warning`, `warning-bg`): stale-data notices; warning text also marks postponed/rescheduled events.
- **Success** (`success`): the provider's “On sale” status, always accompanied by its text.

**The Cobalt and Apricot Rule.** Cobalt carries navigation and actions; apricot marks desktop selection, date indexes, and availability, while compact bottom navigation uses soft blue for its current destination.

## Typography

**Display and body font:** Manrope, with a sans-serif fallback. The variable font is self-hosted at `views/assets/fonts/manrope.ttf`, supports the shipped weights (400–800), uses `font-display: swap`, and is distributed with its SIL OFL license. There is no separate mono or display family.

**Character:** rounded, direct, and substantial. Tight heading tracking gives short titles confidence; ordinary casing and generous body leading keep long event information readable. The hierarchy is role-based, not a manufactured modular scale.

### Hierarchy
- **Display:** `typography.display` for page headings. Discovery headings reduce to 1.75rem below the context threshold. Event-page headings use 1.75rem, reducing to 1.5rem on the smallest phones.
- **Headline:** `typography.headline` for section and preview headings. Event-content section headings use 1.125rem; destination-state headings use 1.5rem.
- **Title:** `typography.title` for supporting headings; `typography.event-title` for result titles, with the more open leading needed by long names.
- **Body:** `typography.body` for the base reading size; `typography.body-small` for event descriptions. Observed reading measures are 65ch for introductions, 70ch for event prose, and 75ch for source notes.
- **Label / action:** `typography.label` for field labels and `typography.action` for primary/secondary controls. Inputs remain at the body size with weight 500.
- **Navigation:** `typography.navigation` for full navigation; current destinations use weight 800. Rail/bottom-nav labels use .6875rem; event-section links use `typography.small-label`, also rising to weight 800 when current.
- **Metadata:** `typography.metadata` for supporting facts and freshness; `typography.small-label` for dates, quiet controls, and availability. Date numbers use `typography.date-number`; month labels use weight 750. Date markers and event-date lines use tabular numerals.

Headings balance lines, and headings/paragraphs can wrap anywhere to handle long provider names. Smaller metadata supports the event title and essential facts rather than replacing them.

**The One Family, Clear Roles Rule.** Use Manrope's size, weight, leading, and spacing to distinguish headings, facts, and controls; keep dates tabular and long event names able to wrap.

## Layout

The centered application shell stops at 100rem and fills at least the dynamic viewport height. Main content uses normal document scrolling. Reused spacing primitives are the source's `space-1` through `space-7`; component-specific insets remain explicit rather than becoming another spacing scale.

| Threshold | Shipped behavior |
| --- | --- |
| Base, below 40rem | Single-column image-led rows; content padding 1.5rem 1.25rem. Compact cobalt header and four-item fixed bottom navigation. Workspace reserves `calc(5.5rem + env(safe-area-inset-bottom))`. |
| At most 23rem | Content padding becomes 1.25rem 1rem; fields form one column. Search loses its decorative icon and tightens its inset; event headings and section links reduce to fit. |
| At least 40rem | General page padding becomes 2rem. Result rows use an 8rem image column and 1.25rem gap; images become square and date markers become horizontal strips below them. |
| At least 62rem | A 5.5rem cobalt navigation rail replaces the bottom bar. Discovery gains a 20rem context column with 1.5rem gutter/padding; result images use a 6.5rem column and 1rem gap. Dedicated event pages gain an 18rem ticket column with a 2rem gap. |
| At least 72rem | A 13.5rem labeled sidebar replaces the rail/header. Discovery context becomes 21rem with 2rem gutters/padding; event/destination page padding is 2rem 2.5rem, with destination top padding increased to 3rem. |
| At least 86rem | Result image columns return to 8rem with 1.25rem gaps. The filter grid becomes three columns, with the city field spanning two. |

Below 62rem, ordinary result selection follows the dedicated event link. An existing selected-preview URL or a resized desktop selection displays context in place of the results, preserving identity. The CSS and JavaScript share the same 62rem threshold.

The context panel is sticky from 62rem (top 1.5rem), with `max-height: calc(100dvh - 3rem)`, vertical overflow, and a stable scrollbar gutter. The full sidebar is sticky and one dynamic viewport tall. Ticket panels remain in the page layout. Filters use two equal columns and a 1rem gap until the small-phone or wide-screen adjustments apply.

## Elevation & Depth

The shipped system has no card or control shadows. Depth comes from white against cool paper, one-pixel quiet borders, stronger control borders, and the apricot selection fill. The selected row stays in the list's geometry. Fixed navigation and sticky context are functional layers, not floating or glass surfaces.

Focus is an outline, not a shadow: a solid cobalt ring (3px) offset from the target (4px). Navigation and brand links on cobalt use apricot focus; bottom navigation on white uses cobalt.

**The Border-Only Depth Rule.** Separate surfaces with tone, spacing, and borders; selection changes the fill without lifting the component or adding a cast shadow.

## Shapes

Use modest, distinct corner treatments: the shared `radius` for context/ticket containers, selected rows, and the search surround; `control` for buttons and fields; `navigation` for navigation; `image` for photographs; and the smaller `date-marker` and `availability` corners for indexing and state. These are rounded rectangles, not a universal pill vocabulary. Unselected result rows are separated by a top rule rather than enclosed in individual cards.

Event imagery fills a clipped frame with `object-fit: cover`. The default frame is 16:9; list images become square at the row-image threshold. Full-event images cap their height at 22rem, and a compact selected preview caps it at 16rem. Preserve provider color and content; the reviewed monochrome photographs are source material, not a prescribed grayscale filter.

Icons are inline outline SVGs with a 24-unit viewBox, rounded caps/joins, and stroke width 1.7. The usual displayed size is 1.25rem; quiet controls use 1rem. The music-icon fallback occupies the same image frame, so a failed image does not erase the event's place in the list.

## Components

### Buttons

Compact, confident, plainly labeled controls. Primary actions pair cobalt with white text; secondary actions use white, cobalt text, and a control-line border. Both use the frontmatter's action typography and padding with a minimum height of 2.75rem. Hover darkens primary actions and gives secondary actions a soft-blue fill. Primary pressed fill is recorded as the source's component-local literal.

Quiet participation controls use a transparent fill, quiet border, small-label typography, and the same minimum target height. Shipped Save, Interested, and Recommend controls are disabled, use muted text, and have nearby explanatory copy. Their appearance does not establish working participation. Underlined text links offer navigation and small utility actions; the preview-close icon has a 2.75rem target.

Action background changes and disclosure-chevron rotation use 160ms; result selection uses 180ms. All use the source's `cubic-bezier(.16, 1, .3, 1)`. Reduced-motion mode sets transition and animation durations to zero and scroll behavior to auto. No loading shimmer is part of the system.

### Inputs / Fields

White, clearly bounded, ordinary fields. Search combines an SVG icon, labeled search input, and submit action inside one control-line surround. The text input can shrink without displacing the action. Labeled filters use a minimum height of 2.875rem, body-sized text, and visible control borders; disabled genre selects use paper and muted ink. Native `details`/`summary` discloses filters and extra event metadata. The shared focus outline remains visible; field errors use explanatory text and the error-state container rather than an invented field-specific color system.

### Navigation

Cobalt sidebar/rail links use pale text, deep-cobalt hover, and apricot current state with ink and stronger weight. Compact bottom navigation has a white surface, quiet top border, icon-over-label layout, and soft-blue current state. Event sections are real links with a quiet baseline; the current section adds a cobalt two-pixel bottom border and heavier text. A current link is also marked with `aria-current`.

### Availability Labels

Small apricot rectangles with apricot ink explain “In development” or a specific unavailable capability. They are informational labels, not selectable filter chips. Do not turn them into a success confirmation.

### Indexed Event Rows

The signature pairs provider photography with compact date indexing. Rows lead with local show date, event title, and venue, followed by sale status and known price or an explicit unknown. Their selected state is an apricot fill; the title and Details remain real event links. The date marker overlays the lower-left image corner on compact rows, then becomes a horizontal index beneath square artwork. Its short date is decorative beside the full readable date; unknown dates say TBA.

Photos are live external Ticketmaster assets retained in normalized `Event.Images`; presentation chooses an available width close to the requested size. No authored shipping raster belongs to this system. Local styles, JavaScript, htmx, and Manrope are Go-embedded assets; SVG supplies the icons. The sidecar demonstrates the shipped image fallback rather than copying provider photography into documentation assets.

### Cards / Containers

Event context and ticket information use white, quietly bordered containers with the shared radius. Context content has 1.25rem padding, reduced to 1rem for the selected compact view at the small-phone threshold; ticket panels use 1.5rem. Empty context uses a soft-blue music placeholder and practical guidance. Loading context uses static blocks, `aria-busy`, and a status announcement. Errors keep retry/navigation available; stale data explains its source state.

Keep event price and sale status ahead of unavailable participation. Desktop selection preserves the list and changes the matching context; close and return interactions restore a useful focus target. Search and pagination retain ordinary links/forms without JavaScript. Discussion/community sections and account destinations currently explain planned availability; they do not render invented engagement.

## Do's and Don'ts

### Do:
- Do use cobalt for navigation and actions, cool light surfaces for reading, and apricot for the shipped selection and indexing roles.
- Do keep real provider imagery, full readable dates, venue, price availability, and sale status connected to the event title.
- Do preserve the 62rem context/rail threshold and 72rem full-sidebar threshold when extending the shared shell.
- Do keep visible focus, wrapping titles, labeled controls, reduced motion, and ordinary link/form behavior.
- Do explain unavailable participation, unknown event facts, and stale provider data in text.

### Don't:
- Don't add cast shadows, glass effects, or raised hover cards to this flat, border-defined system.
- Don't force provider photographs into monochrome or substitute decorative stock imagery for event identity.
- Don't style planned accounts, saves, interest, recommendations, or discussions as completed actions or fabricate participation counts.
- Don't use promotional clutter or manufactured urgency to compete with practical event information.
- Don't promote route-specific composition or synthesized sidecar tonal ramps into new global design tokens.
