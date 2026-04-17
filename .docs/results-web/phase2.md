# Phase 2: Go search enhancements

Ref: [spec.md](spec.md) sections D1, E1, F1, F2

## Instructions

Use the `orchestrator` skill to complete this phase,
coordinating subagents with the `go-implementor` and
`go-reviewer` skills.

## Items

### Batch 1 (parallel)

#### Item 2.1: D1 - Multi-value OR search [parallel with 2.2, 2.3]

spec.md section: D1

Add `MultiSearchParams` type to `results/types.go` with
slice fields for each scalar and `Meta map[string][]string`.
Add `Store.SearchMulti(ctx, params)` to `results/store.go`
generating `IN (?, ?)` for multi-value scalars and
`EXISTS ... value IN (?, ?)` subqueries for multi-value
metadata. Update `results/server.go`: add
`multiSearchParamsFromRequest` replacing the single-value
parser in the search handler. Existing `Search` method and
`SearchParams` remain for backward compatibility. Tests in
`results/store_test.go` and `results/server_test.go`.
Covering all 8 acceptance tests from D1.

- [x] implemented
- [x] reviewed

#### Item 2.2: F1 - List all studies [parallel with 2.1, 2.3]

spec.md section: F1

Add `handleListStudies` handler to `seqmeta/server.go`.
Calls `provider.AllStudies(ctx)` and returns
`[]saga.Study` as JSON. Register route
`router.Get("/studies", server.handleListStudies)`. Tests
in `seqmeta/server_test.go`. Covering all 4 acceptance
tests from F1.

- [x] implemented
- [x] reviewed

#### Item 2.3: F2 - List samples for a study [parallel with 2.1, 2.2]

spec.md section: F2

Add `handleStudySamples` handler to `seqmeta/server.go`.
Calls `provider.AllSamplesForStudy(ctx, id)` and returns
`[]saga.MLWHSample` as JSON. Register route
`router.Get("/study/{id}/samples",
server.handleStudySamples)`. Return 404 for
`saga.ErrNotFound`, 502 for other errors. Tests in
`seqmeta/server_test.go`. Covering all 5 acceptance tests
from F2.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).

### Item 2.4: E1 - Study ID search shortcut

spec.md section: E1

Depends on item 2.1 (D1) for `MultiSearchParams` and
`SearchMulti`.

Add `SeqmetaSampleResolver` struct and
`NewSeqmetaSampleResolver(baseURL, timeout)` to
`results/server.go`. Add `SamplesForStudy(ctx, studyID)`
method that calls `GET <seqmeta-url>/study/<id>/samples`.
Update `NewServer` to accept an optional resolver parameter.
When `study_id` query param is present: resolve samples,
merge SangerIDs into `MultiSearchParams.Meta` under
`seqmeta_sampleid`, search, and wrap results as
`[]SearchResult` with per-result `MatchedSamples`. Return
400 if seqmeta not configured, 502 on seqmeta error. When
`study_id` is absent, response remains plain `[]ResultSet`.
Tests in `results/server_test.go`. Covering all 8
acceptance tests from E1.

- [x] implemented
- [x] reviewed
