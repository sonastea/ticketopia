# Ticketopia

An early-stage event discovery and community app built with Go, Echo, templ, htmx, and
Tailwind CSS, using the Ticketmaster Discovery API.

Discover music events in a detected or remembered city, with editable filters,
image-led results, contextual event previews, dedicated event pages, and load-more
pagination. Cached event data is shared by the responsive web UI and JSON API.

Current discovery covers music. The planned MVP expands to all Ticketmaster event
categories and connects events to private saves, social interest, recommendations,
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
- [x] Load a cached music genre/subgenre catalog for discovery filters.
- [x] Reuse cached data across clients with concurrent-request deduplication,
  stale fallback, and bounded external request usage.
- [x] Provide a searchable, responsive event list with correct ticket links and
  clear loading, empty, error, and freshness states.
- [ ] Persist observations and schedule budgeted city/date refreshes in SQLite.
- [ ] Discover across all Ticketmaster event categories with compatible filters.

### Personal radar

[Goal details](docs/goals/personal-radar.md)

- [x] Find nearby shows by city with clear dates, venues, and known prices.
- [x] Start with an approximate IP-based or remembered city, change it easily,
  and choose a city when detection is unavailable.
- [ ] Validate relevance with people in a pilot city.
- [ ] Save a show and find it again on a later visit.
- [ ] Sign in, manage a profile/privacy preferences, and keep saved events private.
- [ ] Follow artists and venues.
- [ ] See personalized matches with understandable reasons.

### Useful reminders

[Reminder goals](docs/goals/personal-radar.md#reminder-promises)

- [ ] Receive a timely public on-sale reminder for a saved show.
- [ ] Receive meaningful date, venue, cancellation, or postponement updates.
- [ ] Receive a weekly discovery digest.
- [ ] Change or pause reminders and choose notification preferences.

### Positive event communities

[Community goals](docs/goals/event-communities.md)

- [ ] Mark a show Interested and change or remove that choice.
- [ ] Choose public interest visibility; keep interest identity private by default.
- [ ] Recommend an event publicly, optionally explain why, and edit or withdraw it.
- [ ] Browse local/category recommendations and event conversations.
- [ ] Later: Mark a show Going and choose participation visibility.
- [ ] Post a question, tip, or comment on an event.
- [ ] Reply to another person's contribution.
- [ ] Edit or remove your own comments and replies.
- [ ] Give or remove a positive Helpful thumbs-up.
- [ ] Later: Follow a discussion and receive relevant activity updates.
- [ ] Report a concern privately and receive acknowledgment.
- [ ] Review reports as a moderator and communicate clear outcomes.
- [ ] Later: Record Went and share a post-show reflection.

### Responsive event and discussion experience

[Screen and interaction guidelines](docs/design-guidelines.md)

- [x] Browse with a desktop sidebar, intermediate labeled rail, and mobile bottom navigation.
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

- [x] Read events, their metadata, and music genres through the API.
- [ ] Read personalized radar results through the API.
- [ ] Manage personal and community activity from another client.
- [x] Provide an OpenAPI description for implemented endpoints.

### SQLite-first persistence

[Database design](docs/design/database.md)

- [ ] Keep preferences, saved shows, and event history across restarts.
- [ ] Persist community activity, moderation records, and notification jobs.
- [ ] Back up and successfully restore application data.

PlanetScale remains a later migration option if workload or deployment needs warrant it.

### Optional explorations

Optional explorations include a [discovery passport and games](docs/ideas/discovery-games.md)
and [venue/price insights](docs/ideas/venue-insights-and-discovery.md).

## Run locally

Requires Go 1.26+ and a [Ticketmaster API key](https://developer.ticketmaster.com/).

```sh
export TICKETMASTER_KEY=your-api-key
go run ./cmd/ticketopia
```

Open <http://localhost:8080>. Environment variables can also be placed in an
optional `.env` file.

JSON reads start at `/api/v1/events` and `/api/v1/genres`; the contract is served
at `/api/v1/openapi.yaml`. See the [discovery guide](docs/discovery.md) for filters
and `TICKETMASTER_DAILY_BUDGET` configuration.

Localhost starts with a city prompt. Public visitors can receive an approximate
IP-based city; see [location setup](docs/location.md) for the optional lookup
toggle and trusted reverse-proxy configuration.

The cache runs in memory by default. Shared backends are optional; see
[cache configuration](docs/cache.md).

## Development

After editing `.templ` files, regenerate their Go source before running the app:

```sh
go tool templ generate
```

After editing `views/styles/app.css`, rebuild the checked-in stylesheet and restart
the server (browser assets are embedded in the Go binary):

```sh
npx --yes tailwindcss@3.4.17 --input views/styles/app.css --output views/assets/app.css --minify
```

See [DESIGN.md](DESIGN.md) for the visual system and the
[UI guide](docs/discovery.md#responsive-navigation-and-event-details) for routes
and current feature availability.

Verify backend behavior with `go test -race ./...` and `go vet ./...`.

See [documentation and change history](docs/README.md) for feature guides and
project updates.
