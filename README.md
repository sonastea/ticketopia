# Ticketopia

An early-stage music-event discovery app built with Go, Echo, templ, htmx, and
Tailwind CSS, using the Ticketmaster Discovery API.

Currently displays music events grouped by name, with load-more pagination and
cached results.

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
