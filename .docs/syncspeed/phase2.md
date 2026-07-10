# Phase 2: Parity harness + iRODS changed-first (A1-A2)

Ref: [spec.md](spec.md) sections A1, A2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both. Tests are GoConvey acceptance tests asserting
user-visible boundaries: byte-identical warm-vs-cold mirror dumps, the
issued source-query shape, `SyncReport`, and recorded mirror write counts.

This phase first builds the SQLite parity-oracle harness that Phase 3 also
reuses (folded into item 2.1 so it is validated by A1's parity tests),
then reshapes the WARM `seq_product_irods_locations` source into one
self-contained changed-first statement (A1) and proves the zero-change
warm no-op (A2). Items are sequential: 2.1 and 2.2 both edit the warm
iRODS builders in `mlwh/sync.go`, and 2.2 is the zero-change behaviour of
2.1's reshaped path.

Preserve verbatim (spec "Existing behaviour that must be preserved"): the
projected column list/order consumed by
`scanSeqProductIRODSLocationsSyncRow`; the ordering `spi.last_changed,
spi.id_seq_product_irods_locations_tmp` (now on `changed_spi`); the
source-row-boundary batch flush in `syncSeqProductIRODSLocationsTable`;
`enrichSeqProductIRODSLocationsExportFields` (runs per batch, independent
of the source query); and the `JSON_TABLE` composition fragments
byte-for-byte (so the SQLite rewriter keeps matching). Cold and legacy
builders are unchanged; the `isUnsupportedCompositionQueryError` fallback
still routes to the existing legacy warm query.

CAUTION for item 2.1 (A1), from the spec: build the changed set as a
SINGLE named CTE (e.g. `changed_spi`) referenced BY NAME in the outer join
and in each recovery branch's `IN (SELECT id_product FROM changed_spi)`
filter. A repeated derived table would multiply the predicate placeholders
and break the registered arg counts; the single named CTE keeps arg counts
at exactly 1 (incremental) and 3 (from-cursor), with the predicate
placeholders living only in `changed_spi`.

Do NOT run the live sync or any db/network command; rely on the hermetic
SQLite suite. Bound every shell command with `timeout`. Feature-wide: no
source schema change, no `CacheSchemaVersion` bump.

## Items

### Item 2.1: A1 - changed-first warm iRODS + parity harness

spec.md section: A1 (also builds the Test-infrastructure harness reused by
Phase 3)

First add the shared warm-path harness (extend existing test helpers, do
not replace):
- `recordingSource`, a `Querier` wrapping `sqliteJSONTableSource` that
  appends every forwarded `(query, args)` for source-shape assertions;
- `assertWarmMirrorMatchesCold(t, table, seed, variant)`, the parity
  oracle that cold-syncs one `seed(func(*sql.DB))` fixture into cache A and
  warm-syncs it into cache B (seeding `sync_state` for the incremental or
  from-cursor variant), then asserts byte-identical ordered `SELECT *`
  mirror dumps (ORDER BY the mirror primary key);
- extend `rewriteJSONTableQueryForSQLite` for the changed-first statement
  (the `JSON_TABLE` fragments stay byte-identical) and
  `openRealMLWHSchemaSource` with any per-platform fixture columns needed.

Then reshape `seqProductIRODSLocationsSyncSourceQuery` (incremental, 1
arg) and `seqProductIRODSLocationsSyncSourceQueryFromCursor` (from-cursor,
3 args) in `mlwh/sync.go` into ONE self-contained changed-first statement:
a `changed_spi` CTE applying the incremental (`spi.last_changed >= ?`) or
from-cursor two-part predicate, then INNER JOIN each recovery branch
(Illumina composition, PacBio, Elembio, Ultimagen, ONT/oseq) with the
changed id set pushed in via `WHERE <branch>.id_product IN (SELECT
id_product FROM changed_spi)`. This is a semijoin reduction of the current
`spi INNER JOIN (recovery UNION ALL)`, so results cannot change. See the
CAUTION in Instructions: ONE named `changed_spi` CTE referenced by name
everywhere keeps arg counts exactly 1 / 3.

Covering all 4 acceptance tests from A1 (incremental warm mirror
byte-identical to cold over a fixture with one recoverable row per branch,
via `assertWarmMirrorMatchesCold(..., incremental)`; the same forced
from-cursor is byte-identical; a multi-row expansion plus a sibling
changed row keeps every expansion row with `platform` equal to its `spi`
row's `seq_platform_name`, matching cold; through `recordingSource`,
exactly one iRODS source query is issued and its text applies the
`changed_spi` filter to `spi` before the recovery join, with each branch
referencing that changed set). Test file:
`mlwh/sync_real_schema_test.go`.

- [x] implemented
- [x] reviewed

### Item 2.2: A2 - zero-change warm iRODS no-op

spec.md section: A2

With the item-2.1 changed-first path in place, ensure a warm iRODS sync
whose `changed_spi` is empty touches only `sync_state`: no
`seq_product_irods_locations_mirror` write, exactly one `sync_state`
upsert, `high_water` preserved (it is the max source `last_changed` seen,
so unchanged when nothing changed), and `last_run` advanced. Covering both
acceptance tests from A2 (given existing `sync_state` - `Exists`, non-zero
`high_water`, no cursor - and zero source rows at/after the watermark, a
warm sync on `openRecordingSQLiteSyncTestCache` gives `SyncReport{0,0}`, no
mirror write recorded, exactly one `sync_state` upsert, `high_water`
unchanged; and `last_run` advances to a fresh non-empty timestamp while
`high_water` is preserved). Test file: `mlwh/sync_test.go`. Depends on 2.1.

- [x] implemented
- [x] reviewed

For these sequential items, a single review pass after each item is
acceptable; the reviewer for 2.1 must confirm `recordingSource` wraps the
real SQLite source (no stub), `assertWarmMirrorMatchesCold` performs a
real cold AND warm sync and compares full mirror dumps, and the issued
warm query is one statement with arg counts 1 (incremental) / 3
(from-cursor) via a single named `changed_spi` CTE.

## Ordering and dependency notes

- Item 2.1 builds the parity harness reused by Phase 3 (B); Phase 3
  depends on it. Do not start Phase 3 until Phase 2 is reviewed.
- 2.1 and 2.2 are sequential: both edit the warm iRODS builders in
  `sync.go`, and 2.2 is the zero-change behaviour of 2.1's reshaped path.
- No existing WARM iRODS query-text or arg-count test exists to revise
  (spec E2, verified): the `spi.id_..._tmp > ?` assertions target the
  untouched COLD/id-mode builder, and the mock-driven content tests get
  planned rows by table with columns/arg counts preserved.
- `high_water` for iRODS is the max source `last_changed`, so a
  zero-change window PRESERVES it (spec Appendix).
