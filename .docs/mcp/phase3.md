# Phase 3: JSON casing fix (Go side)

Ref: [spec.md](spec.md) sections E1, E2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

This is the Go half of the one coordinated casing break; the frontend Zod
half (E3) ships in Phase 7 within the same atomic change set. The phase is
independent of Phases 1/2, but it must precede Phase 5 (OpenAPI reflects
the post-Goal-4 snake_case JSON field names). E1 (add tags) lands before
E2 (re-prove parity over the new wire shape).

## Items

### Item 3.1: E1 - snake_case tags on Match and TaggedID

spec.md section: E1

Add JSON tags so every response body is consistently snake_case:
`Match` in `mlwh/mlwh.go` (`Kind->kind`, `Canonical->canonical`,
`Sample->sample` omitempty, `Study->study` omitempty, `Run->run`
omitempty, `Library->library` omitempty) and `TaggedID` in
`mlwh/types.go` (`Kind->kind`, `Canonical->canonical`). `RemoteClient`
decodes into the same structs and stays consistent automatically;
`cmd/mlwh_info.go` reads the Go fields and is unaffected. Tests in
`mlwh/server_test.go` and `mlwh/types_test.go`. Covers all 4 acceptance
tests from E1.

- [x] implemented
- [x] reviewed

### Item 3.2: E2 - parity preserved across the casing change

spec.md section: E2

Update/extend `mlwh/parity_test.go` so the `RemoteClient`<->`Client`
parity proof still holds after the casing change: assert
`reflect.DeepEqual(localResult, remoteResult)` for `ClassifyIdentifier`,
`ResolveStudy`, and `ExpandIdentifier` (snake_case round-trip is
lossless), and that the full parity table's asserted method count equals
the `Queryer` member count with all results matching. Covers both
acceptance tests from E2.

Depends on Item 3.1 (parity is the regression guard for the new tags).
Acceptance test 2 references the new search/count/freshness methods; if
they are not yet wired into `RemoteClient`/parity at this point, that
assertion is completed when Phase 4 extends the parity table.

- [x] implemented
- [x] reviewed
