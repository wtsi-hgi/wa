# Phase 4: iRODS Endpoints

Ref: [spec.md](spec.md) sections E1, E2, E3, E4, E5

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Batch 1 (parallel)

#### Item 4.1: E1 - iRODS root [parallel with 4.2, 4.3, 4.4, 4.5]

spec.md section: E1

Implement `IRODSClient.Ping(ctx) error` in `saga/irods.go`. Calls
`GET /integrations/irods/` and returns nil on 200. Test in
`saga/irods_test.go` covering all 2 acceptance tests from E1.

- [x] implemented
- [x] reviewed

#### Item 4.2: E2 - List iRODS samples (paginated) [parallel]

spec.md section: E2

Implement `IRODSClient.ListSamples(ctx, PageOptions)` and
`IRODSClient.AllSamples(ctx)` in `saga/irods.go`. Reuses
pagination pattern from Phase 3. Handles nested `data` maps
with `map[string]any`. Test in `saga/irods_test.go` covering
all 3 acceptance tests from E2.

- [x] implemented
- [x] reviewed

#### Item 4.3: E3 - Get sample files [parallel with 4.1, 4.2, 4.4, 4.5]

spec.md section: E3

Implement `IRODSClient.GetSampleFiles(ctx, sangerID)` in
`saga/irods.go`. Calls
`GET /integrations/irods/samples/{sanger_id}` and returns
`[]IRODSFile`. Test in `saga/irods_test.go` covering all 3
acceptance tests from E3.

- [x] implemented
- [x] reviewed

#### Item 4.4: E4 - Web summary [parallel with 4.1, 4.2, 4.3, 4.5]

spec.md section: E4

Implement `IRODSClient.GetWebSummary(ctx, collection)` in
`saga/irods.go`. Returns raw `[]byte` from
`GET /integrations/irods/web-summary/{collection}`. Test in
`saga/irods_test.go` covering the 1 acceptance test from E4.

- [x] implemented
- [x] reviewed

#### Item 4.5: E5 - List analysis types [parallel with 4.1, 4.2, 4.3, 4.4]

spec.md section: E5

Implement `IRODSClient.ListAnalysisTypes(ctx)` in `saga/irods.go`.
Simple array response returning `[]IRODSAnalysisType`. Test in
`saga/irods_test.go` covering the 1 acceptance test from E5.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
