# Faster Warm MLWH Sync Specification

## Overview

`wa mlwh sync` must be cheap enough to run every 30 minutes when the live MLWH
source has only a few changes since the last run. Today a seeded-warm sync is
dominated by source-side recovery work and destination rewrites across four
tables. This feature reshapes the WARM path of those four tables so a
few-changes window does near-zero source recovery and near-zero mirror writes,
while a zero-change window writes only `sync_state`.

The four tables and their warm reshape:

- `seq_product_irods_locations`: one self-contained changed-first SQL statement
  that filters `spi` before expanding recovery `JSON_TABLE`s.
- `iseq_product_metrics`: an in-Go two-phase warm path (direct changed-row
  fetch, Go classification, scoped composite recovery) replacing the monolithic
  union.
- `iseq_run_status`: retain the last completed `id_run_status` watermark so the
  next warm sync reads only `id_run_status > watermark`.
- `seq_ops_tracking_per_sample`: an atomic in-application diff/apply against the
  current mirror instead of wholesale delete-and-reinsert.

Cold / ascending-id sync, the unsupported-`JSON_TABLE` legacy fallback, all
other tables, and the single-invocation "one command syncs every supported
table" contract are unchanged. No MLWH source schema changes. No cache schema
change and no `CacheSchemaVersion` bump.

## Architecture

### Scope and non-goals

- In scope: the WARM variants only. For `seq_product_irods_locations` and
  `iseq_product_metrics` that means BOTH the plain incremental query (no
  `resume_cursor`) and the from-cursor resume query. For `iseq_run_status` it
  means the ascending-id resume watermark. For `seq_ops_tracking_per_sample` it
  means the full-snapshot apply step.
- Untouched: every cold / ascending-id builder, the unsupported-`JSON_TABLE`
  legacy fallback builders, `finalizeMirrorSyncState` index handling, all other
  supported tables, and `Client.Sync` fan-out (`supportedSyncTables`).
- Forbidden: MLWH source schema changes; a UNIQUE/PRIMARY KEY on the tracking
  mirror's `id_sample_lims`; bumping `CacheSchemaVersion` (routes through
  `migrateCacheSchema`, which drops all mirrors and forces full cold resync).

### Packages, files, types

- Package `mlwh/`. Product changes in `sync.go` and
  `sync_platform_coverage.go`. No new files required.
- Existing types reused unchanged: `Querier` (`QueryContext` only), `Cache`
  (`DB()`, `Dialect()`, `Close()`), `SyncReport{Table, Inserted, Updated,
  Duration, HighWater}`, `syncStateRecord{HighWater, ResumeCursor,
  IndexesDropped, Exists}`, `syncBatchResult{Inserted, Updated}`,
  `seqProductIRODSLocationsSyncRow`, `iseqRunStatusSyncRow`,
  `seqOpsTrackingPerSampleSyncRow`.
- `iseqProductMetricsSyncRow` (struct definition unchanged) is reused ONLY as
  the combined mirror-write OUTPUT row - kept single-component rows plus
  recovered composite rows - fed to the existing batch writer via
  `iseqProductMetricsMirrorRowArgs`. Its `IDSampleTmp int64` /
  `IDStudyLims string` are non-nullable and it has no composition field, so it
  MUST NOT be the Phase-1 scan target.
- New Phase-1 fetch/scan struct (name illustrative,
  `iseqProductMetricsChangedRow`): the own-flowcell metadata is scanned NULLABLE
  - `id_iseq_flowcell_tmp` (`sql.NullInt64`), `id_sample_tmp` (`sql.NullInt64`),
  `id_study_lims` (`sql.NullString`) - because Phase 1 LEFT JOINs own
  `iseq_flowcell` / `SQSCP` `study`, so merged/multi-component products arrive
  with all three NULL. It also carries the raw `iseq_composition_tmp` JSON and
  the direct-row columns (`id_iseq_product`, `id_iseq_pr_metrics_tmp`, `id_run`,
  `position`, `tag_index`, `qc`, `qc_lib`, `qc_seq`, `last_changed`). Go
  classifies component count from `iseq_composition_tmp` and builds
  `iseqProductMetricsSyncRow` outputs; a single-component row is emitted only
  when its own-flowcell `SQSCP` study resolved (both nullable id fields
  non-null). Scanning those NULLs into `iseqProductMetricsSyncRow`'s
  int64/string fields would fail at runtime - the merged-product trap this
  feature must avoid.
- `SyncReport` has no `Deleted` field; deletions are asserted via mirror row
  presence / count, not via the report.

### Registered source queries

`AllSyncSourceQueries()` (exported, signature unchanged:
`func AllSyncSourceQueries() []SyncSourceQuery`) is the single source of truth
the source-schema integration test prepares against the real MLWH. Every new or
reshaped SELECT the sync issues MUST be represented, with a fixed `ArgCount`.
Cold and legacy entries stay as-is.

### Existing behaviour that must be preserved verbatim

- `scanSeqProductIRODSLocationsSyncRow` consumes exactly this projected column
  list and order:
  `id_seq_product_irods_locations_tmp, id_product, irods_root_collection,
  COALESCE(irods_data_relative_path,'') , recovered id_sample_tmp, recovered
  id_study_lims, last_changed, created, seq_platform_name`.
- `seqProductIRODSLocationsSyncSourceQuery` ordering
  `ORDER BY spi.last_changed, spi.id_seq_product_irods_locations_tmp` and the
  source-row-boundary batch flush in `syncSeqProductIRODSLocationsTable` (all
  expansion rows of one source row stay contiguous).
- `enrichSeqProductIRODSLocationsExportFields` (fills
  `id_run`/`position`/`tag_index`/`qc`/`is_deliverable`/`merged` from LOCAL
  product-metrics mirrors) still runs per batch; it is independent of the source
  query.
- `iseq_product_metrics` final combined output ordered by
  `last_changed, id_iseq_pr_metrics_tmp`; from-cursor cursor encoding
  (`last_changed \t id_iseq_pr_metrics_tmp`) unchanged.
- The `JSON_TABLE` composition fragments in
  `seqProductIRODSLocationsIlluminaCompositionRecovery` and
  `iseqProductMetricsCompositeSourceSelect` stay byte-identical (so the SQLite
  test rewriter keeps matching).
- `isUnsupportedCompositionQueryError` fallback to the legacy query stays for
  both reshaped tables.

### Test infrastructure (test files only; extend, do not replace)

Reuse existing helpers in `sync_test.go`, `sync_a5_test.go`,
`sync_real_schema_test.go`:

- `openRealMLWHSchemaSource(t) *sql.DB` - SQLite fixture with the real MLWH
  source schema. Extend with any additional per-platform fixture tables/columns
  a parity fixture needs; keep existing columns.
- `sqliteJSONTableSource{db}` (implements `Querier`) +
  `rewriteJSONTableQueryForSQLite(query string) string`. Extend the rewriter to
  translate the reshaped statements; the `JSON_TABLE` composition fragments are
  unchanged so existing substitutions keep matching.
- `openRecordingSQLiteSyncTestCache(t) (Cache, *sqliteSyncSQLObserver)` -
  observer records every mirror `ExecContext` as a `recordedSQLStatement`
  (`.Query`/`.Args`) via `Statements()`, plus `BeginCount()`/`CommitCount()`.
  `filterRecordedStatements` narrows recorded statements by a predicate (e.g.
  to mirror-table writes). As-is this observer captures only writes
  (`recordingSQLiteConn.ExecContext`); `QueryContext` records nothing and the
  read-only connection has a nil observer, so a mirror diff-read is not
  captured. D2.2 requires EXTENDING this recording driver/observer to also
  capture the mirror diff-read (`QueryContext`) on an observed connection:
  either record `QueryContext` on the write/tx connection (and run the
  diff-read on that connection so it is captured), or attach an observer to the
  read-only connection. No stub: the recorded SELECT must be the real streamed
  diff-read.
- `openSQLiteSyncTestCache(t) Cache`, `seedSyncState(t, db, table, highWater)`,
  `seedSyncStateWithCursor(t, db, table, highWater, resumeCursor)`,
  `withSyncColdBatchSizeForTest(t, size)`,
  `countRows(t, db, query, args...) int`,
  `syncSelectedTablesForTest(ctx, client, tables...) ([]SyncReport, error)`,
  client construction `&Client{cache:..., cacheReader: cacheReadDB(cache),
  syncSource:..., disableSyncLock:true}`.

Add four test-only helpers:

- `recordingSource` - a `Querier` wrapping `sqliteJSONTableSource` that appends
  every `(query, args)` it forwards, so tests assert the warm source-query shape
  and the exact Phase-3 `IN`-list arguments.
- `withSyncCompositeRecoveryChunkSizeForTest(t, size)` (name illustrative) -
  overrides the Phase-3 composite-recovery chunk size (a package-level `var`
  defaulting to `syncStatementRowLimit(1)`) for the test and restores it on
  cleanup, mirroring `withSyncColdBatchSizeForTest`, so a chunk boundary can be
  crossed with a small fixture.
- `withSyncTrackingResidencyHookForTest(t, fn)` (name illustrative) - overrides
  the tracking diff's package-level `syncTrackingMirrorResidencyHook`
  (`func(residentMirrorRows int)`, nil in production, so a no-op) with `fn` and
  restores it on cleanup, mirroring `withSyncColdBatchSizeForTest`. As the D1
  diff advances the streamed mirror side it calls the hook (when non-nil) with
  the count of mirror rows currently held resident in its mirror-side working
  set - `1` per step for the one-row merge - so D2.3 can record peak residency
  and prove bounded (streamed), not full-materialization, mirror reads.
- `assertWarmMirrorMatchesCold(t, table, seed, variant)` - the parity oracle: it
  builds two fresh caches over one `seed(func(*sql.DB))` fixture, cold-syncs the
  table into cache A, warm-syncs it into cache B (seeding `sync_state` to force
  the incremental or from-cursor warm path per `variant`), and asserts the two
  mirrors are byte-identical as ordered `SELECT *` dumps (ORDER BY the mirror
  primary key). Byte-identical warm-vs-cold mirror is the correctness oracle.

## A. Warm changed-first `seq_product_irods_locations`

### A1: Changed-first warm query with cold-identical results

As a sync operator, I want the warm iRODS query to filter changed `spi` rows
before expanding recovery `JSON_TABLE`s, so a few-changes warm window returns
the same rows without the multi-minute broad recovery cost.

Replace the incremental and from-cursor iRODS source builders with a single
self-contained statement: a `changed_spi` CTE (or derived table) applying the
incremental (`spi.last_changed >= ?`) or from-cursor
(`(spi.last_changed > ?) OR (spi.last_changed = ? AND
spi.id_seq_product_irods_locations_tmp > ?)`) predicate, then INNER JOIN each
recovery branch (Illumina composition, PacBio, Elembio, Ultimagen, ONT/oseq)
with the distinct changed `id_product` set pushed into each branch (e.g.
`WHERE <branch>.id_product IN (SELECT id_product FROM changed_spi)`) so
`JSON_TABLE` expands only changed products. This is a semijoin reduction of the
current `spi INNER JOIN (recovery UNION ALL) ON recovery.id_product =
spi.id_product`, so it cannot change the inner-join result. Arg counts stay 1
(incremental) and 3 (from-cursor); the predicate placeholders live only in
`changed_spi`. The projected column list and order, the ordering
`spi.last_changed, spi.id_seq_product_irods_locations_tmp` (now on
`changed_spi`), the source-row-boundary batch flush, and
`enrichSeqProductIRODSLocationsExportFields` are unchanged. Cold and legacy
builders are unchanged; the
`isUnsupportedCompositionQueryError` fallback still routes to the existing
(unchanged) legacy warm query.

**Package:** `mlwh/`
**File:** `mlwh/sync.go`
**Test file:** `mlwh/sync_real_schema_test.go`

Reshape (names illustrative):
`seqProductIRODSLocationsSyncSourceQuery() string` (incremental, 1 arg) and
`seqProductIRODSLocationsSyncSourceQueryFromCursor() string` (from-cursor, 3
args) now emit the changed-first statement.

**Acceptance tests:**

1. Given an `openRealMLWHSchemaSource` fixture with at least one recoverable row
   per branch (Illumina composition merged multi-component product, Illumina
   single-component, PacBio, Elembio, Ultimagen, ONT/oseq) all with
   `last_changed` after a seeded early watermark, when the table is synced warm
   (incremental) via `assertWarmMirrorMatchesCold(..., incremental)`, then the
   warm mirror is byte-identical to the cold-sync mirror of the same fixture.
2. Given the same fixture but the warm path forced via a seeded from-cursor
   `resume_cursor`, when synced via `assertWarmMirrorMatchesCold(...,
   fromCursor)`, then the warm mirror is byte-identical to the cold mirror.
3. Given a source row whose one `id_product` expands to multiple iRODS objects
   (multi-row expansion) and a sibling changed row, when warm-synced, then every
   expansion row is present and `platform` for each mirror row equals the
   `seq_platform_name` of its `spi` row (not the matched metrics table),
   matching cold.
4. Given a changed Illumina product plus an unchanged Illumina product in the
   same run window, when warm-synced through `recordingSource`, then the issued
   iRODS source query text contains the `changed_spi` filter applied to `spi`
   before the recovery join (the query selects from `seq_product_irods_locations
   spi` within a CTE/derived table and each recovery branch references that
   changed set), and exactly one iRODS source query is issued.

### A2: Zero-change warm iRODS no-op

As a sync operator, I want a warm iRODS sync with no changed rows to touch only
`sync_state`, so the 30-minute cadence is cheap.

**Package:** `mlwh/`
**File:** `mlwh/sync.go`
**Test file:** `mlwh/sync_test.go`

**Acceptance tests:**

1. Given a cache with existing `seq_product_irods_locations` `sync_state`
   (`Exists`, non-zero `high_water`, no cursor) and zero source rows at or
   after the watermark, when warm-synced on `openRecordingSQLiteSyncTestCache`,
   then `SyncReport` is `{Inserted:0, Updated:0}`, no
   `seq_product_irods_locations_mirror` write statement is recorded, exactly one
   `sync_state` upsert is recorded, and the finalized `high_water` is unchanged.
2. Given the same setup, when warm-synced, then `last_run` in `sync_state`
   advances to the run time (a fresh non-empty timestamp) while `high_water` is
   preserved.

## B. Warm two-phase `iseq_product_metrics`

### B1: Two-phase warm path with cold-identical results

As a sync operator, I want the warm product-metrics sync to fetch changed rows
directly and run composite recovery only for changed multi-component products,
so a few-changes window avoids the broad `JSON_TABLE` composite recovery.

Replace the warm monolithic union with, when `JSON_TABLE` is supported:

- Phase 1 (one registered SELECT): fetch every changed row with its own-flowcell
  metadata and `iseq_composition_tmp`, LEFT JOIN own `iseq_flowcell` and its
  `SQSCP` `study` (so merged products with a NULL own `id_iseq_flowcell_tmp` are
  NOT dropped). Scan into the nullable Phase-1 fetch struct (Architecture), NOT
  `iseqProductMetricsSyncRow` (whose non-nullable `id_sample_tmp` /
  `id_study_lims` cannot scan a merged product's NULL own-flowcell metadata).
  Predicate: incremental `ipm.last_changed >= ?` (1 arg) or from-cursor two-part
  (3 args). Order by `ipm.last_changed, ipm.id_iseq_pr_metrics_tmp`.
- Phase 2 (Go): classify each changed row by component count parsed from
  `iseq_composition_tmp` (`COALESCE` empty to `{"components":[]}`): <= 1
  component is a direct candidate, > 1 is a composite candidate. This is the
  same disjoint partition the current SQL uses (`NOT EXISTS components[1]` vs
  multi-component).
  Emit a direct row only when its own flowcell exists AND that flowcell's study
  is `SQSCP` (LEFT-joined `id_sample_tmp`/`id_study_lims` non-null); drop
  otherwise. Direct rows keep their own `id_run`/`position`/`tag_index`.
- Phase 3 (Go-built, chunked SELECTs): run the existing composite recovery
  (`iseqProductMetricsCompositeSourceSelect`) scoped by an explicit
  `path_ipm.id_iseq_product IN (<literal changed multi-component ids>)`, chunked
  to the sync statement parameter limit (chunk size a package-level `var`
  defaulting to `syncStatementRowLimit(1)`, shrinkable in tests) so a candidate
  set exceeding one chunk issues multiple literal `IN`-list queries whose bound
  args union to the full candidate set, never a correlated subquery. Composite
  recovery keeps its existing
  semantics: component `JSON_TABLE` expansion, `EXISTS` an Illumina `spi` row
  for the path product, `HAVING COUNT(*) > 1`, `position = 0`, `tag_index = 0`,
  `id_run` = common component run else 0, `MIN` sample/study,
  `COALESCE(path own flowcell, MIN(component flowcell))`, and `qc`/`qc_lib`/
  `qc_seq`/`last_changed` from the path row. A candidate lacking an Illumina
  `spi` row or failing `HAVING` yields no mirror row.
- Combine direct + composite rows, order by `last_changed,
  id_iseq_pr_metrics_tmp`, and write via the existing batch writer / mirror row
  args. The warm run may buffer the full changed-row set in memory (all-or-
  nothing per run); no mid-run `resume_cursor` checkpoint is required.

The cold / ascending-id path stays the current streaming union query, unchanged.
On an `isUnsupportedCompositionQueryError`, the warm path falls back to the
existing (unchanged) legacy direct-only query.

**Package:** `mlwh/`
**File:** `mlwh/sync.go`
**Test file:** `mlwh/sync_real_schema_test.go`

**Acceptance tests:**

1. Given a fixture containing a direct single-component `SQSCP` row, a direct
   single-component row whose own flowcell is NULL, a direct row whose flowcell
   study is not `SQSCP`, a merged multi-component product (NULL own flowcell,
   components in `SQSCP` flowcells, with an Illumina `spi` row present), a
   multi-component candidate with no Illumina `spi` row, and a multi-component
   candidate failing `HAVING`, all changed after a seeded watermark, when warm-
   synced (incremental) via `assertWarmMirrorMatchesCold`, then the warm mirror
   is byte-identical to the cold union-query mirror of the same fixture.
2. Given the same fixture with the from-cursor warm path forced by a seeded
   `resume_cursor`, when synced via
   `assertWarmMirrorMatchesCold(..., fromCursor)`, then the warm mirror is
   byte-identical to the cold mirror.
3. Given the fixture from test 1, when warm-synced, then the merged product's
   mirror row is present with `position = 0`, `tag_index = 0`, and recovered
   `id_sample_tmp`/`id_study_lims` from its components (matching cold), and the
   NULL-flowcell direct row and the non-`SQSCP` direct row are absent (matching
   cold).

### B2: Warm product-metrics efficiency and no-op

As a sync operator, I want the warm product-metrics sync to scope composite
recovery to exactly the changed multi-component products and to write nothing
when nothing changed.

**Package:** `mlwh/`
**File:** `mlwh/sync.go`
**Test file:** `mlwh/sync_test.go`, `mlwh/sync_real_schema_test.go`

**Acceptance tests:**

1. Given a warm window with N changed rows of which exactly K are multi-
   component (1 <= K within one chunk) and the rest single-component, when
   warm-synced through `recordingSource`, then the union of bound args across
   the issued composite-recovery queries equals exactly the K changed multi-
   component `id_iseq_product` values (order-independent set equality), each
   issued composite-recovery query is a literal `IN`-list form, and no issued
   query uses the broad correlated-subquery composite form
   (`... id_iseq_product IN (SELECT ... WHERE last_changed ...)`).
2. Given a warm window with K multi-component candidates where K exceeds the
   Phase-3 chunk size (forced small via
   `withSyncCompositeRecoveryChunkSizeForTest(t, size)`, K > size, small
   fixture), when warm-synced through `recordingSource`, then composite
   recovery issues more than one query, every issued query is a literal
   `IN`-list form with at most `size` bound args (never a correlated subquery),
   and the union of all their bound args equals exactly the set of
   Go-classified multi-component candidate `id_iseq_product` values.
3. Given a warm window with zero multi-component changed rows, when warm-synced
   through `recordingSource`, then no composite-recovery query is issued at all.
4. Given a cache with existing `iseq_product_metrics` `sync_state` and a source
   with zero rows at/after the watermark, when warm-synced on
   `openRecordingSQLiteSyncTestCache`, then `SyncReport` is `{Inserted:0,
   Updated:0}`, no `iseq_product_metrics_mirror` write statement is recorded,
   exactly one `sync_state` upsert is recorded, `high_water` is preserved, and
   `last_run` advances.

## C. Retained `iseq_run_status` watermark

### C1: Retain completed `id_run_status` watermark

As a sync operator, I want `iseq_run_status` to remember the highest synced
`id_run_status`, so the next warm sync reads only newer status rows.

Stop clearing `resume_cursor` at finalize. `syncIseqRunStatusTable` persists,
on successful completion, `resume_cursor = id_run_status \t <last completed id>`
where that id is the max `id_run_status` paged this run, or, when no new
rows were read, the id already stored. `high_water` stays empty (zero). The only
reader is `iseqRunStatusResumeID`; freshness reads `high_water`/`last_run` only,
so it is unaffected. No migration or backfill.

**Package:** `mlwh/`
**File:** `mlwh/sync_platform_coverage.go`
**Test file:** `mlwh/sync_a5_test.go`

**Acceptance tests:**

1. Given a fresh cache cold-synced with source rows up to `id_run_status = M`,
   then a second warm sync after inserting new rows `M+1..M+P`, when the second
   sync runs through `recordingSource`, then its `iseq_run_status` page query
   first uses cursor `M` (not 0), `SyncReport.Inserted == P`,
   `SyncReport.Updated == 0`, the mirror row count equals `M+P`, and the stored
   `resume_cursor` decodes to `M+P`.
2. Given the state after test 1 and no further source rows, when a third warm
   sync runs on `openRecordingSQLiteSyncTestCache`, then no
   `iseq_run_status_mirror` write is recorded, `SyncReport` is `{Inserted:0,
   Updated:0}`, the stored `resume_cursor` still decodes to `M+P` (never NULL),
   and `high_water` remains empty.

### C2: One-time re-read on upgrade

As a sync operator upgrading an existing cache, I want the first post-upgrade
sync to re-establish the watermark exactly once, so subsequent warm syncs are
near-zero.

An existing cache carries `resume_cursor = NULL` (old code cleared it) with a
fully populated mirror and empty `high_water`. `iseqRunStatusResumeID` returns
the cold initial id (0) for a NULL cursor, causing one full re-read; the run
then persists the completed watermark.

**Package:** `mlwh/`
**File:** `mlwh/sync_platform_coverage.go`
**Test file:** `mlwh/sync_a5_test.go`

**Acceptance tests:**

1. Given a cache whose `iseq_run_status` mirror already holds rows `1..M`, whose
   `sync_state` row exists with empty `high_water` and NULL `resume_cursor`, and
   a source holding the same rows `1..M`, when a warm sync runs through
   `recordingSource`, then the page query starts at cursor 0 (the one-time full
   re-read), the mirror still holds exactly M rows, `SyncReport.Inserted == 0`
   and `SyncReport.Updated == M`, and the stored `resume_cursor` afterward
   decodes to `M`.
2. Given the state after test 1 with no new source rows, when a subsequent warm
   sync runs through `recordingSource`, then the page query starts from cursor
   `M` and reads zero rows (no second full re-read).

## D. `seq_ops_tracking_per_sample` diff/apply

### D1: Atomic in-application diff/apply

As a sync operator, I want the tracking table to apply only the differences
against the current mirror, so an unchanged snapshot costs zero mirror writes
and a few-change snapshot costs a few writes, all atomically.

Keep the full source snapshot read (buffered). Sort the buffered snapshot
byte-wise in Go (default Go string comparison on `id_sample_lims`, e.g.
`slices.Sort`). Stream the current mirror ordered by the SAME byte-wise order,
`ORDER BY id_sample_lims COLLATE utf8mb4_bin` (MySQL) or
`ORDER BY id_sample_lims COLLATE BINARY` (SQLite), and merge-compare against
the sorted snapshot, computing: insert (id present in snapshot, absent in
mirror), update (id in both, any non-key column differs), delete (id in
mirror, absent from snapshot). The explicit `COLLATE` is required: the mirror's
`id_sample_lims` is case-insensitively collated in both dialects (SQLite
`COLLATE NOCASE`; MySQL `utf8mb4_0900_ai_ci`/`utf8mb4_general_ci`), so a plain
`ORDER BY id_sample_lims` streams the mirror case-insensitively while Go sorts
byte-wise, desyncing the merge and misclassifying rows (spurious insert+delete,
missed updates). An explicit `ORDER BY ... COLLATE` overrides the column's
declared collation in both engines, and Go reproduces byte order exactly (do
NOT replicate MySQL `ai_ci` in Go). Live ids are all-numeric today, where every
collation agrees, so this is defensive/forward-safe, not currently
load-bearing. Row identity is `id_sample_lims` (unique, never NULL in
source). A row differs only if a non-key column differs when compared as
stored, with NULL-vs-NULL
milestone datetimes treated as equal (so an unchanged snapshot yields zero
updates). Apply all inserts + updates + deletes + the `sync_state` write inside
one write transaction, so a concurrent reader sees either the whole old or the
whole new snapshot. Do NOT add a UNIQUE/PRIMARY KEY on `id_sample_lims` and do
NOT bump `CacheSchemaVersion`. `high_water` remains the refresh time;
`SyncReport.Inserted` counts new ids, `SyncReport.Updated` counts changed
existing ids (deletions are observable via mirror absence / row-count delta).

**Package:** `mlwh/`
**File:** `mlwh/sync_platform_coverage.go`
**Test file:** `mlwh/sync_a5_test.go`

**Acceptance tests:**

1. Given a mirror seeded with tracking rows for ids `{A,B,C,D}` and a source
   snapshot for ids `{A(unchanged), B(one milestone datetime changed),
   E(new)}` (C and D absent from the snapshot), when warm-synced on
   `openRecordingSQLiteSyncTestCache`, then `SyncReport.Inserted == 1` (E),
   `SyncReport.Updated == 1` (B), the mirror afterward holds exactly `{A,B,E}`
   (C and D deleted), and each surviving row's columns equal the snapshot's.
2. Given the mixed scenario of test 1, when it runs, then exactly one write
   transaction is used (`BeginCount()` and `CommitCount()` each 1 for the
   write), containing the insert, the update, and the deletes.
3. Given a mirror row and a source row for the same `id_sample_lims` that are
   identical except both have NULL `manifest_created` (and other NULL
   milestones), when warm-synced, then that row produces no update
   (`SyncReport.Updated` excludes it), proving NULL-vs-NULL equality.
4. Given a mirror and a source snapshot equal over an id set that includes
   case-variant `id_sample_lims` values sorting differently under
   case-insensitive vs byte-wise order (e.g. `A`, `a`, `B`, `b`), when warm-
   synced on `openRecordingSQLiteSyncTestCache`, then `SyncReport` is
   `{Inserted:0, Updated:0}`, no `seq_ops_tracking_per_sample_mirror`
   INSERT/UPDATE/DELETE is recorded, and the mirror row set is unchanged.
   (Under a plain case-insensitive mirror `ORDER BY` the merge desyncs and
   records spurious inserts/deletes; both sides must use byte-wise order.)

### D2: Unchanged snapshot no-op and streamed mirror read

As a sync operator, I want an unchanged tracking snapshot to write only
`sync_state`, and the mirror side to be read as an ordered stream rather than a
second full in-memory copy.

**Package:** `mlwh/`
**File:** `mlwh/sync_platform_coverage.go`
**Test file:** `mlwh/sync_a5_test.go`

**Acceptance tests:**

1. Given a mirror and a source snapshot that are equal over a set of ids,
   when warm-synced on `openRecordingSQLiteSyncTestCache`, then no
   `seq_ops_tracking_per_sample_mirror` INSERT/UPDATE/DELETE statement is
   recorded, exactly one `sync_state` upsert is recorded inside one committed
   transaction, `SyncReport` is `{Inserted:0, Updated:0}`, the mirror row set is
   unchanged, and `high_water`/`last_run` advance to the refresh time.
2. Given the equal mirror/snapshot of test 1 and the recording driver/observer
   extended to capture `QueryContext` on the observed diff-read connection (per
   Test infrastructure), when warm-synced, then the observer records exactly
   one SELECT against `seq_ops_tracking_per_sample_mirror` whose text contains
   `ORDER BY id_sample_lims COLLATE BINARY` (the streamed byte-ordered
   diff-read on SQLite), and records no unordered full-table SELECT that loads
   the whole mirror.
3. (Bounded mirror residency) Given `N = 10000` rows present and identical in
   both the mirror and the source snapshot (so the diff must traverse the whole
   mirror), when warm-synced with the tracking diff's
   `syncTrackingMirrorResidencyHook` installed via
   `withSyncTrackingResidencyHookForTest` to record the maximum
   `residentMirrorRows` it is notified with, then the recorded peak is a small
   constant independent of `N`: the D1 diff streams and merge-compares the
   mirror one row at a time, so the peak equals `1`, whereas a full-
   materialization diff (loading every mirror row into memory alongside the
   buffered snapshot) would record `N` (`10000`). Assert `peak == 1`.
   Deterministic - no `runtime.GC`/`ReadMemStats`, no byte calibration, and no
   heap-size flakiness - it cannot false-pass (materialization records `N`, not
   `1`) nor false-fail (a streamed one-row merge always records `1`). It
   complements D2.2: D2.2 proves the read is ordered by `id_sample_lims`, D2.3
   proves it is streamed (bounded residency), not fully materialized.

## E. Cross-cutting guarantees

### E1: Source-query registry coverage

As a maintainer, I want every reshaped warm SELECT represented in
`AllSyncSourceQueries()`, so the source-schema integration test still validates
each against the real MLWH.

**Package:** `mlwh/`
**File:** `mlwh/sync.go`
**Test file:** `mlwh/sync_source_integration_test.go`

Registry after the change (names illustrative, arg counts binding):

- `seq_product_irods_locations incremental` (1), `... from cursor` (3) - now the
  changed-first statements.
- `seq_product_irods_locations cold` (1), and all three `... legacy ...` entries
  - unchanged.
- `iseq_product_metrics incremental` (1) and `... from cursor` (3) - now the
  Phase-1 direct changed-row fetches.
- `iseq_product_metrics composite recovery` - the scoped composite recovery,
  registered via a canonical single-bound-id representative form (ArgCount 1).
- `iseq_product_metrics cold` (2) and the three `... legacy ...` entries -
  unchanged.

**Acceptance tests:**

1. Given `AllSyncSourceQueries()`, then it contains entries named for the
   reshaped `seq_product_irods_locations` incremental (ArgCount 1) and
   from-cursor (ArgCount 3), the `iseq_product_metrics` Phase-1 incremental
   (ArgCount 1) and from-cursor (ArgCount 3), and the scoped
   `iseq_product_metrics` composite-recovery representative (ArgCount 1); and
   the cold and legacy entries for both tables are still present with unchanged
   ArgCounts.
2. (Gated on `WA_MLWH_DSN`) Given the real MLWH source, when every
   `AllSyncSourceQueries()` query is prepared with its `ArgCount` placeholders,
   then each prepares successfully (the existing
   `TestSyncSourceSchemaMatchesRealMLWH` continues to pass).

### E2: One command still syncs every table; legacy fallback intact

As a sync operator, I want one `wa mlwh sync` invocation to keep syncing every
supported table (no per-table schedule split) and the unsupported-`JSON_TABLE`
fallback to keep working.

This behaviour is unchanged; `supportedSyncTables` and `Client.Sync` fan-out are
not modified. Per testing-principles, no new test asserts unchanged fan-out
beyond the guard below. The existing full-sync and legacy-fallback tests
(`TestSyncAgainstRealMLWHSchema` asserting `len(reports) ==
len(supportedSyncTables)`, and the legacy-fallback tests) continue to pass
UNCHANGED.

The four warm reshapes affect several existing tests. Each was checked and is
either revised (with how) or verified to pass unchanged.

Must be revised:

- `TestRealworldA3IseqProductMetricsSourceQueryIncludesCompositeProducts`
  (`sync_test.go`): the B two-phase reshape invalidates it. It today asserts the
  `iseq_product_metrics` `incremental`, `cold`, and `from cursor` registry
  queries each contain the composite branch and the single-component filter,
  with ArgCounts 2/2/6. Revise it (preserving its intent that composite products
  are still synced):
  - assert the composite-branch fragments - `JSON_TABLE(path_ipm...`, the
    composite `EXISTS ... seq_product_irods_locations spi ... 'illumina'` check,
    and `CASE WHEN MIN(component.component_run) ...` - on the NEW
    `iseq_product_metrics composite recovery` entry (ArgCount 1);
  - assert `iseq_product_metrics incremental` (ArgCount 1) and `from cursor`
    (ArgCount 3) are Phase-1 direct-only: no composite fragments and no
    `NOT EXISTS (... JSON_TABLE(COALESCE(ipm...` single-component filter (Go now
    classifies component count); they keep `study.id_lims = 'SQSCP'` from the
    own-flowcell `SQSCP` LEFT JOIN;
  - leave `iseq_product_metrics cold` assertions unchanged (ArgCount 2, still
    the monolithic union with every fragment above).
  Arg counts match E1's registry.
- `TestClientSyncSeqOpsTrackingPerSampleSwapIsAtomic` (`sync_a5_test.go`): the
  D1 diff/apply replaces the wholesale `writeSeqOpsTrackingPerSampleFullRefresh`
  this test calls directly, so it no longer compiles. Retarget it at the new
  diff/apply entry point, preserving its concurrent-reader atomicity intent (a
  reader never observes a partial table). Its single-write-transaction guarantee
  is additionally proven by new tests D1.2 and D2.1 (`BeginCount()` and
  `CommitCount()` each 1).
- `TestClientSyncSeqOpsTrackingPerSampleFullRefreshReplacesSnapshot`
  (`sync_a5_test.go`): its content assertions still pass under diff/apply
  (disjoint old/new sets: old ids deleted, new ids inserted), but its name and
  comment assert wholesale full-refresh / delete-and-reinsert semantics that no
  longer hold. Rename and recomment it to the diff/apply semantics.

Verified to pass unchanged (checked; no revision needed):

- C (`iseq_run_status`): no existing test pins the cleared-cursor-at-finalize
  behaviour. `TestClientSyncIseqRunStatusReadsRowsInAscendingIDOrder`
  (`sync_a5_test.go`) is a cold sync asserting only the ascending paging cursor
  progression (0..5) and mirror content, not the post-finalize `resume_cursor`.
  `TestFreshnessIseqRunStatusReportsEmptyHighWaterWithLastRun`
  (`freshness_test.go`) reads only `high_water`/`last_run`, both unchanged
  (`high_water` stays empty).
- A (`seq_product_irods_locations`): no existing test pins the WARM query text
  or arg counts. The `spi.id_..._tmp > ?` query-text assertions target the
  untouched COLD/id-mode builder -
  `TestClientSyncSeqProductIRODSLocationsUsesLocationTmpCursor`,
  `TestClientSyncSeqProductIRODSLocationsResumeKeepsExpandedSourceRowsAtomic`,
  `TestSeqProductIRODSLocationsColdSyncUsesSourceIDKeyset` (all `sync_test.go`).
  The iRODS sync content tests driven by the mock source
  (`...KeepsSiblingDataObjects` cold, plus
  `...IncrementalKeepsUnchangedSiblingDataObject`,
  `...IncrementalReplacesOldPathForSourceRow`,
  `...IncrementalResumeReplacesExistingRows`; `sync_test.go`) get planned rows
  by table with the projected columns and arg counts preserved, so they assert
  the same mirror content.
- B (`iseq_product_metrics`): cold-path and mirror-write tests are untouched.
  Cold source path:
  `TestRealworldA3ClientSyncMirrorsCompositeProductMetricsForIRODSJoin` (a cold
  sync of a merged composite fixture),
  `TestClientSyncIseqProductMetricsKeepsProductKeyAndUsesMetricsTmpCursor`,
  `TestIseqProductMetricsColdSyncUsesDescendingSourceIDKeyset`,
  `TestIseqProductMetricsColdIDSyncKeepsMaxHighWater`, and the QC-mapping tests
  (`...PreservesNullQCAsPending`, `...MapsQCOneZeroNullToPassFailPending`).
  Mirror-write batch:
  `TestSparseMySQLIseqProductMetricsIncrementalBatchUsesDeleteThenInsert`,
  `TestReplaceIseqProductMetricsMirrorBatchDeletesDuplicateSparseRows`,
  `TestIseqProductMetricsColdBatchUsesInsertOnly`, and the iRODS counterparts
  `TestSparseMySQLSeqProductIRODSLocationsIncrementalBatchUsesDeleteThenInsert`,
  `TestReplaceSeqProductIRODSLocationsMirrorBatchDeletesDuplicateSparseRows`,
  `TestSeqProductIRODSLocationsColdBatchUsesInsertOnly` (all `sync_test.go`).
- D (`seq_ops_tracking_per_sample`):
  `TestClientSyncSeqOpsTrackingPerSampleSetsRefreshAndSyncTimes`
  (`sync_a5_test.go`) asserts `high_water` == refresh time and `last_run` ==
  sync time, both preserved by diff/apply.
  `TestClientSyncSeqOpsTrackingPerSampleToleratesNullContextColumns`
  (`sync_real_schema_test.go`) inserts into an empty mirror (all rows new) and
  asserts NULL context columns mirror as empty strings.
- Registry iteration: `TestSyncSourceSchemaMatchesRealMLWH` and
  `TestStudyUsersSyncSourceQueryCovered` (`sync_source_integration_test.go`),
  and the gated live-perf cold-builder callers in `integration_test.go`, iterate
  or build cold/registry queries dynamically and pass once E1's registry entries
  land - no per-name revision.
- Read-side tests in other files that only seed a mirror plus `sync_state` and
  then exercise read APIs never invoke the reshaped sync source path and are
  unaffected.

**Package:** `mlwh/`
**File:** `mlwh/sync.go`, `mlwh/sync_platform_coverage.go`
**Test file:** `mlwh/sync_real_schema_test.go`, `mlwh/sync_test.go`,
`mlwh/sync_a5_test.go`

**Acceptance tests:**

1. Given a single combined `openRealMLWHSchemaSource` fixture with changed rows
   for all four reshaped tables plus `sample` and `study`, and existing warm
   `sync_state` for each, when one `Client.Sync`-equivalent multi-table run
   executes, then it returns one report per supported table and the four
   reshaped mirrors are each populated (no reshaped table is skipped).
2. Given a source whose composition query raises an unsupported-`JSON_TABLE`
   error, when `seq_product_irods_locations` and `iseq_product_metrics` are
   warm-synced, then each falls back to its existing legacy query and completes
   without error (the existing legacy-fallback behaviour is preserved). The
   `iseq_product_metrics` fixture MUST include at least one changed
   multi-component candidate so Phase-3 composite recovery runs and its
   `JSON_TABLE` query triggers the fallback: Phase 1 issues no `JSON_TABLE`, so
   without a composite candidate the fallback never triggers and the assertion
   is vacuous.

## Implementation Order

1. **Phase 1 - independent, parallelizable table reshapes (no shared harness).**
   - C (`iseq_run_status` retained watermark): smallest, localized to
     `sync_platform_coverage.go` finalize. No existing test pins the cleared
     cursor, so C revises no existing test (verified, E2).
   - D (`seq_ops_tracking_per_sample` diff/apply): application-code diff in
     `sync_platform_coverage.go`, plus the production-no-op
     `syncTrackingMirrorResidencyHook` seam D2.3 uses to assert bounded mirror
     residency. D2.2 additionally EXTENDS the recording driver/observer to
     capture the mirror diff-read `QueryContext` (per Test infrastructure), not
     only writes. Revise the two existing tracking tests here (E2): retarget
     `TestClientSyncSeqOpsTrackingPerSampleSwapIsAtomic` at the new diff/apply
     entry point, and rename/recomment
     `TestClientSyncSeqOpsTrackingPerSampleFullRefreshReplacesSnapshot` to the
     diff/apply semantics.
   C ships with its tests on the existing recording cache/source; D ships its
   tests on that recording driver/observer extended per D2.2.
2. **Phase 2 - parity oracle harness + `seq_product_irods_locations`.** Add
   `recordingSource` and `assertWarmMirrorMatchesCold`, extend
   `rewriteJSONTableQueryForSQLite`, then implement A (single-statement
   changed-first) and its tests. Depends on the harness.
3. **Phase 3 - `iseq_product_metrics` two-phase (B).** Most complex; reuses the
   Phase-2 harness and rewriter. Depends on Phase 2's harness. This reshape
   immediately changes the `iseq_product_metrics` source SELECTs and their arg
   counts, so land its E1 `AllSyncSourceQueries()` entry updates (incremental
   ArgCount 2->1, from-cursor 6->3, new `composite recovery` ArgCount 1) and
   the `TestRealworldA3...` revision (E2) in THIS phase, so no phase is left
   red.
4. **Phase 4 - cross-cutting (E).** Add E1's unit assertions over the full
   reshaped registry (`seq_product_irods_locations` from Phase 2,
   `iseq_product_metrics` from Phase 3) and the gated real-MLWH prepare check;
   confirm E2's other existing full-sync and legacy-fallback tests still pass.
   The `iseq_product_metrics` registry entries and `TestRealworldA3...`
   revision already landed in Phase 3. Depends on A and B landing.

Within Phase 1 the two tables are independent and may proceed in parallel.
Phases 2 and 3 are sequential (3 builds on 2's harness). Phase 4 is last.

## Appendix: Key Decisions

- **In-code, no-schema-change tracking diff.** The tracking mirror keeps its
  non-unique `id_sample_lims` index; the diff is computed in Go by merging the
  sorted buffered source snapshot against the ordered-streamed mirror. Avoids a
  cache schema change and the destructive `CacheSchemaVersion` bump.
- **Retained watermark, no migration.** `iseq_run_status` simply stops clearing
  `resume_cursor`; existing caches self-heal via one full re-read. `high_water`
  stays zero, so freshness semantics are unchanged.
- **In-memory two-phase for `iseq_product_metrics`.** Justified by the warm
  premise of few changes; all-or-nothing per run, crash resumability provided
  only by the existing from-cursor query. The performance-critical rule: Phase-3
  composite recovery is scoped by an explicit literal `IN`-list of Go-classified
  multi-component ids (chunked to the parameter limit), NEVER a correlated
  subquery that re-derives candidates (which makes `JSON_TABLE` expand far too
  broadly).
- **Single self-contained changed-first SQL for iRODS.** A semijoin reduction of
  the current inner join; equivalence is guaranteed by construction, and proven
  by the byte-identical warm-vs-cold parity oracle across every recovery branch.
- **SQLite parity-oracle testing.** Correctness is proven against the hermetic
  SQLite source by asserting the warm mirror is byte-identical to a cold /
  current-path sync of the same fixture, so the reshapes cannot silently drop or
  alter rows.
- **Efficiency proven without wall-clock.** Wall-clock is not asserted. The two
  provable axes are: (i) the warm run issues the reshaped changed-first /
  from-cursor source queries and scopes composite recovery to exactly the
  Go-classified multi-component ids (and `iseq_run_status` to
  `id_run_status > watermark`), captured via `recordingSource`; and (ii)
  destination mirror write counts are zero on a no-op window and minimal on a
  few-change window, captured via `sqliteSyncSQLObserver`.
- **Zero-change warm post-state.** A zero-change warm sync of any of the four
  tables writes only `sync_state`, performs zero mirror-row writes, reports
  `SyncReport{Inserted:0, Updated:0}`, and advances `last_run`; finalize still
  runs when `state.Exists`. `high_water` is per-table, so the blanket "preserve
  high_water" does NOT hold uniformly:
  - `seq_product_irods_locations` and `iseq_product_metrics`: `high_water` is
    the max source `last_changed` seen, so with no changed rows it is PRESERVED
    at its prior value.
  - `iseq_run_status`: `high_water` stays empty (zero); the retained
    `id_run_status` resume cursor is PRESERVED, never NULL-ed.
  - `seq_ops_tracking_per_sample`: `high_water` is the wall-clock refresh time
    and ADVANCES to the new refresh time on every run (the full snapshot is
    always re-read), even when the diff is empty.

Implementors follow **go-implementor** (TDD) and **go-conventions**; tests
follow **testing-principles** (assert user-visible boundaries: mirror contents,
`SyncReport`, recorded source-query shape, recorded mirror write/commit counts -
never private helper internals). Reviewers follow **go-reviewer**.
