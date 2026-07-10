# Product-Grained Export Specification

## Overview

Replace the MLWH study-manifest surface with a new product-grained relationship
in the existing generic export framework: `products of study`. Product export
starts from a study's Illumina sequencing products (one row per distinct
`(id_run, position, tag_index)` triple in `iseq_product_metrics_mirror`), so it
keeps products that have no matching iRODS object - the one behaviour the
manifest had that plain file exports lack.

This is a breaking change. The manifest endpoint, count endpoint, CLI command,
typed envelope (`StudyManifest`/`PagedStudyManifest`/`ManifestRow`/
`products_without_irods`), registry entries, remote methods, and manifest docs
are all removed. No aliases, redirects, hidden commands, or compatibility
wrappers remain. The proven manifest query logic (product grain, the set-at-once
ranked iRODS join, merged-multilane gap detection) is refactored into the export
product implementation rather than rewritten.

`export products study <id>` is product-grained (includes products with no
iRODS path); `export irods` stays file-object-grained; `export sample-crams`
stays sample-grained (one merged-aware CRAM per sample). The single collateral
consumer, `wa mlwh info`, is rewired onto the products export and its Products
heading total bug is fixed.

## Architecture

### Packages and files

- `mlwh/export.go` - add the `products` relationship, its vocabulary, the
  export-internal product row type, and the products page/stream/total path.
- `mlwh/manifest.go` - refactor its query builders (`manifestProductGrain*`,
  `manifestListSelectPrefix`, `manifestList*` iRODS join blocks,
  `manifestListIRODSUnmatchedExpression`, `scanManifestRow`,
  `countStudyManifestProducts`, `manifestEmptyRequiredSyncTables`) into the
  products export; delete the `StudyManifest` method, envelope helpers, and
  `countManifestProductsWithoutIRODS`.
- `mlwh/qc.go` - reuse `qcRollupString` unchanged for `manual_qc`.
- `mlwh/count.go` - delete `CountStudyManifest` and
  `countStudyManifestForEmptyStudy`; the reused product-grain count lives with
  the refactored query.
- `mlwh/registry.go` - remove the `StudyManifest`/`CountStudyManifest` entries
  and `manifestQueryParams()`; edit the single `Export` entry `Description` and
  the shared `cursor` query-param description.
- `mlwh/server.go` - remove the `StudyManifest`/`CountStudyManifest` handler
  cases, `writeMLWHStudyManifest`, and `studyManifestTotal`.
- `mlwh/remote.go` - remove `StudyManifest`, `StudyManifestPage`,
  `CountStudyManifest`, `remoteManifestQuery`.
- `mlwh/queryer.go` - remove the `StudyManifest` and `CountStudyManifest`
  interface methods.
- `mlwh/types.go` - delete `StudyManifest`, `PagedStudyManifest`,
  `ManifestRow`, and the `products_without_irods` field.
- `cmd/mlwh_export.go` - render products; add bounded-page paging that states
  its mode; update help text.
- `cmd/mlwh_info.go` - rewire the Products section to the products export; fix
  the heading total; replace the `study_manifest` JSON field with a `products`
  array.
- `cmd/mlwh_manifest.go` - delete the whole file.
- `cmd/mlwh.go` - remove the `newMLWHManifestCommand()` registration (line 378).
- `README.md`, `.docs/mcp/glossary.md`, `.docs/mcp/api-reference.md` - remove
  manifest, document product export; regenerate the api-reference fixture.

### The `products` relationship

Add to `mlwh/export.go`:

```go
exportRelationshipProducts exportRelationshipKind = "products"
```

Append to `exportRelationshipSpecs` (order does not matter for correctness):

```go
{
    Children:    "products",
    ParentKinds: []string{"study"},
    Description: "product-grained rows, one per distinct id_run/lane/tag, " +
        "including products with no iRODS object",
    Kind: exportRelationshipProducts,
}
```

`vocabularyForExportKind` returns `productExportVocabulary` for the new kind.

### Product export vocabulary

A dedicated `productExportVocabulary` (NOT `fileExportColumns()`), so product
grain and `irods_unmatched`/`reason` never leak into the `irods`/`sample-crams`
vocabularies. All columns `Supported: true`.

Required selectable columns (11) and product-safe extras (2):

| column                 | alias                | notes                                 |
| ---------------------- | -------------------- | ------------------------------------- |
| name                   |                      | Sanger sample name                    |
| supplier_name          | supplier_sample_name |                                       |
| accession_number       |                      | sample accession                      |
| sanger_sample_id       |                      |                                       |
| id_run                 |                      | integer triple field                  |
| lane                   | position             | integer triple field                  |
| tag_index              |                      | integer triple field                  |
| manual_qc              |                      | product QC roll-up: pass/fail/pending |
| irods_path             |                      | blank when no matching object         |
| irods_unmatched        |                      | true only for merged_multilane gap    |
| reason                 |                      | merged_multilane, else blank          |
| id_study_lims          |                      | study-constant; from resolved parent  |
| study_accession_number |                      | study-constant; from resolved parent  |

Default projection = the first 8 names:
`name, supplier_name, accession_number, sanger_sample_id, id_run, lane,
tag_index, manual_qc`.

Do NOT add `platform` or any iRODS-object-grained column (`created`, `merged`,
`collection`, `data_object`, `id_product`, `deliverable`, ...): products is
Illumina-only (`iseq_product_metrics_mirror`), so `platform` would be a constant
`illumina` that adds nothing, and object-grained columns have no product-grain
meaning.

### Export-internal product row type

Introduce `exportProductRow` (replaces `ManifestRow`'s role; not exported, no
JSON tags), scanned from the refactored manifest list query:

```go
type exportProductRow struct {
    IDRun, Position, TagIndex           int
    Name, SupplierName                  string
    AccessionNumber, SangerSampleID     string
    ManualQC                            string
    IRODSPath                           string
    IRODSUnmatched                      bool
    Reason                              string
    IDStudyLims, StudyAccessionNumber   string // filled from resolved parent
}

func (r exportProductRow) cell(column string) string // like exportIRODSRow.cell
func (r exportProductRow) cursor() exportCursor       // triple; 4th field 0
```

`cell` renders `id_run`/`lane`/`tag_index` as plain integers (no merged-zeroing;
that is an iRODS concept), `manual_qc` via `qcRollupString`, `irods_unmatched`
as `"true"`/`"false"`, `reason` as `merged_multilane`/`""`, and `id_study_lims`/
`study_accession_number` from the constant parent values.

### Row grain, ordering, keyset cursor

- Grain = the manifest grain: reuse `GROUP BY ipm.id_run, ipm.position,
ipm.tag_index`, which collapses the sample/iRODS fan-out and composite/merged
  products sharing a triple. Do NOT switch to an `id_iseq_product` grain: it
  would change `Total` and break the reused fixtures.
- Order `ORDER BY ipm.id_run, ipm.position, ipm.tag_index, MIN(sm.name)`;
  `MIN(sm.name)` is cosmetic only and is NOT part of row identity or the cursor.
- Keyset = the triple. When a cursor is set, add the same dialect-portable
  row-value comparison the iRODS keyset uses, on the group-by columns:
  `AND (ipm.id_run, ipm.position, ipm.tag_index) > (?, ?, ?)`. No
  `id_iseq_product` tiebreaker (the `GROUP BY` makes the triple unique per
  output row).
- Reuse the existing `exportCursor` int64 machinery and
  `encodeExportCursor`/`decodeExportCursor` unchanged: the triple maps onto
  `IDRun`/`Position`/`TagIndex`; the 4th field (`IDSeqProductLocation`) is 0 for
  products. No new TEXT cursor encoding.
- Extend `newExportPlan` so the cursor is decoded for products too:
  `if kind == exportRelationshipIRODS || kind == exportRelationshipProducts`.
- Extend `validateExportContinuationSupport` so `cursor` is accepted for
  `products` (currently iRODS-only), and update its rejection message for the
  remaining bounded kinds from "only for iRODS exports" to "only for iRODS and
  products exports", consistent with the generalized `cursor` registry param
  doc.
- Created-date sort/window stays iRODS-only: products keeps hitting the existing
  `newExportPlan` guard that rejects `Sort`/`Since`/`Until` for non-iRODS kinds.

### iRODS path attachment, merged-gap, file_type

- Selecting any of `irods_path`, `irods_unmatched`, `reason` sets an internal
  `needsIRODS` flag that adds the set-at-once ranked iRODS LEFT JOIN (reused
  from `manifest.go`: derived table ranked by `ROW_NUMBER() OVER (PARTITION BY
id_iseq_product ...)` joined on shared `id_iseq_product` + `id_study_lims`,
  path assembled in Go). A product with no matching object keeps its row with
  blank `irods_path`.
- `irods_unmatched=true` / `reason=merged_multilane` is emitted ONLY for the
  classified merged multi-lane CRAM gap (reuse
  `manifestListIRODSUnmatchedExpression`: a single-lane product with no direct
  object whose sample has a study-scoped merged composite CRAM). A merely
  objectless product leaves `irods_unmatched=false` and `reason` blank. The
  composite path is never copied onto single-lane product rows.
- Merged-gap detection stays gated to `file_type` unset-or-`cram`; any other
  `file_type` never flags unmatched.
- `file_type` is path-attachment only. Products must be `file_type`-aware
  WITHOUT the file-export defaults: it must NOT default to `cram`, must NOT
  force `deliverables_only`, and must NOT drop product rows. Restructure
  `exportFileFilters`: accept `file_type` for products (a new
  `exportRelationshipAttachesFileType(kind)` predicate, true for products,
  suppresses the "file-type applies only to file exports" error), but apply the
  `cram` default and the `deliverables_only` default ONLY for
  `exportRelationshipUsesFileType` (irods/sample-crams). For products,
  `normalised = normaliseFileType(opts.FileType)` (empty stays empty) and
  `deliverablesOnly = opts.DeliverablesOnly != nil && *opts.DeliverablesOnly`.

### Filters (product-row filters)

Extend `validateExportFilterSupport` so `products` supports `qc`,
`library_type`, `organism`, and `deliverables_only`, all as product-row filters
applied identically to the page query and the total count so
`Total == len(all matching rows)`:

- `qc` filters the product's rolled-up `manual_qc` verdict (the `qc.go`
  roll-up), applied as a `HAVING` over the same grouped aggregates
  (`COUNT(*)`, `SUM(CASE WHEN ipm.qc IS NULL ...)`, `MIN(ipm.qc)`) that feed
  `qcRollupString`: `fail` -> `MIN(ipm.qc)=0`; `pending` -> not fail and any
  NULL qc; `pass` -> `MIN(ipm.qc)=1` with no NULL qc. NOT a raw per-object qc.
- `library_type` -> products whose sample has the given `pipeline_id_lims`
  (a `library_samples` EXISTS scoped by the study), a `WHERE` filter.
- `organism` -> products whose sample `common_name` matches the organism
  (resolved via `exportOrganismCommonNames`), a `WHERE` filter on
  `sm.common_name`.
- `deliverables_only` -> the product's OWN deliverable discriminator, NOT the
  iRODS `is_deliverable` flag. For the Illumina product grain this is an EXISTS/
  join on the product's flowcell: `iseq_flowcell.entity_type IN ('library',
'library_indexed')` (the documented per-product `deliverable` definition). It
  must never drop a product that lacks an iRODS object.

`Total` reflects the filtered product-row count. `file_type` NEVER changes
`Total`; `qc`, `library_type`, `organism`, and `deliverables_only` do.

### Total

Reuse/rename `countStudyManifestProducts` as the products export total. Base it
on `manifestProductGrainDistinctSQL` (`SELECT DISTINCT ipm.id_run, ipm.position,
ipm.tag_index FROM iseq_product_metrics_mirror ipm WHERE ipm.id_study_lims = ?`)
and apply the SAME `qc`/`library_type`/`organism`/`deliverables_only` filters as
the page (but NOT `file_type`), so `count == len(all filtered rows)`. When `qc`
is set, the count must be `COUNT(*)` over the grouped subquery (`... GROUP BY
ipm.id_run, ipm.position, ipm.tag_index HAVING <qc>`), NOT the `SELECT DISTINCT`
`manifestProductGrainDistinctSQL` shape, so it matches the rolled-up
`manual_qc` verdict; `library_type`/`organism`/`deliverables_only` are plain
`WHERE` predicates that count over either shape.

### Page vs stream

- Bounded page (`exportPage` -> new `exportProducts`): compute the filtered
  `Total`, query `limit + 1` product rows with the keyset, set `more =
len > limit`, trim, `Complete = !more`, and `NextCursor =
encodeExportCursor(lastRow.cursor())` when `more`. Mirror `exportIRODS`.
- Complete stream (`exportAll` -> new `exportProductsAll`): return a
  `streamRows` closure (`Total: -1`, `Complete: true`, `Rows: nil`) paged by
  keyset via a new `streamExportProductsRows` (mirror `streamExportIRODSRows`,
  advancing the cursor to the last row with no deep OFFSET). Route products
  through this keyset stream in `exportAll`, NOT through `streamExportPages`.
- `limit` defaults to `defaultExportAllLimit` (1000) via the existing
  `exportPaging`. Default HTTP request (no `all`, no `limit`) is therefore a
  bounded page of up to 1000 rows.
- Like iRODS, the keyset `cursor` is the canonical continuation and `limit`
  bounds page size; `offset` is accepted by the shared paging plumbing (a
  first-page skip when no cursor is set), but `cursor` is the way to page
  products.

### Empty / never-synced cascade

`Export` resolves the parent FIRST (`resolveExportParent` ->
`resolveExportStudy` -> `ResolveStudy`), so two cases are caught there, before
the zero-row handler:

- unknown study (synced cache) -> `ResolveStudy` returns `ErrNotFound` (404
  `not_found`).
- never-synced cache -> for a name/accession-resolvable id, `ResolveStudy`
  surfaces the cache-never-synced signal (`ErrNotFound` joined with
  `ErrCacheNeverSynced`). Caveat: a purely NUMERIC id may take `ResolveStudy`'s
  numeric branch and return plain `ErrNotFound` WITHOUT the signal - this reuses
  the export framework's existing behavior (the old manifest never routed
  through `ResolveStudy`) and is intentionally NOT changed.

Once the parent resolves, the zero-row first page reuses the manifest's
product-metrics sync gating (`studyManifestForEmptyStudy`:
`manifestEmptyRequiredSyncTables`, `cacheStudyExists`,
`requiredSyncStateSummary`, `neverSyncedReadErr`):

- study/sample synced but `iseq_product_metrics` never synced -> the joined
  never-synced sentinel (E1.4).
- `needsIRODS` with the iRODS locations mirror never synced -> the
  cache-never-synced signal before attaching paths (E1.5), matching the
  manifest.
- synced study with no products -> `emptyExportResult(plan, 0)` (`Total 0`,
  `Complete true`, empty rows).

### HTTP output shape (do NOT change existing serialization)

`GET /export/products/study/:id` returns the existing `ExportResult`. The struct
has NO JSON tags or custom `MarshalJSON`, so the server serialises Go default
exported-field names: `Columns`, `Rows`, `Total`, `NextCursor`, `Complete`,
`Format`. Do NOT add JSON tags or a marshaller: that would change every existing
export's wire format and break acceptance test 10. (The prompt's snake_case JSON
sample is illustrative; the binding requirement is the existing shape.) The
server already parses `columns`, `file_type`, `qc`, `library_type`, `organism`,
`deliverables_only`, `limit`, `offset`, `all`, `cursor`, `format` via
`mlwhExportRequest`, and `materializeMLWHExportResult` drains the `all=true`
stream, so no server handler change is needed beyond the layer above.

### Registry / OpenAPI / MCP (single Export endpoint)

`Export` is one generic registry entry (`/export/:children/:parent_kind/
:parent_id`). There is no per-relationship OpenAPI channel; OpenAPI, MCP, and
`.docs/mcp/api-reference.md` derive from that entry's `Description` plus the
shared `exportQueryParams()`. Therefore:

- Append a products clause to the `Export` entry `Description` (registry.go
  ~560): products is product-grained (one row per distinct
  `(id_run, position, tag_index)`, including products with no iRODS object),
  keyset-cursor paginated; the default response is a bounded page (up to the
  internal 1000 default) carrying `Total`, `NextCursor`, and `Complete`; pass
  `cursor` to continue; `all=true` returns the complete set; `file_type` only
  restricts the attached `irods_path`. Also reconcile the Description's existing
  "cursor for iRODS keyset pagination" phrase to "cursor for iRODS/products
  keyset pagination" so the endpoint doc is coherent for both.
- Generalise the shared `cursor` query-param description (registry.go ~1243)
  from "opaque keyset cursor returned by a previous iRODS export page" to cover
  "a previous iRODS or products export page".
- Do NOT edit `fetchAllPaginationParams()`'s `limit` wording (registry.go
  ~1296): ~20 unrelated endpoints share it; out of scope.
- Remove the `StudyManifest` (registry.go ~433-444) and `CountStudyManifest`
  (~445-454) entries and `manifestQueryParams()` (~1189-1198).
- Regenerate `.docs/mcp/api-reference.md` (run `TestWriteEndpointReference` with
  `WA_REFRESH_DOCS`); the no-drift diff is scoped to the removed manifest
  sections and the edited Export section.

### CLI

`cmd/mlwh_export.go` already renders TSV/CSV/JSON through `RenderAsTo`. Products
needs a stated bounded-page mode ("no silent truncation"):

- Add `--limit` (int) and `--cursor` (string) flags.
- In `mlwhExportFlags.options`, set `All: true` ONLY when `--limit` is unset
  (preserving today's complete-set default and acceptance test 10 for every
  relationship); when `--limit > 0`, set `All: false`, `Limit`, and `Cursor`,
  and after the data write one status line to stderr stating a bounded page was
  emitted (and that omitting `--limit` exports everything); include a `--cursor
<NextCursor>` continuation hint ONLY when `NextCursor` is non-empty, so a
  limit/offset relationship (empty `NextCursor`) never prints an empty
  `--cursor`; if `Complete`, state it was the final page. The default (no
  `--limit`) path emits nothing extra.
- Update `mlwhExportOptionsHelp`: products is product-grained; `file_type` only
  restricts the attached `irods_path` (no `cram` default, no forced
  deliverables); blank `irods_path` is expected; use `irods_unmatched`/`reason`
  for known gaps; document `--limit`/`--cursor`. Add an example that exercises
  `wa mlwh export products study 7568 --file-type cram` with the full 11-column
  projection.
- The Children and Columns help blocks are generated from
  `ExportRelationshipDescriptions()` / `ExportColumnVocabularies()`, so products
  appears automatically once registered.

### `wa mlwh info` rewire + heading fix

- `mlwhInfoClient` (cmd/mlwh_info.go ~60): replace the `StudyManifest(...)`
  method (line 81) with `Export(ctx, rel mlwh.ExportRelationship, parentID
string, opts mlwh.ExportOptions) (mlwh.ExportResult, error)`.
- The Products call site (~1357): fetch a bounded page of `infoMaxRelated` (50)
  rows with the default 8 product columns and `Limit: infoMaxRelated`; do NOT
  request `irods_path` (info uses the non-iRODS view, matching today's
  `withIRODS=false`).
- Move per-row rendering into info (a new local helper reading cells by column
  name from the `ExportResult` row) producing the same `key=value` line the old
  `writeManifestRow` produced for the 8 fields; `writeManifestRow` disappears
  with `cmd/mlwh_manifest.go`.
- Heading bug fix (~801): pass `ExportResult.Total` (the full distinct-triple
  count) as `total` to `infoListHeading("Products", shown, total)` so a study
  with more than `infoMaxRelated` products renders "Products (50 of 780)" via
  the existing "shown of total" path, instead of always "Products (N)". (The
  "iRODS paths" section's identical `total = 0` at ~954 is out of scope.)
- `wa mlwh info --json` (infoReport ~1436): remove the `StudyManifest
*mlwh.StudyManifest json:"study_manifest,omitempty"` field (line 1452) and add
  a `products` typed array (a `cmd`-local `infoProductRow` with the 8 default
  fields, mapped from the `ExportResult` rows by column), consistent with the
  sibling `samples`/`runs`/`lanes`/`irods_paths` typed arrays. This is a
  deliberate, user-visible breaking change to `info --json`.

### Manifest surface removal / rewire map

Remove (production): `cmd/mlwh.go` line 378 registration; the whole
`cmd/mlwh_manifest.go`; `mlwh/types.go` `StudyManifest`/`PagedStudyManifest`/
`ManifestRow`/`products_without_irods`; `mlwh/registry.go` manifest entries and
`manifestQueryParams()`; `mlwh/remote.go` `StudyManifest`/`StudyManifestPage`/
`CountStudyManifest`/`remoteManifestQuery`; `mlwh/server.go` manifest handler
cases + `writeMLWHStudyManifest` + `studyManifestTotal`; `mlwh/queryer.go`
`StudyManifest` + `CountStudyManifest` methods; `mlwh/count.go`
`CountStudyManifest` + `countStudyManifestForEmptyStudy`; the `StudyManifest`
method + envelope helpers + `countManifestProductsWithoutIRODS` in
`mlwh/manifest.go`.

Reuse (do NOT delete): the manifest query builders, `scanManifestRow` logic, the
product-grain SQL, `countStudyManifestProducts`, and
`manifestEmptyRequiredSyncTables`, refactored into the products export.

Tests: delete `cmd/mlwh_manifest_test.go`; repurpose `mlwh/manifest_test.go`
fixtures for products-export tests; update `mlwh/count_test.go`,
`mlwh/registry_test.go`, `mlwh/server_test.go`, `mlwh/types_test.go`,
`mlwh/cache_mysql_integration_test.go`, `cmd/mlwh_info_test.go`; replace the
manifest local/remote parity coverage in `mlwh/parity_test.go` with
products-export parity. Also update `mlwh/docs_test.go`'s glossary-concept test
`TestGlossaryDefinesPeopleAndManifestConceptsG2` (drop the `"data manifest"`
term, assert the new product-export term per K1.3) - DISTINCT from the same
file's api-reference no-drift test, which G1 already covers.

### Errors

Reuse `ErrUnsupportedIdentifier` (unknown column / unsupported cursor / bad
format / invalid file_type), `ErrNotFound`, and `ErrCacheNeverSynced`
(`neverSyncedReadErr`) exactly as the existing exports and manifest do.

## A. Product export row set and columns

### A1: Default product columns, one row per product incl. objectless

As an API user, I want `export products study <id>` to return one row per
product with the default columns, so objectless products still appear.

**Package:** `mlwh/` **File:** `mlwh/export.go`, `mlwh/manifest.go`
**Test file:** `mlwh/export_test.go` (reuse `seedManifestS1Scenario`)

**Acceptance tests:**

1. Given `seedManifestS1Scenario` (study "S1", 3 products across 2 samples, no
   iRODS objects) and a synced cache, when `Export(ctx,
ExportRelationship{Children:"products", ParentKind:"study"}, "S1",
ExportOptions{})` runs, then `Columns` equals `["name","supplier_name",
"accession_number","sanger_sample_id","id_run","lane","tag_index",
"manual_qc"]`, `Rows` has length 3 ordered by `(id_run, position, tag_index,
name)`, `Total` is 3, `Complete` is true, and `NextCursor` is empty.
2. Given the same, when it runs, then all 3 products appear even though none has
   an iRODS object (product grain, not iRODS grain).
3. Given the same, when `Columns:["position","supplier_sample_name"]` is
   requested, then the result `Columns` are the canonical names
   `["lane","supplier_name"]` (aliases resolve).
4. Given the same, when `Columns:["not_a_column"]` is requested, then Export
   returns an `ErrUnsupportedIdentifier` error whose message contains `unknown
export column "not_a_column"` and `valid columns:`.
5. Given a study with products whose `iseq_product_metrics_mirror.qc` is 1, 0,
   and NULL respectively, when default columns are exported, then those rows'
   `manual_qc` cells are `pass`, `fail`, `pending` (reuse `qcRollupString`).

### A2: Product-safe extras

As an API user, I want `id_study_lims` and `study_accession_number` selectable.

**Acceptance tests:**

1. Given `seedManifestS1Scenario` ("S1"), when
   `Columns:["name","id_study_lims","study_accession_number"]` is exported, then
   every row's `id_study_lims` cell is `"S1"` and every row's
   `study_accession_number` cell equals the resolved study's accession number
   (study-constant, from the resolved parent, not per-row queried).

## B. iRODS path attachment, merged-gap, file_type

### B1: irods_path attaches per product; file_type is attachment-only

As an API user, I want `irods_path` attached per product by file-type without
losing objectless products, so a CRAM view still lists every product.

**Package:** `mlwh/` **File:** `mlwh/export.go`
**Test file:** `mlwh/export_test.go` (reuse
`seedManifestStudy7568MergedCRAMScenario`, study "7568", 97 rows = 96 merged
single-lane + 1 direct)

**Acceptance tests:**

1. Given `seedManifestStudy7568MergedCRAMScenario` and a synced cache, when
   `Columns:["name","id_run","lane","tag_index","irods_path"]` and
   `FileType:"cram"` are exported, then `Rows` has length 97 (all products
   present), exactly 1 row has a non-empty `irods_path`
   (`/seq/illumina/runs/49/49348/lane3/plex99/49348_3#99.cram`), and 96 rows
   have blank `irods_path`, and `Total` is 97.
2. Given the same, when `FileType` is unset (empty) with `irods_path` selected,
   then the single direct-object product still attaches a path and `Total` stays
   97 (products stays product-grained; `file_type` does not remove rows).
3. Given the same, when `FileType:"bam"` with `irods_path` selected, then
   `Total` is still 97 (`file_type` never changes `Total`) and the direct CRAM
   object is not attached (its filename does not end in `.bam`), so `irods_path`
   is blank on all 97 rows.

### B2: irods_unmatched / reason are merged-multilane only

**Acceptance tests:**

1. Given `seedManifestStudy7568MergedCRAMScenario`, when
   `Columns:["irods_path","irods_unmatched","reason"]` and `FileType:"cram"` are
   exported, then exactly 96 rows have `irods_unmatched="true"` and
   `reason="merged_multilane"` with blank `irods_path`, and the 1 direct row has
   `irods_unmatched="false"`, `reason=""`, and a non-empty `irods_path`.
2. Given `seedManifestS1Scenario` (objectless products, no merged composite),
   when `Columns:["irods_path","irods_unmatched","reason"]` is exported with
   `FileType` unset, then every row has blank `irods_path`,
   `irods_unmatched="false"`, and `reason=""` (a merely objectless product is
   NOT flagged unmatched).
3. Given `seedManifestStudy7568MergedCRAMScenario`, when
   `Columns:["irods_unmatched"]` and `FileType:"bam"` are exported, then every
   row has `irods_unmatched="false"` (merged-gap detection is gated to
   `file_type` unset-or-`cram`) and `Total` is 97.
4. Given any product export, when the result is inspected, then `ExportResult`
   carries no `products_without_irods` field or any envelope gap summary (the
   product rows are the source of truth).

## C. Product-row filters

### C1: qc, organism, library_type reduce Total

As an API user, I want `qc`/`organism`/`library_type` to filter product rows, so
`Total` reflects the filtered set.

**Package:** `mlwh/` **File:** `mlwh/export.go`
**Test file:** `mlwh/export_test.go`

**Acceptance tests:**

1. Given `seedManifestS1Scenario` with product qc set to 1/0/NULL on its three
   distinct triples, when `QC:"fail"` is exported with default columns, then
   `Rows` has length 1, that row's `manual_qc` is `fail`, and `Total` is 1.
2. Given the same, when `QC:"pending"` is exported, then `Rows` has length 1 and
   `Total` is 1; and `QC:"pass"` yields `Rows` length 1 and `Total` 1 (the three
   filtered totals sum to the unfiltered `Total` 3).
3. Given a study whose products span two `common_name` organisms, when
   `Organism:"<one organism>"` is exported, then `Total` and `len(Rows)` equal
   the number of products for that organism only (a `WHERE` filter, reduces
   `Total`), and products without an iRODS object are still not dropped.
4. Given a study with products under two `pipeline_id_lims` library types, when
   `LibraryType:"<one type>"` is exported, then `Total` and `len(Rows)` reflect
   only that library type.

### C2: deliverables_only uses the product entity_type discriminator

As an API user, I want `deliverables_only` for products to filter by the
product's own deliverable discriminator and never drop objectless products.

**Acceptance tests:**

1. Given a study with 3 Illumina products - 2 whose `iseq_flowcell.entity_type`
   is in (`library`, `library_indexed`) (at least one of them with NO iRODS
   object) and 1 whose `entity_type` is not (a control) - when
   `DeliverablesOnly` is nil, then `Total` is 3; when `DeliverablesOnly` points
   to true, then `Total` is 2 and `Rows` has length 2, and the objectless
   deliverable product is still present (deliverables filter never drops a
   product for lacking an iRODS object; it uses `entity_type`, not the iRODS
   `is_deliverable` flag).
2. Given the same with `DeliverablesOnly` true, when `FileType:"cram"` is also
   set, then `Total` is still 2 (the deliverables filter changes `Total`,
   `file_type` does not).

## D. Pagination

### D1: keyset bounded page returns Total, NextCursor, Complete

As an MCP/API client, I want bounded keyset pages.

**Package:** `mlwh/` **File:** `mlwh/export.go`
**Test file:** `mlwh/export_test.go`

**Acceptance tests:**

1. Given `seedManifestStudy7568MergedCRAMScenario` (97 product rows), when
   `Export(... "products", "study" ..., ExportOptions{Limit: 50})` runs, then
   `Rows` has length 50, `Total` is 97, `NextCursor` is non-empty, and
   `Complete` is false.
2. Given the first page's `NextCursor`, when `ExportOptions{Cursor: nextCursor,
Limit: 50}` runs, then `Rows` has length 47 (the remaining products in
   `(id_run, position, tag_index)` order after the cursor triple), `Complete` is
   true, and `NextCursor` is empty.
3. Given a large product study (helper `seedLargeProductExportScenario(t, db,
1500)`, 1500 distinct triples) and a default request `ExportOptions{}` (no
   `Limit`, no `All`), when it runs, then `Rows` has length 1000
   (`defaultExportAllLimit`), `Total` is 1500, `NextCursor` is non-empty, and
   `Complete` is false (the default HTTP response is a bounded page, NOT the
   full set).

### D2: --all streams the complete set, memory-bounded, no deep OFFSET

**Acceptance tests:**

1. Given `seedLargeProductExportScenario(t, db, 120000)`, when `Export(...
"products" ..., ExportOptions{All: true, Limit: 997})` runs and the result is
   rendered with `RenderTo(ctx, writer)`, then all 120000 data rows are emitted,
   `result.Rows` is nil (not materialised), `result.Total` is -1,
   `result.NextCursor` is empty, `result.Complete` is true, and heap growth
   measured around the render (per the go-conventions memory-bounded pattern) is
   under 20 MiB.
2. Given the same complete stream, when it pages internally, then it advances by
   keyset cursor (each page filters `(id_run, position, tag_index) > last`) and
   never issues a growing `OFFSET` (mirror the iRODS `--all` path; do not use
   `streamExportPages`).

### D3: cursor accepted for products, still rejected for other bounded kinds

**Acceptance tests:**

1. Given a `NextCursor` from a prior products page (as in D1.2), when
   `ExportOptions{Cursor: nextCursor, Limit: 50}` is supplied for products, then
   Export succeeds and does not return the "cursor pagination is supported only
   for iRODS and products exports" error (products is now cursor-capable via
   `validateExportContinuationSupport`).
2. Given a `samples of study` export (regression), when `Cursor` is supplied,
   then Export still returns `ErrUnsupportedIdentifier` with "cursor pagination
   is supported only for iRODS and products exports" (products did not widen
   cursor support to the limit/offset kinds).
3. Given a products export, when `Sort:"created-desc"`, `Since`, or `Until` is
   supplied, then Export returns `ErrUnsupportedIdentifier` (created-date
   sorting/windows stay iRODS-only via the existing `newExportPlan` guard).

## E. Never-synced / unknown-study / synced-empty cascade

### E1: Products signal unknown, never-synced, and synced-empty studies

As a CLI/API user, I want products to signal unknown, never-synced, and
synced-empty studies as the manifest did, so callers degrade cleanly.

**Package:** `mlwh/` **File:** `mlwh/export.go`
**Test file:** `mlwh/export_test.go`

**Acceptance tests:**

1. Given a synced cache with a known study that has no products, when products
   are exported, then the result has `Rows` empty, `Total` 0, `Complete` true,
   and no error.
2. Given a never-synced cache, when products are exported for a name- or
   accession-resolvable study id (e.g. "S1", as the manifest tests use), then
   parent resolution via `ResolveStudy` returns an error satisfying both
   `errors.Is(err, ErrNotFound)` and `errors.Is(err, ErrCacheNeverSynced)`. A
   purely numeric id may take `ResolveStudy`'s numeric branch and return plain
   `ErrNotFound` without the signal (reused framework behavior; not changed).
3. Given a synced cache and an unknown name/accession id (e.g. "NOPE"), when
   products are exported, then parent resolution returns `ErrNotFound` and NOT
   `ErrCacheNeverSynced`.
4. Given a cache where study/sample synced but `iseq_product_metrics` never
   synced for an otherwise-known study, when products are exported, then Export
   returns the joined never-synced sentinel (not a false empty result).
5. Given a synced product-metrics cache WITH products but an iRODS locations
   mirror that has NEVER synced, when products are exported with an
   iRODS-attachment column (`irods_path`/`irods_unmatched`/`reason`) selected,
   then Export surfaces the cache-never-synced signal (`errors.Is(err,
ErrCacheNeverSynced)`) rather than attaching blank paths, mirroring how
   `sample-crams` gates on `syncTableSeqProductIRODSLocations`; without an
   iRODS-attachment column the iRODS mirror sync state is not required.

## F. HTTP endpoint and remote parity

### F1: GET /export/products/study/:id serves bounded and complete responses

As an HTTP/MCP client, I want the endpoint to serve bounded and complete
responses with local/remote parity, so I can page or fetch everything.

**Package:** `mlwh/` **File:** `mlwh/server.go`, `mlwh/remote.go`
**Test file:** `mlwh/server_test.go`, `mlwh/remote_test.go`,
`mlwh/parity_test.go`

**Acceptance tests:**

1. Given a running test server over a seeded cache (97 products), when `GET
/export/products/study/7568?columns=name,irods_path,irods_unmatched,reason&
file_type=cram&limit=50` is issued, then the JSON body has fields `Columns`,
   `Rows` (length 50), `Total` 97, `NextCursor` non-empty, `Complete` false, and
   `Format`.
2. Given the returned `NextCursor`, when `GET
/export/products/study/7568?...&limit=50&cursor=<NextCursor>` is issued, then
   `Rows` has length 47, `Complete` is true, and `NextCursor` is empty.
3. Given `all=true`, when `GET /export/products/study/7568?...&all=true` is
   issued, then the body carries all 97 rows and `Complete` true (the server
   materialises the stream).
4. Given a local `*Client` and a `RemoteClient` over the same seeded cache, when
   products is exported with `Columns:["name","irods_path"]`, `FileType:"cram"`,
   `Limit:100` on both, then the two `ExportResult`s are `reflect.DeepEqual`
   (add a dedicated products-export parity test; the existing `irods` Export
   parity entry stays for acceptance test 10).
5. Given a `RemoteClient` products export with `All:true` on a study larger than
   one page, when it streams, then it pages by `Cursor` (unsorted, `NextCursor`
   present) without a growing `OFFSET`, emitting the complete set.

## G. Manifest surface removed

### G1: manifest CLI, routes, registry, and metadata are gone

As a maintainer, I want the manifest surface fully removed, so there is one
export model and no dead endpoints or metadata.

**Package:** `cmd/`, `mlwh/`
**Test file:** `cmd/mlwh_test.go`, `mlwh/server_test.go`,
`mlwh/registry_test.go`, `mlwh/openapi_test.go`, `mlwh/docs_test.go`

**Acceptance tests:**

1. Given the built CLI, when `wa mlwh manifest 7568` runs, then it exits with a
   cobra unknown-command error (no manifest subcommand is registered).
2. Given a running test server, when `GET /study/S1/manifest` and `GET
/study/S1/manifest/count` are issued, then both return HTTP 404.
3. Given the registry, when it is enumerated, then it contains no entry with
   `Method` `StudyManifest` or `CountStudyManifest`.
4. Given the generated OpenAPI document, when its paths are inspected, then
   neither `/study/:id/manifest` nor `/study/:id/manifest/count` is present.
5. Given `.docs/mcp/api-reference.md` (regenerated), when it is compared with
   `EndpointReference()` in the no-drift test, then they are byte-equal, it
   contains no `manifest` section, and the Export section documents the products
   relationship's keyset/bounded-page semantics.

## H. `wa mlwh info` rewired

### H1: Products section uses the products export; heading shows "of total"

As a `wa mlwh info` user, I want the Products section fed by the products export
with an accurate heading, so truncation past 50 rows is visible.

**Package:** `cmd/` **File:** `cmd/mlwh_info.go`
**Test file:** `cmd/mlwh_info_test.go`

**Acceptance tests:**

1. Given a study-info stub whose `Export` returns two product rows (one Illumina
   run, one with `id_run` 0) with `manual_qc` `pass` and `pending` and `Total`
   2, when `wa mlwh info 5901 --type study` runs, then the stub is called with
   `ExportRelationship{Children:"products", ParentKind:"study"}`, the default 8
   product columns, `Limit == infoMaxRelated`, no `irods_path` column, and the
   text output contains "Products (2)", the sample names, "manual_qc=pass", and
   "manual_qc=pending".
2. Given a study-info stub whose `Export` returns 2 rows but `Total` 780, when
   `wa mlwh info 5901 --type study` runs, then the text output contains
   "Products (2 of 780)" (the heading uses `ExportResult.Total`, fixing the
   old always-"Products (N)" bug).
3. Given the same stub, when `wa mlwh info 5901 --type study --json` runs, then
   the decoded JSON has a `products` array of typed objects each carrying the 8
   default fields (`name`, `supplier_name`, `accession_number`,
   `sanger_sample_id`, `id_run`, `lane`, `tag_index`, `manual_qc`) and has NO
   `study_manifest` field.

## I. CLI export products rendering

### I1: TSV/CSV/JSON rendering; bounded page states its mode

As a CLI user, I want `export products` to render like other exports and to
announce bounded pages, so nothing is silently truncated.

**Package:** `cmd/` **File:** `cmd/mlwh_export.go`
**Test file:** `cmd/mlwh_export_test.go`

**Acceptance tests:**

1. Given a stub export client returning a known products `ExportResult`, when
   `wa mlwh export products study S1 --columns name,supplier_name,id_run,lane,
tag_index,manual_qc,irods_path,irods_unmatched,reason --file-type cram` runs
   with default format, then stdout is the TSV header row followed by one line
   per row (no `study_manifest` object, no trailing success message).
2. Given the same, when `--format csv` and `--format json` (or `--json`) are
   used, then stdout is the comma-delimited form and the JSON row-array form
   respectively (the normal `RenderAsTo` output).
3. Given the same stub returning `Complete:false` and a non-empty `NextCursor`,
   when `--limit 50` is used, then stdout carries exactly the page rows and
   stderr states a bounded page was emitted with the `--cursor <NextCursor>`
   continuation; when the stub returns `Complete:true`, stderr states it was the
   final page; when the stub returns `Complete:false` with an EMPTY `NextCursor`
   (a limit/offset relationship), stderr states a bounded page but prints no
   `--cursor` hint.
4. Given no `--limit`, when a products export runs, then the client is called
   with `All: true` and no bounded-page status line is written (the complete-set
   default is unchanged).
5. Given the built CLI, when `wa mlwh export --help` runs, then the Children
   section lists `products` with its product-grained description and its
   parent-kind `study`, and the Columns section lists the products default
   projection and available columns (auto-generated from
   `ExportRelationshipDescriptions()` / `ExportColumnVocabularies()`); no
   `manifest` subcommand appears under `wa mlwh --help`.

## J. Existing exports unchanged (regression)

### J1: irods and sample-crams behaviour is preserved

As an existing user, I want `irods` and `sample-crams` exports unchanged, so
this feature is non-breaking for them.

**Package:** `mlwh/` **File:** `mlwh/export.go`
**Test file:** `mlwh/export_test.go`

**Acceptance tests:**

1. Given the existing `irods of study` tests (default columns
   `["supplier_name","sanger_sample_id","manual_qc","irods_path"]`, keyset
   cursor pages, created-desc sort/window, merged-composite honesty), when they
   run unchanged, then they still pass.
2. Given the existing `sample-crams of study` tests (default columns
   `["name","accession_number","irods_path","merged"]`, one merged-aware CRAM
   per sample, limit/offset), when they run unchanged, then they still pass.
3. Given an `irods of study` or `sample-crams of study` export, when
   `Columns:["irods_unmatched"]` or `Columns:["reason"]` is requested, then
   Export returns an `unknown export column` error (the products vocabulary does
   not leak into the file vocabularies).

## K. Documentation

### K1: README and glossary present products, not manifest

As a reader, I want the docs to describe product export instead of manifest, so
guidance matches the code.

**Package:** docs
**File:** `README.md`, `.docs/mcp/glossary.md`
**Test file:** `mlwh/docs_test.go`

**Acceptance tests:**

1. Given `README.md`, when it is read, then it contains no `wa mlwh manifest`
   reference and instead documents `wa mlwh export products study <id>` as
   product-grained (includes products with no iRODS path), `export irods` as
   file-object-grained, and `export sample-crams` as sample-grained.
2. Given `.docs/mcp/glossary.md`, when it is read, then the "Data manifest"
   concept section, its anchor, and its `[data manifest](#data-manifest)`
   cross-references are all removed (no dangling anchors),
   `/study/:id/manifest` is no longer listed, and product export is documented.
3. Given `mlwh/docs_test.go`, when its glossary-concept test
   `TestGlossaryDefinesPeopleAndManifestConceptsG2` runs after K1's glossary
   edit, then it no longer requires the removed `"data manifest"` term and
   instead asserts the new product-export concept heading K1 adds (e.g. term
   `"product export"`), keeping the existing `"file-type filter (filename
suffix)"`, `"faculty sponsor"`, `"study_users / role membership"`, `"manual
qc"`, and `"data access group"` assertions. The test may be renamed to
   reflect product export.

## Implementation Order

Ordered so each phase builds on tested foundations; consumers are rewired onto
the products export before the manifest surface is deleted (to avoid compile
breaks).

1. **Products export core (A, B, E).** Refactor the `manifest.go` query
   builders, `scanManifestRow`, product-grain SQL, and
   `countStudyManifestProducts` into a products export path: add the
   relationship, `productExportVocabulary`, `exportProductRow`, the keyset
   bounded page (`exportProducts`), `Total`, the iRODS attachment + merged-gap +
   `file_type`-as-attachment (`exportFileFilters` restructure +
   `exportRelationshipAttachesFileType`), and the empty/never-synced cascade.
   Keep the old `StudyManifest` working via shared helpers for now. Tests reuse
   `seedManifestS1Scenario` and `seedManifestStudy7568MergedCRAMScenario`.
2. **Filters (C).** Extend `validateExportFilterSupport` and the products page +
   total for `qc` (HAVING), `library_type`, `organism` (WHERE), and
   `deliverables_only` (entity_type). Add the deliverables fixture. Sequential
   after phase 1.
3. **Pagination completeness (D, F remote).** Extend
   `validateExportContinuationSupport` + `newExportPlan` cursor decode for
   products; add `exportProductsAll` + `streamExportProductsRows`; the default
   bounded page; the memory-bounded `--all` stream; add
   `seedLargeProductExportScenario`. Confirm `remoteExportCanPageAll` already
   covers products; add products-export local/remote parity. Sequential after
   phase 1; parallel with phase 2.
4. **CLI (I).** Render products; add `--limit`/`--cursor` and the bounded-page
   status line without changing the no-`--limit` default; update help text.
   Depends on phases 1-3.
5. **info rewire (H).** Swap `mlwhInfoClient.StudyManifest` for `Export`; fetch
   the bounded products page; move row rendering into info; fix the heading via
   `ExportResult.Total`; replace the `study_manifest` JSON field with a
   `products` array. Depends on phase 1. Must precede phase 6 (removes info's
   dependence on `StudyManifest`/`writeManifestRow`).
6. **Manifest surface removal (G).** Delete `cmd/mlwh_manifest.go` +
   registration and `cmd/mlwh_manifest_test.go`; remove the registry entries,
   server cases, remote methods, queryer methods, count methods, and types;
   delete the residual `StudyManifest` method/helpers in `manifest.go`; edit the
   parity table (drop the two manifest entries); update the affected `mlwh`
   tests. Depends on phases 4 and 5.
7. **Docs (F Description, G1.5, K).** Edit the single Export entry `Description`
   and the shared `cursor` param description; regenerate
   `.docs/mcp/api-reference.md`; update `README.md` and `.docs/mcp/glossary.md`.
   Last, after the registry is final.

## Appendix: Key Decisions

- **Keyset, not limit/offset (Notes-binding).** Products is large-scale
  (per-study product rows reach millions on the real mirror), so limit/offset
  `--all` (`streamExportPages`) would be O(n^2) scan-and-discard with a
  recomputed COUNT per page. Reusing the iRODS keyset machinery avoids deep
  OFFSET and keeps `--all` memory-bounded.
- **Manifest grain preserved.** Row identity is the distinct `(id_run, position,
tag_index)` triple from the manifest `GROUP BY`; no `id_iseq_product`
  tiebreaker. This keeps `Total` and the reused fixture counts stable (study
  "S1" = 3, study "7568" fixture = 97) and reuses the int64 `exportCursor`
  (4th field 0).
- **ExportResult serialization is frozen.** No JSON tags / marshaller are added;
  products reuses the identical `Columns/Rows/Total/NextCursor/Complete/Format`
  wire shape so existing exports (acceptance test 10) do not change.
- **Dedicated products vocabulary.** A separate `exportVocabulary` keeps
  `irods_unmatched`/`reason` and product grain out of the `irods`/`sample-crams`
  file vocabularies.
- **file_type is attachment-only for products.** No `cram` default, no forced
  `deliverables_only`, never drops a product row; `file_type` never changes
  `Total`. `deliverables_only`, `qc`, `library_type`, `organism` are product-row
  filters and DO change `Total`.
- **No `products_without_irods`.** The rectangular row set is the source of
  truth; callers count blank `irods_path` or `irods_unmatched` rows.

**Testing strategy.** GoConvey (`So(...)` in independent `Convey` blocks),
copyright headers on new files, `t.TempDir()` for filesystem ops, no `So()` in
loops over 20 iterations (count then assert). Reuse the existing seed helpers
(`seedManifestS1Scenario`, `seedManifestStudy7568MergedCRAMScenario`,
`seedManifestPagedGapScenario`, `newExportTestClient`) and mirror the existing
keyset test `TestExportStudyIRODSBoundedPageTotalAndCursorD1a` and memory-bound
test `TestExportStudyIRODSAllUsesKeysetWithoutCappingRowsD1a`. Every acceptance
test above maps to a GoConvey test; no stubs, hardcoded results, or build-tag
exclusions. See **go-conventions**, **testing-principles**, **go-implementor**,
and **go-reviewer**.
