# Authentication and account changes

## Unreleased

### 2026-10-06 — Direct Google sign-in and durable accounts

- Deliver direct Google code/PKCE/OIDC sign-in using pinned `x/oauth2` and
  `go-oidc`, with browser-bound, atomic one-time flows and verified identity claims.
- Store issuer/subject identities, hashed expiring sessions/API credentials,
  profiles/privacy defaults, and private preferences in MariaDB schema v2.
- Add profile/preferences/interests/public-profile screens, no-JavaScript forms,
  owner-only JSON endpoints, CSRF/origin checks, shared limits, and revocation.
- Brand the Google sign-in CTA with Google's current gradient logo, locally hosted
  Google Sans, and blue-filled button treatment with a white logo tile; retain
  accessible no-JS OAuth navigation and disabled-account behavior. See
  [accounts](../accounts.md).
- Keep discovery public/database-optional and other milestones truthful; default
  event-interest visibility private, notifications paused, and public projection
  limited to ID/name/bio. Use account city only after URL/browser preferences.
- Document setup, Google consent/callback requirements, grants, and the v1-to-v2
  drain/migrate/start boundary; see [accounts](../accounts.md) and
  [persistence](../persistence.md#rolling-update-compatibility).
- Verify signed local provider fixtures, real-MariaDB ownership/restart/
  concurrency/replay/limits, plus 42 browser checks and 13 zero-violation automated
  accessibility views. Real Google consent requires operator-supplied credentials.
- Pass the production asset/application build, full real-MariaDB race suite, vet,
  and OpenAPI lint (only the two existing probe warnings). The independent UI
  reviewer's account-status documentation fix was scored resolved; preserve the
  incumbent design tokens and update its prose/sidecar guardrail consistently.
