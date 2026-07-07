# Phase 6: D5 run aggregation (F1-F2)

Ref: [spec.md](spec.md) sections F1, F2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both.

This phase adds global run aggregation in the new `mlwh/runs_agg.go`:
monthly grouped run counts and a flat global all-runs listing with a
composite id. Run grain is ONE run identifier - never a well/flowcell:
Illumina/Element/Ultima = distinct `id_run`, PacBio = distinct
`pac_bio_run_name`, ONT = distinct `experiment_name`. The per-platform
`date_basis` is authoritative (Illumina/Element `run complete`, Ultima
`run archived`, PacBio `run_complete`, ONT `last_updated` labelled
"warehouse load time - not a true sequencing date"); each response row
states its basis and ONT is INCLUDED under that label, never dropped. The
manufacturer map (state it): Illumina->Illumina, Elembio->Element
Biosciences, Ultimagen->Ultima Genomics, PacBio->PacBio, ONT->Oxford
Nanopore.

Grouping must be index-served via the Phase 1 A6 normalised run-date
columns/indexes and ONT identity. The two items share `runs_agg.go`, so
they are sequential (F2's listing reuses F1's per-platform date_basis/
manufacturer/run-grain derivation). Depends on Phase 1 (A6). MySQL
EXPLAIN/count proofs live in `cache_mysql_integration_test.go`;
behavioural tests are hermetic over the ephemeral SQLite cache.

## Items

### Item 6.1: F1 - monthly grouped run counts

spec.md section: F1

Add `GET /runs/monthly?since&until&platform` -> `[]MonthlyRunCount`
`{month, manufacturer, platform, count, date_basis, cache_synced_at}`
across all platforms, run grain per platform (distinct id_run /
pac_bio_run_name / experiment_name, never wells/flowcells), each row
stating its `date_basis`, ONT included under the labelled warehouse-load
basis. Add CLI `wa mlwh runs --monthly [--since/--until] [--platform
...]`. Files: `mlwh/runs_agg.go`, `cmd/mlwh_runs.go`. Covering all 3
acceptance tests from F1 (over the full window PacBio = distinct
pac_bio_run_name ~2,843 NOT 12,499 wells, ONT = distinct experiment_name
~447, Ultima `run archived` = 242, the 8-well TRACTION-RUN-1000 counts
once in Dec-2023, each row states date_basis; ONT bucket's date_basis ==
"warehouse load time - not a true sequencing date" and rows present, not
dropped; MySQL EXPLAIN shows index-served grouping, no full scan).
Depends on Phase 1 (A6).

- [ ] implemented
- [ ] reviewed

### Item 6.2: F2 - global run listing + composite id

spec.md section: F2

Add `GET /runs?platform&since&until` -> `[]RunListingRow`, one row per
run, `id` = composite `<platform>:<native_id>` (e.g.
`ont:<experiment_name>`) with `platform`, `native_id`, `manufacturer`,
`run_date` + `date_basis` (ONT carries the warehouse-load caveat).
Bounded, paged, with `/count`; the composite id is the keyset cursor.
This parentless GLOBAL listing is exposed as `wa mlwh runs [--platform]
[--since/--until]` (the flat listing; `--monthly` selects F1 instead) and
is DISTINCT from the D1a `export runs` relationship (which requires a
parent). Files: `mlwh/runs_agg.go`, `count.go`. Covering both acceptance
tests from F2 (each `id` is `<platform>:<native_id>`, `native_id` matches
the platform's run identity, `run_date`/`date_basis` follow the
per-platform basis; `--all` keyset paging on the composite id emits every
run with no silent cap and count == len(list-all)). Depends on 6.1 (the
per-platform date_basis/manufacturer/run-grain derivation) and Phase 1
(A6).

- [ ] implemented
- [ ] reviewed

For sequential items, a single review pass after each item is
acceptable; reviewers must confirm run grain is one run identifier (never
a well/flowcell), ONT is included under the labelled warehouse-load
date_basis, and the global `wa mlwh runs` listing is not an `export runs`
form.

## Ordering and dependency notes

- Depends on Phase 1 (A6 ONT `experiment_name` identity, normalised
  indexed run-date columns for Illumina/Element/Ultima/PacBio/ONT).
- Items are sequential because both add to `mlwh/runs_agg.go`: F1
  establishes the per-platform date_basis, manufacturer map, and run-grain
  counting; F2 reuses that for the flat per-run listing and composite id.
- F1 is generalised by G2 (Phase 7, the grouped sequencing aggregate),
  which depends on this phase.
- Registry entries and Description text for `/runs/monthly` and `/runs`
  (incl. the per-platform date_basis and ONT caveat) are consolidated in
  Phase 10 (J).
