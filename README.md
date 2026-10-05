# Ticketopia

An early-stage event discovery and community app built with Go, Echo, templ, htmx, and
shadcn-templ with Tailwind CSS, using the Ticketmaster Discovery API.

Discover events across Ticketmaster categories in a detected or remembered city,
with editable filters, image-led results, contextual event previews, dedicated event pages, and load-more
pagination. Cached event data is shared by the responsive web UI and JSON API.

Discovery includes category-aware genre filters for music, sports, arts, and more.
The planned MVP connects events to private saves, social interest, recommendations,
and asynchronous discussions. See the [design guidelines](docs/design-guidelines.md)
for desktop, tablet, and mobile direction and the
[MVP boundary](docs/goals/delivery.md#mvp-release-boundary) for release scope.

## Goals

Track delivery here: check a milestone once it works for users and its behavior
has been verified. The linked goals and designs describe the expected outcomes.
The [backend and frontend delivery checklist](docs/goals/delivery.md) maps each
goal to implementation work and verification checkpoints.

### Event data foundation

[Discovery guide](docs/discovery.md) · [Cache behavior](docs/cache.md)

- [x] Fetch Ticketmaster events with artist, venue, date, status, and price metadata.
- [x] Load cached category and genre/subgenre catalogs for discovery filters.
- [x] Reuse cached data across clients with concurrent-request deduplication,
  stale fallback, and bounded external request usage.
- [x] Provide a searchable, responsive event list with correct ticket links and
  clear loading, empty, error, and freshness states.
- [ ] Persist observations and schedule budgeted city/date refreshes in MariaDB.
- [x] Discover across all Ticketmaster event categories with compatible filters.

### Personal radar

[Goal details](docs/goals/personal-radar.md)

- [x] Find nearby events by city with clear dates, venues, and known prices.
- [x] Start with an approximate IP-based or remembered city, change it easily,
  and choose a city when detection is unavailable.
- [ ] Validate relevance with people in a pilot city.
- [ ] Save an event and find it again on a later visit.
- [ ] Sign in, manage a profile/privacy preferences, and keep saved events private.
- [ ] Follow artists and venues.
- [ ] See personalized matches with understandable reasons.

### Useful reminders

[Reminder goals](docs/goals/personal-radar.md#reminder-promises)

- [ ] Receive a timely public on-sale reminder for a saved event.
- [ ] Receive meaningful date, venue, cancellation, or postponement updates.
- [ ] Receive a weekly discovery digest.
- [ ] Change or pause reminders and choose notification preferences.

### Positive event communities

[Community goals](docs/goals/event-communities.md)

- [ ] Mark an event Interested and change or remove that choice.
- [ ] Choose public interest visibility; keep interest identity private by default.
- [ ] Recommend an event publicly, optionally explain why, and edit or withdraw it.
- [ ] Browse local/category recommendations and event conversations.
- [ ] Later: Mark an event Going and choose participation visibility.
- [ ] Post a question, tip, or comment on an event.
- [ ] Reply to another person's contribution.
- [ ] Edit or remove your own comments and replies.
- [ ] Give or remove a positive Helpful thumbs-up.
- [ ] Later: Follow a discussion and receive relevant activity updates.
- [ ] Report a concern privately and receive acknowledgment.
- [ ] Review reports as a moderator and communicate clear outcomes.
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
- [ ] Select an event into a contextual desktop discussion panel and expand details.
- [ ] Use adaptive tablet navigation and readable primary/context views.
- [ ] Discover, save, express interest, recommend, and discuss through complete
  single-column mobile screens with bottom navigation and threaded replies.
- [ ] Preserve event context, accessible navigation, and return state across devices.

### Reusable API

[API design](docs/design/radar-api.md)

- [x] Read events, their metadata, categories, and genres through the API.
- [ ] Read personalized radar results through the API.
- [ ] Manage personal and community activity from another client.
- [x] Provide an OpenAPI description for implemented endpoints.

### MariaDB persistence

[Database foundation/setup](docs/persistence.md) · [Database design](docs/design/database.md)

- [x] Establish MariaDB connections, serialized migrations, and minimal durable event identity.
- [ ] Keep preferences, saved events, and event history across restarts.
- [ ] Persist community activity, moderation records, and notification jobs.
- [ ] Share durable state across application replicas with operator-managed MariaDB.
- [ ] Back up and successfully restore application data.

The optional foundation uses MariaDB through `database/sql` and pinned
`go-sql-driver/mysql`, with embedded Goose migrations and `mariadb-operator`
deployment examples. Discovery stays database-free by default; broader durable
features and exercised multi-pod deployment remain planned.

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
optional `.env` file.

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
