# OpenAPI contract changes

## Unreleased

### 2026-10-07 — Private reporting and moderation contract v1.8.0

- Document owner-only receipts/outcomes, private author review requests, authorized
  queue/case reads, and atomic keep/hide/restore with stale-context rejection.
- Add safe hidden-post projections and independent bounded history cursors;
  distinguish shared reasons from moderator-only evidence/notes and author appeals.
  See [moderation](../moderation.md#api).
- Validate the contract with only the two existing health-probe warnings.

### 2026-10-07 — Grouped recommendation contract v1.7.0

- Add `/api/v1/community/recommendations` with event-level pagination, immutable
  first-publication ordering, active counts and three-author previews.
- Preserve individual collections and separate cursor scopes; document withdrawal
  markers, no-bump reactivation and observe-only measurements without quotas.
  See [recommendations](../event-recommendations.md#api).
- Verify strict grouped queries and public projections; lint the 1.7.0 contract
  with only the existing two health-probe warnings.

### 2026-10-07 — Discussion and Helpful contract v1.6.0

- Document root/reply creation and reads, owner edit/removal, independent Helpful,
  and city/category root browsing. Specify public projections, durable retry-key
  retention/conflicts, creation limits, strict bodies, ownership/ancestry, removal
  placeholders, viewer-scoped cursors, and creation snapshot boundaries.
- Keep cookie/bearer identity and CSRF rules aligned with shared services;
  reporting/moderation remain proposed. See [discussions](../event-discussions.md#api).

### 2026-10-06 — Account/save/interest/recommendation contract v1.5.0

- Add own/public profiles, private preferences, browser-only API credential
  management, and current-session/token revocation to the implemented contract.
- Document cookies/bearer credentials, CSRF/origin rules, private projections,
  validation limits, expiration/revocation, and disabled/unavailable account errors;
  keep discovery public. See [accounts](../accounts.md) and [OpenAPI](../openapi.md).
- Add owner-only saved collections/resources/batched state, bounded owner-bound
  cursors, empty-body retry-safe writes and durable snapshot freshness; document
  public detail fallback without bookmark state. See [private saves](../saved-events.md).
- Add independent interest collections/resources/batched states, explicit visibility
  and new-choice defaults, public count/participant/activity reads, scoped keysets,
  and owner-only mutations. See [event interest](../event-interest.md).
- Add public city/category/event/profile recommendations, explicit counts, own
  collections/resources, strict optional-reason replacement, stable publication
  ordering, scoped cursors, and owner-only withdrawal with CSRF/privacy boundaries.
  Correct the obsolete unimplemented-interest note; see [recommendations](../event-recommendations.md).

### 2026-10-04 — OpenAPI guide

- Added an [OpenAPI guide](../openapi.md) explaining the YAML contract, version
  fields, supported endpoints, embedding/publication, tooling, and manual upkeep.
- Linked the guide from the README, documentation index, and discovery API guide;
  distinguished the specification from runtime configuration and code generation.
- Validated the existing contract and documented the two recommended-rule
  warnings for health probes without inventing unsupported 4xx responses.
- Verified the published YAML matches the source in the Go 1.27/Node.js 24
  container and that the contract route rejects query parameters.
