# Debugging Findings

## 1. Duplicate IDs were only removed inside one batch

**Bug:** `processBatch` created a new local `seen` map for every 200-event batch.
The same event ID appearing in later batches was counted again.

**Why:** The de-duplication scope ended whenever a batch ended. The file contains
20,000 lines and provider retries can cross batch boundaries.

**Fix:** Added a package-level `seenEventIDs` map and consulted/updated it for the
entire run.

**Verification:** Baseline output before the fix was compared with
`expected_output.txt`; global duplicate accounting is now covered by the file
run, `go run -race`, and repeated output comparisons.

## 2. Unique opens ignored campaign boundaries

**Bug:** `openedBy` used only `contact_id`, so a contact opening one campaign could
suppress its first open in another campaign.

**Why:** Unique opens are distinct contacts per campaign, not globally distinct
contacts.

**Fix:** Changed the key to a combination of campaign ID and contact ID.

**Verification:** Campaign-specific output and repeated runs confirm opens are
counted once per `(campaign_id, contact_id)` pair.

## 3. Delivered daily buckets used local time

**Bug:** Delivered dates called `ev.Timestamp.Local()` instead of UTC.

**Why:** On machines with a local timezone other than UTC, the date may shift
across midnight even though the event contract supplies UTC timestamps.

**Fix:** Use `ev.Timestamp.UTC().Format("2006-01-02")`.

**Verification:** The program output is compared byte-for-byte with the expected
UTC daily buckets.

## 4. Worker counter updates raced and lost updates

**Bug:** Eight workers concurrently incremented fields and maps through plain ints.

**Why:** Concurrent reads and writes were a data race, so increments could be lost
and output could vary between runs. The race detector exposes this.

**Fix:** Added a package-level mutex around `apply` counter updates. `track` remains
on the main goroutine and keeps delivery and unique-open maps consistent.

**Verification:** `go run -race . events.jsonl` is expected to report no races, and
`go run . events.jsonl` should reproduce `expected_output.txt` every run.
