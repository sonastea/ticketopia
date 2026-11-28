# Pilot-city relevance check: Los Angeles

**Status: Parts 1 and 2 deployed and verified in production (2026-11-27);
Part 3 (the pilot) not started.** Orange County is included (open decision 2),
and the web turns on areas and runs while the API and agents keep them opt-in
(decision 3). Check a part only when its behavior is deployed and verified.
Current behavior lives in [metro areas and runs](../discovery.md#metro-areas-and-runs).

- [x] Part 1 — Los Angeles area: include nearby venue cities
- [x] Part 2 — Quieter lists: one row per run, hide non-events
- [ ] Part 3 — Run the pilot with three people and record the outcome

This closes the open checkpoint in the [delivery checklist](../goals/delivery.md)
("Verify relevance with three people in one well-covered pilot city") and the
root README's "Validate relevance with people in a pilot city". The
[first checkpoint](../goals/personal-radar.md#small-goals-one-at-a-time) is radar
steps 1–2: name a relevant nearby event, understand its date, venue and known
price, save it, leave, and find it again. The same sessions also try
question → reply → Helpful and private reporting, which must happen
[before a public pilot](../goals/delivery.md#current-focus-use-refine-and-verify).

## Why Los Angeles needs work first

Production data audit, 2026-11-27, Discover reads for 2026-11-27 to 2026-12-27
through the public `/api/v1/events` route:

| Finding | Evidence |
| --- | --- |
| Data volume is enough | `Los Angeles, US`: 862 events, 31/31 days collected, coverage complete after refresh. |
| Exact city match leaves out major LA venues | Inglewood 85 (SoFi Stadium, Intuit Dome, YouTube Theater), Hollywood 189 (Palladium, Dolby, Improv), West Hollywood 78 (Troubadour, Whisky A Go Go, Roxy), Pasadena 33 (Rose Bowl), Anaheim 65 (Honda Center), Long Beach 29. None appear under "Los Angeles". |
| Repeated sessions flood the list | "Frosty's Christmas Bar – Los Angeles" is 412 of 862 listings (15-minute entry slots, up to 27 a day). It took 9 of the first 20 results. 32 titles repeat across days at one venue (Frosty's 27 days, *Destiny of Desire* 21, *Purpose* 15, *Elf* 12, *Oh, Mary!* and *The Lion King* 9 each). Folding same-day listings gives 416 rows; folding each run gives 280. |
| Non-event listings | "Upgrade: Mezzanine Bleachers" and "Upgrade: Premium Viewing Deck" (Regent Theater add-ons) and "Test – Ashley Kamphaus". All are classified Miscellaneous/Undefined, like many real events, so stored categories cannot separate them. |
| Prices are sparse | 81 of 862 (9%) carry a price range. This is provider data and is not fixed here; the pilot checks whether "price not listed" plus the ticket link is acceptable. |
| Stale until visited | Production runs no scheduled collection (`EVENT_HISTORY_ENABLED` unset), so LA is collected on demand. The first read returned data from 2026-10-29 marked stale; it refreshed on that visit. |
| Category mix | Miscellaneous 451 (mostly the Frosty slots), Music 248, Arts & Theatre 95, Undefined 41, Sports 26, Film 1. |

Unfixed, the pilot would mostly re-measure these known gaps. LA participants
treat SoFi, the Forum, the Hollywood Bowl, the Palladium and the Troubadour as
LA, so leaving them out hides the events most likely to draw interest.

## Part 1 — Los Angeles area

**Outcome:** choosing Los Angeles shows events at venues in its nearby cities,
labelled "Los Angeles area", with a way back to Los Angeles only.

### Approach: curated metro areas over existing city scopes

A metro area is a named, fixed list of member venue cities in one country,
checked into code (for example `internal/discovery/metros.go`). The first and
only entry is Los Angeles. Every member is collected and covered by the
existing per-city on-demand scopes, so collection, budget, leases and coverage
receipts are unchanged.

- **Membership.** Curated: 34 cities in `internal/discovery/metros.go`, from
  Hollywood, Inglewood and Pasadena to Long Beach, Pomona and Orange County
  (Anaheim, Santa Ana, Costa Mesa, Irvine, Fullerton, Huntington Beach, Newport
  Beach). The planned coordinate audit could not run against production's
  database; after deploying, check the production list's venue cities and add
  any LA-area spellings it reveals.
- **Matching.** A city search for a metro's anchor or any member resolves to the
  metro. The stored read uses `p.city IN (members) AND p.country=?`. The
  existing `event_search_places` lookup already starts from place rows, so this
  stays an index range per member. Run the query benchmarks
  ([query performance](../query-performance.md)) before and after.
- **Coverage.** Collect members on demand in the same request as today's single
  city, within the existing durable budget. The metro is `complete` only when
  every member is complete and fresh, `stale` if any member is stale, and
  `partial` otherwise. The list's `data_as_of` is the oldest member's. A member
  outage never blanks the others; the existing partial notice explains it.
- **Budget.** At least one provider request per member per refresh, plus Los
  Angeles's own pages: about 45 per metro refresh, at most four refreshes a day
  with 6-hour freshness. This is inside the 4,500/day budget. A cold area takes
  about ten seconds at four requests a second. Enabling scheduled collection is a
  separate operator decision.
- **Contract.** Add an `area` query parameter: `metro` (include nearby cities)
  or `city` (exact). The web defaults to `metro` when the chosen city has a
  metro; the JSON API and agent tools default to `city`, so existing clients do
  not change. List metadata reports the resolved area and its member cities.
  Update `internal/api/openapi.yaml` and the `find_events` agent tool together,
  sharing one service method.
- **Web.** The location label reads "Los Angeles area" and gives the choice
  "Only Los Angeles" (and back), as an ordinary link/GET form that works without
  JavaScript; a hidden field keeps "only this city" when filters are applied.
  The remembered-city cookie stores only the city, so the next visit starts with
  the area again. Event cards already show each venue's city.
- **Radar.** Radar uses discovery with the account's preferred city, so it
  inherits the area. Its explanation names the area rather than the city.

**Out of scope:** geographic radius search (still planned separately in
[live activity filters](live-activity-filters.md#radius-extension)), metros
other than Los Angeles, and metro matching in Community, Happening Now and
recommendation city browsing, which keep exact city matching until a later
decision.

**Done when** an LA search shows SoFi/Intuit Dome/Palladium/Troubadour/Rose Bowl
events, "Only Los Angeles" restores today's results, coverage stays truthful
when one member fails, and API/agent results without `area` are unchanged.

## Part 2 — Quieter lists

**Outcome:** a list page shows distinct things to do, not 15-minute entry slots
or ticket add-ons.

### How comparable sites handle repeated listings

Checked 2026-11-27 against each site's public Los Angeles pages:

| Site | Same show on many dates | Many times in one day | Source |
| --- | --- | --- | --- |
| TodayTix (theatre, LA + OC) | One card per show; performances are chosen on the show page. *Destiny of Desire* and *Purpose* appear once each. | Chosen on the show page | [LA shows](https://www.todaytix.com/los-angeles/shows) |
| Eventbrite | A recurring series appears once in search results, linked to its next date (`series_id`, `is_series`, `parent_url`), with a series page listing all dates. | Same, inside the series | [LA events](https://www.eventbrite.com/d/ca--los-angeles/events/), [recurring events help](https://www.eventbrite.com/help/en-us/articles/256014/create-and-manage-multiple-date-or-recurring-events/) |
| Songkick (concerts) | Multi-day items are one row with a date range ("Swiss Winter Village, 20 Nov 2026 – 3 Jan 2027"). Separate concerts by one artist stay separate rows. | Two sets are two rows (Kenny Garrett, Blue Note) | [LA metro area](https://www.songkick.com/metro-areas/17835-us-los-angeles-la) |
| Ticketmaster Discovery API | Each performance is its own event; grouping is left to clients. Test entities are excluded unless `includeTest` is sent. | Separate events | [Discovery API v2](https://developer.ticketmaster.com/products-and-docs/apis/discovery-api/v2/) |

Sites built around theatre, experiences and recurring events show one row per
run and choose the date inside it. Concert-first Songkick still collapses
multi-day items into a date range. Ticketopia's list mixes all of these, and
Ticketmaster leaves grouping to the client, so Part 2 folds runs.

### One row per run

The event model stays one occurrence per item (`internal/models/event.go`);
saves, Interested, recommendations and discussions remain per occurrence.
Only list presentation folds.

- **Run key:** identical event name and same first venue ID, within the
  requested date range. This covers entry slots on one day, theatre runs,
  residencies and daily tours with one rule. Never fold across venues or
  differently named listings; that would be the title-based grouping the model
  deliberately avoids. Generic recurring titles ("Comedy Night" at one club)
  also fold. That is acceptable because every date stays one tap away, and the
  pilot debrief checks it.
- **Placement:** the row sits at the run's earliest occurrence in the requested
  range, as Eventbrite and Songkick place a series at its next or start date. A
  narrower date search, such as "This weekend", still finds the run at its first
  date in that range. Name sort shows one row per run.
- **Query:** page over runs in SQL, grouped by name and first venue ID, ordered
  by each run's earliest (date, time), with the cursor on (first date, first
  time, name, venue ID). Totals count runs. Fetch run members only for the runs
  on the page. If the query benchmarks need it, add an indexed virtual column for
  the first venue ID.
- **Contract:** a `fold=run` parameter. The list item is the run's earliest
  occurrence plus a `run` summary: occurrence count, distinct days, first and
  last date, and first and last time when the run is one day. The web uses
  `fold=run`; the API and agent tools default to unfolded, so existing clients and
  cursors are unchanged. Document it in `internal/api/openapi.yaml`.
- **Web row:** one row per run. The row keeps the date and time of the listing
  it opens and adds a note: "Also 18 more times that day, until 9:30 PM", "Also on
  11 more dates, through Sun, Dec 27" or "412 times over 27 days, through Sat,
  Dec 26". (Built this way rather than as a date range, so the row never hides
  which date it opens.) A single occurrence looks as it does today. Status and
  price come from the occurrence shown; if another date is cancelled, postponed or
  rescheduled, the note adds "Some dates changed".
- **Event page:** an "All dates" section lists the run's occurrences by day, each
  a link, with the current one marked and status shown per date. More than
  about 10 entries collapse behind a native disclosure that works without
  JavaScript. People save, mark Interested or ask about the exact date they open.

### Hide non-event listings

- **Test entities are already excluded.** Ticketopia does not send
  `includeTest`, so Ticketmaster omits flagged test events. "Test – Ashley
  Kamphaus" is therefore not flagged and needs no special handling here; record
  it as a pilot observation if it is still listed.
- **The provider marks add-ons (verified 2026-11-27).** The raw "Upgrade:
  Mezzanine Bleachers" record has classification type **Upsell**
  (`KZAyXgnZfZ7v7nJ`), subtype Special Entry. "Test – Ashley Kamphaus" has type
  Undefined and `test: false`, so it stays listed.
- **Built:** classifications keep `type`; Upsell listings get a search facet and
  are excluded from stored Discover, Radar and event dates. Direct event links
  and existing saves keep working. No name rules.
- Venue tours (Coliseum, SoFi, Intuit Dome) are real things to do and stay.

**Done when** the first LA page shows each run once (Frosty's, *Destiny of
Desire*, *Elf*), a narrower date search still finds a run, every date stays
reachable from the event page, flagged add-ons are gone, and the unfolded API
output is unchanged.

## Part 3 — Run the pilot

Moderated sessions on each participant's own device, against production, after
Parts 1–2 are deployed.

### Before the sessions

- [x] Deploy Parts 1–2 and repeat the audit above (2026-11-27: the folded LA
      area lists 657 rows from 18 member cities, coverage complete, no add-ons).
- [ ] Add each participant's Google account as a test user in Google Auth
      Platform (the production OAuth app is in Testing mode). Sign in fails
      otherwise. If that isn't possible, use the
      [local fallback](#fallback-sessions-on-your-laptop).
- [ ] Turn on scheduled collection for the Los Angeles area in Coolify:
      `EVENT_HISTORY_ENABLED=true`,
      `EVENT_HISTORY_CITIES=[{"city":"Los Angeles","country":"US","area":"metro"}]`
      and `EVENT_HISTORY_INTERVAL=5h` (about 600 requests a day; see
      [metro areas](../event-history.md#metro-areas)). Without it, Los Angeles's
      default 90-day view exceeds the provider's 1,000-result on-demand limit and
      reads as incomplete, and the area stays fresh only when someone visits.
- [ ] Confirm your moderator role works on production: open the review queue.
- [ ] Line up a second person to reply to questions during the session (another
      participant, or you on a separate account).

### Fallback: sessions on your laptop

Use this only if participants can't sign in to production. It needs no code
changes: the participant uses your laptop, already signed in as a throwaway
local account. Production stays the preferred setup, because the fallback
changes what the pilot measures (below). Do not build a pilot sign-in route for
participants' own phones; that would be a new way to authenticate, for three
people.

1. Run the usual local setup (`make local-setup` once, then `air`) and open
   `http://localhost:3000`. Your `.env` must point `AUTH_BASE_URL` at that same
   address: account forms reject any other origin.
2. Before each participant, run `make local-test-accounts`. It replaces the
   throwaway accounts, so each participant starts with an empty account and the
   previous participant's saves are gone.
3. In a fresh browser profile, open DevTools → Application → Cookies →
   `http://localhost:3000` and add a cookie named `ticketopia_session` (HttpOnly)
   whose value is `accounts.other.session` from `.local/local-test-accounts.json`.
   Use `other`: `owner` has Chicago as its account city, which would change
   where Discover starts. Don't show the file on screen.
4. Open Discover for Los Angeles once before the participant arrives so the area
   is collected and fresh (about ten seconds the first time), or set the same
   scheduled collection in your `.env`.
5. Afterwards, run `make local-test-accounts-clean`.

Checked 2026-11-27 against the `air` setup: a browser with `other`'s cookie
opened the Los Angeles area, saved an event, and found it under Saved.

What changes: participants don't use their own device or sign in themselves,
and the next-day return (step 6) becomes a return later in the same session,
after closing the tab. Record that in the results. Production sign-in, their own
devices and a real next-day return then remain unverified, so the delivery
checklist item stays open until one production session covers them.

### Who

Three people who live in or around Los Angeles and go to at least a few events a
year, who haven't seen Ticketopia. Mix their interests (for example music,
comedy/theatre, sports) and devices: at least one iPhone, one Android phone and
one laptop.

### Session script (about 35 minutes)

Say: "I'm testing the site, not you. Think out loud. There are no wrong answers."
Don't point at controls; if they're stuck for two minutes, note it, then help.

1. **Arrive (2 min).** Send only the link. Note the city the site starts with
   (estimated from their IP, which may be a suburb or wrong) and whether they
   change it.
2. **Find something worth considering (8 min).** "Find something in the next
   month you'd actually consider going to." When they pick one, ask them to tell
   you the date and time, the venue, and what it costs or whether a price is
   listed. Note results they skip and why, and any venue they expected but
   didn't find.
3. **Keep it (5 min).** "Keep it so you can come back to it." Note whether they
   find Save, how sign-in goes, and whether they confuse Save with Interested
   or Recommend.
4. **Ask a question (6 min).** "Ask something you'd want to know before going."
   Your helper replies. The participant marks the reply Helpful. Note whether
   they notice the reply and what Helpful means to them.
5. **Report (4 min).** Point them to a deliberately off-topic post you prepared
   on an unrelated event. "Let the site know this doesn't belong here." Check
   that they see a private receipt; review it as moderator afterwards.
6. **Come back (next day, 3 min).** Send no reminder link. Ask them to open the
   site and find what they kept. Note the path they take and how long it takes.
7. **Debrief (5 min).** What would make you come back? What was missing or
   confusing? Was anything in the list not something you'd call an event? For a
   row showing several dates, could you tell which date you were opening? Would
   you use it instead of what you use now?

### What to record per participant

Use P1–P3, never names or emails, in anything committed to the repository.

| Signal | P1 | P2 | P3 |
| --- | --- | --- | --- |
| Device / browser | | | |
| Starting city shown; changed it? | | | |
| Minutes to a relevant event | | | |
| Event chosen (category) | | | |
| Stated date, venue, price correctly? | | | |
| Expected venue or event missing | | | |
| Results skipped as noise | | | |
| Saved without help? Sign-in issues | | | |
| Question → reply → Helpful completed? | | | |
| Report receipt seen? | | | |
| Found the save next day, unaided? | | | |
| Would return / why | | | |

### Pass criteria

The checkpoint is met when all three participants name a relevant event and
correctly state its date and venue and whether a price is known, and save it and
find it again the next day without help. Record any failure as a finding with
its cause; one failing participant means another session after a fix, not a
silent pass. The question/reply/Helpful and reporting results count toward the
separate community verification items and do not block this checkpoint.

### Afterwards

- Write anonymized results into a "Results" section of this document.
- If passed, check the delivery checklist item and the README goal, and add an
  entry to `docs/changes/personal-radar.md`.
- Triage findings as small fixes, radar/relevance work or out of scope, as
  [learn before expanding](../goals/personal-radar.md#learn-before-expanding) describes.

## Open decisions

1. Approve Parts 1 and 2, or run the pilot without one of them.
2. Whether the Los Angeles area includes Orange County (Honda Center, Angel
   Stadium, Segerstrom Center) or stops at Long Beach. TodayTix groups
   "Los Angeles + OC", and Songkick's LA metro reaches Santa Ana, Newport Beach,
   Riverside and Ontario. Recommended: include Orange County's main venue cities
   (Anaheim, Santa Ana, Costa Mesa, Irvine) but not the Inland Empire. The
   debrief question about missing venues checks this choice.
3. Web defaults to the area; API and agents default to exact city. Recommended
   as written, so existing clients keep their behavior.
4. Scheduled collection: decided (2026-11-27) to keep the Los Angeles area fresh
   with scheduled collection. Los Angeles alone has about 1,300 events in
   Discover's default 90 days, beyond on-demand collection's 1,000 results, so it
   is collected day by day while the other members refresh broadly. Enabling it
   is an operator change in Coolify.

## Verification

Review-server fixtures with metro members, a failing member, runs (one-day
slots, multi-day runs, a run partly outside the date range, mixed cancelled
dates) across page boundaries, and add-on listings. Real-MariaDB tests for
the folded and metro queries plus the before/after query benchmarks. Route tests
confirm API and agent defaults are unchanged. Check desktop and phone, keyboard,
JavaScript off, reduced motion, forced colours and 200% text; run axe. Have an
agent use the changed `find_events` tool as described in
[App MCP](../app-mcp.md#verifying-new-tools-with-an-agent). After deploying,
rerun the production audit with read-only reads.
