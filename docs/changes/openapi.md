# OpenAPI contract changes

## Unreleased

### 2026-10-06 — Account contract v1.2.0

- Add own/public profiles, private preferences, browser-only API credential
  management, and current-session/token revocation to the implemented contract.
- Document cookies/bearer credentials, CSRF/origin rules, private projections,
  validation limits, expiration/revocation, and disabled/unavailable account errors;
  keep discovery public. See [accounts](../accounts.md) and [OpenAPI](../openapi.md).

### 2026-10-04 — OpenAPI guide

- Added an [OpenAPI guide](../openapi.md) explaining the YAML contract, version
  fields, supported endpoints, embedding/publication, tooling, and manual upkeep.
- Linked the guide from the README, documentation index, and discovery API guide;
  distinguished the specification from runtime configuration and code generation.
- Validated the existing contract and documented the two recommended-rule
  warnings for health probes without inventing unsupported 4xx responses.
- Verified the published YAML matches the source in the Go 1.27/Node.js 24
  container and that the contract route rejects query parameters.
