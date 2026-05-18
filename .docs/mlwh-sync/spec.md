# MLWH Sync and Cache Correctness Specification

## Overview

`wa mlwh` keeps a local SQLite or MySQL cache of selected Sanger
Multi-LIMS Warehouse (MLWH) tables that powers every sample / study /
run / library / iRODS read path in `wa` (identifier resolution,
hierarchy walks, `seqmeta` enrichment and diff, `wa results` search
expansion). The current implementation hangs on cold sync, eventually
crashes mid-stream with `mysql: invalid connection`, silently collapses
many-to-many sample <-> study relationships via correlated `LIMIT 1`
subqueries, writes mid-stream rows from read paths, and falls back to
direct MLWH queries when the cache misses.

This spec replaces the sync engine, redesigns the cache schema around
the read paths it actually serves, and removes every live-MLWH read
path. The cache is authoritative: it is populated and refreshed only
by admin-run `wa mlwh sync` (manually or by cron). All reads come
from the cache; an empty cache returns `ErrNotFound` plus a typed
`ErrCacheNeverSynced` sentinel that surfaces an actionable hint
(`mlwh: cache has never been synced; run "wa mlwh sync" first`).

Key behaviours:

- Five upstream tables are synced in parallel: `study`, `sample`,
  `iseq_flowcell`, `iseq_product_metrics`,
  `seq_product_irods_locations`. Each runs in its own goroutine with
  its own MLWH streaming connection.
- Cache schema is versioned and migrated by `OpenCache`. Cache tables
  carry only columns that the audited read paths consume; PKs collapse
  to rowid (SQLite) / clustered (MySQL); no FK constraints;
  case-insensitive collation on text columns used in equality / prefix
  search.
- Per-table batched 1000-row idempotent upserts via unique keys.
  Resume mid-table via `sync_state.resume_cursor` (strict `>` keyset).
  Cold `sample_mirror` load drops and recreates its 8 secondary
  indexes around bulk load; an `indexes_dropped` flag survives crashes
  and is repaired by `OpenCache`.
- Up to 5 reconnect attempts per upstream fault (exponential backoff
  1 s -> 30 s, one stderr line per attempt). Per-cache advisory lock
  prevents concurrent syncs.
- Per-table 2x wall-clock perf gate (vs. unbuffered streaming of the
  equivalent source SQL) gated by `MLWH_SYNC_PERF_TEST=1`.
- Sample <-> study fan-out: `sample_mirror.id_study_lims` and
  `sample_mirror.library_type` are removed; the many-to-many lives in
  `library_samples`. `StudyForSample` becomes `StudiesForSample`;
  callers fan out per-pairing or carry `Studies` / `Libraries`
  slices on the sample. `FindSamplesBy<Column>` is one column per
  method with `LIMIT 2` + `ErrAmbiguous`. Ambiguity rules apply
  consistently wherever the underlying column is not unique in MLWH.

This spec supersedes the "fall back to a single direct MLWH query
and write the result back" sentence in `.docs/mlwh/prompt.md`.

## Architecture

### Packages and files

- `mlwh/` - cache, sync engine, resolver, hierarchy reads.
    - `cache.go` - cache open / close, dialect dispatch, advisory lock.
    - `cache_schema.go` + `cache_schema/{sqlite,mysql}/*.sql` -
      embedded DDL, parity test.
    - `sync.go` - per-table sync goroutines, batching, retry,
      resume, index drop/recreate.
    - `resolver.go`, `resolver_sample.go`, `resolver_reject.go` -
      identifier resolution (cache-only).
    - `hierarchy.go` - cache-only hierarchy reads.
    - `all_studies.go` - cache-only `AllStudies`.
    - `types.go`, `mlwh.go` - public types and sentinels.
- `cmd/mlwh.go`, `cmd/mlwh_info.go` - CLI.
- `seqmeta/{enrich,diff,validate,client_adapter,provider,server}.go`
    - consumers updated for fan-out, `StudiesForSample`,
      `FindSamplesBy*` shape changes.
- `results/{server,mlwh_search_resolver}.go` - search expansion
  fans out per (sample, study) pairing.

### Public types (changes from current)

```go
package mlwh

// Sample loses IDStudyLims and LibraryType. Many-to-many study /
// library data lives on slices populated from library_samples.
type Sample struct {
    IDSampleTmp     int64
    IDLims          string
    IDSampleLims    string
    UUIDSampleLims  string
    Name            string
    SangerSampleID  string
    SupplierName    string
    AccessionNumber string
    DonorID         string
    TaxonID         int
    CommonName      string
    Description     string

    // Optional per-sample fan-out, populated by hierarchy reads that
    // walk library_samples. Empty for raw resolver hits.
    Studies   []Study
    Libraries []Library
}

// Library identifies one (pipeline_id_lims, id_study_lims) pairing.
type Library struct {
    PipelineIDLims string
    IDStudyLims    string
}

// ErrCacheNeverSynced is returned (wrapped) by every read entry point
// when no sync_state row exists for any table needed to answer the
// read. Wraps ErrNotFound so existing errors.Is(..., ErrNotFound)
// callers keep working.
var ErrCacheNeverSynced = errors.New(
    "mlwh: cache has never been synced; run \"wa mlwh sync\" first",
)
```

### Cache schema (version 2)

`CacheSchemaVersion` bumps from 1 to 2. `OpenCache` is the SOLE
migrator: when it reads a `schema_version` row whose value is less
than the embedded version, it drops every cache table whose shape
changed, drops the matching `sync_state` rows, recreates the tables
from the embedded DDL, and updates `schema_version`. On migration it
prints one stderr line: `mlwh cache: schema vX->vY, recreated
tables: [t1, t2, ...]`. No stderr line is printed when no migration
runs. `wa mlwh sync` does not duplicate this logic; it calls
`OpenCache` first.

Cross-cutting rules applied to every table:

- Integer PKs declared `INTEGER PRIMARY KEY` (SQLite) /
  `BIGINT PRIMARY KEY` (MySQL) so they collapse onto rowid /
  clustered key.
- No foreign-key constraints.
- Text columns used for equality / prefix lookup use a
  case-insensitive collation: `COLLATE NOCASE` (SQLite) and
  `utf8mb4_0900_ai_ci` (MySQL, falling back to `utf8mb4_general_ci`
  on MySQL < 8). Applied to the columns listed in each table's
  collation set below. Other text columns inherit the default
  binary / case-sensitive collation.
- Indexes are derived from the read-path audit below. Each column
  with a consumer has an index that can serve it.

#### Per-table read-path audit

Every cached column must be traceable to a read path; columns with no
consumer are dropped. The audit below is the authoritative list.

`sample_mirror`:

| Column           | Consumers                                                    |
| ---------------- | ------------------------------------------------------------ |
| id_sample_tmp    | PK; JOIN target for `library_samples`, `donor_samples`,      |
|                  | `iseq_product_metrics_mirror`, `LanesForSample`,             |
|                  | `IRODSPathsForSample`.                                       |
| id_lims          | SQSCP filter on every resolver lookup.                       |
| id_sample_lims   | `resolveSampleByLimsID`, `FindSamplesByIDSampleLims`,        |
|                  | `ClassifyIdentifier` integer branch.                         |
| uuid_sample_lims | `resolveSampleByUUID`, `ClassifyIdentifier` UUID branch.     |
| name             | `ResolveSample` Sanger name step, canonical output, JOINs    |
|                  | by Sanger name in seqmeta enrichment, `LanesForSample`.      |
| sanger_sample_id | `FindSamplesBySangerID`, `ResolveSample` sanger_sample_id    |
|                  | step.                                                        |
| supplier_name    | `FindSamplesBySupplierName`, `ResolveSample` supplier step.  |
| accession_number | `FindSamplesByAccessionNumber`, `ResolveSample`              |
|                  | accession step.                                              |
| donor_id         | `ResolveSample` donor_id step (with LIMIT 2 + ErrAmbiguous). |
| taxon_id         | `cmd/mlwh info` sample display, seqmeta enrichment payload.  |
| common_name      | seqmeta enrichment payload, `cmd/mlwh info` sample display.  |
| description      | seqmeta enrichment payload.                                  |
| last_updated     | Watermark advancement (transient; not exposed to readers).   |

Dropped from previous schema: `id_study_lims` (now in
`library_samples`), `library_type` (now in `library_samples`),
`sanger_id` (duplicate of `name`).

Collation set: `id_sample_lims`, `uuid_sample_lims`, `name`,
`sanger_sample_id`, `supplier_name`, `accession_number`.

Indexes (8 in total - all dropped on cold load, recreated after
bulk load):

- `(id_sample_lims)`
- `(uuid_sample_lims)`
- `(name)`
- `(sanger_sample_id)`
- `(supplier_name)`
- `(accession_number)`
- `(donor_id)`
- `(last_updated)`

`study_mirror`:

| Column                      | Consumers                                |
| --------------------------- | ---------------------------------------- |
| id_study_tmp                | PK; JOIN target for `runsForStudy`.      |
| id_lims                     | SQSCP filter.                            |
| id_study_lims               | Canonical study key; every               |
|                             | StudiesForSample / library_samples join. |
| uuid_study_lims             | `ResolveStudy` UUID step.                |
| name                        | `ResolveStudy` name step, enrichment.    |
| accession_number            | `ResolveStudy` accession step.           |
| study_title                 | Enrichment payload.                      |
| faculty_sponsor             | Enrichment payload.                      |
| state                       | Enrichment payload.                      |
| data_access_group           | Enrichment payload.                      |
| programme                   | Enrichment payload.                      |
| reference_genome            | Enrichment payload.                      |
| ethically_approved          | Enrichment payload.                      |
| study_type                  | Enrichment payload.                      |
| contains_human_dna          | Enrichment payload.                      |
| contaminated_human_dna      | Enrichment payload.                      |
| study_visibility            | Enrichment payload.                      |
| ega_dac_accession_number    | Enrichment payload.                      |
| ega_policy_accession_number | Enrichment payload.                      |
| data_release_strategy       | Enrichment payload.                      |
| data_release_timing         | Enrichment payload.                      |
| last_updated                | Watermark advancement.                   |

Dropped: `abstract`, `abbreviation`, `description`,
`hmdmc_number`, `created` (no current read-path consumers, verified
by grepping the seqmeta payload fixtures and the `cmd/mlwh info`
study display).

Collation set: `id_study_lims`, `uuid_study_lims`, `name`,
`accession_number`.

Indexes:

- `(id_study_lims)`
- `(uuid_study_lims)`
- `(accession_number)`
- `(name)`

`library_samples` (one row per
`(pipeline_id_lims, id_sample_tmp, id_study_lims)` triple):

| Column           | Consumers                                        |
| ---------------- | ------------------------------------------------ |
| pipeline_id_lims | `FindSamplesByLibraryType`, `LibrariesForStudy`, |
|                  | `SamplesForLibrary`, `SamplesForLibraryType`,    |
|                  | `ResolveLibrary`.                                |
| id_sample_tmp    | JOIN to `sample_mirror`; `StudiesForSample`,     |
|                  | `LibrariesForSample`.                            |
| id_study_lims    | `SamplesForStudy`, `LibrariesForStudy`,          |
|                  | `StudiesForSample`. NOT NULL. CHECK(<> '').      |

Unique constraint:
`UNIQUE(pipeline_id_lims, id_sample_tmp, id_study_lims)` (powers
idempotent upserts; replaces per-row DELETE+INSERT).

Collation set: `pipeline_id_lims`, `id_study_lims`.

Indexes (derived from the audit; the unique key already covers
`pipeline_id_lims` prefix scans, so no separate single-column index
on it):

- The UNIQUE acts as the `(pipeline_id_lims, id_sample_tmp,
id_study_lims)` index.
- `(id_sample_tmp, id_study_lims)` to serve `StudiesForSample` /
  `LibrariesForSample`.
- `(id_study_lims, pipeline_id_lims, id_sample_tmp)` to serve
  `SamplesForStudy`, `LibrariesForStudy`, `SamplesForLibrary` with
  no extra lookup.

`donor_samples`:

| Column        | Consumers                                                 |
| ------------- | --------------------------------------------------------- |
| donor_id      | `ResolveSample` donor_id step (`LIMIT 2 + ErrAmbiguous`). |
| id_sample_tmp | JOIN to `sample_mirror`.                                  |

Unique constraint: `UNIQUE(donor_id, id_sample_tmp)`.
Collation set: `donor_id`.
Indexes: `(donor_id)` (covered by the UNIQUE).
Dropped from previous schema: `id_study_lims`.

`iseq_product_metrics_mirror` (new):

| Column               | Consumers                                           |
| -------------------- | --------------------------------------------------- |
| id_iseq_product      | PK (`BIGINT`/`INTEGER`); JOIN to                    |
|                      | `seq_product_irods_locations_mirror`.               |
| id_iseq_flowcell_tmp | JOIN to `library_samples` via flowcell -> sample.   |
| id_run               | `RunsForStudy`, `SamplesForRun`, `LanesForSample`.  |
| position             | `LanesForSample`, `LanesForStudy`.                  |
| tag_index            | `LanesForSample`.                                   |
| id_sample_tmp        | Denormalised from `iseq_flowcell` at sync time so   |
|                      | `LanesForSample` reads without an iseq_flowcell     |
|                      | join.                                               |
| id_study_lims        | Denormalised from the row's parent flowcell         |
|                      | `iseq_flowcell.id_study_tmp -> study.id_study_lims` |
|                      | (SQSCP) at sync time. NOT NULL. CHECK(<> '').       |
|                      | Powers `RunsForStudy` / `LanesForStudy`.            |
| qc                   | `cmd/mlwh info` run display, enrichment payload.    |
| qc_lib               | Enrichment payload.                                 |
| qc_seq               | Enrichment payload.                                 |
| last_updated         | Watermark advancement.                              |

Collation set: `id_study_lims`.

Indexes:

- `(id_run, position, tag_index)` - `SamplesForRun`, `LanesForSample`.
- `(id_sample_tmp, id_run, position, tag_index)` - `LanesForSample`
  (covers sample -> lanes without `iseq_flowcell` join).
- `(id_iseq_flowcell_tmp)` - join from `library_samples` to lanes.
- `(id_study_lims, id_run, position)` - `RunsForStudy` /
  `LanesForStudy` as single index seeks.

Rows whose flowcell does not resolve to an SQSCP study are skipped
by the sync source query and never inserted.

`seq_product_irods_locations_mirror` (new):

| Column                   | Consumers                                    |
| ------------------------ | -------------------------------------------- |
| id_iseq_product          | PK; JOIN target.                             |
| irods_root_collection    | `IRODSPathsForSample`, `IRODSPathsForStudy`. |
| irods_data_relative_path | `IRODSPathsForSample`, `IRODSPathsForStudy`. |
| irods_collection         | `IRODSPathsForSample`, `IRODSPathsForStudy`. |
| irods_file_name          | `IRODSPathsForSample`, `IRODSPathsForStudy`. |
| id_sample_tmp            | Denormalised; serves                         |
|                          | `IRODSPathsForSample` without joining        |
|                          | `iseq_product_metrics_mirror`.               |
| id_study_lims            | Denormalised; serves `IRODSPathsForStudy`.   |
|                          | NOT NULL. CHECK(<> '').                      |
| last_updated             | Watermark advancement.                       |

Collation set: `id_study_lims`.

Indexes:

- `(id_sample_tmp)` - `IRODSPathsForSample`.
- `(id_study_lims, id_sample_tmp)` - `IRODSPathsForStudy`.

`sync_state`:

| Column          | Notes                                             |
| --------------- | ------------------------------------------------- |
| table_name      | PK; one of `study`, `sample`, `iseq_flowcell`,    |
|                 | `iseq_product_metrics`,                           |
|                 | `seq_product_irods_locations`.                    |
| high_water      | TEXT (RFC3339Nano). Zero means cold.              |
| last_run        | TEXT (RFC3339Nano).                               |
| resume_cursor   | TEXT NULL. Tab-separated encoding of the          |
|                 | ordering tuple of the last row in the last        |
|                 | committed batch (RFC3339Nano for `last_updated`). |
|                 | NULL at end-of-stream.                            |
| indexes_dropped | INTEGER NOT NULL DEFAULT 0. Set in the same       |
|                 | transaction that drops indexes; cleared in the    |
|                 | same transaction that recreates them.             |

`schema_version`: unchanged single-row TEXT/INT version table; value
bumped to 2.

Dropped tables (no consumers remain after the read-path rework):
`negative_cache`, `enrich_cache`, `watermarks`. Their callers
(`isSampleNegativeCached`, the enrich cache writer, the
`upsertHierarchyReadThrough` watermark step) are removed.

### Sync engine

`Client.Sync(ctx)` fans out one goroutine per supported table. The
public command surface accepts no `--tables` flag. Each goroutine:

1. Reads its `sync_state` row (`high_water`, `resume_cursor`,
   `indexes_dropped`).
2. If `indexes_dropped == 1` and `high_water` is zero (i.e. a prior
   cold load resumed mid-stream), keeps indexes off; otherwise
   indexes stay on (incremental sync). For `sample_mirror` only, a
   fresh cold load (no `sync_state` row, or `high_water` zero and
   `indexes_dropped == 0`) drops the 8 secondary indexes in a
   transaction that also sets `indexes_dropped = 1`.
3. Opens a streaming MLWH connection (Go MySQL driver in unbuffered
   mode) and issues the source query with a strict `>` keyset
   predicate built from `resume_cursor`, or `last_updated >=
high_water` when no cursor.
4. Buffers up to `syncBatchSize = 1000` rows then commits one
   transaction containing the batched multi-row INSERT and the
   updated `resume_cursor` (the ordering tuple of the last row in
   the batch). The high-water inside the row is also tracked.
5. On `rows.Err()` / `invalid connection` / `unexpected EOF` / any
   other transient I/O error: reopen the upstream connection and
   reissue with the latest `resume_cursor`. Up to 5 retries per
   fault, exponential backoff starting at 1 s, doubling, capped at
   30 s per wait. One stderr line per retry:
   `mlwh sync: <table> reconnecting attempt N/5 after <err>: backoff <duration>`.
6. After the last batch: write `resume_cursor = NULL` and
   `high_water = max(last_updated)` in one transaction. For
   `sample_mirror` only, recreate the 8 secondary indexes in a
   transaction that also clears `indexes_dropped = 0`.
7. Print `<table> inserted=<n> updated=<n> high_water=<ts>` to
   stdout as soon as the table finishes, regardless of other
   tables' progress.

Source queries (no correlated subqueries, no `COALESCE(..., '')`):

```sql
-- sample (no id_study_lims; many-to-many lives in library_samples)
SELECT id_sample_tmp, id_lims, id_sample_lims, uuid_sample_lims,
       name, sanger_sample_id, supplier_name, accession_number,
       donor_id, taxon_id, common_name, description, last_updated
FROM sample
WHERE id_lims = 'SQSCP'
  AND ((last_updated > ?)                                 -- cursor ts
       OR (last_updated = ? AND id_sample_tmp > ?))       -- cursor tie
ORDER BY last_updated, id_sample_tmp;

-- study
SELECT <study cols>, last_updated
FROM study
WHERE id_lims = 'SQSCP'
  AND ((last_updated > ?)
       OR (last_updated = ? AND id_study_tmp > ?))
ORDER BY last_updated, id_study_tmp;

-- iseq_flowcell -> library_samples (one row per
-- (pipeline_id_lims, id_sample_tmp, id_study_lims) triple; drop
-- rows without an SQSCP study; no COALESCE)
SELECT iseq_flowcell.pipeline_id_lims,
       iseq_flowcell.id_sample_tmp,
       study.id_study_lims,
       iseq_flowcell.last_updated
FROM iseq_flowcell
INNER JOIN study ON study.id_study_tmp = iseq_flowcell.id_study_tmp
                AND study.id_lims = 'SQSCP'
WHERE ((iseq_flowcell.last_updated > ?)
       OR (iseq_flowcell.last_updated = ? AND
           (iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp,
            study.id_study_lims) > (?, ?, ?)))
ORDER BY iseq_flowcell.last_updated,
         iseq_flowcell.pipeline_id_lims,
         iseq_flowcell.id_sample_tmp,
         study.id_study_lims;

-- iseq_product_metrics (denormalised id_study_lims via flowcell)
SELECT ipm.id_iseq_product, ipm.id_iseq_flowcell_tmp, ipm.id_run,
       ipm.position, ipm.tag_index, ifc.id_sample_tmp,
       study.id_study_lims, ipm.qc, ipm.qc_lib, ipm.qc_seq,
       ipm.last_updated
FROM iseq_product_metrics ipm
INNER JOIN iseq_flowcell ifc
        ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp
INNER JOIN study
        ON study.id_study_tmp = ifc.id_study_tmp
       AND study.id_lims = 'SQSCP'
WHERE ((ipm.last_updated > ?)
       OR (ipm.last_updated = ? AND ipm.id_iseq_product > ?))
ORDER BY ipm.last_updated, ipm.id_iseq_product;

-- seq_product_irods_locations (denormalised id_study_lims via flowcell)
SELECT spi.id_iseq_product, spi.irods_root_collection,
       spi.irods_data_relative_path, spi.irods_collection,
       spi.irods_file_name, ifc.id_sample_tmp, study.id_study_lims,
       spi.last_updated
FROM seq_product_irods_locations spi
INNER JOIN iseq_product_metrics ipm
        ON ipm.id_iseq_product = spi.id_iseq_product
INNER JOIN iseq_flowcell ifc
        ON ifc.id_iseq_flowcell_tmp = ipm.id_iseq_flowcell_tmp
INNER JOIN study
        ON study.id_study_tmp = ifc.id_study_tmp
       AND study.id_lims = 'SQSCP'
WHERE ((spi.last_updated > ?)
       OR (spi.last_updated = ? AND spi.id_iseq_product > ?))
ORDER BY spi.last_updated, spi.id_iseq_product;
```

Upserts use the dialect-appropriate idempotent form:

```sql
-- SQLite
INSERT INTO sample_mirror (...) VALUES (?, ?, ?, ...), (?, ?, ?, ...)
ON CONFLICT(id_sample_tmp) DO UPDATE SET
  id_sample_lims = excluded.id_sample_lims, ...;

-- MySQL
INSERT INTO sample_mirror (...) VALUES (?, ?, ?, ...), (?, ?, ?, ...)
ON DUPLICATE KEY UPDATE
  id_sample_lims = VALUES(id_sample_lims), ...;
```

Per-batch insert/update counters are tracked by inspecting the
upsert affected-row counts (SQLite `RETURNING xmax` is not used; for
the batched upsert path, `inserted` counts rows whose unique key did
not exist before the batch, computed by a single
`SELECT COUNT(*) FROM <t> WHERE <unique_key> IN (...)` immediately
before the upsert inside the same transaction). The two counters do
not need to be exact under crash recovery; resume sets them based on
what is seen on the wire from the resume cursor onward.

`syncBatchSize` is a package-level `const int = 1000`; not exposed
as a flag or env var.

SQLite write-side connection pragmas applied to every sync
connection (one per concurrent table; resolver / read connections
keep their existing defaults) and restored before the command
returns: `synchronous=NORMAL`, `cache_size=-200000`,
`temp_store=MEMORY`. WAL mode remains on throughout.

Per-cache advisory lock:

- SQLite: an `IMMEDIATE` transaction on a dedicated single-row
  `sync_lock` table (`id INTEGER PRIMARY KEY CHECK(id = 1)`)
  acquired at command start and held for the lifetime of
  `wa mlwh sync`.
- MySQL: `GET_LOCK('wa_mlwh_sync_<cache_id>', 0)` where
  `<cache_id>` derives from the cache DSN's host/database (so two
  caches on the same MySQL server don't lock each other out).

If the lock cannot be acquired, the command exits non-zero with
`mlwh sync: another sync is already running against this cache` and
prints no per-table summary lines. The lock releases on normal
exit, on error, and on signal.

### Read-path correctness

- Every `*SourceSQL` in `mlwh/hierarchy.go` and the live-MLWH
  fallback in `mlwh/all_studies.go` is removed. `mlwh.Client` no
  longer holds a `syncSource Querier` for read paths; the `Querier`
  surface is used only by `sync.go`.
- `upsertHierarchyReadThrough` is removed. Hierarchy reads never
  write to any cache table.
- `StudyForSample(ctx, sangerName) (*Study, error)` is replaced by:

    ```go
    func (c *Client) StudiesForSample(
        ctx context.Context, sangerName string,
    ) ([]Study, error)
    ```

    The new method joins `library_samples` -> `study_mirror` on
    `id_study_lims` (with `id_lims='SQSCP'`) for the given
    `sample_mirror.name`, ordering by `id_study_lims`. Returns
    `ErrNotFound` if no rows exist for any reason other than an empty
    cache (cf. `ErrCacheNeverSynced`).

- `Sample.LibraryType` and `Sample.IDStudyLims` are removed. Callers
  that need libraries or studies for a sample call
  `LibrariesForSample` / `StudiesForSample` or read the slice fields
  populated by hierarchy methods.

- `FindSamplesBy<Column>` is one column per method, each runs
  `LIMIT 2`. Each returns one of: a 1-element slice (one match),
  `ErrAmbiguous` (>=2 matches with both candidate primary keys in the
  error string), or `ErrNotFound`.

    ```go
    FindSamplesBySangerID(ctx, raw)        // WHERE sanger_sample_id = ?
    FindSamplesByIDSampleLims(ctx, raw)    // WHERE id_sample_lims = ?
                                            //   AND id_lims = 'SQSCP'
    FindSamplesByAccessionNumber(ctx, raw) // WHERE accession_number = ?
    FindSamplesBySupplierName(ctx, raw)    // WHERE supplier_name = ?
    FindSamplesByLibraryType(ctx, raw)
        // FROM library_samples
        // INNER JOIN sample_mirror ON sample_mirror.id_sample_tmp =
        //                              library_samples.id_sample_tmp
        // WHERE library_samples.pipeline_id_lims = ?
        // ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp
        // LIMIT 2
    ```

- `ResolveSample` donor_id step uses `LIMIT 2` and returns
  `ErrAmbiguous` if two distinct samples share the donor id.

- `resolveStudyFromCacheWithWarmup` is renamed to
  `resolveStudyFromCache` (no warmup) and the
  `accession_number` step uses `LIMIT 2` + `ErrAmbiguous`. UUID and
  `id_study_lims` steps remain single-result (MLWH guarantees
  uniqueness by construction).

- `ClassifyIdentifier` integer-raw branch drops the spurious
  `uuid_sample_lims = ?` lookup. Only the UUID-shaped branch queries
  that column.

- Pagination ordering is fully deterministic. Every
  `ORDER BY <sample>.name LIMIT ? OFFSET ?` gains
  `, sample_mirror.id_sample_tmp` as a tie-breaker.

- `ResolveLibrary` reads the cache only. The
  `ResolveLibrary` -> `Sync(iseq_flowcell)` retry loop and the
  `ensureResolverTableSynced` / `hasResolverSyncState` helpers are
  removed.

- `Client.AllStudies` reads only `study_mirror`. The live-MLWH
  fallback and `upsertAllStudiesReadThrough` are removed.

### Empty-cache semantics

`OpenCache` does not pre-create `sync_state` rows. Read entry points
classify the cache state per-call:

- **Never-synced**: every table needed by the call has no
  `sync_state` row. The entry point returns
  `fmt.Errorf("%w: %w", ErrNotFound, ErrCacheNeverSynced)`. List
  endpoints return an empty slice and the same wrapped error.
- **Partially-synced**: at least one needed table has a non-zero
  `high_water`. Reads proceed against populated tables; reads that
  depend on an absent table behave as `ErrNotFound` for that lookup.
- **Fully-synced**: every needed `sync_state` row has non-zero
  `high_water`. Absences are authentic `ErrNotFound`.

The `indexes_dropped` flag is orthogonal: when `OpenCache` reads a
`sync_state` row whose `indexes_dropped == 1` AND whose `high_water`
is non-zero (i.e. a sync died after the final batch but before
recreating the indexes), `OpenCache` recreates the expected
secondary indexes for that table in its own transaction and clears
the flag. Standalone cold-load resumes (`indexes_dropped == 1` with
zero `high_water`) leave the flag set; the next `wa mlwh sync`
resumes from the cursor with indexes still off and recreates them
after its own final batch.

### Sample fan-out across studies / libraries

The audit-driven principle: callers that produce display /
filterable rows fan out per-pairing; callers that walk per-sample
state carry slices.

| Call site                                | Shape                               |
| ---------------------------------------- | ----------------------------------- |
| `cmd/mlwh_info.go` sample report         | per-pairing rows: one               |
|                                          | `(pipeline_id_lims, id_study_lims)` |
|                                          | line per `library_samples` row.     |
| `seqmeta/enrich.go` sample detail        | per-sample with                     |
|                                          | `Studies []Study`,                  |
|                                          | `Libraries []Library` populated     |
|                                          | from `library_samples`.             |
| `seqmeta/enrich.go` study detail         | per-sample with                     |
|                                          | `Libraries` slice; library          |
|                                          | groupings come from                 |
|                                          | `library_samples`, not              |
|                                          | `Sample.LibraryType`.               |
| `seqmeta/enrich.go`                      | iterates                            |
| `distinctLibrariesForSamples`            | `sample.Libraries` slice.           |
| `seqmeta/enrich.go libraryLinkForSample` | iterates `sample.Libraries` and     |
|                                          | returns one link per pairing.       |
| `seqmeta/diff.go`                        | iterates                            |
|                                          | `sample.Studies` /                  |
|                                          | `sample.Libraries` slices,          |
|                                          | hashes one entry per pairing.       |
| `seqmeta/validate.go`                    | iterates slices on the resolved     |
|                                          | sample object when comparing        |
|                                          | studies / libraries.                |
| `results/mlwh_search_resolver.go`        | per-pairing rows: emits one         |
|                                          | tagged-id row per                   |
|                                          | `(sample, study)` pairing.          |
| `results/server.go` search expansion     | per-pairing rows: filters and       |
|                                          | display rows behave as if MLWH      |
|                                          | had distinct rows.                  |

The hierarchy methods that walk multiple studies / libraries for a
sample share a single helper that loads
`library_samples` rows by `id_sample_tmp` and returns them sorted
deterministically (`id_study_lims`, `pipeline_id_lims`).

### Error handling

- `ErrPasswordInDSN`, `ErrUpstreamImpaired`, `ErrNotFound`,
  `ErrAmbiguous`, `ErrUnsupportedIdentifier`: unchanged.
- `ErrCacheNeverSynced`: new, wraps nothing on its own but is
  always paired with `ErrNotFound` via `fmt.Errorf("%w: %w", ...)`
  so callers can `errors.Is(err, mlwh.ErrCacheNeverSynced)`.
- Source rows that fail a cache CHECK constraint (e.g. empty
  `id_study_lims`) are a programming error: the sync goroutine
  fails immediately with an error naming the offending row's PK,
  the overall command exits non-zero.

### Performance budget

A `MLWH_SYNC_PERF_TEST=1`-gated integration test asserts that each
of the five tables completes its cold sync in no more than **2x**
the wall-clock time of streaming the equivalent source SQL through
the Go MySQL driver in unbuffered mode, measured on the same host
in the same test run against a fresh empty cache. There is no
aggregate cap. The test does not shell out to `mysql`; it times
the same SQL through `*sql.DB`. When the gate is unset the test
skips with `t.Skip("MLWH_SYNC_PERF_TEST not set")`.

---

## A. Cache schema

### A1: Schema version bump and OpenCache migration

As an operator, when I upgrade `wa` and run any `mlwh` command
against an existing v1 cache, I want `OpenCache` to migrate the
cache transparently, so that the next sync uses the new schema.

**Package:** `mlwh/`
**File:** `mlwh/cache.go`, `mlwh/cache_schema.go`
**Test file:** `mlwh/cache_test.go`, `mlwh/cache_schema_test.go`

`OpenCache` reads the `schema_version` row, compares it to the
`CacheSchemaVersion` constant (now 2), and on mismatch:

- Drops every cache table affected by this version bump:
  `sample_mirror`, `study_mirror`, `library_samples`,
  `donor_samples`, plus `negative_cache`, `enrich_cache`,
  `watermarks` (removed) and the new
  `iseq_product_metrics_mirror`, `seq_product_irods_locations_mirror`
  if they exist from a partial earlier rebuild.
- Recreates them from the embedded DDL.
- Deletes the matching `sync_state` rows for the recreated tables.
- Writes the new `schema_version` value.
- Prints `mlwh cache: schema vX->vY, recreated tables: [t1, t2, ...]`
  to stderr (one line, tables alphabetically).

**Acceptance tests:**

1. Given an empty SQLite cache opened with `CacheSchemaVersion = 2`,
   when `OpenCache` returns, then `schema_version` contains the row
   `(version = 2)` and `sync_state` is empty.
2. Given a SQLite cache pre-populated with the v1 schema and one
   `sync_state` row for `sample`, when `OpenCache` runs with v2,
   then the captured stderr contains exactly one line
   `mlwh cache: schema v1->v2, recreated tables:
[donor_samples, iseq_product_metrics_mirror, library_samples,
sample_mirror, seq_product_irods_locations_mirror, study_mirror]`,
   `sync_state` is empty, and querying `SELECT COUNT(*) FROM
sample_mirror` returns 0 without error.
3. Given a SQLite cache already at v2, when `OpenCache` runs again,
   then no stderr line is emitted.
4. Given the same migration scenario as test 2 but with a MySQL
   cache backend, when `OpenCache` runs, then the same stderr line
   format is emitted and the migration applies in MySQL.

### A2: Per-dialect schema parity

As a developer, I want the SQLite and MySQL embedded DDL to declare
identical tables, columns, primary keys, unique constraints, and
index columns, so that one backend is never silently behind.

**Package:** `mlwh/`
**File:** `mlwh/cache_schema_test.go`,
`mlwh/cache_schema/{sqlite,mysql}/*.sql`

The parity test loads both dialects, parses them, and asserts the
shapes match.

**Acceptance tests:**

1. Given the embedded SQLite and MySQL schemas at v2, when
   `parseSchemaShape` runs on each, then the resulting shapes have
   the same set of table names: `{sample_mirror, study_mirror,
library_samples, donor_samples,
iseq_product_metrics_mirror, seq_product_irods_locations_mirror,
sync_state, schema_version, sync_lock}`.
2. Given the same setup, when comparing each table's column set,
   then the column names match (collation and type-family
   normalisations from `normaliseTypeFamily` already permitted).
3. Given the same setup, when comparing index sets per table, then
   the column-list of each index matches across dialects (order of
   columns inside an index is significant; index name is not).
4. Given the same setup, when comparing unique constraints, then
   the unique-key column tuples per table are identical across
   dialects: `library_samples (pipeline_id_lims, id_sample_tmp,
id_study_lims)`, `donor_samples (donor_id, id_sample_tmp)`.

### A3: Case-insensitive text columns

As a user, when I look up
`SELECT ... WHERE name = 'HCA-LCa6-1'` in either backend, I want a
case-insensitive match using the column's collation, not a
functional index, so that the lookup is fast and consistent across
backends.

**Package:** `mlwh/`
**File:** `mlwh/cache_schema/{sqlite,mysql}/*.sql`

**Acceptance tests:**

1. Given a populated `sample_mirror` row with `name = 'HCA-LCA6-1'`
   in SQLite, when querying
   `SELECT id_sample_tmp FROM sample_mirror WHERE name = 'hca-lca6-1'`,
   then the row is returned.
2. Given the same row in MySQL, when running the same query, then
   the row is returned.
3. Given a populated `study_mirror` with
   `accession_number = 'EGAS00001006568'`, when querying with
   `'egas00001006568'`, then it matches in both backends.
4. Given a `library_samples` row with
   `pipeline_id_lims = 'Standard'`, when querying with
   `'STANDARD'`, then the row is returned in both backends (powers
   `FindSamplesByLibraryType` case-insensitivity).

## B. Sync engine

### B1: Five-table parallel sync

As an operator, when I run `wa mlwh sync` against a freshly-opened
v2 cache and a populated MLWH replica, I want all five tables to
sync concurrently, so that total wall-clock is the max of the
per-table wall-clocks rather than their sum.

**Package:** `mlwh/`, `cmd/`
**File:** `mlwh/sync.go`, `cmd/mlwh.go`
**Test file:** `mlwh/sync_test.go`, `cmd/mlwh_test.go`

```go
func (c *Client) Sync(ctx context.Context) ([]SyncReport, error)
```

`Sync` always starts exactly five goroutines (one per supported
table) regardless of cache state. Each goroutine returns its
`SyncReport`. The aggregate function returns the slice ordered by
table-finish time. Errors from individual tables are accumulated
via `errors.Join` and the overall command exits non-zero. There is
no `--tables` flag.

**Acceptance tests:**

1. Given a stub MLWH `Querier` that returns deterministic rows for
   `sample`, `study`, `iseq_flowcell`, `iseq_product_metrics`,
   `seq_product_irods_locations`, when `Client.Sync` runs against
   an empty SQLite cache, then the returned slice contains exactly
   five `SyncReport`s with `Table` set to each table name and
   `Inserted` equal to the row count emitted by the stub for that
   table.
2. Given the same setup, when one of the five table stubs returns
   an error, then `Sync` returns a joined error mentioning that
   table only, the other four tables still committed their rows,
   and the overall slice contains four successful reports plus the
   failing table's name in the error.
3. Given an instrumented stub that records the goroutine ID and
   start timestamp for each table's first row read, when `Sync`
   runs, then the five start timestamps overlap (each table started
   within `100ms` of the first table to start) and the goroutine
   IDs are distinct.
4. Given the `cmd/mlwh.go` `sync` subcommand, when invoked with no
   flags against the stub from test 1, then `cmd.Execute` succeeds
   and stdout contains exactly five lines, one per table, each
   matching the regex
   `^(sample|study|iseq_flowcell|iseq_product_metrics|`
   `seq_product_irods_locations) inserted=\d+ updated=\d+ `
   `high_water=\d{4}-.+Z$`.
5. Given the `cmd/mlwh.go` `sync` subcommand invoked with
   `--tables sample`, when parsing flags, then the command exits
   non-zero with `unknown flag: --tables` (flag removed).
6. Given a stub MLWH source whose row iterator emits one row, then
   blocks on a channel for 200ms before emitting the next row (per
   table), AND an instrumented write-side transaction recorder, when
   `Client.Sync` runs against an empty SQLite cache with
   `syncBatchSize = 2` for the test, then the recorder observes the
   FIRST committed batch transaction on each table BEFORE the
   upstream stub has emitted its final row (proving the MySQL
   driver-equivalent source iterator is consumed in unbuffered /
   streaming mode and not fully drained into memory before any
   write happens). The same property is asserted against the real
   `*sql.DB`-backed source by inspecting the DSN options passed to
   `sql.Open("mysql", dsn)` and asserting the DSN contains
   neither `interpolateParams=true` only nor any prefetch buffer
   setting that disables row streaming; specifically the captured
   DSN options include `multiStatements=false`, no
   `rowsAffected` buffering parameter, and the driver `Rows` value
   returned by the test source exposes a streaming `Next()` (the
   helper records that `Next()` returned `true` before the row
   producer finished emitting).
7. Given an instrumented in-process stub where `study` blocks for
   500ms on its FIRST row read while `iseq_flowcell` returns its
   rows immediately, when `cmd.Execute` runs `wa mlwh sync`, then
   the FIRST line written to stdout begins with `iseq_flowcell
inserted=`, the LAST stdout line begins with `study inserted=`,
   and the captured stdout line order is NOT lexical / NOT source
   order but matches the per-table finish order recorded by the
   stub (i.e. each table's stdout line is emitted as soon as that
   goroutine returns, not buffered until all five complete).
8. Given an in-process stub for `cmd/mlwh_test.go` where TWO
   tables are forced to fail simultaneously - `study` returns
   `fmt.Errorf("forced study failure")` on its first row read and
   `iseq_flowcell` returns `fmt.Errorf("forced iseq_flowcell
failure")` on its first row read, while the remaining three
   tables succeed - when `wa mlwh sync` is invoked via
   `cmd.Execute`, then the process exits non-zero (cobra returns a
   non-nil error), the captured stderr (or the joined error
   surfaced by cobra) contains the substring `study` AND the
   substring `iseq_flowcell` AND the substring
   `forced study failure` AND the substring
   `forced iseq_flowcell failure`, and stdout still contains the
   three success lines for `sample`, `iseq_product_metrics`, and
   `seq_product_irods_locations` (each matching the success-line
   regex from test 4).

### B2: Batched idempotent upserts

As a developer, I want batched 1000-row idempotent upserts so the
sync is fast and replayable.

**Package:** `mlwh/`
**File:** `mlwh/sync.go`
**Test file:** `mlwh/sync_test.go`

Package-level `const syncBatchSize = 1000`.

**Acceptance tests:**

1. Given a stub MLWH that emits 2500 sample rows, when sync runs to
   completion, then the cache contains 2500 rows in
   `sample_mirror`, the stub observed exactly 3 transactions on the
   write connection (batches of 1000, 1000, 500), and
   `SyncReport.Inserted == 2500`, `SyncReport.Updated == 0`.
2. Given the same input replayed against the populated cache,
   when sync runs, then `SyncReport.Inserted == 0` and
   `SyncReport.Updated == 2500` (every row is detected as an
   existing key).
3. Given a stub emitting 250 sample rows where the 100th row has
   `IDSampleLims = "X"` followed by the same `id_sample_tmp` with
   updated `IDSampleLims = "Y"`, when sync runs, then the cache
   row's `id_sample_lims` is `"Y"` (upsert is idempotent and
   last-write-wins inside a batch).
4. Given a `library_samples` source emitting two rows with the same
   `(pipeline_id_lims, id_sample_tmp, id_study_lims)` triple, when
   sync runs, then the cache contains exactly one row for the
   triple and a second sync run does not raise a unique-constraint
   error.

### B3: Resume cursor

As an operator, when a sync dies mid-stream, I want the next sync
to resume from the last committed batch's ordering tuple rather
than restarting from zero.

**Package:** `mlwh/`
**File:** `mlwh/sync.go`
**Test file:** `mlwh/sync_test.go`

Cursor format: tab-separated fields, RFC3339Nano for `last_updated`;
NULL at end-of-stream.

**Acceptance tests:**

1. Given a stub that emits 1500 sample rows and then returns
   `io.EOF` (clean end), when sync completes, then
   `sync_state.resume_cursor` for `sample` is `NULL` and
   `high_water` equals the maximum `last_updated` in the input.
2. Given a stub that emits 1500 sample rows followed by a
   `driver.ErrBadConn` (simulating an aborted stream) and the
   retry policy is bypassed (max attempts 1) for the test, when
   sync fails, then `sync_state.resume_cursor` for `sample` is
   non-NULL and equals
   `<rfc3339nano of row 1000.last_updated>\t1000` (the last row of
   the last committed batch is row 1000, with `id_sample_tmp =
1000`).
3. Given the cursor in test 2 and a stub that emits the remaining
   500 rows (rows 1001..1500) when queried with
   `last_updated > ? OR (last_updated = ? AND id_sample_tmp > ?)`,
   when sync runs again, then the cache ends with 1500 rows and
   `sync_state.resume_cursor` is `NULL` and
   `SyncReport.Inserted == 500`.
4. Given the `iseq_flowcell` ordering tuple
   `(last_updated, pipeline_id_lims, id_sample_tmp, id_study_lims)`,
   when sync emits 2000 rows and dies, then `resume_cursor` is
   `<ts>\t<pid>\t<sid>\t<lid>` (4 tab-separated fields) and the
   captured resume query SQL contains the explicit 4-column keyset
   predicate
   `(iseq_flowcell.last_updated > ?) OR (iseq_flowcell.last_updated
= ? AND (iseq_flowcell.pipeline_id_lims, iseq_flowcell.id_sample_tmp,
study.id_study_lims) > (?, ?, ?))`
   (or an equivalent row-comparison decomposition that ANDs the
   `last_updated = ?` tie with strict `>` on the remaining
   `(pipeline_id_lims, id_sample_tmp, id_study_lims)` triple), with
   the bound parameters in that order matching the four cursor
   fields.

### B4: Cold-load index drop / recreate for sample_mirror

As an operator, I want the 8 `sample_mirror` secondary indexes
dropped during cold load and recreated afterwards, so cold sync is
not bottlenecked on per-row index maintenance.

**Package:** `mlwh/`
**File:** `mlwh/sync.go`, `mlwh/cache_schema.go`
**Test file:** `mlwh/sync_test.go`

**Acceptance tests:**

1. Given an empty SQLite cache (cold `sample_mirror`), when sync
   reads the `sample_mirror` `sync_state` row and finds it absent,
   then the first action it takes against the cache is a
   transaction that drops the 8 secondary indexes and inserts a
   `sync_state` row with `indexes_dropped = 1` and `high_water` as
   the zero time. (Verify via captured DDL log.)
2. Given the same setup, when sync completes its last batch, then
   the final cache state has the 8 indexes recreated (queryable via
   `SELECT name FROM sqlite_master WHERE type='index' AND
tbl_name='sample_mirror'`) and `indexes_dropped = 0`.
3. Given a populated cache (incremental sync with non-zero
   `high_water`), when sync runs, then the indexes are NOT dropped
   at any point (captured DDL log contains no `DROP INDEX`
   statements).
4. Given a cold-load sync that dies after the final batch commit
   but before the index recreation transaction (simulated by
   killing the goroutine after the last
   `INSERT ... ON CONFLICT` commits), when `OpenCache` is called
   again on the same cache, then `OpenCache` recreates the 8
   indexes in one transaction, clears `indexes_dropped`, and
   prints no stderr line (this is a recovery, not a schema
   migration).
5. Given the other four tables (`study_mirror`,
   `library_samples`, `iseq_product_metrics_mirror`,
   `seq_product_irods_locations_mirror`), when cold sync runs,
   then `indexes_dropped` remains 0 throughout (their index counts
   do not justify the drop/recreate path).
6. Given a MySQL cache backend, a MySQL cache integration env var
   (the same gate the existing MySQL cache tests use:
   `WA_MLWH_CACHE_MYSQL_DSN` set; test skips cleanly otherwise),
   and an empty cold `sample_mirror` (no `sync_state` row), when
   sync begins, then a query against
   `INFORMATION_SCHEMA.STATISTICS WHERE TABLE_SCHEMA = DATABASE()
AND TABLE_NAME = 'sample_mirror' AND INDEX_NAME <> 'PRIMARY'`
   observed AFTER the cold-load `indexes_dropped = 1` transaction
   commits and BEFORE the final recreate transaction returns zero
   rows (all 8 secondary indexes are absent mid-load).
7. Given the same MySQL setup as test 6, when sync completes its
   final batch and recreates indexes, then
   `INFORMATION_SCHEMA.STATISTICS` lists exactly 8 distinct
   `INDEX_NAME` values (one per audited single-column index) on
   `sample_mirror`, covering the column set `{id_sample_lims,
uuid_sample_lims, name, sanger_sample_id, supplier_name,
accession_number, donor_id, last_updated}` (one index per
   column, by `COLUMN_NAME` introspection), and the
   `sync_state.indexes_dropped` value for `sample_mirror` is 0.

### B5: Upstream reconnect / retry

As an operator, when MLWH closes a streaming connection mid-sync, I
want the table's sync goroutine to reopen the connection and resume
from `resume_cursor`, up to 5 attempts per fault with exponential
backoff.

**Package:** `mlwh/`
**File:** `mlwh/sync.go`
**Test file:** `mlwh/sync_test.go`

Backoff: 1 s, 2 s, 4 s, 8 s, 16 s (capped at 30 s per wait). One
stderr line per retry:
`mlwh sync: <table> reconnecting attempt N/5 after <err>: backoff <duration>`.

**Acceptance tests:**

1. Given a stub that emits 1000 rows, returns `invalid connection`,
   then on reconnect emits the remaining 500 rows, when sync runs,
   then `SyncReport.Inserted == 1500`, captured stderr contains
   exactly one line matching
   `mlwh sync: sample reconnecting attempt 1/5 after .*invalid `
   `connection.*: backoff 1s`, and the test-scoped sleeper observed
   exactly one `1s` wait.
2. Given a stub that fails on every attempt with
   `unexpected EOF`, when sync runs, then the table's sync fails
   after the 5th attempt, stderr contains exactly 5 reconnect
   lines with `attempt 1/5 .. attempt 5/5` and backoffs `1s 2s 4s
8s 16s`, and the returned error names `sample`.
3. Given a stub that fails with a non-transient error
   (`fmt.Errorf("syntax error")`), when sync runs, then the table
   fails on the first attempt without retry and the captured
   stderr contains no reconnect line.
4. Given two tables both faulting once each, when sync runs, then
   both tables emit one reconnect line each and both eventually
   complete; the overall command exits zero.

### B6: Per-cache advisory lock

As an operator, when two `wa mlwh sync` processes target the same
cache, I want the second to refuse to run rather than corrupt the
cache.

**Package:** `mlwh/`
**File:** `mlwh/cache.go`, `mlwh/sync.go`

**Acceptance tests:**

1. Given a SQLite cache and two `wa mlwh sync` invocations started
   concurrently against it, when both attempt the
   `IMMEDIATE BEGIN` on `sync_lock`, then exactly one acquires the
   lock and the other exits non-zero with stderr `mlwh sync:
another sync is already running against this cache` and stdout
   empty.
2. Given a MySQL cache and the same concurrent setup, when both
   attempt `GET_LOCK('wa_mlwh_sync_<id>', 0)`, then exactly one
   acquires the lock and the other exits non-zero with the same
   message.
3. Given the lock is held and the holder's process is killed
   (SIGKILL), when a new `wa mlwh sync` is started, then the new
   process acquires the lock (for SQLite via the abandoned
   transaction being rolled back on connection close; for MySQL
   via `GET_LOCK` releasing on connection drop).
4. Given a `wa mlwh info` invocation (read-only), when run against
   a cache currently held by `wa mlwh sync`, then `wa mlwh info`
   succeeds and does not attempt the advisory lock.

### B7: Source filter tightening (no empty id_study_lims)

As a developer, I want `library_samples`,
`iseq_product_metrics_mirror`, and
`seq_product_irods_locations_mirror` to never contain rows whose
`id_study_lims` is empty, so the cache never serves
`WHERE id_study_lims = ''` and never trips the CHECK constraint.

**Package:** `mlwh/`
**File:** `mlwh/sync.go`,
`mlwh/cache_schema/{sqlite,mysql}/library_samples.sql`,
`.../iseq_product_metrics_mirror.sql`,
`.../seq_product_irods_locations_mirror.sql`

**Acceptance tests:**

1. Given a stub `iseq_flowcell` source emitting one row whose
   joined `study.id_lims` is `'GCLP'` (not SQSCP), when sync runs,
   then the row is filtered out at the source `INNER JOIN study
ON ... AND study.id_lims = 'SQSCP'` and `library_samples` has 0
   rows.
2. Given a stub `iseq_product_metrics` source emitting one row
   whose flowcell has no SQSCP study, when sync runs, then
   `iseq_product_metrics_mirror` has 0 rows.
3. Given a test that bypasses the source filter and tries to
   `INSERT INTO library_samples (...) VALUES ('Standard', 1, '')`,
   when committed, then the CHECK constraint rejects the insert
   in both SQLite and MySQL.
4. Given the same forced insert path used inside a sync batch,
   when sync commits the batch, then the sync goroutine fails
   immediately with an error naming the offending row's
   `(pipeline_id_lims, id_sample_tmp)` and the overall command
   exits non-zero.

### B8: SQLite write pragmas

As an operator, I want sync to apply tuned SQLite pragmas only on
the write connection it uses and to restore the previous values on
exit, so resolver / read connections are unaffected.

**Package:** `mlwh/`
**File:** `mlwh/cache.go`, `mlwh/sync.go`

**Acceptance tests:**

1. Given a SQLite cache, when sync starts, then the captured PRAGMA
   sequence on the sync connection contains `PRAGMA
synchronous=NORMAL`, `PRAGMA cache_size=-200000`, `PRAGMA
temp_store=MEMORY` in that order.
2. Given a SQLite cache, when sync finishes (success or error),
   then the captured PRAGMA sequence ends with the pre-recorded
   values being restored.
3. Given a MySQL cache, when sync starts, then no SQLite pragmas
   are issued (the pragma helper is a no-op for MySQL).

## C. Read-path correctness

### C1: StudiesForSample replaces StudyForSample

As a `cmd/mlwh info` user, when I inspect a sample that has been
prepped against two studies, I want to see both studies, not one
arbitrarily-picked study.

**Package:** `mlwh/`
**File:** `mlwh/hierarchy.go`
**Test file:** `mlwh/hierarchy_test.go`

```go
func (c *Client) StudiesForSample(
    ctx context.Context, sangerName string,
) ([]Study, error)
```

Query joins `library_samples` to `study_mirror` via `id_study_lims`,
filtered by `sample_mirror.name = ?` and `study_mirror.id_lims =
'SQSCP'`, ordered by `study_mirror.id_study_lims`. `StudyForSample`
is removed (no callers).

**Acceptance tests:**

1. Given a cache with sample `S1` (`id_sample_tmp = 1`,
   `name = 'S1'`) and `library_samples` rows
   `('Standard', 1, '6568')`, `('Chromium', 1, '6569')`, plus
   matching `study_mirror` rows, when `StudiesForSample(ctx,
"S1")` runs, then it returns exactly two studies ordered by
   `id_study_lims`: `[Study{IDStudyLims:"6568"},
Study{IDStudyLims:"6569"}]`.
2. Given the same cache with sample `S2` that has no
   `library_samples` rows, when `StudiesForSample(ctx, "S2")`
   runs, then it returns `(nil, ErrNotFound)`.
3. Given a never-synced cache, when `StudiesForSample(ctx, "S1")`
   runs, then it returns
   `(nil, err)` where `errors.Is(err, ErrCacheNeverSynced)` and
   `errors.Is(err, ErrNotFound)` are both true.
4. Given a sample whose flowcell links to a non-SQSCP study, when
   `StudiesForSample` runs, then the non-SQSCP study is excluded.

### C2: Per-pairing sample fan-out

As a seqmeta consumer, when a sample has multiple
`(library, study)` pairings, I want one record per pairing rather
than a single arbitrarily-picked pairing.

**Package:** `seqmeta/`, `results/`, `cmd/`
**File:** `seqmeta/enrich.go`, `seqmeta/diff.go`,
`seqmeta/validate.go`, `results/server.go`,
`results/mlwh_search_resolver.go`, `cmd/mlwh_info.go`
**Test files:** the matching `*_test.go` files in each package.

Per the audit table in the architecture section. The
`Sample.Studies []Study` and `Sample.Libraries []Library` fields
are populated by hierarchy methods via a single helper
`loadSampleFanOut(ctx, c, []int64) (map[int64][]Library,
map[int64][]Study, error)` that walks `library_samples`.

**Acceptance tests:**

1. Given a sample `S1` with `library_samples` rows
   `('Standard', 1, '6568')` and `('Chromium', 1, '6569')`, when
   `cmd/mlwh info S1` runs in text mode, then the output contains
   exactly two `library:` lines, one
   `library: Standard / 6568` and one `library: Chromium /
6569` in `id_study_lims` order.
2. Given the same cache, when seqmeta's
   `buildSampleDetailFromProvider` runs for `S1`, then the
   returned `*SampleDetail.Libraries` has length 2 with
   `[{PipelineIDLims:"Standard", IDStudyLims:"6568"},
{PipelineIDLims:"Chromium", IDStudyLims:"6569"}]`.
3. Given a `wa results` search filter `--sample S1`, when
   `results.SeqmetaSampleResolver.Expand(KindSangerSampleName,
"S1")` runs, then the returned `(samples, runs, lanes)` triple
   reflects both studies' lanes (no dedup that hides the per-study
   provenance).
4. Given `seqmeta/diff.go` `Diff` for a study `6568`, when one of
   its samples is also paired with study `6569`, then the diff for
   `6568` hashes the `('Standard', 1, '6568')` pairing only (not
   the `6569` pairing), and the diff for `6569` independently
   hashes the `('Chromium', 1, '6569')` pairing.

### C3: FindSamplesBy<Column> one-column-per-method

As a seqmeta `Validate` caller, when I look up a sample by
accession number, I want only the accession column queried and to
get `ErrAmbiguous` if two samples share that accession.

**Package:** `seqmeta/`
**File:** `seqmeta/client_adapter.go`, `seqmeta/provider.go`
**Test file:** `seqmeta/client_adapter_test.go`

Each method runs one indexed query against `sample_mirror`, with
`LIMIT 2` and `id_lims = 'SQSCP'` where appropriate. Behaviour:

- 1 match: returns a 1-element slice.
- 2 matches: returns `nil, fmt.Errorf("%w: %q ambiguous between
%s and %s", ErrAmbiguous, raw, pk1, pk2)`.
- 0 matches: returns `nil, ErrNotFound`.

**Acceptance tests:**

1. Given a cache with two samples sharing
   `accession_number = "DUP"` (different `id_sample_tmp`s), when
   `FindSamplesByAccessionNumber(ctx, "DUP")` runs, then it
   returns `(nil, err)` where `errors.Is(err, ErrAmbiguous)` and
   `err.Error()` contains the two `id_sample_tmp` values.
2. Given the same cache, when
   `FindSamplesBySangerID(ctx, "DUP")` runs, then it returns
   `nil, ErrNotFound` (it queries only `sanger_sample_id`, not
   `accession_number`).
3. Given a cache with one sample `S1` and
   `library_samples('Standard', 1, '6568')`, when
   `FindSamplesByLibraryType(ctx, "Standard")` runs, then it
   returns a 1-element slice containing `S1` (no `Sample.LibraryType`
   field is populated; the slice is the canonical sample).
4. Given a never-synced cache, when any
   `FindSamplesBy<Column>` runs, then it returns
   `(nil, err)` where `errors.Is(err, ErrCacheNeverSynced)`.

### C4: Resolver ambiguity rules

As a resolver caller, when an input matches multiple records on
columns where MLWH does not guarantee uniqueness, I want
`ErrAmbiguous`, not the lowest-PK pick.

**Package:** `mlwh/`
**File:** `mlwh/resolver.go`, `mlwh/resolver_sample.go`

**Acceptance tests:**

1. Given two samples sharing `donor_id = "DON1"`, when
   `ResolveSample(ctx, "DON1")` runs, then it returns
   `(Match{}, err)` where `errors.Is(err, ErrAmbiguous)`.
2. Given two studies sharing `accession_number = "EGAS..."`, when
   `ResolveStudy(ctx, "EGAS...")` runs, then it returns
   `(Match{}, err)` where `errors.Is(err, ErrAmbiguous)`.
3. Given two studies sharing `name = "HCA"`, when
   `ResolveStudy(ctx, "HCA")` runs, then it returns
   `errors.Is(err, ErrAmbiguous)` (the name step keeps its existing
   LIMIT 2).
4. Given one study with `uuid_study_lims = "u1"`, when
   `ResolveStudy(ctx, "u1")` runs, then it returns the single
   study (UUID lookup is single-result by construction).
5. Given an integer-shaped raw `"42"` and a sample with
   `id_sample_lims = "42"`, when `ClassifyIdentifier(ctx, "42")`
   runs, then it does NOT query `uuid_sample_lims = "42"` (the
   captured cache SQL log contains no such statement).
6. Given a cache with sample A where `sample_mirror.name = "X"`
   and `id_sample_tmp = 10`, and a DIFFERENT sample B where
   `sample_mirror.sanger_sample_id = "X"` and
   `id_sample_tmp = 20` (no other column on either row equals
   `"X"`), when `ResolveSample(ctx, "X")` runs (driving the
   cross-column cascading lookup that probes name and
   sanger_sample_id in turn), then it returns
   `(Match{}, err)` where `errors.Is(err, ErrAmbiguous)` is true
   and `err.Error()` contains BOTH `id_sample_tmp` values
   (substrings `"10"` and `"20"`), and the error mentions the raw
   `"X"` so the operator can identify which input triggered the
   collision. The lowest-PK pick (sample A on name) MUST NOT be
   returned silently.

### C5: Deterministic pagination

As a paginated reader, when I page through samples for a study, I
want a stable ordering so the same row never appears on two pages.

**Package:** `mlwh/`
**File:** `mlwh/hierarchy.go`

Every pagination SQL adds `, sample_mirror.id_sample_tmp` after the
existing `ORDER BY <sample>.name`.

**Acceptance tests:**

1. Given two samples sharing `name = "NAME"` (different
   `id_sample_tmp`s) in study `6568`, when
   `SamplesForStudy(ctx, "6568", 1, 0)` and
   `SamplesForStudy(ctx, "6568", 1, 1)` are both called, then they
   return distinct samples and the union covers both rows.
2. Given the captured query SQL for `SamplesForStudy`,
   `SamplesForLibrary`, `SamplesForLibraryType`, when each is
   inspected, then each contains
   `ORDER BY sample_mirror.name, sample_mirror.id_sample_tmp
LIMIT ? OFFSET ?`.

### C6: No live-MLWH read fallback

As a developer, I want every read path to read only from the cache,
so cold-cache states cannot mask sync failures and we cannot
accidentally hammer the upstream replica with read-time queries.

**Package:** `mlwh/`
**File:** `mlwh/hierarchy.go`, `mlwh/all_studies.go`,
`mlwh/resolver.go`

**Acceptance tests:**

1. Given a `Client` constructed with a non-nil `cache` and a nil
   `syncSource` (read-only deployment), when each of
   `ResolveSample`, `ResolveStudy`, `ResolveLibrary`, `ResolveRun`,
   `StudiesForSample`, `SamplesForStudy`, `SamplesForLibrary`,
   `SamplesForLibraryType`, `SamplesForRun`, `LibrariesForStudy`,
   `RunsForStudy`, `LanesForSample`, `IRODSPathsForSample`,
   `IRODSPathsForStudy`, `AllStudies` is called, then each
   succeeds or returns `ErrNotFound`/`ErrCacheNeverSynced` without
   attempting to dial the MLWH source.
2. Given a `Client` configured with a `syncSource` that returns
   `t.Fatal` from `QueryContext`, when any of the read paths in
   test 1 runs, then no test failure is triggered (read paths
   never touch the sync source).
3. Given a `Client.AllStudies` call against an empty `study_mirror`
   on a partially-synced cache (no `sync_state` row for `study`),
   when called, then it returns
   `([]Study{}, fmt.Errorf("%w: %w", ErrNotFound,
ErrCacheNeverSynced))`.

### C7: No mid-read cache writes

As a concurrent reader, I want hierarchy reads to never write to
the cache, so reads do not block on the writer lock and can never
leave the cache in a partially-populated state.

**Package:** `mlwh/`
**File:** `mlwh/hierarchy.go`

**Acceptance tests:**

1. Given a populated cache and the SQLite WAL set up with a
   read-only connection, when 10 concurrent hierarchy reads of
   `SamplesForStudy`, `LibrariesForStudy`, `RunsForStudy`,
   `LanesForSample`, `IRODSPathsForSample` run, then each
   succeeds and no `BEGIN ... COMMIT` is captured against the
   write connection.
2. Given the previous `upsertHierarchyReadThrough` symbol, when
   the package is built, then the symbol does not exist (compile
   guard via grep in the test or via referencing the symbol in a
   `_ = ...` line that must fail to compile in a build-tagged
   test file).

## D. Empty-cache semantics

### D1: Never-synced cache returns ErrCacheNeverSynced

As a CLI user installing `wa` for the first time, when I run any
`wa mlwh info` query before running `wa mlwh sync`, I want an
actionable error message telling me to run sync.

**Package:** `mlwh/`, `cmd/`
**File:** `mlwh/{resolver,hierarchy,all_studies}.go`,
`cmd/mlwh_info.go`, `seqmeta/server.go`
**Test files:** corresponding `*_test.go`.

**Acceptance tests:**

1. Given a freshly-opened cache with no `sync_state` rows, when
   `cmd/mlwh info` runs against any identifier, then the command
   exits non-zero and stderr contains
   `mlwh: cache has never been synced; run "wa mlwh sync" first`.
2. Given the same cache, when `ResolveSample(ctx, "S1")` is
   called, then it returns `(Match{}, err)` with
   `errors.Is(err, ErrCacheNeverSynced)` and
   `errors.Is(err, ErrNotFound)` both true.
3. Given the seqmeta HTTP `/validate/{id}` endpoint, when the
   cache is never-synced, then the response status is `404` and
   the body's `error` field contains the actionable hint.
4. Given a partially-synced cache where only `study` has been
   synced, when `ResolveStudy(ctx, "6568")` returns a `Study` and
   `StudiesForSample(ctx, "S1")` is then called, then
   `StudiesForSample` returns
   `(nil, fmt.Errorf("%w: %w", ErrNotFound, ErrCacheNeverSynced))`
   (because `library_samples`'s `sync_state` row is absent).
5. Given a fully-synced cache, when `ResolveSample(ctx, "missing")`
   runs against a value not in the cache, then it returns
   `ErrNotFound` WITHOUT `ErrCacheNeverSynced` wrapping (cache is
   authoritative).

### D2: No automatic re-sync from reads

As a developer, I want `ResolveLibrary` and friends to never call
`Sync`, so a stream of frontend misses cannot starve the real sync
loop.

**Package:** `mlwh/`
**File:** `mlwh/resolver.go`

**Acceptance tests:**

1. Given a never-synced cache and a `Client` whose `Sync` method
   has been wrapped to record invocations, when `ResolveLibrary`,
   `ResolveSample`, `ResolveStudy`, `ResolveRun`,
   `ClassifyIdentifier` are each called once, then the wrapper
   records zero `Sync` invocations.
2. Given the previous helpers `ensureResolverTableSynced` and
   `hasResolverSyncState`, when the package is built, then the
   symbols do not exist (grep guard in the package test).

## E. Performance

### E1: 2x per-table cold-sync budget

As an operator, when I run `wa mlwh sync` against the live MLWH
replica with `MLWH_SYNC_PERF_TEST=1`, I want each table to finish
within 2x the wall-clock time of streaming the equivalent source
SQL through the Go MySQL driver.

**Package:** `mlwh/`
**File:** `mlwh/integration_test.go`
**Test file:** same.

**Acceptance tests:**

1. Given `MLWH_SYNC_PERF_TEST=1`, `WA_MLWH_DSN`, and
   `WA_MLWH_PASSWORD` all set, and a fresh empty SQLite cache file
   under `t.TempDir()`, when the test:
   (a) times the unbuffered streaming of each of the five source
   queries via `*sql.DB.QueryContext` (counting rows, no
   writes); call this `streamDuration[table]`;
   (b) times `Client.Sync` end-to-end against the same cache; call
   the per-table report's wall-clock `syncDuration[table]`,
   then for every table:
   `syncDuration[table] <= 2 * streamDuration[table]`.
2. Given `MLWH_SYNC_PERF_TEST` is unset, when the test runs, then
   it skips cleanly with the message
   `MLWH_SYNC_PERF_TEST not set`.
3. Given `MLWH_SYNC_PERF_TEST=1` but no `WA_MLWH_DSN`, when the
   test runs, then it skips with `WA_MLWH_DSN not set`.

## Implementation Order

Phases are sequential by default; sub-bullets within a phase may
proceed in parallel since they touch independent files.

### Phase 1: Schema, types, and migration

- A1: schema version bump + `OpenCache` migration.
- A2: dialect parity test updates (drop removed tables, add new
  ones, update unique constraints).
- A3: collation tests.
- `mlwh/types.go` changes: drop `Sample.IDStudyLims`,
  `Sample.LibraryType`, `Sample.SangerID`; add `Sample.Studies`
  and `Sample.Libraries`; add `Library.IDStudyLims`. Add
  `ErrCacheNeverSynced`.

Acceptance: tests A1.{1..4}, A2.{1..4}, A3.{1..4} pass against the
new embedded DDL.

### Phase 2: Sync engine

- B1: parallel five-table sync with per-table goroutines.
- B2: batched idempotent upserts.
- B3: resume cursor.
- B4: cold-load index drop/recreate for `sample_mirror`, with
  `OpenCache` recovery.
- B5: reconnect / retry.
- B6: advisory lock.
- B7: source filter tightening.
- B8: SQLite write pragmas.

Acceptance: tests B1.{1..8}, B2.{1..4}, B3.{1..4}, B4.{1..7},
B5.{1..4}, B6.{1..4}, B7.{1..4}, B8.{1..3} pass against a mocked
MLWH source (B4.{6..7} skip when the MySQL cache integration env
var is unset).

### Phase 3: Read-path correctness

- C1: `StudiesForSample` (remove `StudyForSample`).
- C3: `FindSamplesBy<Column>` one-per-method with `LIMIT 2`.
- C4: ambiguity rules across the resolver +
  `ClassifyIdentifier` integer-branch fix.
- C5: deterministic pagination.
- C6: remove all `*SourceSQL` paths,
  `upsertHierarchyReadThrough`, `queryAllStudiesSource`,
  `upsertAllStudiesReadThrough`, `ensureResolverTableSynced`,
  `hasResolverSyncState`, `negative_cache` reads/writes,
  `enrich_cache`.
- C7: prove no mid-read writes.

Acceptance: tests C1.{1..4}, C3.{1..4}, C4.{1..6}, C5.{1..2},
C6.{1..3}, C7.{1..2} pass.

### Phase 4: Sample fan-out across studies / libraries

- C2: update every consumer per the audit table (cmd/mlwh_info,
  seqmeta/enrich, seqmeta/diff, seqmeta/validate, results/server,
  results/mlwh_search_resolver). Add `loadSampleFanOut` helper.

Acceptance: tests C2.{1..4} plus every existing seqmeta /
frontend / results test that was previously asserting the
single-`IDStudyLims` / `LibraryType` shape is updated to assert
slice contents.

### Phase 5: Empty-cache semantics

- D1: `ErrCacheNeverSynced` wired through resolver, hierarchy,
  `AllStudies`, `cmd/mlwh info`, `seqmeta/server`.
- D2: confirm read paths never call `Sync`.

Acceptance: tests D1.{1..5}, D2.{1..2} pass.

### Phase 6: Performance

- E1: integration test gated by `MLWH_SYNC_PERF_TEST=1`.

Acceptance: test E1.{1..3} passes when gates are set; skips
otherwise.

## Appendix: Key Decisions

### Cache is authoritative

The cache is the sole source of truth. No read path falls back to
the live MLWH. This is the single largest correctness gain in this
spec: every "silently wrong because we hit the live database for
one column" path is impossible by construction. Operators run
`wa mlwh sync` on a schedule; an empty cache returns
`ErrCacheNeverSynced` with an actionable hint.

### Many-to-many lives in library_samples

`Sample` no longer carries `IDStudyLims` or `LibraryType`. Every
caller that needs them either:

- fans out to per-pairing rows (display / filter / search), or
- carries `Studies []Study`, `Libraries []Library` populated from
  `library_samples` (per-sample state walkers).

This is the second largest correctness gain. The cherry-picked
`LIMIT 1` subquery in `sampleSyncSourceQuery`, the per-row
`LIMIT/OFFSET` library_type fill in `samplesForStudySourceSQL`,
the cross-table `library_type` reads in the resolver - all of them
are gone.

### Resume cursor over high-water alone

The current code only advances `high_water` when the entire table
commits. With 10M-row cold loads on flaky upstream connections,
that means every restart begins at zero. The
`(resume_cursor, indexes_dropped)` pair plus batch-scoped
transactions let cold loads resume cheaply and lets the cold-load
index drop survive a kill -9.

### Per-table 2x perf gate, not aggregate

The five tables stream concurrently. SQLite's writer lock
partially serialises their commits, but the wall-clock measure
that matters in practice is the longest single table. A 2x
per-table gate measures the right thing (sync vs. raw stream
time) and stays meaningful when one or two tables are tiny.

### Testing strategy

- Unit tests use a `sqlmock`-style stub for the MLWH source and a
  real `modernc.org/sqlite` cache under `t.TempDir()`.
- Schema parity is asserted via the existing
  `mlwh/cache_schema_test.go` shape parser, extended for the two
  new mirror tables.
- The `MLWH_SYNC_PERF_TEST=1` gate is the only test that touches
  the live replica. Skipped by default.
- GoConvey `So()` assertions throughout, per go-conventions.

### Out of scope

- Adding MLWH source tables beyond the five listed
  (`pac_bio_*`, `oseq_*`, AVUs, etc.).
- Replacing `modernc.org/sqlite` or the MySQL driver.
- Frontend UX for the empty-cache hint message.
- Cosmetic refactors of seqmeta / results / frontend beyond what
  the schema migration and read-path fixes require.

### Implementor / reviewer skills

- Implementor: `go-implementor`. TDD, one acceptance test at a
  time, run the test suite after each green.
- Reviewer: `go-reviewer`. Verifies every acceptance test maps to
  a GoConvey assertion; verifies no production code was added
  beyond what the spec required.
