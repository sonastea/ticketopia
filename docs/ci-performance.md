# CI performance audit

Audit date: **2026-11-16**. Scope: optimize the existing image-publishing
pipeline, without publishing, changing runners or repository protection, or
adding PR execution to the trusted self-hosted runner.

## Merge-safety limitation

There is **no PR or merge-queue validation workflow** in this checkout or the
available Actions history. Publishing validates only after a push to `main`,
a release tag, or a manual run on those refs. That cannot enforce tests before
merging. The repository is public; running arbitrary PR code on its Docker-enabled
self-hosted runner would cross the documented trust boundary.

The owner selected **“Optimize existing CI only”** during this audit. PR
validation and configuring its required status check remain blockers to the
pre-merge requirement. No protection settings were changed. Reading classic
branch protection required authentication (HTTP 401); the public rulesets
endpoint returned an empty list. Neither result proves effective protection.

## Historical baseline

`gh` was not installed and no GitHub API token was configured. The public REST
API returned **96 available runs** out of the requested latest
150, from 2026-10-02 through 2026-11-15:

| Workflow | Events / branch | Runs | Outcomes |
| --- | --- | ---: | --- |
| Publish app image | `push` / `main` | 86 | 76 success, 8 failure, 2 cancelled |
| Dependency Graph (generated workflow) | `dynamic` / `main` | 10 | 10 success |
| PR / merge queue | None | 0 | No baseline |

All listed runs were completed; none were in progress. The API's anonymous rate
limit prevented retrieving 43 job listings (41 HTTP 403, two HTTP 429); **53
listings** were retrieved: 48 successful and two cancelled publishing runs,
plus three dependency-graph runs. This is a recent, availability-limited sample
(2026-10-23 onward), not a random sample. Earlier failed runs and the slowest
historical success remain included in the run-level summary below.

### Main: creation to completion of the publishing job

All stages are sequential in one `publish` job. Consequently its critical path
is the job timeline, not a sum across overlapping jobs. Timing includes runner
waiting, action setup, validation, publication and post-action cleanup; it does
not stop at the test command. Required branch-protection checks could not be
enumerated, so this measures the workflow's entire check, not proven merge gates.

| Metric (seconds) | n | p50 | p95 |
| --- | ---: | ---: | ---: |
| Total, successful publish jobs | 48 | 183.5 | 218 |
| Total, including cancelled publish jobs | 50 | 184 | 276 |
| Runner queue: job created → started, successful jobs | 48 | 2 | 2 |
| Dispatch: workflow created → job created, successful jobs | 48 | 0 | 1 |
| Job dependency waiting | 50 | 0 | 0 |
| Job setup | 48 | 3 | 5 |
| Checkout | 48 | 1 | 1 |
| Package access/privacy preflight | 48 | 1 | 1 |
| Buildx initialization | 48 | 2 | 3 |
| Docker assets/build/race tests/vet/cache I/O, combined | 48 | 138.5 | 171 |
| Runtime image build/push/cache I/O, combined | 48 | 23 | 30 |
| Other overhead, including cleanup and unassigned job time | 48 | 12 | 14 |

The last row excludes setup, checkout, preflight, Buildx, the two build actions,
queueing and dispatch. Quantiles are calculated separately and **must not be
added**. p50 is the median; p95 is the nearest-rank observation. With only 48
successful detailed samples, p95 is the third-slowest run: provisional, not a
reliable tail-latency guarantee.

### Before/after CI scorecard

After-change GitHub measurements require an owner-authorized push/run and are
unavailable. These are main-publishing metrics, **not PR metrics**:

| Metric | Before | After | Improvement |
| --- | --- | --- | --- |
| Total CI p50 | 183.5 s (48 successes) | Unmeasured | Unverified |
| Total CI p95 | 218 s (48 successes; provisional) | Unmeasured | Unverified |
| Waiting, queue p50 / p95 | 2 / 2 s | Unmeasured | Unverified |
| Setup, combined p50 / p95 | 7 / 9 s | Unmeasured | Unverified |
| Compilation | Not separable in historical Docker steps | Unmeasured in CI | See local diagnostics |
| Testing | Not separable in historical Docker steps | Unmeasured in CI | Unverified |
| Linting / vet | Not separable in historical Docker steps | Unmeasured in CI | Unverified |
| Other overhead, p50 / p95 | 12 / 14 s; publication separately 23 / 30 s | Unmeasured | Cache I/O reduction unquantified |

### Full retained run-level history

Job timestamps are unavailable for older runs. These figures use
`updated_at - created_at`, which can include later run updates and is **not**
an exact check-completion timestamp:

| Population | n | p50 (s) | p95 (s) | Maximum (s) |
| --- | ---: | ---: | ---: | ---: |
| All main publishing outcomes | 86 | 176.5 | 276 | 2186 |
| Successful main publishing | 76 | 178 | 253 | 2186 |
| Failed main publishing | 8 | 96.5 | 100 | 100 |
| Cancelled main publishing | 2 | 344 | 377 | 377 |
| Dependency Graph, `dynamic` | 10 | 39.5 | 44 | 44 |

Small-population p95 values above are descriptive order statistics only.
Failures are not faster successful feedback. Cancellations are not passes and
their time to cancellation is not an estimate of time to successful completion.
The 2186-second successful run
([37392668218](https://github.com/sonastea/ticketopia/actions/runs/37392668218))
is retained, not trimmed. The cancelled runs are
[39916143821](https://github.com/sonastea/ticketopia/actions/runs/39916143821)
and [42555169524](https://github.com/sonastea/ticketopia/actions/runs/42555169524).

Publishing jobs use `[self-hosted, linux, x64]` (`github-runner`). The three
observed dependency-graph jobs use `ubuntu-latest`; they are GitHub-generated,
not duplicate repository build/test workflows. No tag, manual, or PR events
appear in the available sample.

For the three dependency-graph jobs with timestamps, creation-to-job-completion
was p50 33 s / empirical p95 36 s; runner queue 2 / 9 s, dispatch 1 / 2 s,
job setup 1 / 1 s, and the combined `Run Dependabot` step 25 / 29 s.
Only one job was visible per run. Its generated workflow's internal setup,
dependency resolution, cache and artifact costs are not separately observable.
These three observations are insufficient for a reliable p95.

### Timing and cache observability limits

Go initialization, module downloads, compilation, test execution, vet, cache
restoration/upload and Docker build-record artifact handling are inside the
Docker actions. Step timestamps cannot separate them. Do not label the
138.5-second validation step as “test execution” or report its compilation and
cache costs again elsewhere. Authenticated build logs are needed for a historical
breakdown and cache-hit rates; those measurements were unavailable.

The public cache inventory reported 209 entries; the fetched first page of 100
accounted for 947 MB. This is a partial inventory, not total cache size or a
per-run transfer measurement. Entries are BuildKit layer blobs/indexes, not an
`actions/setup-go` cache. No historical GOCACHE/GOMODCACHE hit ratio is inferable.

## Bottlenecks and changes

### Preserve the existing Go cache mounts

**Before:** setup-buildx generated a new builder and removed its state after each
run. Docker's GHA layer exporter does not include cache-mount contents. Even a
cached `go mod download` layer can therefore leave `/go/pkg/mod` empty in the
next builder. A changed source layer or forced validation recompiles packages
and can redownload modules. Local Docker logs reproduced downloads after a
cached dependency-download step.

**Change:** use setup-buildx's supported `name` and `keep-state: true` inputs.
The name hashes repository ID and runner name, so it is stable, accepts arbitrary
runner-name characters, and separates different runner services sharing a Docker
daemon. Normal action cleanup still removes the builder container; its BuildKit
state volume is reused on the next job on that runner.

The existing `/go/pkg/mod`, `/root/.cache/go-build` and `/root/.npm` mounts now
survive that lifecycle. Go's content-addressed cache still keys compilation by
toolchain, platform, source, dependencies and flags. No copied test binaries,
manual package keys or custom compilation infrastructure are introduced.
`go test -race -count=1 ./...` remains intact: compilation is reusable;
**test results are never substituted for execution**.

**Risk/tradeoff:** runner-local disk usage increases and BuildKit garbage
collection may evict caches. A replacement/ephemeral runner remains cold and
correct; the remote layer cache remains available. Never share this trusted
builder with untrusted PR jobs or put credentials in build arguments/context.
Per-job Docker authentication configuration and logout remain unchanged.

### Invalidate validation results without clearing compilation caches

**Measured problem:** even retaining a builder and its state did not preserve
the validation mounts when using `--no-cache-filter validate`. A minimal
cache-marker experiment reproduced the deletion after builder recreation.
BuildKit's no-cache handling resets cache mounts used in the affected stage;
this also explains the repeated downloads in the second baseline Docker run.
Retaining the builder alone was insufficient and the initial proposal was revised.

**Change:** the validation stage consumes a `VALIDATION_RUN` build argument and
rejects an empty value before running the unchanged full race/vet commands.
GitHub supplies its unique run ID and attempt; `make check` supplies timestamp
plus shell PID. Each invocation invalidates the validation RUN's result through
an ordinary build argument, without asking BuildKit to reset its cache mounts.
The argument is confined to the validation stage and does not invalidate asset
or production-binary stages.

**Correctness risk:** reusing a caller-provided value could reuse a completed
validation layer. The maintained workflow and Make target generate fresh values;
manual callers must do the same. Missing values fail rather than accepting a
cached pass. No cached Go test results are used (`-count=1` is retained).

### Export the remote cache once

**Before:** validation and publication both imported and exported `mode=max` in
the same builder and default GHA cache scope. Publication already reused the
validation build's common stages locally, and its export replaced the same cache
index. The always-executed validation layer is not a useful remote test-result
cache.

**Change:** import remote layers during validation, then export common build
layers once during publication. The second import and first export are removed.
The final runtime build still waits for successful validation and still exports
`mode=max` for a cold runner. No cache errors are suppressed.

**Expected benefit:** less duplicate remote-cache I/O on the serial critical
path. The exact seconds saved are unmeasured without an authorized CI run;
the entire 23-second publication action must not be claimed as eliminated.

### Preserve pending main validations

`cancel-in-progress: false` does not, by itself, preserve pending runs:
GitHub's default `queue: single` replaces the previous pending run on a new
arrival. Set the supported `queue: max` while retaining the same per-ref group
and `cancel-in-progress: false`. Publishing remains serialized; no main or tag
run is deliberately cancelled to report a shorter CI duration.
GitHub limits this queue to **100 pending runs**, after which it rejects further
arrivals. This is a platform limitation, not a guarantee against every possible
cancellation. Historical queue time was typically 2 s; no speed improvement is
claimed for this correctness safeguard. Burst waiting may increase because more
checks actually execute.

## Compilation and test investigation

One Go module targets Go 1.27. The default test graph contains 777 package
variants: 392 external packages and 34 test binaries. Large source/dependency
groups include generated views, persistence/API tests, Redis, AWS SES,
OpenTelemetry and the MCP SDK. Generated assets/templates are rebuilt from
source in Docker and retained in validation.

Production uses `CGO_ENABLED=0`, `-trimpath`, and explicit target OS/architecture;
race validation uses `CGO_ENABLED=1` and GCC. Race instrumentation requires
different compiled artifacts. These build modes are intentional, not duplicate
checks to remove. `go test` also performs its default vet subset; the explicit
full `go vet ./...` remains. Sharing one GOCACHE allows compatible package and
analysis actions to be reused without changing either command.

The host baseline (`darwin/arm64`, Go 1.27.3, existing cache) ran the unchanged
full race suite with `GOFLAGS=-trimpath` in **80.01 s**. It passed 34 test packages
and 819 test/subtest pass events; 148 existing opt-in test/subtest skips were
reported. These counts are not 819 independent top-level tests.

Slowest package-reported elapsed times were API 40.18 s, persistence 29.23 s,
activity 26.57 s and app-MCP 21.49 s. They overlap: **do not add them**. Slow tests
include the seeded soak analyses (13.97 s / 10.39 s), queued-owner rebuild
(9.78 s), HTTP/MCP parity (9.21 s) and capped provider pagination (6.10 s).
Membership tests deliberately cross real refresh deadlines; many tests mutate
environment variables or state. No blanket `t.Parallel`, shorter waits, reduced
bootstrap iterations, test sharding or new shared fixtures were applied.

MariaDB, SES/browser, deployment/load/soak and tagged `activityablation` checks
were already opt-in, not part of this workflow's enabled suite. They remain
unchanged; passing the default suite does not verify them. No coverage setting,
race flag, build tag, supported platform or timeout was removed or weakened.

### Isolated compilation/cache measurements

Each state below is **one local diagnostic sample**, not a CI quantile.
Commands ran sequentially on darwin/arm64 with Go 1.27.3, `-trimpath`, isolated
initially empty GOCACHE/GOMODCACHE, static app/health builds and CGO-enabled race
compilation. They used an unchanged committed-source copy outside the checkout;
the partial state appended a comment to `internal/api/account_navigation.go`.
This synthetic small edit is not representative of all PRs.

| Command / diagnostic | Cold (s) | Warm (s) | Partial (s) |
| --- | ---: | ---: | ---: |
| `go mod download` | 5.36 | 0.02 | Not repeated |
| Static app build | 20.48 | 0.23 | 0.75 |
| Static health-check build | 0.63 | 0.08 | 0.08 |
| Race test build/link/start, no test bodies | 52.41 | 13.89 | 17.58 |
| Full `go vet ./...` | 5.80 | 0.50 | 0.64 |
| Sequential build/race-build/vet total, excluding module command | 79.31 | 14.71 | 19.06 |

The race diagnostic used `go test -race -run '^$' -count=1 -json` only to
separate build/link overhead from test bodies. **It is not a validation run**;
no required workflow or Docker command uses that filter. The full-suite runs
and their failure are reported separately below.

Go's `-debug-actiongraph` confirmed 675 executed package-build actions for the
cold static app versus zero warm and one partial. Race package-build actions
were 775 cold, zero warm and two partial; all three states still linked 34 test
binaries. The largest cold package-command costs included Redis (7.63 s static /
14.99 s race), generated icons (3.69 / 4.55 s), and race variants of views/home
(5.29 s) and persistence tests (4.76 s). These commands overlap and their times
must not be summed into end-to-end duration.

The measured compilation-diagnostic saving was **64.61 s (81.5%)** for warm
versus cold caches, not a forecast of CI savings. The isolated caches occupied
1.3 GiB (GOCACHE) and 365 MiB (GOMODCACHE) after the experiment. Preserving them
locally avoids a new archive restore/upload, but remote layer-cache transfer
times remain unmeasured.

## Verification and follow-up

The first unchanged Docker validation completed successfully in **205.90 s**:
56.9 s for asset generation/static builds and 144.8 s for the combined race/vet
step. It was a **partial layer cache**, not a fully cold build: dependency/npm
layers were cached, but Go cache mounts needed downloads/compilation.

The scheduled second cache-state measurement **failed in 122.61 s**, at
`internal/activity/membership_test.go:490`,
`TestCappedRebuildHoldsQueuedAccounts`: “account 214 was closed while its rebuild
was queued.” This run also redownloaded modules and recompiled dependencies;
it must not be reported as a successful warm-cache speedup. The cache-marker
experiment identified no-cache mount invalidation, rather than assuming GC.
No retry-until-green, extra timeout, skipped assertion or test fix was introduced.
This existing failure is a **merge-readiness blocker**, even though the earlier
host and container full-suite runs passed.

After the cache-invalidation fix, **`make check` passed in 123.86 s**, including
all 34 default race-test packages and full vet. Assets/static builds took 41.3 s
and the race/vet stage 78.9 s. This first after-change sample still redownloaded
ten tool dependencies missing from the previously cleared mounts. The observed
82.05-second reduction from the first successful baseline is descriptive:
different cache histories and one sample per condition do not establish a CI
speedup or resolve the earlier intermittent queued-account failure.

A second **fresh-identifier, warm-cache `make check` passed in 52.40 s**,
including all 34 default race-test packages and full vet. The validation RUN
took 50.6 s and downloaded **zero Go modules**. Its identifier differed from the
first after-change run, and neither run returned cached Go test results.
This is a warm-path measurement, not a retry of a failed after-change check.

| Full local Docker validation | n | Elapsed (s) | Outcome |
| --- | ---: | ---: | --- |
| Before: partial layers, cleared Go mounts, rebuilt assets | 1 | 205.90 | Pass |
| Before: cached assets, no-cache filter cleared Go mounts again | 1 | 122.61 | Fail: queued-account test |
| After: retained race compilation, missing tool/vet cache entries | 1 | 123.86 | Pass |
| After: warm mounts, fresh validation identifier | 1 | 52.40 | Pass |

The observed 153.50-second difference between the first baseline and warm path
includes different cache conditions and must not be presented as an A/B CI
estimate. There are no meaningful local p50/p95 estimates from these samples.
The container environment was Linux ARM64 in Colima, with four CPUs and about
8 GiB RAM. A separate **linux/amd64 runtime image build passed in 36.74 s**;
it was not pushed, and x64 race execution remains unverified locally.

The revised minimal lifecycle test passed with the same builder name, a fresh
Docker configuration directory, different validation identifiers, and
`--keep-state` between builders. It verified that the second RUN executes while
its cache marker survives, and that omitting the identifier fails. The proof's
local configuration exposed the existing Homebrew Buildx plugin directory,
without copying credentials or installing a replacement tool.
The actual application's `validate` target also rejected an omitted identifier
before any test execution.

Workflow YAML parsing and before/after invariants verified unchanged triggers,
runner labels, permissions, concurrency group/non-cancellation, action pins, timeout, step order, default
failure gates and forced validation, plus a single remote cache export. Executing
the preflight JavaScript with test inputs checked stable/syntax-safe builder IDs,
repository/runner isolation, unique Docker configuration directories and
fail-closed package visibility.

All configured workflow checks remain present, with the same action/check names
and default failure propagation. No Go/test/generated source changed, and
`git diff --check` passed. The complete **enabled** suite passed after the
changes, but the existing opt-in integration/browser suites were not enabled.
The baseline queued-account failure correctly failed Docker, and the workflow
still gates publishing on successful validation. **Merge blocking itself cannot
be confirmed** without required-check settings and a PR validation workflow.

Local ARM measurements are not predictions for the Linux x64 runner. No
after-change GitHub run was triggered, so CI after-change p50/p95 and target
achievement remain **unverified**. PR p50/p95 are unavailable, not zero.

Further work, in priority order:

1. Establish safe PR/merge-queue validation and make its complete check required,
   with owner authorization. This is a correctness blocker, not a speed tweak.
2. Diagnose the queued-account failure under constrained race-test execution,
   without retries or relaxed correctness checks. A later passing container check
   does not explain or erase the original intermittent failure.
3. Collect authenticated BuildKit logs and at least 150 new runs where available.
   Compare success, failure and cancellation populations separately; measure
   creation through completion, including cache uploads and cleanup.
4. Check warm-cache compilation versus real test execution before changing test
   scheduling. Profile expensive HTTP/rendering and soak-analysis paths; preserve
   fixtures, assertions, random seeds, iteration counts and isolation.
5. Monitor retained BuildKit disk usage/GC. Avoid transferring a large full Go
   cache through another action unless saved compilation exceeds transfer costs.
   No new service or paid infrastructure is needed for the implemented changes.

References:
[cache mounts in Actions](https://docs.docker.com/build/ci/github-actions/cache/#cache-mounts),
[Buildx keep-state](https://docs.docker.com/reference/cli/docker/buildx/rm/#keep-buildkit-state---keep-state),
[GitHub concurrency queues](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency),
[publishing workflow](../.github/workflows/publish-image.yml).
