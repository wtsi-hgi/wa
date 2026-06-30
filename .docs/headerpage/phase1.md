# Phase 1: Response Envelope Types and JSON Casing Tests

Ref: [spec.md](spec.md) sections D1, D2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor`
(`/home/ubuntu/.agents/skills/go-implementor/SKILL.md`) and
`go-reviewer` (`/home/ubuntu/.agents/skills/go-reviewer/SKILL.md`)
skills.

Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

This is the type/test foundation for the remote methods in phase 5.

## Items

### Item 1.1: D1 - Study manifest page envelope types

spec.md section: D1

Add `PagedStudyManifest` to `mlwh/types.go` and JSON casing coverage in
`mlwh/types_test.go` for `study_manifest`, `total`, and `next_offset`.
Do not add the remote method yet. The full D1 story has 6 acceptance
tests; this item covers the JSON-casing contract from D1 acceptance
test 6.

- [x] implemented
- [x] reviewed

### Item 1.2: D2 - Study and run detail envelope types

spec.md section: D2

Add `PagedStudyDetail`, `PagedRunDetail`, and `DetailOptions` to
`mlwh/types.go`, with compile-time checks and JSON marshal tests in
`mlwh/types_test.go`. Keep `DetailOptions` as a plain Go input type
with no JSON contract. The full D2 story has 7 acceptance tests; this
item covers the type and JSON-casing contract from D2 acceptance test 7.

- [x] implemented
- [x] reviewed
