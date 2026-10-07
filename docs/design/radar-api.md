# Radar API

Status: The [discovery read API](../discovery.md#json-api) and
[own/public account APIs](../accounts.md#sessions-and-api-clients) and
[private saved-event APIs](../saved-events.md#api) and
[event-interest APIs](../event-interest.md#api) and
[public-recommendation APIs](../event-recommendations.md#api) are implemented.
Personal radar, durable ingestion, reminders, and other community endpoints below remain
draft designs. Track backend and frontend delivery in the
[aligned checklist](../goals/delivery.md).

## Implemented discovery slice

The HTML search and `/api/v1/events`, `/api/v1/events/{event_id}`, and
`/api/v1/genres` use a shared discovery service and KV cache. The
[OpenAPI contract](../../internal/api/openapi.yaml) documents the current API.
Event/artist/venue IDs are stable provider-scoped IDs, not title groups.

Discovery now supports all Ticketmaster segments with an optional category filter
and compatible genre filters. The current OpenAPI contract also documents the
implemented account slice; resources explicitly labeled planned below remain proposals.

Current pagination wraps provider pages in filter-bound cursors; it does not yet
offer a stored snapshot or an ID tie-breaker across upstream pages. Freshness is
collection time, not durable first/last-seen history. The persistence and scheduled
ingestion architecture below is the next stage, not current storage behavior.

## Shared backend

Provide a versioned REST/JSON API under `/api/v1` for a web client, a personal
mobile app, and other apps or scripts. Keep Ticketmaster access and scheduled
ingestion on the server. Clients work with Ticketopia resources.

```text
Ticketmaster -> ingestion/normalization -> MariaDB events and observations
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
where appropriate; planned durable application state lives in MariaDB, accessed
through `database/sql` and `github.com/go-sql-driver/mysql`. The
[database plan](database.md) covers shared storage across application replicas,
with `mariadb-operator` managing MariaDB in Kubernetes.

Ingest city/date batches once and reuse them across users within the upstream
request budget. Record collection coverage and failures alongside observations.
The current normalized model already preserves source event and artist IDs,
actual concert start dates, statuses, source event URLs, and optional price ranges.

## Proposed resources

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/api/v1/events` | Implemented: filter events by city, date window, artist, venue, or genre. |
| GET | `/api/v1/events/{event_id}` | Implemented: normalized event with source and freshness information. |
| GET | `/api/v1/genres` | Implemented: cached music genre/subgenre catalog. |
| GET | `/api/v1/categories` | Implemented: Ticketmaster segments/categories for all-category discovery. |
| GET | `/api/v1/artists` | Search artists to follow. |
| GET | `/api/v1/venues` | Search venues to follow. |
| GET, PATCH | `/api/v1/me/preferences` | Implemented: private location, category, time-zone, and future notification preferences. |
| GET | `/api/v1/me/radar` | Get ranked events with structured match reasons. |
| GET | `/api/v1/me/follows` | List artist and venue follows. |
| PUT, DELETE | `/api/v1/me/follows/{kind}/{id}` | Follow/unfollow an `artist` or `venue`. |
| GET | `/api/v1/me/saved-events` | Implemented: owner-only paginated last-known saved events. |
| GET, PUT, DELETE | `/api/v1/me/saved-events/{event_id}` | Implemented: read/save/unsave an owner-only bookmark. |
| GET | `/api/v1/me/saved-events/states` | Implemented: bounded batch of private viewer flags, separate from public metadata. |
| GET | `/api/v1/me/reminders` | List reminder settings and scheduling states. |
| PUT, DELETE | `/api/v1/me/reminders/{event_id}/{kind}` | Configure/remove a reminder; initially `public-on-sale`. |

Using natural resource paths for follows, saves, and reminder settings makes
repeated PUT/DELETE requests safe for clients retrying on unreliable connections.
For the first reminder kind, the request specifies a lead time in minutes and
the email channel. A successfully stored setting can be pending a source date;
it does not imply that a notification has been delivered.

Personal routes use the identity established by verified credentials. Mobile
and other API clients use user-scoped bearer credentials; the browser may use a
session mapped to the same identity. Direct Google sign-in and expiring opaque
API credentials are implemented; see [issuance/revocation](../accounts.md).

## Response conventions

- Use stable Ticketopia IDs and retain unique `(provider, source_id)` mappings.
  Title changes or identical event names must not change or merge identities.
  Keep current `ticketmaster:SOURCE_ID` public IDs resolvable through persistence;
  introducing internal surrogate keys must not break saved or discussion links.
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

The same shared backend serves the [event community goals](../goals/event-communities.md)
and [responsive journeys](../design-guidelines.md#discovery-to-discussion-journeys-and-shared-state).
MVP resources distinguish private bookmarks, interest, public event endorsements,
and post-level Helpful reactions. Going/Went, followed-discussion notifications,
and richer social features remain later goals. Basic private reports and moderator
review remain required before the public community pilot.

### Proposed MVP community resources

Except the marked profiles/interest/recommendation routes, routes below are **unimplemented proposals** under `/api/v1`. Saved
event routes above are implemented with owner-only semantics. Public reads return only
permitted fields; writes require verified identity and ownership/role checks.

| Method | Path | Purpose |
| --- | --- | --- |
| GET, PATCH | `/me/profile` | Implemented: own profile and new-interest privacy default; allowlisted writes |
| GET | `/users/{user_id}` | Implemented: ID, display name, bio only; no private account fields/activity |
| GET | `/users/{user_id}/activity` | Paginated public contributions, recommendations, opted-in interests; not a personalized feed |
| GET | `/me/event-interests` | Implemented: owner's interest collection, including private entries |
| GET, PUT, DELETE | `/me/event-interests/{event_id}` | Implemented: read/set/remove interest; explicit visibility or new-choice default, initially private |
| GET | `/me/event-interests/states` | Implemented: batched counts and owner-only state, without provider calls |
| GET | `/users/{user_id}/event-interests` | Implemented: public-opt-in event activity only |
| GET | `/events/{event_id}/interest` | Implemented: aggregate count including private choices, no identities |
| GET | `/me/event-recommendations` | Implemented: owner's published endorsements for management |
| GET, PUT, DELETE | `/me/event-recommendations/{event_id}` | Implemented: read/publish/replace reason/withdraw one public endorsement |
| GET | `/events/{event_id}/community` | Aggregate counts and a separately scoped viewer state when authenticated |
| GET | `/events/{event_id}/interested-users` | Implemented: paginated public-opt-in interested identities only |
| GET | `/events/{event_id}/recommendations` | Implemented: public endorsements, authors, optional reasons and explicit count |
| GET | `/users/{user_id}/event-recommendations` | Implemented: public endorsements independently of interest visibility |
| GET, POST | `/events/{event_id}/discussions` | List visible root posts / create a root post |
| GET | `/events/{event_id}/discussions/{discussion_id}` | Root post and event reference for direct-link context |
| GET, POST | `/events/{event_id}/discussions/{discussion_id}/replies` | Paginated replies / create reply with optional same-thread parent target |
| PATCH, DELETE | `/posts/{post_id}` | Author edit / removal preserving reply context |
| PUT, DELETE | `/posts/{post_id}/reactions/helpful` | Set/remove current user's positive post reaction |
| GET | `/recommendations` | Implemented: city/country/category-scoped endorsements; original-publication ordering, not personalized ranking |
| GET | `/community/discussions` | City/category-scoped active event threads |
| POST | `/posts/{post_id}/reports` | Submit a private concern |
| GET | `/me/reports` | Reporter-only acknowledgment and permitted status |
| GET, PATCH | `/moderation/reports/{report_id}` | Authorized case review/decision, excluding private notes from public responses |
| GET | `/moderation/reports` | Authorized paginated review queue |

Use unique user/event constraints for interest and recommendations, user/post
constraints for Helpful, and independent bookmark ownership. PUT/DELETE retries
must not double-count or create duplicate endorsements. Accept bounded text and
an idempotency key for post/reply/report creation so retries do not duplicate a
contribution; reject reuse with a different payload. Specify limits and key
retention before these endpoints ship. Mutations return authoritative viewer
state and affected counts (or trigger a scoped refetch after `204`). Do not infer
recommendation from Helpful, save, interest, or text mentioning an event.

Enforce thread/event identity, reply ancestry, edit ownership, and content
visibility in shared services, not only templates. A discussion count counts
visible root posts; reply counts and post reaction counts have separate fields.
Public participant lists can be smaller than interest totals because identities
are private by default. Changing interest visibility affects all public surfaces;
it does not hide separately published recommendations or posts. Responses must
never disclose private bookmark status to another viewer.

### Contextual loading and client state

Keep the initial event search bounded. Hydrate public counts and viewer flags for
the returned event IDs in batched local queries; do not issue a provider request
or a discussion query per card. Load the selected event's first root-post page
only on selection, with replies fetched on expansion. HTML fragments and JSON
use the same services; htmx enhancement does not require a SPA or server self-HTTP.

Use independent opaque cursors for discovery, root posts, replies, endorsements,
and profile/community activity (default 20, max 100). Bind local cursors to scope,
filters, sort, and visibility; sort root posts newest-first and replies oldest-first
with `(created_at, id)` tie-breakers. Active-community ordering may use
`(last_activity_at, id)` with a stable snapshot boundary for pagination. Fresh
activity is revealed by refresh rather than reordering a page being read. Current
provider discovery retains its documented non-snapshot pagination limits.

The URL owns selected event/filter/section/thread identity; client view state owns
scroll, loaded pages, pending requests, and unsent drafts. Key responses by event
and thread ID and reject outdated selection responses. Selection loads should
reuse cached event metadata and fail independently of discovery. Load-more errors
retain previous pages. Save/interest/recommendation mutations synchronize card,
detail, panel, and owner collections without resetting discovery filters.

Cache public metadata independently from authenticated viewer state; responses
containing private viewer fields must not enter a public shared cache. Counts
must honor moderation/visibility policy, and unknown counts remain unknown.
Keep local event identity/discussion available during Ticketmaster outages, with
explicit stale/unavailable metadata. Basic request/refresh behavior suffices for
asynchronous discussion; no WebSockets, message broker, or new database is required.

Interest identity visibility defaults to private and follows explicit user choices.
Recommendations and contributions are public with clear disclosure. Reactions support positive
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

MariaDB, `database/sql`, `github.com/go-sql-driver/mysql`, and `mariadb-operator`
are selected for planned persistence. Select migration tooling, supported pinned
database/operator versions, and database HA topology before deployment; see the
[database plan](database.md). Choose the pilot city/date horizon, account/token
approach, email provider, refresh cadence, and initial reminder lead time when
the corresponding product goals are ready for implementation.
