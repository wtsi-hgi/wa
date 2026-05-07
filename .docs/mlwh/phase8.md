# Phase 8: Search Regressions

Ref: [spec.md](spec.md) sections G1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 8.1: G1 - Study search expands via mlwh

spec.md section: G1

Wire `results/server.go` search expansion onto
`mlwh.ExpandIdentifier` so `study=`, `library=`, `sample=`, and
`run=` filters fan out via the resolver-expanded tag set with an `OR`
group. Verify cache-backed lookups (at most one
`mlwh.ExpandIdentifier` invocation within 5 minutes) and library
search latency under 1 second against the in-memory fixture in
`results/server_test.go`. Covers all 4 acceptance tests from G1.
Live-MLWH integration tests (`mlwh/integration_test.go`,
`seqmeta/integration_test.go`) run only when `WA_MLWH_DSN` is set.

- [x] implemented
- [x] reviewed
