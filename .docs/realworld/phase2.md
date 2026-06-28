# Phase 2: Availability (B1-B3) + Recency (C1-C2)

Ref: [spec.md](spec.md) sections B1, B2, B3, C1, C2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both (four-step add-a-query recipe, `id_lims = 'SQSCP'`
invariant, never-synced cascade, count<->list cross-check).

All new code lives in `mlwh/availability.go` (with `IRODSPath` field
additions in `types.go` and the SQL select changes in `hierarchy.go` for
B3). Tests live in `mlwh/availability_test.go` and
`mlwh/hierarchy_test.go`, hermetic over `openSQLiteSyncTestCache`.

B and C share the same membership SQL (`library_samples ->
sample_mirror -> seq_product_irods_locations_mirror` scoped by
`id_study_lims`), so build that membership query once and reuse it: the
windowed count/list (C) is the same query plus a half-open
`[since, until)` filter on the mirrored `created` column over the
`(id_study_lims, created)` index.

## Items

### Item 2.1: B3 - enumerate samples with / without data + iRODS row identity

spec.md section: B3

Implement `SamplesWithData(ctx, studyLimsID string, limit, offset int)`
and `SamplesWithoutData(ctx, studyLimsID string, limit, offset int)`
returning `[]SampleWithData` (distinct-sample partition; `with_data` = >=1
study-scoped iRODS row, `without_data` = linked samples minus those,
`platforms` empty for registered and `["ONT"]` for ONT). Add
`id_sample_tmp` and Sanger `name` to the `IRODSPath` rows returned by
`/study/:id/irods` (additive `types.go` fields + `hierarchy.go` SQL
select; `/sample/:id/irods` may carry them too). This item establishes
the shared membership SQL the rest of the phase reuses. Covers all 6
acceptance tests from B3 (disjoint with/without lists summing to total;
PacBio platforms; ONT platforms `["ONT"]`; registered `[]`; iRODS rows
carry id_sample_tmp + name and group to the 3 with-data samples;
count<->list cross-check). Depends on Phase 1 (mirror `platform`/
`created`, per-platform linkage).

- [ ] implemented
- [ ] reviewed

### Item 2.2: B2 - bare count of samples-with-data

spec.md section: B2

Implement `CountSamplesWithData(ctx, studyLimsID string) (Count, error)` via
`queryCount` over the same membership join as 2.1,
`COUNT(DISTINCT id_sample_tmp)`, with the never-synced/empty/unknown
cascade matching `CountSamplesForStudy`. Covers all 3 acceptance tests
from B2 (distinct-sample count not data-object count; count == list
length; never-synced ErrCacheNeverSynced+ErrNotFound). Depends on 2.1
(reuses its membership SQL).

- [ ] implemented
- [ ] reviewed

### Item 2.3: B1 - cheap study overview (S + O1 collapsed)

spec.md section: B1

Implement `StudyOverview(ctx, studyLimsID string) (StudyOverview, error)` ->
`StudyOverview`, all figures as single indexed aggregates over
`library_samples`, `seq_product_irods_locations_mirror` (study-scoped,
with `created`/`platform`), the product-metrics mirrors, and the library
tables. Use the distinct-sample partition (most-advanced-phase
precedence) for `samples_with_data`/`without_data`/`sequenced_no_data`;
set `cache_synced_at` to the oldest `last_run` across feeding tables;
honour the half-open `added_last_7_days` window. Covers all 6
acceptance tests from B1 (full figures incl. sorted library_types;
newest_data_added + half-open 7-day window; study-scoping excludes
cross-study data; never-synced ErrCacheNeverSynced+ErrNotFound; unknown
study ErrNotFound; empty synced study all-zero with cache_synced_at).
Depends on 2.1/2.2 (reuses membership SQL and the distinct-sample
partition).

- [ ] implemented
- [ ] reviewed

### Item 2.4: C1 - windowed samples-with-data count

spec.md section: C1

Implement
`CountSamplesWithDataSince(ctx, studyLimsID, since, until string) (Count, error)`
filtering the membership query on the mirrored `created` column over the
`(id_study_lims, created)` index, half-open `[since, until)`,
`COUNT(DISTINCT id_sample_tmp)`; without `since` it behaves as B2
(all-time). Handler-level: a malformed `since`/`until` -> 400 before the
queryer is reached. The Description must state it filters on the iRODS
creation timestamp (never `last_updated`/`last_run`), the half-open
semantics, and the freshness caveat. Covers all 4 acceptance tests
from C1 (window boundary count; on-boundary since-included/until-
excluded; malformed since -> 400; never-synced
ErrCacheNeverSynced+ErrNotFound). Depends on 2.2 (extends its query with
the window filter).

- [ ] implemented
- [ ] reviewed

### Item 2.5: C2 - windowed samples-with-data list

spec.md section: C2

Extend the B3 list endpoint so
`GET /study/:id/samples-with-data?since=&until=` applies the same
half-open `[since, until)` `created` filter, returning the distinct
in-window samples as `[]SampleWithData`, paginated. Covers both
acceptance tests from C2 (in-window list length equals
`CountSamplesWithDataSince(...,"")`; on-boundary membership matches the
half-open rule). Depends on 2.1 (the list) and 2.4 (the window filter).

- [ ] implemented
- [ ] reviewed

## Ordering and dependency notes

- This phase depends on Phase 1 being fully reviewed (mirror `created`/
  `platform` columns and per-platform linkage).
- Items are sequential because they share one membership SQL: 2.1 builds
  it (and the iRODS-row identity fields), 2.2 counts over it, 2.3
  aggregates over it, 2.4 adds the window filter, 2.5 reuses 2.1+2.4.
- Phase 2 and Phase 3 are independent of each other and may run in
  parallel once Phase 1 is reviewed.
- Per-endpoint Registry/handler/RemoteClient wiring is done incrementally
  here (per the spec's G note); Phase 5 only does the final doc
  regeneration and drift-guard verification.
