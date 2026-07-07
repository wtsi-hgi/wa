# Phase 5: D2 recency (E1-E2)

Ref: [spec.md](spec.md) sections E1, E2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both.

This phase surfaces iRODS recency: `created` ("data added", never
`last_changed`, RFC3339 UTC) on `IRODSPath` and as a selectable export
column, `order_by=created_desc` + a half-open `[since, until)` window on
all three iRODS scopes (matching `SamplesWithData`), and the bounded
"latest data" endpoints. The recency indexes come from Phase 1 A4:
study-scoped is served by `(id_study_lims, created)`, sample-scoped by
`(id_sample_tmp, created)`, run-scoped by `(id_run, created)` (so the
run-scoped recency path scans the iRODS mirror by its denormalised
`id_run`, not the product-metrics INNER JOIN). Reuse the existing
`errUntilRequiresSince` for `until` without `since`.

The two items share `mlwh/availability.go`, so they are sequential (E2's
latest-data endpoints build on E1's `created`/recency indexes). Depends
on Phase 1 (A4 recency indexes). MySQL EXPLAIN proofs live in
`cache_mysql_integration_test.go`; behavioural tests are hermetic over
the ephemeral SQLite cache.

## Items

### Item 5.1: E1 - created on IRODSPath + recency order/window

spec.md section: E1

Add `Created` (RFC3339 UTC, "data added") to `IRODSPath` (additive;
preserve existing field doc comments) as a selectable export column, and
give ALL THREE iRODS list scopes (study, sample, run) - and thus the
`export irods` relationship - `order_by=created_desc` (default order
unchanged) and `since`/`until` (half-open `[since, until)` over
`created`). Study-scoped uses `(id_study_lims, created)`; sample-scoped
the A4 `(id_sample_tmp, created)` index; run-scoped the A4 `(id_run,
created)` index. Files: `hierarchy.go`, `availability.go`. Covering all 5
acceptance tests from E1 (study rows newest-first with populated RFC3339
`created`; sample-scoped window newest-first; run-scoped window
newest-first; `since <= created < until` on any scope and `until` without
`since` errors via `errUntilRequiresSince`; MySQL EXPLAIN of the
sample-scoped and run-scoped `created_desc` paths each uses its recency
index, no full scan, no filesort). Depends on Phase 1 (A4).

- [x] implemented
- [x] reviewed

### Item 5.2: E2 - latest-data endpoints (study + faculty-sponsor)

spec.md section: E2

Add `GET /study/:id/latest-data` and `GET
/latest-data/faculty-sponsor/:name` returning a BOUNDED, pageable page
ordered `created DESC` (small default N, e.g. 10), ties broken by
`(id_run, id_product)` - NOT an unbounded "all rows tied at MAX(created)"
set. Rows are `RecentDataRow`. Membership basis = the raw
`seq_product_irods_locations_mirror` scan on `(id_study_lims, created)`
(document this so results reconcile with `StudyOverview.newest_data_added`);
the faculty-sponsor variant joins `study_mirror.faculty_sponsor` and
merges each study's top-N via the `(id_study_lims, created)` index
(avoids a global filesort). Add CLI `wa mlwh latest <study-id |
--faculty-sponsor NAME> [--file-type cram]`. Files: `availability.go`,
`count.go`, `cmd/mlwh_latest.go`. Covering all 3 acceptance tests from E2
(`/study/:id/latest-data` returns <= N rows created DESC, first row's
`created` == `StudyOverview.newest_data_added`, each row carries the named
fields; the Anderson faculty-sponsor variant returns top-N across studies
in ONE call, not one-per-study fan-out; `wa mlwh latest 5901 --file-type
cram` prints newest cram rows and exits 0). Depends on 5.1 (`created` +
recency indexes) and Phase 1 (A4).

- [x] implemented
- [x] reviewed

For sequential items, a single review pass after each item is
acceptable; reviewers must confirm `created` uses the iRODS "data added"
time (never `last_changed`), latest-data is a BOUNDED page (not a MAX-tie
set), and the run-scoped recency path uses the `(id_run, created)` index
rather than the product-metrics join.

## Ordering and dependency notes

- Depends on Phase 1 (A4 `(id_sample_tmp, created)` and `(id_run,
  created)` recency indexes; the study-scoped `(id_study_lims, created)`
  aggregate). A6 normalised dates are unrelated here (they back Phase 6).
- Items are sequential because both edit `mlwh/availability.go`: E1 adds
  `created` + order/window to the three scopes, E2 adds the latest-data
  endpoints and CLI on top.
- `created` becomes a selectable `export irods` column, so the export
  layer (Phase 4) is a soft prerequisite for the column-selection AT; the
  endpoint/order/window behaviour here does not otherwise depend on it.
- Registry entries and Description text for the latest-data endpoints are
  consolidated in Phase 10 (J).
