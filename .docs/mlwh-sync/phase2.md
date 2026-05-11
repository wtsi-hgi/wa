# Phase 2: Sync engine

Ref: [spec.md](spec.md) sections B1, B2, B3, B4, B5, B6, B7, B8

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 2.1: B1 - Five-table parallel sync

spec.md section: B1

Implement `Client.Sync(ctx) ([]SyncReport, error)` in
`mlwh/sync.go` and the corresponding `cmd/mlwh.go sync` subcommand:
always fan out exactly five goroutines (one per supported table)
with streaming MLWH connections, accumulate errors via
`errors.Join`, return reports ordered by table-finish time, emit
per-table stdout lines as each goroutine returns, and remove the
`--tables` flag. Covers all 8 acceptance tests from B1.

- [ ] implemented
- [ ] reviewed

### Item 2.2: B2 - Batched idempotent upserts

spec.md section: B2

Introduce `const syncBatchSize = 1000` and per-table dialect-aware
multi-row upserts (`ON CONFLICT DO UPDATE` / `ON DUPLICATE KEY
UPDATE`) using each table's unique key. Track per-batch
inserted/updated counters via the pre-upsert
`SELECT COUNT(*) ... WHERE <unique_key> IN (...)` inside the same
transaction. Covers all 4 acceptance tests from B2.

- [ ] implemented
- [ ] reviewed

### Item 2.3: B3 - Resume cursor

spec.md section: B3

Per-batch commit must update `sync_state.resume_cursor` to the
tab-separated ordering tuple of the batch's last row (RFC3339Nano
for `last_updated`), set NULL at end-of-stream, and use a strict
`>` keyset predicate built from the cursor on the next iteration.
Includes the explicit 4-column keyset query for `iseq_flowcell` /
`library_samples`. Covers all 4 acceptance tests from B3. Depends
on item 2.2.

- [ ] implemented
- [ ] reviewed

### Item 2.4: B4 - Cold-load index drop / recreate for sample_mirror

spec.md section: B4

For `sample_mirror` only, drop the 8 secondary indexes in a
transaction that also sets `indexes_dropped = 1` on cold load,
recreate them in a transaction that clears the flag after the
final batch, and have `OpenCache` recover an
`indexes_dropped == 1 && high_water != 0` state by recreating
the indexes silently. Confirm the other four tables never enter
the drop/recreate path. Covers all 7 acceptance tests from B4
(tests 6 and 7 are MySQL-gated). Depends on items 2.2 and 2.3.

- [ ] implemented
- [ ] reviewed

### Item 2.5: B5 - Upstream reconnect / retry

spec.md section: B5

On `rows.Err()` / `invalid connection` / `unexpected EOF` /
similar transient errors, reopen the upstream connection and
resume from `resume_cursor` up to 5 attempts per fault. Backoff
1 s, 2 s, 4 s, 8 s, 16 s (capped at 30 s). Emit one stderr line
per retry matching the spec format. Non-transient errors do not
retry. Covers all 4 acceptance tests from B5. Depends on item
2.3.

- [ ] implemented
- [ ] reviewed

### Batch 6 (parallel, after item 2.5 is reviewed)

#### Item 2.6: B6 - Per-cache advisory lock [parallel with 2.7, 2.8]

spec.md section: B6

Add the per-cache advisory lock: SQLite `IMMEDIATE` transaction on
a single-row `sync_lock` table held for the lifetime of
`wa mlwh sync`, MySQL `GET_LOCK('wa_mlwh_sync_<cache_id>', 0)`
derived from the cache DSN. Second concurrent sync exits non-zero
with the spec stderr message, empty stdout. Release on normal
exit, error, and signal; `wa mlwh info` never takes the lock.
Covers all 4 acceptance tests from B6.

- [ ] implemented
- [ ] reviewed

#### Item 2.7: B7 - Source filter tightening [parallel with 2.6, 2.8]

spec.md section: B7

Ensure the source queries for `library_samples`,
`iseq_product_metrics_mirror`, and
`seq_product_irods_locations_mirror` `INNER JOIN study ON ... AND
study.id_lims = 'SQSCP'` so rows without an SQSCP study are never
inserted. Add `NOT NULL` + `CHECK(id_study_lims <> '')` to those
tables in both dialects. Sync goroutine surfaces a constraint
violation immediately, naming the offending row's PK. Covers all
4 acceptance tests from B7.

- [ ] implemented
- [ ] reviewed

#### Item 2.8: B8 - SQLite write pragmas [parallel with 2.6, 2.7]

spec.md section: B8

Apply `PRAGMA synchronous=NORMAL`, `PRAGMA cache_size=-200000`,
`PRAGMA temp_store=MEMORY` to each sync write connection in the
spec order, record the pre-existing values, and restore them on
command exit (success or error). MySQL caches skip the pragma
helper. Covers all 3 acceptance tests from B8.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).
