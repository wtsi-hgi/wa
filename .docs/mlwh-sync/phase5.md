# Phase 5: Empty-cache semantics

Ref: [spec.md](spec.md) sections D1, D2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Batch 1 (parallel)

#### Item 5.1: D1 - Never-synced cache returns ErrCacheNeverSynced [parallel with 5.2]

spec.md section: D1

Wire `ErrCacheNeverSynced` through `mlwh/resolver.go`,
`mlwh/resolver_sample.go`, `mlwh/hierarchy.go`,
`mlwh/all_studies.go`, `cmd/mlwh_info.go`, and
`seqmeta/server.go`. Every read entry point inspects the
`sync_state` rows it needs and, when all are absent, returns
`fmt.Errorf("%w: %w", ErrNotFound, ErrCacheNeverSynced)`. List
endpoints return an empty slice alongside the wrapped error.
Partially-synced caches behave per the spec's classification
rules. Covers all 5 acceptance tests from D1.

- [ ] implemented
- [ ] reviewed

#### Item 5.2: D2 - No automatic re-sync from reads [parallel with 5.1]

spec.md section: D2

Confirm by test (Sync-wrapper recording invocations + grep guard
that `ensureResolverTableSynced` and `hasResolverSyncState` no
longer exist) that `ResolveLibrary`, `ResolveSample`,
`ResolveStudy`, `ResolveRun`, and `ClassifyIdentifier` never call
`Sync`. Any residual call sites must be removed. Covers both
acceptance tests from D2.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).
