# Phase 2: D4 semantics (B1-B2)

Ref: [spec.md](spec.md) sections B1, B2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both.

This phase turns the Phase 1 denormalised `qc` and `is_deliverable`
columns (A4) into two user-facing semantics: `manual_qc` (a rolled-up
pass|fail|pending verdict) and the `--deliverables-only` filter. Both
read the A4 columns per platform, so a non-Illumina product/iRODS row is
NOT blank. Reuse `qc.go` for the roll-up so it never disagrees with
`SampleProgress.qc` / `StatusBreakdown`. State the exact definitions in
any surfaced text: `manual_qc` = `iseq_product_metrics.qc` rolled up by
`qc.go` (fail>pending>pass); `deliverable` = `entity_type IN
('library','library_indexed')` (Element/Ultima `is_sequencing_control=0`),
NOT `is_spiked`, pass-through for PacBio/ONT.

The two items share `mlwh/hierarchy.go`, so they are sequential. Depends
on Phase 1 (A4 columns, A3 composite product rows,
A1 flowcell/is_sequencing_control). MySQL EXPLAIN proofs live in
`cache_mysql_integration_test.go` (skipped without creds); the
behavioural tests are hermetic over the ephemeral SQLite cache. This
phase feeds C4 (Phase 3) and D1 (Phase 4).

## Items

### Item 2.1: B1 - manual_qc roll-up surface (reuse qc.go)

spec.md section: B1

Expose `manual_qc` (pass|fail|pending via `qc.go`) wherever product/iRODS
rows list: the export context (`IRODSPath.ManualQC`), `StudyManifest`
rows (additive `manual_qc` per row), and the iRODS listings. Render it
from the row's denormalised per-platform `qc` (A4) so Element/Ultima/
PacBio rows are non-blank; for a composite product resolve to the
composite's own `qc` (via A3), never blank; ONT has no product/qc so
`manual_qc` is empty (the one legitimate blank). Reuse `qc.go` (do not
re-implement the precedence). Files: `qc.go` (reuse), `manifest.go`,
`hierarchy.go`. Covering all 3 acceptance tests from B1 (`qc=1` ->
"pass", `qc=0` -> "fail", `qc=NULL` -> "pending"; a merged composite with
`qc=1` -> "pass" not empty; Element and Ultima rows non-blank, ONT row
empty). Depends on Phase 1 (A3, A4).

- [x] implemented
- [x] reviewed

### Item 2.2: B2 - deliverable filter via entity_type; pass-through PacBio/ONT

spec.md section: B2

Add a server-side, indexed `--deliverables-only` filter resolved from the
row's denormalised `is_deliverable` tri-state (A4): it excludes ONLY
`is_deliverable = 0` (`... AND (is_deliverable = 1 OR is_deliverable IS
NULL)`), so it drops Illumina/Element/Ultima controls/spikes but is
PASS-THROUGH for PacBio/ONT (NULL retained, never a false "no data"). On
the sample-scoped search variant apply the same tri-state via the
product/flowcell join; PacBio/ONT-only samples are never dropped. Files:
`hierarchy.go`, `search.go`, `count.go`. Covering all 4 acceptance tests
from B2 (study 7556 cram export `--deliverables-only` equals the
entity_type-derived count, ~886 - assert the actual figure and document
any delta from 886 and the iRODS `target=1` AVU; a PacBio+ONT-only study
drops nothing and the count is unchanged; EXPLAIN shows index-served
filtering via the flowcell PK/entity_type index and the denormalised
`is_deliverable`; an Element/Ultima study excludes `is_sequencing_control
=1` and retains `=0`, and each retained non-Illumina row's `manual_qc` is
a non-empty pass/fail/pending). Depends on 2.1 (shared `hierarchy.go`;
retained rows must render `manual_qc`) and Phase 1 (A1, A4).

- [x] implemented
- [x] reviewed

For sequential items, a single review pass after each item is
acceptable; reviewers must confirm `manual_qc` reuses `qc.go` (agreeing
with SampleProgress/StatusBreakdown) and that `--deliverables-only` is
pass-through for PacBio/ONT (drops only `is_deliverable = 0`).

## Ordering and dependency notes

- Depends on Phase 1 (A4 denormalised `qc`/`is_deliverable`, A3 composite
  product rows, A1 flowcell + `is_sequencing_control`).
- Items are sequential because both edit `mlwh/hierarchy.go`: B1 adds the
  `manual_qc` roll-up render, B2 adds the deliverable filter to the same
  read paths and the sample-search variant.
- The deliverable/manual_qc semantics defined here are reused by C4
  (Phase 3, shared filter family) and D1 (Phase 4, the export surface),
  so keep the filter logic factored for reuse.
- Registry/remote/server Descriptions stating these definitions are
  consolidated in Phase 10 (J).
