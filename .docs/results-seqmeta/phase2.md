# Phase 2: Provider extension and mock

Ref: [spec.md](spec.md) sections B1, B2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 2.1: B1 - Extended interface and adapter

spec.md section: B1

Extend the `seqmeta.SAGAProvider` interface with the nine new
methods listed under Architecture: the five `FindSamplesBy*`
methods, `StudyForSample`, and the three `ListProject*` helpers
(`ListProjectStudies`, `ListProjectSamples`, `ListProjectUsers`).
Update the production `saga.Client` adapter in
`seqmeta/client_adapter.go` so it satisfies the extended interface
by delegating to `saga.MLWHClient` or `saga.ProjectsClient` as
appropriate. Covers all 3 acceptance tests from B1.

- [ ] implemented
- [ ] reviewed

### Item 2.2: B2 - Mock provider fields

spec.md section: B2

Add matching `*Fn` fields (and safe zero-value behaviour) to
`seqmeta.MockProvider` for each new interface method. Depends on
item 2.1. Covers all 3 acceptance tests from B2.

- [ ] implemented
- [ ] reviewed
