---
name: Ticketopia
description: "Local frequency: record-shop imagery, mixtape intimacy, and clear everyday event discovery."
colors:
  ink: "#29252d"
  muted: "#69616d"
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
  error: "#a22b39"
  error-bg: "color-mix(in oklch, var(--error) 6%, var(--surface))"
  warning: "#795318"
  warning-bg: "color-mix(in oklch, var(--warning) 6%, var(--surface))"
  success: "#286247"
typography:
  display:
    fontFamily: "Manrope, sans-serif"
    fontSize: "clamp(1.75rem, 1.5rem + .625vw, 2rem)"
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
    fontFamily: "Manrope, sans-serif"
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
  bottom-nav-current:
    backgroundColor: "{colors.brand-soft}"
    textColor: "{colors.brand}"
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
    backgroundColor: "{colors.brand-soft}"
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

**Creative North Star: "Local frequency"**

Local frequency makes event discovery feel personally assembled: the presence of a record shop, the intimacy of a mixtape, and the clarity of an everyday planning tool. Deep plum actions and pale-plum selection sit within light-stone navigation and near-white reading surfaces; compact dusty-rose date indexes give the collection a warm, human rhythm.

Bold, readable Manrope and real provider photography carry the personality. Familiar labeled controls keep everyday planning practical: useful dates, venues, prices, and clear availability matter more than promotional decoration. Density changes with the available space while the event identity stays recognizable.

**Key Characteristics:**
- Deep plum actions and pale-plum selection against light-stone navigation, near-white paper, and white surfaces.
- Dusty-rose date indexing and neutral, honest availability labels.
- One expressive, readable Manrope family with substantial heading weights.
- Record-sleeve provider imagery and compact, aligned event facts.
- Border-only depth, restrained state transitions, and visible keyboard focus.

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
- **Pale plum** (`brand-soft`): selected event rows, current navigation on every device class, secondary-action hover, image/preview fallbacks, static loading artwork, and browser text selection.

### Secondary
- **Dusty-rose date paper** (`date-surface`): compact date markers only, paired with ordinary ink. Its low-chroma rose tint harmonizes with plum selection and stone surfaces without competing with event imagery. It is not a selection, action, availability, or focus color.

### Neutral
- **Ink** (`ink`): headings, body content, default navigation text, and the collapsed-rail tooltip background.
- **Muted ink** (`muted`): venues, explanatory copy, source freshness, and unavailable controls.
- **Light stone** (`nav-surface`): desktop sidebar, intermediate rail, and neutral availability labels.
- **Hover stone** (`nav-hover`): default navigation and sidebar utility hover.
- **Near-white paper** (`paper`): page background, disabled selects, and light tooltip text.
- **White surface** (`surface`): compact header, bottom navigation, search/field surfaces, event context, tickets, and navigation links during customization; also light text on filled plum actions.
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
Date markers use ink, not muted text. Loading actions pair pale plum with plum
text rather than white.

## Typography

**Display and body font:** Manrope, with a sans-serif fallback. The variable font is self-hosted at `views/assets/fonts/manrope.ttf`, supports the shipped weights (400–800), uses `font-display: swap`, and is distributed with its SIL OFL license. There is no separate mono or display family.

**Character:** rounded, direct, and substantial. Tight heading tracking gives short titles confidence; ordinary casing and generous body leading keep long event information readable. The hierarchy is role-based, not a manufactured modular scale.

### Hierarchy
- **Display:** `typography.display` for page headings, fluid from 1.75rem to 2rem. Event-page headings use the separate `--text-event-heading` token, fluid from 1.5rem to 1.75rem. Both use rem bounds and rem + viewport interpolation rather than font-size breakpoint overrides.
- **Headline:** `typography.headline` for section and preview headings. Event-content section headings use 1.125rem; destination-state headings use 1.5rem.
- **Title:** `typography.title` for supporting headings; `typography.event-title` for result titles, with the more open leading needed by long names.
- **Body:** `typography.body` for the base reading size; `typography.body-small` for event descriptions. Observed reading measures are 65ch for introductions, 70ch for event prose, and 75ch for source notes.
- **Label / action:** `typography.label` for field labels and `typography.action` for primary/secondary controls. Labeled filter inputs remain at the body size with weight 500; search uses ordinary body weight.
- **Navigation:** `typography.navigation` for full navigation (weight 550), with `typography.navigation-current` for the current destination (weight 700). Group labels use `typography.navigation-group`; rail/bottom-nav labels use .6875rem and the same normal/current weights. Collapsed desktop labels remain accessible and appear as .75rem tooltips (weight 650). Event-section links use `typography.quiet-action`, rising to weight 800 when current.
- **Metadata:** `typography.metadata` for supporting facts and freshness; `typography.small-label` for date lines. Quiet participation uses `typography.quiet-action`, and neutral availability labels use `typography.availability`. Date numbers use `typography.date-number`; month labels use weight 750. Date markers and event-date lines use tabular numerals.

Themed library primitives retain their own utility leading where the app has not overridden it: navigation, quiet buttons, and section links use the `text-sm` ratio (`calc(1.25 / .875)`); availability and sale-status badges use the `text-xs` ratio (`calc(1 / .75)`); search/filter input text uses 1.5. These are observed inherited roles, not a universal 1.6 line-height applied to every control. Plain date lines and sidebar customization labels retain the body leading.

Headings balance lines, prose uses pretty wrapping where supported, and inherited
`overflow-wrap: break-word` handles long provider names without shrinking
min-content-sized boxes to a single letter. Flex/grid items can shrink with
`min-inline-size: 0`. Manrope's real variable weights are used without synthetic
bold/italic; font smoothing is explicit. Smaller metadata supports the event title
and essential facts rather than replacing them.

**The One Family, Clear Roles Rule.** Use Manrope's size, weight, leading, and spacing to distinguish headings, facts, and controls; keep dates tabular and long event names able to wrap.

**Provider-brand exception:** only the Google sign-in CTA uses self-hosted Google
Sans Medium (500) at .875rem/1.25rem, following Google's branding requirements.
Manrope remains the family for all surrounding account and application UI.

## Layout

The centered application shell stops at 100rem and fills at least the small
viewport height (`100svh`), avoiding document relayout as mobile toolbars move.
Main content uses normal document scrolling. Fixed-height sidebar UI still uses
`100dvh`. Reused spacing primitives are the source's `space-1` through `space-7`;
component-specific insets remain explicit rather than becoming another scale.

| Threshold | Shipped behavior |
| --- | --- |
| Base, below 40rem | Single-column image-led rows; content padding 1.5rem 1.25rem. Light compact header and four-item fixed bottom navigation. Workspace reserves `calc(5.5rem + env(safe-area-inset-bottom))`. |
| At most 23rem | Content padding becomes 1.25rem 1rem; fields form one column. Search loses its decorative icon and tightens its inset; event headings and section links reduce to fit. |
| At least 40rem | General page padding becomes 2rem. Result rows use an 8rem image column and 1.25rem gap; images become square and date markers become horizontal strips below them. |
| At least 62rem | A fixed 5.5rem light-stone navigation rail replaces the bottom bar; the light header remains. Discovery gains a 20rem context column with 1.5rem gutter/padding; result images use a 6.5rem column and 1rem gap. Dedicated event pages gain an 18rem ticket column with a 2rem gap. |
| At least 72rem | A sticky labeled sidebar replaces the fixed rail/header, occupying 14rem expanded or 4.5rem collapsed. Discovery context becomes 21rem with 2rem gutters/padding; event/destination page padding is 2rem 2.5rem, with destination top padding increased to 3rem. |
| At least 86rem | Result image columns return to 8rem with 1.25rem gaps. The filter grid becomes three columns, with the city field spanning two. |

The CSS and JavaScript share the 62rem context and 72rem desktop-navigation thresholds. Selection, direct-entry, return-state, and resize strategy belong to the [responsive UX guide](docs/design-guidelines.md#discovery-to-discussion-journeys-and-shared-state) and [surface contract](.impeccable/surfaces/views-home-index-templ.md), not additional global design rules.

The context panel is sticky from 62rem (top 1.5rem), with `max-height: calc(100dvh - 3rem)`, vertical overflow, and a stable scrollbar gutter; it is keyboard focusable. The full sidebar is one dynamic viewport tall, with grouped navigation and a footer pushed to the bottom. During customization its navigation region scrolls independently, keeps a stable scrollbar gutter, and becomes keyboard focusable so tools remain reachable at small viewport heights. Ticket panels remain in the page layout. Filters use two equal columns and a 1rem gap until the small-phone or wide-screen adjustments apply.

## Elevation & Depth

The normal interface has no cast shadows. Depth comes from white against near-white paper and light stone, one-pixel quiet borders, stronger control borders, and pale-plum selection. The selected row stays in the list's geometry. Fixed navigation, sticky context, and collapsed-rail tooltips are functional layers, not floating or glass surfaces.

Focus is an outline, not a shadow: a solid plum ring (3px) offset from the target (4px), including navigation and sidebar controls. Themed library focus rings and card shadows are explicitly suppressed.

The source does use `box-shadow` for drag insertion markers: `inset 0 2px 0 var(--brand)` before a target and `inset 0 -2px 0 var(--brand)` after it. These are functional two-pixel rules inside a reorder target, not elevation or cast depth; the dragged item/section also dims to opacity .55.

**The Border-Only Depth Rule.** Separate surfaces with tone, spacing, and borders; selection changes the fill without lifting the component or adding a cast shadow.

## Shapes

Use modest, role-specific corner treatments: the shared `radius` for context/ticket containers, selected rows, and the search surround; `control` and `navigation` for buttons, fields, and links; `image` for photographs; and the smaller `date-marker` and `availability` corners for indexing and state. Sidebar move/drag tools use `sidebar-move`, while collapse, commit controls, and tooltips use `sidebar-utility`. These are rounded rectangles, not a universal pill vocabulary. Unselected result rows are separated by a top rule rather than enclosed in individual cards.

Some observed component-local corners are intentionally outside the reusable scale: the search input is .2rem, filter summary .3rem, preview artwork .6rem, and the library's inner empty-context Card uses its derived `radius-xl` (1.05rem) within the outer .75rem context boundary. They are real roles, not a mandate to homogenize radii or invent new primitives for every measurement.

Event imagery fills a clipped frame with `object-fit: cover`. The default frame is 16:9; list images become square at the row-image threshold. Full-event images cap their height at 22rem, and a compact selected preview caps it at 16rem. Preserve provider color and content; the reviewed monochrome photographs are source material, not a prescribed grayscale filter.

Icons are outline SVGs with a 24-unit viewBox, rounded caps/joins, and stroke width
1.7. Label-adjacent icons use font-relative em sizing and cannot shrink; larger
decorative/fallback icons retain explicit rem sizes. The fallback uses a music
icon for classified music events and a ticket icon otherwise. It occupies the
same image frame, so a failed image does not erase the event's place in the list.

## Components

### Buttons

Compact, confident, plainly labeled controls. Primary actions pair plum with white text; secondary actions use white, plum text, and a control-line border. Both use the frontmatter's action typography and padding with a minimum height of 2.75rem (44px), not a fixed height that clips wrapping labels. Minimum heights, borders, focus outlines, and transitions live in the sidecar snippets because the frontmatter's component schema cannot express them accurately. Hover darkens primary actions and gives secondary actions a pale-plum fill. Pressed primary actions use `brand-active`; there is no pressed translation or raised effect.

Quiet event controls use a transparent fill, quiet border, quiet-action typography, and the same minimum target height. Recommend is a secondary link that opens the native Community composer; Save, Interested, and Recommend explain account-disabled or unavailable states rather than simulate success. Underlined text links offer navigation and small utility actions; the preview-close icon has a 2.75rem target.

Private Save / Remove from Saved controls are implemented when opt-in accounts are enabled. They inherit the quiet control's type, corners, target height, and plum focus outline; a saved event adds pale-plum fill with plum text and border, while hover uses pale plum and dark-plum text. Signed-out visitors get a Sign in to save link; disabled deployments explain the account requirement, and unavailable saved state is labeled rather than guessed. Ordinary POST forms work without JavaScript. Enhanced writes show pending, success, or retry feedback below the shared controls row and synchronize visible row/preview state only after confirmation. Feedback never participates in button alignment or wrapping; controls align at their top edge when labels wrap. Failed writes retain the prior state. Removing from the Saved collection leaves a named confirmation and restores focus to a remaining event's removal control, or the empty-state Discover action.

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

Action background changes, sidebar width changes, and disclosure-chevron rotation
use 160ms; result selection uses 180ms. Buttons and badges share the source's
`cubic-bezier(.16, 1, .3, 1)`; other library buttons retain 150ms for color/fill
changes only. Geometry transitions opt into motion only when no reduction is
requested. The reduced-motion safeguard also disables vendored animations and
smooth scrolling. Hover requires a fine, hover-capable pointer; touch press
feedback changes fill/color without movement. No loading shimmer ships.

### Inputs / Fields

White, clearly bounded, ordinary fields. Search combines an SVG icon, labeled search input, and submit action inside one control-line surround. The text input can shrink without displacing the action. Labeled filters use a minimum height of 2.875rem, body-sized text, and visible control borders; disabled genre selects use paper and muted ink. Native `details`/`summary` discloses filters and extra event metadata. The shared focus outline remains visible; field errors use explanatory text and the error-state container rather than an invented field-specific color system.

Category and genre use the same native select treatment. All categories disables genre; changing category clears the previous genre and offers compatible options. The ordinary GET form supports the same workflow without JavaScript by applying category before selecting a genre. Result rows, previews, and event pages show supplied category/genre metadata with an explicit unknown fallback.

### Navigation

Quiet, grouped icon-and-text navigation. Expanded desktop navigation uses light stone, a quiet right border, ink labels (weight 550), muted outline icons, hover stone, and pale-plum current state with plum text/icons (weight 700). Browse groups Discover and Community; Your space groups Saved and Interests. Profile remains fixed in the bottom footer. Links and utility/move controls preserve 44px targets.

Desktop collapse hides visual labels without removing their accessible names. CSS shows a compact label tooltip on both hover and keyboard focus: ink background, paper text, no cast shadow. Group labels and customization controls hide while collapsed. The intermediate fixed rail and compact four-item bottom navigation keep a fixed order: Discover, Saved, Community, Profile. Bottom navigation uses white with a quiet top border and icon-over-label layout; both compact layouts share the pale-plum current state.

Explicit customization reveals section/link drag handles and Move up / Move down controls; links use white draft surfaces. Sections can move, links stay within their section, and Profile stays fixed. Done commits the draft; Cancel or Escape restores the previous order; Reset to default changes only the cancellable draft. Collapse is disabled while editing; leaving the desktop threshold cancels unfinished editing. Preferences are browser-only, not account persistence or cross-device sync. Storage failure leaves current-page controls usable with honest feedback. Without JavaScript, the expanded default navigation remains and enhancement-only tools hide. See the [sidebar preference contract](docs/design-guidelines.md#sidebar-preferences) for behavior details.

Event sections remain real links with a quiet baseline; the current section adds a plum two-pixel bottom border and heavier text. Links wrap onto another row when the available width or enlarged text requires it. Current destinations use `aria-current`, not partial ARIA-tab semantics.

### Availability Labels

Small neutral rectangles with light-stone fill, muted text, a quiet one-pixel border, and availability corners explain “In development” or a specific unavailable capability. They are informational labels, not selectable filter chips. Do not give them date-marker styling or turn them into a success confirmation.

### Indexed Event Rows

The signature pairs provider photography with compact dusty-rose date indexing. Rows lead with local show date, event title, and venue, followed by sale status and known price or an explicit unknown. Their selected state is pale plum; the title and Details remain real event links. The dusty-rose date marker overlays the lower-left image corner on compact rows, then becomes a horizontal index beneath square artwork. Its short date is decorative beside the full readable date; unknown dates say TBA.

The private Saved collection reuses these rows, ordered newest saved first, with ordinary pagination and event links that retain the return page. Its privacy statement and last-known-details guidance distinguish durable saves from public interest and current ticket availability; Save and Interested remain independent controls on the shared rows. The own Interested collection uses the same rows, newest interest first, above private category preferences on `/me/interests`. Its event links and participant pagination preserve the originating collection page. Empty collections lead back to Discover. See [saved events](docs/saved-events.md), [event interest](docs/event-interest.md), and the [Saved surface contract](.impeccable/surfaces/views-home-saved-templ.md) for feature behavior.

Photos are live external Ticketmaster assets retained in normalized `Event.Images`; presentation chooses an available width close to the requested size. No authored shipping raster belongs to this system; the sole local provider raster is Google's unmodified gradient G for sign-in. Local styles, JavaScript, htmx, fonts, and that provider logo are Go-embedded assets; SVG supplies the app icons. The sidecar demonstrates the shipped image fallback rather than copying provider photography into documentation assets.

### Public Recommendations

The composer inherits a bordered, plum-text native disclosure and the white account-form textarea, with the public audience explained before the optional reason. Publish/save use secondary actions; withdrawal is an underlined utility action. Ordinary POST forms remain usable without JavaScript. Enhancement dims pending forms, preserves drafts and confirmed content on failure, and closes the editor with focus restored to its summary after confirmation.

Event, profile, and Community lists share flat, rule-separated attribution and reasons rather than raised social cards. Collection entries retain image/date indexing and essential event facts ahead of the linked author; event-local lists omit duplicate event artwork. Reasons preserve line breaks, wrap long text, and use the existing event-prose measure (70ch); publication and edit metadata stay secondary. See [public recommendations](docs/event-recommendations.md) and the [recommendation surface contract](.impeccable/surfaces/views-home-recommendations-templ.md) for audience, ordering, and browsing behavior.

### Event Discussions

Questions and replies inherit Manrope, plum actions, native disclosures, and flat
rule-separated reading surfaces. Dedicated threads keep event/date/venue context
above the conversation. One indented reply level names same-thread targets;
public text preserves line breaks and the 70ch prose measure. Helpful uses a
44px secondary button, with pale-plum selected state and an inline post-level count.
Removed contributions leave content-free placeholders. The labeled composer
discloses its public audience; errors preserve drafts and confirmed state. See
[event discussions](docs/event-discussions.md). No new global tokens or assets.

Within discovery, compact event/date/venue context and price/sale facts stay above
questions or the selected root and replies. A native **Event details & actions**
disclosure contains imagery, participation, ticket links, and source detail rather
than leading the panel with artwork. Full-event and full-conversation links open
dedicated pages. At the existing 62rem threshold, discussion shares the sticky
context panel alongside results; below it, the selected event/thread becomes the
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

Event context and ticket information use white, quietly bordered containers with the shared radius. Context content has 1.25rem padding, reduced to 1rem for the selected compact view at the small-phone threshold; ticket panels use 1.5rem. Empty context uses a pale-plum ticket placeholder and practical guidance. Loading context uses static blocks, `aria-busy`, and a status announcement. Errors keep retry/navigation available; stale preview/full-event data places a last-known provenance notice immediately before price and sale status, using the existing warning treatment rather than presenting stored availability as live.

Keep event price and sale status ahead of participation controls. Search and pagination retain ordinary links/forms without JavaScript. Event Community lists public recommendations separately from explicitly public participants, with profile links and ordinary pagination; private interest contributes to interest counts without naming anyone. Public profiles show opted-in interest and independently published recommendations, using quiet separators and ordinary event links. Discussion shows public questions and links dedicated threads with replies and Helpful. Opt-in Google account destinations provide profiles/privacy defaults, private preferences, durable private saves, independent event interest, public recommendations, and private personalized Radar; disabled deployments explain that accounts are not enabled. Notification delivery remains planned. Selection and return strategy remain in the linked UX guide and surface contracts.

Account forms inherit the same type, colors, corners, and controls. A separate white, bordered public preview sits beside profile editing on wide screens and stacks below it on compact screens. Real Profile/Preferences/Interests links use the section-link current state. Security follows the form; native disclosure keeps API controls secondary. Google sign-in alone follows the provider-brand exception above; no new global tokens are introduced. See [accounts](docs/accounts.md) and the [account surface contract](.impeccable/surfaces/views-account-settings-templ.md).

[Radar](docs/radar.md) inherits event collection rows, with readable follow/category
reasons in the event copy before disclosure and participation actions. The header
keeps city/date scope, freshness and coverage explicit. Desktop Your space links
directly to Radar; compact screens reach it through Profile's account navigation,
without adding a fifth destination. Location setup, nearby starter suggestions,
no-match and changed-page recovery use the existing empty/error treatments and
native links. No design tokens, motion or shipping raster assets change; see the
[Radar surface contract](.impeccable/surfaces/views-home-radar-templ.md).

## Do's and Don'ts

### Do:
- Do use plum for actions, focus, and selection; light stone for navigation; near-white surfaces for reading; and dusty rose only for date indexes.
- Do keep real provider imagery, full readable dates, venue, price availability, and sale status connected to the event title.
- Do preserve the 62rem context/rail threshold and 72rem full-sidebar threshold when extending the shared shell.
- Do keep visible focus, wrapping titles, labeled controls, reduced motion, and ordinary link/form behavior.
- Do explain unavailable participation, unknown event facts, and stale provider data in text.
- Do keep sidebar editing explicit, cancellable, keyboard accessible, and truthful about browser-only persistence.

### Don't:
- Don't add cast shadows, glass effects, or raised hover cards to this flat, border-defined system.
- Don't force provider photographs into monochrome or substitute decorative stock imagery for event identity.
- Don't conflate private saves, event interest, recommendations, discussion posts, Helpful, private reports, or category preferences. Basic reporting/moderation and explicit-preference Radar are implemented; reminder delivery remains planned. Don't imply public-pilot approval, fabricate participation counts, imply that interest/recommendation confirms attendance or ticket ownership, or imply that saving reserves tickets.
- Don't use promotional clutter or manufactured urgency to compete with practical event information.
- Don't promote route-specific composition or synthesized sidecar tonal ramps into new global design tokens.
- Don't reuse date paper for selected rows, current navigation, availability labels, caret, or focus.
