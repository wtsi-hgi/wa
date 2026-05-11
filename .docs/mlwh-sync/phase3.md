# Phase 3: Read-path correctness

Ref: [spec.md](spec.md) sections C1, C3, C4, C5, C6, C7

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 3.1: C6 - Remove live-MLWH read fallback

spec.md section: C6

Remove every `*SourceSQL` path in `mlwh/hierarchy.go`,
`queryAllStudiesSource` / `upsertAllStudiesReadThrough` in
`mlwh/all_studies.go`, `upsertHierarchyReadThrough`,
`ensureResolverTableSynced`, `hasResolverSyncState`, and the
remaining `negative_cache` / `enrich_cache` reads and writes.
`Client.syncSource` is no longer consulted by any read path.
Covers all 3 acceptance tests from C6. This item is the
foundation other phase-3 items build on; it should land first.

- [ ] implemented
- [ ] reviewed

### Item 3.2: C1 - StudiesForSample replaces StudyForSample

spec.md section: C1

Replace `StudyForSample` with `StudiesForSample(ctx, sangerName)
([]Study, error)` in `mlwh/hierarchy.go`: join `library_samples`
to `study_mirror` on `id_study_lims` (with `id_lims='SQSCP'`),
filter by `sample_mirror.name`, order by
`study_mirror.id_study_lims`. Covers all 4 acceptance tests from
C1. Depends on item 3.1 (live-MLWH paths must be gone first).

- [ ] implemented
- [ ] reviewed

### Batch 3 (parallel, after item 3.2 is reviewed)

#### Item 3.3: C3 - FindSamplesBy<Column> one-per-method [parallel with 3.4, 3.5, 3.6]

spec.md section: C3

Implement `FindSamplesBySangerID`, `FindSamplesByIDSampleLims`,
`FindSamplesByAccessionNumber`, `FindSamplesBySupplierName`,
`FindSamplesByLibraryType` as one indexed query per column with
`LIMIT 2`, `id_lims = 'SQSCP'` where applicable, returning a
1-element slice, `ErrAmbiguous` (mentioning both candidate PKs),
or `ErrNotFound`. Covers all 4 acceptance tests from C3.

- [ ] implemented
- [ ] reviewed

#### Item 3.4: C4 - Resolver ambiguity rules [parallel with 3.3, 3.5, 3.6]

spec.md section: C4

Update `mlwh/resolver.go` and `mlwh/resolver_sample.go` so every
non-unique-by-construction column uses `LIMIT 2` +
`ErrAmbiguous`: `ResolveSample` donor_id step,
`ResolveStudy` accession_number step (and name step retained).
Rename `resolveStudyFromCacheWithWarmup` to
`resolveStudyFromCache`. Fix `ClassifyIdentifier` integer branch
to drop the spurious `uuid_sample_lims = ?` lookup. Ensure the
cross-column cascade in `ResolveSample` surfaces ambiguity with
both PKs and the raw input. Covers all 6 acceptance tests from
C4.

- [ ] implemented
- [ ] reviewed

#### Item 3.5: C5 - Deterministic pagination [parallel with 3.3, 3.4, 3.6]

spec.md section: C5

Append `, sample_mirror.id_sample_tmp` to every
`ORDER BY <sample>.name LIMIT ? OFFSET ?` in `mlwh/hierarchy.go`
(`SamplesForStudy`, `SamplesForLibrary`, `SamplesForLibraryType`).
Covers both acceptance tests from C5.

- [ ] implemented
- [ ] reviewed

#### Item 3.6: C7 - No mid-read cache writes [parallel with 3.3, 3.4, 3.5]

spec.md section: C7

Add the assertion test in `mlwh/hierarchy_test.go` that 10
concurrent hierarchy reads never open a write transaction, and
the build/grep guard proving `upsertHierarchyReadThrough` no
longer exists in the package. Covers both acceptance tests from
C7. Depends on item 3.1.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).
