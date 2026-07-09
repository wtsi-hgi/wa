# Phase 4: D1 export (D1a-D1c)

Ref: [spec.md](spec.md) sections D1a, D1b, D1c

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both.

This is the flagship phase: a fast, generic, column-selectable, multi-
format `wa mlwh export` over any entity->children relationship, replacing
`wa mlwh irods`. The backing layer (D1a) reads the Phase 1 A4 iRODS
mirror as ONE index-ordered range scan (sample_mirror joined by the
indexed `id_sample_tmp` only for identity columns), reuses `qc.go` for
`manual_qc` (composite-aware, B1), and applies the shared filter family
(C4). `--all` streams the COMPLETE set via KEYSET pagination on the
canonical tuple `(id_run, position, tag_index,
id_seq_product_irods_locations_tmp)` - NOT LIMIT/OFFSET - and the CLI
always states whether it emitted a bounded page or the complete set (HARD
REQ 2, no silent truncation). This phase also completes C4's export-
surface touchpoint (the shared filter family now applies on `export.go`).

Depends on Phase 1 (A4), Phase 2 (B), and Phase 3 (C). D1a is sequential
(it defines `export.go`, its relationships, and column vocabularies);
D1b (CLI) and D1c (counts) both depend on D1a and touch disjoint files
(`cmd/` vs `mlwh/count.go`,`server.go`,`remote.go`), so they run as a
parallel batch after D1a. MySQL EXPLAIN/scale proofs and the memory-
bounded `--all` test live in `cache_mysql_integration_test.go` /
`export_test.go`; behavioural tests are hermetic over the ephemeral
SQLite cache.

## Items

### Item 4.1: D1a - export backing projection layer

spec.md section: D1a

Create `mlwh/export.go` with `Export(ctx, rel ExportRelationship,
parentID string, opts ExportOptions) (ExportResult, error)` and the
`ExportRelationship`/`ExportOptions`/`ExportResult` types per the spec.
Support the relationship set (`irods`/`files`, `samples`, `runs`,
`libraries`, `lanes`, `studies`, `users`, `sample-crams`), formats
`tsv|csv|json`, ordered `--columns` over per-relationship vocabularies
(unknown column = actionable error listing the valid set), the shared
filter family (`--file-type` default cram; `--deliverables-only` ON BY
DEFAULT for cram listings; `--qc` per-product; `--library-type`,
`--organism`), the canonical iRODS order with merged objects sorting
first, and `--all` KEYSET streaming plus bounded-page total+cursor.
Correctness: `irods_path` verified against real paths (incl. merged
`.../lane1-2/plex1/49348_1-2#1.cram`); the study-7568 cram export returns
732 attributed rows (no `name:""`/`id_sample_tmp:0`). File: `export.go`.
Covering all 6 acceptance tests from D1a (7556 irods-of-study cram export
with alias resolution, every path `.cram`, count = deliverable cram count
~886 assert actual; unknown column error + no rows; json/csv/tsv formats;
`--all` at 7699 100k+ rows via keyset with bounded heap <20 MiB, no cap;
bounded page `Total >= len(Rows)` + non-empty `NextCursor`; MySQL EXPLAIN
= index range scan on the A4 covering index, <1s per page). Depends on
Phase 1 (A4), Phase 2 (B), Phase 3 (C4 shared filters).

- [x] implemented
- [x] reviewed

### Batch 1 (parallel, after item 4.1 is reviewed)

D1b (CLI) and D1c (counts + paging headers) both build on the D1a backing
layer and touch disjoint files, so they run concurrently.

#### Item 4.2: D1b - wa mlwh export CLI (replaces wa mlwh irods) [parallel with 4.3]

spec.md section: D1b

Add `cmd/mlwh_export.go` implementing the grammar `wa mlwh export
<children> <parent-kind> <parent-id> [flags]` with `--columns`,
`--format tsv|csv|json`, `--json`, `--file-type` (default cram for cram
listings), `--deliverables-only`/`--include-controls`, `--qc`,
`--library-type`, `--organism`, `--sort created-desc`, `--since/--until`,
`--limit/--offset`, `--all`, `--server`; remove `cmd/mlwh_irods.go`. Both
local-cache and `--server` modes; graceful degradation exit 0. Covering
all 4 acceptance tests from D1b (`export irods study 5901 --file-type
cram` prints the cram TSV and exits 0, the behavioural equivalent of the
removed `irods` command; `export runs sample DN1234 --columns
id_run,platform,run_date`; the CLI states bounded-page + total + next
cursor without `--all` and complete-set with `--all`; unknown parent id
renders a clean not-found and exits 0). Depends on 4.1.

- [x] implemented
- [x] reviewed

#### Item 4.3: D1c - per-relationship /count and sizing [parallel with 4.2]

spec.md section: D1c

Give every export relationship a `/count` counterpart and set
`X-Total-Count` / `X-Next-Offset` (reuse the existing `Page[T]`/paging
machinery). Counts must match `len(list-all)` per relationship. Files:
`count.go`, `server.go`, `remote.go`. Covering the single acceptance test
from D1c (for any relationship, count == len(rows)). Depends on 4.1 (the
relationship set).

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

## Ordering and dependency notes

- Depends on Phase 1 (A4 covering index + denormalised columns), Phase 2
  (B1 manual_qc, B2 deliverable), and Phase 3 (C4 shared filter family).
- D1a is sequential and first (it defines `export.go`, the relationship
  set, and the column vocabularies that D1b and D1c both consume). D1b
  and D1c are a parallel batch over disjoint files (`cmd/mlwh_export.go`
  vs `mlwh/count.go`/`server.go`/`remote.go`).
- This phase completes C4's export-surface touchpoint: the shared filter
  family (organism/library-type/qc/deliverable) now applies on `export.go`
  and C4 acceptance test 4 (filters on the export surface) is exercised
  here.
- `--all` uses keyset pagination on the canonical order tuple, never
  LIMIT/OFFSET (HARD REQ 2); the CLI always states bounded-page vs
  complete-set.
- The `runs` export relationship is strictly parent-scoped (`runs of
study | sample`); the parentless global all-runs listing is F2 (Phase
  6), NOT an `export runs` form.
- Registry entries and full Description text are consolidated in Phase 10
  (J); D1c wires the count handlers/remote methods here.
