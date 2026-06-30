# Phase 4: D3 QC counts (D1q) + D5 study metadata (F1)

Ref: [spec.md](spec.md) sections D1q, F1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both (the four-step add-a-query recipe, the `id_lims =
'SQSCP'` invariant, the never-synced/unknown/synced-empty cascade, and
behaviour-focused tests over the hermetic SQLite cache).

Both items extend EXISTING aggregates additively (no new endpoint): D1q
folds a `QC StudyQCBreakdown` sub-struct into `StatusBreakdown`
(`mlwh/progress.go`), and F1 surfaces four study-metadata fields on
`StudyOverview` (`mlwh/availability.go`) plus a search-disambiguation
confirmation. The per-sample QC roll-up rule is identical to
`rollUpSampleQC` (fail > pending > pass over `iseq_product_metrics.qc`,
1=pass / 0=fail / NULL=pending), but study-scoped, so a single-study
sample's study QC verdict cannot disagree with `SampleProgress.qc`.
Tests live in `mlwh/progress_test.go`, `mlwh/availability_test.go`, and
`mlwh/search_test.go`, hermetic over `openSQLiteSyncTestCache`.

CAUTION (Note 1, D3 SEED FOOTGUN): D1q.2 asserts the strict equality
`qc_pass + qc_fail + qc_pending == samples_total - distinct.registered`.
This holds in production ONLY because the iRODS mirror is populated
exclusively via INNER JOINs to the product-metrics source tables (every
`with_data` sample has a product-metrics row). Therefore the D3 test
SEEDS (and any shared J1 seed they reuse) must NOT introduce an
iRODS-only sample lacking a product-metrics row (e.g. the artificial
`ont`-platform iRODS row used in the B1.2 seed of Phase 2) into the D3
fixtures, or the strict-equality assertion will spuriously fail. The
not_tracked / ONT / registered-only samples in the D3 scenario must have
NO product-metrics rows (so they land in `distinct.registered`, the
not-sequenced bucket) and must NOT also carry an iRODS row.

## Items

### Batch 1 (parallel)

D1q (QC split on `StatusBreakdown`) and F1 (study metadata on
`StudyOverview` + search confirmation) touch disjoint files
(`progress.go` vs `availability.go`) and disjoint structs, so they can be
implemented concurrently. Both depend only on Phase 1.

#### Item 4.1: D1q - study-level qc_pass / qc_fail / qc_pending over sequenced samples [parallel with 4.2]

spec.md section: D1q

Extend `StatusBreakdown` (`mlwh/progress.go`) to fill `QC
StudyQCBreakdown` (the `StudyQCBreakdown{QCPass,QCFail,QCPending}` struct
added additively to `types.go`); the existing `distinct`, `per_platform`,
`with_detailed_timeline`, and `cache_synced_at` are unchanged. Add ONE
grouped query over the study's distinct sequenced samples (each linked
sample with >=1 study-scoped product-metrics row via
`studyScopedProductMetricsExists`): a UNION ALL of each platform's
product-metrics mirror scoped by `id_study_lims` and `id_sample_tmp`,
grouped by sample with conditional aggregation (`MIN(qc)=0` -> fail; else
any `qc IS NULL` -> pending; else pass), then an outer count by bucket.
Reuse `statusBreakdownFeedingTables`. Per-platform QC counts are DEFERRED
(the response shape leaves room for a future `per_platform[i].qc`). The
method signature is unchanged. The `Description` states: received =
`samples_total`, sequenced = `samples_total - registered`, not-sequenced
= `registered`; `qc` is the QC split of the SEQUENCED distinct samples
using the same per-sample roll-up as `/sample/:id/progress` (fail >
pending > pass), summing to sequenced; not_tracked/ONT samples are
not-sequenced and excluded from the QC split; and the freshness caveat.
Covering all 6 acceptance tests from D1q (the A/B/C/D/E scenario gives
`distinct.registered=2`, sequenced=3, `qc={1,1,1}` summing to 3; the sum
equals `samples_total - distinct.registered`; a mixed qc=0/qc=1 sample
lands in `qc_fail`, matching `SampleProgress.qc`; an ONT-only sample is
in `distinct.registered` and in no QC bucket -- never a false
`qc_pending`; never-synced both sentinels / unknown `ErrNotFound` /
synced-empty all-zero ladders AND `qc={0,0,0}` with cache_synced_at; the
existing distinct/per_platform/with_detailed_timeline regression tests
still pass unchanged). Heed the SEED FOOTGUN caution above when building
the A-E fixture. Depends on Phase 1.

- [x] implemented
- [x] reviewed

#### Item 4.2: F1 - study metadata on the overview + disambiguation on search [parallel with 4.1]

spec.md section: F1

Surface `name`, `accession_number`, `faculty_sponsor`,
`data_access_group` on `StudyOverview` (`mlwh/availability.go` +
additive `types.go` fields), read from `study_mirror` in the existing
`StudyOverview` path (a single-row select of those columns, or reuse
`resolveStudyFromCache` before building the overview); populate them on
the empty-study path too (the study exists, only its samples are zero).
The `Description` adds that the overview now carries the study's
`data_access_group` (and name / accession / faculty_sponsor) so "data
access groups for study X" is one small call without the giant
`/study/:id/detail`. `SearchStudies` already returns full `Study` rows
(carrying `id_study_lims`, `name`, `faculty_sponsor`) -- NO SQL change;
add a test confirming a search row carries those three fields (Q5
disambiguation) and add the disambiguation note to the `SearchStudies`
`Description`. Wire the additive `StudyOverview` field changes through
the Registry `Description` only (no new endpoint). Covering all 3
acceptance tests from F1 (overview with 5 samples populates the four
metadata fields alongside counts; a synced study with zero samples still
populates the four fields with zero counts and populated cache_synced_at;
two "Malaria" studies each carry distinct `id_study_lims`/`name`/
`faculty_sponsor` from `SearchStudies`). Depends on Phase 1.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

## Ordering and dependency notes

- This phase depends on Phase 1 being fully reviewed; it is independent
  of Phases 2-3 and may run in parallel with them (all are post-Phase-1).
- Batch 1 (D1q, F1) is parallel: D1q extends `StatusBreakdown`
  (progress.go), F1 extends `StudyOverview` (availability.go) and adds a
  search confirmation; the two touch disjoint files and structs.
- SEED FOOTGUN (Note 1): the D3 strict-equality assertion `qc_pass +
qc_fail + qc_pending == samples_total - distinct.registered` holds only
  because every with_data sample has a product-metrics row. The D3 (and
  any shared J1) fixtures must NOT add an iRODS-only sample lacking a
  product-metrics row; not_tracked/ONT/registered-only samples must have
  no product-metrics rows and no iRODS row. The same caution is repeated
  in Phase 7 (which seeds the J1 scenario for the MySQL tests).
- Both items are additive; reviewers must confirm the existing
  `StatusBreakdown` (distinct/per_platform/with_detailed_timeline) and
  `StudyOverview` regression tests still pass unchanged (per J2).
- Per-endpoint Registry/handler/RemoteClient wiring is done incrementally
  here (per the spec's G note); Phase 6 does the final `APIVersion` bump,
  CLI, doc regeneration, and drift-guard verification.
