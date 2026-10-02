# Venue insights and discovery games

Status: Exploration, not a committed feature set. The first goal is
[personal radar and reminders](../goals/personal-radar.md). Historical event data
can support either an analytical tool or a more playful discovery experience.

## Useful analysis questions

- Which genres make up the observed listings at each venue?
- How do advertised entry-price ranges differ by venue, genre, or weekday?
- Which venues regularly list shows matching someone's interests and budget?
- How far ahead are events first observed, and when do public sales begin?
- Which combinations of date, distance, genre, and venue lead users to save a show?

A first venue profile could show genre mix, upcoming shows, and the median of
advertised minimum prices, together with its sample size and observation period.
This would also make radar recommendations more explainable.

## Capture observations before estimating anything

The KV cache expires and cannot serve as a historical dataset. Store normalized
observations durably, preserving these fields as they become available:

| Data | Why retain it? |
| --- | --- |
| Provider, source event/artist/venue IDs, internal IDs | Deduplicate ingestion and join records reliably. |
| Event date/time, time zone, status, public on-sale time | Analyze schedules and changes; support reminders. |
| Artist and event genre classifications, including unknowns | Describe the observed programming at a venue. |
| Venue location and stable identity | Group shows consistently, including after a name change. |
| Advertised minimum/maximum price, currency, price type, known fee basis | Compare like-for-like listings. |
| Observation time, first/last seen, material-field version | Separate what was known at each time from the current record. |
| Ingestion run, query window, page coverage, success/failure | Distinguish incomplete collection from an absence of shows. |

Keep the latest event record plus versioned observations when material fields
change, and log collection coverage even when values stay identical. A CSV/JSON
export into a notebook is a sufficient first analysis tool.

## Interpret the results accurately

- Label statistics as based on observed listings from the available source.
  Ticketmaster coverage may represent only part of a venue's programme.
- Advertised minimum/maximum prices are listing information, not purchase prices,
  final checkout totals, or confirmed available inventory. An unknown price is
  missing data, not zero.
- For comparisons, specify currency, date window, price/fee basis, and inclusion
  rules. Count each distinct event once using a stated snapshot rule, such as
  its first observed price; repeated refreshes must not overweight a show.
- Show the count of priced events and the count with missing prices. A median of
  advertised minimums describes advertised entry prices, not an average ticket.
- Missing listings or failed refreshes do not establish cancellations. First
  seen is an observation timestamp, not necessarily an announcement timestamp.

Genre mix and price summaries are descriptive analysis. Estimating an unknown
or future price is a separate prediction experiment; interpolation between
observed prices cannot establish what happened during a gap. Begin with
comparable historical groups and report sample size, missingness, and variation.
If predictions become useful, evaluate on later, held-out events with only data
available before the prediction. Label estimates separately from observed values.

## Discovery experiments

Venue profiles could also support a playful experience: discover a venue new to
you, compare your genre discoveries with a friend, or guess a venue's most-listed
genre from a clearly described sample of events.

The [discovery games guide](discovery-games.md) explores the personal passport,
social recommendations, and reasons to return. These ideas can reuse event
observations while keeping the client API useful for analysis and ordinary radar
clients.

After the radar pilot, a small venue-profile view can help answer whether the
analysis itself is useful: can someone use it to find a show or venue they would
consider? Compare that feedback with the discovery-game experiment before
choosing a larger direction.

Data reference: [Ticketmaster Discovery API](https://developer.ticketmaster.com/products-and-docs/apis/discovery-api/v2/),
which documents classifications, venues, event statuses, sale dates, and price
ranges when supplied. Field completeness needs to be checked in the pilot city.
