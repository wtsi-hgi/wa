# Phase 2: Dynamic Header Access

Ref: [spec.md](spec.md) sections A1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor`
(`/home/ubuntu/.agents/skills/go-implementor/SKILL.md`) and
`go-reviewer` (`/home/ubuntu/.agents/skills/go-reviewer/SKILL.md`)
skills.

Bound shell commands that could hang with `timeout`. Do not run
long-lived servers. This phase may run in parallel with phase 3 after
phase 1 is reviewed.

## Items

### Item 2.1: A1 - Dynamic call with headers

spec.md section: A1

Implement `(*RemoteClient).CallWithHeaders` in `mlwh/remote.go` using
the same `rc.do` path as `Call`, and update `Call` to wrap it and
discard headers. Preserve existing registry lookup, path-param arity,
`decodeRemoteError`, and sentinel behavior. Add tests in
`mlwh/remote_test.go` covering all 3 acceptance tests from A1.

- [ ] implemented
- [ ] reviewed
