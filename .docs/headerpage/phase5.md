# Phase 5: Manifest and Detail Header Envelopes

Ref: [spec.md](spec.md) sections D1, D2

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

### Item 5.1: D1 - Study manifest page envelope

spec.md section: D1

Add the shared private helper over `rc.do` in `mlwh/remote.go` for
non-array result envelopes: decode `*T`, read `X-Total-Count` and
`X-Next-Offset` with `remoteHeaderInt`, and return the typed value plus
totals. Use the `PagedStudyManifest` type and JSON casing coverage from
phase 1. Implement `StudyManifestPage` using Registry method
`StudyManifest` and `remoteManifestQuery`, preserving the existing
body-only `StudyManifest` method. Add tests in `mlwh/manifest_test.go`
for query strings, omitted optional params, missing headers, errors, and
body-only behavior. Covers D1 acceptance tests 1 through 5; phase 1
covers acceptance test 6.

- [x] implemented
- [x] reviewed

### Item 5.2: D2 - Study and run detail options with headers

spec.md section: D2

Add `remoteDetailQuery(DetailOptions)` in `mlwh/remote.go`, always
sending literal `limit` and `offset` and sending `lean=true` only when
requested. Implement `StudyDetailWithOptions` and
`RunDetailWithOptions` with Registry methods `StudyDetail` and
`RunDetail`, reusing the non-array envelope helper from item 5.1. Use
the `PagedStudyDetail`, `PagedRunDetail`, `DetailOptions`, and JSON
casing coverage from phase 1. Preserve existing body-only
`StudyDetail(ctx, id)` and `RunDetail(ctx, id)` with no query params.
Add tests in `mlwh/remote_test.go` for lean and non-lean queries,
no-header fallbacks, unchanged body-only URIs, and errors. Do not add a
sample detail options method. Covers D2 acceptance tests 1 through 6;
phase 1 covers acceptance test 7. Depends on item 5.1.

- [x] implemented
- [x] reviewed
