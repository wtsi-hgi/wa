# Phase 1: Products export core

Ref: [spec.md](spec.md) sections A1, A2, B1, B2, E1, J1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

Dependencies: none (foundational phase). Keep the existing
`StudyManifest` method and its helpers working via the shared query
builders throughout this phase; the manifest surface is not removed
until phase 6. Reuse the existing seed helpers `seedManifestS1Scenario`
and `seedManifestStudy7568MergedCRAMScenario` (see the spec's "Testing
strategy").

## Items

Items are sequential. Item 1.1 (A1) establishes the `products`
relationship, `productExportVocabulary`, `exportProductRow`, the
refactored product-grain query, the products page path
(`exportProducts`), and `Total`. Items 1.2-1.6 build on that foundation
and all edit `mlwh/export.go` and `mlwh/export_test.go`, so they run in
order to avoid conflicts.

Scope note: this phase produces `Columns`/`Rows`/`Total`/`Complete` for
requests that need no pagination (results fit; no incoming cursor).
Full keyset pagination (`limit + 1` page, `NextCursor`, cursor
continuation, the default 1000-row bounded page) and the `--all` stream
are added in phase 3 (D1-D3), so leave those to that phase.

### Item 1.1: A1 - Default product columns, one row per product incl. objectless

spec.md section: A1

Files: `mlwh/export.go`, `mlwh/manifest.go`; tests in
`mlwh/export_test.go`. Add `exportRelationshipProducts` and its
`exportRelationshipSpecs` entry (parent kind `study`); add the dedicated
`productExportVocabulary` (13 columns, all `Supported: true`; aliases
`position`->`lane` and `supplier_sample_name`->`supplier_name`; default
projection = the first 8 names) and return it from
`vocabularyForExportKind`; add `exportProductRow` with `cell` and
`cursor` methods (the distinct triple; cursor 4th field 0). Refactor the
`mlwh/manifest.go` product-grain builders (`manifestProductGrain*`,
`manifestListSelectPrefix`, `scanManifestRow` logic) into the products
page path `exportProducts`, reusing `GROUP BY ipm.id_run, ipm.position,
ipm.tag_index`, `ORDER BY ..., MIN(sm.name)`, and the int64
`exportCursor` machinery; render `manual_qc` via `qcRollupString`
(`mlwh/qc.go`). Covers all 5 acceptance tests A1.1-A1.5 (default columns
and `(id_run, position, tag_index, name)` order; product grain incl.
objectless rows; alias resolution to canonical names; unknown-column
`ErrUnsupportedIdentifier` with "unknown export column" and "valid
columns:"; qc roll-up pass/fail/pending). Reuse `seedManifestS1Scenario`.

- [x] implemented
- [x] reviewed

### Item 1.2: A2 - Product-safe extras

spec.md section: A2

Make `id_study_lims` and `study_accession_number` selectable via the
`productExportVocabulary`, filled from the resolved parent (study-constant,
not per-row queried) onto `exportProductRow.IDStudyLims` and
`exportProductRow.StudyAccessionNumber`. Covers the 1 acceptance test
A2.1 (`id_study_lims` cell is the study id and `study_accession_number`
equals the resolved study's accession number on every row). Reuse
`seedManifestS1Scenario`.

- [x] implemented
- [x] reviewed

### Item 1.3: B1 - irods_path attaches per product; file_type is attachment-only

spec.md section: B1

Set an internal `needsIRODS` flag when any of `irods_path`,
`irods_unmatched`, or `reason` is selected; attach the set-at-once
ranked iRODS LEFT JOIN reused from `mlwh/manifest.go` (derived table
ranked by `ROW_NUMBER() OVER (PARTITION BY id_iseq_product ...)`, joined
on shared `id_iseq_product` + `id_study_lims`, path assembled in Go),
keeping a blank `irods_path` for products with no matching object.
Restructure `exportFileFilters` and add an
`exportRelationshipAttachesFileType(kind)` predicate (true for products)
so products accepts `file_type` as attachment-only: no `cram` default,
no forced `deliverables_only`, and it never drops a product row; apply
the `cram`/`deliverables_only` defaults only for
`exportRelationshipUsesFileType` (irods/sample-crams). Covers all 3
acceptance tests B1.1-B1.3 (97 rows with one attached path under
`file_type=cram`; empty `file_type` still attaches and keeps 97; `bam`
attaches nothing and `Total` stays 97). Reuse
`seedManifestStudy7568MergedCRAMScenario`.

- [x] implemented
- [x] reviewed

### Item 1.4: B2 - irods_unmatched / reason are merged-multilane only

spec.md section: B2

Emit `irods_unmatched="true"` / `reason="merged_multilane"` ONLY for the
classified merged multi-lane CRAM gap (reuse
`manifestListIRODSUnmatchedExpression`: a single-lane product with no
direct object whose sample has a study-scoped merged composite CRAM),
gated to `file_type` unset-or-`cram`; a merely objectless product leaves
`irods_unmatched="false"` and blank `reason`, and the composite path is
never copied onto single-lane rows. Ensure `ExportResult` carries no
`products_without_irods` or envelope gap summary (the rows are the
source of truth). Covers all 4 acceptance tests B2.1-B2.4 (96 unmatched

- 1 direct under `cram`; objectless S1 rows all false/blank; `bam`
  flags nothing with `Total` 97; no envelope gap field). Reuse
  `seedManifestS1Scenario` and `seedManifestStudy7568MergedCRAMScenario`.

* [x] implemented
* [x] reviewed

### Item 1.5: E1 - Products signal unknown, never-synced, synced-empty studies

spec.md section: E1

Resolve the parent FIRST in `Export` (`resolveExportParent` ->
`resolveExportStudy` -> `ResolveStudy`) so unknown-study and
never-synced are caught before the zero-row handler; for the zero-row
first page reuse the manifest's product-metrics sync gating
(`studyManifestForEmptyStudy`: `manifestEmptyRequiredSyncTables`,
`cacheStudyExists`, `requiredSyncStateSummary`, `neverSyncedReadErr`).
Covers all 5 acceptance tests E1.1-E1.5 (synced study, no products ->
`emptyExportResult(plan, 0)`; never-synced cache -> joined `ErrNotFound`

- `ErrCacheNeverSynced` for a resolvable id; unknown id -> `ErrNotFound`
  only; product-metrics never synced -> joined never-synced sentinel;
  `needsIRODS` with the iRODS locations mirror never synced ->
  cache-never-synced before attaching, matching `sample-crams`). Reuse
  `seedManifestS1Scenario`.

* [x] implemented
* [x] reviewed

### Item 1.6: J1 - irods and sample-crams behaviour is preserved (regression)

spec.md section: J1

Regression guard for the phase-1 dedicated-vocabulary and
`exportFileFilters` restructure. Confirm the existing `irods of study`
and `sample-crams of study` tests still pass unchanged, and that the
`productExportVocabulary` does not leak product-grain columns into the
file vocabularies. Covers all 3 acceptance tests J1.1-J1.3 (existing
irods tests pass; existing sample-crams tests pass; requesting
`irods_unmatched` or `reason` on `irods`/`sample-crams` returns an
"unknown export column" `ErrUnsupportedIdentifier`).

- [x] implemented
- [x] reviewed
