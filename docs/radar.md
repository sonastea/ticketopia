# Personalized radar

`/radar` and `GET /api/v1/me/radar` share one private, explainable ranking service.
Requires [accounts](accounts.md), MariaDB schema **11**, and the existing runtime
grants; no new migration, feature flag, notification delivery or public-pilot
approval. Scheduled collection is optional. Database-free Discover stays public.

## Choices and matches

- Set city **and** country at `/me/preferences`. Radar uses this account location
  on every client, not remembered browser/IP locations. Dates start today in the
  account time zone (UTC fallback) through 89 days later, inclusive. Event dates
  remain venue-local; this is city-based discovery, not a distance-radius claim.
- Follow artists/venues at `/follows`; choose categories at `/me/interests`.
  Matching uses exact provider/taxonomy IDs, not names or inferred preferences.
  Saves, Interested, recommendations, Helpful and discussions do not influence it.
- An event must match at least one choice. Categories are OR signals, not hard
  exclusions: a followed artist in another category can still appear.
- Distinct signal types score artist **4**, venue **2**, category **1**. Multiple
  matches combine; repeated classifications or large lineups do not inflate rank.
  Sort strongest first, then local date/time (unknown times last), then binary
  event ID. Each matched identity has a code and understandable explanation.
- Without follows/categories, show date-ordered **nearby suggestions**, explicitly
  not personalized matches. Without city/country, ask for location first. Unknown
  retained category IDs remain choices; they do not silently broaden results.
- Exclude cancelled/postponed events, unknown/TBA/TBD dates, events outside the
  window, and known start instants already elapsed when the first page was read.
  Missing locations or matching artist/category identities cannot establish a
  match. Missing labels use generic explanations; prices, images and sale times
  remain unknown, not free or invented. Native details, save/interest controls,
  ticket links when supplied, and navigation work without JavaScript.

## Coverage, failure and privacy

The first page can request one broad city/date collection through the existing
[coverage-aware discovery service](event-history.md#stored-discover-and-coverage-aware-reads).
It reuses receipts, refresh leases, budget and cooldowns across users; never one
provider request per follow/event or an optional taxonomy call. Good records
survive provider failures and restarts. Continuation pages only read local SQL.
Signals, candidates and coverage are read in one repeatable-read transaction.

List and item `meta` describe actual collection/observation time. Old snapshots
and events awaiting omission verification are labeled last-known. Coverage can
be complete, partial, stale or not_collected. An empty list is only “no matches
in available data,” not proof that no event exists. `data_as_of` is null without
evidence. Storage failures return 503, never a fabricated empty radar.

Radar/follows/preferences are owner-only and absent from public profiles. Reads
use browser sessions or personal bearer tokens, `Cache-Control: private, no-store`,
and the existing credential checks. No owner/filter metadata can be supplied by
clients, and no reminders are scheduled by reading Radar or making a follow.

## Pagination and API

Accept only `limit` (default **20**, maximum **100**) and opaque `cursor`; reject
unknown/repeated parameters. The web accepts exactly the same query and service
results. Each response has `{items, next_cursor, mode, scope, meta}`; each item
has `{event, reasons, meta}`. Reason codes are `followed_artist`, `followed_venue`,
`preferred_category`, or starter-only `nearby`; targeted reasons include `target_id`.

Cursors bind owner, limit, first-read time/date window, preferences/follows and
the complete ranked event-record version, with an ID anchor. They work across
replicas/restarts without a process-local snapshot and expire after **24 hours**.
Unchanged versions cannot duplicate/skip events, even if SQL row order changes.
Time-based staleness alone does not invalidate them. Changes to choices or any
ranked event snapshot/observation return **409 `radar_changed`**; discard the cursor
and refresh from page one. Malformed/foreign/expired/changed-limit cursors return
**400 `invalid_input`**. `next_cursor` is null at the end. These are version-checked
pages, not frozen copies: active ingestion may require a refresh while browsing.
The date window/elapsed-event cutoff stay pinned until refresh or expiration.

See the [OpenAPI contract](openapi.md) for complete schemas and auth/error behavior.

## Verification

Run `go test -race ./...`, `go vet ./...`, `npm run build`, OpenAPI lint and
`make check`. Real SQL checks use the isolated local MariaDB fixture with
`MARIADB_TEST_ADDR=127.0.0.1:3307`; do not point it at shared/production data.

Optional native/enhanced Chromium checks:

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 \
RADAR_BROWSER_SCRIPT="$PWD/scripts/radar-browser.cjs" \
PLAYWRIGHT_MODULE=/absolute/path/to/node_modules/playwright \
AXE_SCRIPT=/absolute/path/to/axe.min.js \
go test -race ./internal/persistence -run '^TestMariaDBRadarBrowserReview$' -v
```

Fixtures cover web/API reasons/order, independent owners, pagination/changed-set
recovery, first-run/nearby/no-match states, provider outage and unavailable labels,
native navigation/saving, compact/desktop layouts, long content, enlarged text,
touch, forced-colors keyboard focus, reduced motion and automated accessibility.
Captures go to ignored `.impeccable/review/radar-*`. Physical-device, manual
screen-reader, comprehensive cross-browser and human relevance pilots are separate.
