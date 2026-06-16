# Phase 1: mlwh query surface foundations

Ref: [spec.md](spec.md) sections E1, C1, C2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

These are pure `mlwh`-package additions/moves with no server yet.

## Items

### Item 1.1: E1 - ExpandSearchValues returns SearchValues end to end

spec.md section: E1

Reshape `(*Client).ExpandSearchValues` in `mlwh/hierarchy.go` to return
`(SearchValues, error)` (new named struct `Samples`/`Runs`/`Lanes`)
instead of three slices. `ExpandIdentifier` (`[]TaggedID`) and
`ExpandSampleSearchValues` (`[]string`) are unchanged. The
`results`/`cmd` callers are updated in Phase 4; this item covers the
`mlwh`-package change and its tests
(`mlwh/hierarchy_test.go`). Covering all 3 acceptance tests from E1
(the registry/server assertion in E1.3 is satisfied in Phase 2).

This item should be done before Item 1.3 if any detail/Enrich builder
uses search values; otherwise it may run in parallel.

- [ ] implemented
- [ ] reviewed

### Item 1.2: C1 - detail-builder methods on Client

spec.md section: C1

Move `SampleDetail`/`StudyDetail`/`RunDetail`/`LibraryDetail` from
`seqmeta/enrich.go` into `mlwh/enrich.go` as cache-backed methods that
compose existing hierarchy reads (`SamplesForStudy`,
`LibrariesForStudy`, `LanesForSample`, `IRODSPathsForSample`,
`StudiesForSample`, `SamplesForRun`). No new columns. Tests in
`mlwh/enrich_test.go`. Covering all 5 acceptance tests from C1.

- [ ] implemented
- [ ] reviewed

### Item 1.3: C2 - Enrich composite method preserves the contract

spec.md section: C2

Implement `Client.Enrich(ctx, identifier)` in `mlwh/enrich.go`, moving
the classifier cascade and graph assembly wholesale from
`seqmeta/enrich.go` and composing the C1 detail methods. Returns
`EnrichmentResult` value. Preserve `MaxSamplesPerHop` truncation and
`Partial`/`MissingHop` semantics, and the field-for-field JSON graph
contract. Move `EnrichmentResult`/`EnrichmentGraph`/`MissingHop` and the
hop/reason constants into `mlwh/types.go`. Tests in `mlwh/enrich_test.go`.
Covering all 5 acceptance tests from C2.

Depends on Item 1.2 (uses the detail builders) and, if applicable, Item
1.1.

- [ ] implemented
- [ ] reviewed
