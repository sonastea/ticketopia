# App-wide rework: one job per screen

**Status: approved, in progress (2026-11-19).** Phases are implemented and
committed in order; each phase's section notes when it ships. Until then a
section describes intended work, not the app.

## Why

The account rework (`4f765d5`) took `/me` from a 22/40 critique, 15 notes and a
1,678px desktop page to four focused pages, with Profile now about one screen
tall. People liked the result. The rest of the app has the same habits that made
`/me` feel cluttered, and it feels inconsistent next to the new account pages.

This plan applies the same philosophy to every screen. It keeps the visual world
in [DESIGN.md](../../DESIGN.md) (plum, light stone, Bricolage/Manrope, dusty-rose
date stubs, flat surfaces) and adds no new tokens. It changes structure, density
and consistency, not identity.

[PRODUCT.md](../../PRODUCT.md) records the approval: broad in scope, shipped as
small, independently releasable phases.

## The design philosophy

These rules were proven on the account pages. Each phase applies them to its
screens.

1. **One job per screen, one form per section.** When a screen does several
   jobs, split it, or order its sections by the decision a person is making.
   Each form saves one thing.
2. **Lead with what it is, then what you can do.** A borderless plum-tinted
   summary (the profile card treatment) states identity or status. A white,
   bordered panel holds editing or actions. Never two of the same weight side by
   side.
3. **A notes budget.** At most one `.field-note` per section, at .875rem.
   Privacy, provenance and freshness are stated **once per page or list**, not
   once per item. A repeated caveat becomes a list-level line or a "Who sees
   what" style summary.
4. **One header pattern.** Every destination starts with a heading, at most one
   intro line, and its local navigation, as `layouts.AccountHeader` does now.
   This becomes a general `layouts.PageHeader`.
5. **One return path.** A screen has a single return or breadcrumb line, never
   two or three stacked return links.
6. **Rare and destructive actions last.** Edit, remove, report and sign-out
   actions sit at the end or behind one disclosure. A destructive action names
   its effects before it runs.
7. **Confirmations next to the thing confirmed.** Status messages appear in the
   section that changed, not at the top of the page.
8. **Cards for browsing, rows for summaries.** Browse lists use one event card.
   Collections and side panels use one compact event row. Each shows a fact
   once.
9. **Everything still works natively.** No-JS forms, native disclosures, 44px
   targets, visible focus, forced colours, reduced motion and 200% text are
   acceptance criteria, not polish.

## What the current app looks like

Measured on the baseline review scenario on 2026-11-19, signed in, as page
height in pixels at 1440px desktop and 390px phone widths.

| Screen | Desktop | Phone | Notes | Main problems |
| --- | ---: | ---: | ---: | --- |
| Discover `/` | 2,465 | 3,220 | 2 | The date appears three times per card; "Public sale" shows on every card, even when already on sale; two stacked full-width buttons per card; uneven card heights; the preview panel holds two generic messages |
| Event overview | 1,297 | 1,627 | 3 | A tall empty tinted band when there is no image; the sale line floats outside the hero; **Your plans & community** (Interested, Recommend, Reminder) sits below the section content, after the discussion |
| Event: Recommendations / Q&T | 1,519–1,670 | 1,897–1,973 | 7–8 | Privacy and "not live chat" explanations in each section; a long intro before the first post |
| Conversation thread | 1,712 | 1,969 | 9 | Three return links stacked (Discover, Back to Questions & tips, Event Overview); separate Edit and Remove disclosures under each post; UTC timestamps |
| Community | 1,383 | 2,601 | 2 | A freshness warning box on every card; attribution split over three lines; two links to the same place ("Recommendations", "See all recommendations") |
| Happening Now (empty) | 901 | 901 | 10 | The empty state is a display-size heading bigger than the page title; 10 notes for 64 words |
| Radar | 2,098 | 2,865 | 6 | ISO dates in the scope line; three stacked status notes before the first card; per-card "Updated…" repeated; the sale line is larger than the title |
| Saved | 1,477 | 1,963 | 1 | The same card problems as Discover |
| Agent drafts | 1,372 | 1,971 | 10 | A note per draft field |
| Agents guide `/agents` | 3,280 | 4,647 | 0 | 850 words with no "On this page" navigation |
| Moderation queue | 901 | 953 | 1 | Fine, but uses its own heading pattern |

Six different event presentations exist today: the Discover card, Community
card, Radar card, collection row (`EventRows`), public profile row and the new
compact profile row. They show different subsets of facts in different orders.

## The plan

Seven phases. Each phase is one reviewable change: a critique, then build, then
reviewer, then browser verification, as with the account pages. Order is by user
value: decide, then discover, then talk, then browse community, then personal
collections.

### Phase 0: Shared foundations

**Shipped 2026-11-19.** `PageHeader`, `SectionNotice`, `ConfirmDisclosure`,
`Stamp`/`StampDate`, `.summary-card`/`.panel` and the notes-budget check exist;
posted/updated times across the app use `Stamp`. `EventCard` and `EventRow` are
built in Phases 1 and 2, where their first screens adopt them, rather than ahead
of any caller.

No visible redesign on its own. It gives later phases the same parts.

- **`layouts.PageHeader(title, intro)`.** It generalizes `AccountHeader`; local
  navigation (account families, Community tabs, an event return line) is passed
  as its children.
- **Event presentation, two components.**
  - **`EventCard`** (browse) has artwork with the date stub, title, a venue
    line, one fact line (price · on-sale state · category) and one action row.
    The row shows **Tickets** as the primary text action and **Save** as an icon
    toggle with a visible label on wide screens. Sale timing appears only when
    the sale is in the future.
  - **`EventRow`** (compact) has small artwork, the date, title and venue, plus
    one caller-supplied line ("You recommended this", "Why: in your city").
  - Discover, Saved, Radar, Interests, Community and profiles all move to these.
- **`SummaryCard` and `Panel` classes.** The tinted summary and the bordered
  panel from Profile and Privacy become shared, documented classes. They replace
  `.profile-card`, `.privacy-summary` and `.profile-editor` one-offs.
- **`ConfirmDisclosure`.** The "Sign out of all devices" pattern (summary, list
  of effects, danger button) becomes a reusable templ component for removals and
  withdrawals.
- **Section notices.** The `NoticeFor` pattern becomes a helper any page can use.
- **Time display.** `layouts.Stamp` and `StampDate` render posted and updated
  times. Without JavaScript they read as exact UTC; with it, viewer-local
  relative time within the last day, otherwise the exact local date and time.
  They replace scattered "· 08:19 UTC" strings.
- **Checks.** A test that counts `.field-note` per section in rendered pages, to
  hold the notes budget.

### Phase 1: Event page (the decision screen)

**Shipped 2026-11-19.** Plans (Interested, Recommend, reminder) sit under
Tickets and Save with one privacy line, on the page and in the preview; an
upcoming public sale moves into the identity facts and past ones are dropped;
without artwork the identity card leads alone; section and composer notes fit
the budget, which a test now enforces on every event section. The desktop
overview went from 1,297px to about 1,140px. A single quiet button group for all
five actions was not adopted: Tickets and Save keep their compact sticky bar on
phones, so plans stay a second row.

Files: `views/home/event.templ`, `recommendations.templ`, `discussions.templ`
(section parts), `more_events.templ`.

- **Hero.** When there is no image, the tinted band shrinks to the summary card
  itself; there is no empty band above it. The sale timing moves into the hero
  fact line.
- **One action cluster** directly under the hero:
  - **Tickets** (primary).
  - **Save**, **Interested**, **Recommend** and **On-sale reminder** as one
    quiet button group.
  - Visibility ("Interested · Public") stays on its button.

  This replaces the separate **Your plans & community** block after the section
  content.
- **One privacy line for the whole cluster**, replacing the per-control notes:
  "Saves and reminders are private; Interested is your choice; recommendations
  are public."
- **Sections (Overview, Questions & tips, Recommendations)** keep in-place tabs.
  Each section intro becomes one line, and the composer explanation moves next
  to the composer.
- **Event source & history** stays a closing disclosure.
- **Target:** overview at about one desktop screen, and no section above 4
  notes.

### Phase 2: Discover

**Shipped 2026-11-19.** Result rows (shared by Discover, Saved, Radar and
Interests) show **Tickets** and a Save/Saved toggle on one row, keep the
provider and the action in the accessible names, and show a public sale only
while it is ahead. The empty preview is one prompt. Six results went from
2,465px to 2,309px on desktop and from 3,220px to 2,908px on a phone. That
misses the 1,700px/2,300px target: what remains is artwork-led card height,
and shrinking the artwork would change the visual identity this plan keeps.
`EventCard`/`EventRow` stayed one shared row component (`EventRowsContent`),
which every collection already uses, rather than a new pair.

Files: `views/home/index.templ`, `more_events.templ`, preview panel parts.

- Results use `EventCard`. The readable date shows once beside the decorative
  stub. Only upcoming sales
  show their time. One action row replaces the two stacked buttons.
- The "Updated … Venue-local event times." line stays once, above the list.
- **Preview panel.** Before a selection it shows one short prompt, not two
  generic messages. After a selection it uses the Phase 1 event summary and
  action cluster, so the panel and the event page match.
- The Ticketmaster disclaimer stays in the footer, once.
- **Target:** a desktop page with 6 results drops from 2,465px to about
  1,700px, and the phone from 3,220px to about 2,300px, without hiding any
  decision fact (date, venue, price, availability, ticket link).

### Phase 3: Conversations

**Shipped 2026-11-19, with changes to the plan below.** The return links share
one line; Follow and **Your reply notifications** share one row (the inbox
link stays outside the disclosure, where it is reachable without opening it);
and the composer's reply-target and audience notes are one. Report stays a
visible link rather than moving behind a "More" disclosure: it is a single
action, and hiding it would add a step to private reporting. The owner's Edit
and Remove disclosures stay below the action row: moved into it, opening one
moved its own summary onto a new line, and no native layout keeps a
disclosure's trigger in place while its content spans the row.

Files: `views/home/discussions.templ`.

- **One return line**: "← A good night in the city · Questions & tips". It
  replaces the three stacked links. The event title and date stay as context.
- **Follow** stays a single button with its frequency. "Your reply
  notifications" moves into the follow button's disclosure.
- **Per-post actions.** Reply and Helpful stay visible. **Report**, **Edit** and
  **Remove** move into one "More" disclosure per post, with Remove using
  `ConfirmDisclosure`. The owner sees Edit and Remove; others see Report.
- Timestamps use `layouts.Time` ("2 hours ago", with the full time on the
  `time` element).
- The composer note shrinks to one line: "Your name and reply are public."
- **Target:** under 5 notes per thread page, and one visible action row per
  post.

### Phase 4: Community, Conversations, Happening Now

Files: `recommendations.templ`, `discussions.templ` (lists),
`happening_now.templ`, `live_filters.templ`.

- **Community cards** use `EventCard` with a community slot: the strongest
  reason as a quote, then "Riley and 1 other recommend this", with one link to
  all recommendations.
- The **last-known freshness warning** shows once above the list when any item
  is stale. A single small marker stays on the stale card.
- **Happening Now.** The empty state uses a normal h2-size heading. Snapshot and
  connection notes merge into one status line. The filter bar keeps its three
  controls.
- All three tabs use `PageHeader` with the Community tab nav.

### Phase 5: Personal collections

Files: `saved.templ`, `radar.templ`, `interests.templ`, `follows.templ`,
`account/reminders.templ`, `account/drafts.templ`, `discussion_follows.templ`.

- Saved, Radar and Interests use `EventCard` or `EventRow` consistently. Radar's
  reason ("Why this event") becomes the row's one caller line.
- **Radar status.** The scope line uses friendly dates. The three status notes
  collapse into one status line with a single disclosure for "Why these
  results".
- **Drafts.** One note per draft at most. Publish and Discard stay; the
  replaced-reason warning moves beside Publish.
- **Reminders, follows, followed discussions and notifications** get the same
  row grammar: title link, one meta line, primary verb, quiet secondary verb.

### Phase 6: Public and edge screens

- **Public profile `/users/{id}`** mirrors the new `/me` card (initial, name,
  bio) and lists public activity with `EventRow`.
- **Sign-in**: one paragraph and one privacy note instead of two.
- **404, offline and failure pages** share one small error layout.
- **`/agents`**: an "On this page" nav and the shared header. When signed in,
  it links to Security & Apps.
- **Consent page**: one statement of what the app can do, using the
  Who-sees-what pattern for "Can / Can't".

### Phase 7: Moderation

The review queue and case pages move to `PageHeader` and the row grammar, and
case actions use `ConfirmDisclosure`. This is a lower priority, because it is
used by few people and already works.

## What does not change

- Routes, the JSON API, MCP tools, permissions, privacy semantics and data.
  This work is presentation-only. Any route change gets a redirect.
- The visual world: palette, fonts, radii, date stubs, the shell (sidebar,
  rail, floating phone navigation), motion tokens. No dark mode, no new tokens.
- Native-first behavior: every form and disclosure keeps working without
  JavaScript.

## How each phase is verified

- An impeccable critique before and after, with the score recorded in
  `.impeccable/critique/`.
- Page height and notes count measured on the same review scenario as the table
  above, desktop and phone.
- The `reviewer` subagent on the diff, then fixes, then re-verification in a
  real browser: desktop and phone, keyboard, reduced motion, forced colours,
  JavaScript off and 200% text. Automated axe checks are extended to the
  phase's screens.
- `go vet ./...`, `go test -race ./...` and `make check` before each commit.
- Docs: DESIGN.md, design-guidelines and a `docs/changes/` entry per phase.

## Risks

- **Fewer words, missing facts.** Cutting notes must not remove required
  disclosures, such as privacy, ticket availability caveats and last-known data.
  Each removed note is either merged into the page-level statement or kept.
- **Moving actions changes muscle memory.** The event action cluster is the
  biggest move. Phase 1 should be checked against the existing sign-in
  continuity and draft-safety journeys (`scripts/*-browser.cjs`).
- **Browser scripts and tests depend on text and structure.** Each phase
  updates its scripts in the same change, as the account phase did.
- **Local time display** must not mislabel venue-local event times. Event start
  times stay venue-local; only "posted" and "updated" stamps become
  viewer-local.

## Decisions

Approved 2026-11-19:

1. The broader scope, recorded in PRODUCT.md.
2. Phase order as listed: shared foundations, then the event page.
3. Save is an icon toggle whose label shows on wide screens; its accessible name
   always says Save or Remove from Saved.
4. Posts and "updated" stamps show viewer-local relative time with the exact
   time available; event start times stay venue-local.
