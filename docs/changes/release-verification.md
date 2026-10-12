# Release verification and scope

## Unreleased

### 2026-10-12 — Use, refine, and verify

- Treat discovery and neighboring-screen cohesion as complete; focus on everyday
  app use, small nuisances and flow/copy fixes rather than another feature sprint.
- Remove weekly discovery digests and additional sale/price/event-change emails
  from active scope; retain implemented opt-in on-sale delivery, in-app history
  and discussion replies. Ticket-provider transactional notifications are not an app obligation.
- Keep manual device/cross-browser/screen-reader and small-pilot checks open.
  Verify live SES only if enabled; defer cluster replica/restart/upgrade/migration,
  runner publishing and populated-data backup/restore checks until Kubernetes deployment.
- Correct followed-discussion checklist status and schema-13 upgrade guidance;
  no cluster deployment, recovery rehearsal or human-pilot approval is claimed.
- See [delivery priorities](../goals/delivery.md#current-focus-use-refine-and-verify),
  [product scope](../../PRODUCT.md) and [schema rollout](../persistence.md#rolling-update-compatibility).
