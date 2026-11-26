---
name: Ticketopia
description: "Local frequency: record-shop imagery, mixtape intimacy, and clear everyday event discovery."
colors:
  ink: "#29252d"
  muted: "#69616d"
  muted-warm: "color-mix(in oklch, var(--muted), var(--brand) 12%)"
  brand: "#6b3d5f"
  brand-dark: "color-mix(in oklch, var(--brand), black 18%)"
  brand-active: "color-mix(in oklch, var(--brand), black 28%)"
  brand-soft: "#ebdde8"
  nav-surface: "#f2f0f3"
  nav-hover: "color-mix(in oklch, var(--nav-surface), var(--ink) 6.5%)"
  paper: "#faf9fb"
  surface: "#ffffff"
  line: "#dfdae2"
  control-line: "#938b99"
  date-surface: "#e8d2db"
  brand-tint: "oklch(0.66 0.080387 337.297)"
  event-tint-surface: "color-mix(in oklch, var(--event-tint) 18%, var(--surface))"
  error: "#a22b39"
  error-bg: "color-mix(in oklch, var(--error) 6%, var(--surface))"
  warning: "#795318"
  warning-bg: "color-mix(in oklch, var(--warning) 6%, var(--surface))"
  success: "#286247"
typography:
  display:
    fontFamily: "Bricolage Grotesque, Manrope, sans-serif"
    fontSize: "clamp(1.75rem, 1.5rem + .625vw, 2rem)"
    fontWeight: 800
    lineHeight: 1.25
    letterSpacing: "-.02em"
  empty-display:
    fontFamily: "Manrope, sans-serif"
    fontSize: "clamp(1.375rem, 1.2rem + .75vw, 1.75rem)"
    fontWeight: 800
    lineHeight: 1.1
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
  discover-lead:
    fontFamily: "Bricolage Grotesque, Manrope, sans-serif"
    fontSize: "clamp(1.25rem, 1.0625rem + .65cqi, 1.625rem)"
    fontWeight: 750
    lineHeight: 1.2
    letterSpacing: "-.02em"
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
    fontWeight: 550
    lineHeight: "calc(1.25 / .875)"
    letterSpacing: "normal"
  navigation-current:
    fontFamily: "Manrope, sans-serif"
    fontSize: ".9375rem"
    fontWeight: 700
    lineHeight: "calc(1.25 / .875)"
    letterSpacing: "normal"
  navigation-group:
    fontFamily: "Manrope, sans-serif"
    fontSize: ".75rem"
    fontWeight: 700
    lineHeight: 1.25
    letterSpacing: ".02em"
  small-label:
    fontFamily: "Manrope, sans-serif"
    fontSize: ".8125rem"
    fontWeight: 650
    lineHeight: 1.6
    letterSpacing: "normal"
  quiet-action:
    fontFamily: "Manrope, sans-serif"
    fontSize: ".8125rem"
    fontWeight: 650
    lineHeight: "calc(1.25 / .875)"
    letterSpacing: "normal"
  availability:
    fontFamily: "Manrope, sans-serif"
    fontSize: ".8125rem"
    fontWeight: 650
    lineHeight: "calc(1 / .75)"
    letterSpacing: "normal"
  metadata:
    fontFamily: "Manrope, sans-serif"
    fontSize: ".75rem"
    fontWeight: 400
    lineHeight: 1.6
    letterSpacing: "normal"
  date-number:
    fontFamily: "Bricolage Grotesque, Manrope, sans-serif"
    fontSize: "1.375rem"
    fontWeight: 800
    lineHeight: 1.2
    letterSpacing: "normal"
rounded:
  radius: ".75rem"
  control: ".5rem"
  navigation: ".5rem"
  image: ".65rem"
  date-marker: ".45rem"
  availability: ".4rem"
  sidebar-move: ".35rem"
  sidebar-utility: ".4rem"
  community-surface: "calc(var(--radius) + .25rem)"
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
    backgroundColor: "{colors.brand}"
    textColor: "{colors.surface}"
    typography: "{typography.action}"
    rounded: "{rounded.control}"
    padding: ".65rem 1rem"
  button-primary-hover:
    backgroundColor: "{colors.brand-dark}"
  button-primary-active:
    backgroundColor: "{colors.brand-active}"
  button-secondary:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.brand}"
    typography: "{typography.action}"
    rounded: "{rounded.control}"
    padding: ".65rem 1rem"
  button-secondary-hover:
    backgroundColor: "{colors.brand-soft}"
    textColor: "{colors.brand-dark}"
  button-quiet-unavailable:
    backgroundColor: "transparent"
    textColor: "{colors.muted}"
    typography: "{typography.quiet-action}"
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
    textColor: "{colors.ink}"
    typography: "{typography.navigation}"
    rounded: "{rounded.navigation}"
    padding: ".5rem .75rem"
  nav-link-hover:
    backgroundColor: "{colors.nav-hover}"
    textColor: "{colors.ink}"
  nav-link-current:
    backgroundColor: "{colors.brand-soft}"
    textColor: "{colors.brand}"
    typography: "{typography.navigation-current}"
  sidebar:
    backgroundColor: "{colors.nav-surface}"
    textColor: "{colors.ink}"
    padding: "1.25rem .75rem 1rem"
    width: "14rem"
  sidebar-collapsed:
    width: "4.5rem"
  sidebar-tooltip:
    backgroundColor: "{colors.ink}"
    textColor: "{colors.paper}"
    rounded: "{rounded.sidebar-utility}"
    padding: ".3rem .55rem"
  sidebar-move:
    backgroundColor: "transparent"
    textColor: "{colors.muted}"
    rounded: "{rounded.sidebar-move}"
    width: "2.75rem"
  bottom-nav:
    backgroundColor: "color-mix(in oklch, var(--surface), var(--brand-soft) 30%)"
    rounded: "1.9375rem"
    padding: ".3125rem"
  bottom-nav-current:
    backgroundColor: "{colors.brand-soft}"
    textColor: "{colors.brand}"
    rounded: "1.625rem"
  availability-label:
    backgroundColor: "{colors.nav-surface}"
    textColor: "{colors.muted}"
    typography: "{typography.availability}"
    rounded: "{rounded.availability}"
    padding: ".35rem .65rem"
  event-row:
    textColor: "{colors.ink}"
    padding: "1.5rem .75rem"
  event-row-selected:
    backgroundColor: "color-mix(in oklch, var(--brand-soft) 35%, var(--surface))"
    rounded: "{rounded.radius}"
  event-image:
    backgroundColor: "{colors.brand-soft}"
    rounded: "{rounded.image}"
    width: "100%"
  date-marker:
    backgroundColor: "{colors.date-surface}"
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
    typography: "{typography.quiet-action}"
    padding: ".5rem .1rem"
  section-link-current:
    textColor: "{colors.brand}"
---

# Design System: Ticketopia

## Overview

**Creative North Star: "Local frequency, less interface"**

### Design philosophy

A sleek, modern interface earns its clarity through hierarchy and restraint.
The event is the subject; controls support it. Lead with the title, date, venue,
and useful facts. Group frequent actions into a quiet, labeled toolbar; disclose
secondary navigation and administration instead of giving every option a box.
Keep zero-interest counts silent until there is something to count.

Use one visual grammar per interaction: event-section navigation is a light-stone
segmented group with a white, bordered current segment and plum text. It does not
mix button silhouettes with an underline. Inactive participation actions are
transparent; selected and pending states carry deliberate emphasis. Preserve
44px targets, explicit privacy labels, and visible keyboard focus as the chrome
gets quieter. A selected preview has one outer boundary and a labeled return
action, rather than nested cards or an unexplained close icon.

Modernity comes from precise alignment, readable type, and fewer competing
surfaces. Retain the plum identity, self-hosted Manrope and Bricolage Grotesque,
provider photography, and dusty-rose date indexes. Avoid ornamental shadows,
purple gradients, glowing buttons, italic serif accents, and rows of stats for
their own sake. Keep meaningful counts next to the actions they explain.

Local frequency makes event discovery feel personally assembled: the presence of a record shop, the intimacy of a mixtape, and the clarity of an everyday planning tool. Deep plum actions and pale-plum selection sit within light-stone navigation and near-white reading surfaces; compact dusty-rose date indexes give the collection a warm, human rhythm.

Bold, readable Manrope, a Bricolage Grotesque display voice for titles and dates, and real provider photography carry the personality. Familiar labeled controls keep everyday planning practical: useful dates, venues, prices, and clear availability matter more than promotional decoration. Density changes with the available space while the event identity stays recognizable.

**Key Characteristics:**
- Deep plum actions and pale-plum selection against light-stone navigation, near-white paper, and white surfaces.
- Dusty-rose date indexing and neutral, honest availability labels.
- Readable Manrope for ordinary rows, prose, and controls; Bricolage Grotesque for page and event headings, the Discover lead, and date indexing.
- Record-sleeve provider imagery and compact, aligned event facts.
- Border-only depth, restrained state transitions, and visible keyboard focus.

### Reference sites

Use the first two as the main web benchmarks for Discover and event details.
Use the others for the specific patterns noted, not as templates.

- **[Southbank Centre — What's on](https://www.southbankcentre.co.uk/whats-on/).**
  [Substrakt's website redesign](https://substrakt.com/projects/southbank-centre/)
  pairs date shortcuts and a calendar with category filters, while keeping
  individual events and curated programmes distinct. For Ticketopia, keep
  date-led browsing prominent and additional filters easy to find without
  letting them dominate the agenda.
- **[Massey Hall and Roy Thomson Hall — What's on](https://masseyhall.mhrth.com/tickets/).**
  The [four-venue website redesign](https://substrakt.com/projects/massey-hall-roy-thomson-hall/)
  was [recognized in 2026](https://we-awards.com/winner/massey-hall/).
  Month headings and recurring event facts make the listings easy to scan
  without giving every venue the same visual identity. For Ticketopia, keep
  the venue and date in predictable places and let provider artwork vary.
- **[Edinburgh Festival Fringe — Plan your visit](https://www.edfringe.com/plan-your-fringe/plan-your-visit/).**
  The [November 2026 rebuild](https://www.edfringe.com/about/news-and-blogs/welcome-to-the-new-edfringecom/)
  gives audience planning its own path, separate from artist and industry
  tasks. Use it as a reference for making practical information easy to find.
  The redesigned audience booking and show discovery are slated for early
  2027, so this is not yet a reference for the new event listings.
- **[Eventbrite — Local events](https://www.eventbrite.com/).**
  Its [March 2025 brand refresh and consumer app redesign](https://www.eventbrite.com/blog/press/newsroom/eventbrite-refreshes-brand/)
  is a useful marketplace reference, not evidence of a full website rebuild.
  Look to the web listings for compact date, venue, and price placement, and
  to the app's named local curators for how to keep recommendations attributed.
  Leave out neon gradients, promoted placements, and campaign styling.

**Programme references.** [DICE's London browse page](https://dice.fm/browse/london)
keeps real artwork beside the date, venue, and price. [Songkick's London calendar](https://www.songkick.com/metro-areas/24426-uk-london)
groups shows by date and gives readers shortcuts such as Tonight and This
weekend. [Cafe OTO's programme](https://www.cafeoto.co.uk/events/) gives each
listing a steady date–artwork–title rhythm and says plainly when an event is
cancelled or what an advance ticket costs. Borrow those information patterns,
not their skins: Ticketopia keeps its own palette and type, unfiltered provider
images, and the same browsing and participation behavior.

**In Ticketopia:** Discover keeps date shortcuts above a compact, ruled
filter disclosure, with space reserved for the agenda rather than another panel.
The results heading names the list; count and sort are supporting information.
Listings, event details, and public collections reuse the same decorative date
stub and venue-local time markup. In the agenda the visible date can be shorter,
but assistive technology gets the full date; unknown times never imply midnight.

**Implementation authority:** [`views/styles/app.css`](views/styles/app.css) is the unminified token and style source; [`views/assets/app.css`](views/assets/app.css) is its compiled output. Frontmatter records the shipped values, retaining CSS custom-property names where present. Tailwind v4 `@source` declarations in that stylesheet supply scan paths. Copied shadcn-templ primitives live in `views/components`; the [component guide](docs/ui-components.md) records theme mappings and native-form adaptations. [`PRODUCT.md`](PRODUCT.md) owns product truth; [`docs/design-guidelines.md`](docs/design-guidelines.md) owns UX; the [surface contract](.impeccable/surfaces/views-home-index-templ.md) retains route strategy and the selected direction's seed. The sidecar's synthesized tonal strips are panel previews, not additional shipping colors.

## Colors

The palette combines muted deep plum, light stone, near-white reading space, and a small dusty-rose date vocabulary; dark ink and clearly differentiated borders keep it practical.

App CSS expresses pinned base colors in OKLCH; hex values here remain familiar
equivalent base swatches. Hover/pressed shades and status washes derive from their
base using `color-mix(in oklch, ...)`, as the frontmatter records. Selection plum
and rose date paper stay separately pinned semantic colors. Library light/dark
pairs use `light-dark()` with a light fallback for older browsers; this does not
enable automatic or whole-application dark mode. See the
[modern CSS conventions](docs/ui-components.md#modern-css-conventions).

### Primary
- **Deep plum** (`brand`): primary actions, actionable text, selected navigation text/icons, brand icon/stop, caret, and focus outlines.
- **Dark plum** (`brand-dark`): primary-action hover, secondary-action hover text, and readable event-date text.
- **Pressed plum** (`brand-active`): primary-action pressed fill.
- **Pale plum** (`brand-soft`): current navigation on every device class, secondary-action hover, image/preview fallbacks, static loading artwork, and browser text selection.

### Secondary
- **Dusty-rose date paper** (`date-surface`): compact ticket-stub date markers only, paired with ordinary ink. Its low-chroma rose tint harmonizes with plum selection and stone surfaces without competing with event imagery. It is not a selection, action, availability, or focus color.

### Neutral
- **Ink** (`ink`): headings, body content, default navigation text, and the collapsed-rail tooltip background.
- **Muted ink** (`muted`): the pinned base for secondary text. It is not applied directly.
- **Warm muted ink** (`muted-warm`): muted ink mixed 12% toward deep plum in OKLCH, so secondary text reads warm rather than neutral grey. It colors venues, explanatory copy, metadata, source freshness, quiet icons, unavailable controls, and the library's `muted-foreground`.
- **Light stone** (`nav-surface`): desktop sidebar, intermediate rail, and neutral availability labels.
- **Hover stone** (`nav-hover`): default navigation and sidebar utility hover.
- **Near-white paper** (`paper`): page background, disabled selects, and light tooltip text.
- **White surface** (`surface`): compact header, search/field surfaces, event context, tickets, and navigation links during customization; also light text on filled plum actions.
- **Quiet line** (`line`): result separators and container boundaries.
- **Control line** (`control-line`): stronger field and secondary-action boundaries.

### Status
- **Error / error wash** (`error`, `error-bg`): failed reads and canceled-event status.
- **Warning / warning wash** (`warning`, `warning-bg`): stale-data notices; warning text also marks postponed/rescheduled events.
- **Success** (`success`): the provider's “On sale” status, always accompanied by its text.

**The Plum State, Rose Date Rule.** Plum carries actions, focus, and selection; dusty rose belongs only to date indexing. Availability stays neutral, and every navigation layout shares the pale-plum current state.

The supporting washes deliberately separate their roles: pale plum (`#ebdde8`)
makes selection visible, derived hover stone stays neutral, and warmer
dusty rose (`#e8d2db`) gives dates their own index. Keep the deep-plum action
colors and near-white reading surfaces unchanged. Selection is also communicated
through current-navigation weight and `aria-current`, with a distinct plum focus
outline; do not rely on pale fills alone. Ink on date paper is 10.50:1, plum on
selection is 6.53:1, and muted ink on selection is 4.54:1. The selection wash is
one RGB step lighter than `#eadce7`, whose muted-text contrast falls below 4.5:1.
Warm muted ink is slightly darker than its base: 6.20:1 on white, 5.91:1 on paper,
5.47:1 on light stone, 5.74:1 on the phone navigation surface, and 4.74:1 on
selection. It also stays AA on every tinted event header (`internal/discovery/tint_test.go`).
Date markers use ink, not muted text. Loading actions pair pale plum with plum
text rather than white.

## Typography

**Body font:** Manrope, with a sans-serif fallback. The variable font is self-hosted as two WOFF2 `unicode-range` subsets (`views/assets/fonts/manrope-latin.woff2`, preloaded, and `manrope-extended.woff2`), supports the shipped weights (400–800), uses `font-display: swap`, and is distributed with its SIL OFL license.

**Display font:** Bricolage Grotesque (SIL OFL), exposed as `--font-display` with a Manrope fallback. It is self-hosted and preloaded as a 65 KB Latin subset (`views/assets/fonts/bricolage-grotesque-latin.woff2`): width pinned to 100, weights 600–800, the optical-size axis kept, and tabular/lining figures retained. Its ink-trap grotesque shapes give titles a record-sleeve voice next to Manrope's rounded text. Use it **only** for page titles (`h1`), the event title (the event page heading and the preview heading), Discover's **Up next** lead, date numbers on ticket-stub markers, Discover's calendar agenda headings (Today, This week, December…), and the community section titles described below. Ordinary row titles (including the lead when a preview is open), empty-state lines, navigation, controls and prose stay Manrope. Display sizes keep the existing fluid tokens; `--text-display` spans 1.75–2rem (1.14×), within the 2.5× maximum. Display headings use −.02em tracking, which suits the face's tighter display optical size. Sourcing and subsetting commands are in [`views/assets/fonts/README.md`](views/assets/fonts/README.md). Inter and italic serif are not app type roles.

**Character:** rounded, direct, and substantial. Tight heading tracking gives short titles confidence; ordinary casing and generous body leading keep long event information readable. The hierarchy is role-based, not a manufactured modular scale.

### Hierarchy
- **Display:** `typography.display` (Bricolage Grotesque) for page headings, fluid from 1.75rem to 2rem. Event-page headings use the separate `--text-event-heading` token, fluid from 1.5rem to 1.75rem. Both use rem bounds and rem + viewport interpolation rather than font-size breakpoint overrides.
- **Headline:** `typography.headline` for section and preview headings, including the lead line of destination empty states. Event-content section headings use 1.125rem.
- **Title:** `typography.title` for supporting headings; `typography.event-title` for result titles, with the more open leading needed by long names.
- **Discover lead:** `typography.discover-lead` scales the Up next title from 1.25rem to 1.625rem with the results container. With a preview open, the lead uses ordinary row typography.
- **Body:** `typography.body` for the base reading size; `typography.body-small` for event descriptions. Observed reading measures are 65ch for introductions, 70ch for event prose, and 75ch for source notes.
- **Label / action:** `typography.label` for field labels and `typography.action` for primary/secondary controls. Labeled filter inputs remain at the body size with weight 500; search uses ordinary body weight.
- **Navigation:** `typography.navigation` for full navigation (weight 550), with `typography.navigation-current` for the current destination (weight 700). Group labels use `typography.navigation-group`; rail/bottom-nav labels use .75rem (`typography.navigation-group` size) and the same normal/current weights. Collapsed desktop labels remain accessible and appear as .75rem tooltips (weight 650). Event-section links use quiet-action sizing and weight 650, rising to 750 when current, with body leading.
- **Metadata:** `typography.metadata` for supporting facts and freshness; `typography.small-label` for date lines. Quiet participation uses `typography.quiet-action`, and neutral availability labels use `typography.availability`. Date numbers use `typography.date-number` in the display face; month labels stay Manrope at weight 750. Community empty states lead with `typography.empty-display`, a Manrope line fluid from 1.375rem to 1.75rem (1.27×), kept below the page heading. Date markers and event-date lines use tabular numerals.

Themed library primitives retain their own utility leading where the app has not overridden it: button-based navigation and quiet buttons use the `text-sm` ratio (`calc(1.25 / .875)`); availability and sale-status badges use the `text-xs` ratio (`calc(1 / .75)`); search/filter input text uses 1.5. These are observed inherited roles, not a universal 1.6 line-height applied to every control. Event-section links, plain date lines and sidebar customization labels retain the body leading.

Headings balance lines, prose uses pretty wrapping where supported, and inherited
`overflow-wrap: break-word` handles long provider names without shrinking
min-content-sized boxes to a single letter. Flex/grid items can shrink with
`min-inline-size: 0`. Manrope's real variable weights are used without synthetic
bold/italic; font smoothing is explicit. Smaller metadata supports the event title
and essential facts rather than replacing them.

**The One Voice, Clear Roles Rule.** Bricolage Grotesque is a voice for titles and dates, not a second text family. Use Manrope's size, weight, leading, and spacing to distinguish everything else; keep dates tabular and long event names able to wrap.

**Wordmark exception:** the `ticketopia.` wordmark in the compact header is a logo, not a text role. It stays at `min(1.6rem, 8vw)` (weight 800, −.04em tracking) and steps down to the headline size, `min(1.25rem, 6.25vw)`, once the compact header sticks. The `vw` caps hold it at its default-text size on a 320px screen, so enlarged text cannot wrap it; the desktop sidebar sizes it separately.

**Provider-brand exception:** only the Google sign-in CTA uses self-hosted Google
Sans Medium (500) at .875rem/1.25rem, following Google's branding requirements.
Manrope remains the family for all surrounding account and application UI.

## Layout

The centered application shell stops at 100rem (120rem for discovery) and fills at least the small
viewport height (`100svh`), avoiding document relayout as mobile toolbars move.
Main content uses normal document scrolling. Fixed-height sidebar UI still uses
`100dvh`. Reused spacing primitives are the source's `space-1` through `space-7`;
component-specific insets remain explicit rather than becoming another scale.

| Threshold | Shipped behavior |
| --- | --- |
| Base, below 40rem | Content padding 1.5rem 1.25rem. Light compact header and a four-item floating pill navigation, inset .75rem from the sides and `max(.75rem, .25rem + env(safe-area-inset-bottom))` from the bottom; its normal-flow row and margins reserve that space. Below 62rem and at least 32rem tall, the header sticks and its wordmark steps down where supported. A 44px search icon ends the header row; on Discover it focuses the keyword field in place, elsewhere it opens Discover there. With `viewport-fit=cover`, the workspace is inset by the left/right safe areas (a landscape notch), and the header's white bar runs edge to edge while its content clears them. |
| At most 23rem | Content padding becomes 1.25rem 1rem. Navigation side insets cap at 2.5vw so enlarged labels stay whole. Event headings and section links reduce to fit. |
| Intrinsic | Filter fields pair while each column keeps 9.5rem for a full date, otherwise forming one column. The keyword field keeps 6rem beside the search icon, otherwise Find events wraps below it; in a results column of at most 21rem (a container query, so enlarged text counts) search drops its decorative icon and tightens its inset. A long event-page return label wraps beside Share. The compact header shows its decorative tagline only once the header container reaches 35rem, so enlarged text hides it. |
| At least 40rem | General page padding becomes 2rem. |
| At least 62rem | A fixed 5.5rem light-stone navigation rail replaces the bottom bar; the light header remains. Discovery gains a 20rem context column with 1.5rem gutter/padding. Below this threshold, event pages at least 32rem tall (measured at the page's actual root text size) pin Tickets/Save above the bottom navigation. |
| At least 72rem | A sticky labeled sidebar replaces the fixed rail/header, occupying 14rem expanded or 4.5rem collapsed. Discovery context becomes 21rem with 2rem gutters/padding; event/destination page padding is 2rem 2.5rem, with destination top padding increased to 3rem. |
| At least 86rem | The filter grid becomes three columns, with the city field spanning two. |

Result lists respond to their `results` container rather than these thresholds: artwork stacks above facts below 17.5rem (enlarged text on phones), phone-width containers (17.5–26rem, low enough for a padded 320px phone) put a square 5.5rem thumbnail beside the facts with a 1rem gap and 1rem row padding while details and actions span below, rows use a square 7.5–11rem image column with a 1.25rem gap from 26rem, and from 48rem an intrinsic grid (16rem minimum, at most three columns, 1.5rem gap) shows cards. The CSS and JavaScript share the 62rem context and 72rem desktop-navigation thresholds. Selection, direct-entry, return-state, and resize strategy belong to the [responsive UX guide](docs/design-guidelines.md#discovery-to-discussion-journeys-and-shared-state) and [surface contract](.impeccable/surfaces/views-home-index-templ.md), not additional global design rules.

Once the actual workspace reaches 64rem, a selected discovery event uses a 2:3
results/context split, retaining a 24rem minimum results column. Collapsing
navigation can also unlock this layout. Inside selected thread previews, discussion and
community use an intrinsic two-column grid with 18rem shrink-safe column minima
and a 1.5rem gap; narrower previews stack in reading order. Other event selections
use the shared Overview, Questions & tips, and Recommendations navigation. Overview
prose and practical notices remain visible. In panels Tickets/Save precede artwork; secondary
participation follows content, with source/history in one disclosure. Standalone
identity is a wide 2:1 artwork band (22rem cap; a 6rem strip without artwork) with
facts on a tinted surface overlapping its lower edge by 2.5rem. The tint is the
event's stored artwork colour (`--event-tint`, set inline from `Event.tint`) or
`brand-tint` without one, mixed 18% into white in OKLCH. Every stored tint has
OKLCH lightness .66 and chroma of at most .12, so all header text, including muted
venue text, stays at WCAG AA on any hue; the raw brand colour would leave muted
text at 4.35:1, so the fallback is the brand hue at tint lightness. Default context columns
cap at their 20rem/21rem roles or 45% of available content width, whichever is
smaller, so enlarged text does not consume the results column.

The context panel is sticky from 62rem (top 1.5rem), with `max-height: calc(100dvh - 3rem)`, vertical overflow, and a stable scrollbar gutter; it is keyboard focusable. From 72rem, selected context has the same minimum height, avoiding a short, wide panel when activity is sparse. The full sidebar is one dynamic viewport tall, with grouped navigation and a footer pushed to the bottom. During customization its navigation region scrolls independently, keeps a stable scrollbar gutter, and becomes keyboard focusable so tools remain reachable at small viewport heights; scrolling navigation pads .5rem so focus outlines aren't clipped. The collapsed rail can't scroll without clipping its labels, so when its destinations don't fit the window app.js flags it and it grows to its content and scrolls with the page. The intermediate rail scrolls when enlarged text outgrows a short window. Ticket panels remain in the page layout. Filters use two equal columns and a 1rem gap until the small-phone or wide-screen adjustments apply. Below 62rem, with JavaScript, Filters opens as a white bottom sheet (native modal `<dialog>`, at most 40rem wide, 1.25rem top corners, 40% ink backdrop) with a sticky Filters heading and close control; its columns follow the sheet's width. It rises in 240ms with `--ease-standard` only under `prefers-reduced-motion: no-preference` and closes at once. Without JavaScript the same fields stay inline in the disclosure.

## Elevation & Depth

White, paper, and stone surfaces are separated by spacing, quiet borders, and
stronger control borders. Discover and collection cards, the event identity and
preview, Community and Happening Now cards, and event composers, notes and quotes
use flat surfaces without corner glows, cast shadows, or hover lifts. The selected
result stays in the list geometry: a pale-plum wash and a plum **Viewing** label,
with a distinct forced-colors frame. Pointer and press feedback changes the
surface or text color; the title and artwork links do not paint their own
pressed box. Fixed navigation, sticky context, and collapsed-rail tooltips are
functional layers. The phone navigation and compact event action bar can float
without needing a shadow or blur.

Focus is an outline, not a shadow: a solid plum ring (3px) offset from the target (4px), including navigation and sidebar controls. Themed library focus rings and card shadows are explicitly suppressed.

The source does use `box-shadow` for drag insertion markers: `inset 0 2px 0 var(--brand)` before a target and `inset 0 -2px 0 var(--brand)` after it. These are functional two-pixel rules inside a reorder target, not elevation or cast depth; the dragged item/section also dims to opacity .55.

**The Border-Only Depth Rule.** Separate surfaces with tone, spacing, and borders.
Selection is tone plus a label, never a lift. Keep functional focus, arrival,
and reorder markers distinct from decoration.

## Shapes

Use modest, role-specific corner treatments: the shared `radius` for context/ticket containers, selected rows, and the search surround; `control` and `navigation` for buttons, fields, and links; `image` for photographs; and the smaller `date-marker` and `availability` corners for indexing and state. Sidebar move/drag tools use `sidebar-move`, while collapse, commit controls, and tooltips use `sidebar-utility`. These are rounded rectangles, not a universal pill vocabulary; the phone navigation and its current destination are the one pill-shaped exception. Narrow result rows are separated by a top rule; only the wide results grid encloses events in white, bordered cards.

Some component-local corners are intentionally outside the reusable scale: the search input is .2rem, filter summary .3rem, and preview artwork .6rem. The idle Discover preview uses one boundary around the compact ticket mark, heading, and guidance, not a second nested card or placeholder image.

Event imagery fills a clipped frame with `object-fit: cover`. The default frame is 16:9; regular container rows use square thumbnails beside their facts, while the Discover lead uses a full-width 2:1 frame in narrow lists with no open preview. The full-event band is 2:1 capped at 22rem, and a compact selected preview caps artwork at 16rem. Preserve provider color and content; the reference photographs do not prescribe a grayscale filter.

Icons are outline SVGs with a 24-unit viewBox, rounded caps/joins, and stroke width
1.7. Label-adjacent icons use font-relative em sizing and cannot shrink; larger
decorative/fallback icons retain explicit rem sizes. The fallback uses a music
icon for classified music events and a ticket icon otherwise. It occupies the
same image frame, so a failed image does not erase the event's place in the list.
The fallback carries the event's own colour (`--art-tint`): its stored artwork
tint, or, without artwork, one of seven hues (plum, rose, amber, sage,
teal, indigo, orchid) chosen from its ID at the stored tints' fixed OKLCH
lightness .66 and chroma .08. It uses a flat event tint, a faint .875rem
perforation dot grid, and a solid white icon tile (smaller and clear of the date
stub in thumbnails). Forced colours drop the paint and keep the icon.

## Components

### Buttons

Compact, confident, plainly labeled controls. Primary actions pair plum with white text; secondary actions use white, plum text, and a control-line border. Both use the frontmatter's action typography and padding with a minimum height of 2.75rem (44px), not a fixed height that clips wrapping labels. Minimum heights, borders, focus outlines, and transitions live in the sidecar snippets because the frontmatter's component schema cannot express them accurately. Hover darkens primary actions and gives secondary actions a pale-plum fill. Pressed primary actions use `brand-active`; there is no pressed translation or raised effect.

**One Tickets and Save family.** Tickets is solid plum with white text on the event page and in the preview, and outlined with plum text in result lists, where it remains the prominent action beside quiet Save. Ticket actions share .625rem corners, a trailing 1.125rem external-link icon and a 44px minimum. The label is **Tickets on Ticketmaster · From USD 25.00** on the event (the price drops from view in a pinned bar under 24rem and the provider under 26rem, both kept in the accessible name) and **Tickets** in rows, whose accessible name keeps the provider and event. A row's Tickets fills its action bar beside Save (up to 22rem) and, in a row's action group narrower than 15rem (enlarged text), drops its icon rather than break the word. Ghost companions (`.card-action-secondary`) carry other one-step destinations: the preview's **Open event page** and Happening Now's **Hide/Show activity toasts** and **Refresh**, each with an icon.

From 62rem, the filled Tickets button in event details and previews is capped
at 28rem so it does not stretch across wide layouts.

Quiet event controls use transparent fills, quiet-action typography, and the same
minimum target height. Recommend is a secondary link that opens the native
Community composer; Save, Interested, and Recommend explain account-disabled or
unavailable states rather than simulate success. Underlined text links offer
navigation and small utility actions; the labeled preview return link has a
2.75rem target. Event-section current text uses weight 750 and inherited body
leading; its links have their own styles rather than button utility classes.

Inside the shared event-actions row, enabled quiet controls use transparent surfaces
and borders, .5rem label/icon gaps, and .625rem/.75rem block/inline
padding. The row wraps intrinsically with .5rem/.75rem block/inline gaps; utility
links retain lighter, underlined quiet-action type. Save and Interested share
pale-plum selected states, tinted hover, and a deeper flat press wash. Unavailable
controls remain muted and transparent; pending forms preserve plum state. No
shadows or geometry animation are added.

On-sale reminder is a quiet bell-and-label action alongside Interested and
Recommend in the event view's secondary participation group. Save stays beside
tickets and never enrolls someone in reminders. The shared reminder control is
shown only for an eligible saved event with supported configuration; it navigates
to settings without implying enrollment or an active reminder. Confirmed Save
responses synchronize settings visibility; failed writes retain the prior state.

Private Save controls are implemented when opt-in accounts are enabled. Their visible label is the state, Save or Saved, except in Saved itself, where it is the action, Remove; the accessible name always says the action (Save / Remove from Saved, with the event name), and the label reserves the width of its longest form. They inherit the quiet control's type, corners, target height, and plum focus outline; a saved event adds pale-plum fill with plum text and border, while hover uses pale plum and dark-plum text. Signed-out visitors get a Sign in to save link; disabled deployments explain the account requirement, and unavailable saved state is labeled rather than guessed. Ordinary POST forms work without JavaScript. Enhanced writes show pending, success, or retry feedback below the shared controls row and synchronize visible row/preview state only after confirmation. Feedback never participates in button alignment or wrapping; controls align at their top edge when labels wrap. The signed-in Save control always reserves the width of **Remove from Saved**, so toggling never moves it or rewraps the row. Failed writes retain the prior state. Saved/Interests removal keeps surviving rows mounted, names the event, and restores removal-control focus without stealing focus moved elsewhere. Empty later pages prioritize newest-page recovery; a wholly empty collection leads to Discover. Transport/unreadable-response failures explain uncertain confirmation and safe retry without affecting independent recommendations. See [action feedback and removal](docs/saved-events.md#action-feedback-and-collection-removal).

Interested is an independent durable choice when opt-in accounts are enabled. It inherits the same quiet control treatment, with pale-plum fill, plum text/border, and `aria-pressed` for the selected state. The separate aggregate uses .875rem ink text with a bold, tabular number. The action visibly says “Interested · Private” or “Interested · Public”, and its accessible name includes that label; do not repeat the visibility elsewhere while collapsed. Identity starts private; a profile visibility default applies only to new choices, not existing ones. A compact “Change visibility” disclosure beside the count reveals the per-event form and public event/profile explanation. Only one editor opens at a time; saving closes it and restores focus to its summary. Signed-out visitors get a sign-in link. Ordinary POST forms and enhanced writes both work; enhancement synchronizes confirmed row/preview state and announces pending/success without persistent visual status paragraphs. A single dismissible error stays near the action, outside the controls row, until dismissed, retried, or replaced. Failed writes retain the prior choice. Save is unaffected. See [event interest](docs/event-interest.md) for the feature contract.

The Google sign-in CTA is a scoped provider-brand exception using Google's
documented filled-blue treatment: #0b57d0 fill/border, white text, .25rem corners,
and a 20px current gradient G centered in a 36px white tile with a .625rem label
gap. Compact .1875rem insets frame the tile; .875rem trailing padding balances
the label. Hover/pressed fills mix the provider blue with 18%/35% black in OKLCH
rather than adopting plum.
It retains the app's 44px minimum target, plum focus outline, flat depth, and a
no-JavaScript OAuth link. Logo/font sources and licensing are recorded in
[`views/assets/fonts/README.md`](views/assets/fonts/README.md).
Event sign-in keeps the event/section identity and a native guest return; it resumes
the explicit action without performing it, except that Sign in to save completes
the Save it names. Expired participation writes expose a
contextual sign-in link. See [task continuity](docs/accounts.md#sign-in-task-continuity).

Action background changes and disclosure-chevron rotation
use 160ms; result selection uses 180ms. Disclosures grow open over 200ms and
shut over 140ms where `::details-content` and intrinsic-size interpolation are
supported, and a
loaded preview settles in (fade and .375rem rise, 200ms). Sidebar collapse and
desktop preview open/close run as same-document view transitions on the drawer
curve `--ease-drawer` (`cubic-bezier(.32, .72, 0, 1)`), 280ms to expand or open
and 200ms to collapse or close: the page switches to
its new layout at once while the rail or preview slides over it like a drawer,
revealed under a clip. Collapsed-rail labels fade in over 120ms. Saved
bookmarks and interested stars fill in over 160ms and, when turned on in place,
pop with a 280ms overshoot (from .85× to a 1.12× peak at 40% of the time). Cross-document view transitions run 250ms with the shared ease-out curve, moving per-event artwork/titles between rows, preview and event page; they are enabled only under `prefers-reduced-motion: no-preference`. Buttons and badges share the source's
`cubic-bezier(.16, 1, .3, 1)`; other library buttons retain 150ms for color/fill
changes only. Script animations read `--ease-out` and `--ease-standard` from the
root instead of copying the curves, and htmx's injected ease-in indicator style is
turned off (`includeIndicatorStyles: false`). Geometry transitions opt into motion only when no reduction is
requested; there is no global safeguard, so each animation carries its own guard.
Native links, buttons, summaries and selects share a 160ms ease-out colour
baseline (background, text, border and
underline colour), which reduced motion keeps. Pressed colours apply at once
(`:active` transitions take 0ms) and only the release eases back; under no
reduction, primary actions (`.action`, Tickets, Save) also settle to .97× over
160ms while pressed. Under no reduction, action feedback,
inline errors, revealed controls (reminder, visibility, custom timing)
fade in over 160ms, live-activity toasts drop in over 200ms, pending forms dim
gradually, a Save/Interested/recommendation/discussion write still pending after 300ms
swaps the action's icon or label for a same-size spinning ring (pulsing under
reduced motion), and collapsed-rail labels also fade out; hiding is otherwise instant.
A confirmed Helpful count rolls in from the direction it moved (240ms, .4em;
a plain fade under reduced motion). An event section or conversation still
loading after 250ms keeps its content at full contrast and shows a 2px brand
line along its top edge with a progress cursor (a 160ms fade, kept under reduced
motion; a border, so forced colours keep it); the arrival removes it at once. Helpful shares the
participation controls' tinted hover and deeper press wash. A Discover search
still loading after 300ms shows the same ring on its submit button (white on
plum), cleared when Back restores the page. A section nav's current-tab
indicator (event segment surface, Community/moderation underline) slides to
the newly current tab (240ms, `--ease-standard`): in place for event sections,
by cross-document view transition for page tabs; reduced motion and no-script
navigation show it in place at once.
A reader-chosen event section or conversation fades in (160ms); Load more rows
settle in before first paint, the first few cascading 0/40/70/90ms apart (all at
once when a visible keyboard focus moves into them), and late-loading result artwork fades over its placeholder
(200ms). A post reached by link, or just published or edited, flashes a pale-plum
fill once over 1.6s without a surrounding shadow (static fill under reduced
motion, outlined in forced colours). Rows that survive a Saved/Interests
removal and reordered sidebar items and sections slide to their new places
(240ms, `--ease-standard`). Hover requires a fine, hover-capable pointer; touch
press feedback changes fill/color, with only the primary actions' small press scale.

The compact header's scroll-bound motion is gated on
`prefers-reduced-motion: no-preference`. Below 62rem
and on viewports at least 32rem tall, browsers with scroll-state container queries
(`@supports (container-type: scroll-state)`) stick the compact header. Once 1rem
has slid off the top it is stuck: in one step its content recentres (a 160ms
ease-out translate) and the wordmark drops to 1.25rem, reversing when it unsticks.
It is a threshold, not a scroll-linked shrink, so layout happens only at the two
transitions. Root scroll padding grows to clear it, and root scroll anchoring is
off so the step cannot pull the page back above the threshold. Other browsers keep
the header static in the page flow.

### Inputs / Fields

White, clearly bounded, ordinary fields. First-time Discover puts city and optional country before the search and keeps advanced filters collapsed. With a city selected, the command bar joins **Events in {city}** (a link that opens the city filter) and the search segment (SVG icon, labeled keyword input, submit action) inside one control-line surround. It is one row once the results container reaches 34rem and stacks below that. The borderless keyword field lends its focus to the surround: the bar's border turns plum while typing and carries the keyboard focus ring (browsers without `:has()` ring the field). Beneath it, **Tonight / This weekend / Next 7 days** chips wrap as needed. They set only `start_date`/`end_date` for the browsed city's calendar day. The current chip uses a pale-plum fill, plum border and underline, and `aria-current="true"`. Discover's labeled fields use a minimum height of 2.875rem, body-sized text, and visible control borders; disabled genre selects use paper and muted ink. Native `details`/`summary` discloses filters and extra event metadata. The shared focus outline remains visible; field errors use explanatory text and the error-state container rather than an invented field-specific color system.

Community keeps the City, Country code, Category and, when available, Events
labels visible even after the user fills the fields. Its native GET filters
wrap in reading order on a ruled strip with 44px minimum controls and one
outlined Apply action. Happening Now uses the same ruled strip for live scope,
filter disclosure, and actions without changing their reading order.

Category and genre use the same native select treatment. All categories disables genre; changing category clears the previous genre and offers compatible options. The ordinary GET form supports the same workflow without JavaScript by applying category before selecting a genre. Result rows, previews, and event pages show supplied category/genre metadata with an explicit unknown fallback.

### Navigation

Quiet, grouped icon-and-text navigation. Expanded desktop navigation uses light stone, a quiet right border, ink labels (weight 550), muted outline icons, hover stone, and pale-plum current state with plum text/icons (weight 700). Browse groups Discover and Community; Your space groups Saved and Interests. Profile remains fixed in the bottom footer. Links and utility/move controls preserve 44px targets.

Desktop collapse hides visual labels without removing their accessible names. CSS shows a compact label tooltip on both hover and keyboard focus: ink background, paper text, no cast shadow. The pointer can cross onto the tooltip without it closing, and Escape dismisses it until the link is neither hovered nor focused. Group labels and customization controls hide while collapsed. The intermediate fixed rail and compact four-item bottom navigation keep a fixed order: Discover, Saved, Community, Profile. On phones the bottom navigation is a floating pill: inset from the screen edges and safe areas, a soft white surface with 30% pale plum, a one-pixel border, and icon-over-label items with .75rem labels. The current destination is a clear pill of its own: pale-plum fill, plum text, a plum-tinted border (Highlight in forced colors). The rail shares that current state with .5rem corners. The intrinsic grid reflows to two rows when enlarged text needs more space, with complete, unbroken labels inside their targets. A sticky, normal-flow shell row and its margins reserve the pill's actual height and the bottom safe area; the intermediate rail remains fixed. Below the context threshold, on Overview only, the event page's Tickets/Save bar sticks .5rem above the pill as a rounded, bordered card aligned to its edges, with both actions on one row; a bar narrower than 26rem shows **Tickets** and keeps the provider in the accessible name. Where scroll-direction container queries are supported and motion is allowed, scrolling down tucks the pill below the screen edge (220ms) and any upward scroll or focus inside it brings it back; a stuck Tickets/Save bar lowers into the freed space. Short viewports and other browsers keep the pill in place.

Explicit customization reveals section/link drag handles and Move up / Move down controls; links use white draft surfaces. Sections can move, links stay within their section, and Profile stays fixed. Done commits the draft; Cancel or Escape restores the previous order; Reset to default changes only the cancellable draft. Collapse is disabled while editing; leaving the desktop threshold cancels unfinished editing. Preferences are browser-only, not account persistence or cross-device sync. Storage failure leaves current-page controls usable with honest feedback. Without JavaScript, the expanded default navigation remains and enhancement-only tools hide. See the [sidebar preference contract](docs/design-guidelines.md#sidebar-preferences) for behavior details.

Event sections remain real links in a light-stone segmented group. The current
section has a white surface, quiet full border, heavier plum text, and an explicit
forced-colors border. Inactive hover uses neutral stone, not the current state.
The segmented grid adds rows as space runs out so every section remains visible
on phones and at enlarged text. Focus rings draw inside the tab. The date
shortcuts also wrap; neither control requires sideways scrolling. All retain
44px targets. Current
destinations use `aria-current`, not partial ARIA-tab semantics. Other section
navigation retains its straight underline with square link corners.

Same-event section changes keep the event header/actions, section navigation,
participation group and source/history disclosure mounted, on Discover and
standalone event pages. A quiet inline message labels retained previous content
during loading; completion is announced without persistent visual chrome. Local
Retry/native links recover failed reads. Overview artwork belongs to the context
section; standalone identity artwork stays in the header. The existing palette,
type roles, targets, thresholds and native link semantics are unchanged.

Standalone participant, recommendation and conversation drill-downs share a muted,
regular-weight collection return above a labeled group of section-return and Event
Overview links. The event links wrap together before event identity; named links,
44px targets, safe origins and restoration hooks remain intact. See the
[return hierarchy](docs/design-guidelines.md#drill-down-return-hierarchy).

### Availability Labels

Small neutral rectangles with light-stone fill, muted text, a quiet one-pixel border, and availability corners explain “In development” or a specific unavailable capability. They are informational labels, not selectable filter chips. Do not give them date-marker styling or turn them into a success confirmation.

### Indexed Event Rows

Discover extends the image/facts pair with supplied performers/teams, separate
sale information, and a short description spanning the row width. Direct
**Tickets** (named "Tickets on Ticketmaster" for the event) and the quiet Save/Saved toggle share one row and follow wrapping public activity
**count chips**: pale-plum pills (icon, tabular count and label, 44px) that open
the event's Questions & tips or Recommendations section.

**Discover as an agenda.** Date-sorted results are grouped by the browsed city's
calendar: **Today**, **This week** (through Sunday), **Next week**, **Later in
{month}**, then each month (with the year once it differs). Each stretch opens
with a heading in the display face (fluid 1–1.1875rem, weight 700, below the
"Upcoming events" heading; sized to the results column so a month name stays whole), its dates in muted tabular text, and a
hairline running to the edge. Headings span the card grid; in lists they replace
the next row's rule. They repeat each row's full date, so they are `aria-hidden`
list items rather than headings, and Load more drops one that continues the
previous page's stretch (keys carry each stretch's dates, and every page uses the
city's remembered calendar day). Name-sorted results stay one list. The soonest
event on the first page leads as an **Up next** card when its date is known and
not past: across the whole card grid with
artwork at the start and a fluid Bricolage Grotesque title beside it, and with
full-width 2:1 provider artwork above its facts in narrower lists (an ordinary
row while a preview is open). When there is no usable photograph, a compact
square fallback sits beside the title instead. A white bordered label
marks its artwork's top start corner with how soon it is (**Up next ·
Tonight** for a known evening start, Today, Tomorrow, In N days within two weeks,
otherwise **Up next**). An event already under way passes the lead to the next.
With three card columns (a results column of at least 51rem) the lead takes two
when another event of its stretch follows to sit beside it. From 62rem, where the compact header doesn't
stick, each agenda heading is sticky at the top of the viewport over opaque paper
(`Canvas` in forced colours), so each later heading covers the one stuck before
it. The root's block-start scroll
padding grows to 4.5rem so revealed and focused rows clear it. Count chips show a
short visible form (**2 questions**, **2 recommend**) with a bold count that begins
its accessible name (2 questions & tips, 2 recommend this event), so voice control
can use the visible words; live updates rebuild the same label.

**Event cards.** Wide result lists in Discover, Saved, Interests and Radar use the
Community cards' anatomy: full-bleed 16:9 artwork with the date stub, 1.25rem
padded facts, and a hairline-divided action bar at the foot that lines up across
a row of cards. The lowest known price leads the facts as an ink price tag
(.9375rem, weight 800, tabular: **From USD 25.00**, compared numerically within the
first priced currency, or the price alone when there is one fixed range) before the
sale status; an unknown price keeps "Price not listed". Under an agenda heading a
row's visible date drops what the heading and stub already say (**Sunday · 7:30
PM** within a week, **Sun, Dec 6 · 7:30 PM** within a month) while screen readers
keep the full date. With motion allowed and a fine pointer, hovering a card's
title or artwork eases its photo to 1.04× inside the frame (300ms, `--ease-out`);
Community cards share it. The frame and card do not lift. Discover and Radar
cards quote their event's newest public recommendation reason as a pull quote:
two lines at most (three on the Up
next lead), body size (the lead's larger), the plum opening mark, then a muted
"{name} recommends this event" with no profile link; a reason-less recommendation
shows nothing. Question text and author links stay in event sections, and the
compact collection rows show no quotes. Saved, Interests, and
Radar share the Overview title/image links and direct ticket action, preserving
collection context and Radar reasons. Only Interests retains interest management
in its rows; reminders remain in event views. Missing URLs and disabled accounts
have concise explanations rather than inert list toolbars.

The signature pairs provider photography with compact dusty-rose date indexing. Rows lead with artwork, then local show date, event title, and venue, followed by sale status and known price or an explicit unknown. Their selected state is a flat pale-plum wash labeled "Viewing"; the title and image remain real Overview links. The date marker is a ticket stub: radial-gradient masks perforate both inline edges, and on artwork it is a one-line chip (uppercase month, tabular day) in the image's lower start corner, wrapping between month and day only when space runs out. Its short date is decorative beside the full readable date; unknown dates say TBA.

The private Saved collection reuses these rows, ordered newest saved first, with ordinary pagination and event links that retain the return page. Its privacy statement and last-known-details guidance distinguish durable saves from public interest and current ticket availability. Save remains independent of Interested, whose list control appears only in the own Interests collection. That collection uses the same rows, newest interest first, above private category preferences on `/me/interests`. Its event links and participant pagination preserve the originating collection page. Empty collections lead back to Discover. See [saved events](docs/saved-events.md), [event interest](docs/event-interest.md), and the [Saved surface contract](.impeccable/surfaces/views-home-saved-templ.md) for feature behavior.

Photos are live external Ticketmaster assets retained in normalized `Event.Images`; presentation chooses an available width close to the requested size. The only authored rasters are the app icons (home-screen and manifest PNGs rendered from the SVG ticket-tile sources in `views/assets/icons/`); the sole local provider raster is Google's unmodified gradient G for sign-in. Local styles, JavaScript, htmx, fonts, the web manifest, app icons and that provider logo are Go-embedded assets; SVG supplies the interface icons and the favicon. The sidecar demonstrates the shipped image fallback rather than copying provider photography into documentation assets.

### Public Recommendations

Recommendations lead with people's reasons. **Write a recommendation** (or **Edit your recommendation**, its accessible name once you have one) is a compact bordered disclosure at the end of the **Public recommendations** heading row; signed-out visitors get a **Sign in to recommend** link in the same place. Opening it gives the editor the full row beneath the heading, above the list: a paper-filled textarea, a solid-plum **Publish/Save recommendation** with a one-line **Public, with your name and profile link.** beside it, and a **Where it appears** disclosure carrying the full visibility explanation. Withdrawal is a second step: a **Withdraw** disclosure names the consequence before its **Withdraw recommendation** button; **Keep recommendation** (with JavaScript) or the summary closes it without touching the draft. Ordinary POST forms remain usable without JavaScript. Enhancement dims pending forms, preserves drafts and confirmed content on failure, and closes the editor with focus restored to its summary after confirmation. Beside an open desktop preview, the selected Discover row hides its own Tickets/Save; the preview carries them.

Section titles in these community sections, and thread headings, use the display face at `--text-section-display` (-.03em). List headers pair an h3 with a pale-plum count pill.

On the event, recommendations are **quote cards** in an intrinsic grid (18rem minimum, auto-fit): flat white cards whose reason is the headline (1.0625–1.1875rem, weight 550), with a quotation mark and a rule-separated byline (initials avatar, linked name, **You** when applicable, date). A plum border distinguishes the viewer's own card. A reason-less card reads "Recommends this event" in muted text.

Questions and replies share one **note** shape: a pale-plum 2.25rem initials
avatar (2rem for replies, a dashed empty circle when no author is shown) beside
a 44px line holding the linked name, any "via" app, and a muted tabular date or
time. The person's words follow at body size in ink, wrapped at 70ch with line
breaks kept. Notes share a bordered panel with quiet dividers. When enlarged
text leaves less than 14rem beside the avatar, the words wrap below it. Explicitly
public interest renders as bordered name chips with small avatars. Profile lists
keep flat, rule-separated attribution and reasons beside event identity.

Community recommendations, conversations, and open questions use bordered
cards in an intrinsic grid (18rem minimum). Wide grids and the narrowest
containers stack 16:9 artwork above identity. A single-column list with room
for both uses a square thumbnail with the date chip beside the title, full
date, and venue. The quote, event facts, and actions keep the full card width.
The recommendation byline
keeps the pale-plum initials circle, linked name, and muted attribution with any
app. The action bar is separated by a rule and keeps a soft-plum main destination
and a quiet companion (**See all** or **Questions & tips**), both 44px with
complete accessible names; on very narrow cards the icons drop and labels wrap.
Event-local lists omit duplicate artwork. Publication and edit metadata stay
secondary. See [public recommendations](docs/event-recommendations.md) and
the [recommendation surface contract](.impeccable/surfaces/views-home-recommendations-templ.md)
for audience, ordering, and browsing behavior.

### Live Activity

Live activity retains its collapsed native disclosure above the workspace. Its
nested **Filter live activity** editor reuses account fields, checkbox rows and
plum Apply/quiet Reset actions for city/country and catalog event types. Confirmed
scope labels wrap in ordinary text; an empty match explicitly offers Reset without
widening automatically. Guest settings stay browser-local, while account settings
have equivalent native controls in Preferences. Applying filters replaces only the
live region, preserving event composers, drafts, focus and participation controls.
Happening Now puts its scope summary, the filter disclosure and the toast and Refresh ghost actions in one compact bar. Its heading carries a small uppercase **Live** pill (success green, its dot pulsing a ring outward with motion allowed) once a stream message has arrived, **Reconnecting** (warning colours) while a retry is scheduled, and nothing when stopped, paused or only polling over HTTP. Its word is read with the heading; the dot is decorative. Activity renders as compact cards (20rem grid minimum): a square thumbnail with the date chip, drawn from stored snapshots on the server render. Each card shows an initials circle with the first name's initial, the group count, or an empty dashed circle for Someone, plus attribution, the title link (stretched to cover the card) and city/time. Live-pushed cards use fallback art until the next render, and no reason is shown beside anonymous attribution. Empty Community and Happening Now states lead with a large `empty-display` line and the main action as a primary button. See [live filtering](docs/live-community-activity.md#city-and-event-type-filters-part-2).

### Event Discussions

Questions and replies use the event **note** shape (see Public Recommendations).
On phones, borrow YouTube's avatar, author, message, and action rhythm and
Reddit’s flat, divided discussion rows, not either app’s visual branding.
The list, opening post, and replies use the full content width with no enclosing
cards. Each question has one **Open conversation · N replies** link with a
trailing arrow on compact layouts. The composer starts as a ruled prompt; its
open form keeps a clear editing boundary. Wider layouts retain the bordered
reading groups. Dedicated threads keep event/date/venue context above the root.
One reply level names same-thread targets with a plum **In reply to**
link on the name line; public text preserves line breaks and the 70ch prose measure.
Each post keeps **Reply**, **Helpful**, and the root's **Same question** at hand.
A trailing native **More contribution actions** disclosure reveals the owner's
**Edit**/**Remove** or the **Report privately** link. An opened editor takes the
row's full width; closing More returns the reply composer without losing the edit
draft. Helpful keeps its full accessible name and 44px target; on phones it shows
the thumbs-up and count without a default outline. Selected toggles keep their
pale-plum state and pressed semantics. Removed contributions leave content-free placeholders. The
labeled composer discloses its public audience before the field, with a
solid-plum publish action and discard directly below; a reply target is quoted on a paper panel.
Long fields adapt to the visual viewport; pending fields remain selectable.
Explicit discard changes only the draft. Uncertain creation retries keep their
body/target/key immutable; confirmation selects the exact contribution. Standalone
same-thread reads/writes retain scoped drafts and control focus/reading position,
matching contextual discussions. Errors distinguish a failed refresh from a saved
post. See
[event discussions](docs/event-discussions.md). No new global tokens or assets.

Community **Conversations** and **Open questions** lead with the question and its author, then a compact event row (3.5rem thumbnail beside date, title and venue), then **Open conversation · N replies** or **Answer**. Wide columns keep bordered cards; below a 37.5rem Community container they become flat rows divided by rules. Price and sale facts stay on the event, not the conversation.

Private follow/unfollow uses a compact native disclosure after the conversation
root, with a labeled reply-frequency select for every new reply, UTC daily/weekly
summaries, or muted follows. Account Preferences exposes a native **Pause all
notifications** checkbox; pause notices keep that account-wide override explicit.
Followed discussions and the in-app reply inbox inherit account navigation and
flat, rule-separated, event-named records. Due updates show visible reply counts,
delivery time (a `layouts.Stamp`), and read state, never contribution excerpts or private reporting
details. **Open conversation at reply** selects the exact latest visible reply,
including one outside the current reply page; **Mark read** remains a separate
native form action. Controls and links work without JavaScript. No email or push
is sent. See [followed discussions](docs/followed-discussions.md).

Within discovery, compact event/date/venue context and price/sale facts stay above
questions or the selected root and replies. Tickets and Save are immediately
available, before artwork or extended content. Overview exposes description and
practical notices. Interested, Recommend and reminders form one plans group
directly under Tickets and Save, with one privacy line, before the section tabs;
without artwork, the tinted identity card leads on its own instead of overlapping
an empty band. **Event source & history** discloses provenance, with stale warnings kept
beside affected facts. Desktop **Open event page** and full-conversation links open
dedicated pages. Successful compact context omits event expansion because it is
already the primary event view; failed reads retain standalone recovery.
At the existing 62rem threshold, discussion shares the sticky context panel
alongside results. Panels and dedicated pages share one segmented navigation for
Overview, Questions & tips, and Recommendations. Links wrap by their content width
instead of squeezing longer labels into fixed segments. Selected roots/replies
retain community beside them when space allows, otherwise below. Overview panel
artwork caps at 16rem; standalone artwork caps at 22rem.
Below the context threshold, the selected event/thread becomes the
single primary view. URL selection, drafts, and return continuity remain in the
[responsive UX guide](docs/design-guidelines.md#discovery-to-discussion-journeys-and-shared-state)
and the discovery surface contract, not new global composition rules.

### Private Reporting and Moderator Review

Reporting inherits the event/date/venue heading, 70ch reading measure, native
selects/textareas and plum secondary action. Personal receipts/outcomes are flat,
rule-separated records with event names, contribution type/time, and direct links
to the affected contribution or withheld-content placeholder. A selected reply
outside the current reply page is shown separately, without duplicating its ID.
Current account-area destinations keep the section-link underline and `aria-current`.

The moderator queue orders contribution context, not report volume. Cases separate
current and report-time text, shared reasons and private notes, and author requests
and decision history. Native disclosures keep report evidence scannable; errors
retain escaped drafts. All controls remain usable without JavaScript, with 44px
selects and wrapped long/enlarged text. Affected authors cannot read their own case
evidence even with moderator membership; public hiding withholds text/attribution
and preserves replies. See [moderation](docs/moderation.md) and its
[surface contract](.impeccable/surfaces/views-home-moderation-templ.md). No new global
tokens, motion, typography, or raster assets are introduced.

### Cards / Containers

Event context and ticket information use white, quietly bordered containers with the shared radius. Context content has 1.25rem padding, reduced to 1rem for the selected compact view at the small-phone threshold; ticket panels use 1.5rem. Idle context uses a compact pale-plum ticket mark and practical guidance inside its single boundary. Loading context uses static blocks, `aria-busy`, and a status announcement. While Discover's **Load more events** request runs, three `aria-hidden` placeholder rows appear above the action. They follow the result list's container layout (stacked, artwork beside facts, or cards) and stay static pale-plum blocks in all motion modes; the button's "Loading events…" label remains the announced state. Errors keep retry/navigation available; stale preview/full-event data places a last-known provenance notice immediately before price and sale status, using the existing warning treatment rather than presenting stored availability as live.

Keep event price and sale status ahead of participation controls. Search and pagination retain ordinary links/forms without JavaScript. Event Community lists public recommendations separately from explicitly public participants, with profile links and ordinary pagination; private interest contributes to interest counts without naming anyone. Public profiles show opted-in interest and independently published recommendations, using quiet separators and ordinary event links. Discussion shows public questions and links dedicated threads with replies and Helpful. Opt-in Google account destinations provide profiles/privacy defaults, private preferences, durable private saves, independent event interest, public recommendations, and private personalized Radar; disabled deployments explain that accounts are not enabled. Explicit on-sale emails and in-app reply updates are implemented; additional sale/price/event-change emails and discovery digests are out of scope, and push is not implemented. Selection and return strategy remain in the linked UX guide and surface contracts.

Account forms inherit the same type, colors, corners, and controls. Each account page does one job: Profile, Privacy, Preferences, and Security & Apps are separate routes with one form per section. Profile and Privacy lead with a borderless plum-tinted summary (`color-mix` of `brand-soft` and `surface`; a transparent border survives forced colors): a Bricolage initial monogram, name, bio, and compact public-activity rows on Profile, and a "Who sees what" list on Privacy. The editable form sits beside it in an intrinsic grid when it fits and stacks below on compact/enlarged-text screens. `layouts.AccountHeader` gives every personal page one heading, at most one intro line, and family-local navigation: the current Account, Your events, Updates, or enabled Safety family as wrapping underlined tabs with `aria-current`, and each other family as one quiet chevron link on the same rule. Safety preserves report enablement and moderator role gates. Notes are .875rem, at most one per section. The account-wide sign-out is the only `.action-danger` (error fill) control, placed last behind a native disclosure that lists its effects. Google sign-in alone follows the provider-brand exception above; no new global tokens are introduced. See [accounts](docs/accounts.md) and the [account surface contract](.impeccable/surfaces/views-account-settings-templ.md).

Shared screen primitives ([app rework](docs/design/app-rework.md)): empty-state display lines stay below the page heading; every destination starts with `layouts.PageHeader` (one heading, at most one intro line, local navigation). `.summary-card` is the borderless plum-tinted statement of what something is; `.panel` is the white bordered surface for what you can do; never two of the same weight side by side. A page that couldn't do what was asked (404/405, account, event task, app connection and follow failures) uses `layouts.ErrorPage`: one heading, what happened, optional advice, then the way forward with the primary action first; the self-contained offline page repeats that shape. Confirmations use `layouts.SectionNotice` inside the section they confirm, and rare or destructive actions use `layouts.ConfirmDisclosure`, listing effects before the confirming button. Posted and updated times use `layouts.Stamp`: exact UTC without JavaScript; with it, viewer-local relative time within the last day and the exact local date and time otherwise, with the exact time in the title and for screen readers. `StampDate` always shows a date. Event start times stay venue-local. Each section has at most one explanatory note; `internal/htmlcheck` enforces it on the account settings pages, and each rework phase extends the check to its screens.

Community/public-profile artwork and titles share Overview destinations and return
context; event/date/venue lead attribution and text. Named recommendation/question/
thread links remain direct entries, without per-person event toolbars. Artist/venue
search is a native disclosure open for first use, submissions and errors, compactable
on revisits. Followed conversations expose actual counts and readable frequency;
notifications expose Available times (`layouts.Stamp`) and explicit read state. Open conversation
is primary, Mark read secondary. Publication audience sits beside the writing
action, with owner/report tools quieter. Expanded desktop navigation scrolls within
its region in short viewports rather than overlapping the footer. These bounded
extensions preserve the existing tokens, thresholds, flat surfaces, targets and
focus; see the [neighboring-screen record](.impeccable/surfaces/views-layouts-account-nav-templ.md).

Reminder settings use event-first identity, an Overview link, distinct public-sale
time, saved configuration and truthful readiness before the native lead-time
select. Custom minutes, inline validation, a rule-separated target preview and
Save/Stop use existing account primitives. Collections are flat, event-named rows
with account-zone target times and concise states; SES accepted is not confirmed
inbox delivery. Preferences separates Location & time zone from Notifications;
permission/pause labels and help form first-line-aligned checkbox rows with compact
parent-owned spacing and .875rem descriptions. Dedicated 44px links stay outside
prose so they do not enlarge text line boxes. A quiet rule separates in-app replies;
and optional email-delivery details keep caveats secondary. Out-of-scope digests
and change emails are not advertised. See [reminders](docs/reminders.md) and the
[reminder surface record](.impeccable/surfaces/views-account-reminders-templ.md).

[Radar](docs/radar.md) inherits event collection rows, with readable follow/category
reasons in the event copy before direct ticket and private Save actions. The header
keeps city/date scope, freshness and coverage explicit. Desktop Your space links
directly to Radar; compact screens reach it through Profile's account navigation,
without adding a fifth destination. Location setup, nearby starter suggestions,
no-match and changed-page recovery use the existing empty/error treatments and
native links. No design tokens, motion or shipping raster assets change; see the
[Radar surface contract](.impeccable/surfaces/views-home-radar-templ.md).

## Do's and Don'ts

### Do:
- Do use plum for actions, focus, and selection; light stone for navigation; near-white surfaces for reading; and dusty rose only for date indexes. Date shortcut chips are filters, so they take plum selection, not rose.
- Do keep real provider imagery, full readable dates, venue, price availability, and sale status connected to the event title.
- Do preserve the 62rem context/rail threshold and 72rem full-sidebar threshold when extending the shared shell.
- Do keep visible focus, wrapping titles, labeled controls, reduced motion, and ordinary link/form behavior.
- Do explain unavailable participation, unknown event facts, and stale provider data in text.
- Do keep sidebar editing explicit, cancellable, keyboard accessible, and truthful about browser-only persistence.

### Don't:
- Don't add cast shadows, glass effects, or raised hover cards to this flat, border-defined system.
- Don't force provider photographs into monochrome or substitute decorative stock imagery for event identity.
- Don't conflate private saves, event interest, recommendations, discussion posts, Helpful, private reports, or category preferences. Basic reporting/moderation, explicit-preference Radar and opt-in on-sale reminders are implemented; additional sale/price/event-change emails and digests are out of scope. Don't imply confirmed inbox delivery or public-pilot approval, fabricate participation counts, imply that interest/recommendation confirms attendance or ticket ownership, or imply that saving reserves tickets.
- Don't use promotional clutter or manufactured urgency to compete with practical event information.
- Don't promote route-specific composition or synthesized sidecar tonal ramps into new global design tokens.
- Don't reuse date paper for selected rows, current navigation, availability labels, caret, or focus.
