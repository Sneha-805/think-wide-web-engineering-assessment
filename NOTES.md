# Engineering Notes

## Interpretation

I am building a provider-facing campaign event ingestion API. Providers may submit
a batch of sent, delivered, opened, and clicked events; retries must not change
campaign totals. The dashboard needs event counts for a campaign, while bad
provider records should be reported without preventing valid records in the same
batch from being accepted.

## Assumptions

- An `event_id` uniquely identifies an event across all campaigns.
- Duplicate means a reused `event_id` whose canonical payload matches the
  previously accepted event. It is counted as a retry and adds no totals.
- A reused `event_id` with any differing canonical field is a conflict. It is
  rejected, does not add totals, and the first accepted event remains
  authoritative for that ID for the lifetime of the process.
- After RFC3339 validation, timestamps are normalized to UTC. Equivalent offsets
  therefore produce the same canonical timestamp for duplicate comparison.
- Campaign totals count distinct provider event IDs, not distinct contacts, for
  `sent`, `delivered`, `opened`, and `clicked`.
- For malformed or conflict-preserving records, rejection is preferable to
  guessing or partially normalizing data.
- The event timestamp must be valid RFC3339. Any RFC3339 offset is accepted and
  normalized to UTC before storage. UTC is also used for date-based operations
  and late arrival does not change the provider timestamp.
- The service is expected to be small enough for in-memory persistence in this
  exercise, but it must remain safe under concurrent HTTP requests.

## Ambiguities

- Whether `opened` means raw opened events or distinct contacts.
- The expected status codes for mixed-validity batches.
- Whether campaign statistics should include only cumulative counts or daily/lifetime
  data.
- Metadata may contain arbitrary JSON values; nested objects and arrays are
  represented through `map[string]any`.
- Persistence after a process restart, and whether counts are ever reset.

## Questions for the PM

- Should event IDs be globally unique, or unique only inside a campaign?
- Should a campaign count one unique opener per contact or one event per provider
  open? Both interpretations are useful, but may be shown separately.
- On a conflict, should the first or most recently received payload win?
- Should HTTP 200 mean the whole batch was stored, or 207-style partial success?
- Is opening/clicking before delivery an invalid sequence or simply late/out-of-order data?
- Which fields, IDs, and timestamps are canonical enough to enforce strictly?

## Priorities

1. Implement safe, item-level JSON decoding and validation.
2. Deduplicate in memory with correct concurrent locking.
3. Provide cumulative and unique-open campaign statistics.
4. Add focused tests for duplicate, malformed, late, and mixed-batch behavior.
5. Fix the four debugging exercise bugs and document verification.
6. Add optional pagination or SQLite only if time remains; correctness comes first.

## Scale memo

### What breaks first

With 100 million events/day, the in-memory map and process-local counters break
first: memory will grow, a restart loses state and deduplication history, and
locks prevent useful horizontal scaling. The same is true of the process-local
event cache. A few thousand events fit comfortably in memory, but this is not a
persistence design.

### Change order

1. Move canonical event storage and counters to a transactional database.
2. Add indexes by campaign and timestamp and add a materialized/updated counters table.
3. Partition or warehouse raw events for scale, with retention and aggregation policy.
4. Add idempotency at the database layer and then multiple API instances behind a load balancer.
5. Add a queue only if bursts or downstream work make synchronous ingestion too fragile.

I would keep the current HTTP contract where practical: same path, same event
array, and a batch result shape. Large-result event listing is the first contract
area likely needing cursor pagination.

### Queue and storage

Storage becomes authoritative in PostgreSQL, for example: one row per `event_id`,
campaign stats per campaign, and daily bucket rows. A queue adds buffering and
retry, but also duplicate delivery, poison messages, lag, at-least-once processing,
and ordering ambiguity.

Duplicates can then appear across producers, queue redelivery, worker retries,
API replicas, and crash/restart windows. The database unique constraint on
`event_id` or an upsert by it remains the final guard.

Counters stay trustworthy by:

- inserting canonical events transactionally,
- recomputing counters from accepted event rows if needed,
- periodic reconciliation jobs,
- and publishing an ingestion watermark and replay/correction process.

### Monitoring

I would monitor accepted, duplicate, rejected, and conflict rates; queue lag and
retry/dead-letter counts; database latency and lock waits; API p50/p95/p99
latency; campaign counter drift; and reconciliation differences.

### Deliberately not solved yet

For the initial scale, I would not build a UI, cross-region replication, sharding,
Kafka, custom stream processing, authentication, or real-time exactly-once delivery.
Those are worth solving only after measured bottlenecks justify their complexity.

## The angry marketer

Plausible explanations include:

- Delivered events arrived late while opened events were corrected, reclassified,
  deduplicated, or rejected; late delivery can legitimately increase one counter.
- Opened may mean different things: raw opens, unique contacts, qualified opens,
  deduplicated contacts, or opens after a dashboard is corrected.
- A dashboard is mixing counters with different windows or refresh times.
- The dashboard switched data source, campaign filter, timezone, tenant, or version.
- A replay or partial backfill was applied between snapshots.
- Duplicate suppression or conflict handling changed the canonical event winner.
- An `event_id` collision or malformed record affected ingestion.
- One provider batch was only partly committed or the dashboard showed a stale cache.
- Workers lost/raced counter updates, or concurrent aggregation used non-atomic writes.
- The dashboard opened/clicked numbers include or exclude bot filtering, consent, or test traffic.
- Corrective events reverse counts in a downstream model.
- Manual data repair, campaign deletion/restoration, or campaign reassignment occurred.
- A clock skew or event-time/date bucket definition changed.

I would check first: the ingestion and reconciliation audit logs for `c1` and the
time window, duplicate/conflict counts, provider event timestamps, raw event
counts, and the exact metric definition used by the dashboard. Then compare
dashboard values with a direct database/Store snapshot and replay the same set
of accepted records twice.

The delivered rise is not inherently suspicious. Delivered is monotonic if counts
are cumulative and provider delivery confirmations arrive late; each new event
can raise delivered after it is accepted. Opened may also be non-monotonic if
“opened” means current distinct contact count after deduplication, corrections,
backfills, or metric semantics change. Raw monotonically accepted opened events
should not decrease; unique opens could only decrease from a correction/rebuild,
not normally from late new data.

## What I completed / what I intentionally skipped

Completed: assignment notes, a functional in-memory Go event service with item-level
batch validation, concurrent de-duplication and unique-open counting, focused tests,
all four debugging fixes, BUGS.md, scale reasoning, marketer analysis, and AI usage.
Intentionally skipped: the optional paginated event-list endpoint, SQLite, authentication,
deployment, and a frontend.

## If I had another day

I would add persistent storage, an end-to-end HTTP test suite that loads the seed,
cursor pagination for event history, reconciliation/replay tooling, operational
metrics, and a conflict policy documented with explicit status semantics.
