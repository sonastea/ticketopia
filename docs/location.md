# Location-aware discovery

Status: Implemented for the web discovery page. The first visit starts with a
relevant city when one can be determined; the city remains visible and editable.

## Location precedence

The HTML search uses:

1. **The city in the URL/form**, including an explicitly chosen country.
2. **A remembered city**, when no location was supplied in the URL.
3. **An approximate IP-based city**, when neither of the above is available.
4. **A city-entry prompt**, when location is unavailable.

Explicitly blank city inputs and country-only searches ask for a city. They do
not silently restore a different location. Venue-filtered links already identify
a location and can be used without a city. Invalid queries are rejected before
location resolution or Ticketmaster requests.

The page labels a detected city as estimated from the IP address and a remembered
city as remembered. **Change city** focuses the editable field. IP location can
reflect a VPN, mobile carrier, or network gateway; it is an approximate city hint.
Event filtering is city-based, rather than a distance radius around GPS coordinates.

With no usable city or venue, the page fetches neither events nor genres from
Ticketmaster. Entering a city starts normal discovery. Localhost and private-network
development therefore start with the city form. Detection and manual selection
work without JavaScript or a browser-location permission prompt.

### Detecting a city on localhost

With JavaScript enabled, **Detect my city** makes an on-demand request from the
browser to `https://ipwho.is/`, which sees the browser's public IP rather than
the server's loopback address. This also works on localhost. It fills city and
country for review; choose **Find events** to search and remember the selection.
The result is approximate and may reflect a VPN's location. No GPS permission
is needed. Successful browser lookups are cached locally for 24 hours. Failure
leaves the existing fields editable and shows a manual-entry message.

This explicit browser action is independent of the server-side IP lookup toggle.
It requires access to the provider from the browser and shares its public-endpoint
quota for the site's domain. Without JavaScript, manual city entry still works.

## Remembering a choice

A valid explicit city search stores city/country in a `ticketopia_city` cookie
for 30 days. The cookie is HTTP-only, SameSite=Lax, and secure on HTTPS. Detected
cities are not automatically saved as explicit preferences. Pagination carries
the resolved city/country in its URL and does not update the remembered choice.
Resetting search filters keeps the remembered city.

Cookie values are treated as user input and validated on each read. This is a
browser preference; account-based location and cross-device synchronization
belong to the planned preferences milestone.

## IP lookup and caching

`internal/location` sends the resolved public client IP over HTTPS to
[`ipwho.is`](https://ipwhois.io/documentation), requesting only success, city,
and country-code fields. It requires no API key. Loopback, private, link-local,
multicast, documentation, and common reserved addresses are skipped.

- Successful city hints are cached for **24 hours** in the existing KV backend.
- Unsuccessful lookups are cached for **15 minutes**.
- Concurrent lookups for the same IP share one request per process; a cancelled
  caller does not cancel work shared with other visitors.
- Cache keys hash the IP. Values retain only city/country, success, and expiry.
  Raw IPs and full provider responses are not stored in these values or logged
  by the lookup service.
- HTTP calls time out after two seconds; shared lookup work is bounded to three
  seconds. Response bodies are limited to 64 KiB.
- Provider failures pause new lookups for a minute. A 429 uses `Retry-After`, or
  a 24-hour pause if no valid retry time is supplied.
- The free provider documents 1,000 requests/day. Ticketopia caps lookups at 900
  per process window of 24 hours, starting with its first lookup. Budget and
  cooldown counters reset on restart; cached hints can be shared through KV.

Event caches still use normalized city/date/filter keys, so different visitors
in the same city reuse the same Ticketmaster results. HTML responses use
`Cache-Control: private, no-store` because their initial location can vary by
visitor. This does not prevent server-side event-cache reuse.

## Configuration

| Variable | Default | Purpose |
| --- | --- | --- |
| `IP_GEOLOCATION_ENABLED` | `true` | Set to `false` to disable external IP lookups. Manual and remembered locations still work. |
| `TRUSTED_PROXY_CIDRS` | Empty | Comma-separated CIDRs of reverse proxies permitted to supply client IPs through `X-Forwarded-For`. |

Without trusted proxies, the server uses the direct connection's IP and ignores
forwarded IP headers. With configured proxies, it walks `X-Forwarded-For` from
the nearest proxy toward the client and uses the first untrusted address.
Private and loopback networks are not implicitly trusted.

For a reverse proxy that connects over loopback, for example:

```sh
export TRUSTED_PROXY_CIDRS='127.0.0.1/32,::1/128'
```

Use the actual proxy ranges for your deployment. Configure the edge proxy to
set/append the client address in `X-Forwarded-For` and the original scheme in
`X-Forwarded-Proto`. Invalid CIDR or boolean configuration fails startup.

## API clients and verification

The [JSON event API](discovery.md#json-api) remains explicitly filter-driven.
Its `city`/`country` filters are the same ones used by resolved web searches;
it does not depend on browser cookies or implicit IP defaults.

Tests cover location precedence, cache sharing/expiry, concurrent lookups,
negative caching, provider failures/timeouts/budgets, trusted proxy chains,
invalid cookies, and the no-worldwide-fallback behavior. Live browser checks
exercise detection, changing/remembering a city, pagination, no-JavaScript
browsing, mobile widths, and automated WCAG A/AA checks.
