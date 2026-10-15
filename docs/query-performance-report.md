# Query-plan evaluation — 2026-10-14

## Result

Retain migration **15**: equality-first coverage index and removal of the duplicate
discussion index. Keep Discover ordering/cursors and community query shapes.
No generated column, hint, collation change or runtime grant is introduced.

Measured on Apple M1 / darwin-arm64, TLS to local Docker MariaDB
**12.3.3-MariaDB-ubu2404**, fixed reference `2026-10-08T12:00:00Z`.
The [workflow](query-performance.md) describes fixtures, commands and timing
boundaries. Baseline restores schema-14 indexes; current uses schema 15.
Statistics are refreshed for each configuration. The buffer pool is 512 MiB and
the server query cache is OFF. All reads are warm; no cold-cache
claim. Executed 1,000 and 10,000 events; 100,000 is available explicitly but was
not executed in this evaluation.

## SQL evidence

Times below are median `ANALYZE FORMAT=JSON` execution milliseconds from five
warm executions on the same fixture. “Rows examined” uses `r_index_rows` when
available, otherwise `r_rows`, multiplied by loops. Coverage has one base table;
the figures include index-condition candidates, not just returned rows.
Index-size changes refer to the affected indexes' allocated bytes.

| Case | Dataset | Before | After | Rows examined | Index-size change |
| --- | ---: | ---: | ---: | ---: | ---: |
| Artist coverage | 1,000 | 0.0290 ms | 0.0140 ms | 100 → 4 | 0 |
| Venue coverage | 1,000 | 0.0297 ms | 0.0123 ms | 100 → 3 | 0 |
| Artist coverage | 10,000 | 0.2350 ms | 0.0574 ms | 1,000 → 33 | 0 |
| Venue coverage | 10,000 | 0.2395 ms | 0.0541 ms | 1,000 → 33 | 0 |
| Coverage, selective start boundary | 10,000 | 0.0915 ms | 0.0186 ms | 112 → 8 | 0 |
| Coverage, selective end boundary | 10,000 | 0.2178 ms | 0.0260 ms | 1,000 → 33 | 0 |
| Broad overlapping coverage | 10,000 | 0.6664 ms | 0.6713 ms | 1,000 → 1,000 | 0 |
| Scheduled coverage | 10,000 | 0.0163 ms | 0.0151 ms | 30 → 30 | 0 |
| Discussion duplicate index | 1,000 | 2 identical indexes | 1 index | unchanged access available | −180,224 B |
| Discussion duplicate index | 10,000 | 2 identical indexes | 1 index | unchanged access available | −2,637,824 B |

At 10,000 events, artist/venue coverage estimates 33 candidates, visits 33 index
entries and returns 26 rows after index-condition filtering; residual filtering
rejects none. The baseline scans 1,000 rows and rejects 97.4%. The retained key
uses city/country/artist/venue plus the start-date range, then sorts 26 successes.
The success-order alternative avoids that sort but visits more entries for the
selective start boundary and occupies 16,384 additional bytes. Broad coverage
still favors a scan: this index is not claimed to optimize every range and sort.
Total schema secondary-index allocation falls from **32,129,024 to 29,491,200 B**
at 10,000 events (2.52 MiB saved); coverage itself remains 131,072 B.

### Candidates not retained

- **Date/time composites, ascending and mixed descending:** prefix-name ordering
  still requires filesort. Across all three sorts and both pages, results were
  mixed, not a repeatable common-case improvement. For example, descending-first
  SQL was 36.25 ms with current indexes versus 39.23 ms with the descending key.
- **Wider name prefix (512 characters) plus date/time:** later name SQL increased
  from 25.83 to 30.21 ms in this run; its index grew from 1,589,248 to 2,670,592 B.
  Even this prefix cannot guarantee full-name order. A full 1,024-character
  utf8mb4 name exceeds the InnoDB key limit before adding other sort columns.
- **Expanded cursor predicates:** did not establish a repeatable benefit over
  tuple predicates. Date/name filesort and broad candidate scans remain. These
  measurements do not justify a generated normalized-time column.
- **Community join order:** city/country start from snapshot location indexes;
  selective category/combined requests start from `search_facet`, rather than the
  activity stream. Preserve migration-14 first-venue/per-field fallback semantics.
  Discussion child-existence materialization can still scan 21,200 posts; this is
  visible diagnostic debt, not evidence for forcing the main join order.

## Go benchmark measurements

Each cell is **ns/op · B/op · allocs/op**. These 10,000-event samples use ten
iterations, with setup/cursor traversal excluded. They are descriptive, not
statistical latency claims. The 1,000-event matrix was also run in three alternating
baseline/current rounds of ten iterations. Fixed-clock repository benchmarks are
not comparable to the previous service-level fixtures.

| Case (10,000 events) | Before | After |
| --- | ---: | ---: |
| Discover date asc, first | 37,925,950 · 154,167 · 519 | 37,353,121 · 153,533 · 513 |
| Discover date asc, later | 31,947,229 · 155,361 · 554 | 30,176,388 · 155,196 · 550 |
| Discover date desc, first | 40,318,533 · 154,342 · 570 | 40,081,350 · 153,236 · 564 |
| Discover date desc, later | 29,258,658 · 156,484 · 559 | 29,446,208 · 155,351 · 554 |
| Discover name asc, first | 42,073,429 · 150,973 · 523 | 40,833,371 · 155,772 · 551 |
| Discover name asc, later | 30,389,254 · 154,060 · 545 | 30,111,771 · 153,021 · 537 |
| Discover count-cache hit | 36,585,288 · 152,615 · 508 | 36,872,717 · 153,476 · 512 |
| Discover count-cache miss | 62,141,575 · 154,825 · 542 | 75,043,908 · 154,400 · 537 |
| Recommendation combined filters | 4,819,792 · 199,660 · 829 | 4,855,579 · 198,751 · 827 |
| Discussion global roots | 20,341,579 · 190,948 · 862 | 30,805,367 · 191,577 · 866 |
| Discussion event roots | 2,137,442 · 20,198 · 156 | 2,109,754 · 20,243 · 155 |
| Discussion thread replies | 2,511,400 · 193,048 · 835 | 2,556,017 · 189,531 · 820 |
| Radar first | 86,550,204 · 183,009 · 801 | 89,316,542 · 177,447 · 775 |
| Radar later | 87,211,817 · 184,244 · 812 | 90,721,592 · 181,826 · 800 |

### Write overhead, separately timed

| Case | Dataset | Before (ns/op · B/op · allocs/op) | After (ns/op · B/op · allocs/op) |
| --- | ---: | ---: | ---: |
| Collection page, 100 observations | 10,000 | 2,276,587,138 · 4,993,614 · 52,366 | 2,267,188,729 · 4,973,947 · 52,292 |
| Discussion insertion | 10,000 | 5,658,579 · 19,155 · 231 | 5,117,596 · 19,919 · 234 |
| Coverage scope claim | 10,000 | 2,534,608 · 5,279 · 90 | 2,297,404 · 5,341 · 91 |

The three-round 1,000-event median collection batch was 2.306 → 2.313 seconds,
discussion insertion 5.280 → 5.426 ms, and scope claim 2.517 → 2.500 ms.
There is no demonstrated material write-overhead increase, nor a claimed write
speedup. Removing one identically keyed discussion index eliminates its storage
and maintenance; the retained index still supports the root foreign key.

## Variance and validation

Five additional paired rounds on one 10,000-event fixture, alternating configuration
order and measuring 20 operations per sample, produced these medians (same
**ns/op · B/op · allocs/op** units):

| Case | Before | After |
| --- | ---: | ---: |
| Discussion global roots | 21,119,635 · 191,263 · 864 | 20,912,754 · 191,313 · 867 |
| Discussion city | 22,594,006 · 192,412 · 872 | 22,818,329 · 193,191 · 875 |
| Discussion country | 22,554,156 · 192,432 · 868 | 22,498,241 · 192,654 · 868 |
| Discussion category | 19,076,210 · 193,829 · 891 | 18,907,462 · 194,130 · 889 |
| Discussion combined | 15,975,841 · 194,702 · 887 | 15,771,287 · 194,898 · 888 |
| Discover category | 6,777,643 · 156,062 · 553 | 6,958,639 · 155,552 · 548 |
| Discover count-cache miss | 65,656,229 · 155,247 · 544 | 64,772,254 · 155,016 · 545 |
| Artist coverage | 1,951,725 · 3,645 · 63 | 1,791,820 · 3,752 · 64 |
| Venue coverage | 1,923,239 · 3,741 · 64 | 1,756,000 · 3,737 · 64 |

The initial material regressions did not repeat in the paired medians. Global
discussion samples still ranged 20.54–21.22 ms before and 20.83–32.70 ms after:
there is plan variance, not a claim that every sample improves. Category reads
overlapped (6.54–7.92 versus 6.65–8.05 ms); count misses were 64.74–67.36 versus
64.50–65.34 ms. Coverage's SQL-work reduction is repeatable; its end-to-end benefit
is diluted by transaction/TLS overhead, and the Go timing distributions overlap.

Repeated statistics refreshes can change join order and child
materialization even with unchanged query/index layouts: observed global count
plans alternated between a 21,200-post scan and 10,000 snapshot probes; filtered
discussion plans alternated between child materialization and indexed probes.
This explains the isolated short-sample regressions and why unrelated apparent
improvements are not attributed to migration 15. No systematic common-case
regression was established; no optimizer hint or community query rewrite is retained.

Real-MariaDB race tests passed, including fresh/14-to-15/repeated migrations,
preserved populated replies/coverage, foreign-key enforcement, full traversal of
null-time/collation/full-name/ID ties, and existing community fallback regressions.
Docker `make check` passed (asset build, race tests and vet). Plan/index choices
are not asserted by CI; only the intentional migration schema is checked.
