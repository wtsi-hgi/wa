# Phase 9: D6 fixes (I1-I2)

Ref: [spec.md](spec.md) sections I1, I2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both.

This phase is the mirror correctness/perf cleanup: return `[]` (never
`null`) for `StatusBreakdown.per_platform` on empty studies, and bring
`StudyOverview` and `StatusBreakdown` under 1s at study-7699 scale (both
~3s today). The pure iRODS aggregate is already 0.085s; the slowness is
the sample-membership / per-platform / QC-rollup arms and the
varchar-collated join, so profile and fix those with the same
denormalisation/index discipline, reusing the Phase 1 A2 `char(64)` join
and A4 columns. Do not regress the existing per-platform breakdown.

This phase depends ONLY on Phase 1 (A2, A4) and per the spec's
Implementation Order can run in PARALLEL with Phases 4-8. The two items
share `mlwh/progress.go`, so they are sequential. The perf proof (EXPLAIN,
<1s) lives in `cache_mysql_integration_test.go` (skipped without creds);
the `[]` behaviour is hermetic over the ephemeral SQLite cache. Do NOT
run the full test suite.

## Items

### Item 9.1: I1 - StatusBreakdown.per_platform [] for empty studies

spec.md section: I1

Make `StatusBreakdown` return `[]` (never `null`) for `per_platform` when
a study has no products (observed on 5990, 8338), so the array schema
holds and downstream validation does not break. File: `progress.go`.
Covering the single acceptance test from I1 (a study with no products has
`per_platform == []` (empty array), not null, and JSON serialises `[]`).

- [x] implemented
- [x] reviewed

### Item 9.2: I2 - StudyOverview / StatusBreakdown < 1s at big-study scale

spec.md section: I2

Bring both `StudyOverview` and `StatusBreakdown` under 1s at study-7699
scale (both ~3s today). Profile and fix the sample-membership /
per-platform / QC-rollup arms with the denormalisation/index discipline,
reusing the A2 `char(64)` join and the A4 columns; do not regress the
existing per-platform breakdown. Files: `availability.go`, `progress.go`.
Covering the single acceptance test from I2 (study 7699 on MySQL:
`StudyOverview` and `StatusBreakdown` EXPLAIN show index-served arms - no
full scans / correlated subqueries - and each completes < 1s). Depends on
Phase 1 (A2, A4) and 9.1 (shared `progress.go`).

- [x] implemented
- [x] reviewed

For sequential items, a single review pass after each item is
acceptable; reviewers must confirm `per_platform` serialises as `[]` (not
null) and that the perf fix is index-served (no full scans / correlated
subqueries) without regressing the per-platform breakdown content.

## Ordering and dependency notes

- Depends ONLY on Phase 1 (A2 `char(64)` join, A4 denormalised columns);
  per the spec's Implementation Order this phase may run in PARALLEL with
  Phases 4-8 once Phase 1 is reviewed.
- Items are sequential because both edit `mlwh/progress.go`: I1 is the
  small `[]` correctness fix, I2 is the perf work on the overview/
  status-breakdown arms.
- The APIVersion bump is Phase 1 (A8); the final registry/docs consolidation
  is Phase 10 (J); this phase changes only query/serialisation behaviour.
