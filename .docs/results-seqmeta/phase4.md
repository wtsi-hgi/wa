# Phase 4: Enrichment algorithm

Ref: [spec.md](spec.md) sections C1, C2, C3, C4, C5, C6, C7, C8,
C9, C10

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 4.1: C1 - study_id enrichment

spec.md section: C1

Implement the `study_id` classification hop and the shared
`enrichStudy` helper that populates `Graph.Studies`,
`Graph.Libraries`, and `Graph.Samples` from a study id. Establishes
the hop-dispatch and partial/`MissingHop` pattern used by C2-C8.
Covers all 2 acceptance tests from C1.

- [x] implemented
- [x] reviewed

### Item 4.2: C2 - study_accession enrichment

spec.md section: C2

Implement the `study_accession` hop that resolves an accession to a
study id and then reuses the C1 pipeline. Depends on item 4.1.
Covers all 2 acceptance tests from C2.

- [x] implemented
- [x] reviewed

### Batch 3 (parallel, after batch 2 is reviewed)

#### Item 4.3: C3 - sanger_sample_id enrichment [parallel with 4.4, 4.5, 4.6, 4.7, 4.8]

spec.md section: C3

Implement the `sanger_sample_id` classification hop using
`FindSamplesBySangerID` plus `StudyForSample` and the shared study
pipeline. Covers all 3 acceptance tests from C3.

- [x] implemented
- [x] reviewed

#### Item 4.4: C4 - sample_lims_id enrichment [parallel with 4.3, 4.5, 4.6, 4.7, 4.8]

spec.md section: C4

Implement the `sample_lims_id` hop using
`FindSamplesByIDSampleLims`. Covers the 1 acceptance test from C4.

- [x] implemented
- [x] reviewed

#### Item 4.5: C5 - sample_accession enrichment [parallel with 4.3, 4.4, 4.6, 4.7, 4.8]

spec.md section: C5

Implement the `sample_accession` hop using
`FindSamplesByAccessionNumber`. Covers the 1 acceptance test from
C5.

- [x] implemented
- [x] reviewed

#### Item 4.6: C6 - run_id enrichment [parallel with 4.3, 4.4, 4.5, 4.7, 4.8]

spec.md section: C6

Implement the `run_id` hop using `FindSamplesByRunID`. Covers all
3 acceptance tests from C6.

- [x] implemented
- [x] reviewed

#### Item 4.7: C7 - library_type enrichment and fan-out cap [parallel with 4.3, 4.4, 4.5, 4.6, 4.8]

spec.md section: C7

Implement the `library_type` hop using `FindSamplesByLibraryType`
and enforce `MaxLibrarySamples = 1000` with a `samples_truncated`
flag at status 200 (libraries and studies remain complete). Covers
all 3 acceptance tests from C7.

- [x] implemented
- [x] reviewed

#### Item 4.8: C8 - project_name enrichment [parallel with 4.3, 4.4, 4.5, 4.6, 4.7]

spec.md section: C8

Implement the four-hop `project_name` classification path
(project -> studies -> samples -> libraries). Covers all 2
acceptance tests from C8.

- [x] implemented
- [x] reviewed

### Batch 4 (parallel, after batch 3 is reviewed)

#### Item 4.9: C9 - All classification hops fail with 5xx [parallel with 4.10]

spec.md section: C9

Implement the 502 decision: when every classification hop reports
a 5xx/transport failure, the handler returns 502 (not 404). Covers
the 1 acceptance test from C9.

- [x] implemented
- [x] reviewed

#### Item 4.10: C10 - Unknown identifier [parallel with 4.9]

spec.md section: C10

Implement the 404 decision: when every classification hop reports
empty/not-found, the handler returns 404. Covers all 2 acceptance
tests from C10.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).
