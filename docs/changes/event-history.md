# Reliable event history changes

## Unreleased

### 2026-11-08 — Label omitted Discover events per row

- Fresh complete coverage now dates and freshens the stored Discover list. Events
  it omitted (cancelled, moved, or removed upstream and 404 on every detail retry)
  no longer mark every page "Showing last-known results" with their old time.
- Each stored list item carries `meta`; stale rows show "Last-known as of …".
  Partial/stale/uncollected coverage keeps the oldest-event list freshness.
- MariaDB regression covers omission plus a 404 detail retry. See
  [stored Discover](../event-history.md#stored-discover-and-coverage-aware-reads).

### 2026-10-29 — Set-based collection and Discover page writes

- Write each collection page with about 30 multi-row statements instead of ~26
  per event (2,604 for 100 events), sharing the per-event search-projection and
  event-update rules. Receipts stay strict `INSERT`s, without `IGNORE`.
- Discover scope pages share the same snapshot writes and batch their scope
  receipts: a 100-event page runs 26 statements instead of 2,502 (about 2.3 s to
  66 ms over the host-to-Docker TLS connection, not the in-network benchmark).
- Single-event writes (saves, interests, posts, reminders, detail refreshes) use
  the same writer: 20 statements regardless of lineup, instead of 220 for 50 artists.
- A 100-event page fell from 561 ms to 43 ms, with 65% fewer allocations, on the
  in-network Docker MariaDB benchmark. Regression tests compare every table with
  sequential writes after each page, bound the statement count and race
  overlapping pages against newer single-event writes. See
  [collection](../event-history.md#stored-discover-and-coverage-aware-reads).

### 2026-10-28 — Coverage evidence, count reuse and page order

- Order stored Discover pages after the snapshot join too, so batched joins cannot
  reorder a page or anchor its continuation cursor on the wrong event.
- Count scheduled coverage by distinct local dates (accent variants of one city no
  longer cover a second date) and mark partially collected old days stale.
- Recount continuations on a count-cache miss instead of trusting the cursor's
  carried total; a newer receipt after a complete enclosing scope still
  invalidates cached totals.
- A capped (`limited`) enclosing scope's retry cooldown no longer blocks
  collecting a shorter range. See
  [stored Discover](../event-history.md#stored-discover-and-coverage-aware-reads).

### 2026-10-14 — Keyset Discover pagination and count reuse

- Replace stored Discover `OFFSET` paging with filter/limit/sort-bound keyset
  cursors across `date_asc`, `date_desc`, and `name_asc`; narrow date/name index
  bounds and defer snapshot/omission reads until after `ORDER BY ... LIMIT`.
- Skip `COUNT(*)` on first-page reads that fit within `limit`, reuse exact counts
  across repeated searches, sort/limit changes, and continuation cursors when
  coverage and catalog state are unchanged, and invalidate cached/cursor totals
  whenever stored events or collection receipts change.
- Preserve SQL `NULL` time ordering for TBA/unknown times, case-insensitive
  Unicode name tie-breaking, binary `event_id` ordering, and the 512-byte cursor
  bound by resolving long anchor titles from `events`.
- Add 1,000/10,000-event first/halfway-page benchmarks and MariaDB/unit
  regressions for all sort orders, TBA times, case collisions, long titles,
  cursor validation, and catalog-write count invalidation. See
  [stored Discover](../event-history.md#stored-discover-and-coverage-aware-reads).

### 2026-10-09 — Keep Discover usable during refresh and cancellation

- Serve retained catalog results immediately during due collection; isolate
  shared refreshes from browser cancellation and drain them before SQL/cache close.
- Finalize failed/partial ingestion with bounded cleanup so interrupted writes
  do not leave the scope running until its lease expires.
- Give each on-demand page a fresh 20-second fetch/ingestion budget instead of
  sharing 12 seconds across the entire scan. Bound cold-request waits to 12
  seconds without canceling collection; regression-test later-page SQL time,
  partial-to-complete background progress and stalled-page cleanup.
- Buffer HTML and commit through Echo, preventing partial pages and duplicate
  response headers when rendering fails; cap the optional page taxonomy wait at
  one second. Regression-test cancellation, retained reads, failure receipts,
  shutdown, slow optional metadata and response tracking.
- See [stored Discover behavior](../event-history.md#stored-discover-and-coverage-aware-reads).

Older entries: [event history archive](archive/event-history/).
