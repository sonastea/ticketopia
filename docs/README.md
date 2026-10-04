# Documentation

The root [README](../README.md) covers the project overview and quick start.
Detailed setup, product goals, and design notes live here. Plans are labeled with
their status; they describe intended work rather than available features.

## Goals and design

- [Implemented visual system](../DESIGN.md): tokens, typography, components, and
  the cobalt/record-shop/mixtape visual direction.
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
- [Database plan](design/database.md): SQLite first, durable application state,
  and when to consider a migration to PlanetScale.
- [Venue insights](ideas/venue-insights-and-discovery.md): exploratory uses of
  event history for venue and price analysis.
- [Discovery games](ideas/discovery-games.md): passport, pick swaps, and other
  hypotheses for meaningful repeat visits and social interaction.

## Current features

- [OpenAPI contract](openapi.md): API specification format, published YAML,
  tooling, and maintenance alongside Go handlers.
- [Container deployment](deployment.md): Docker builds, Coolify settings, health
  checks, graceful shutdown, and Kubernetes probes.
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
