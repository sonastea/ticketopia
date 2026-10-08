# Event community changes

[Older history](archive/event-communities/).

## Unreleased

### 2026-10-07 — Private reporting, review, and durable moderator roles

- Deliver private report receipts/status, moderator queue/context, shared reasons
  with private notes, reversible hiding/restoration, and author second reviews.
  Preserve replies and prevent hidden-text editing or stale/unseen-context decisions.
- Store moderator membership by stable account ID, with audited operator grant/revoke
  commands; deny role-table writes to runtime and make revocation effective without
  restart. No default admin, email authorization, admin web UI, or notifications.
- Add schema 8, OpenAPI 1.8.0, native forms/draft recovery, and bounded independent
  histories. See [moderation](../moderation.md) for bootstrap, retention and upgrade.
- Verify the full race suite against MariaDB, vet/build/lint, schema-7 preservation,
  races/rollback/privacy, and 51 Chromium outcomes with 13 clean axe captures.
  This completes basic reporting/review, not public-pilot or production certification.
- Finish review scores all three fixes resolved: protect case evidence from
  moderator-authors, identify personal records/direct targets, and document delivery.

### 2026-10-07 — Grouped recommendations and observe-only protection

- Show one Community entry per event, active counts, up to three named reasons,
  and a full-list link. Keep event/profile individual attribution and APIs compatible.
- Preserve immutable event and account/event publication positions through edits,
  additional authors and all-withdrawn republication; clear withdrawn reason text.
- Record committed first-publication/reactivation counts in bounded ten-minute
  windows and lifetime totals. Exclude edits/retries; add no quotas, cooldowns,
  IP grouping or automatic sanctions. See [recommendations](../event-recommendations.md).
- Add the grouped API in OpenAPI 1.7.0. Reporting/moderation still gates a pilot.
- Verify real-MariaDB races, vet/build/lint and 82 Chromium outcomes with 11
  zero-violation axe captures; the scoped grouped-feed review disposition is ship.

### 2026-10-07 — Durable event discussions and Helpful

- Deliver public questions/tips, independently paginated replies, same-thread
  reply targets, author editing/removal, and reversible Helpful acknowledgments.
- Preserve event snapshots, ancestry, retry keys, and removed reply context across
  restarts/provider outages; keep saves, interest, and recommendations independent.
- Add event/preview composers, shareable threads, native draft recovery, scoped
  enhanced drafts, and separate city/category conversation browsing. Extend
  OpenAPI to 1.6.0; see [discussions](../event-discussions.md).
- Reporting/moderation and discussion notifications remain planned. Moderation
  remains required before the first public community pilot.
- Verify the full Go race suite with real MariaDB, vet, production build, OpenAPI
  lint, and 24 native/enhanced Chromium outcomes. Exercise five responsive thread
  captures with zero axe violations; do not claim whole-application certification.
- Resolve the independent review's transport-error copy finding: uncertain writes
  preserve drafts/retry keys and explain confirmation uncertainty rather than
  claiming nothing was saved. Final verdict marks that scored fix resolved.

### 2026-10-06 — Interested visibility and public recommendations

- Implement durable, reversible event interest independently from saves, category
  preferences, endorsements and attendance. Keep identity private by default;
  profile defaults affect only new choices. See [event interest](../event-interest.md).
- Deliver shared owner APIs, public aggregate counts, own collection/visibility
  management, and public-opt-in participants/profile activity with bounded keysets.
- Add native/enhanced discovery, preview, Saved and event controls with distinct
  selected state/counts, pending/error feedback, and immediate initiating-panel
  refresh after visibility revocation. Preserve private saves and retained details.
- Verify ownership/CSRF/validation, concurrency/rollback, privacy/keysets, migration
  preservation/restarts, Chromium desktop/tablet/mobile/native/error journeys and
  automated accessibility. Discussions/moderation remain planned.
- Resolve the independent review's label-in-name and participant-return findings;
  verify 54 Chromium checks and seven zero-violation accessibility captures, the
  full Go race suite with real MariaDB, vet, production build, and OpenAPI lint.
- Simplify repeated interest state: keep privacy on the button, place **Change
  visibility** beside a more readable count, and close the editor after saving.
  Announce success without lingering paragraphs; show one dismissible error and
  clear stale feedback on retry/new actions/history restore. Preserve native forms,
  per-event visibility, private defaults, and focus continuity.
- Verify the refinement with 84 Chromium checks/ten zero-violation axe captures,
  targeted view/API race tests and vet, production build, and in-thread responsive
  inspection. Only pre-existing detector notices remain.
- Deliver public recommendations independently of Save/Interested: one per
  account/event, optional 500-character reason, stable publication/edit times,
  ownership/CSRF, and retry-safe edit/withdrawal. See [recommendations](../event-recommendations.md).
- Add event/public-profile attribution, explicit counts, recent city/category
  browsing, native/enhanced forms with preserved drafts, and paginated event/return
  context. Keep discussions/moderation truthful and require moderation before a pilot.
- Verify SQL/API race tests, concurrency/rollback, migration preservation/restarts,
  scoped keysets/privacy, and 62 Chromium checks with eight accessibility scans
  across desktop/tablet/phone/200% text. Fix enlarged-text recommendation wrapping.
- Address the independent review's direct-open, native credential-failure draft,
  and obsolete-availability findings; verify rejected writes remain unapplied and
  native credential recovery retains safe event/return context. The reviewer scores
  all four listed fixes resolved; existing ticket-button clipping at 200% remains
  outside that scoped verdict.
