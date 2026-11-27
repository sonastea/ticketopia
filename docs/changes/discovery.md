# Event discovery changes

[Older history](archive/discovery/).

## Unreleased

### 2026-11-27 — Los Angeles area, one row per run, no ticket add-ons

- Pre-pilot fixes from the [pilot-city plan](../design/pilot-city-relevance.md).
  A Los Angeles search on the web now covers the Los Angeles area (34 venue
  cities from Hollywood and Inglewood to Long Beach and Orange County), with
  **Only Los Angeles** to narrow it. Each member is collected as its own city
  scope; the area is covered only when every member is. Radar reads the same area.
- The web lists each run (the same name at the same venue, such as a theatre
  run or 15-minute entry slots) once, at its first date in the range, with
  "Also on 11 more dates, through …". The event page lists **All dates**, each its
  own event. In production LA data this turns 862 listings in a month into 280 rows.
- Ticketmaster's Upsell listings (seat upgrades, viewing decks) no longer list
  in stored Discover, including the JSON API and agents, or Radar.
- The JSON API and `find_events` keep exact cities and unfolded listings unless
  asked (`area=metro`, `fold=run`); see [discovery](../discovery.md#metro-areas-and-runs).
- Review fixes: only due members refresh, a cold area answers once any member has
  results, runs compare titles exactly, placeholder times never show as
  showtimes, started dates drop from **All dates**, and database-free Discover
  hides the area choice.

### 2026-11-23 — Discover holds up under load as the catalog grows

- Production overloaded between 100 and 150 Discover pages a second (480 on
  2026-11-10): each city search read every stored snapshot, in every city,
  twice per page (page and count), so MariaDB hit its two CPUs as the catalog
  grew to about 10,000 events. City and country searches now start from that
  place's rows, reading about a quarter as many in production, with identical
  results ([stored Discover](../event-history.md#stored-discover-and-coverage-aware-reads)).
- Identical stored reads are shared for 10 seconds per replica, with
  concurrent ones coalesced; a catalog write in the same replica ends sharing
  at once.
- The query benchmark gains a minority-city case
  ([query performance](../query-performance.md#timing-boundaries-and-interpretation)).
- Radar and on-demand collection's omission check use the same city-first
  search. Provider request accounting no longer expires Discover's cached
  counts and shared reads, and waits for the budget row one at a time per
  replica instead of each holding a database connection.
- Event-card and recommendation previews rank keys only and read text and
  authors for the winners (previews 21% faster in the benchmark).

### 2026-11-22 — Every card quotes a recommendation reason

- Discover and Radar cards quote their event's newest public recommendation
  reason (two lines; the Up next lead keeps three). Saved, Interests and other
  compact rows don't. See [DESIGN.md](../../DESIGN.md#indexed-event-rows).

### 2026-11-21 — Agenda Discover, one Tickets button and shared event cards

- Date-sorted Discover reads as an agenda: visual headings open each stretch
  (Today, This week, Next week, Later in {month}, then each month) in the
  browsed city's calendar, and the soonest event on the first page leads as a
  wide **Up next** card (known, non-past date; full-width artwork in narrower
  lists, with its own image sizes). Pages share the city's day. Load more drops a
  repeated heading and focuses the first new event. Name sort stays one list.
- Result cards (Discover, Saved, Interests, Radar) share the Community cards'
  anatomy: full-bleed artwork, raised surface, a hairline action bar. Tickets is
  the event page's gradient button everywhere (rows rest without the coloured
  lift); Save carries the same ring as on the event page. In narrow rows with
  enlarged text, Tickets drops its icon rather than breaking the word.
- Row activity links became count chips (questions, recommendations); live
  count updates rewrite only the chip's label. The preview's Open event page
  and Happening Now's toast/Refresh controls are ghost actions with icons.
- Cards lead their facts with an ink price tag (From USD 25.00), shorten the
  visible date under agenda headings (full date kept for screen readers), and
  ease the photo in on hover with motion allowed. The event's Tickets shows the
  price where the bar has room. Events without artwork get a tinted placeholder
  (stored tint, or one of seven hues chosen from the ID) with a perforation dot
  grid. The lead says how soon it is (Tonight, Tomorrow, In 4 days) and takes two
  of three columns beside the next event; desktop agenda headings stick while
  their stretch scrolls; count chips read "2 questions" / "2 recommend". The
  routine "Updated …" line moves below the results (warnings stay above), and
  Happening Now shows a Live / Reconnecting signal that follows the stream. The
  lead quotes its newest public recommendation's reason.
  See [indexed event rows](../../DESIGN.md#indexed-event-rows).

### 2026-11-20 — Depth layer across Discover and the event page

- Discover's result cards (Saved, Interests and Radar stay flat), the preview
  panel, the event identity card, and Community and Happening Now cards are raised
  (soft plum-tinted elevation, a corner glow, a 2px hover lift with motion allowed).
  Tickets and the current date chip take the brand gradient.
- Quieter controls (list rows when lit, command/filter bars, chips, Save and plans,
  section tabs) get a hairline ring and soft shadow. CSS only; no behaviour change.
- Pressing an event title (iOS Safari touch-down included) no longer paints a box
  behind the text; its row or card takes the press wash and the title darkens.
  See the [depth layer](../../DESIGN.md) and the [community entry](event-communities.md).
- Chromium journeys pass with zero axe violations: recommendations, discussions,
  responsive, moderation, saved, interests and action feedback. Conversation
  ergonomics passes apart from an intermittent view-transition abort that also
  occurs on unchanged code. WebKit: action feedback, sign-in, drill-down pass.
