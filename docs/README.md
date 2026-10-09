# Documentation

The root [README](../README.md) covers the project overview and quick start.
Detailed setup, product goals, and design notes live here. Plans are labeled with
their status; they describe intended work rather than available features.

## Goals and design

- [Implemented visual system](../DESIGN.md): tokens, typography, components, and
  the plum/light-stone visual direction, event imagery, and dusty-rose date indexes.
- [Responsive UX and interaction guidelines](design-guidelines.md): current
  navigation and responsive behavior, shared state, participation semantics,
  accessibility, and explicitly planned community interactions.
- [Backend and frontend delivery checklist](goals/delivery.md): aligned product
  outcomes, implementation tasks, and verification checkpoints.
- [Personal radar and reminders](goals/personal-radar.md): core discovery and
  reminder goals, with small user outcomes and pilot checkpoints.
- [Positive event communities](goals/event-communities.md): MVP interest,
  recommendations, threaded discussions, Helpful reactions, reports/moderation,
  and retained later attendance and notification goals.
- [Radar API](design/radar-api.md): a proposed shared backend for web, mobile,
  and other clients, including community participation.
- [Database plan](design/database.md): MariaDB foundation and broader planned
  shared application state with `mariadb-operator` in Kubernetes.
- [Venue insights](ideas/venue-insights-and-discovery.md): exploratory uses of
  event history for venue and price analysis.
- [Discovery games](ideas/discovery-games.md): passport, pick swaps, and other
  hypotheses for meaningful repeat visits and social interaction.

## Current features

- [Followed discussions](followed-discussions.md): private conversation subscriptions,
  frequency-controlled in-app reply updates and visibility-safe exact reply links.
- [Personalized radar](radar.md): private nearby matches from follows/category
  preferences, structured explanations, first-run/freshness states and versioned pagination.
- [Private artist and venue follows](follows.md): authenticated name search,
  follow/unfollow, durable owner-only collections and equivalent APIs.
- [Reliable event history](event-history.md): durable artist/venue catalogs,
  database-backed Discover and coverage-aware fallback, dated public detail
  history, missing-event refreshes, collection receipts and shared provider accounting.
- [Private reporting and moderation](moderation.md): report receipts, moderator
  keep/hide/restore, author outcomes/second reviews, durable roles, and audited
  operator grant/revoke commands.
- [Event discussions](event-discussions.md): public questions, threaded replies,
  contextual discovery-to-discussion journeys, owner edit/removal, Helpful,
  direct expansion links, and city/category browsing.
- [Public event recommendations](event-recommendations.md): optional reasons,
  edit/withdraw, public event/profile attribution, grouped city/category browsing,
  durable anti-bumping, and observe-only participation measurements.
- [Event interest](event-interest.md): independent Interested choices, private-by-default
  visibility, aggregate counts, own collections, and public-opt-in participants/profile activity.
- [Private saved events](saved-events.md): durable owner-only bookmarks, web/API
  save/remove controls, paginated Saved collection, and last-known event snapshots.
- [Google sign-in and accounts](accounts.md): provider setup, profiles/privacy,
  durable preferences, browser sessions, and revocable personal API tokens.
- [MariaDB foundation](persistence.md): explicit connections, TLS/readiness,
  serialized migrations/recovery, durable event identity, and local/operator examples.
- [OpenAPI contract](openapi.md): API specification format, published YAML,
  tooling, and maintenance alongside Go handlers.
- [Container deployment](deployment.md): Docker builds, Coolify settings, health
  checks, graceful shutdown, Kubernetes probes, local pre-commit Docker validation,
  and private GHCR publishing with a self-hosted GitHub Actions runner.
- [Kubernetes bootstrap and handoff prompt](kubernetes-bootstrap.md): secure Secret
  provisioning, operator grants, and continuing an existing Cilium/Gateway setup.
- [UI components](ui-components.md): shadcn-templ installation, themed primitives,
  Tailwind v4 builds, progressive enhancement, and component updates.
- [Ticketmaster discovery](discovery.md): event/metadata fetching, JSON API,
  frontend search, request budgets, and verification.
- [Location-aware discovery](location.md): editable IP/remembered city defaults,
  lookup caching, manual fallback, and trusted-proxy configuration.
- [Cache configuration](cache.md): backends, environment variables, expiration,
  fallback behavior, and NATS setup.
- [Product context](../PRODUCT.md): agreed audience, web platform, current UI scope,
  and accessibility baseline.

## History

- [Recent feature changes](CHANGELOG.md): a short index with one link per feature.
- [All feature histories](changes/): related changes stay in the same feature
  document, with older entries archived under that feature when needed.

## Documenting future changes

1. Add or update a focused feature guide and link it above. Guides describe the
   current behavior.
2. Update `changes/<feature>.md` for the feature being worked on. Related ongoing
   changes belong in the same dated **Unreleased** entry; revise or extend its
   summary rather than create a document for each edit. Add a dated entry for a
   distinct milestone, and record shared infrastructure under its own feature.
3. Link to the relevant goals/designs for detail. When publishing a release,
   move its entries under the release version and date, preserving their history.
4. Update that feature's single line in `CHANGELOG.md` with its latest change
   date. Keep the 20 most recently updated features, newest first. Feature files
   remain available after their links age out of the recent index.
5. Keep each feature history within 100 lines. Move older completed entries into
   `changes/archive/<feature>/YYYY-part-NN.md` files, each within 100 lines, retaining
   their dates/releases. Link to that archive directory from the feature history
   once it exists. Detailed explanations belong in the feature guides.

For example, cache changes go in `changes/cache.md`, community changes in
`changes/event-communities.md`, and persistence changes in `changes/database.md`.

Read guides for current setup and behavior. When history is needed, open the
matching feature file and search its archives only if necessary. Routine work
does not require loading the history of other features.
