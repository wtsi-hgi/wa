# Phase 7: Key Use Cases

Ref: [spec.md](spec.md) sections G1, G2, G3, G4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Batch 1 (parallel)

#### Item 7.1: G1 - All metadata for a sample [parallel with 7.2]

spec.md section: G1

Implement `Client.SampleAllMetadata(ctx, sangerID)` in
`saga/usecases.go`. Merges MLWH sample rows (all runs/lanes) with
iRODS file metadata. Collects AVUs from all iRODS files into a
deduplicated `map[string][]string`. Uses `SampleMetadata` domain
type. Depends on MLWH and iRODS sub-clients from Phases 3-4. Test
in `saga/usecases_test.go` covering all 3 acceptance tests from
G1.

- [ ] implemented
- [ ] reviewed

#### Item 7.2: G2 - All samples in a study [parallel with 7.1]

spec.md section: G2

Implement `Client.StudyAllSamples(ctx, studyID)` in
`saga/usecases.go`. Auto-paginates all MLWH samples and filters
client-side by `id_study_lims`. Returns partial results with error
if pagination fails mid-way. Uses `StudySamples` domain type.
Depends on MLWH sub-client from Phase 3. Test in
`saga/usecases_test.go` covering all 3 acceptance tests from G2.

- [ ] implemented
- [ ] reviewed

### Batch 2 (parallel, after batch 1 is reviewed)

#### Item 7.3: G3 - iRODS files for a sample [parallel with 7.4]

spec.md section: G3

Implement `Client.SampleIRODSFiles(ctx, sangerID, filter)` in
`saga/usecases.go`. Calls `IRODS().GetSampleFiles()` then applies
client-side `FilterOptions` (by `AnalysisType` and `Metadata`
map). Uses `SampleFiles` domain type. Depends on iRODS sub-client
from Phase 4. Test in `saga/usecases_test.go` covering all 4
acceptance tests from G3.

- [ ] implemented
- [ ] reviewed

#### Item 7.4: G4 - iRODS files for a study [parallel with 7.3]

spec.md section: G4

Implement `Client.StudyIRODSFiles(ctx, studyID, filter)` in
`saga/usecases.go`. Finds iRODS samples with `avu:study_id`
matching, fetches files for each. Falls back to MLWH
cross-reference if iRODS metadata is insufficient. Applies
client-side `FilterOptions`. Uses `StudyFiles` domain type.
Depends on iRODS and MLWH sub-clients from Phases 3-4. Test in
`saga/usecases_test.go` covering all 5 acceptance tests from G4.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
