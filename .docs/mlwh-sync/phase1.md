# Phase 1: Schema, types, and migration

Ref: [spec.md](spec.md) sections A1, A2, A3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Batch 1 (parallel)

#### Item 1.1: Types - public type changes [parallel with 1.2]

spec.md section: Architecture / Public types (Phase 1 bullet 4)

Update `mlwh/types.go` (and `mlwh/mlwh.go` as needed) to drop
`Sample.IDStudyLims`, `Sample.LibraryType`, and `Sample.SangerID`;
add `Sample.Studies []Study` and `Sample.Libraries []Library`;
introduce the `Library` struct with `PipelineIDLims` and
`IDStudyLims`; declare `ErrCacheNeverSynced` per the spec wording.
No acceptance tests of its own; this item is the type foundation
that every later item compiles against.

- [ ] implemented
- [ ] reviewed

#### Item 1.2: A1 - Schema version bump and OpenCache migration [parallel with 1.1]

spec.md section: A1

Bump `CacheSchemaVersion` from 1 to 2. Author the new embedded DDL
under `mlwh/cache_schema/{sqlite,mysql}/` for `sample_mirror`,
`study_mirror`, `library_samples`, `donor_samples`,
`iseq_product_metrics_mirror`,
`seq_product_irods_locations_mirror`, `sync_state`,
`schema_version`, `sync_lock` per the per-table audit. Make
`OpenCache` the sole migrator: drop affected tables (including
removed `negative_cache`, `enrich_cache`, `watermarks`),
recreate from embedded DDL, delete matching `sync_state` rows,
update `schema_version`, and emit the single alphabetised stderr
migration line. Covers all 4 acceptance tests from A1 (test 4
exercises the MySQL path under the existing MySQL cache gate).

- [ ] implemented
- [ ] reviewed

### Batch 2 (parallel, after batch 1 is reviewed)

#### Item 1.3: A2 - Per-dialect schema parity [parallel with 1.4]

spec.md section: A2

Extend `mlwh/cache_schema_test.go` (and the shape parser) to
assert SQLite/MySQL parity for the v2 table set: identical table
names, column sets, index column-lists, and unique-constraint
column tuples. Covers all 4 acceptance tests from A2.

- [ ] implemented
- [ ] reviewed

#### Item 1.4: A3 - Case-insensitive text columns [parallel with 1.3]

spec.md section: A3

Apply `COLLATE NOCASE` (SQLite) and `utf8mb4_0900_ai_ci` (MySQL,
with `utf8mb4_general_ci` fallback for MySQL < 8) to the
collation sets called out per table in the spec. Add tests
proving case-insensitive equality on `sample_mirror.name`,
`study_mirror.accession_number`, and
`library_samples.pipeline_id_lims` in both backends. Covers all
4 acceptance tests from A3.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).
