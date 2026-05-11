# Feature: Make `wa mlwh` correct, fast, and reliable

This spec is about the `mlwh` subsystem as a whole: syncing the cache,
maintaining it, and serving every consumer that reads from it. Speed
and reliability of `wa mlwh sync` are the entry point, but the spec
MUST also fix every "bodge" the audit below uncovers — places where the
code silently drops data, mis-models MLWH, mis-counts rows, returns
the wrong answer for an edge case, or papers over a many-to-many
relationship with a `LIMIT 1`.

Correctness for every read path listed in `.docs/mlwh/prompt.md` is
the bar. Performance work then makes those correct paths fast on both
the SQLite and MySQL cache backends.

## User bug reports

> `export WA_ENV=development; make dev &; .tmp/wa mlwh sync` hangs for
> 10s of minutes. I gave up before letting it complete. What is it doing,
> and is it reasonable that it takes so long? I feel like we should be
> able to just pull the entire relevant mlwh tables faster than this?
> See what queries we're doing for a sync, see if those work with your
> own direct queries against mlwh (see connection details in my
> .env.development.local), and see if it's the read or the write to our
> own cache that's an issue.

> I let a sync attempt run to completion, and it eventually fails with:
>
> ```
> [mysql] 2026/05/11 10:20:06 packets.go:100 unexpected EOF
> Error: mlwh: read sample sync source: invalid connection
> ```

## Diagnostic findings (verified against the live MLWH replica)

### What `wa mlwh sync` does today

Default invocation syncs three tables in a fixed order, defined by
`supportedSyncTables` in `mlwh/sync.go`:

1. `sample` → `sample_mirror` + `donor_samples` rows
2. `study` → `study_mirror` rows
3. `iseq_flowcell` → `library_samples` rows

Each table is processed by `Client.syncTable` (`mlwh/sync.go`):

- Open one cache transaction.
- Read the high-water mark from `sync_state` for that table; if absent,
  start at the zero `time.Time` (cold sync == full table scan).
- Stream rows from MLWH ordered by `last_updated, <pk>`.
- For every row: run a `SELECT 1 ... LIMIT 1` existence check, then
  either upsert (`sample_mirror`, `study_mirror`) or `DELETE` + `INSERT`
  (`donor_samples`, `library_samples`). For the flowcell table, an
  in-memory `seen` set deduplicates the `(pipeline_id_lims,
id_sample_tmp, id_study_lims)` tuple.
- Commit. Then write the `sync_state` row with the new high-water
  mark, but **only if the transaction committed** (so a killed cold
  sync replays the entire table next run).

Sync source SQL, copied verbatim from `mlwh/sync.go`:

```sql
-- sampleSyncSourceQuery
SELECT id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims,
       name, sanger_sample_id, supplier_name, accession_number,
       donor_id, taxon_id, common_name, description,
       COALESCE((SELECT study.id_study_lims
                 FROM iseq_flowcell
                 INNER JOIN study ON study.id_study_tmp = iseq_flowcell.id_study_tmp
                 WHERE iseq_flowcell.id_sample_tmp = sample.id_sample_tmp
                   AND study.id_lims = 'SQSCP'
                 LIMIT 1), '') AS id_study_lims,
       last_updated
FROM sample
WHERE id_lims = 'SQSCP' AND last_updated >= ?
ORDER BY last_updated, id_sample_tmp;

-- study (built dynamically from studyMirrorColumns)
SELECT <study cols>, last_updated
FROM study
WHERE id_lims = 'SQSCP' AND last_updated >= ?
ORDER BY last_updated, id_study_tmp;

-- flowcellSyncSourceQuery
SELECT iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp,
       COALESCE(study.id_study_lims, ''), iseq_flowcell.last_updated
FROM iseq_flowcell
LEFT JOIN study ON study.id_study_tmp = iseq_flowcell.id_study_tmp
WHERE iseq_flowcell.last_updated >= ?
  AND (study.id_lims = 'SQSCP' OR study.id_lims IS NULL)
ORDER BY iseq_flowcell.last_updated, iseq_flowcell.pipeline_id_lims,
         iseq_flowcell.id_sample_tmp, COALESCE(study.id_study_lims, '');
```

### Live MLWH timings (cold parameter `'0001-01-01T00:00:00Z'`)

| Probe                                                        |                           Rows |  Time |
| ------------------------------------------------------------ | -----------------------------: | ----: |
| `COUNT(*)` on all three filtered sources (single connection) | 10,295,943 + 8,170 + 9,198,300 |  42 s |
| `mysql --quick` streaming the exact study sync query         |                          8,170 |  <1 s |
| `mysql --quick` streaming the exact flowcell sync query      |                      9,198,300 |  76 s |
| `mysql --quick` streaming the exact sample sync query        |                     10,295,943 | 135 s |

So the upstream can serve everything we need in roughly 3–4 minutes
of pure read time. That is the floor.

### Source-side `EXPLAIN`

- Sample: ref-scans the `id_lims, id_sample_lims` index for ~5M rows,
  filesorts by `last_updated`, and runs a **DEPENDENT SUBQUERY** into
  `iseq_flowcell` + `study` for every output row.
- Flowcell: full table scan of `iseq_flowcell` (~8M rows reported), with
  `Using temporary; Using filesort` for the multi-key ORDER BY, then
  eq_ref into `study`.
- Study: trivial — 8k rows, ref + filesort.

### Cache-write timings (scratch SQLite under `.tmp/agent/`)

Same env, same binary, isolated empty cache file per run, 5-minute cap:

| `wa --env development mlwh sync --tables ...` | Result                                         |
| --------------------------------------------- | ---------------------------------------------- |
| `study`                                       | success in 4 s                                 |
| `iseq_flowcell`                               | killed at 240 s; 0 rows committed; 253 MiB WAL |
| `sample`                                      | killed at 300 s; 0 rows committed; 28 MiB WAL  |

Per-row `existence-check + upsert` (sample/study) or
`existence-check + DELETE + INSERT` (donor/flowcell child tables) inside
one transaction is the dominant cost. SQLite is in WAL mode with
`synchronous=FULL` (`mlwh/cache.go::sqliteWritableDSN`).

### Current cache schema (matters for write throughput)

- `sample_mirror`: PK on `id_sample_tmp`, plus 8 secondary indexes
  (`id_sample_lims`, `uuid_sample_lims`, `name`, `sanger_sample_id`,
  `supplier_name`, `accession_number`, `donor_id`, `last_updated`).
- `study_mirror`: PK on `id_study_tmp`, 3 secondary indexes.
- `library_samples`: **no primary or unique key**, 3 single-column
  indexes (`pipeline_id_lims`, `id_study_lims`, `id_sample_tmp`). Code
  treats `(pipeline_id_lims, id_sample_tmp, id_study_lims)` as the
  logical key (see `replaceLibrarySample`) but the schema does not
  enforce it.
- `donor_samples`: **no primary or unique key**, single index on
  `donor_id`. Code treats `id_sample_tmp` as the logical key
  (`replaceDonorSample` deletes by it before inserting) but the schema
  does not enforce it.
- MySQL DDL mirrors the same shape (no PK / unique keys on
  `library_samples`, `donor_samples`).

### The crash bug

A full cold sync to a real cache eventually crashes mid-stream with
`mysql: invalid connection` after `unexpected EOF` on the sample query.
The MLWH server (or an intermediate proxy) closes the streaming
connection while we are still consuming rows, because we hold it open
for many minutes while doing per-row SQLite work between `rows.Next()`
calls. There is no resume: the next sync starts again from the zero
high-water mark.

## Correctness bodges that must be fixed

The audit below was done against MLWH's real schema and the current
codebase. Every item must be resolved by this spec; resolutions are
specified in the "What this spec must deliver" section.

1. **`sample_mirror.id_study_lims` populated by correlated `LIMIT 1`
   subquery.** A sample can be prepped into libraries against many
   studies via `iseq_flowcell`; the subquery cherry-picks one. The
   single value then leaks into `donor_samples.id_study_lims`,
   `Sample.IDStudyLims`, and `StudyForSample`. References:
   `mlwh/resolver.go` `sampleStudyLimsSubquery`; `mlwh/sync.go`
   `sampleSyncSourceQuery`; `sample_mirror.id_study_lims` column.
2. **`donor_samples` schema treats `id_sample_tmp` as a unique key
   and carries one `id_study_lims`.** `replaceDonorSample`
   (`mlwh/sync.go`) DELETE+INSERTs on `id_sample_tmp` alone; the
   stored `id_study_lims` is the cherry-pick from #1.
3. **`StudyForSample` returns one study** via the cherry-picked
   `donor_samples.id_study_lims` (`mlwh/hierarchy.go`
   `studyForSampleSQL`).
4. **`Sample.LibraryType` is single-valued and is written empty by
   sync** (`sample_mirror` has no source library column), yet is
   keyed on by `seqmeta/enrich.go` (`libraryLinkForSample`,
   `buildSampleDetailFromProvider`, `buildStudyDetail`,
   `distinctLibrariesForSamples`). Read-through paths in
   `mlwh/hierarchy.go` populate it from `iseq_flowcell.pipeline_id_lims`
   with `LIMIT/OFFSET` non-determinism per sample.
5. **`ResolveSample` donor_id step silently takes the lowest
   `id_sample_tmp`** (`mlwh/resolver.go`), violating the resolver's
   own `ErrAmbiguous` contract for other steps.
6. **`resolveStudyFromCacheWithWarmup` skips ambiguity detection
   for `accession_number`** (`mlwh/resolver.go`), in contrast to
   `resolveStudyByName` which does `LIMIT 2` + ambiguity.
7. **`FindSamplesBy<Column>` family is wrong-shape.**
   `seqmeta/client_adapter.go`
   `FindSamplesBy{SangerID,IDSampleLims,AccessionNumber}` all funnel
   into `ResolveSample`, return at most one sample, and run the full
   text cascade (`name → sanger_sample_id → supplier_name →
accession_number`). A string that looks like a Sanger name
   resolves to a name match even when called from
   `FindSamplesByAccessionNumber`.
8. **`ClassifyIdentifier` integer-raw branch queries
   `uuid_sample_lims` with an integer** (`mlwh/resolver.go`).
   Copy-paste bug from the UUID branch.
9. **`COALESCE(NULL, '')` is written as data, then matched as
   data.** `flowcellSyncSourceQuery` writes
   `library_samples.id_study_lims=''` for missing/non-SQSCP study
   rows; `samplesForLibraryTypeSourceSQL` does the same on
   `sample_mirror`. Causes unique-key collisions on `''` and
   `WHERE id_study_lims = ''` returning everything-studyless.
10. **Pagination ordering is not deterministic.**
    `samplesForStudyCacheSQL`, `samplesForLibraryCacheSQL`,
    `samplesForLibraryTypeCacheSQL`, and their `*SourceSQL`
    counterparts (`mlwh/hierarchy.go`) use
    `ORDER BY <sample>.name LIMIT ? OFFSET ?` on a non-unique column.
11. **`upsertHierarchyReadThrough` writes mid-stream rows without a
    watermark** (`mlwh/hierarchy.go`). `sample_mirror` and
    `library_samples` rows from `LIMIT/OFFSET` source queries become
    visible to other readers without ever being a full sync.
12. **`ResolveLibrary` triggers a full incremental flowcell sync on
    every `ErrNotFound`** (`mlwh/resolver.go`), serialised through
    `syncMu`. A stream of frontend misses can starve real syncs.
13. **Lazy resolver warm syncs only the requested table**, but
    cross-table reads (`LibrariesForStudy`, `StudyForSample`,
    `samplesForStudyCacheSQL`) silently report partial data when
    only some of the three `sync_state` rows exist.

## What this spec must deliver

A reworked mlwh subsystem that resolves every bodge above and meets
the requirements below. The single observable command remains
`wa mlwh sync`: it prints
`<table> inserted=<n> updated=<n> high_water=<ts>` per table on
success, exits non-zero on failure, and works with either an SQLite
file or a MySQL DSN as the cache backend. The `--tables` flag is
removed; the command always syncs all five supported tables (see
section 2). The new mlwh subsystem treats the cache as authoritative:
the cache is populated and refreshed exclusively by admin-run
`wa mlwh sync` (manually or by cron). There is no lazy / read-time
warm and no live-MLWH fallback in any read path. A fresh-install
cache that has never been synced answers every read with `ErrNotFound`
or an empty result; the CLI / API responds with an actionable hint:
`mlwh: cache has never been synced; run "wa mlwh sync" first`.

### 1. Cache schema changes

**Design principle: the cache is optimised for the queries we run,
not a mirror of MLWH's source schema.** Every column, index, PK
choice and denormalisation below is justified by an explicit read
path in `.docs/mlwh/prompt.md` (resolver lookups, hierarchy reads,
run / lane / iRODS reads, seqmeta enrichment, results search). The
spec author MUST, as part of authoring the spec, produce a per-table
audit table that lists for each cached column the read paths that
consume it; any column with no consumer is dropped from the cache
schema. The cache enforces no foreign-key constraints (referential
integrity is a sync-time concern, not a write-time one) and uses
unique constraints solely to power idempotent upserts.

The schema is versioned; this spec bumps the version. `OpenCache` is
the SOLE place that detects an older `schema_version` and rebuilds
affected tables (drops and recreates them, clears their `sync_state`
rows). `wa mlwh sync` does not duplicate that logic; it calls
`OpenCache` first and then proceeds. On migration `OpenCache` prints
one stderr line of the form
`mlwh cache: schema vX->vY, recreated tables: [t1, t2, ...]` and
nothing when no migration runs. Both SQLite and MySQL dialects must
stay in lock-step; the existing `mlwh/cache_schema/{sqlite,mysql}`
parity test must keep passing.

Cross-cutting layout rules applied to every cached table:

- **Integer PKs are declared as `INTEGER PRIMARY KEY` in SQLite** so
  they collapse onto the rowid and skip a B-tree level on every
  lookup and join. The MySQL equivalents use `BIGINT PRIMARY KEY`.
  This applies to `sample_mirror.id_sample_tmp`,
  `study_mirror.id_study_tmp`,
  `iseq_product_metrics_mirror.id_iseq_product`, and
  `seq_product_irods_locations_mirror.id_iseq_product`.
- **No foreign-key constraints** are declared on any cached table.
- **Text columns used for equality lookup and prefix search**
  (`sample_mirror.name`, `sample_mirror.sanger_sample_id`,
  `sample_mirror.id_sample_lims`, `sample_mirror.supplier_name`,
  `sample_mirror.accession_number`, `sample_mirror.uuid_sample_lims`,
  `study_mirror.name`, `study_mirror.accession_number`,
  `study_mirror.uuid_study_lims`) use a case-insensitive collation
  consistently across both dialects: `COLLATE NOCASE` in SQLite and
  `utf8mb4_0900_ai_ci` (or the closest available case-insensitive
  utf8mb4 collation; falling back to `utf8mb4_general_ci` on older
  MySQL) in MySQL. The collation is declared on the column so an
  index on the column can serve `LIKE 'prefix%'` lookups without
  a separate functional index. The spec author must audit which
  text columns are searched with LIKE vs equality and apply the
  collation only where it makes a difference.
- **Secondary indexes are derived from the read-path audit, not
  copied from MLWH.** Each table's index list in this spec is the
  starting point; the spec author may consolidate redundant
  single-column indexes into a covering composite, or drop an
  index whose only consumer was removed by the resolver fixes in
  section 3, provided every read path retains an index that can
  serve it.

The set of cached tables grows from three to five so every read path
in `.docs/mlwh/prompt.md` can be served entirely from the cache:

- existing: `sample_mirror`, `study_mirror`, `library_samples`,
  `donor_samples` (already in the cache today).
- new: `iseq_product_metrics_mirror` mirroring the columns of
  `iseq_product_metrics` that the run / lane read paths use
  (id_iseq_product, id_run, position, tag_index, id_iseq_flowcell_tmp,
  qc, qc_lib, qc_seq, last_updated, plus any other columns the audit
  shows are read today by `RunsForStudy`, `LanesForSample`,
  `SamplesForRun`).
- new: `seq_product_irods_locations_mirror` mirroring the columns of
  `seq_product_irods_locations` used by `IRODSPathsForSample` and
  `IRODSPathsForStudy` (id_iseq_product, irods_root_collection,
  irods_data_relative_path, irods_collection, irods_file_name,
  last_updated, plus any other columns these read paths use).

The spec author MUST audit the existing `*SourceSQL` for these read
paths and ensure the mirror schemas cover every column they read.

Schema changes in this version:

- **`sample_mirror.id_study_lims` column is removed.** The
  many-to-many between samples and studies lives entirely in
  `library_samples`. `Sample.IDStudyLims` is removed from
  `mlwh/types.go`. `mlwh/resolver.go` `sampleMirrorSelectColumns`
  drops the column. Row scanners are updated.
- **`sample_mirror.library_type` column is removed.** The column was
  never written by sync and was only ever populated mid-flight by
  read-through paths. `Sample.LibraryType` is removed. Library
  information is read from `library_samples` joined on
  `id_sample_tmp` (and `id_study_lims` when the caller's scope is a
  single study).
- **`donor_samples` becomes the pure donor → sample mapping.**
  Columns: `donor_id`, `id_sample_tmp`. `id_study_lims` is removed.
  `UNIQUE(donor_id, id_sample_tmp)` constraint added (composite: a
  donor has many samples, and a sample can in principle appear under
  more than one donor row).
- **`library_samples` gains `UNIQUE(pipeline_id_lims, id_sample_tmp,
id_study_lims)`** in both dialects, replacing the
  per-row DELETE+INSERT pattern with idempotent upsert
  (`INSERT … ON CONFLICT … DO UPDATE` / `INSERT … ON DUPLICATE KEY
UPDATE`).
- **`library_samples.id_study_lims` is `TEXT NOT NULL` with a
  `CHECK(id_study_lims <> '')`** (or the MySQL equivalent). The
  source query and sync filter never write `''`; rows whose upstream
  study cannot be resolved to an SQSCP `id_study_lims` are skipped
  entirely.
- **`iseq_product_metrics_mirror`** gets a PK on `id_iseq_product`,
  a denormalised `id_study_lims TEXT NOT NULL` column copied from
  the row's parent flowcell at sync time (so study-scoped run / lane
  reads filter without a join through `iseq_flowcell`), and the
  following secondary indexes derived from the read-path audit:
  `(id_run, position, tag_index)` to serve `SamplesForRun` /
  `LanesForSample`, `(id_iseq_flowcell_tmp)` to serve
  `LanesForFlowcell` / sample-from-product joins, and
  `(id_study_lims, id_run, position)` to serve `RunsForStudy` /
  `LanesForStudy` as a single index seek.
- **`seq_product_irods_locations_mirror`** gets a PK on
  `id_iseq_product` (one iRODS record per product), the same
  denormalised `id_study_lims TEXT NOT NULL` column (copied at sync
  time from the flowcell that owns the product), and an index on
  `(id_study_lims)` so `IRODSPathsForStudy` reads a contiguous
  range without joining flowcell.
- **`sync_state` gains two columns**:
    - `resume_cursor TEXT NULL` — a compact, deterministic encoding
      of the ordering tuple of the last row in the last committed
      batch (tab-separated fields, with RFC3339Nano for the
      `last_updated` part). Used as a strict `>` keyset predicate on
      resume. Set to `NULL` at end-of-stream. A NULL `resume_cursor`
      with a non-zero `high_water` means "no resume in progress;
      start the next incremental scan from `last_updated > high_water`
      and capture a fresh cursor on the first committed batch".
    - `indexes_dropped INTEGER NOT NULL DEFAULT 0` — flag indicating
      that secondary indexes for the table are currently dropped
      (set in the same transaction that drops them, cleared in the
      same transaction that recreates them).

### 2. Sync engine rewrite

- **Five tables synced in parallel.** All five tables
  (`study`, `sample`, `iseq_flowcell`, `iseq_product_metrics`,
  `seq_product_irods_locations`) are synced as concurrent
  goroutines, each with its own MLWH connection and its own batched
  commit loop, started from a single `wa mlwh sync` invocation
  after `OpenCache` returns. Total wall-clock is the maximum of the
  per-table wall-clocks (rather than the sum). The SQLite cache's
  writer lock will partially serialise commits in practice and that
  is acceptable; no special branching on backend is required. Each
  table's `<table> inserted=… updated=… high_water=…` summary is
  printed as that table finishes (not in a fixed pre-order), so the
  output ordering depends on completion order. The command exits
  non-zero if any table fails; the error names the failing tables.
- **Batched prepared multi-row INSERTs** at a fixed package-level
  constant batch size of **1000** rows. No env-var or flag override.
- **No per-row existence check.** Idempotent upserts use the unique
  keys defined in section 1.
- **No correlated subqueries in source SQL.** The sample source
  query drops the correlated `id_study_lims` subquery from
  `sampleSyncSourceQuery` entirely. `COALESCE(..., '')` wrappers are
  removed from all source queries so the Go scanner sees real NULLs.
- **Source filter tightening.** The flowcell source query's
  `study.id_lims IS NULL` half of the OR is dropped; only rows with
  a resolvable SQSCP study are inserted. There is therefore no
  back-fill, no garbage-collection of `''` rows, and no
  `WHERE id_study_lims = ''` read path anywhere. The same rule
  applies to the two new mirror tables: the source queries for
  `iseq_product_metrics` and `seq_product_irods_locations` join
  through `iseq_flowcell` and SQSCP `study` to attach
  `id_study_lims` to each row, and rows whose flowcell does not
  resolve to an SQSCP study are skipped (never inserted with an
  empty `id_study_lims`).
- **Resume semantics.** Each batch is committed in its own
  transaction. The `resume_cursor` for the table is written in the
  same transaction as the batch's rows, so it is always consistent
  with the cache contents. On resume mid-table, the source query is
  re-issued with the cursor as a strict `>` keyset predicate. The
  ordering tuples are:
    - `study`: `(last_updated, id_study_tmp)`
    - `sample`: `(last_updated, id_sample_tmp)`
    - `iseq_flowcell`: `(last_updated, pipeline_id_lims,
id_sample_tmp, id_study_lims)`
    - `iseq_product_metrics`: `(last_updated, id_iseq_product)`
    - `seq_product_irods_locations`: `(last_updated, id_iseq_product)`
- **Cold-load index handling for `sample_mirror`.** "Cold" means
  the `sample_mirror` `sync_state` row is absent or its `high_water`
  is the zero value (`time.Time{}`). On cold load: drop the eight
  secondary indexes in a transaction that also sets
  `indexes_dropped = 1`, bulk-load, then recreate the indexes in a
  transaction that clears `indexes_dropped = 0`. Incremental syncs
  (non-zero `high_water`) keep indexes live throughout. The same
  drop/recreate rule applies in both SQLite and MySQL, and to any
  other future table whose secondary index count justifies it. The
  initial spec only drops indexes for `sample_mirror`; the other
  four tables keep indexes live throughout (each has only a handful).
- **Recovery if a sync dies after the final batch but before index
  recreation.** When `OpenCache` sees `indexes_dropped = 1` on a
  cache whose `high_water` is non-zero, it recreates the expected
  secondary indexes and clears the flag in its own transaction.
  Standalone cold-load resumes (`indexes_dropped = 1` with zero
  `high_water`) continue from the resume cursor with indexes still
  off and recreate after their own final batch.
- **Upstream reconnect / retry policy.** When the upstream MLWH
  driver returns `invalid connection`, `unexpected EOF`, or any
  other transient I/O error, the table's sync reopens the upstream
  connection and re-issues its source query from `resume_cursor`.
  Up to 5 attempts total per fault, exponential backoff starting at
  1 s and doubling each attempt, capped at 30 s per wait. After 5
  failures the table's sync fails (other concurrently-running
  tables continue to completion), and the overall command exits
  non-zero naming every failed table. Each retry emits one line to
  stderr:
  `mlwh sync: <table> reconnecting attempt N/5 after <err>: backoff <duration>`.
  Successful retries do not print an extra line; the final
  per-table summary on stdout is unchanged.
- **Source rows that fail a cache CHECK constraint** (e.g. an
  upstream-corrupt row that produces an empty `id_study_lims` after
  filter) are dropped by the source-side WHERE filter, not the
  cache-side CHECK. The CHECK is a defence-in-depth assertion; if
  any batch insert ever trips it, that is a programming error and
  the table sync fails immediately with a non-zero exit and an
  error naming the offending row's primary key.
- **SQLite write-side pragma tuning during sync.** On every SQLite
  sync connection (one per concurrent table; resolver / read
  connections keep their existing defaults), set
  `synchronous=NORMAL`, `cache_size=-200000` (200 MiB), and
  `temp_store=MEMORY` for the duration of the sync. WAL stays on
  throughout. Pre-sync values are restored before the command
  returns, regardless of success or failure.
- **MySQL streaming reads.** The MySQL driver is used in unbuffered
  streaming mode (one streaming connection per concurrent table)
  so the source cursors do not buffer all rows client-side.
- **Per-cache advisory lock.** `wa mlwh sync` takes a per-cache
  advisory lock at startup and refuses to run concurrently against
  the same cache. SQLite uses an immediate transaction on a
  dedicated `sync_lock` row; MySQL uses
  `GET_LOCK('wa_mlwh_sync_<cache_id>', 0)`. If the lock cannot be
  acquired, the command exits non-zero with
  `mlwh sync: another sync is already running against this cache`
  and prints no per-table summary lines. The lock is released on
  normal exit, on error, and on signal. Read-side resolver /
  hierarchy paths do not take this lock (they never write).

### 3. Read-path correctness fixes

- **No live-MLWH read fallback anywhere.** All `*SourceSQL` paths in
  `mlwh/hierarchy.go`, the live-MLWH fallback in
  `mlwh/all_studies.go`, and any other read-side code that opens an
  MLWH connection are removed. The cache is the sole source of
  truth for every resolver, hierarchy, run, lane, iRODS, and
  `AllStudies` read. The spec must call out that
  `.docs/mlwh/prompt.md`'s "fall back to a single direct MLWH query
  and write the result back" sentence is superseded by this spec.
- **No mid-read cache writes.** `upsertHierarchyReadThrough` is
  removed. Hierarchy reads never write to any cache table.
- **`StudyForSample` is replaced by
  `StudiesForSample(id_sample_tmp) ([]Study, error)`**, joining
  `library_samples` → `study_mirror` and returning the full set
  ordered by `id_study_lims`. All callers are updated.
- **Sample fan-out across studies / libraries.** The audit-driven
  principle is: callers are updated to match MLWH's real shape, with
  no backwards compatibility. Per call-site:
    - `cmd/mlwh_info.go` — prints the full list of
      `(pipeline_id_lims, id_study_lims)` pairings drawn from
      `library_samples` for the sample, one line per pairing.
    - `seqmeta/enrich.go` (sample-detail enrichment, study-detail
      enrichment, `distinctLibrariesForSamples`,
      `libraryLinkForSample`, the "studies seen" / "ordered study
      IDs" loops) — a sample carries `Studies []Study` and
      `Libraries []Library` slices populated from `library_samples`;
      enrichment iterates the slices rather than reading a single
      field. `Library` records are keyed by
      `(PipelineIDLims, IDStudyLims)`.
    - `results/mlwh_search_resolver.go` and `results/server.go`
      search-and-expand paths — search results emit one row per
      `(sample, study)` pairing where a sample is associated with
      multiple studies, so existing filters and display rows behave
      as if MLWH had distinct rows.
    - `seqmeta/diff.go` / `seqmeta/validate.go` — diff/validate
      iterate `Studies` / `Libraries` slices on the sample and
      compare per-pairing. Tests are updated accordingly.
      The spec author MUST enumerate any further callers discovered
      during research and assign one of these two shapes
      (per-pairing rows vs per-sample with slice fields) using the rule
      of thumb: callers that produce display / filterable rows fan out;
      callers that walk per-sample state keep one struct with slices.
- **Ambiguity rules across the resolver.** `LIMIT 2` + `ErrAmbiguous`
  is applied consistently wherever the underlying column is not
  guaranteed unique in MLWH:
    - `ResolveSample` `donor_id` step.
    - `resolveStudyFromCacheWithWarmup` `accession_number` step
      (note: the helper is renamed to drop "WithWarmup" since there
      is no warmup any more, but the body retains its lookup logic).
    - `seqmeta/client_adapter.go`
      `FindSamplesBy{SangerID,IDSampleLims,AccessionNumber,
SupplierName,LibraryType}` (see next bullet).
    - The cross-column `FindSample(text)` cascade also returns
      `ErrAmbiguous` when two different columns each match different
      samples for the same input text.
      Lookups where MLWH guarantees uniqueness by construction
      (`id_study_lims`, `uuid_study_lims`) remain single-result. The
      spec calls out this asymmetry explicitly.
- **`FindSamplesBy<Column>` is one column per method.** Each method
  queries ONLY the column its name advertises, with the SQSCP
  `id_lims` filter where appropriate:
    - `FindSamplesBySangerID` → `WHERE sanger_sample_id = ?`
    - `FindSamplesByIDSampleLims` → `WHERE id_sample_lims = ?`
    - `FindSamplesByAccessionNumber` → `WHERE accession_number = ?`
    - `FindSamplesBySupplierName` → `WHERE supplier_name = ?`
    - `FindSamplesByLibraryType` →
      `SELECT … FROM library_samples INNER JOIN sample_mirror ON
    sample_mirror.id_sample_tmp = library_samples.id_sample_tmp
    WHERE library_samples.pipeline_id_lims = ?
    ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp
    LIMIT 2`
      Each runs `LIMIT 2`. One match returns a single-element slice.
      Two-or-more matches return `ErrAmbiguous` with both candidate
      identifiers named in the error. Zero matches returns
      `ErrNotFound`.
- **`ClassifyIdentifier` integer-raw branch** drops the spurious
  `uuid_sample_lims = ?` lookup. Only the UUID branch queries that
  column.
- **Pagination ordering is fully deterministic.** Every
  `ORDER BY <sample>.name LIMIT ? OFFSET ?` in `mlwh/hierarchy.go`
  gains `, sample_mirror.id_sample_tmp` as the tie-breaker. Tests
  assert stable pagination across pages.

### 4. Empty-cache semantics

There is no lazy / read-time warm and no automatic re-sync triggered
by `ErrNotFound`. The cache is populated exclusively by
`wa mlwh sync` (admin-run, manually or by cron). The classifications
are:

- **Never-synced cache**: every table's `sync_state` row is absent.
  Reads return `ErrNotFound` (or an empty slice for list endpoints).
  Resolver and hierarchy APIs include a typed sentinel
  `ErrCacheNeverSynced` so CLI/API surfaces can render the actionable
  hint `mlwh: cache has never been synced; run "wa mlwh sync" first`.
- **Partially-synced cache** (one or more `sync_state` rows present
  with non-zero `high_water`, others absent or zero): reads against
  populated tables work; reads that depend on absent tables behave
  as if those rows do not exist (`ErrNotFound` / empty). A sync
  that resumed and finished one table but failed another leaves the
  cache in this state, and a subsequent `wa mlwh sync` picks up from
  the resume cursor.
- **Fully-synced cache**: every `sync_state` row has non-zero
  `high_water`. Reads return what is in the cache; absences are
  authentic `ErrNotFound`s.
- The `indexes_dropped` flag is orthogonal to all of the above;
  `OpenCache` rebuilds dropped indexes whenever it sees the flag
  set on a non-zero-high-water table, regardless of whether the
  caller is a read or a write.

The `ResolveLibrary` / `ErrNotFound` retry-via-sync path is removed.
The `ensureResolverTableSynced` / `hasResolverSyncState` helpers are
removed. `ExpandIdentifier`, `ResolveLibrary`, the `donor_id` step
of `ResolveSample`, and every other resolver entry point just read
the cache.

### 5. Performance budget

Cold-sync speed is gated per table, not in aggregate. An integration
test asserts that each of the five tables completes its cold sync
in no more than **2x** the wall-clock time of streaming the
equivalent source SQL in unbuffered streaming mode through the Go
MySQL driver, measured on the same host in the same test run
(against a fresh empty cache). There is no aggregate wall-clock cap.

The per-table 2x budget covers everything `wa mlwh sync` does for
that table end-to-end: the row read, the batched inserts, and the
post-cold-load index recreation (only relevant for `sample_mirror`).
No post-sync back-fill or cross-table fix-up exists in the new
design.

The integration test does not shell out to the `mysql` client binary;
it times the same SQL through the Go MySQL driver. The test is gated
by a dedicated env var `MLWH_SYNC_PERF_TEST=1` **in addition to** the
existing `WA_MLWH_DSN` / `WA_MLWH_PASSWORD` requirements, so it does
not run on every developer workstation. When the gate is unset the
test skips cleanly.

## Reference material

- `.docs/mlwh/prompt.md` — the original mlwh spec describing every
  read path the cache must serve. Anything fast there must remain
  fast here. This spec supersedes its "fall back to a single direct
  MLWH query and write the result back" sentence.
- `mlwh/sync.go`, `mlwh/cache.go`, `mlwh/cache_schema.go`,
  `mlwh/cache_schema/{sqlite,mysql}/*.sql` — current implementation.
- `mlwh/resolver.go`, `mlwh/resolver_sample.go`,
  `mlwh/resolver_reject.go`, `mlwh/hierarchy.go`,
  `mlwh/all_studies.go`, `mlwh/types.go` — read paths.
- `results/mlwh_search_resolver.go`,
  `seqmeta/enrich.go`, `seqmeta/client_adapter.go`,
  `seqmeta/server.go` — consumers that need updating.
- `cmd/mlwh.go`, `cmd/mlwh_info.go` — CLI surface that must not
  break (except for the `--tables` flag removal and the
  `mlwh info` study-fan-out change).

## Out of scope

- Adding new MLWH source tables beyond the five listed in section 1
  (long-read platforms, AVUs, etc.).
- Replacing `modernc.org/sqlite` or the MySQL driver.
- Cosmetic / unrelated refactors of `seqmeta`, `results`, or the
  frontend. Changes to those packages are in scope only where this
  spec's correctness fixes or cache-schema migrations force them.
- Frontend UX for the empty-cache hint message (the API returns a
  typed `ErrCacheNeverSynced`; presenting it well is left to the
  frontend team).
