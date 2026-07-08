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
    ["DN1", "supplier-1", "49348", "1", "7", "pass", "", "true", "merged_multilane"]
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

