# Phase 3: MLWH Endpoints

Ref: [spec.md](spec.md) sections D1, D2, D3, D4, D5, D6

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 3.1: D1 - List studies (paginated)

spec.md section: D1

Implement `MLWHClient.ListStudies(ctx, PageOptions)` and
`MLWHClient.AllStudies(ctx)` in `saga/mlwh.go`. Establish the
paginated response pattern with `PaginatedResponse[Study]`,
auto-pagination collecting all pages, and partial error returns.
Implement `PageOptions` query parameter encoding. Test in
`saga/mlwh_test.go` covering all 4 acceptance tests from D1.

- [ ] implemented
- [ ] reviewed

### Batch 1 (parallel, after 3.1 is reviewed)

#### Item 3.2: D2 - Get study [parallel with 3.3, 3.4, 3.5, 3.6]

spec.md section: D2

Implement `MLWHClient.GetStudy(ctx, studyID)` in `saga/mlwh.go`.
Calls `GET /integrations/mlwh/studies/{studyID}`. Test in
`saga/mlwh_test.go` covering all 2 acceptance tests from D2.

- [ ] implemented
- [ ] reviewed

#### Item 3.3: D3 - List samples (paginated) [parallel with 3.2, 3.4, 3.5, 3.6]

spec.md section: D3

Implement `MLWHClient.ListSamples(ctx, PageOptions)` and
`MLWHClient.AllSamples(ctx)` in `saga/mlwh.go`. Handle nullable
`total` field; auto-pagination stops on empty page. Test in
`saga/mlwh_test.go` covering all 3 acceptance tests from D3.

- [ ] implemented
- [ ] reviewed

#### Item 3.4: D4 - List faculty sponsors [parallel with 3.2, 3.3, 3.5, 3.6]

spec.md section: D4

Implement `MLWHClient.ListFacultySponsors(ctx, PageOptions)` and
`MLWHClient.AllFacultySponsors(ctx)` in `saga/mlwh.go`. Reuses
pagination pattern from D1. Test in `saga/mlwh_test.go` covering
the 1 acceptance test from D4.

- [ ] implemented
- [ ] reviewed

#### Item 3.5: D5 - List programmes [parallel with 3.2, 3.3, 3.4, 3.6]

spec.md section: D5

Implement `MLWHClient.ListProgrammes(ctx)` in `saga/mlwh.go`.
Simple array response (no pagination). Test in `saga/mlwh_test.go`
covering the 1 acceptance test from D5.

- [ ] implemented
- [ ] reviewed

#### Item 3.6: D6 - List data release strategies [parallel]

spec.md section: D6

Implement `MLWHClient.ListDataReleaseStrategies(ctx)` in
`saga/mlwh.go`. Simple array response. Test in `saga/mlwh_test.go`
covering the 1 acceptance test from D6.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
