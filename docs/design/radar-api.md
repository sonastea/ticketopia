# Radar API

Status: Draft design for [personal radar](../goals/personal-radar.md) and
[event communities](../goals/event-communities.md).
These JSON endpoints are proposed; the current server exposes the HTML event list.

## Shared backend

Provide a versioned REST/JSON API under `/api/v1` for a web client, a personal
mobile app, and other apps or scripts. Keep Ticketmaster access and scheduled
ingestion on the server. Clients work with Ticketopia resources.

```text
Ticketmaster -> ingestion/normalization -> SQLite events and observations
                                            |
                                  shared application services
                                    /         |         \
                              templ UI    JSON API    reminder worker
                                             |              |
                                      mobile / other apps   email
```

HTML handlers and JSON handlers call the same services. The web server does not
need to call its own HTTP API. Ranking and notification logic have one owner.
The KV cache contains replaceable read results, with user-specific cache keys
where appropriate; durable application state lives in SQLite. The
[database plan](database.md) covers the initial single-host deployment and a
possible later migration to PlanetScale.

Ingest city/date batches once and reuse them across users within the upstream
request budget. Record collection coverage and failures alongside observations.
The current event model needs source event and artist IDs, actual concert start
dates, statuses, source event URLs, and optional price ranges.

## Proposed resources

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/events` | Filter events by city, date window, artist, venue, or genre. |
| GET | `/api/v1/events/{event_id}` | Read a normalized event with source and freshness information. |
| GET | `/api/v1/artists` | Search artists to follow. |
| GET | `/api/v1/venues` | Search venues to follow. |
| GET, PATCH | `/api/v1/me/preferences` | Read/update location, interests, time zone, and email/digest preferences. |
| GET | `/api/v1/me/radar` | Get ranked events with structured match reasons. |
| GET | `/api/v1/me/follows` | List artist and venue follows. |
| PUT, DELETE | `/api/v1/me/follows/{kind}/{id}` | Follow/unfollow an `artist` or `venue`. |
| GET | `/api/v1/me/saved-events` | List saved events. |
| PUT, DELETE | `/api/v1/me/saved-events/{event_id}` | Save/unsave an event. |
| GET | `/api/v1/me/reminders` | List reminder settings and scheduling states. |
| PUT, DELETE | `/api/v1/me/reminders/{event_id}/{kind}` | Configure/remove a reminder; initially `public-on-sale`. |

Using natural resource paths for follows, saves, and reminder settings makes
repeated PUT/DELETE requests safe for clients retrying on unreliable connections.
For the first reminder kind, the request specifies a lead time in minutes and
the email channel. A successfully stored setting can be pending a source date;
it does not imply that a notification has been delivered.

Personal routes use the identity established by verified credentials. Mobile
and other API clients use user-scoped bearer credentials; the browser may use a
session mapped to the same identity. The authentication provider and token
issuance/revocation flow need a concrete design before personal endpoints ship.

## Response conventions

- Use stable Ticketopia IDs and retain unique `(provider, source_id)` mappings.
  Title changes or identical event names must not change or merge identities.
- Normalize events into application DTOs rather than expose provider payloads
  or template types. Include artist/venue references and the source event URL.
- Keep concert start time, public on-sale time, and time-zone information
  separate. Represent missing instants as `null`; preserve local dates and
  TBA/TBD flags when a complete instant is unavailable. Return known instants
  as RFC 3339 timestamps.
- Represent advertised price ranges with currency, decimal-string amounts, and
  fee-inclusion information when known. Unknown prices are `null` and are not
  treated as free. Event listing status does not establish live seat inventory.
- Include `first_seen_at`, `last_seen_at`, and material-record `updated_at` values.
  First seen means first observed by Ticketopia, not the official announcement.
- Return radar reasons as codes plus display text, such as `followed_artist` or
  `preferred_genre`, so clients can display or localize explanations.
- Paginate collections with an opaque cursor, default limit 20, maximum 100,
  and `next_cursor: null` at the end. Use deterministic ordering with an ID
  tie-breaker; bind cursors to the filters and a result-set version for radar.
- Use a collection envelope such as `{ "items": [], "next_cursor": null,
  "meta": { "data_as_of": "...", "stale": false } }`. Freshness describes the
  underlying ingested data, not just the time the response was generated.

Return `200` for reads/updates, `201` with `Location` for a newly created resource,
and `204` for successful deletion. Use structured `application/problem+json`
errors with a stable application error code and field errors when relevant:
`400` invalid input/cursor, `401` unauthenticated, `403` forbidden, `404` missing
resource, `429` throttled with `Retry-After`, and `503` temporarily unavailable.
Validate filter combinations and bound date windows as well as page size.

Stored event reads can continue during an upstream outage with freshness
metadata. Ingestion failures must not erase the last successful observation.
Keep responses additive within v1 and publish an OpenAPI contract as the first
endpoints are implemented.

## Reminder execution

Run durable notification jobs against current event versions and preferences.
Track scheduled, pending-date, sent, cancelled, and failed states separately from
the user's reminder setting. Use a stable job identity based on the user, event,
notification kind, and relevant occurrence/change, with atomic worker claims
and recorded delivery attempts. Reconcile jobs when source dates change.

Keep the user's local schedule and time zone alongside event time-zone data.
First observation establishes a baseline for later change detection. Recheck
the event and delivery preferences before sending, cancel obsolete jobs, and
use delivery-provider idempotency where available. Retain uncertain delivery
outcomes for investigation instead of assuming an interrupted request failed.

The first delivery adapter is email. The same event and reminder services can
support mobile push later without changing how clients follow or save events.

## Community clients

The same shared backend should serve the core
[event community goals](../goals/event-communities.md): Interested/Going/Went
states, comments and replies, positive Helpful reactions, followed discussions,
private reports, and moderator review. Define the resource contracts as the
corresponding user goals are implemented.

Participation visibility follows user choices. Reactions support positive
acknowledgment only; there is no downvote operation. Report submission and status
are private to the reporter and authorized moderators. Moderator decisions and
private case notes require moderator access; public conversation responses must
not expose reports or reporter identities. Authors receive their own moderation
outcomes without access to private reports.

Store community contributions and moderation state durably, with the same
ownership and visibility rules for every client. Represent removed comments in
a way that preserves reply context while withholding their removed content.

## Analysis clients

A later read-only export of normalized observations can support notebooks and
venue analysis. Preserve the history described in the
[exploration notes](../ideas/venue-insights-and-discovery.md) now; define aggregate
or export endpoints once an actual analysis question establishes the needed shape.

## Open implementation choices

SQLite is the initial database. Select its Go driver and migration tooling,
the pilot city/date horizon, account/token approach, email provider, refresh
cadence, and initial reminder lead time when the corresponding product goals are
ready for implementation. Choose a PlanetScale engine if a later migration is
warranted by the [database plan](database.md).
