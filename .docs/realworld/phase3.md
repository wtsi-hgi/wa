# Phase 3: Run overview (D1) + Budget-safety (E1-E3)

Ref: [spec.md](spec.md) sections D1, E1, E2, E3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both (four-step add-a-query recipe, count<->list
cross-check, never-synced cascade, supported test boundaries: HTTP
contract, typed `Client`/`RemoteClient` results, persisted state).

These four items are largely independent of each other (distinct files
and surfaces) and can be implemented in parallel. They all depend on
Phase 1 but are independent of Phase 2.

## Items

### Batch 1 (parallel)

#### Item 3.1: D1 - cheap run overview [parallel with 3.2, 3.3, 3.4]

spec.md section: D1

Implement `RunOverview(ctx, idRun string) (RunOverview, error)` in
`mlwh/availability.go` -> `RunOverview` (distinct samples / studies /
iRODS data objects on the run, `sequencing_date_range` from iRODS
`created`, freshness). `:id` is the Illumina NPG `id_run` (state in the
Description; non-Illumina -> not-found). Keep it a separate small
aggregate (NOT folded into `/run/:id/detail`, NOT added to the bare
`Run` struct). Test in `mlwh/availability_test.go`. Covering all 3
acceptance tests from D1 (samples=4/studies=2/data_objects=6 with
date-range; never-synced ErrCacheNeverSynced+ErrNotFound; invalid run
ErrNotFound). Depends on Phase 1 (iRODS `created`/`platform`).

- [ ] implemented
- [ ] reviewed

#### Item 3.2: E1 - /count counterpart for every paginated list [parallel with 3.1, 3.3, 3.4]

spec.md section: E1

Extend `mlwh/count.go` with a `Count` endpoint per paginated list (each
`queryCount` + four-step recipe, same filter/join as its list, no LIMIT,
so count == len(list-all)): `/study/:id/irods/count`,
`/sample/:id/irods/count`, `/study/:id/runs/count`,
`/study/:id/libraries/count`, `/sample/:id/lanes/count`,
`/run/:id/samples/count`, the `library*/samples` counts, and the
`find/sample/*` counts. Test in `mlwh/count_test.go`. Covering all 3
acceptance tests from E1, applied to each new count (count == len(list);
never-synced ErrCacheNeverSynced+ErrNotFound as its list; synced-empty
parent -> Count{0}). Depends on Phase 1.

- [ ] implemented
- [ ] reviewed

#### Item 3.3: E2 - list-sizing response headers + typed Page[T] remote variant [parallel with 3.1, 3.2, 3.4]

spec.md section: E2

In `mlwh/server.go`, set `X-Total-Count` (total matching rows; one extra
COUNT query) and `X-Next-Offset` (`offset+len(items)` if more remain,
else `-1`) on paginated list handlers; bodies stay bare JSON arrays. Add
`Page[T]` typed paged-variant methods on `RemoteClient` (`mlwh/remote.go`)
that parse those headers in the single remote header path; the `Page[T]`
type lives in `mlwh/types.go`. Existing bare-slice methods stay
unchanged; do NOT expose sizing via the dynamic `Call` dispatcher or a
stateful "last page meta" accessor. Test in `mlwh/server_test.go` and
`mlwh/remote_test.go`. Covering all 3 acceptance tests from E2 (first
page headers 25/10; last page headers 25/-1; `Page[T]` parses
Total=25/NextOffset=10 with Items equal to the bare-slice result).
Depends on Phase 1.

- [ ] implemented
- [ ] reviewed

#### Item 3.4: E3 - lean / de-duplicated detail aggregates [parallel with 3.1, 3.2, 3.3]

spec.md section: E3

In `mlwh/enrich.go` (detail builders), with supporting changes in
`mlwh/types.go`, `mlwh/server.go`, `mlwh/registry.go`: add `limit`/
`offset` pagination of nested collections (libraries/samples for study;
samples/studies/study_details for run); add a boolean `lean` query param
that drops the heavy nested objects (top-level entity + flat id lists);
de-duplicate repeated nested entities into a per-id lookup table. Test in
`mlwh/enrich_test.go`. Covering all 3 acceptance tests from E3 (distinct
study/library once in a lookup with id references, no duplicate
embedding; `lean=true` omits heavy nested objects with strictly smaller
serialized size; `/run/:id/detail?limit=2` returns at most 2 nested
samples and `X-Total-Count` reports the full count). Depends on Phase 1;
the `X-Total-Count` assertion in E3.3 relies on the header work in 3.3.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

## Ordering and dependency notes

- This phase depends on Phase 1 being fully reviewed; it is independent
  of Phase 2 and may run in parallel with it.
- All four items are independent and form a single parallel batch. One
  soft coupling: E3's third acceptance test asserts `X-Total-Count` on
  `/run/:id/detail`, which is produced by E2's header work (3.3).
  Implement 3.3's header helper such that 3.4 can reuse it; the reviewer
  should confirm 3.4 does not re-implement header logic. If the
  orchestrator prefers strict isolation, sequence 3.4 immediately after
  3.3 within the batch.
- Per-endpoint Registry/handler/RemoteClient wiring is done incrementally
  here (per the spec's G note); Phase 5 only does the final doc
  regeneration and drift-guard verification.
