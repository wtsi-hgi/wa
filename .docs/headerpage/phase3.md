# Phase 3: Complete Bare-List Page Methods

Ref: [spec.md](spec.md) sections B1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor`
(`/home/ubuntu/.agents/skills/go-implementor/SKILL.md`) and
`go-reviewer` (`/home/ubuntu/.agents/skills/go-reviewer/SKILL.md`)
skills.

Bound shell commands that could hang with `timeout`. Do not run
long-lived servers. This phase may run in parallel with phase 2 after
phase 1 is reviewed.

## Items

### Item 3.1: B1 - Simple paged list wrappers

spec.md section: B1

Add the missing bare-list `Page[T]` wrappers in `mlwh/remote.go`:
`AllStudiesPage`, `SamplesForRunPage`, `SamplesForLibraryPage`,
`SamplesForLibraryIDPage`, `SamplesForLibraryLimsIDPage`,
`SamplesForLibraryTypePage`, `LibrariesForStudyPage`,
`RunsForStudyPage`, `LanesForSamplePage`, `SearchStudiesPage`, and
`SearchSamplesPage`. Each must call the same Registry method and path
params as its body-only counterpart, using `remotePagination` and
`remoteCallPage`. Add table-driven remote tests in
`mlwh/remote_test.go` for paths, query strings, header values,
missing-header fallback, parity with body-only calls, search-default
dynamic calls, search max-limit errors, and sentinels. Covers all 7
acceptance tests from B1.

- [ ] implemented
- [ ] reviewed
