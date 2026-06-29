# Phase 3: D2 -- Study data manifest (C1-C2)

Ref: [spec.md](spec.md) sections C1, C2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both (the four-step add-a-query recipe, the `id_lims =
'SQSCP'` invariant, the never-synced/unknown-study/synced-empty cascade
matching `CountSamplesForStudy`, and the count<->list cross-check
`count == len(StudyManifest(...).Rows)`).

All new list code lives in `mlwh/manifest.go` (with the `ManifestRow` /
`StudyManifest` structs in `mlwh/types.go`) and the count in
`mlwh/count.go`; tests in `mlwh/manifest_test.go` and
`mlwh/count_test.go`, hermetic over `openSQLiteSyncTestCache`. This phase
depends on Phase 1 (the iRODS `id_iseq_product` index) and Phase 2 (the
run-scope / file-type linkage reused for the optional iRODS-path column).

The manifest row grain is ONE row per sequencing product (distinct
`id_run, position, tag_index`), joined to its sample's identity in
`sample_mirror`, scoped by the product-metrics `id_study_lims`. The study
metadata (`name`/`accession_number`/`faculty_sponsor`/`data_access_group`)
is carried ONCE in the envelope from `study_mirror`, never per row. The
optional iRODS-path column is a set-at-once LEFT JOIN + GROUP BY product
(never a per-row correlated subquery -- that is the
per-platform-breakdown perf class); a product with no matching iRODS
object has `irods_path=""`.

## Items

### Item 3.1: C1 - study manifest list (one row per product) + optional iRODS path

spec.md section: C1

Implement `StudyManifest(ctx, studyLimsID, fileType string, withIRODS
bool, limit, offset int) (StudyManifest, error)` for `GET
/study/:id/manifest`. The `StudyManifest` envelope carries the study
`id_study_lims`/`name`/`accession_number`/`faculty_sponsor`/
`data_access_group` once (from `study_mirror`), `cache_synced_at`, and a
page of `ManifestRow` (one per product: `name`, `supplier_name`,
`accession_number`, `sanger_sample_id`, `id_run`, `lane` [=position],
`tag_index`). Order by `(id_run, position, tag_index, name)` for
determinism; paginate by `limit/offset` (fetch-all default) with
`X-Total-Count` / `X-Next-Offset` sized by C2's count. When
`with_irods=true`, each row also carries `irods_path` via LEFT JOIN
`seq_product_irods_locations_mirror` on `id_iseq_product` (and
`id_study_lims`), restricted by the D1 `file_type` filter when set
(with_irods and no file_type = any one object for the product; do NOT
default file_type to cram); set-at-once LEFT JOIN + GROUP BY product. The
never-synced/unknown-study/synced-empty cascade matches
`CountSamplesForStudy` (never-synced -> zero value +
`neverSyncedReadErr()`; unknown -> `ErrNotFound`; synced-empty -> envelope
with study metadata, empty `rows`, populated `cache_synced_at`). Wire the
Registry entry (with `with_irods` + `file_type` `QueryParams`), `Queryer`
member, `server.go` case, and `RemoteClient` method (plain `remoteCall`
-- the manifest is an envelope, not a bare slice). The `Description`
states: row grain (one per product run x lane x tag); study metadata in
the envelope, not per row; `with_irods` + `file_type` add the cram (or
suffix) path via D1's run-scope/suffix semantics; bounded-by-default and
pageable; the freshness caveat. Covering all 5 acceptance tests from C1
(metadata once + 3 product rows with correct per-row fields and no
irods_path + populated cache_synced_at; `cram`+`with_irods` gives 2 rows
their `.cram` path and the uncovered row `""`, row count still 3;
`limit=2&offset=0` gives 2 rows, `X-Total-Count: 3`, `X-Next-Offset: 2`;
never-synced both sentinels / unknown `ErrNotFound` / synced-empty
envelope with metadata + empty rows + cache_synced_at; MySQL EXPLAIN
index-served with and without `with_irods` -- see I1/Phase 7). Depends on
Phases 1 and 2 (the iRODS join/index and file-type linkage).

- [x] implemented
- [x] reviewed

### Item 3.2: C2 - study manifest count

spec.md section: C2

Implement `CountStudyManifest(ctx, studyLimsID string) (Count, error)`
for `GET /study/:id/manifest/count` via `queryCount` as `COUNT(*)` over
the same `SELECT DISTINCT (id_run, position, tag_index)` products as the
manifest list, with no LIMIT, so count == len(rows-all). The `file_type`
/ `with_irods` params do NOT change the count (the manifest is
product-grained; a product with no matching iRODS object is still a row).
Cascade matches `CountSamplesForStudy`. Wire the Registry entry,
`Queryer` member, `server.go` case, and `RemoteClient` method. Covering
both acceptance tests from C2 (`Count{3}` equal to
`len(StudyManifest("S1","",false,all).Rows)`; never-synced both sentinels
/ unknown `ErrNotFound` / synced-empty `Count{0}` no error). Depends on
3.1 (same DISTINCT product grain).

- [x] implemented
- [x] reviewed

For sequential items, a single review pass after each item is
acceptable; reviewers must confirm the count matches the list-all length
exactly and that the iRODS-path join is set-at-once (LEFT JOIN + GROUP
BY), not a per-row correlated subquery.

## Ordering and dependency notes

- This phase depends on Phases 1 and 2 being fully reviewed (the iRODS
  `id_iseq_product` index and the D1 run-scope/file-type linkage reused
  for the optional iRODS-path column).
- Items are sequential: 3.1 builds the manifest list and its DISTINCT
  product grain, 3.2 counts over the same grain so count == len(rows).
- The optional iRODS-path column must be a set-at-once LEFT JOIN + GROUP
  BY product; reviewers should confirm it is not a per-row correlated
  subquery (the per-platform-breakdown perf trap). The MySQL EXPLAIN
  proof (with and without `with_irods`) lives in Phase 7 (I1).
- Per-endpoint Registry/handler/RemoteClient wiring is done incrementally
  here (per the spec's G note); Phase 6 does the final `APIVersion` bump,
  CLI, doc regeneration, and drift-guard verification.
