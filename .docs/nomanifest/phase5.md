# Phase 5: info rewire

Ref: [spec.md](spec.md) sections H1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

Dependencies: phase 1 (products export core). This phase MUST precede
phase 6: it removes `wa mlwh info`'s dependence on the `StudyManifest`
method and `writeManifestRow`, which phase 6 deletes.

## Items

### Item 5.1: H1 - Products section uses the products export; heading shows "of total"

spec.md section: H1

`cmd/mlwh_info.go`: replace the `mlwhInfoClient.StudyManifest` method
(~line 81) with `Export(ctx, rel mlwh.ExportRelationship, parentID
string, opts mlwh.ExportOptions) (mlwh.ExportResult, error)`. At the
Products call site (~line 1357) fetch a bounded page of `infoMaxRelated`
(50) rows with the default 8 product columns and `Limit: infoMaxRelated`
and NO `irods_path` (the non-iRODS view, matching today's
`withIRODS=false`). Move per-row rendering into info as a local helper
that reads cells by column name from the `ExportResult` row and produces
the same `key=value` line the old `writeManifestRow` emitted for the 8
fields. Fix the heading (~line 801) to pass `ExportResult.Total` as
`total` to `infoListHeading("Products", shown, total)` so a study with
more than 50 products renders "Products (50 of 780)" instead of always
"Products (N)". In `info --json` (`infoReport`, ~line 1436) remove the
`StudyManifest *mlwh.StudyManifest` field (~line 1452) and add a `cmd`
-local `infoProductRow` typed `products` array carrying the 8 default
fields mapped from the `ExportResult` rows by column (a deliberate,
user-visible breaking change to `info --json`). Covers all 3 acceptance
tests H1.1-H1.3 (stub called with the products relationship, 8 columns,
`Limit == infoMaxRelated`, no `irods_path`, and text shows "Products
(2)", sample names, and the manual_qc cells; `Total` 780 -> "Products (2
of 780)"; `--json` has a typed `products` array and no `study_manifest`
field). Test file: `cmd/mlwh_info_test.go`.

- [ ] implemented
- [ ] reviewed
