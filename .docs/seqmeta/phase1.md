# Phase 1: SAGAProvider Interface

Ref: [spec.md](spec.md) sections A1, A2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 1.1: A1 - Interface definition

spec.md section: A1

Define `SAGAProvider` interface in `seqmeta/provider.go` with 6
methods (`GetStudy`, `AllStudies`, `AllSamples`,
`AllSamplesForStudy`, `GetSampleFiles`, `ListProjects`). Create
`MockProvider` test helper. Covering all 2 acceptance tests from
A1.

- [x] implemented
- [x] reviewed

### Item 1.2: A2 - ClientAdapter

spec.md section: A2

Implement `ClientAdapter` struct wrapping `saga.Client` to satisfy
`SAGAProvider` in `seqmeta/provider.go`. `NewClientAdapter`
constructor, each method delegates to the corresponding saga
sub-client. Covering all 5 acceptance tests from A2, including
interface satisfaction compile check.

- [x] implemented
- [x] reviewed
