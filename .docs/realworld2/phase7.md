# Phase 7: Real-MySQL and source integration tests (I1-I2)

Ref: [spec.md](spec.md) sections I1, I2 (with the J1 shared seed and J2 regression preservation)

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both. These are the integration tests that prove the new
query paths execute and are index-served on MySQL (not only SQLite) and
that the assumed source schema stays true against the real warehouse.

Both items follow EXISTING harnesses and gate on environment variables,
so they skip cleanly when the env is absent: I1 follows
`cache_mysql_integration_test.go` (skip when `WA_MLWH_CACHE_PATH` is
absent; `realMySQLCacheDSNOrSkip` + `createThrowawayMySQLCacheDBOrSkip`
with a `t.Cleanup` dropping the throwaway DB on success and failure) and
the `explainRunsForStudy` / `mysqlExplainRow` EXPLAIN helper pattern; I2
follows `sync_source_integration_test.go` (skip when `WA_MLWH_DSN` is
absent; `openRealMLWHSourceOrSkip`, `prepareAndCloseSourceQuery`). Do NOT
run a live warehouse yourself; the tests are written to skip without the
env and are exercised in CI where the env is present. This phase depends
on ALL prior phases (the query paths and the `study_users` sync source
query must exist).

CAUTION (Note 1, D3 SEED FOOTGUN -- applies to the J1 seed used by I1):
the D1q.2 strict equality `qc_pass + qc_fail + qc_pending == samples_total
- distinct.registered` holds in production ONLY because the iRODS mirror
is populated exclusively via INNER JOINs to the product-metrics source
tables (every `with_data` sample has a product-metrics row). The J1
scenario seed and the I1 MySQL fixtures must NOT introduce an iRODS-only
sample lacking a product-metrics row (e.g. the artificial `ont`-platform
iRODS row used for B1.2 in Phase 2) into the QC-count fixtures, or the
strict-equality assertion will spuriously fail on MySQL. The
not_tracked / ONT / registered-only samples must have NO product-metrics
rows and no iRODS row in the QC scenario.

## Items

### Batch 1 (parallel)

I1 (real-MySQL EXPLAIN-proven query tests) and I2 (source-schema
prepare-validation) live in disjoint test files
(`cache_mysql_integration_test.go` vs `sync_source_integration_test.go`)
and gate on different env vars, so they can be implemented concurrently.

#### Item 7.1: I1 - real-MySQL integration test of the new query paths (EXPLAIN-proven) [parallel with 7.2]

spec.md section: I1

In `mlwh/cache_mysql_integration_test.go`: build the schema in a
throwaway MySQL DB, seed the J1 scenario (extend the shared seed helpers
per J1 -- `seedStudyMirrorRow` for `faculty_sponsor`/`data_access_group`/
`name`/`accession_number`; the A-E QC mix via `seedIseqProductMetricsMirrorRow`
/ `...WithQC`; `seedIRODSLocationMirrorRowWithCreatedPlatform` for
`.cram`/`.bai` objects matching the product-metrics rows incl. one
Illumina [id_run derivable] and one non-Illumina [id_run 0]; a new
`seedStudyUsersMirrorRow(t, db, idStudyUsersTmp, idStudyTmp int64, role,
login, email, name string)` and matching study rows for the D4 person
scenarios; `seedSyncStateRun`/`seedSyncState` for the feeding tables and
`study_users`), heeding the SEED FOOTGUN caution above for the QC
fixtures. Assert on MySQL: `IRODSPathsForRun`/`CountIRODSPathsForRun`
(with and without `file_type`); `StudyManifest`/`CountStudyManifest`
(with and without `with_irods`+`file_type`); `StatusBreakdown.QC`
(qc_pass/qc_fail/qc_pending); `StudiesForFacultySponsor`/
`StudiesForUser`/`ResolvePerson` -- each returning the same counts/rows
the SQLite tests assert, with the count<->list cross-check. Via EXPLAIN
(`explainRunsForStudy`/`mysqlExplainRow`): the run-scoped iRODS query, the
manifest query, and the file-type-filtered study iRODS query are
index-served (a real `key`, `LOWER(type) != "all"`, no full scan of the
9M-row iRODS or product-metrics mirrors); the `/studies/user` query uses
a `study_users_mirror` lookup index. Covering all 3 acceptance tests from
I1 (each new query path returns the SQLite counts/rows on MySQL; EXPLAIN
of the run-scoped iRODS, manifest, and file-type-filtered study iRODS
queries each has a non-empty real index key and is not a full scan;
EXPLAIN of `/studies/user` is served by a `study_users_mirror` index, not
a full scan). Depends on all prior phases.

- [x] implemented
- [x] reviewed

#### Item 7.2: I2 - source integration test for the new source columns/tables [parallel with 7.1]

spec.md section: I2

In `mlwh/sync_source_integration_test.go`: extend the source-schema test
so `AllSyncSourceQueries()` includes the new `study_users` wholesale
source SELECT and `supportedSyncTables` / `wholesaleMirrorTables()`
include `study_users`, so the generic `prepareAndCloseSourceQuery`
validator PREPAREs the `study_users` query against the real source
(proving `study_users` exists with `id_study_users_tmp`, `id_study_tmp`,
`role`, `login`, `email`, `name`, `last_updated`). Add a probe SELECT
(via `PrepareContext`) asserting `study.faculty_sponsor`,
`study.data_access_group`, and `iseq_product_metrics.qc` exist. Covering
both acceptance tests from I2 (every sync source query including the new
`study_users` query PREPAREs/validates and `study_users` is covered by
the supported-tables check; the probe SELECT of `study.faculty_sponsor`,
`study.data_access_group`, `iseq_product_metrics.qc` PREPAREs
successfully). Depends on Phase 1 (the `study_users` sync source query
and supported-tables registration).

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

## Ordering and dependency notes

- This phase depends on ALL prior phases being fully reviewed: I1
  exercises every new query path on MySQL (Phases 2-5) and I2 validates
  the `study_users` source query and the new source columns (Phase 1).
- Batch 1 (I1, I2) is parallel: disjoint test files, different env-var
  gates.
- SEED FOOTGUN (Note 1, repeated here for the J1/I1 fixtures): the
  QC-count fixtures must NOT add an iRODS-only sample lacking a
  product-metrics row, or D1q.2's strict equality fails on MySQL. Build
  the not_tracked/ONT/registered-only samples with no product-metrics row
  and no iRODS row.
- REGRESSION PRESERVATION (J2): all existing tests must still pass.
  Reviewers must confirm this phase adds tests and does not weaken or
  remove the existing regression suite (the `StatusBreakdown`
  distinct/per_platform/with_detailed_timeline tests, the
  `IRODSPathsForStudy`/`...Sample` tests, the `StudyOverview` tests, the
  cross-dialect shape test, the count<->list cross-checks, the
  recency-Description guard, `TestRegistryCoversQueryer`,
  `TestRegistryNewEndpointsAreFullyDocumentedG1` [extended set], and the
  docs/OpenAPI drift guards). Every spec acceptance test must have a real
  GoConvey test -- no stubs, hardcoded results, swallowed failures, or
  build-tag exclusions.
- These tests skip cleanly without `WA_MLWH_CACHE_PATH` (I1) /
  `WA_MLWH_DSN` (I2); do not run a live warehouse to "verify" them.
