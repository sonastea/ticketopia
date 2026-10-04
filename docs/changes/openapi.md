# OpenAPI contract changes

## 2026-10-04 — Unreleased

- Added an [OpenAPI guide](../openapi.md) explaining the YAML contract, version
  fields, supported endpoints, embedding/publication, tooling, and manual upkeep.
- Linked the guide from the README, documentation index, and discovery API guide;
  distinguished the specification from runtime configuration and code generation.
- Validated the existing contract and documented the two recommended-rule
  warnings for health probes without inventing unsupported 4xx responses.
- Verified the published YAML matches the source in the Go 1.27/Node.js 24
  container and that the contract route rejects query parameters.
