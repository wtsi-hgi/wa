# Phase 10: Wiring + CLI (J, K)

Ref: [spec.md](spec.md) sections J, K

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both.

This is the final consolidation phase: wire every new endpoint into the
registry, server handlers, remote client, and Queryer, state each exact
definition in its `Description`/`Summary`/`Query` (HARD REQ 9), regenerate
docs, and confirm the drift/parity guards are green (J); then expose
everything via `wa mlwh` in both local-cache and `--server` modes with
graceful degradation exit 0 (K). Some per-endpoint wiring already exists
from earlier phases (e.g. D1c counts in Phase 4); J completes and verifies
the FULL set. Every `Description` must state the definitions from the
spec's "Definitions to state" (manual_qc via `qc.go`; deliverable via
`entity_type`, not `is_spiked`, pass-through PacBio/ONT; cram = filename
suffix; recency = iRODS `created`; per-platform run `date_basis` incl. the
ONT warehouse-load caveat; search default = literal whole-value prefix vs
`--words` vs the exact filters incl. the QC grain difference; programme
unit; study->users direction + role vocabulary; merged-CRAM attribution;
`cache_synced_at`/`/freshness` caveat; `faculty_sponsor` is a `Study`
field, NOT a role).

J (package `mlwh/`) and K (package `cmd/`) touch disjoint packages, but
K's `--server` round-trip depends on J's remote/server wiring, so they are
sequential (J then K). Do NOT run a live warehouse or the full test suite;
rely on the hermetic GoConvey suite, the drift/parity guards, and the
`--server` round-trip tests over the in-process server.

## Items

### Item 10.1: J - registry, remote, server, docs wiring

spec.md section: J

Add registry entries + handler cases + remote client methods + Queryer
members for every new endpoint (`/study/:id/latest-data`,
`/latest-data/faculty-sponsor/:name`, `/runs/monthly`, `/runs`,
`/sequencing/aggregate`, `/studies/programme/:name`, `/programmes`,
`/study/:id/users`, `/study/:id/sample-crams`, and their `/count`
siblings), plus the changed `manual_qc`/`created`/filter params on the
iRODS/search/manifest entries. Every Description states its exact
definition (see Instructions); update the SearchSamples/CountSampleSearch
Descriptions to state the NEW default (literal whole-value prefix over the
four fields) vs `--words` vs the exact filters and the QC grain
difference. Regenerate docs; drift/parity guards green. Files:
`registry.go`, `server.go`, `remote.go`, `queryer.go`, `docs.go`.
Covering both acceptance tests from J (every new endpoint has a
Description stating its definition and the search entry no longer claims
word-prefix as default; each new endpoint has a remote method + server
handler that round-trip remote == local, and the docs/parity drift guards
pass). Depends on Phases 4-8 (the endpoints being wired) and Phase 1 (A8
APIVersion 1.8.0).

- [x] implemented
- [x] reviewed

### Item 10.2: K - CLI exposure

spec.md section: K

Make all features reachable from `wa mlwh` in local-cache and `--server`
modes, graceful degradation exit 0: wire `cmd/mlwh.go` to drop `irods` and
add `export`, `runs`, `latest`, `programmes`; ensure D1 `export`, D2
`latest`/`export irods --sort created-desc`, D3 `search` flags, D5 `runs
--monthly`/`runs`/`export runs`, D7 `studies --programme`/`programmes`/
`runs --monthly --group-by`/`export users`/`programme` in overview, D8
`export sample-crams`/`products_without_irods` in manifest/merged
attribution, and D4 `manual_qc` + `--deliverables-only` render wherever
product rows show. Files: `cmd/mlwh.go`, `cmd/mlwh_export.go`,
`cmd/mlwh_runs.go`, `cmd/mlwh_latest.go`, `cmd/mlwh_studies.go`,
`cmd/mlwh_manifest.go`, `cmd/mlwh_info.go`, `cmd/mlwh_search.go`. Covering
all 3 acceptance tests from K (`wa mlwh --help` lists `export`, `runs`,
`latest`, `programmes` and NOT `irods`; each command with a never-synced
cache renders a clean message and exits 0 in both local and `--server`
modes; `wa mlwh info <study>` with sequenced products renders both
`programme` (D7) and a `manual_qc` value/section (D4), including a
non-blank `manual_qc` for a study with non-Illumina products). Depends on
10.1 (the `--server` round-trip needs J's remote/server wiring) and the
CLI commands built in Phases 4-8.

- [x] implemented
- [x] reviewed

For sequential items, a single review pass after each item is
acceptable; reviewers must confirm every Description states the exact
definition (HARD REQ 9), the search Description no longer claims
word-prefix as the default, remote == local for each new endpoint, the
docs/parity/drift guards are green, and `wa mlwh irods` is gone while
`export`/`runs`/`latest`/`programmes` are present.

## Ordering and dependency notes

- This is the last phase; it depends on Phases 1-9 (the endpoints,
  behaviours, and CLI commands being wired and exposed) and on Phase 1's
  A8 APIVersion 1.8.0.
- Items are sequential (J then K): J and K touch disjoint packages
  (`mlwh/` vs `cmd/`), but K's `--server` acceptance test round-trips
  through J's server handlers and remote client, so J must land first.
- Some per-endpoint wiring already exists from earlier phases (D1c counts
  in Phase 4); J completes and verifies the FULL endpoint set, the
  Description text, docs regeneration, and the drift/parity guards.
- After this phase the orchestrator runs the spec-aware and spec-free
  pr-reviewer passes per the orchestrator skill (two consecutive clean
  passes each).
