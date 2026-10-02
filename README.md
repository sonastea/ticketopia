# Ticketopia

An early-stage music-event discovery app built with Go, Echo, templ, htmx, and
Tailwind CSS, using the Ticketmaster Discovery API.

Currently displays music events grouped by name, with load-more pagination and
cached results.

## Goals

Track delivery here: check a milestone once it works for users and its behavior
has been verified. The linked goals and designs describe the expected outcomes.

### Personal radar

[Goal details](docs/goals/personal-radar.md)

- [ ] Find relevant nearby shows with clear dates, venues, and known prices.
- [ ] Save a show and find it again on a later visit.
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
- [ ] Mark a show Going and choose participation visibility.
- [ ] Post a question, tip, or comment on an event.
- [ ] Reply to another person's contribution.
- [ ] Edit or remove your own comments and replies.
- [ ] Give or remove a positive Helpful thumbs-up.
- [ ] Follow a discussion and receive relevant activity updates.
- [ ] Report a concern privately and receive acknowledgment.
- [ ] Review reports as a moderator and communicate clear outcomes.
- [ ] Record Went and share a post-show reflection.

### Reusable API

[API design](docs/design/radar-api.md)

- [ ] Read events and personalized radar results through the API.
- [ ] Manage personal and community activity from another client.
- [ ] Provide an OpenAPI description for implemented endpoints.

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

The cache runs in memory by default. Shared backends are optional; see
[cache configuration](docs/cache.md).

## Development

After editing `.templ` files, regenerate their Go source before running the app:

```sh
go tool templ generate
```

See [documentation and change history](docs/README.md) for feature guides and
project updates.
