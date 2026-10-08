# Ticketopia

An early-stage event discovery and community app built with Go, Echo, templ, htmx, and
shadcn-templ with Tailwind CSS, using the Ticketmaster Discovery API.

Discover events across Ticketmaster categories in a detected or remembered city,
with editable filters, image-led results, contextual event previews, dedicated event pages, and load-more
pagination. Cached event data is shared by the responsive web UI and JSON API.

Discovery includes category-aware genre filters for music, sports, arts, and more.
Private saves, artist/venue follows, Interested, and public recommendations connect people to events;
asynchronous discussions add questions, replies, and Helpful acknowledgments.
Private reporting and moderator review close the safety loop. See the [design guidelines](docs/design-guidelines.md)
for desktop, tablet, and mobile direction and the
[MVP boundary](docs/goals/delivery.md#mvp-release-boundary) for release scope.

## Goals

Track delivery here: check a milestone once it works for users and its behavior
has been verified. The linked goals and designs describe the expected outcomes.
The [backend and frontend delivery checklist](docs/goals/delivery.md) maps each
goal to implementation work and verification checkpoints.

### Event data foundation

[Discovery guide](docs/discovery.md) · [Cache behavior](docs/cache.md) · [Reliable history](docs/event-history.md)

- [x] Fetch Ticketmaster events with artist, venue, date, status, and price metadata.
- [x] Load cached category and genre/subgenre catalogs for discovery filters.
- [x] Reuse cached data across clients with concurrent-request deduplication,
  stale fallback, and bounded external request usage.
- [x] Provide a searchable, responsive event list with correct ticket links and
  clear loading, empty, error, and freshness states.
- [x] Persist observations and schedule budgeted city/date refreshes in MariaDB.
- [x] Power Discover/API with the durable catalog, coverage-aware fallback, and dated event history.
- [x] Discover across all Ticketmaster event categories with compatible filters.

### Personal radar

[Goal details](docs/goals/personal-radar.md)

- [x] Find nearby events by city with clear dates, venues, and known prices.
- [x] Start with an approximate IP-based or remembered city, change it easily,
  and choose a city when detection is unavailable.
- [ ] Validate relevance with people in a pilot city.
- [x] Save an event and find it again on a later visit.
- [x] Sign in with Google and manage a profile, privacy defaults, and private preferences.
- [x] Keep saved events private.
- [x] Follow artists and venues.
- [ ] See personalized matches with understandable reasons.

### Useful reminders

[Reminder goals](docs/goals/personal-radar.md#reminder-promises)

- [ ] Receive a timely public on-sale reminder for a saved event.
- [ ] Receive meaningful date, venue, cancellation, or postponement updates.
- [ ] Receive a weekly discovery digest.
- [ ] Change or pause reminders and choose notification preferences.

### Positive event communities

[Community goals](docs/goals/event-communities.md)

- [x] Mark an event Interested and change or remove that choice.
- [x] Choose public interest visibility; keep interest identity private by default.
- [x] Recommend an event publicly, optionally explain why, and edit or withdraw it.
- [x] Browse event-grouped recommendations by city/category and individual event/profile endorsements.
- [x] Prevent withdrawal/republication feed bumping without participation quotas.
- [x] Browse event conversations alongside recommendations.
- [ ] Later: Mark an event Going and choose participation visibility.
- [x] Post a question, tip, or comment on an event.
- [x] Reply to another person's contribution.
- [x] Edit or remove your own comments and replies.
- [x] Give or remove a positive Helpful thumbs-up.
- [ ] Later: Follow a discussion and receive relevant activity updates.
- [x] Report a concern privately and receive acknowledgment.
- [x] Review reports as a moderator and communicate clear outcomes.
- [ ] Later: Record Went and share a post-event reflection.

### Responsive event and discussion experience

[Screen and interaction guidelines](docs/design-guidelines.md)

- [x] Browse with a desktop sidebar, intermediate labeled rail, and mobile bottom navigation.
- [x] Collapse the desktop sidebar and rearrange sections/destinations with
  browser-local preferences.
- [x] Use themed shadcn-templ components throughout the existing responsive UI.
- [x] Preview real event details alongside results and open a dedicated event page.
- [x] Share/reload selected events and sections, recover local preview failures,
  and return to results with filters and available browser history state.
- [x] Select an event into a contextual desktop discussion panel and expand details.
- [x] Use adaptive tablet navigation and readable primary/context views.
- [x] Discover, save, express interest, recommend, and discuss through complete
  single-column mobile screens with bottom navigation and threaded replies.
- [x] Preserve event context, accessible navigation, and return state across responsive layouts.

Responsive journeys are verified with isolated Chromium fixtures, keyboard/touch,
320px/200%-text and automated accessibility checks. History and drafts stay in the
current browser/tab; manual screen-reader and physical-device checks remain in the
[delivery verification checklist](docs/goals/delivery.md#6-responsive-discovery-to-discussion-experience).

### Reusable API

[API design](docs/design/radar-api.md)

- [x] Read events, their metadata, categories, and genres through the API.
- [ ] Read personalized radar results through the API.
- [ ] Manage personal and community activity from another client.
- [x] Provide an OpenAPI description for implemented endpoints.

### MariaDB persistence

[Database foundation/setup](docs/persistence.md) · [Database design](docs/design/database.md)

- [x] Establish MariaDB connections, serialized migrations, and minimal durable event identity.
- [x] Keep accounts, credentials, and private preferences across restarts.
- [x] Keep private artist/venue follows and last-known metadata across restarts.
- [x] Keep saved events and last-known event details across restarts.
- [x] Keep event interest and its visibility across restarts.
- [x] Keep public recommendations, reasons, and original publication times across restarts.
- [x] Keep dated event observation history across restarts.
- [x] Keep event discussions and Helpful reactions across restarts.
- [x] Persist private reports, moderation decisions, author review requests, and moderator roles/audits.
- [ ] Persist notification jobs.
- [ ] Share durable state across application replicas with operator-managed MariaDB.
- [ ] Back up and successfully restore application data.

The optional foundation uses MariaDB through `database/sql` and pinned
`go-sql-driver/mysql`, with embedded Goose migrations and `mariadb-operator`
deployment examples. Discovery stays database-free by default; broader durable
features and exercised multi-pod deployment remain planned. Opt-in
[Google accounts](docs/accounts.md) store profiles, preferences, and credentials;
[private saves](docs/saved-events.md) retain bookmarks and last-known event details.
[Private follows](docs/follows.md) add artist/venue search and owner-only collections.
[Interested](docs/event-interest.md) keeps separate choices, private-by-default identities,
aggregate counts, and explicitly public participant/profile activity.
[Public recommendations](docs/event-recommendations.md) provide separate endorsements,
optional reasons, edit/withdrawal, public attribution, grouped city/category browsing,
durable anti-bumping, and observe-only activity measurements without automatic quotas.
[Event discussions](docs/event-discussions.md) add durable public questions,
threaded replies, author edit/removal, and independent Helpful reactions.

### Container deployment

[Deployment guide](docs/deployment.md)

- [x] Build and run a non-root container with embedded assets, health checks, and
  graceful shutdown.
- [x] Identify the deployed source revision in startup logs using runtime `SOURCE_COMMIT`.
- [x] Publish a private amd64 app image to GHCR with authenticated pulls.
- [ ] Verify automated GHCR publishing on the self-hosted GitHub Actions runner
  ([workflow setup](docs/deployment.md#github-actions-on-a-self-hosted-runner)).

### Optional explorations

Optional explorations include a [discovery passport and games](docs/ideas/discovery-games.md)
and [venue/price insights](docs/ideas/venue-insights-and-discovery.md).

## Run locally

Requires Go 1.27+ and a [Ticketmaster API key](https://developer.ticketmaster.com/).

```sh
export TICKETMASTER_KEY=your-api-key
go run ./cmd/ticketopia
```

Open <http://localhost:8080>. Environment variables can also be placed in an
optional `.env` file. [`.env.example`](.env.example) documents deployment settings
for all implemented features; it enables MariaDB and Google accounts, so replace
its placeholders and complete the [deployment setup](docs/deployment.md#environment-template)
before using it. For database-free discovery, set `PERSISTENCE_MODE=disabled` and
`AUTH_ENABLED=false`.

JSON reads start at `/api/v1/events`, `/api/v1/categories`, and `/api/v1/genres`.
The [OpenAPI contract](docs/openapi.md) describes the HTTP API's routes, parameters,
and responses and is served at `/api/v1/openapi.yaml`. See the
[discovery guide](docs/discovery.md) for filters and `TICKETMASTER_DAILY_BUDGET`
configuration.

Localhost starts with a city prompt. Public visitors can receive an approximate
IP-based city; see [location setup](docs/location.md) for the optional lookup
toggle and trusted reverse-proxy configuration.

The cache runs in memory by default. Shared backends are optional; see
[cache configuration](docs/cache.md).

MariaDB is opt-in with `PERSISTENCE_MODE=mariadb`; see the [persistence guide](docs/persistence.md)
for the local database, verified TLS, separate migration command, and SQL tests.
Run `make db-setup` to prepare local MariaDB, apply migrations and runtime grants,
and verify required permissions. It is safe to rerun after migration changes;
normal app restarts do not require it. `make migrate` applies only schema migrations
to an existing database using your environment/`.env` settings and migration credentials;
it does not start containers or apply runtime grants.

Accounts are separately opt-in with `AUTH_ENABLED=true`, `AUTH_BASE_URL`,
`GOOGLE_CLIENT_ID`, and `GOOGLE_CLIENT_SECRET`; see [Google sign-in setup](docs/accounts.md).
Discovery stays public. Private saves, Interested, and public recommendations are
available with accounts enabled, along with event discussions. Private reporting and
moderation are implemented for discussion posts/replies, with receipts, reversible hiding,
author outcomes, and durable moderator roles. See [moderation](docs/moderation.md)
for the operator grant/revoke CLI. [Reliable event history](docs/event-history.md)
adds dated changes, opt-in city/date collection and shared provider budgets.
The current binary requires schema 11 with stored Discover/coverage and
[private artist/venue follows](docs/follows.md).
Drain older binaries, migrate and reapply grants before starting this version.
This is not public-pilot approval.

## Run in a container

```sh
docker build -t ticketopia:local .
docker run --rm -p 8080:8080 -e TICKETMASTER_KEY ticketopia:local
```

Export `TICKETMASTER_KEY` first as above. The image builds and embeds frontend
assets, runs as non-root, and includes a curl-free liveness check on port 8080.
Use the Dockerfile build strategy in Coolify; no custom build/start command is
needed. See [container deployment](docs/deployment.md) for runtime configuration,
health endpoints, shutdown behavior, and Kubernetes probes.

## Development

Use Go 1.27+ and Node.js 24 with npm for the Tailwind v4 asset build. `go.mod`
declares the Go requirement; `.nvmrc` and the npm engine declaration select Node
24. With nvm, run `nvm install` and `nvm use` first. Install the pinned dependencies
and build templates, component scripts, CSS, and the Go binary:

```sh
npm ci
npm run build
```

Run `./bin/ticketopia` or restart `go run ./cmd/ticketopia` after rebuilding;
browser assets are embedded in the Go binary. Add library components with:

```sh
go tool shadcn-templ add textarea
npm run build
```

See the [component guide](docs/ui-components.md) for build outputs, theme setup,
and local component adaptations, [DESIGN.md](DESIGN.md) for the visual system, and the
[UI guide](docs/discovery.md#responsive-navigation-and-event-details) for routes
and current feature availability.

Verify backend behavior with `go test -race ./...` and `go vet ./...`.

See [documentation and change history](docs/README.md) for feature guides and
project updates.
