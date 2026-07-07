# Phase 8: D8 merged CRAM (H1-H4)

Ref: [spec.md](spec.md) sections H1, H2, H3, H4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both.

This phase makes merged multi-lane / composite CRAMs first-class rather
than silently dropped. Per code-authority correction #2 (verified against
the Go code): the STUDY-scoped iRODS listing already attributes
`id_sample_tmp`/`name` for merged CRAMs (via the mirror's denormalised
`id_sample_tmp` + a `sample_mirror` join), so this phase does NOT re-fix
that attribution. It DOES: represent a composite's run honestly
(`merged=true`, `id_run=0`) from the A4 denormalised `merged` column; make
composites visible/attributed in the RUN-scoped listing (using the A3
composite product rows); add the per-sample `sample-crams` export
(merged-aware, de-duplicated); and make the manifest gap explicit (per-row
flag + envelope counter) WITHOUT duplicating a composite path across a
sample's single-lane rows.

Depends on Phase 1 (A3 composite product rows, A4 `merged` column) and
Phase 4 (`export.go`/`cmd/mlwh_export.go`, for H3's `sample-crams`
relationship). The items share `mlwh/hierarchy.go` (H1-H3) and
`mlwh/types.go` (H1, H4), so they are sequential with a logical build-up.
MySQL count proofs (7568 = 732) live in
`cache_mysql_integration_test.go`; behavioural tests are hermetic over the
ephemeral SQLite cache.

## Items

### Item 8.1: H1 - honest id_run / merged for composite objects in listings/export

spec.md section: H1

Represent a composite object's run HONESTLY - `merged=true` and
`id_run=0` (a composite spanning lanes/runs has no single run) - rather
than the current misleading `0` with no flag; `merged` comes from the A4
denormalised column. The export (which projects sample identity for all
scopes via the `sample_mirror` join on the denormalised `id_sample_tmp`)
surfaces `name`/`supplier_name`, `merged`, and `id_run=0` for composites,
so no export row is `name:""`/`id_sample_tmp:0`. Files: `hierarchy.go`,
`types.go`. Covering both acceptance tests from H1 (study-7568's merged
`lane1-2` object listed via the STUDY iRODS scope and the `export irods`
relationship carries correct non-empty `name`/`id_sample_tmp` and now
`merged=true`, `id_run=0`; a single-lane object has `merged=false` and
its real `id_run`). Depends on Phase 1 (A4 `merged`).

- [ ] implemented
- [ ] reviewed

### Item 8.2: H2 - run-scoped composite visibility

spec.md section: H2

With A3 (composite product rows now mirrored carrying the run's `id_run`
for a single-run multi-lane composite), make the run-scoped iRODS listing
- which currently INNER-JOINs product-metrics on `id_iseq_product` and
cannot see composites - see and attribute the composite, representing it
with `merged=true`. Where the composite genuinely spans runs (no single
run), it is surfaced via the study/sample listings and `sample-crams`,
not fabricated onto a run. File: `hierarchy.go`. Covering the single
acceptance test from H2 (`IRODSPathsForRun(49348)` returns the single-run
multi-lane composite object, attributed and `merged=true`, alongside the
single-lane objects - no longer invisible). Depends on 8.1 (shared
`hierarchy.go`, the `merged` representation) and Phase 1 (A3).

- [ ] implemented
- [ ] reviewed

### Item 8.3: H3 - per-sample sample-crams export

spec.md section: H3

Add `GET /study/:id/sample-crams` -> `[]SampleCram` `{name, ega_id,
irods_cram_path, merged}`, one row per sample, resolving each sample's
cram through the sample<->iRODS-mirror linkage (`id_sample_tmp`) so
merged CRAMs are included and a sample's multi-lane rows collapse to one
CRAM (de-duplicated: prefer the merged composite object when present,
else the single-lane cram). Bounded/paged + `/count`. Backs `wa mlwh
export sample-crams study 7568`. Files: `hierarchy.go`, `count.go`,
`mlwh/export.go`, `cmd/mlwh_export.go`. Covering all 3 acceptance tests
from H3 (study 7568 returns all 732 samples each with a populated
`irods_cram_path`, the 48 run-49348 merged-CRAM samples included and each
collapsed to one carrying `merged=true`, none blank; a sample with
multiple single-lane crams and no merge collapses to one row
deterministically; the study `export irods` cram listing for 7568 has all
merged rows attributed, total 732, no `id_sample_tmp:0`/`name:""`).
Depends on 8.1/8.2 (shared `hierarchy.go`, `merged`) and Phase 4
(`export.go`/`cmd/mlwh_export.go`).

- [ ] implemented
- [ ] reviewed

### Item 8.4: H4 - manifest gap made explicit

spec.md section: H4

For the product-grained `StudyManifest --with-irods`, when a single-lane
product's CRAM cannot be matched (merged into a composite object), set
per-row `irods_unmatched=true` with `reason="merged_multilane"` and add an
envelope `products_without_irods` counter. Do NOT resolve the composite
path onto each single-lane row (that would duplicate one merged path
across a sample's lane rows); direct callers to `sample-crams` / `export
irods`. Update the manifest registry `Description` to name merged
multi-lane CRAMs as the common cause of an empty `irods_path`. Files:
`manifest.go`, `types.go`, `registry.go`, `cmd/mlwh_manifest.go`.
Covering all 3 acceptance tests from H4 (study 7568 `manifest
--with-irods --file-type cram`: `products_without_irods` equals the empty-
`irods_path` product rows, each such row has
`irods_unmatched=true`/`reason="merged_multilane"`, and the arithmetic
closes - assert the actual figure and its relation to the 48 merged
samples / 96 single-lane rows; a study with no merged CRAMs has
`products_without_irods == 0` and no `irods_unmatched` row; the CLI
surfaces `products_without_irods` and exits 0). Depends on 8.1 (shared
`types.go`).

- [ ] implemented
- [ ] reviewed

For sequential items, a single review pass after each item is
acceptable; reviewers must confirm composites are represented honestly
(`merged=true`, `id_run=0`), the study-scoped attribution that already
works is NOT re-fixed, and the manifest surfaces the gap (flag + counter)
without duplicating a composite path across single-lane rows.

## Ordering and dependency notes

- Depends on Phase 1 (A3 composite product rows, A4 `merged` column) and
  Phase 4 (`export.go`/`cmd/mlwh_export.go`, for H3's `sample-crams`).
- Items are sequential: H1 (honest `merged`/`id_run` in `hierarchy.go`/
  `types.go`), H2 (run-scoped visibility, `hierarchy.go`), and H3
  (`sample-crams`, `hierarchy.go`) share `hierarchy.go`; H4 (manifest
  counter) shares `types.go` with H1. The logical build-up is
  merged-flag -> run visibility -> per-sample collapse -> manifest gap.
- Per code-authority correction #2, the study-scoped `id_sample_tmp`/
  `name` attribution already works and is NOT re-fixed here.
- The `sample-crams` registry entry and manifest Description text are
  finalised in Phase 10 (J); H4 updates the manifest Description inline.
