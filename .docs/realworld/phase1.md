# Phase 1: Schema + sync foundation (A1-A6)

Ref: [spec.md](spec.md) sections A1, A2, A3, A4, A5, A6

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both.

This is the foundation phase: every downstream phase reads these tables,
so correctness here is load-bearing. The schema change bumps
`CacheSchemaVersion` and triggers a full resync that simultaneously
backfills `created`, `platform`, and the now-nullable QC columns (A1,
A2, A3 share one resync). Do NOT run a live warehouse or a full test
suite; rely on the hermetic GoConvey suite over the ephemeral SQLite
cache (`openSQLiteSyncTestCache`) and the cross-dialect schema-shape
tests.

## Items

### Batch 1 (parallel)

The two schema tracks touch disjoint `.sql` files and disjoint registry
lists, so they can be implemented concurrently. Both must land before
the sync work in Batch 2.

#### Item 1.1: A1 - iRODS mirror gains created + platform, both dialects [parallel with 1.2]

spec.md section: A1

Add `created` and `platform` columns plus the
`spi_mirror_study_lims_created_idx (id_study_lims, created)` index to
`seq_product_irods_locations_mirror` in both
`cache_schema/sqlite/seq_product_irods_locations_mirror.sql` and
`cache_schema/mysql/seq_product_irods_locations_mirror.sql`. Bump
`CacheSchemaVersion` in `cache.go`. Keep the table in
`cacheMigrationRecreateTables`/`cacheMigrationDropTables`
(`cache_schema.go`) so migration recreates it. Covering all 3
acceptance tests from A1 (sqlite shape, mysql shape + cross-dialect
equality, ephemeral insert/read-back).

- [ ] implemented
- [ ] reviewed

#### Item 1.2: A4 - new platform-coverage mirror tables [parallel with 1.1]

spec.md section: A4

Add CREATE TABLE + indexes in both dialects and register each in
`schemaStatementOrder` and the migration lists (`cache_schema.go`) for:
`pac_bio_product_metrics_mirror`, `pac_bio_run_well_metrics_mirror`,
`eseq_product_metrics_mirror`, `eseq_run`/`eseq_run_lane_metrics`
mirrors, `useq_product_metrics_mirror`, `useq_run_metrics_mirror`,
`oseq_flowcell_mirror` (ONT identity only), `iseq_run_status_mirror`,
`iseq_run_status_dict_mirror`, and `seq_ops_tracking_per_sample_mirror`
(all 9 milestone datetime columns + lookup/context columns, indexed by
`id_sample_lims`, `sanger_sample_name`, `study_id`). Per-platform
product-metrics mirrors carry nullable QC. Covering all 3 acceptance
tests from A4 (cross-dialect existence + equality, ephemeral
insert/read-back per table, tracking mirror has all 9 milestone columns
and its three indexes).

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

### Batch 2 (sequential, after batch 1 is reviewed)

The sync work depends on the schema columns/tables from Batch 1.

#### Item 1.3: A3 - nullable QC across all platforms, NULL preserved as pending

spec.md section: A3

Make `qc`, `qc_seq`, `qc_lib` nullable in both dialects on every
product-metrics mirror that carries QC (Illumina, PacBio, Elembio,
Ultimagen) in `cache_schema/{sqlite,mysql}/*_product_metrics_mirror.sql`
and stop coercing NULL->0 in the sync scan/insert (`mlwh/sync.go`). This
is folded into the same full resync as A2. Covering all 3 acceptance
tests from A3 (nullable in both dialects, NULL stored not 0 and mapped
to `pending`, qc 1/0/NULL read back as pass/fail/pending). Depends on
1.1/1.2 schema.

- [ ] implemented
- [ ] reviewed

#### Item 1.4: A2 - iRODS sync mirrors created and platform across all platforms

spec.md section: A2

Add `spi.created` and `spi.seq_platform_name` to all six iRODS source
query funcs (composition-expansion + legacy x initial/cold/cursor) and
replace the Illumina-only sample/study recovery with a UNION over every
platform's `*_product_metrics` keyed on `spi.id_product` (Illumina
join UNCHANGED; PacBio via `pac_bio_run`; Elembio via `eseq_flowcell`;
Ultimagen via `useq_wafer`). The UNION recovers only
`id_sample_tmp`/`id_study_lims`; `platform` always comes from
`spi.seq_platform_name`. Extend `seqProductIRODSLocationsSyncRow`
(`Created time.Time`, `Platform string`), its scan func, and the
mirror columns/row-args (format `Created` via `formatSyncTime`). All in
`mlwh/sync.go`. Covering all 3 acceptance tests from A2 (Illumina row
stores created+platform; PacBio row not dropped and platform from
seq_platform_name; platform not derived from matched table). Depends on
1.1 (mirror columns) and 1.2 (per-platform metrics mirrors for the
UNION joins).

- [ ] implemented
- [ ] reviewed

#### Item 1.5: A5 - sync strategies for the new tables

spec.md section: A5

In `mlwh/sync.go`: sync `iseq_run_status` in ascending-id mode on the
`id_run_status` PK (cf. `seqProductIRODSLocationsIDMode`, no
`last_changed`); mirror `iseq_run_status_dict`, `oseq_flowcell`, and the
per-platform status/dict tables wholesale / by available key; sync
`seq_ops_tracking_per_sample` by full-table refresh with build-and-
atomic-swap (`high_water` = refresh time, `last_run` = sync time, own
cadence); per-platform `*_product_metrics` incremental tables follow the
existing `last_changed` precedent. Covering all 3 acceptance tests from
A5 (ascending-id read order for run-status; atomic full-refresh swap for
tracking with old rows gone and no partial-table read; tracking
sync_state high_water=refresh, last_run=sync). Depends on 1.2 (target
mirror tables).

- [ ] implemented
- [ ] reviewed

#### Item 1.6: A6 - freshness surface includes every new mirror

spec.md section: A6

In `mlwh/freshness.go`: append every new sync table to
`freshnessSyncTables` (order: existing 5, then the new ones). Make
`HighWater` RFC3339-or-empty by sync mode (refresh time for full-refresh
tracking, latest `last_changed` for incremental, empty for ascending-id
`iseq_run_status`); `last_run` universal. Update the
`len(...Tables) == 5` assertions to the new total. Covering all 3
acceptance tests from A6 (never-synced returns one entry per table with
ever_synced=false and empty timestamps and no error; tracking
high_water=refresh and last_run=sync; iseq_run_status high_water empty
and last_run set). Depends on 1.2/1.5 (the new tables and their sync
modes).

- [ ] implemented
- [ ] reviewed

## Ordering and dependency notes

- This phase is the foundation for all later phases; nothing in Phases
  2-5 should start until Phase 1 is fully reviewed.
- Batch 1 (A1 schema, A4 schema) is parallel; Batch 2 (A3, A2, A5, A6)
  is sequential and depends on Batch 1's schema. Within Batch 2, A2/A3
  share the single full resync, A5 wires the new tables' sync strategies,
  and A6 surfaces them in freshness.
- The single schema bump in A1 (plus the new tables in A4) drives one
  full resync covering A1's `created`, A2's `platform`/linkage, and A3's
  nullable QC. Reviewers should confirm the resync is not split into
  multiple version bumps.
