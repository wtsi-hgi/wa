# Phase 4: Cross-cutting registry coverage and full-sync guarantees (E1-E2)

Ref: [spec.md](spec.md) sections E1, E2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those skills
reference `go-conventions` and `testing-principles`; ensure subagents
follow both. Tests are GoConvey acceptance tests.

This phase is test-only and confirmatory: the production reshapes already
landed (A in Phase 2, B in Phase 3; the `iseq_product_metrics` registry
entries and the `TestRealworldA3...` revision landed in Phase 3, and the
`seq_product_irods_locations` registry entries' text updated automatically
in Phase 2 with unchanged arg counts 1/3). It depends on A and B landing.
It adds the cross-cutting assertions and confirms the verified-unchanged
existing tests still pass.

Do NOT run the live sync or any db/network command; the real-MLWH prepare
check (E1.2) is gated on `WA_MLWH_DSN` and skips without creds - do not
run it here. Bound every shell command with `timeout`. Feature-wide: no
source schema change, no `CacheSchemaVersion` bump; `supportedSyncTables`
and `Client.Sync` fan-out are NOT modified (per testing-principles, no new
test asserts unchanged fan-out beyond E2's guard).

## Items

### Batch 1 (parallel)

The two items add independent assertions over already-landed code in
disjoint test files. Use one subagent per item.

#### Item 4.1: E1 - source-query registry coverage [parallel with 4.2]

spec.md section: E1

Add a unit test over `AllSyncSourceQueries()` asserting the full reshaped
registry: the reshaped `seq_product_irods_locations` incremental (ArgCount
1) and from-cursor (3); the `iseq_product_metrics` Phase-1 incremental (1)
and from-cursor (3); the scoped `iseq_product_metrics composite recovery`
representative (1); and the cold and legacy entries for both tables still
present with unchanged ArgCounts. Keep the gated real-MLWH prepare check
(`TestSyncSourceSchemaMatchesRealMLWH`, gated on `WA_MLWH_DSN`) green:
every registered query prepares with its `ArgCount` placeholders. Covering
both acceptance tests from E1. Test file:
`mlwh/sync_source_integration_test.go`.

- [x] implemented
- [x] reviewed

#### Item 4.2: E2 - full-sync + legacy fallback [parallel with 4.1]

spec.md section: E2

Add the two cross-cutting guarantees and confirm the verified-unchanged
tests pass. Covering both acceptance tests from E2 (a single combined
`openRealMLWHSchemaSource` fixture with changed rows for all four reshaped
tables plus `sample`/`study` and existing warm `sync_state`, run through
one `Client.Sync`-equivalent multi-table sync, returns one report per
supported table with all four reshaped mirrors populated - no reshaped
table skipped; a source whose composition query raises an
unsupported-`JSON_TABLE` error makes both `seq_product_irods_locations`
and `iseq_product_metrics` fall back to their legacy queries and complete
without error). The `iseq_product_metrics` fallback fixture MUST include
at least one changed multi-component candidate so Phase-3 composite
recovery runs and its `JSON_TABLE` query triggers the fallback (Phase 1
issues no `JSON_TABLE`, so without a composite candidate the assertion is
vacuous). Confirm the existing full-sync and legacy-fallback tests (e.g.
`TestSyncAgainstRealMLWHSchema` asserting `len(reports) ==
len(supportedSyncTables)`) still pass unchanged. Test files:
`mlwh/sync_real_schema_test.go`, `mlwh/sync_test.go`,
`mlwh/sync_a5_test.go`.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item. Launch review
subagents using the `go-reviewer` skill (review both items in the batch
together in a single review pass).

## Ordering and dependency notes

- Depends on Phase 2 (A) and Phase 3 (B) landing; run last.
- Test-only: no production change remains. The `iseq_product_metrics`
  registry entries and the `TestRealworldA3...` revision landed in Phase
  3; the `seq_product_irods_locations` registry text updated in Phase 2
  (arg counts unchanged 1/3), so E1 here only ASSERTS the registry, it
  does not edit it.
- E2's legacy-fallback test MUST seed a changed multi-component candidate
  so Phase-3 composite recovery actually issues a `JSON_TABLE` query to
  trigger the fallback.
- `supportedSyncTables` / `Client.Sync` fan-out unchanged; only E2's guard
  asserts the per-table report set, per testing-principles.
