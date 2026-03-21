# Phase 6: Saga Samples & Studies

Ref: [spec.md](spec.md) sections H1, H2, H3, H4, H5

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 6.1: H1 - List Saga samples (paginated)

spec.md section: H1

Implement `SamplesClient.List(ctx, PageOptions)` and
`SamplesClient.All(ctx)` in `saga/samples.go`. Reuses pagination
pattern from Phase 3. Test in `saga/samples_test.go` covering all
3 acceptance tests from H1.

- [x] implemented
- [x] reviewed

### Batch 1 (parallel, after 6.1 is reviewed)

#### Item 6.2: H2 - Create Saga sample [parallel with 6.3, 6.4, 6.5]

spec.md section: H2

Implement `SamplesClient.Create(ctx, source, sourceID)` in
`saga/samples.go`. Calls `POST /samples/` with
`CreateSampleRequest` body and invalidates the samples list cache.
Test in `saga/samples_test.go` covering all 2 acceptance tests
from H2.

- [x] implemented
- [x] reviewed

#### Item 6.3: H3 - Get sample by source [parallel with 6.2, 6.4, 6.5]

spec.md section: H3

Implement `SamplesClient.GetBySource(ctx, source, sourceID)` in
`saga/samples.go`. Calls
`GET /samples/{source}/{source_id}`. Test in
`saga/samples_test.go` covering all 2 acceptance tests from H3.

- [x] implemented
- [x] reviewed

#### Item 6.4: H4 - Get samples for study by source [parallel with 6.2, 6.3, 6.5]

spec.md section: H4

Implement `SamplesClient.GetStudySamples(ctx, source, studySourceID)`
in `saga/samples.go`. Calls
`GET /samples/{source}/studies/{study_source_id}`. Test in
`saga/samples_test.go` covering all 3 acceptance tests from H4.

- [x] implemented
- [x] reviewed

#### Item 6.5: H5 - List Saga studies [parallel with 6.2, 6.3, 6.4]

spec.md section: H5

Implement `SamplesClient.ListStudies(ctx)` in `saga/samples.go`.
Calls `GET /studies/` returning `[]SagaStudy`. Test in
`saga/samples_test.go` covering all 2 acceptance tests from H5.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
