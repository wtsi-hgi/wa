# Phase 1: Schema, sync, and migration foundation (A1-A5)

Ref: [spec.md](spec.md) sections A1, A2, A3, A4, A5

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both.

This is the foundation phase: every downstream phase reads these tables
and indexes, so correctness here is load-bearing. The schema change adds
`study_users_mirror` plus new indexes and bumps `CacheSchemaVersion` 10
-> 12, which triggers the recreate-tables migration (a FULL resync) -- do
NOT take the additive `IF NOT EXISTS` no-version-bump path. Keep both
SQL dialects in parity (the cross-dialect shape test compares them) and
preserve the `id_lims = 'SQSCP'` invariant in the new wholesale sync. Do
NOT run a live warehouse or the full test suite; rely on the hermetic
GoConvey suite over the ephemeral SQLite cache
(`openSQLiteSyncTestCache`) and the cross-dialect schema-shape tests.

## Items

### Batch 1 (parallel)

The two schema tracks touch disjoint `.sql` files and disjoint registry
lists, so they can be implemented concurrently. Both must land before
the sync work in Batch 2.

#### Item 1.1: A1 - study_users_mirror table, both dialects [parallel with 1.2]

spec.md section: A1

Add `cache_schema/sqlite/study_users_mirror.sql` and
`cache_schema/mysql/study_users_mirror.sql` (mirror the
`oseq_flowcell_mirror.sql` style; mysql uses `{{MYSQL_TEXT_COLLATION}}`,
sqlite uses `COLLATE NOCASE` on `login`/`email`/`name`) with columns
`id_study_users_tmp` (INTEGER PK), `id_study_tmp`, `role`, `login`,
`email`, `name`, `last_updated`, and the five declared indexes
(`..._id_study_tmp_idx`, `..._login_idx`, `..._email_idx`,
`..._name_idx`, `..._role_idx`). Register `study_users_mirror` in
`schemaStatementOrder`, `cacheMigrationRecreateTables`, and
`cacheMigrationDropTables`, and add the sync table name to
`cacheMigrationSyncStateTables` (`cache_schema.go`). Covering all 3
acceptance tests from A1 (sqlite shape with the 7 columns + 5 indexes;
mysql shape + cross-dialect equality; ephemeral insert/read-back of the
column list).

- [x] implemented
- [x] reviewed

#### Item 1.2: A2 - faculty_sponsor index + iRODS id_iseq_product index, both dialects [parallel with 1.1]

spec.md section: A2

Add `study_mirror_faculty_sponsor_idx (faculty_sponsor)` to
`cache_schema/{sqlite,mysql}/study_mirror.sql` (backs the D4
faculty-sponsor lookup and the resolve-person `GROUP BY faculty_sponsor`
enumeration), and `spi_mirror_iseq_product_idx (id_iseq_product)` to
`cache_schema/{sqlite,mysql}/seq_product_irods_locations_mirror.sql`
(backs the D1 run-scoped iRODS join and the D2 manifest per-product
iRODS LEFT JOIN over the ~9M-row iRODS mirror). Covering both acceptance
tests from A2 (both schemas have the two new indexes in both dialects and
compare equal; the existing cross-dialect shape test still passes).

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

### Batch 2 (sequential, after batch 1 is reviewed)

The sync, sparse-read, and migration work depends on the schema
tables/indexes from Batch 1.

#### Item 1.3: A3 - study_users wholesale sync

spec.md section: A3

Add `syncTableStudyUsers = "study_users"` (`sync.go`) and place it in
`supportedSyncTables` (`sync.go`) and `freshnessSyncTables`
(`freshness.go`) in the same position. Add `studyUsersWholesaleSpec()`
returning a `wholesaleMirrorSpec` in the `oseqFlowcellWholesaleSpec`
pattern: `mirrorColumns` = the 7 columns; `sourceQuery` INNER JOINs
`study_users su` to `study ON study.id_study_tmp = su.id_study_tmp AND
study.id_lims = 'SQSCP'` ordered by `su.id_study_users_tmp` (so only
SQSCP-study rows mirror and the `study_mirror.id_study_tmp` link always
resolves); `scan` reads the 7 columns with nullable text
(`sql.NullString` -> `''`) and skips a row whose `id_study_tmp` is 0.
Add `syncTableStudyUsers` to `wholesaleMirrorTables()`
(`sync_platform_coverage.go`) and a `case` to `wholesaleMirrorSpecFor`
and the `syncTableData` wholesale `case` group. Covering all 4 acceptance
tests from A3 (rows mirrored with a NULL email stored as `''` and correct
id_study_tmp/role/login/name; a non-SQSCP study row dropped by the INNER
JOIN; never-synced `Freshness` returns a `study_users` entry with
`ever_synced=false`/empty timestamps/no error; a `study_users` sync_state
row reported with its `last_run`). Depends on 1.1 (the mirror table) and
the existing `study_mirror.id_study_tmp` link key.

- [x] implemented
- [x] reviewed

#### Item 1.4: A5 - sparse cold-load read-index decisions

spec.md section: A5

In `mlwh/sync.go`: add `spi_mirror_iseq_product_idx (id_iseq_product)`
(from 1.2) to BOTH the `seq_product_irods_locations_mirror` full
secondary-index set AND `seqProductIRODSLocationsMirrorReadIndexes`, so
the D1 run-scoped iRODS join and the D2 manifest iRODS LEFT JOIN are
index-served immediately after a cold load (not only after the full index
rebuild). Do NOT add `study_mirror` or `study_users_mirror` to the sparse
cold-load read-index machinery -- they are small reference tables and
their indexes (1.1, 1.2) are created with the table. Covering both
acceptance tests from A5 (`seqProductIRODSLocationsMirrorReadIndexes`
includes `spi_mirror_iseq_product_idx`; the post-cold-load index-shape
accept-list -- `sqliteLargeCacheReadIndexShape` /
`...SparseReadIndexColumns` -- is updated to the new shape and still
passes). Depends on 1.2 (the index definition).

- [x] implemented
- [x] reviewed

#### Item 1.5: A4 - CacheSchemaVersion bump (full resync)

spec.md section: A4

Bump `CacheSchemaVersion` from 10 to 12 (`cache.go`). The new
`study_users_mirror` table and all new indexes are created by the
recreate-tables migration (full resync); do NOT take the additive
no-version-bump path. `study_users_mirror` is already in
`cacheMigrationRecreateTables`/`...DropTables` and `study_users` in
`cacheMigrationSyncStateTables` (from 1.1), so the migration recreates
the table cleanly and the next sync repopulates it. Covering both
acceptance tests from A4 (`CacheSchemaVersion == 12`; the existing
recreate-migration test, extended to cover `study_users_mirror`,
recreates the tables, clears the sync-state rows for the recreated
tables, and stamps `schema_version` to 12). Depends on 1.1 (table +
migration-list registration) and 1.3 (the sync state for `study_users`).

- [x] implemented
- [x] reviewed

For sequential items, a single review pass after each item (or one pass
over Batch 2) is acceptable; reviewers must confirm the schema change is
ONE version bump driving ONE full resync, not multiple bumps.

## Ordering and dependency notes

- This phase is the foundation for all later phases; nothing in Phases
  2-7 should start until Phase 1 is fully reviewed.
- Batch 1 (A1 table, A2 indexes) is parallel; Batch 2 (A3 sync, A5
  sparse-read, A4 version bump) is sequential and depends on Batch 1's
  schema. A4 must be last so the migration lists and sync state for
  `study_users` (from A1/A3) are in place when the version bump triggers
  the recreate migration.
- The single bump 10 -> 12 drives one full resync that creates
  `study_users_mirror` and the new indexes; reviewers should confirm the
  resync is not split into multiple version bumps and that the additive
  `IF NOT EXISTS` no-version-bump path is NOT used.
- Per-endpoint Registry/handler/RemoteClient wiring is done incrementally
  in Phases 2-5 (per the spec's G note); Phase 6 does the final
  `APIVersion` bump, CLI, doc regeneration, and drift-guard verification.
