# Artist and venue follow changes

## Unreleased

### 2026-10-08 — Private artist and venue follows

- Add authenticated, cached artist/venue name search and ordinary web Follow/Unfollow
  forms with an owner-only, filtered/paginated collection. Guests return to the task
  after sign-in; disabled accounts and empty/provider-error states are explicit.
- Persist case-sensitive artist/venue follows in schema 10, sharing the existing
  catalogs/provider mappings. Preserve retry timestamps, atomicity, cross-session
  continuity and provider-free collection/removal. Keep follows off public profiles.
- Expose equivalent session/bearer APIs with owner scoping, cookie CSRF and no-store
  responses, and publish OpenAPI 1.9.0. Add grants/access checks and migration guidance.
- Verify with service/provider/API regressions and real MariaDB ownership, concurrency,
  keyset, rollback, retained metadata, restart and upgrade tests. See
  [follows](../follows.md) for behavior and browser verification.
- The real-MariaDB full Go race suite, vet, production asset/binary build and OpenAPI
  lint pass (two existing probe warnings). Chromium verifies 49 checks and six
  zero-violation automated accessibility scans across native/no-JS/touch/keyboard,
  desktop/tablet/mobile/320px, enlarged text and outage recovery.
- Explicitly label missing venue locations in search and retained follows; add
  rendering and browser regressions and resolve the finish review's sole UI finding.
- `make check` also passes the publishing-context Docker generation/build/race/vet gate.
- Personalized matching, notifications and public-pilot approval remain out of scope.
