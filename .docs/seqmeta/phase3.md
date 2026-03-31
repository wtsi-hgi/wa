# Phase 3: Diff Engine

Ref: [spec.md](spec.md) sections C1, C2, C3, C4, C5, C6, C7

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 3.1: C1 - First poll returns all added

spec.md section: C1

Implement generic `Diff[T]` function in `seqmeta/diff.go`. On
first poll (no prior entries in store), all current items are
returned as `Added`. Results are never nil slices. Covering all
2 acceptance tests from C1. Depends on Phase 1 (SAGAProvider
types) and Phase 2 (Store).

- [x] implemented
- [x] reviewed

### Item 3.2: C2 - Unchanged data returns empty diff

spec.md section: C2

Extend `Diff[T]` so that a poll with identical data returns empty
`Added`, `Modified`, and `Removed` slices. Uses SHA256 hash
comparison of JSON-marshalled entries. Covering the 1 acceptance
test from C2.

- [x] implemented
- [x] reviewed

### Item 3.3: C3 - Detect new, modified, and removed entries

spec.md section: C3

Extend `Diff[T]` to classify entries as added (new ID),
modified (same ID, different hash), or removed (ID missing from
current). Removed entries become tombstones. Re-appeared
tombstoned entries are reported as added. Covering all 2
acceptance tests from C3.

- [x] implemented
- [x] reviewed

### Item 3.4: C4 - Group hashing for shared IDs

spec.md section: C4

Extend `Diff[T]` so items sharing the same ID (from `idFunc`)
are grouped and hashed together. Items within a group are sorted
by JSON representation before hashing for determinism. Covering
all 3 acceptance tests from C4.

- [x] implemented
- [x] reviewed

### Item 3.5: C5 - Tombstone persistence

spec.md section: C5

Ensure removed entries become tombstones that are not re-reported
on subsequent polls. After removal, `LoadEntries` shows
`Tombstone == true`. Covering all 2 acceptance tests from C5.

- [x] implemented
- [x] reviewed

### Batch 2 (parallel, after item 3.5 is reviewed)

#### Item 3.6: C6 - DiffStudySamples convenience wrapper [parallel with 3.7]

spec.md section: C6

Implement `DiffStudySamples` in `seqmeta/diff.go`. Fetches
samples via `SAGAProvider.AllSamplesForStudy`, uses query key
`"study_samples:<studyID>"` and `idFunc` returning
`MLWHSample.SangerID`. Atomic failure: saga errors prevent store
update. Covering all 3 acceptance tests from C6.

- [x] implemented
- [x] reviewed

#### Item 3.7: C7 - DiffSampleFiles convenience wrapper [parallel with 3.6]

spec.md section: C7

Implement `DiffSampleFiles` in `seqmeta/diff.go`. Fetches files
via `SAGAProvider.GetSampleFiles`, uses query key
`"sample_files:<sangerID>"` and `idFunc` returning
`IRODSFile.Collection`. Atomic failure on saga errors. Covering
all 3 acceptance tests from C7.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
