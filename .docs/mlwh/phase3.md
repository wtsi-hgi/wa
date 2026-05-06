# Phase 3: Hierarchy and Expansion

Ref: [spec.md](spec.md) sections C1, C2, C3, C4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Batch 1 (parallel)

#### Item 3.1: C1 - SamplesForStudy / SamplesForRun / SamplesForLibrary [parallel with 3.2, 3.3]

spec.md section: C1

Implement the three `Samples*` methods in `mlwh/hierarchy.go`. Cache-
backed joins of `library_samples` to `sample_mirror`; cold-cache
read-through write-back; `ErrNotFound` on missing parent, empty slice
on parent-with-no-children. Honour `limit`/`offset`. Covers all 8
acceptance tests from C1.

- [ ] implemented
- [ ] reviewed

#### Item 3.2: C2 - LibrariesForStudy, RunsForStudy, LanesForSample, IRODSPathsFor\* [parallel with 3.1, 3.3]

spec.md section: C2

Implement `LibrariesForStudy` (with `SampleCount` distinct per
`(pipeline_id_lims, id_study_lims)`), `RunsForStudy`, `LanesForSample`,
`IRODSPathsForSample`, `IRODSPathsForStudy`, and `StudyForSample` in
`mlwh/hierarchy.go`. Same parent-existence contract as C1. Covers all
6 acceptance tests from C2.

- [ ] implemented
- [ ] reviewed

#### Item 3.3: C4 - AllStudies cache-backed enumeration [parallel with 3.1, 3.2]

spec.md section: C4

Implement `(*Client).AllStudies(ctx, limit, offset)` reading from
`study_mirror` when warm, falling back to a single MLWH `SELECT ...
WHERE id_lims = 'SQSCP' ORDER BY id_study_lims LIMIT ? OFFSET ?` and
upserting rows back into `study_mirror` without advancing the
watermark. Covers all 6 acceptance tests from C4.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).

### Item 3.4: C3 - ExpandIdentifier

spec.md section: C3

Implement `(*Client).ExpandIdentifier(ctx, kind, canonical)` using the
hierarchy methods from items 3.1 and 3.2 plus the in-process
`expandIdentifierTTL = 5 * time.Minute` result cache, invalidated on
`Sync` commit. Output is `[]TaggedID` in deterministic sorted order.
Depends on items 3.1, 3.2, 3.3. Covers all 7 acceptance tests from C3.

- [ ] implemented
- [ ] reviewed
