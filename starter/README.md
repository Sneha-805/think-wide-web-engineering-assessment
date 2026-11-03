# campaign-events starter

A minimal skeleton. You can change anything in it — the routing, the types,
the layout. It exists so you don't spend your first twenty minutes on
boilerplate.

## Implementation behavior

- Events are stored in memory and must be re-loaded after a process restart.
- `event_id` is globally unique. A replay with the same canonical fields is a
  duplicate; changing another field for an existing ID is rejected as a conflict.
- Opened counts are raw accepted event counts. `unique_opens` counts distinct
  contacts within each campaign.
- Valid RFC3339 timestamps with offsets are accepted and normalized to UTC.
- Batches are processed item-by-item. The response reports accepted, duplicate,
  and rejected items with array indexes.
- Unknown JSON fields remain accepted for forward compatibility. Request bodies
  are limited to 1 MiB, and trailing JSON values are rejected.
- All accepted state is protected by a mutex, so concurrent HTTP requests are safe.

## Requirements

- Go 1.22 or later (the `"POST /events"` routing pattern needs it).
  Check with `go version`. On older Go, replace the patterns with manual
  method/path handling — that's fine too.
- No third-party dependencies or persistence features are required. The `go.mod`
  is intentionally limited to the Go standard library.

## Run

    go run .

Then, in another terminal:

    curl -s -X POST localhost:8080/events \
      -H 'Content-Type: application/json' \
      -d '[{"event_id":"evt_1","campaign_id":"cmp_summer_sale","contact_id":"ct_001","type":"delivered","timestamp":"2026-08-10T06:15:00Z"}]'

    curl -s localhost:8080/campaigns/cmp_summer_sale/stats

Run tests with:

    go test ./...

The seed response may use HTTP 207 when mixed valid and invalid records are present.

## Seed data

`seed/events.json` is one day of realistic provider traffic: 235 lines of events
including retries, conflicts, and malformed records. Your service should
survive all of it. Surviving it is not the same as accepting all of it — what
you accept, reject, and normalise is yours to decide and document.

One practical note: `[]Event` with strict field types means one bad element
can fail the whole array decode. Whether that is acceptable behaviour for a
batch endpoint is one of the decisions we are interested in.

To load the seed:

    curl -s -X POST localhost:8080/events \
      -H 'Content-Type: application/json' \
      --data-binary @seed/events.json
