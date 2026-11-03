# Think Wide Web Campaign Events Assignment

A Go service that ingests provider campaign events, protects campaign counters
from retries, and exposes aggregated campaign statistics. The submission also
includes a separate debugging exercise and its documented findings.

## Repository layout

- `starter/` - campaign events HTTP service
- `starter/seed/events.json` - supplied provider event dataset
- `debugging/` - standalone campaign statistics debugging exercise
- `NOTES.md` - design interpretation, scale memo, and angry-marketer analysis
- `BUGS.md` - debugging findings and verification
- `AI_USAGE.md` - AI usage disclosure

## Architecture

The service uses Go's standard library HTTP server and an in-memory store.
Each accepted event is retained by its globally unique `event_id`. A mutex
protects the event deduplicator, campaign counters, and unique-open sets so
concurrent requests update state safely.

Provider batches are decoded item by item. Valid events are accepted or
identified as duplicates; malformed items and conflicting event IDs are reported
individually. The store is process-local and resets when the service restarts.
This is appropriate for the assignment and is intentionally not persisted to a
database.

`event_id` is globally unique for the lifetime of the running process. A replay
whose canonical JSON fields match the first accepted event is counted as a
duplicate. A payload with an already-used `event_id` but different canonical
fields is rejected as a conflict; the first accepted event remains authoritative.
RFC3339 timestamps with different timezone offsets are treated as the same
timestamp after both are normalized to UTC.

Campaign statistics count distinct accepted provider event IDs for sent, delivered,
opened, and clicked. `unique_opens` additionally counts distinct contacts per
campaign.

## Requirements

- Go 1.22 or later
- Windows PowerShell, macOS, or Linux
- No third-party Go dependencies

## Setup and run

From the repository root in PowerShell:

```powershell
cd starter
go run .
```

The server listens on `http://localhost:8080`.

## API

### POST /events

Accepts a JSON array containing provider events. The entire request must be a
single JSON array with no trailing JSON values. The maximum request size is
1 MiB. Unknown fields are accepted for forward compatibility.

Request:

```json
[
  {
    "event_id": "evt_00042",
    "campaign_id": "cmp_summer_sale",
    "contact_id": "ct_017",
    "type": "opened",
    "timestamp": "2026-08-10T09:15:04Z",
    "metadata": {
      "url": "https://example.com/offer",
      "attempt": 2,
      "trusted": true
    }
  }
]
```

Example:

```powershell
curl.exe -X POST http://localhost:8080/events `
  -H "Content-Type: application/json" `
  --data-binary '[{"event_id":"evt_00042","campaign_id":"cmp_summer_sale","contact_id":"ct_017","type":"opened","timestamp":"2026-08-10T09:15:04Z","metadata":{"url":"https://example.com/offer"}}]'
```

Successful response shape:

```json
{
  "accepted": 1,
  "duplicates": 0,
  "rejected": 0,
  "results": [
    {
      "index": 0,
      "status": "accepted",
      "event_id": "evt_00042"
    }
  ]
}
```

- `200 OK`: accepted or duplicate-only batch
- `207 Multi-Status`: batch contains accepted results and rejected items
- `400 Bad Request`: malformed body, trailing JSON, or only rejected items
- `413 Request Entity Too Large`: body exceeds 1 MiB

### GET /campaigns/{campaign_id}/stats

Returns cumulative counts for one campaign.

Example:

```powershell
curl.exe http://localhost:8080/campaigns/cmp_summer_sale/stats
```

Example response:

```json
{
  "campaign_id": "cmp_summer_sale",
  "sent": 40,
  "delivered": 35,
  "opened": 27,
  "clicked": 12,
  "unique_opens": 19
}
```

`404 Not Found` is returned for a campaign with no accepted events.

## Timestamp policy

Every timestamp must be valid RFC3339. Timestamps with timezone offsets are
accepted and normalized to UTC before storage. UTC is used for any date-based
operation. Arrival time is not stored separately, so a late delivery does not
change the provider event's timestamp.

## Load the supplied seed

Start the service, then run:

```powershell
cd starter
curl.exe -X POST http://localhost:8080/events `
  -H "Content-Type: application/json" `
  --data-binary "@seed/events.json"

curl.exe http://localhost:8080/campaigns/cmp_summer_sale/stats
```

The current dataset test verifies `210 accepted`, `15 duplicates`, and
`10 rejected`.

## Testing

Campaign service tests:

```powershell
cd starter
go test ./...
go test -race ./...
go vet ./...
```

## Debugging exercise

From the debugging directory:

```powershell
cd debugging
go run . events.jsonl
go run -race . events.jsonl
```

Verify exact stdout:

```powershell
go run . events.jsonl > actual.txt
git diff --no-index --text expected_output.txt actual.txt
```

Run both PowerShell commands from the `debugging/` directory. The Unix
equivalent is:

```sh
cd debugging
go run . events.jsonl | diff -u expected_output.txt -
```
