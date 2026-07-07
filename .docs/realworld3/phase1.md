# Phase 1: Foundation (A1-A9)

Ref: [spec.md](spec.md) sections A1, A2, A3, A4, A5, A6, A7, A8, A9

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both.

This is the load-bearing foundation: every later phase reads these
tables, columns, and indexes. Keep both SQL dialects
(`cache_schema/sqlite/*.sql` and `cache_schema/mysql/*.sql`) in parity -
the cross-dialect shape test compares them. Bump `CacheSchemaVersion` 12
-> 13 exactly ONCE (A8); the single bump drives the existing
recreate-tables migration (a FULL resync) that creates every new
mirror/column/index below - do NOT take an additive no-version-bump path
and do NOT split the bump. Preserve the `id_lims = 'SQSCP'` invariant in
all new sync selection.

Two code-authority corrections scope this phase (verified against the Go
code; see spec "Code-authority corrections"): (1) A2 only changes the
`iseq_product_metrics_mirror.id_iseq_product` column TYPE to `char(64)`
(MySQL) and drops the redundant `ipm_mirror_iseq_product_idx` - the
PRIMARY KEY already exists, so do NOT "add a PRIMARY KEY". (2) The
study-scoped iRODS listing already attributes `id_sample_tmp`/`name` for
merged CRAMs, so nothing here re-fixes that attribution.

Do NOT run a live warehouse or the full test suite; rely on the hermetic
GoConvey suite over the ephemeral SQLite cache and the cross-dialect
schema-shape tests. MySQL-only assertions (EXPLAIN, `char(64)`) belong in
the throwaway-DB integration tests (`cache_mysql_integration_test.go`,
`sync_source_integration_test.go`), skipped without creds.

## Items

### Batch 1 (parallel)

These three touch disjoint files (two schema-only, one source-test-only)
with no shared sync logic, so they can be implemented concurrently. A2
must land before A4 (both edit
`seq_product_irods_locations_mirror.sql`), so it belongs before Batch 2.

#### Item 1.1: A2 - id_iseq_product char(64) + drop redundant index [parallel with 1.2, 1.3]

spec.md section: A2

Change `id_iseq_product` to `CHAR(64) NOT NULL PRIMARY KEY` (MySQL; was
`VARCHAR(255)`) in `cache_schema/mysql/iseq_product_metrics_mirror.sql`
(SQLite keeps `TEXT ... PRIMARY KEY`), align
`cache_schema/mysql/seq_product_irods_locations_mirror.sql`
`id_iseq_product` to `CHAR(64)` so the iRODS<->product join is
fixed-width both sides, and drop the redundant
`ipm_mirror_iseq_product_idx` (it duplicates the PK). The PRIMARY KEY
already exists - only the type changes. Covering both acceptance tests
from A2 (a MySQL describe shows `char(64)` PK and no separate
`ipm_mirror_iseq_product_idx`; EXPLAIN uses the PK for the join with no
implicit collation conversion).

- [ ] implemented
- [ ] reviewed

#### Item 1.2: A7 - study_mirror.programme index [parallel with 1.1, 1.3]

spec.md section: A7

Add `study_mirror_programme_idx ON study_mirror(programme)` to
`cache_schema/{sqlite,mysql}/study_mirror.sql` (both dialects, in parity)
so the D7 exact filter/group-by is index-served. `programme` already
exists as a column, so this is index-only (no new column). Covering the
single acceptance test from A7 (both dialects load with a `programme`
index, in parity).

- [ ] implemented
- [ ] reviewed

#### Item 1.3: A9 - source integration test for new source columns/tables [parallel with 1.1, 1.2]

spec.md section: A9

Extend `mlwh/sync_source_integration_test.go` (throwaway/skip-without-
creds pattern) to assert the SOURCE schema these features assume stays
true: `iseq_flowcell.entity_type`/`pipeline_id_lims`;
`{eseq,useq}_product_metrics.is_sequencing_control`;
`{iseq,eseq,useq,pac_bio}_product_metrics.qc`; run dates incl.
`iseq_run_status` `run complete`/`run archived` and
`oseq_flowcell.last_updated`; `study_users` role rows; `study.programme`;
and a merged/composite iRODS object's `id_sample_tmp`/`id_study_lims`
linkage. Reads the source only, so it is independent of the mirror
changes. Covering the single acceptance test from A9 (with creds each
asserted column/table exists with the expected type and the merged-object
linkage is present; without creds it skips).

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

### Batch 2 (sequential, after batch 1 is reviewed)

The sync work shares `mlwh/sync.go` and has real ordering dependencies,
so these items are sequential. Order matters: A1 and A3 both feed the A4
denormalisation, and A8's version bump plus cold-load read-index set must
be last (it references the A1/A4/A6 indexes).

#### Item 1.4: A1 - iseq_flowcell_mirror + eseq/useq is_sequencing_control + sync

spec.md section: A1

Add new mirror `iseq_flowcell_mirror` (both dialects, in parity) with
columns `id_iseq_flowcell_tmp` (PK), `entity_type`, `pipeline_id_lims`,
`id_sample_tmp`, `id_study_tmp`, an index on `(entity_type)`, and the PK
for the product join; register it in `cache_schema.go`
(`schemaStatementOrder`, recreate/drop lists, sync-state tables). Add a
nullable `is_sequencing_control INT` column to BOTH
`eseq_product_metrics_mirror` and `useq_product_metrics_mirror` (both
dialects, in parity). Add the `iseq_flowcell` wholesale source arm
(`syncTableIseqFlowcell`) and the Element/Ultima
`is_sequencing_control` source selection in `sync.go` /
`sync_platform_coverage.go`. Covering all 3 acceptance tests from A1
(iseq_flowcell_mirror shape/PK/entity_type index in dialect parity;
entity_type rows mirrored; is_sequencing_control on both product mirrors
in parity and populated from source).

- [ ] implemented
- [ ] reviewed

#### Item 1.5: A3 - composite (merged) product rows in iseq_product_metrics_mirror

spec.md section: A3

Extend `sync.go` / `sync_platform_coverage.go` source selection to ALSO
mirror composite/merged `iseq_product_metrics` rows (those whose
`id_iseq_product` matches a composite iRODS object), not only single-lane
rows, so a composite carries its own `id_run`/`qc`/`id_iseq_flowcell_tmp`.
A composite spanning several lanes of one run carries that run's `id_run`;
one spanning runs carries `id_run` 0. Do NOT mirror
`iseq_composition`/component structure. Covering both acceptance tests
from A3 (the study-7568 `49348_1-2#1.cram` composite id_iseq_product is
present with its `qc` and `id_iseq_flowcell_tmp`; the export
iRODS<->product join matches the composite so `manual_qc` is not empty).
Depends on 1.1 (the char(64) join key).

- [ ] implemented
- [ ] reviewed

#### Item 1.6: A4 - denormalised export columns on seq_product_irods_locations_mirror + covering index

spec.md section: A4

Add `id_run`, `position`, `tag_index`, `qc` (raw 1/0/NULL),
`is_deliverable` (tinyint, NULLABLE tri-state), and `merged` (tinyint)
columns to `cache_schema/{sqlite,mysql}/
seq_product_irods_locations_mirror.sql` (`created` already mirrored), and
populate them at sync PER PLATFORM by resolving each iRODS row's
`id_iseq_product` against the platform-appropriate product mirror by its
PK (Illumina+composite via `iseq_product_metrics_mirror` then
`iseq_flowcell_mirror` for `entity_type`; Element/Ultima via their product
mirror + `is_sequencing_control`; PacBio `qc` only, is_deliverable NULL;
ONT both NULL). `is_deliverable` is a tri-state: 1=deliverable, 0=known
control/spike, NULL=no discriminator (PacBio/ONT). Add the covering index
`(id_study_lims, id_run, position, tag_index,
id_seq_product_irods_locations_tmp)`, keep `(id_study_lims, created)`, and
add `(id_sample_tmp, created)` and `(id_run, created)`. Covering all 4
acceptance tests from A4 (columns + covering + recency indexes in parity;
MySQL EXPLAIN of the study export scan is an index range scan, no
filesort; merged row has merged=1/id_run=0/correct id_sample_tmp; Element/
Ultima/PacBio/ONT rows carry the specified qc/is_deliverable values).
Depends on 1.1 (char(64) join), 1.4 (iseq_flowcell + is_sequencing_control
sources), and 1.5 (composite product rows).

- [ ] implemented
- [ ] reviewed

#### Item 1.7: A5 - common_name_word_mirror organism vocabulary

spec.md section: A5

Add new helper mirror `common_name_word_mirror` (both dialects, in parity)
with rows `(word, common_name)` - one per distinct word per distinct
`common_name` - built at sync by tokenising the ~16,234 distinct
`common_name` values with the same tokeniser as `sample_search_token`
(lowercased `[a-z0-9]` runs); index `(word)` (~40k rows). Wire the build
into `sync.go`. Covering the single acceptance test from A5 (word
`musculus` maps to both `Mus Musculus` and `Mus musculus castaneus`, not
`Homo sapiens`, and no row has the mid-word fragment `usculus`).

- [ ] implemented
- [ ] reviewed

#### Item 1.8: A6 - ONT run identity + normalised run dates + run-aggregation indexes

spec.md section: A6

Extend `oseq_flowcell_mirror` with `experiment_name`, `run_id` (NULL for
all rows), `run_uuid`, `last_updated`, indexed on `(experiment_name)` and
`(last_updated)`. Add indexed, sync-derived normalised run-date columns so
monthly grouping stays index-served: `iseq_run_status_mirror` (normalised
`date` + `run complete`/`run archived` status filtering),
`pac_bio_run_well_metrics_mirror.run_complete`,
`oseq_flowcell_mirror.last_updated`, plus a `(normalised_date)` (or
month-friendly) index per source. Ensure Element/Ultima `id_run`s are in
`iseq_run_status_mirror` (else fall back to the platform run mirrors'
`run_complete`/`run_archived`). Files include
`cache_schema/{sqlite,mysql}/oseq_flowcell_mirror.sql`,
`iseq_run_status_mirror.sql`, `pac_bio_run_well_metrics_mirror.sql`,
`useq_run_metrics_mirror.sql`, and `sync.go`. Covering both acceptance
tests from A6 (ONT experiment_name/last_updated carried, run_id NULL;
EXPLAIN of the monthly run count uses a normalised-date index per
platform, no full scan).

- [ ] implemented
- [ ] reviewed

#### Item 1.9: A8 - version bump, migration, cold-load read-index set

spec.md section: A8

Bump `CacheSchemaVersion` 12 -> 13 (`cache.go`) and `APIVersion` ->
`"1.8.0"` (`openapi.go`); the single bump drives the existing recreate-
tables migration (full resync). Add the large new/changed mirrors' read-
critical indexes (the A4 iRODS covering index, the A1 `iseq_flowcell` PK,
the A6 run-date indexes) to the sparse cold-load read-index set in
`cache.go`/`sync.go` so cold reads are index-served. Covering both
acceptance tests from A8 (a v12 cache opened by v13 code triggers the
recreate migration reporting `12 -> 13`; the generated OpenAPI doc has
`info.version == "1.8.0"`). MUST be last: it references the A1/A4/A6
indexes and the migration lists they register. Depends on 1.4, 1.6, 1.8.

- [ ] implemented
- [ ] reviewed

For sequential items, a single review pass after each item (or one pass
over Batch 2) is acceptable; reviewers must confirm the schema change is
ONE version bump driving ONE full resync (A8), both dialects stay in
parity, and A2 only re-types `id_iseq_product` (no new PRIMARY KEY).

## Ordering and dependency notes

- This phase is the foundation for all later phases; nothing in Phases
  2-10 should start until Phase 1 is fully reviewed.
- Batch 1 (A2 type change, A7 index, A9 source test) is parallel over
  disjoint files. A2 precedes A4 because both edit
  `seq_product_irods_locations_mirror.sql`.
- Batch 2 is sequential (shared `sync.go` + real dependencies): A1 (new
  flowcell mirror + is_sequencing_control) and A3 (composite product rows)
  both feed A4 (the denormalised iRODS export columns); A5 (organism
  vocabulary) and A6 (run identity/dates) are logically independent but
  serialised on `sync.go`; A8 (version bump + cold-load read-index set)
  is last because it references the A1/A4/A6 indexes.
- A single bump 12 -> 13 drives one full resync; reviewers should confirm
  it is not split into multiple bumps and that the additive no-version-
  bump path is NOT used.
- Per-endpoint Registry/handler/RemoteClient/Queryer wiring and the CLI
  are consolidated in Phase 10 (J, K), except where a feature phase names
  those files explicitly (e.g. D1c counts in Phase 4).
