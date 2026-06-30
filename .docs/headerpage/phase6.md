# Phase 6: Exported Comments and Focused Tests

Ref: [spec.md](spec.md) section E, item 6

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor`
(`/home/ubuntu/.agents/skills/go-implementor/SKILL.md`) and
`go-reviewer` (`/home/ubuntu/.agents/skills/go-reviewer/SKILL.md`)
skills.

Bound shell commands that could hang with `timeout`. Do not run
long-lived servers. This phase follows phases 1 through 5.

## Items

### Item 6.1: E item 6 - Exported comments and focused tests

spec.md section: E, item 6

Update exported Go comments in `mlwh/remote.go` and result-type comments
in `mlwh/types.go` for the new header-aware API. Do not change
generated HTTP API docs unless a no-drift test requires it. Run the
focused test command from the repository root with a timeout:

```bash
timeout 120s env CGO_ENABLED=1 go test -tags netgo --count 1 ./mlwh -v \
  -run 'Test(RemoteClient|Page|StudyManifest|.*Detail)'
```

This item verifies the completed A1, B1, C1, C2, C3, D1, and D2 work and
covers the final Implementation Order check; it has no standalone story
acceptance tests.

- [x] implemented
- [x] reviewed
