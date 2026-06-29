# Sequencing Availability, Recency and Sample Progress Specification

## Overview

The `wa mlwh serve` REST API (package `mlwh/`) cannot today answer cheaply the
commonest study questions: how many samples have sequencing data (and how
many do not), whether any data was added to iRODS recently, what is in a study,
and where a sample is in the pipeline. This feature adds a small, indexed,
platform-aware aggregate + recency + pipeline-progress + budget-safety surface,
so each question is one request with a fixed-size response.

It works for **every** sequencing platform through one uniform mechanism. The
cache's iRODS sample/study linkage becomes a UNION across every platform's
`*_product_metrics` table, each iRODS row carrying a `platform` and a `created`
timestamp. QC and within-sequencing status come from each platform's own
tables. ONT, which has no product-metrics/iRODS/QC/status, resolves identity,
study and the milestone timeline and returns an explicit "not tracked for ONT"
on availability/recency/QC/status -- never a false zero (HARD REQ 11).

Three pipeline-progress layers ensure every sample resolves: an always-derivable
P0 baseline (registered -> sequenced[+QC] -> delivered[+date]) from already
mirrored data; the milestone timeline from `seq_ops_tracking_per_sample`; and
the within-sequencing run-status detail per platform. Layer absence is reported
as _less detail_, never as an error.

All new aggregates reuse `queryCount`, the four-step add-a-query recipe, the
one-`case`-per-`Method` handler, the `id_lims = 'SQSCP'` invariant, and the
never-synced / empty-study cascade established by `CountSamplesForStudy`.

## Architecture

### Packages and files

- `mlwh/` -- all production code (new files below; reuse existing files).
- `mlwh/cache_schema/{sqlite,mysql}/*.sql` -- schema in both dialects.
- `.docs/mcp/api-reference.md`, `.docs/mcp/glossary.md` -- regenerated/updated.

### New / changed source files

| File                   | Purpose                                                                                                        |
| ---------------------- | -------------------------------------------------------------------------------------------------------------- |
| `mlwh/availability.go` | overview, samples-with/without-data counts+lists, windowed counts/lists, run overview                          |
| `mlwh/progress.go`     | P0 baseline, `/sample/:id/progress`, `/study/:id/status-breakdown`, run-status timeline                        |
| `mlwh/types.go`        | new result structs (additive)                                                                                  |
| `mlwh/registry.go`     | new `Endpoint` entries                                                                                         |
| `mlwh/queryer.go`      | new `Queryer` members                                                                                          |
| `mlwh/server.go`       | one handler `case` per new Method; `since`/`until` query parsing; response-header writes (M)                   |
| `mlwh/remote.go`       | new `RemoteClient` methods incl. the typed `Page[T]` paged variants (M)                                        |
| `mlwh/sync.go`         | iRODS source SELECTs gain `spi.created`, `spi.seq_platform_name`; per-platform UNION linkage; new mirror syncs |
| `mlwh/cache_schema.go` | register new mirror tables in the order/migration lists                                                        |
| `mlwh/freshness.go`    | extend `freshnessSyncTables`; `HighWater` semantics per sync mode                                              |

### Existing infrastructure (authoritative; reuse, do not duplicate)

- Registry:
  `Endpoint{Method,Verb,Path,PathParams,Query,Paginated,NewResult,Summary,Description,QueryParams}`;
  `newResult[T]`, `newSliceResult[T]`, `fetchAllPaginationParams()`. OpenAPI +
  endpoint reference derive from it.
- `queryCount(ctx, sql, action, args...) (int, error)` (`mlwh/count.go`).
- Never-synced cascade: `requireAnySyncState`, `requiredSyncStateSummary`,
  `cacheStudyExists`, `neverSyncedReadErr` (`ErrNotFound` +
  `ErrCacheNeverSynced`), and `countSamplesForEmptyStudy` (the
  study-exists/empty/unknown pattern).
- Handler switch `mlwhEndpointHandler` (one `case` per Method); `mlwhPathParam`,
  `mlwhIDAndPagination`, `mlwhQueryInt`, `writeMLWHResult`,
  `writeMLWHBadRequest`, and the handler-helper family.
- RemoteClient `remoteCall[T]` (decodes body only -- discards headers).
- Freshness: `freshnessSyncTables`,
  `TableFreshness{Table,HighWater,LastRun,EverSynced}`,
  `formatFreshnessTime`, `utcRFC3339Layout`.
- Sync: `seqProductIRODSLocationsSyncRow`, the six iRODS source query funcs
  (composition-expansion + legacy x initial/cold/cursor),
  `scanSeqProductIRODSLocationsSyncRow`,
  `seqProductIRODSLocationsMirrorColumns`/`...RowArgs`.
- `CacheSchemaVersion` (`cache.go`) -- bump on the schema change; migration uses
  `cacheMigrationRecreateTables`/`...DropTables`/`...SyncStateTables`.

### Canonical phase vocabularies (closed enums; assert verbatim)

- **9 milestone names** (ordered): `manifest_created`, `manifest_uploaded`,
  `labware_received`, `order_made`, `working_dilution`, `library_start`,
  `library_complete`, `sequencing_run_start`, `sequencing_qc_complete`.
- **3 baseline phases**: `registered`, `sequenced`, `delivered`.
- **Status-breakdown ladder** (mutually exclusive, sums per partition):
  `with_data`, `sequenced_no_data`, `registered`.
- **Open status vocabulary** (NOT a frozen list; source/dict pass-through):
  Illumina `iseq_run_status_dict.description`; PacBio/Elembio/Ultimagen native
  `run_status`/`well_status`; ONT none.
- **QC string**: `pass` / `fail` / `pending` (overall `qc`: 1->pass, 0->fail,
  NULL->pending). Per-sample roll-up: fail if any product fails, else pending if
  any is pending, else pass.

### New result types (additive to `mlwh/types.go`)

```go
// StudyOverview is the fixed-size study aggregate (S + O1 collapsed).
type StudyOverview struct {
    IDStudyLims          string   `json:"id_study_lims" doc:"LIMS study id"`
    SamplesTotal         int      `json:"samples_total" doc:"distinct samples linked via library_samples"`
    SamplesWithData      int      `json:"samples_with_data" doc:"distinct samples with >=1 study-scoped iRODS row"`
    SamplesWithoutData   int      `json:"samples_without_data" doc:"samples_total minus samples_with_data"`
    SamplesSequencedNoData int    `json:"samples_sequenced_no_data" doc:"distinct samples with product-metrics in this study but no iRODS rows (distinct-sample partition)"`
    DataObjects          int      `json:"data_objects" doc:"study-scoped iRODS data objects"`
    Runs                 int      `json:"runs" doc:"distinct runs for the study"`
    Libraries            int      `json:"libraries" doc:"distinct libraries for the study"`
    LibraryTypes         []string `json:"library_types" doc:"distinct library types present"`
    SequencingDateRange  *DateRange `json:"sequencing_date_range,omitempty" doc:"earliest/latest iRODS created for the study"`
    NewestDataAdded      string   `json:"newest_data_added" doc:"latest study-scoped iRODS created (UTC RFC3339), empty if none"`
    AddedLast7Days       int      `json:"added_last_7_days" doc:"distinct samples whose data was added in [now-7d, now)"`
    CacheSyncedAt        string   `json:"cache_synced_at" doc:"oldest last_run across feeding tables (UTC RFC3339)"`
}

// DateRange is an earliest/latest RFC3339 pair (empty strings when absent).
type DateRange struct {
    Earliest string `json:"earliest" doc:"earliest timestamp (UTC RFC3339)"`
    Latest   string `json:"latest" doc:"latest timestamp (UTC RFC3339)"`
}

// SampleWithData is the enriched list row for samples-with/without-data; it
// carries platforms so every negative is platform-qualified (HARD REQ 11). It
// is a NEW type, not the shared Sample struct.
type SampleWithData struct {
    Sample    Sample   `json:"sample" doc:"the sample"`
    Platforms []string `json:"platforms" doc:"platforms the sample has products on; empty for registered, [\"ONT\"] for ONT"`
}

// RunOverview is the fixed-size run aggregate (O2).
type RunOverview struct {
    IDRun               int        `json:"id_run" doc:"Illumina NPG run id"`
    Samples             int        `json:"samples" doc:"distinct samples on the run"`
    Studies             int        `json:"studies" doc:"distinct studies on the run"`
    DataObjects         int        `json:"data_objects" doc:"iRODS data objects for the run"`
    SequencingDateRange *DateRange `json:"sequencing_date_range,omitempty" doc:"earliest/latest iRODS created for the run"`
    CacheSyncedAt       string     `json:"cache_synced_at" doc:"oldest last_run across feeding tables (UTC RFC3339)"`
}

// StatusBreakdown is the per-baseline-phase rollup (P3).
type StatusBreakdown struct {
    IDStudyLims      string                  `json:"id_study_lims" doc:"LIMS study id"`
    Distinct         PhaseLadder             `json:"distinct" doc:"distinct-sample partition, sums to samples_total"`
    PerPlatform      []PlatformPhaseLadder   `json:"per_platform" doc:"per-platform partition; grand total may exceed samples_total"`
    WithDetailedTimeline int                 `json:"with_detailed_timeline" doc:"samples also present in the tracking mirror"`
    CacheSyncedAt    string                  `json:"cache_synced_at" doc:"oldest last_run across feeding tables (UTC RFC3339)"`
}

type PhaseLadder struct {
    WithData        int `json:"with_data" doc:"samples with >=1 study-scoped iRODS row"`
    SequencedNoData int `json:"sequenced_no_data" doc:"samples sequenced in this study but no iRODS rows"`
    Registered      int `json:"registered" doc:"linked samples with no product-metrics (incl. ONT)"`
}

type PlatformPhaseLadder struct {
    Platform string      `json:"platform" doc:"platform name"`
    Ladder   PhaseLadder `json:"ladder" doc:"buckets summing to this platform's sample count"`
}

// SampleProgress is the unified progress response (P2/P4/P5/P6).
type SampleProgress struct {
    Sample           Sample           `json:"sample" doc:"the sample"`
    Platforms        []string         `json:"platforms" doc:"detected platforms; [\"ONT\"] for ONT, empty when registered only"`
    BaselinePhase    string           `json:"baseline_phase" doc:"registered|sequenced|delivered (most-advanced across platforms)"`
    QC               string           `json:"qc" doc:"pass|fail|pending|not_tracked (overall verdict, rolled up)"`
    DeliveredAt      string           `json:"delivered_at" doc:"earliest iRODS created (UTC RFC3339), empty if none"`
    DetailedTimeline bool             `json:"detailed_timeline" doc:"true when the sample is in the tracking mirror"`
    TimelineReason   string           `json:"timeline_reason,omitempty" doc:"why detailed_timeline is false (e.g. not in tracking window)"`
    Milestones       []Milestone      `json:"milestones,omitempty" doc:"ordered milestone timeline when detailed_timeline"`
    CurrentMilestone string           `json:"current_milestone,omitempty" doc:"latest reached milestone whose successor is NULL"`
    Runs             []RunStatusTimeline `json:"runs,omitempty" doc:"per-run within-sequencing status timeline"`
    CacheSyncedAt    string           `json:"cache_synced_at" doc:"oldest last_run across feeding tables (UTC RFC3339)"`
}

// Milestone is one wet-lab/sequencing milestone (uses reached_at).
type Milestone struct {
    Name       string `json:"name" doc:"one of the 9 milestone names"`
    ReachedAt  string `json:"reached_at" doc:"when the milestone was reached (UTC RFC3339)"`
    DurationToNext string `json:"duration_to_next,omitempty" doc:"ISO8601-style duration to the next reached milestone; empty for the open current phase"`
}

// RunStatusTimeline is one run's normalized status timeline (P5). Embedded per
// run in SampleProgress AND returned by GET /run/:id/status, so they never drift.
type RunStatusTimeline struct {
    IDRun   int               `json:"id_run" doc:"Illumina NPG run id (0/empty for non-Illumina)"`
    Platform string           `json:"platform" doc:"platform of the run"`
    Events  []RunStatusEvent  `json:"events" doc:"ordered status events; empty for ONT"`
    Current string            `json:"current" doc:"phase of the event with the latest entered_at (derived, not source iscurrent)"`
    NotTracked string         `json:"not_tracked,omitempty" doc:"set to a reason when status is not tracked for the platform"`
}

// RunStatusEvent is one status transition (uses entered_at).
type RunStatusEvent struct {
    Phase     string `json:"phase" doc:"native status description (open vocabulary)"`
    EnteredAt string `json:"entered_at" doc:"when the phase was entered (UTC RFC3339)"`
    Duration  string `json:"duration,omitempty" doc:"duration to the next event; empty for the current/open phase"`
}

// Page is the typed paged variant exposing list-sizing headers to Go callers (M).
type Page[T any] struct {
    Items      []T
    Total      int
    NextOffset int
}
```

### Sizing metadata (M) -- headers, bodies unchanged

- Paginated list handlers additionally set response headers `X-Total-Count`
  (total matching rows) and `X-Next-Offset` (offset of the next page, or `-1`
  when the page is the last). Response **bodies stay bare JSON arrays** -- no
  envelope; existing tests, OpenAPI schemas and the MCP surface are unchanged.
- `RemoteClient` gains typed `Page[T]` paged-variant methods that parse these
  headers in the single remote header path (e.g. `SamplesForStudyPage`,
  `IRODSPathsForStudyPage`, `SamplesWithDataPage`, ...). Existing bare-slice
  methods stay unchanged. Do NOT expose sizing via the dynamic `Call`
  dispatcher; do NOT use a stateful "last page meta" accessor.
- The `/count` counterparts (N) remain the canonical pre-transfer sizing method.

### Definition of "sequencing data available" (state in every Description)

- A sample **has data for this study** iff it has >=1 row in
  `seq_product_irods_locations_mirror` scoped by `id_study_lims = :id` (real
  data files in iRODS), anchored on `library_samples` membership.
- Study scoping is by `seq_product_irods_locations_mirror.id_study_lims = :id`
  (as `/study/:id/irods` already scopes), NOT "data the sample has anywhere".
- `samples-without-data` = study's linked samples minus samples-with-data;
  `with_data + without_data = samples_total`. It includes `sequenced_no_data`,
  `registered`, and ONT.
- The overview also reports `samples_sequenced_no_data` (product-metrics in this
  study, no iRODS rows), scoped by product-metrics `id_study_lims`.

### Multi-platform partitioning (two denominators, by surface)

- `GET /study/:id/status-breakdown` `per_platform`: within each platform the
  ladder buckets sum to that platform's sample count; a sample's true state
  shows under each of its platforms, so the grand total may exceed
  `samples_total`.
- Overview ladder figures and `samples-with-data`/`samples-without-data` lists +
  counts: distinct-sample partition summing to `samples_total`; a multi-platform
  sample collapses to one bucket by most-advanced phase, precedence
  `with_data` > `sequenced_no_data` > `registered`.

### Window semantics (T, overview)

- Half-open `[since, until)`: `created >= since AND created < until`. `until`
  optional (open-ended when omitted). `created == since` included;
  `created == until` excluded. Compare in normalized UTC.
- `since`/`until` are RFC3339 query params; a malformed value -> 400. The
  overview's `added_last_7_days` uses the same half-open rule over
  `[now-7d, now)`.

### Run-id space (O2, P5; state in Description)

- `:id` for `/run/:id/status` and `/run/:id/overview` is the **Illumina NPG
  `id_run`** (existing `Run`/`ResolveRun` space; no new resolver). A
  non-Illumina run yields the existing not-found / unsupported-identifier error.
  Cross-platform sequencing status is reached via `/sample/:id/progress` where
  the platform is known.

### Error handling

- Reuse the existing sentinels (`ErrNotFound`, `ErrCacheNeverSynced`,
  `ErrUpstreamImpaired`) and the never-synced cascade. New count/aggregate
  endpoints return `Count{}` / zero-value + `neverSyncedReadErr()` on a
  never-synced cache, `ErrNotFound` for an unknown study/run, and a zero-figure
  result for a synced-but-empty study -- as `CountSamplesForStudy` does.

## A. Schema and sync changes (foundation)

### A1: iRODS mirror gains created + platform, both dialects

As an implementor, I want the iRODS mirror to carry the creation time and the
platform, so recency and platform-aware availability are answerable.

Add to `seq_product_irods_locations_mirror` in **both**
`cache_schema/sqlite/...` and `cache_schema/mysql/...`:

- column `created TEXT NOT NULL` (sqlite) / `created VARCHAR(255) NOT NULL`
  (mysql), stored RFC3339 like `last_updated`.
- column `platform TEXT NOT NULL` (sqlite) / `platform VARCHAR(255) NOT NULL`
  (mysql).
- index `spi_mirror_study_lims_created_idx (id_study_lims, created)` in both
  dialects.

Bump `CacheSchemaVersion`. Keep `seq_product_irods_locations_mirror` in
`cacheMigrationRecreateTables` and `cacheMigrationDropTables` so the migration
recreates it cleanly; the next iRODS sync repopulates it.

**Package:** `mlwh/`
**Files:** `cache_schema/{sqlite,mysql}/seq_product_irods_locations_mirror.sql`,
`cache_schema.go`, `cache.go`
**Test file:** `mlwh/cache_schema_test.go`

**Acceptance tests:**

1. Given the sqlite schema, when parsed into a `schemaShape`, then
   `seq_product_irods_locations_mirror` has columns `created` (text) and
   `platform` (text) and an index on `(id_study_lims, created)`.
2. Given the mysql schema, when parsed, then the same columns and index exist,
   so the two dialects compare equal (existing cross-dialect shape test passes).
3. Given an opened ephemeral sqlite cache, when a row is inserted with the new
   column list, then it reads back with the stored `created` and `platform`.

### A2: iRODS sync mirrors created and platform across all platforms

As an implementor, I want the iRODS source SELECTs to carry `spi.created` and
`spi.seq_platform_name`, and the sample/study linkage to span all platforms, so
non-Illumina data stops being dropped.

- Add `spi.created` and `spi.seq_platform_name` to **all six** iRODS source
  query funcs (composition-expansion initial/cold/cursor + legacy
  initial/cold/cursor). Keep the incremental window keyed on `spi.last_changed`
  (no high-water change); `created` rides along.
- Replace the single Illumina-only sample/study recovery with a **UNION** over
  every platform's `*_product_metrics` keyed on `spi.id_product`:
    - Illumina: keep the existing composition-expansion join (and the legacy
      direct join) UNCHANGED, to preserve current `/study/:id/irods` results.
    - PacBio: `pac_bio_product_metrics.id_pac_bio_product = spi.id_product` ->
      `pac_bio_run` for sample/study.
    - Elembio: `eseq_product_metrics.id_eseq_product = spi.id_product` ->
      `eseq_flowcell`.
    - Ultimagen: `useq_product_metrics.id_useq_product = spi.id_product` ->
      `useq_wafer` (join `useq_product_metrics.id_useq_wafer_tmp`; `useq_wafer`
      carries `id_sample_tmp`/`id_study_tmp`).
    - The UNION recovers only `id_sample_tmp`/`id_study_lims`. `platform` comes
      from `spi.seq_platform_name`, NEVER from which metrics table matched.
- Extend `seqProductIRODSLocationsSyncRow` with `Created time.Time` and
  `Platform string`; `scanSeqProductIRODSLocationsSyncRow` scans both;
  `seqProductIRODSLocationsMirrorColumns`/`...RowArgs` include both;
  `...MirrorRowArgs` formats `Created` via `formatSyncTime`.

**Package:** `mlwh/`
**File:** `mlwh/sync.go`
**Test file:** `mlwh/sync_test.go`

**Acceptance tests:**

1. Given a mocked source returning iRODS rows with `created` and
   `seq_platform_name = "illumina"`, when the iRODS table syncs, then mirror
   rows store the supplied `created` and `platform`.
2. Given a mocked source PacBio iRODS row whose `id_product` matches a
   `pac_bio_product_metrics` row (and no Illumina product), when syncing, then a
   mirror row is written with the PacBio sample/study and `platform` from
   `seq_platform_name` -- not dropped.
3. Given a row whose `seq_platform_name` is "pacbio" but that also matches an
   Illumina product, when syncing, then `platform` is `pacbio` (from
   `seq_platform_name`), proving platform is not derived from the matched table.

### A3: nullable QC across all platforms, NULL preserved as pending

As an implementor, I want mirrored QC columns nullable and NULL-preserving, so
pending is distinct from fail.

- Make `qc`, `qc_seq`, `qc_lib` nullable in both dialects on every
  product-metrics mirror that carries QC (Illumina, PacBio, Elembio, Ultimagen);
  stop coercing NULL->0 in the sync scan/insert.
- Folded into the same full resync as A2 (one resync covers `created`,
  `platform`, nullable QC).

**Package:** `mlwh/`
**Files:** `cache_schema/{sqlite,mysql}/*_product_metrics_mirror.sql`,
`mlwh/sync.go`
**Test file:** `mlwh/sync_test.go`

**Acceptance tests:**

1. Given the schema, when parsed, then
   `iseq_product_metrics_mirror.qc/qc_seq/qc_lib` are nullable in both
   dialects.
2. Given a source product-metrics row with NULL `qc`, when synced, then the
   mirror stores SQL NULL (not 0), and a downstream read maps it to `pending`.
3. Given source rows with `qc` of 1, 0 and NULL, when synced, then they read
   back as `pass`, `fail`, `pending` respectively.

### A4: new platform-coverage mirror tables

As an implementor, I want the per-platform linkage/QC/status tables and the
tracking + run-status tables mirrored, so the feature has its sources.

Add CREATE TABLE + indexes in **both** dialects, register each in
`schemaStatementOrder` and the migration lists, for:

- `pac_bio_product_metrics_mirror` (`id_pac_bio_product`, sample/study link from
  `pac_bio_run`, nullable `qc`), `pac_bio_run_well_metrics_mirror`
  (`run_start`/`run_complete`/`well_complete`/`qc_seq_date`,
  `run_status`/`well_status`).
- `eseq_product_metrics_mirror` (nullable `qc`/`qc_seq`/`qc_lib`, link from
  `eseq_flowcell`), `eseq_run`/`eseq_run_lane_metrics` mirrors for status+dates.
- `useq_product_metrics_mirror` (nullable QC, link via `useq_wafer`),
  `useq_run_metrics_mirror` for status+dates.
- `oseq_flowcell_mirror` (ONT identity/metadata only: `id_sample_tmp`,
  `id_study_lims` / study link, no product/QC/status).
- `iseq_run_status_mirror` (`id_run_status` PK, `id_run`, `date`,
  `id_run_status_dict`, `iscurrent`), indexes `(id_run)` and `(id_run, date)`;
  `iseq_run_status_dict_mirror` (`id_run_status_dict`, `description`,
  `temporal_index`) mirrored wholesale.
- `seq_ops_tracking_per_sample_mirror`: the 9 milestone `datetime` columns
  (RFC3339 TEXT) + lookup/context columns (`id_sample_lims`, `sanger_sample_id`,
  `sanger_sample_name`, `study_id`, `programme`, `faculty_sponsor`,
  `library_type`, `platform`); indexed by `id_sample_lims`,
  `sanger_sample_name`, `study_id`.

**Package:** `mlwh/`
**Files:** `cache_schema/{sqlite,mysql}/*.sql`, `cache_schema.go`
**Test file:** `mlwh/cache_schema_test.go`

**Acceptance tests:**

1. Given the sqlite and mysql schemas, when parsed, then every new mirror table
   above exists with its declared indexes and the two dialects compare equal.
2. Given an opened ephemeral cache, when a row is inserted into each new mirror
   with its column list, then it reads back unchanged.
3. Given `seq_ops_tracking_per_sample_mirror`, when parsed, then it carries all
   9 milestone columns and is indexed by `id_sample_lims`, `sanger_sample_name`,
   `study_id`.

### A5: sync strategies for the new tables

As an implementor, I want each new mirror synced by the right strategy, so the
freshness surface is honest.

- `iseq_run_status`: ascending-id mode on the `id_run_status` PK (cf.
  `seqProductIRODSLocationsIDMode`); no `last_changed`. Derive "current" at read
  time from the latest `date` per `id_run` (never the source `iscurrent`).
- `iseq_run_status_dict` and `oseq_flowcell` (and the per-platform status/dict
  tables that lack `last_changed`): mirror wholesale / by their available key.
- `seq_ops_tracking_per_sample`: **full-table refresh, build-and-atomic-swap**
  each run (no `GREATEST(milestones)` watermark). `high_water` = refresh time;
  `last_run` = sync time. Allowed to run on its own slower cadence.
- Per-platform `*_product_metrics` incremental tables follow the existing
  `last_changed` precedent.

**Package:** `mlwh/`
**File:** `mlwh/sync.go`
**Test file:** `mlwh/sync_test.go`

**Acceptance tests:**

1. Given a mocked `iseq_run_status` source, when synced, then rows are read in
   ascending `id_run_status` order and the mirror contains all of them.
2. Given an existing populated `seq_ops_tracking_per_sample_mirror` and a fresh
   source snapshot, when the full-refresh sync runs, then the mirror equals the
   new snapshot (old rows gone) and the swap is atomic (a concurrent read sees
   either all-old or all-new, never a partial table).
3. Given a tracking-table sync, when it completes, then its `sync_state`
   `high_water` is the refresh time and `last_run` is the sync time.

### A6: freshness surface includes every new mirror

As a caller, I want `/freshness` to report the new mirrors, so I can caveat
recency honestly.

- Append every new sync table to `freshnessSyncTables` (order: existing 5, then
  the new ones).
- `HighWater` is RFC3339-or-empty by sync mode: refresh time for full-refresh
  tables (tracking), latest `last_changed` for incremental tables, empty for
  ascending-id tables (`iseq_run_status`). `last_run` is universal.
- Update the `len(...Tables) == 5` assertions to the new total.

**Package:** `mlwh/`
**File:** `mlwh/freshness.go`
**Test file:** `mlwh/freshness_test.go`

**Acceptance tests:**

1. Given a never-synced cache, when `Freshness` runs, then it returns one entry
   per table in `freshnessSyncTables` (the new total), each `ever_synced=false`
   with empty timestamps, and succeeds (no error).
2. Given a tracking-table `sync_state` row, when `Freshness` runs, then that
   table's `high_water` equals its refresh time and `last_run` its sync time.
3. Given an `iseq_run_status` `sync_state` row, when `Freshness` runs, then its
   `high_water` is empty and `last_run` carries its sync time.

## B. Availability (S, C, E)

### B1: cheap study overview (S + O1 collapsed)

As a study owner, I want one small response answering "what's in study X / how
much data / anything new", so I avoid the giant aggregates.

`GET /study/:id/overview` -> `StudyOverview`. All figures are single indexed
aggregates over `library_samples`, `seq_product_irods_locations_mirror`
(study-scoped, with `created`/`platform`), product-metrics mirrors, and the
library tables. `samples_with_data`/`without_data`/`sequenced_no_data` use the
distinct-sample partition (most-advanced-phase precedence). `cache_synced_at` =
oldest `last_run` across the feeding tables (iRODS + product-metrics + study +
sample). `Description` states the "available" definition, the study-scoping
rule, the `samples_sequenced_no_data` definition, the half-open
`added_last_7_days` window, and the freshness caveat.

**Package:** `mlwh/`
**File:** `mlwh/availability.go`
**Test file:** `mlwh/availability_test.go`

**Method signature:**
`StudyOverview(ctx, studyLimsID string) (StudyOverview, error)`

**Acceptance tests:**

1. Given study `S1` with 5 linked samples: 3 with study-scoped iRODS rows
   (Illumina), 1 sequenced (product-metrics, no iRODS), 1 registered (library
   only); 7 total iRODS data objects across 2 runs; library types
   {Standard, Chromium}, when `StudyOverview("S1")` is called, then
   `samples_total=5`, `samples_with_data=3`, `samples_without_data=2`,
   `samples_sequenced_no_data=1`, `data_objects=7`, `runs=2`,
   `library_types` (sorted) = ["Chromium","Standard"].
2. Given the same study with iRODS `created` times of 2026-06-01, 2026-06-25,
   2026-06-26 and "now" = 2026-06-28, when called, then `newest_data_added` is
   `2026-06-26T...Z` and `added_last_7_days` counts only samples whose data was
   added in `[2026-06-21, 2026-06-28)`.
3. Given a sample of `S1` shared with study `S2` that has iRODS data only under
   `S2`, when `StudyOverview("S1")` is called, then that sample is NOT counted
   in `S1`'s `samples_with_data` (study-scoped rule).
4. Given a never-synced cache, when called, then it returns an error satisfying
   both `ErrCacheNeverSynced` and `ErrNotFound`.
5. Given an unknown study id on a synced cache, when called, then it returns
   `ErrNotFound`.
6. Given a synced study with no samples, when called, then it returns a
   `StudyOverview` with all counts 0 and `cache_synced_at` populated.

### B2: bare count of samples-with-data

As a caller, I want a bare count of samples-with-data, so I can size before
listing.

`GET /study/:id/samples-with-data/count` -> `Count`, via `queryCount` over
`library_samples -> sample_mirror -> seq_product_irods_locations_mirror` scoped
by `id_study_lims`, `COUNT(DISTINCT id_sample_tmp)`. Same membership as the
list (B3). Never-synced/empty/unknown cascade as `CountSamplesForStudy`.

**Package:** `mlwh/`
**File:** `mlwh/availability.go`
**Test file:** `mlwh/availability_test.go`

**Method signature:**
`CountSamplesWithData(ctx, studyLimsID string) (Count, error)`

**Acceptance tests:**

1. Given study `S1` (3 samples with study-scoped iRODS rows; one of them has 4
   iRODS rows), when `CountSamplesWithData("S1")` is called, then `Count{3}`
   (distinct samples, not data objects).
2. Given `S1`, when both `CountSamplesWithData("S1")` and the
   `SamplesWithData("S1", all)` list are taken, then the count equals the list
   length.
3. Given a never-synced cache, then the same `ErrCacheNeverSynced`+`ErrNotFound`
   behaviour as `CountSamplesForStudy`.

### B3: enumerate samples with / without data + iRODS row identity

As a caller, I want to list which samples have or lack data and to aggregate the
study iRODS list by sample, so I can act per sample.

- `GET /study/:id/samples-with-data` and `GET /study/:id/samples-without-data`
  -> `[]SampleWithData`, paginated like other study fan-outs, distinct-sample
  partition. `with_data` = samples with >=1 study-scoped iRODS row;
  `without_data` = linked samples minus those (includes sequenced_no_data,
  registered, ONT). `platforms` is empty for registered, `["ONT"]` for ONT.
- Add `id_sample_tmp` and Sanger `name` to the `IRODSPath` rows returned by
  `/study/:id/irods` (additive fields), so that list is aggregatable by sample
  standalone. (`/sample/:id/irods` rows may carry them too for consistency.)

**Package:** `mlwh/`
**File:** `mlwh/availability.go` (+ `IRODSPath` field additions in `types.go`,
SQL select changes in `hierarchy.go`)
**Test file:** `mlwh/availability_test.go`, `mlwh/hierarchy_test.go`

**Method signatures:**
`SamplesWithData(ctx, studyLimsID string, limit, offset int) ([]SampleWithData, error)`
`SamplesWithoutData(ctx, studyLimsID string, limit, offset int) ([]SampleWithData, error)`

**Acceptance tests:**

1. Given study `S1` (3 with data, 2 without), when `SamplesWithData("S1", all)`
   and `SamplesWithoutData("S1", all)` are called, then the two lists are
   disjoint, their lengths are 3 and 2, and union length = `samples_total` (5).
2. Given the PacBio sample in `S1`, when it appears in `SamplesWithData`, then
   its `platforms` contains `"PacBio"`.
3. Given the ONT sample linked to `S1` (no iRODS), when `SamplesWithoutData` is
   called, then it appears with `platforms == ["ONT"]` (not folded into a bare
   "no data").
4. Given a registered-only sample (library link, no products), when
   `SamplesWithoutData` is called, then it appears with `platforms == []`.
5. Given `/study/:id/irods` for `S1`, when the rows are fetched, then each row
   carries the correct `id_sample_tmp` and Sanger `name`, and grouping the rows
   by `id_sample_tmp` yields exactly the 3 samples-with-data.
6. Given `CountSamplesWithData("S1")`, then it equals
   `len(SamplesWithData("S1", all))` (cross-check).

## C. Recency (T)

### C1: windowed samples-with-data count

As a caller, I want a count of samples whose data was added to iRODS in a
window, so I can answer "new this week".

`GET /study/:id/samples-with-data/count?since=<RFC3339>[&until=<RFC3339>]` ->
`Count`. Filters on the mirrored `created` column over the
`(id_study_lims, created)` index, half-open `[since, until)`,
`COUNT(DISTINCT id_sample_tmp)`. Without `since`, behaves as B2 (all-time). A
malformed `since`/`until` -> 400. `Description` states it filters on the
iRODS-creation timestamp (NEVER `last_updated`/`last_run`), the half-open
semantics, and the freshness caveat (complete only up to `last_run`).

**Package:** `mlwh/`
**File:** `mlwh/availability.go`
**Test file:** `mlwh/availability_test.go`

**Method signature:**
`CountSamplesWithDataSince(ctx, studyLimsID, since, until string) (Count, error)`

**Acceptance tests:**

1. Given `S1` with iRODS `created` at 2026-06-20, 2026-06-25, 2026-06-26 (3
   distinct samples), when called with `since=2026-06-21T00:00:00Z`, then
   `Count{2}`.
2. Given a row with `created == since` and another with `created == until`, when
   called with that `since` and `until`, then the `since` row is included and
   the `until` row excluded (half-open).
3. Given `since=not-a-date`, when called, then the handler returns 400
   bad_request (the queryer is not reached).
4. Given a never-synced cache, then `ErrCacheNeverSynced`+`ErrNotFound`.

### C2: windowed samples-with-data list

As a caller, I want the newly-covered samples in the window, so I can see what
is new.

`GET /study/:id/samples-with-data?since=<RFC3339>[&until=<RFC3339>]` reuses the
B3 list endpoint with the same window filter, returning `[]SampleWithData`
(distinct samples whose data was added in `[since, until)`), paginated.

**Package:** `mlwh/`
**File:** `mlwh/availability.go`
**Test file:** `mlwh/availability_test.go`

**Acceptance tests:**

1. Given `S1` as in C1.1, when `SamplesWithData("S1", all)` is called with
   `since=2026-06-21T00:00:00Z`, then the list has the 2 in-window samples and
   its length equals `CountSamplesWithDataSince("S1", since, "")`.
2. Given on-boundary rows, when listed with `since`/`until`, then membership
   matches the half-open rule (C1.2).

## D. Run overview (O2)

### D1: cheap run overview

As a caller, I want a small run aggregate, so "what's on this run / how much"
needs neither `/run/:id/detail` nor per-sample calls.

`GET /run/:id/overview` -> `RunOverview` (distinct samples / studies / iRODS
data objects on the run, sequencing date range from iRODS `created`, freshness).
`:id` is the Illumina NPG `id_run` (stated in the Description). Separate small
aggregate; NOT folded into `/run/:id/detail`, NOT added to the bare `Run`
struct.

**Package:** `mlwh/`
**File:** `mlwh/availability.go`
**Test file:** `mlwh/availability_test.go`

**Method signature:** `RunOverview(ctx, idRun string) (RunOverview, error)`

**Acceptance tests:**

1. Given run `52553` with 4 distinct samples across 2 studies and 6 iRODS data
   objects, when `RunOverview("52553")` is called, then `samples=4`,
   `studies=2`, `data_objects=6`, and `sequencing_date_range` spans the min/max
   iRODS `created`.
2. Given a never-synced cache, then `ErrCacheNeverSynced`+`ErrNotFound`.
3. Given an `:id` that is not a valid Illumina run, then `ErrNotFound`.

## E. Budget-safety completion (N, M, L)

### E1: /count counterpart for every paginated list

As a caller, I want a `/count` for each paginated list, so any list can be sized
before transfer.

Add `Count` endpoints (each `queryCount` + four-step recipe, same filter/join as
its list, no LIMIT, so count == len(list-all)):

- `/study/:id/irods/count`, `/sample/:id/irods/count`,
  `/study/:id/runs/count`, `/study/:id/libraries/count`,
  `/sample/:id/lanes/count`, `/run/:id/samples/count`,
  the `library*/samples` counts, and the `find/sample/*` counts.

**Package:** `mlwh/`
**File:** `mlwh/count.go` (extend)
**Test file:** `mlwh/count_test.go`

**Acceptance tests:**

1. For each new `/count`, given a seeded fixture, when the count and the
   corresponding list-all are taken, then `count == len(list)`.
2. For each new `/count`, given a never-synced cache, then the same
   `ErrCacheNeverSynced`+`ErrNotFound` behaviour as its list.
3. Given a synced-but-empty parent, when each `/count` is called, then
   `Count{0}` with no error.

### E2: list-sizing response headers + typed Page[T] remote variant

As a Go consumer, I want list-sizing metadata, so one page reveals how much
remains.

- Paginated list handlers set `X-Total-Count` (total matching rows; one extra
  COUNT query) and `X-Next-Offset` (`offset+len(items)` if more remain, else
  `-1`). Bodies remain bare arrays.
- Add `Page[T]` typed paged-variant methods on `RemoteClient` that parse those
  headers. Existing bare-slice methods unchanged.

**Package:** `mlwh/`
**Files:** `mlwh/server.go`, `mlwh/remote.go`, `mlwh/types.go`
**Test file:** `mlwh/server_test.go`, `mlwh/remote_test.go`

**Acceptance tests:**

1. Given 25 matching rows and a request with `limit=10&offset=0`, when the list
   endpoint responds, then the body is a 10-element array, `X-Total-Count: 25`,
   `X-Next-Offset: 10`.
2. Given `limit=10&offset=20`, when it responds, then 5 rows,
   `X-Total-Count: 25`, `X-Next-Offset: -1`.
3. Given a `RemoteClient` `Page[T]` variant against a server returning those
   headers, when called, then `Page.Total == 25` and `Page.NextOffset == 10`,
   and `Page.Items` equals the bare-slice method's result for the same args.

### E3: lean / de-duplicated detail aggregates

As a caller, I want bounded detail responses, so `/study/:id/detail` and
`/run/:id/detail` do not blow the budget.

- Add `limit`/`offset` pagination of the nested collections (libraries/samples
  for study; samples/studies/study_details for run).
- Add a `lean` query param (boolean) that drops the heavy nested objects
  (returns the top-level entity + flat id lists rather than embedded detail).
- De-duplicate repeated nested entities: return each study/library once in a
  lookup table (keyed by id) instead of re-embedding it under every sample.

**Package:** `mlwh/`
**Files:** `mlwh/enrich.go` (detail builders), `mlwh/types.go`,
`mlwh/server.go`, `mlwh/registry.go`
**Test file:** `mlwh/enrich_test.go`

**Acceptance tests:**

1. Given a study whose libraries cover the same study metadata repeatedly, when
   `StudyDetail` is built, then each distinct study/library appears once in the
   lookup table and nested rows reference it by id (no duplicate embedding).
2. Given `/study/:id/detail?lean=true`, when called, then the response omits the
   heavy nested per-sample objects and carries flat id lists, and its serialized
   size is strictly smaller than the non-lean response for the same study.
3. Given `/run/:id/detail?limit=2&offset=0`, when called, then at most 2 nested
   samples are returned and `X-Total-Count` reports the full count.

## F. Sample progress (P0-P6)

### F1: always-available baseline (P0)

As a user, I want a coarse phase that always resolves for any sample on any
platform, so there is no "works for this sample, not that one" cliff.

Derive `baseline_phase`: `registered` (linked, no products) -> `sequenced` (has
product-metrics; QC rolled up to pass/fail/pending) -> `delivered` (has
study-scoped iRODS rows; `delivered_at` = earliest `created`). Sourced via the
platform-coverage union (product-metrics + iRODS mirrors for Illumina/PacBio/
Elembio/Ultimagen; `oseq_flowcell` for ONT, iRODS/QC reported `not_tracked`).
A multi-platform sample's baseline is the most-advanced phase across its
platforms; QC is the per-sample roll-up (fail > pending > pass) on the overall
`qc`.

**Package:** `mlwh/`
**File:** `mlwh/progress.go`
**Test file:** `mlwh/progress_test.go`

**Acceptance tests:**

1. Given a sample with a library link but no products, when its baseline is
   derived, then `baseline_phase == "registered"`, `qc == "pending"` is NOT set
   (qc is `not_tracked` / empty for no products), `delivered_at == ""`.
2. Given a sample with Illumina products (`qc` NULL) and no iRODS, then
   `baseline_phase == "sequenced"` and `qc == "pending"`.
3. Given a sample with products (`qc`=1 on all) and study-scoped iRODS rows
   created 2026-06-25 and 2026-06-26, then `baseline_phase == "delivered"`,
   `qc == "pass"`, `delivered_at == 2026-06-25T...Z` (earliest).
4. Given a sample with two products, one `qc`=0 and one `qc`=1, then
   `qc == "fail"` (any fail -> fail).
5. Given the ONT sample (`oseq_flowcell`, no products/iRODS), then
   `baseline_phase == "registered"`, `qc == "not_tracked"`,
   `platforms == ["ONT"]`.
6. Given a sample delivered on Illumina but only sequenced on PacBio, then
   `baseline_phase == "delivered"` (most-advanced); `platforms` has both.

### F2: within-sequencing run-status timeline (P5)

As a user, I want the within-sequencing status timeline per run, so I can see
the NPG lifecycle.

`GET /run/:id/status` -> a single `RunStatusTimeline` (normalized
`{phase, entered_at, duration}` events). `:id` = Illumina NPG `id_run`.

- Illumina: from `iseq_run_status_mirror` joined to
  `iseq_run_status_dict_mirror` for `phase` (= `description`); ordered by
  `date`; `entered_at` = `date`;
  `current` = phase of the latest `date` (DERIVED, never source `iscurrent`);
  recurrences / on-hold / cancelled / stopped-early preserved faithfully (not
  forced monotonic).
- The same normalized type is produced for PacBio/Elembio/Ultimagen from their
  own status/dates (used by F3's per-run embedding); ONT -> empty events +
  `not_tracked`.
- `Description`: the open status vocabulary is a dict/source pass-through (NOT a
  frozen list); `current` is derived; `entered_at` is the run-lifecycle
  phase-entry field (distinct from milestone `reached_at`, with rationale).

**Package:** `mlwh/`
**File:** `mlwh/progress.go`
**Test file:** `mlwh/progress_test.go`

**Method signature:** `RunStatus(ctx, idRun string) (RunStatusTimeline, error)`

**Acceptance tests:**

1. Given run `52553` with `iseq_run_status` rows pending(t0) -> in progress(t1)
   -> complete(t2) -> ... -> qc review pending(t6, latest), when
   `RunStatus("52553")` is called, then `events` are ordered by `date`, each
   `entered_at` = its `date`, each non-last `duration` = delta to the next, the
   last event's `duration` is empty, and `current == "qc review pending"`.
2. Given a run whose source `iscurrent=1` is on an EARLIER `date` than the
   latest row, when called, then `current` is the latest-`date` phase, proving
   it is derived not from `iscurrent`.
3. Given a run with a repeated phase (e.g. "analysis in progress" twice) and an
   "on hold" event, when called, then both occurrences and the on-hold event
   appear in order (not deduplicated, not reordered).
4. Given a new (unknown) `iseq_run_status_dict` description in the seed, when
   called, then it passes through as the `phase` value (open vocabulary, not
   rejected).
5. Given an `:id` that is not a valid Illumina run, then `ErrNotFound`.

### F3: unified sample progress endpoint (P2/P4/P6)

As a user, I want one response with the baseline, the milestone timeline (when
tracked) and the per-run status, so "what's happening with my sample" is one
call.

`GET /sample/:id/progress` (by Sanger name) -> `SampleProgress`. Always returns
the P0 baseline. When the sample is in `seq_ops_tracking_per_sample_mirror`, it
adds the ordered `milestones` (each `reached_at` + `duration_to_next`),
`current_milestone` = latest reached milestone whose successor is NULL, and sets
`detailed_timeline=true`. Otherwise `detailed_timeline=false` with a
`timeline_reason` (e.g. "not in tracking window") -- never an error. For each of
the sample's runs, embeds the F2 `RunStatusTimeline` (same type, no drift).
`cache_synced_at` = oldest `last_run` across feeding tables (tracking +
`iseq_run_status` + product-metrics + iRODS). The open/current phase returns the
timestamp for the caller to compute elapsed (no server "now" subtraction).
`Description` pins the 9 milestone names and 3 baseline phases, the QC mapping
and roll-up and that overall `qc` is authoritative, and the `reached_at` vs
`entered_at` distinction.

**Package:** `mlwh/`
**File:** `mlwh/progress.go`
**Test file:** `mlwh/progress_test.go`

**Method signature:**
`SampleProgress(ctx, sangerName string) (SampleProgress, error)`

**Acceptance tests:**

1. Given a sample present in the tracking mirror with milestones filled
   `manifest_created`..`sequencing_run_start` and `sequencing_qc_complete` NULL,
   when `SampleProgress` is called, then `detailed_timeline=true`, `milestones`
   are in canonical order, each non-final `duration_to_next` = delta to the next
   reached milestone, and `current_milestone == "sequencing_run_start"`.
2. Given a sample ABSENT from the tracking mirror but with product-metrics and
   iRODS rows, when called, then `detailed_timeline=false` with a non-empty
   `timeline_reason`, and the P0 baseline (`baseline_phase == "delivered"`,
   `qc`, `delivered_at`) is still returned (less detail, not an error).
3. Given a tracked sample with one Illumina run carrying `iseq_run_status` rows,
   when called, then `runs` contains one `RunStatusTimeline` equal to
   `RunStatus(thatRun)` (same events + `current`).
4. Given the ONT sample, when called, then it resolves identity + study,
   `platforms == ["ONT"]`, `qc == "not_tracked"`, `runs` empty, and (if outside
   tracking) `detailed_timeline=false` -- never a bare "no data".
5. Given a tracked sample whose `library_complete` is set but
   `sequencing_run_start` NULL, then `current_milestone == "library_complete"`
   and the `library_start`->`library_complete` `duration_to_next` is the delta.
6. Given an unknown Sanger name on a synced cache, then `ErrNotFound`; on a
   never-synced cache, `ErrCacheNeverSynced`+`ErrNotFound`.

### F4: study status-breakdown rollup (P3)

As a study owner, I want counts of all samples by baseline phase, so I see study
progress without per-sample fan-out.

`GET /study/:id/status-breakdown` -> `StatusBreakdown`. `distinct` is the
distinct-sample partition (most-advanced phase, sums to `samples_total`).
`per_platform` is the per-platform partition (each platform's buckets sum to its
sample count; grand total may exceed `samples_total`).
`with_detailed_timeline` = count of the study's samples also present in the
tracking mirror. One small grouped query per partition -- never N per-sample
lookups. Samples with no product-metrics (incl. ONT) are `registered` (+ "not
tracked for delivery"), never folded into without-data. `Description` pins the
ladder enum, the two denominators, and the freshness caveat.

**Package:** `mlwh/`
**File:** `mlwh/progress.go`
**Test file:** `mlwh/progress_test.go`

**Method signature:**
`StatusBreakdown(ctx, studyLimsID string) (StatusBreakdown, error)`

**Acceptance tests:**

1. Given study `S1` (3 delivered, 1 sequenced-no-data, 1 registered; 2 of them
   in the tracking mirror), when `StatusBreakdown("S1")` is called, then
   `distinct == {with_data:3, sequenced_no_data:1, registered:1}` summing to 5,
   and `with_detailed_timeline == 2`.
2. Given a multi-platform sample in `S1` delivered on Illumina but only
   sequenced on PacBio, when called, then in `per_platform` it counts under
   Illumina `with_data` AND under PacBio `sequenced_no_data` (grand total > 5),
   while in `distinct` it counts once under `with_data` (most-advanced).
3. Given the ONT sample linked to `S1`, when called, then it is counted in
   `registered` (not without-data as a separate negative), and the `distinct`
   buckets still sum to `samples_total`.
4. Given a never-synced cache, then `ErrCacheNeverSynced`+`ErrNotFound`; an
   unknown study -> `ErrNotFound`; a synced empty study -> all-zero ladders.

## G. Registry, remote, docs (wiring; HARD REQ 4, 5, 6)

### G1: Registry entries + handler cases + remote methods

As an implementor, I want every new endpoint wired through the four-step recipe,
so local and remote surfaces stay aligned and self-describing.

For each new endpoint: add the `Queryer` member, the `Client` method, the
`RemoteClient` method (and `Page[T]` variant for new paginated lists), the
`Registry` `Endpoint` (Summary + verbatim-for-MCP Description), and the
`server.go` handler `case`. Each Description states, as applicable: the precise
"available" definition, recency/window semantics + parameters, the study-scoping
rule, platform-qualification (and ONT "not tracked"), the QC string mapping +
authoritative field + roll-up, the run-id space, and the freshness caveat.

**Package:** `mlwh/`
**Files:** `mlwh/registry.go`, `mlwh/queryer.go`, `mlwh/server.go`,
`mlwh/remote.go`
**Test file:** `mlwh/registry_test.go`, `mlwh/server_test.go`,
`mlwh/remote_test.go`

**Acceptance tests:**

1. Given the Registry, when iterated, then every new Method has a non-empty
   `Summary` and `Description`, and every new paginated entry declares
   limit/offset `QueryParams`.
2. Given each new endpoint, when its `server.go` handler runs against a seeded
   cache, then it returns the same value as the `Client` method (the switch has
   a case for every new Method; no panic).
3. Given each new endpoint, when called via `RemoteClient` against a test
   server, then it round-trips to the same typed result as the local `Client`.
4. Given each new windowed/recency Description, when inspected, then it states
   that "added since" filters on the iRODS creation timestamp and never on
   `last_updated` or `last_run`.

### G2: regenerate docs; drift guards green; glossary updated

As a maintainer, I want the generated docs refreshed and the glossary extended,
so the MCP surface and drift guards stay correct.

Run `WA_REFRESH_DOCS=1 go test ./mlwh -run TestWriteEndpointReference` to
rewrite `.docs/mcp/api-reference.md`. Add glossary entries for "sequencing data
available", "added to iRODS", "baseline phase", "detailed timeline", "platform".

**Package:** `mlwh/`
**Files:** `.docs/mcp/api-reference.md`, `.docs/mcp/glossary.md`
**Test file:** `mlwh/docs_test.go`

**Acceptance tests:**

1. Given the regenerated reference and the OpenAPI document, when
   `TestEndpointReferenceAndOpenAPICoverSamePathsG1` runs, then both cover the
   same set of Registry paths (no drift).
2. Given the committed reference, when compared to `EndpointReference()`, then
   they match (no-drift guard passes after regeneration).
3. Given the glossary, when inspected, then it defines "sequencing data
   available" and "added to iRODS".

## Implementation Order

Each phase builds on tested foundations from prior phases.

1. **Phase 1 -- Schema + sync foundation (A1-A6).** iRODS mirror `created` +
   `platform` + index, both dialects; iRODS source SELECTs + per-platform UNION
   linkage; nullable QC; new platform-coverage / tracking / run-status mirrors +
   their sync strategies; freshness surface extension. Sequential (everything
   downstream reads these tables). Bump `CacheSchemaVersion`.
2. **Phase 2 -- Availability (B1-B3) + Recency (C1-C2).** Overview,
   samples-with/without-data counts+lists, iRODS-row identity, windowed
   count/list. Depends on Phase 1. B and C share the same membership SQL.
3. **Phase 3 -- Run overview (D1) + Budget-safety (E1-E3).** Run overview,
   `/count` counterparts, sizing headers + `Page[T]`, lean/de-dup detail.
   Depends on Phase 1; independent of Phase 2 (parallel after Phase 1).
4. **Phase 4 -- Progress (F1-F4).** P0 baseline, run-status timeline, unified
   progress endpoint, status-breakdown. Depends on Phase 1 (and reuses the
   availability SQL from Phase 2 for the delivered phase / ladder).
5. **Phase 5 -- Wiring + docs (G1-G2).** Registry/handler/remote wiring is done
   incrementally per endpoint within Phases 2-4; this phase is the final doc
   regeneration, glossary update, and drift-guard verification across all new
   endpoints.

## Appendix: Key Decisions

- **Why headers, not an envelope, for M.** Keeps the bare-slice body contract,
  existing tests, OpenAPI schemas and the MCP surface unchanged; Go consumers
  read sizing via typed `Page[T]` variants. Settled in the prompt Notes.
- **Why `seq_platform_name` for platform, not the matched metrics table.** The
  source column is authoritative and unambiguous; deriving from the matched
  table can contradict the source and is harder to seed. Illumina keeps its
  composition-expansion join unchanged to preserve current `/study/:id/irods`.
- **Why full-refresh atomic-swap for the tracking table.** It has no
  `last_changed`, mutates in place, and ~55% of rows have NULL
  `manifest_created`; a `GREATEST(milestones)` watermark misses in-place and
  backfilled fills. Its lag is shown honestly via the freshness caveat.
- **Why `created` not `last_updated` for recency.** `last_updated` (source
  `last_changed`) conflates newly-added with later-modified rows; `last_run` is
  when wa synced. Only `created` answers "added to iRODS since X". Every recency
  Description must say so (HARD REQ 3).
- **Two denominators (P3 vs overview/lists).** Per-platform partition shows each
  platform's true state (grand total may exceed `samples_total`); the
  distinct-sample partition collapses multi-platform samples to their
  most-advanced phase (sums to `samples_total`). Tests assert both.
- **`reached_at` vs `entered_at`.** Deliberately distinct: a milestone is
  _reached_; a run lifecycle phase is _entered_. Value semantics identical
  (RFC3339 + duration-to-next + open-phase handling); only the name differs.
- **Closed enums vs open vocabulary.** The 9 milestones and 3 baseline phases
  are closed (asserted verbatim, pinned in Descriptions); the within-sequencing
  status vocabulary is an open dict/source pass-through (tested as pass-through,
  not a frozen list, so new dict rows do not break it).
- **Freshness caveat = oldest `last_run` across feeding tables.** Each
  availability/recency/progress response surfaces `cache_synced_at` as the
  oldest `last_run` of the tables that fed it, distinct from any data timestamp.

### Testing strategy

- Hermetic GoConvey over the ephemeral SQLite cache (`openSQLiteSyncTestCache`),
  seeded via the existing helpers (`seedHierarchyStudy`, `seedHierarchySample`,
  `seedLibrarySample`, `seedIseqProductMetricsMirrorRow`,
  `seedIRODSLocationMirrorRow`, `seedSyncStateRun`/`seedSyncState`) extended for
  the new columns (`created`, `platform`, nullable QC) and the new mirrors
  (tracking, run-status, per-platform incl. a PacBio and an ONT seeder).
- Reuse the count<->list cross-check pattern (count == len(list-all)) for every
  new count.
- The HARD REQ 7 scenario seed (one study): samples with and without iRODS rows;
    > =2 runs/tags; iRODS rows with differing `created` (inside and outside the
    > window, plus on-boundary); >=1 sample shared with a second study (scoping);
    > tracking-mirror samples filled to different milestones (library prep,
    > sequencing, qc-complete) and samples absent from it but with product-metrics/
    > iRODS; `iseq_run_status` rows across several phases incl. a recurrence and a
    > derived-current; >=1 PacBio sample (iRODS via `pac_bio_product_metrics`) and
    > =1 ONT sample (`oseq_flowcell`, no iRODS); and a multi-platform sample.
- Schema tests assert both dialects' new columns/indexes and that the dialects
  compare equal (existing cross-dialect shape test).
- Never put `So()` in loops > 20 iterations; count and assert the final count.

### Implementor / reviewer references

- Follow **go-conventions** (copyright header, modern Go, GoConvey mechanics,
  four-step add-a-query recipe, `id_lims = 'SQSCP'` invariant) and
  **testing-principles** (behaviour-focused; supported boundaries: HTTP
  contract, typed `Client`/`RemoteClient` results, persisted mirror state,
  generated docs).
- Every spec acceptance test MUST have a corresponding GoConvey test -- no
  stubs, no hardcoded results, no swallowed failures, no build-tag exclusions.
