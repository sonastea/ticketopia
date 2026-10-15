# Public HTTP caching measurements — 2026-10-15

## Result

Conditional event reads reduce response bytes, **not service/database invocation
counts**. Every measured 200 and 304 traversed the discovery service and performed
one real repository search, including page, count-cache and coverage work.
Serialization and SHA-256 hashing still run for conditional JSON responses.
A 304 alone is not evidence of avoided backend work or improved latency.

The preceding query/index stage is already implemented in migration 15. Its
[before/after report](query-performance-report.md) records baseline plans, retained
indexes, rejected candidates, write costs and migration verification. This HTTP
stage uses that schema; it introduces no additional query or index change.

## Reproduce

```sh
MARIADB_TEST_ADDR=127.0.0.1:3307 go test ./internal/persistence \
  -run '^$' -bench '^BenchmarkMariaDBPublicHTTP$' \
  -benchmem -benchtime=20x -count=5 -timeout=30m
```

Uses the [isolated MariaDB fixture](query-performance.md), random disposable
schemas/users and deterministic 1,000/10,000-event datasets. No race instrumentation.
Each response contains the first 20 Berlin events over the explicit fixed date
range. The wrapper binds the existing repository reference-time path to
2026-10-08 12:00 UTC, preventing live coverage expiry from changing the workload.
The initial GET primes the count cache and produces the validator. Both measured
paths use the normal router and discovery service; no stored response is reused.

Run on Apple M1, darwin/arm64, local Docker MariaDB, five samples of 20 requests
per case. Fixture setup and request construction are outside timing. Timings and
allocations include router/service/repository/encoding, the response recorder,
and byte-accounting harness. Repository reads count calls across the service-to-
database repository boundary, **not individual SQL statements** or provider calls.
This workload makes no provider calls. These are warm reads, not cold-buffer-pool
measurements; allocation pools and database scheduling cause sample variation.

## Measurements

Medians across five samples:

| Events | Status | Body bytes | Response bytes | ms/op | B/op | allocs/op | Repository reads/op |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1,000 | 200 | 54,776 | 54,939 | 6.919 | 367,384 | 631 | 1 |
| 1,000 | 304 | 0 | 141 | 6.918 | 319,867 | 631 | 1 |
| 10,000 | 200 | 54,653 | 54,816 | 39.249 | 354,625 | 603 | 1 |
| 10,000 | 304 | 0 | 141 | 38.479 | 296,662 | 598 | 1 |

Response bytes count body, application headers and an HTTP/1.1 status line,
excluding transport framing, TLS and net/http's automatic Date/Content-Length
headers. Thus these are reproducible application-byte counts, not packet captures.
There is no compression negotiation in these handlers.

Elapsed-time samples (ms/op, in execution order):

| Case | Samples |
| --- | --- |
| 1,000 / 200 | 6.707, 6.771, 6.919, 7.088, 7.514 |
| 1,000 / 304 | 7.504, 7.198, 6.658, 6.856, 6.918 |
| 10,000 / 200 | 45.673, 38.597, 39.249, 40.849, 38.667 |
| 10,000 / 304 | 38.384, 38.404, 41.181, 38.653, 38.479 |

The timings overlap and do not establish a repeatable latency improvement. The
observable benefit is eliminating the approximately 55 KB response body (and its
recorder copy), while preserving current refresh, coverage and error semantics.
Taxonomy/contract freshness can additionally avoid requests at compliant HTTP
caches for their bounded max-age; this event benchmark does not measure that.
