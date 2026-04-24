# Phase 1: SAGA find-samples methods

Ref: [spec.md](spec.md) sections A1, A2, A3, A4, A5, A6

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Batch 1 (parallel)

#### Item 1.1: A1 - FindSamplesBySangerID

spec.md section: A1

Implement `FindSamplesBySangerID(ctx, sangerID string)
([]MLWHSample, error)` on `saga.MLWHClient` hitting
`/integrations/mlwh/samples` with `filters={"sanger_id":<id>}`,
including pagination. Establishes the filter-key pattern for
A2-A5. Covers all 4 acceptance tests from A1.

- [ ] implemented
- [ ] reviewed

#### Item 1.2: A6 - StudyForSample helper

spec.md section: A6

Implement `StudyForSample(ctx, sample MLWHSample) (*Study, error)`
on `saga.Client` which calls `GetStudy(sample.IDStudyLims)` or
returns `saga.ErrNotFound` when the sample has no study id.
Independent of A1-A5. Covers all 2 acceptance tests from A6.

- [ ] implemented
- [ ] reviewed

### Batch 2 (parallel, after batch 1 is reviewed)

#### Item 1.3: A2 - FindSamplesByIDSampleLims

spec.md section: A2

Implement `FindSamplesByIDSampleLims(ctx, lims string)
([]MLWHSample, error)` using filter key `id_sample_lims`. Covers
all 2 acceptance tests from A2.

- [ ] implemented
- [ ] reviewed

#### Item 1.4: A3 - FindSamplesByRunID

spec.md section: A3

Implement `FindSamplesByRunID(ctx, runID int) ([]MLWHSample,
error)` using filter key `id_run` (value encoded as JSON string).
Covers all 2 acceptance tests from A3.

- [ ] implemented
- [ ] reviewed

#### Item 1.5: A4 - FindSamplesByLibraryType

spec.md section: A4

Implement `FindSamplesByLibraryType(ctx, libraryType string)
([]MLWHSample, error)` using filter key `library_type`. Covers
the 1 acceptance test from A4.

- [ ] implemented
- [ ] reviewed

#### Item 1.6: A5 - FindSamplesByAccessionNumber

spec.md section: A5

Implement `FindSamplesByAccessionNumber(ctx, acc string)
([]MLWHSample, error)` using filter key `accession_number`.
Covers the 1 acceptance test from A5.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).
