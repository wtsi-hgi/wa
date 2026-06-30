# Phase 4: Filtered Page Methods

Ref: [spec.md](spec.md) sections C1, C2, C3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor`
(`/home/ubuntu/.agents/skills/go-implementor/SKILL.md`) and
`go-reviewer` (`/home/ubuntu/.agents/skills/go-reviewer/SKILL.md`)
skills.

Bound shell commands that could hang with `timeout`. Do not run
long-lived servers. This phase starts after phases 2 and 3 have
established the shared header/page helper behavior.

## Items

### Batch 1 (parallel)

#### Item 4.1: C1 - Windowed samples-with-data page [parallel with 4.2, 4.3]

spec.md section: C1

Add `SamplesWithDataSincePage` in `mlwh/remote.go`, using Registry
method `SamplesWithData` and
`remotePaginationWithAddedWindow(limit, offset, since, until)`. Add
tests in `mlwh/remote_test.go` for populated windows, empty
`since`/`until`, header metadata, and upstream bad-request handling.
Covers all 3 acceptance tests from C1.

- [x] implemented
- [x] reviewed

#### Item 4.2: C2 - iRODS file-type page variants [parallel with 4.1, 4.3]

spec.md section: C2

Add `IRODSPathsForSamplePage`,
`IRODSPathsForSampleByFileTypePage`,
`IRODSPathsForStudyByFileTypePage`, and
`IRODSPathsForRunByFileTypePage` in `mlwh/remote.go`. Use the same
Registry methods as the body-only calls and
`remotePaginationWithFileType` for filtered variants. Add remote tests
for sample, study, and run URIs, empty `fileType` omission and parity,
header values, and invalid `file_type` errors. Covers all 4 acceptance
tests from C2.

- [x] implemented
- [x] reviewed

#### Item 4.3: C3 - Existing filtered page coverage [parallel with 4.1, 4.2]

spec.md section: C3

Keep the existing filtered page methods unchanged in `mlwh/remote.go`
and add coverage in `mlwh/remote_test.go` for
`StudiesForFacultySponsorPage`, `StudiesForUserPage`, and
`ResolvePersonPage`, including role query strings, parity fixtures,
header values, and remote `bad_request` / `cache_never_synced`
sentinels. Covers all 5 acceptance tests from C3.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).
