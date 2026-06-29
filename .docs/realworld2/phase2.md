# Phase 2: D1 -- Run-scoped iRODS, id_run/platform, and the file-type filter (B1-B3)

Ref: [spec.md](spec.md) sections B1, B2, B3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both (the four-step add-a-query recipe, the `id_lims =
'SQSCP'` invariant, the never-synced cascade, and the count<->list
cross-check `count == len(list-all)`).

All new code lives in `mlwh/hierarchy.go` (SQL + scan), `mlwh/count.go`
(counts), `mlwh/types.go` (the `IRODSPath` field additions), and the
wiring files (`registry.go`, `queryer.go`, `server.go`, `remote.go`).
Tests live in `mlwh/hierarchy_test.go`, `mlwh/count_test.go`,
`mlwh/server_test.go`, and `mlwh/remote_test.go`, hermetic over
`openSQLiteSyncTestCache`. This phase depends on Phase 1's
`spi_mirror_iseq_product_idx (id_iseq_product)` index, which makes the
run-scope join index-served.

The items are sequential because they share one read path: B1 adds
`id_run`/`platform` to the iRODS select and scan, B2 adds the file-type
filter to that select plus the HTTP `file_type` param, and B3 adds the
run-scoped variant reusing both. The file-type filter is a
FILENAME-SUFFIX match (`irods_file_name LIKE '%.<token>'`,
case-insensitive), NOT a real file-type column: a valid-but-unmatched
suffix yields an EMPTY result (no error); 400 is reserved for an
empty/whitespace token or one containing `%`, `_`, or `/`.

CAUTION (Note 3, B1 / `IRODSPath`): the `IRODSPath` struct edit (adding
`IDRun` and `Platform`) is purely ADDITIVE. Preserve the existing field
doc comments verbatim (e.g. the `Name` field wording "Sanger sample name
of the sample the data object belongs to; empty when absent from the
sample mirror"); do not reword existing fields while adding the two new
ones.

## Items

### Item 2.1: B1 - id_run and platform on every iRODS row

spec.md section: B1

Extend the study/sample/run iRODS read SQL
(`irodsPathsForStudyCacheSQL`, `irodsPathsForSampleCacheSQL`) to LEFT
JOIN `iseq_product_metrics_mirror` on `id_iseq_product` and select a
deterministic `MIN(id_run)` (or `MAX`) as `id_run` plus the iRODS
mirror's `platform`; `id_run` is 0 when the LEFT JOIN finds no match
(non-Illumina/unmatched). Add `IDRun int` and `Platform string` to the
`IRODSPath` struct (`types.go`, ADDITIVE -- preserve existing field doc
comments per the caution above) and to the scan in `queryIRODSPaths` /
`queryIRODSPathsWithSample`. Keep the matching `/count` DISTINCT grain
unchanged (`id_iseq_product` already keys a row) so count == len(list).
Method signatures unchanged (`IRODSPathsForStudy`, `IRODSPathsForSample`).
Covering all 3 acceptance tests from B1 (an Illumina row gets
`id_run=52553`/`platform="illumina"`; an unmatched ont row gets
`id_run=0`/`platform="ont"`; `CountIRODSPathsForStudy` equals
`len(IRODSPathsForStudy(...,all))` -- grain unchanged). Depends on Phase
1.

- [x] implemented
- [x] reviewed

### Item 2.2: B2 - file-type filter on study/sample/run iRODS lists and counts

spec.md section: B2

Add a `fileType string` parameter (after the id, before `limit, offset`)
to the iRODS list/count methods via file-type-aware variants, keeping the
existing bare methods delegating with `fileType=""`:
`IRODSPathsForStudyByFileType`, `IRODSPathsForSampleByFileType`,
`CountIRODSPathsForStudyByFileType`, `CountIRODSPathsForSampleByFileType`.
When `fileType` is empty, behaviour is unchanged (all rows); when set,
normalise it (strip one leading `.`, lowercase) and append `AND
LOWER(irods_file_name) LIKE '%.' || LOWER(?)` (sqlite) / the mysql
equivalent, binding the normalised token. A normalised token containing
`%`, `_`, or `/`, or empty after trimming, is rejected with
`ErrUnsupportedIdentifier`. In `server.go`, the existing
`/study/:id/irods`, `/sample/:id/irods` (and `/count`) accept a
`file_type` query param parsed by a new helper `mlwhFileTypeFromQuery`
(empty -> `("", true)`; invalid chars -> 400 via `writeMLWHBadRequest`
before the queryer; else `(normalised, true)`); dispatch bare vs
file-type via an optional-capability interface so the `Method` names and
Registry entries are unchanged with `file_type` as an additive `Query`
param (add `fileTypeQueryParam` in `registry.go`). Covering all 6
acceptance tests from B2 (cram filter returns the 2 `.cram` of 3 objects,
count 2; `.CRAM` leading-dot/mixed-case matches; valid-but-unmatched
`bam` -> empty list + count 0, no error; empty/`%`/`a/b`/`a_b` -> 400
before the queryer; sample `.cram` filter returns 1; the MySQL EXPLAIN of
the file-type query is index-served -- see I1/Phase 7). Depends on 2.1
(extends the same select).

- [x] implemented
- [x] reviewed

### Item 2.3: B3 - run-scoped iRODS list + count

spec.md section: B3

Implement `IRODSPathsForRun(ctx, idRun, fileType string, limit, offset
int) ([]IRODSPath, error)` and `CountIRODSPathsForRun(ctx, idRun,
fileType string) (Count, error)` for `GET /run/:id/irods` (+
`/run/:id/irods/count`). `:id` is the Illumina NPG `id_run` (resolved via
`ResolveRun`; non-Illumina/invalid -> the existing
not-found/unsupported-identifier error; a numeric run absent from a
synced cache -> `ErrNotFound`; never-synced -> an error satisfying both
`ErrCacheNeverSynced` and `ErrNotFound`). The list joins the run's
`iseq_product_metrics_mirror` rows (filtered by `id_run`) to the iRODS
mirror on `id_iseq_product`, returning `IRODSPath` rows (each with
`id_run` = the run, `platform`); the B2 `file_type` filter applies.
Paginated with `X-Total-Count` / `X-Next-Offset`; the count is the same
join with no LIMIT (count == len(list)). Wire the Registry entry,
`Queryer` member, `server.go` case, and `RemoteClient` method (+
`IRODSPathsForRunPage`) incrementally. Covering all 5 acceptance tests
from B3 (run 52553 returns 6 rows each `id_run=52553`; `file_type=cram`
returns 4 with count 4; `limit=2&offset=0` body of 2, `X-Total-Count: 6`,
`X-Next-Offset: 2`; non-numeric -> `ErrUnsupportedIdentifier`,
absent-numeric -> `ErrNotFound`, never-synced -> both sentinels;
count == len(list)). Depends on 2.1 (id_run/platform select), 2.2
(file-type filter), and Phase 1 (the `id_iseq_product` index).

- [x] implemented
- [x] reviewed

For sequential items, a single review pass after each item is
acceptable; reviewers must confirm the count<->list grain identity holds
for every variant and that the file-type 400 is raised in the handler
before the queryer is reached.

## Ordering and dependency notes

- This phase depends on Phase 1 being fully reviewed (the
  `spi_mirror_iseq_product_idx (id_iseq_product)` index in the full and
  sparse read sets makes the run-scope join index-served immediately
  after a cold load).
- Items are sequential because they share one read path: 2.1 adds the
  id_run/platform select and scan, 2.2 adds the file-type filter to that
  select and the HTTP param, and 2.3 reuses both for the run scope.
- The `IRODSPath` edit is additive (Note 3): preserve every existing
  field doc comment.
- The MySQL EXPLAIN proof that B2's file-type study iRODS query and B3's
  run-scoped query are index-served lives in Phase 7 (I1); this phase
  must keep the queries index-friendly (study-scoped index for B2,
  `id_iseq_product` join for B3) so that proof holds.
- Per-endpoint Registry/handler/RemoteClient wiring is done incrementally
  here (per the spec's G note); Phase 6 does the final `APIVersion` bump,
  CLI, doc regeneration, and drift-guard verification.
