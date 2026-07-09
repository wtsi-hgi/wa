# Phase 4: CLI export products rendering

Ref: [spec.md](spec.md) sections I1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

Dependencies: phases 1-3 (the products export path, its keyset
pagination, and the `--all` stream must exist).

## Items

### Item 4.1: I1 - TSV/CSV/JSON rendering; bounded page states its mode

spec.md section: I1

`cmd/mlwh_export.go`: render products via the existing `RenderAsTo` path;
add `--limit` (int) and `--cursor` (string) flags. In
`mlwhExportFlags.options`, set `All: true` ONLY when `--limit` is unset
(preserving today's complete-set default and acceptance test 10 for
every relationship); when `--limit > 0`, set `All: false`, `Limit`, and
`Cursor`, and after the data write one stderr status line stating a
bounded page was emitted (and that omitting `--limit` exports
everything), with a `--cursor <NextCursor>` continuation hint ONLY when
`NextCursor` is non-empty and a final-page statement when `Complete`;
the no-`--limit` path emits nothing extra. Update
`mlwhExportOptionsHelp`: products is product-grained; `file_type` only
restricts the attached `irods_path` (no `cram` default, no forced
deliverables); blank `irods_path` is expected; use `irods_unmatched`/
`reason` for known gaps; document `--limit`/`--cursor`; add a
`wa mlwh export products study 7568 --file-type cram` example with the
full 11-column projection. The Children and Columns help blocks
auto-generate from `ExportRelationshipDescriptions()` /
`ExportColumnVocabularies()`. Covers all 5 acceptance tests I1.1-I1.5
(default TSV header + rows, no `study_manifest`, no success message; CSV
and JSON forms; `--limit` bounded-page stderr with/without a `--cursor`
hint and the final-page case; no-`--limit` -> `All: true` and no status
line; `--help` lists `products` and no `manifest` subcommand). Test
file: `cmd/mlwh_export_test.go`.

Note on I1.5: the "no `manifest` subcommand under `wa mlwh --help`"
clause depends on the manifest removal scheduled for phase 6 (G1). At
this phase's boundary the `manifest` subcommand is still registered
(`cmd/mlwh.go` line 378), so phase 4's I1.5 test verifies that `wa mlwh
export --help` lists `products` (its product-grained description and
`study` parent-kind) and the products Columns block; the manifest-absence
assertion is realised once phase 6 removes the registration (covered
there by G1.1's unknown-command check). Do NOT remove the manifest
command in this phase.

- [x] implemented
- [x] reviewed
