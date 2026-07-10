# Feature: replace the study manifest API/CLI with product-grained export rows

## Summary

The existing MLWH study manifest surface predates the generic export API. It now
duplicates part of `export` while keeping a separate endpoint, CLI command,
typed envelope, fixed columns, and special gap summary. Replace it with a single
product-grained export relationship and remove the manifest surface entirely.

This is a breaking change by design. **Do not preserve backward compatibility.**
Remove the manifest endpoint, manifest count endpoint, manifest CLI command, and
manifest-specific types/methods/docs rather than adding aliases or compatibility
wrappers.

The goal is:

```bash
wa mlwh export products study 7568 \
  --file-type cram \
  --columns name,supplier_name,accession_number,sanger_sample_id,id_run,lane,tag_index,manual_qc,irods_path,irods_unmatched,reason
```

instead of:

```bash
wa mlwh manifest 7568 --with-irods --file-type cram
```

and:

```http
GET /export/products/study/7568?file_type=cram&columns=name,supplier_name,accession_number,sanger_sample_id,id_run,lane,tag_index,manual_qc,irods_path,irods_unmatched,reason
```

instead of:

```http
GET /study/7568/manifest?with_irods=true&file_type=cram
GET /study/7568/manifest/count
```

## Background

Current code has both:

- Generic export:
    - HTTP: `GET /export/:children/:parent_kind/:parent_id`
    - CLI: `wa mlwh export <children> <parent-kind> <parent-id>`
    - Result type: `ExportResult` with selectable `columns`, rectangular `rows`,
      `total`, `next_cursor`, `complete`, and `format`
    - Current relationships include `irods`, `samples`, `runs`, `libraries`,
      `lanes`, `studies`, `users`, and `sample-crams`
- Manifest:
    - HTTP: `GET /study/:id/manifest`
    - HTTP: `GET /study/:id/manifest/count`
    - CLI: `wa mlwh manifest <study>`
    - Result type: `StudyManifest` envelope containing study metadata once,
      fixed `ManifestRow` fields, and `products_without_irods`

The manifest's one useful non-export behavior is its row grain: it starts from
study products, so products without a matching iRODS object still appear. That
row grain should move into export as `products of study`.

The manifest envelope is not a sufficient reason to keep a separate surface:
study metadata can be fetched through the normal study detail/overview/info
paths. Product rows should be exported as product rows.

## Required Change

Add a new generic export relationship:

```text
products of study
```

Supported forms:

```http
GET /export/products/study/:id
```

```bash
wa mlwh export products study <id>
```

The new relationship must be implemented in the existing export framework, not
as a new manifest-shaped endpoint under another name.

Remove these public surfaces:

- `GET /study/:id/manifest`
- `GET /study/:id/manifest/count`
- `wa mlwh manifest`
- registry entries for `StudyManifest` and `CountStudyManifest`
- remote-client calls dedicated to manifest
- manifest-specific OpenAPI/MCP descriptions
- README/help text that advertises `manifest`

Remove or refactor manifest-specific internal types and files as appropriate:

- `StudyManifest`
- `ManifestRow`, unless it is renamed/refactored into an export-internal product
  row type
- `mlwh/manifest.go`, unless its query logic is moved/refactored into the export
  product implementation
- `cmd/mlwh_manifest.go`

No aliases, redirects, hidden commands, compatibility wrappers, or "deprecated
but still works" behavior should remain.

## Product Export Semantics

`export products study <id>` is product-grained.

The base row set is the study's sequencing products from product metrics, scoped
by the product-metrics `id_study_lims`. It must not be iRODS-object-grained and
must not drop products just because no matching iRODS object exists.

Rows should be ordered deterministically by:

```text
id_run, lane, tag_index, name
```

Default columns should be the old manifest essentials:

```text
name,supplier_name,accession_number,sanger_sample_id,id_run,lane,tag_index,manual_qc
```

Supported selectable columns should include at least:

```text
name
supplier_name
accession_number
sanger_sample_id
id_run
lane
tag_index
manual_qc
irods_path
irods_unmatched
reason
```

Column aliases:

- `position` aliases `lane`
- `supplier_sample_name` aliases `supplier_name`

`manual_qc` keeps the current product-row semantics: use the product QC roll-up
string (`pass`, `fail`, `pending`) via the existing `qc.go` behavior.

## iRODS Path Semantics

`irods_path` is just another selectable export column. There is no `with_irods`
boolean.

If `irods_path` is requested, attach the matching product iRODS path by the same
set-at-once join currently used by the manifest implementation: product metrics
to `seq_product_irods_locations_mirror` by `id_iseq_product` and `id_study_lims`,
with deterministic collapse when several iRODS rows exist for one product.

If no matching object exists, keep the product row and emit an empty
`irods_path`.

The same is true when `file_type` / `--file-type` is supplied: the file-type
restriction filters the eligible iRODS object considered for `irods_path`; it
does not transform the export into an iRODS-object listing and it must not remove
the product row solely because the path is missing. In other words, product
export remains product-grained. A CRAM-focused product export should show every
product in the study, with blank `irods_path` for products that have no matching
CRAM object.

Do not default product export to CRAM. `file_type` is explicit. If no
`file_type` is supplied and `irods_path` is requested, attach any deterministic
object for the product, matching current manifest behavior.

`export irods ...` remains the file-object-grained export. Users who want only
actual files should use `export irods`; users who want product rows, including
products without files, should use `export products`.

## Missing iRODS Rows

Do not carry `products_without_irods` into the export result.

Reason: export is a rectangular row set, not an envelope. A summary count would
recreate the manifest's special result shape and would be ambiguous under
paging unless it was carefully defined as page-only vs full-scope. The product
rows themselves are the source of truth.

Instead:

- every product without a matching iRODS object remains one output row
- `irods_path` is blank for that row
- `irods_unmatched` may be selected to show `true` for known missing-path cases
- `reason` may be selected to show a stable reason such as `merged_multilane`

If a caller wants the count of products without iRODS, they can export all
matching product rows and count blank `irods_path` values or `irods_unmatched`
rows. Do not add a full-scope `products_without_irods` summary field.

For merged multi-lane CRAMs, preserve the current honest behavior: do not copy a
single merged/composite path onto every single-lane product row. Leave
`irods_path` blank on the unmatched product rows and surface the cause with
`irods_unmatched=true` and `reason=merged_multilane` when those columns are
selected.

## Export Options

The product export should use the existing export options where they make
sense:

- `columns`
- `format`
- `json`
- `limit`
- `offset`
- `all`
- `file_type` / `--file-type`
- `qc`
- `library_type`
- `organism`

The spec-writer should decide whether cursor pagination is required for
`products` immediately or whether bounded `limit`/`offset` plus `--all` is
acceptable, but it must preserve the "no silent truncation" rule: the CLI must
state when it emitted a bounded page and must offer a complete export mode.

`deliverables_only` should not be blindly inherited from iRODS export semantics.
If supported for products, it must be defined as a product-row filter over the
product's deliverable discriminator. It must not silently drop product rows
because no iRODS path exists.

Created-date sorting/windowing should remain iRODS-specific unless the spec
defines a clear product-date basis. Do not invent a product recency meaning by
accident.

## Output Shape

HTTP `GET /export/products/study/:id` returns the existing `ExportResult` shape:

```json
{
    "columns": [
        "name",
        "supplier_name",
        "id_run",
        "lane",
        "tag_index",
        "manual_qc",
        "irods_path",
        "irods_unmatched",
        "reason"
    ],
    "rows": [
        [
            "DN1",
            "supplier-1",
            "49348",
            "1",
            "7",
            "pass",
            "",
            "true",
            "merged_multilane"
        ]
    ],
    "total": 780,
    "next_cursor": "",
    "complete": true,
    "format": "json"
}
```

The CLI renders through the existing export renderers:

- TSV by default
- CSV with `--format csv`
- JSON with `--json` / `--format json`

JSON CLI output should be the normal export JSON row-array behavior, not a
`StudyManifest` object.

## Documentation Requirements

Update docs/help/registry text to present the new model clearly:

- `export products study <id>` is product-grained and includes products with no
  iRODS path
- `export irods ...` is file-object-grained and contains only actual iRODS data
  objects
- `export sample-crams study <id>` is sample-grained and merged-aware, one CRAM
  per sample
- `file_type` on product export filters the eligible path attachment, not the
  product row set
- blank `irods_path` is expected for products without a matching object
- use optional `irods_unmatched` and `reason` columns to explain known gaps
- `manifest` no longer exists

Remove all README/help references that suggest `wa mlwh manifest` is available.

## Acceptance-Test Intent

The eventual spec should include acceptance tests for at least:

1. `GET /export/products/study/S1` returns `ExportResult` with default product
   columns and one row per product, including products with no iRODS object.
2. `GET /export/products/study/S1?columns=...irods_path...&file_type=cram`
   returns all product rows and leaves `irods_path` blank for products without a
   matching CRAM object.
3. `irods_unmatched` and `reason` columns identify merged multi-lane CRAM gaps
   without adding any envelope summary.
4. `total` equals the full product-row count for the export relationship.
5. `wa mlwh export products study S1 --columns ... --file-type cram` renders
   TSV/CSV/JSON through the normal export path.
6. `wa mlwh manifest` is not registered and reports an unknown command.
7. `GET /study/S1/manifest` and `/study/S1/manifest/count` return 404.
8. OpenAPI/registry/MCP metadata no longer lists `StudyManifest`,
   `CountStudyManifest`, or manifest endpoints.
9. README and CLI help list `export products` instead of `manifest`.
10. Existing `export irods` and `export sample-crams` behavior remains unchanged.

## Non-Goals

- No backward-compatible manifest endpoint.
- No backward-compatible manifest CLI command.
- No manifest-shaped export response.
- No `products_without_irods` summary field.
- No path duplication from merged CRAMs onto single-lane product rows.
- No caller-side SQL workaround as the official answer.

## Notes

These notes resolve decisions raised during clarification. They are binding
requirements for the spec.

### Pagination: keyset cursor (not limit/offset)

`products of study` must use **keyset cursor pagination**, the same model the
`irods` export already uses via `validateExportContinuationSupport` — NOT the
limit/offset model used by `sample-crams` and the other bounded relationships.

Rationale (confirmed against the real MLWH mirror): products is a large-scale
relationship, not a small per-parent list. Per-study product-row counts reach
~3.03M (study 6187), 875 studies exceed 1k products, 83 exceed 10k, and study
7699 has ~153k product rows — more than its ~52k iRODS objects, because
objectless products inflate the count. Limit/offset `--all` streaming
(`streamExportPages`) loops with a growing `OFFSET` and recomputes the `total`
COUNT per page, so a complete export of a large study would be O(n^2)
scan-and-discard with thousands of COUNTs — the exact pathology keyset was
designed to avoid (realworld3 "no silent truncation", memory-bounded HARD REQ).

Requirements:

- Row grain is the manifest's grain: one row per distinct
  `(id_run, position, tag_index)` triple, produced by reusing the manifest's
  `GROUP BY ipm.id_run, ipm.position, ipm.tag_index` (which collapses the
  sample/iRODS fan-out, and — as today — collapses composite/merged products
  that share a triple). Preserve this grain; do NOT switch to a per-
  `id_iseq_product` grain: it would change `total` and break the reused
  `manifest_test.go` fixture counts (e.g. study 7568 is ~780 distinct triples,
  not its ~828 raw product-metrics rows).
- Because the `GROUP BY` makes the triple unique per output row, the keyset IS
  the triple: `ORDER BY id_run, position, tag_index` with a row-value keyset
  `(id_run, position, tag_index) > (?, ?, ?)` (the same dialect-portable
  row-value comparison the iRODS keyset already uses). No extra tiebreaker is
  needed — do NOT add an `id_iseq_product` tiebreaker; it is not in the grain
  and is unnecessary for monotonicity. `MIN(sm.name)` stays only as the cosmetic
  final `ORDER BY` term the manifest already uses; it does not affect row
  identity or the cursor.
- Reuse the existing `exportCursor` int64 machinery: the triple maps onto its
  integer fields (the iRODS-only 4th component is unused/zero for products), so
  NO new TEXT-capable cursor encoding is required.
- `total` is the full count of distinct `(id_run, position, tag_index)` product
  rows for the relationship (acceptance test 4), matching the manifest's
  product-grained count; `file_type` never changes it.
- Extend `validateExportContinuationSupport` so `cursor` is accepted for
  `products` (currently iRODS-only).
- A bounded page returns `Total`, `NextCursor`, and `Complete`; `--all` /
  `all=true` streams the complete set memory-bounded via keyset (no deep
  OFFSET), mirroring the iRODS `--all` path. The CLI must state whether it
  emitted a bounded page or the complete set (CLI currently always sends
  `All: true`).

### Bounded pages for MCP / small-data clients

`GET /export/products/study/:id` must serve small bounded pages so MCP-style
clients that cannot handle large results can page through:

- Default (no paging params) returns a bounded page — `limit` defaults to 0 and
  becomes the internal `defaultExportAllLimit` (1000) — with `total`,
  `next_cursor`, and `complete:false`. It must NOT dump the full result set by
  default.
- A client sets `limit` for a smaller page (e.g. `?limit=50`) and passes
  `cursor=<next_cursor>` to fetch the next page; `complete:true` with an empty
  `next_cursor` marks the last page.
- `all=true` is the opt-in complete stream (used by the CLI); MCP clients simply
  omit it. `limit`, `offset`, `all`, and `cursor` are already advertised via
  `exportQueryParams()`.
- `Export` is a SINGLE generic registry entry
  (`/export/:children/:parent_kind/:parent_id`); OpenAPI/MCP/api-reference derive
  from that entry's `Description` plus the shared `exportQueryParams()` param
  descriptions. There is NO per-relationship OpenAPI channel
  (`exportRelationshipSpec.Description` feeds only CLI help). Therefore:
    - State products' paging semantics in the single Export endpoint
      `Description`: products is product-grained (one row per distinct
      `(id_run, position, tag_index)`, including products with no iRODS object),
      keyset-cursor paginated; the default response is a bounded page (≤ the
      internal 1000 default) carrying `total`, `next_cursor`, and `complete`;
      pass `cursor` to continue; `all=true` returns the complete set; `file_type`
      only restricts the attached `irods_path`. This mirrors how the
      per-relationship `columns` vocabulary already lives inside the shared
      endpoint metadata.
    - Generalise the Export `cursor` query-param description (currently
      "opaque keyset cursor returned by a previous iRODS export page") to cover
      "a previous iRODS or products export page".
    - Do NOT rewrite the shared `fetchAllPaginationParams()` `limit` wording
      ("defaults to a fetch-all page that returns every matching row"): it is
      used by ~20 unrelated endpoints and is out of scope. The Export
      `Description` clause above is what makes products' documented behaviour
      accurate.
    - Regenerate the `.docs/mcp/api-reference.md` no-drift fixture; the diff is
      scoped to the Export endpoint section.

### `irods_unmatched` / `reason`: merged-multilane only

Selecting `irods_path`, `irods_unmatched`, or `reason` triggers the set-at-once
product→iRODS join. `irods_unmatched=true` (with `reason=merged_multilane`) is
emitted ONLY for the classified merged multi-lane CRAM gap — the current
`manifest.go` behavior (`detectMergedCRAMGap` /
`manifestListIRODSUnmatchedExpression`: a single-lane product with no direct
object whose sample has a merged composite object). A product that is merely
objectless (never sequenced/delivered) leaves `irods_path` empty and
`irods_unmatched`/`reason` blank/false. Merged-gap detection stays gated to
`file_type` unset-or-`cram`; other file types never flag unmatched.

### Columns: dedicated products vocabulary, 11 required + product-safe extras

products gets its own export vocabulary (a new `exportVocabulary`), NOT
`fileExportColumns()`, so `irods_unmatched`/`reason` and product grain never
leak into the `irods`/`sample-crams` file exports (protects acceptance test 10).

- Required selectable columns (all 11): `name`, `supplier_name` (alias
  `supplier_sample_name`), `accession_number`, `sanger_sample_id`, `id_run`,
  `lane` (alias `position`), `tag_index`, `manual_qc`, `irods_path`,
  `irods_unmatched`, `reason`.
- Default projection = the first 8:
  `name,supplier_name,accession_number,sanger_sample_id,id_run,lane,tag_index,manual_qc`.
- Also expose product-safe extras: `id_study_lims` and `study_accession_number`
  (both cleanly product/study-grained). Include `platform` only if it is
  cleanly derivable at product grain; otherwise omit it rather than invent it.
- Do NOT add iRODS-object-grained columns (`created`, `merged`, `collection`,
  `data_object`, `id_product`, ...) to products.

### Filters: qc, library_type, organism, and product-grain deliverables_only

Extend `validateExportFilterSupport` so `products` supports `qc`,
`library_type`, `organism`, and `deliverables_only`, all as product-row filters.

- `deliverables_only` for products MUST use the product's own deliverable
  discriminator (Illumina `iseq_flowcell.entity_type IN ('library',
'library_indexed')`, Element/Ultima `is_sequencing_control=0`, pass-through
  for PacBio/ONT — the same per-product `deliverable` definition already
  documented for `/study/:id/irods`), NOT the iRODS `is_deliverable` flag. It
  must never drop a product that has no iRODS object.
- `qc` filters against the product's rolled-up `manual_qc` (the `qc.go`
  roll-up), not a raw per-object qc.
- `total` reflects the filtered product-row count (acceptance test 4).
  `file_type` NEVER changes `total` (product grain); `qc`, `library_type`,
  `organism`, and `deliverables_only` do.

### `file_type` on products is path-attachment only

products must be `file_type`-aware WITHOUT the file-export defaults. Current
`exportFileFilters` / `exportRelationshipUsesFileType` force `file_type` to
`cram` and `deliverables_only` to true for file exports; products must do
NEITHER. `file_type` only restricts which iRODS object is eligible for the
`irods_path` attachment (and merged-gap detection); it must not default to
`cram`, must not force `deliverables_only`, and must not drop product rows.

### Internal placement, removals, and cascade

- Reuse the proven `manifest.go` query logic (the product grain, the
  set-at-once ranked iRODS derived-table join on shared `id_iseq_product` +
  `id_study_lims`, and merged-gap detection) by refactoring it into the export
  product implementation rather than rewriting it. Rename `ManifestRow` into an
  export-internal product row type (or fold it into the export projection).
- Remove the `StudyManifest` envelope, `products_without_irods`,
  `cmd/mlwh_manifest.go`, the `StudyManifest`/`CountStudyManifest` registry
  entries, their `Queryer`/remote methods, and `manifestQueryParams` /
  `with_irods`, per the prompt's removal list.
- The never-synced / unknown-study / synced-empty cascade should reuse the
  export framework's existing cache-never-synced signaling and the manifest's
  product-metrics sync gating: unknown study → not_found; never-synced →
  not_found + cache-never-synced signal; synced study with no products → empty
  rows with `total` 0 and `complete:true`.

### Test data

Reuse the existing merged-multilane and products-without-iRODS fixtures from
`manifest_test.go` for the new acceptance tests, and regenerate the
`.docs/mcp/api-reference.md` (or equivalent) no-drift fixture after the registry
changes.

### `wa mlwh info` must be rewired (collateral consumer) + heading bug fix

`wa mlwh info` is the ONLY non-manifest consumer of the removed surface and must
be rewired, not left broken. Today `cmd/mlwh_info.go` declares
`StudyManifest(...)` in its client interface (line 81), calls it (line 1357) to
fill a study "Products" section, renders rows via `writeManifestRow` (which
lives only in the deleted `cmd/mlwh_manifest.go`), and serialises the
`study_manifest` envelope in `wa mlwh info --json` (line 1452).

Required changes:

- Rewire the Products section to the new product-grained export (the existing
  `Export` path / the refactored product query), replacing the
  `StudyManifest(...)` client-interface method. Fetch a bounded page of
  `infoMaxRelated` (50) rows with the default product columns
  (`name,supplier_name,accession_number,sanger_sample_id,id_run,lane,tag_index,manual_qc`);
  do NOT request `irods_path` (info uses the non-iRODS view, matching today's
  `withIRODS=false` call).
- Move the per-row rendering into info (or a shared helper); `writeManifestRow`
  disappears with `cmd/mlwh_manifest.go`.
- In `wa mlwh info --json`, remove the `study_manifest` envelope field and
  replace it with a `products` array of typed product-row objects carrying the
  eight default fields, consistent with the typed-array shape of the sibling
  `samples` / `runs` / `lanes` / `irods_paths` sections. This is a deliberate,
  user-visible breaking change to `info --json`.
- **Heading bug fix:** the Products heading currently passes `total = 0` to
  `infoListHeading(label, shown, total)` (line 801), so it always renders
  "Products (N)" even when the study has more products than the shown 50 (the
  `StudyManifest` envelope carried no total count). Use the products export's
  `ExportResult.Total` (the full distinct-`(id_run, position, tag_index)` count)
  as `total` so the heading renders "Products (50 of 780)" via the existing
  "shown of total" path the Runs/Samples/Libraries sections already use. Add an
  acceptance test: a study with more than `infoMaxRelated` products shows
  "Products (<shown> of <total>)" in text output. (The "iRODS paths" section at
  line 954 has the same latent `total = 0` issue but is out of scope for this
  feature.)
- Update `cmd/mlwh_info_test.go`: the stub currently returns
  `mlwh.StudyManifest`; retype it to the products export, update
  `TestMLWHInfoStudyShowsProgrammeAndManualQCProducts`, and assert the new
  "of total" heading.

### Complete manifest-surface removal / rewire map

Remove (production):

- `cmd/mlwh.go` line 378 — the `newMLWHManifestCommand()` registration.
- `cmd/mlwh_manifest.go` — the entire file (command, `runMLWHManifest`,
  `writeManifestJSON` / `writeManifestText` / `writeManifestHeader` /
  `writeManifestRow`, `mlwhManifestClient`, the open-client helpers).
- `mlwh/types.go` — delete `StudyManifest` and `PagedStudyManifest`; rename
  `ManifestRow` into the export-internal product row type (or fold it into the
  export projection). Delete `ProductsWithoutIRODS`.
- `mlwh/registry.go` — the `StudyManifest` and `CountStudyManifest` entries and
  `manifestQueryParams()` (with its `with_irods` param).
- `mlwh/remote.go` — `StudyManifest`, `StudyManifestPage`, `CountStudyManifest`
  and `remoteManifestQuery`.
- `mlwh/server.go` — the `StudyManifest` and `CountStudyManifest` handler cases,
  `writeMLWHStudyManifest`, and the manifest count helper.
- `mlwh/queryer.go` — the `StudyManifest` and `CountStudyManifest` interface
  methods.
- `mlwh/count.go` — `CountStudyManifest` and `countStudyManifestForEmptyStudy`.

Reuse (do NOT delete):

- `mlwh/count.go`'s `countStudyManifestProducts` helper (distinct
  `(id_run, position, tag_index)` count) is exactly the products export `total`
  source — reuse/rename it rather than deleting it.

Update / replace tests:

- Delete `cmd/mlwh_manifest_test.go`.
- Repurpose `mlwh/manifest_test.go` fixtures for the products export tests.
- Update `mlwh/count_test.go`, `mlwh/registry_test.go`, `mlwh/server_test.go`,
  `mlwh/types_test.go`, `mlwh/cache_mysql_integration_test.go`, and
  `cmd/mlwh_info_test.go`.
- `mlwh/parity_test.go` — replace the manifest local/remote parity coverage with
  products-export parity.
