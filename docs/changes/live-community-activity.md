# Live community activity changes

## Unreleased

### 2026-11-23 — Shared event totals on one replica

- Event activity totals (`/api/v1/events/activity-counts` and stream counts) are
  shared for 10 seconds per replica; only events without a current total are
  counted. A recommendation or interest write in the same process ends sharing
  at once. See the [rollout decision](../live-activity-rollout.md).

### 2026-10-29 — Reconcile membership once per candidate generation

- Private deliveries skip membership reconciliation when the candidate generation
  is unchanged, instead of rescanning the whole window for every tab each second.
  With 256 tabs on one account, a warm tick at 10,000 candidates fell from about
  139 ms to 1.6 ms and no longer grows with the window. See
  [private connections](../live-community-activity.md#private-connections-part-4).

### 2026-10-28 — Membership SQL failures close held streams

- An SQL error during a private membership rebuild now always ends the
  10-second hold of the last complete view: a later successful batch, a
  time-boxed revision read, or a failed final revision check no longer clears
  or skips it. See [private connections](../live-community-activity.md#private-connections-part-4).

### 2026-10-27 — Preserve pending filter edits and fresh previews

- Preserve unapplied live-filter drafts across background refreshes and Apply
  confirmation (`013bc65`), including undoing a selection while Apply or Reset is
  pending, or when another tab's refresh lands first. Keep the confirmed summary
  separate and explain when newer edits need Apply.
- Allow Discover toasts beside its inline nonmodal filter dialog; actual modal
  dialogs and ARIA-modal surfaces still suppress them.
- Merge the latest loaded-page cursor when preview/section navigation commits;
  invalidate unclaimed preview prefetches around private writes and browser-cache
  restores, and refetch a claimed preview when a write finished during its
  request, to avoid stale Save/Interested controls. Add browser regressions for these races and correct
  collapsed coverage assertions and the pre-submit spinner-size baseline.
  See [filter behavior](../live-community-activity.md#city-and-event-type-filters-part-2).

### 2026-10-22 — Happening Now moves into Community

- For the pilot, remove Happening Now from the desktop sidebar, intermediate rail
  and bottom bar: its 10-minute window is usually empty. It becomes a Community
  tab, keeps Community current in main navigation, and remains linked from toasts
  and Preferences. The bottom bar returns to Discover/Saved/Community/Profile.
- The empty feed explains the window and links to earlier Community
  recommendations; the duplicate Explore Community link is gone.
  See [navigation](../live-community-activity.md#attribution-and-controls).

### 2026-10-22 — Part 5: soak write-gate diagnosis

- Extend the interleaved soak phase to ABBA×4, save samples on failure, and record
  r0 delivery ticks and CPU. Join each probe request to nginx's probe-only timing log
  and, with `LIVE_ACTIVITY_LOAD_TRACE`, to r0's Go execution trace (tagged builds
  only; production marks request tasks and SQL/notification regions).
- Index private streams by owner and keep delivery's subscriber scans outside the
  activity mutex, so membership writes no longer wait for O(streams) work (loaded
  notification 0.035 → 0.003 ms mean).
- Open: the write gate fails on this fixture (ABBA×4 1.160×, 95% CI 1.105–1.226), and
  the 1.10 threshold is unchanged. The remaining shift is mostly SQL time under
  concurrent read load; it needs validation on representative Linux infrastructure.
  The diagnostics are evidence against CPU saturation and delivery-aligned bursts;
  they do not eliminate CPU, database or platform effects. Ingress has not been run.
  See the [rollout record](../live-activity-rollout.md#p13-write-gate-request-level-diagnosis--2026-10-22).
- Splice cached view encodings and write them directly to SSE frames
  (benchmarks: 28 µs → 1.1 µs per generation re-encode, 28 µs → 0 per frame). Drop the
  projection's redundant per-contribution `accounts` join (EXPLAIN: identical rows,
  −9–15% with past-recommendation history). Count GETs stay uncached for
  post-write freshness. Gate-of-record rerun: write 1.108× (CI 1.076–1.139,
  inconclusive), still failed; browse 1.031×. See the
  [rerun](../live-activity-rollout.md#p13-encoding-projection-and-gate-rerun--2026-10-22).
- Add a diagnostic ablation phase (`LIVE_ACTIVITY_ABLATION`, tagged builds only) that
  replays projection and/or count SQL under full load, plus a
  [representative Linux runbook](../live-activity-rollout.md#representative-linux-run).
- On a Linux (WSL2) host, the unchanged soak gate passes: write 1.016× (CI 1.003–1.026),
  browse 1.030×. Accepted as representative (assuming production is similar or better):
  P1.3 met. Ablation finds no measurable effect from projection or count SQL. See the
  [Linux results](../live-activity-rollout.md#linux-wsl2-gate-and-ablation--2026-10-23).

Older entries: [archive](archive/live-community-activity/).
