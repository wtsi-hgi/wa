# Phase 3: Enrichment cache

Ref: [spec.md](spec.md) sections D1, D2, D3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 3.1: D1 - Cache schema and LoadEnrichCache

spec.md section: D1

Add the `enrich_cache` SQLite table (or equivalent) to
`seqmeta.Store`, plus `LoadEnrichCache`, `SaveEnrichCache`,
`DeleteEnrichCache`, and `InvalidateEnrichFor` methods using
`WithLock`. Establishes the cache primitives D2 and D3 build on.
Covers all 5 acceptance tests from D1.

- [x] implemented
- [x] reviewed

### Batch 2 (parallel, after batch 1 is reviewed)

#### Item 3.2: D2 - Expiry is never served [parallel with 3.3]

spec.md section: D2

`LoadEnrichCache` returns the raw row (including `FetchedAt` and
`TTL`); the freshness check is performed at the call site via
`entry.FetchedAt.Add(entry.TTL).Before(time.Now())` so that expired
entries are treated as absent and never served. Covers the 1
acceptance test from D2.

- [x] implemented
- [x] reviewed

#### Item 3.3: D3 - Invalidation on diff mutations [parallel with 3.2]

spec.md section: D3

Implement `InvalidateEnrichFor(queryKind, queryID)` deletion logic
so that `queryKind == "study_samples"` removes matching entries by
identifier and by `id_study_lims` substring in the body, and
`queryKind == "sample_files"` removes only the entry whose
identifier equals `queryID`. Wiring into the HTTP diff handlers
(`handleStudyDiff`/`handleSampleDiff`) is out of scope here and
belongs to phase 5. Covers all 4 acceptance tests from D3.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).
