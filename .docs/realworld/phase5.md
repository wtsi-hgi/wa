# Phase 5: Wiring + docs (G1-G2)

Ref: [spec.md](spec.md) sections G1, G2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both (the four-step add-a-query recipe, verbatim-for-MCP
Descriptions, and the no-drift guards).

Per the spec, the Registry/handler/RemoteClient wiring for each endpoint
is done incrementally within Phases 2-4. This phase verifies that wiring
is complete and consistent across ALL new endpoints (G1) and performs the
final documentation regeneration and glossary update (G2). Do NOT run a
live warehouse; the relevant tests are hermetic
(`registry_test.go`, `server_test.go`, `remote_test.go`, `docs_test.go`)
and the doc regeneration is a guarded `go test` run.

## Items

### Item 5.1: G1 - Registry entries + handler cases + remote methods (verify + complete)

spec.md section: G1

For each new endpoint added across Phases 2-4, confirm the full four-step
wiring exists and is consistent: the `Queryer` member (`mlwh/queryer.go`),
the `Client` method, the `RemoteClient` method (and `Page[T]` variant for
new paginated lists) (`mlwh/remote.go`), the `Registry` `Endpoint` with a
non-empty Summary and a verbatim-for-MCP Description
(`mlwh/registry.go`), and the `server.go` handler `case`. Fill any gaps.
Each Description must state, as applicable: the precise "available"
definition, recency/window semantics + parameters, the study-scoping
rule, platform-qualification (and ONT "not tracked"), the QC string
mapping + authoritative field + roll-up, the run-id space, and the
freshness caveat. Test in `mlwh/registry_test.go`, `mlwh/server_test.go`,
`mlwh/remote_test.go`. Covering all 4 acceptance tests from G1 (every new
Method has non-empty Summary/Description and paginated entries declare
limit/offset QueryParams; handler returns the same value as the `Client`
method for every new Method with no panic; `RemoteClient` round-trips to
the same typed result; every windowed/recency Description states the
filter is on the iRODS creation timestamp, never `last_updated`/
`last_run`). Depends on Phases 2-4 (all endpoints implemented).

- [ ] implemented
- [ ] reviewed

### Item 5.2: G2 - regenerate docs; drift guards green; glossary updated

spec.md section: G2

Run `WA_REFRESH_DOCS=1 go test ./mlwh -run TestWriteEndpointReference` to
rewrite `.docs/mcp/api-reference.md`. Add glossary entries
(`.docs/mcp/glossary.md`) for "sequencing data available", "added to
iRODS", "baseline phase", "detailed timeline", and "platform". Test in
`mlwh/docs_test.go`. Covering all 3 acceptance tests from G2
(`TestEndpointReferenceAndOpenAPICoverSamePathsG1` shows reference and
OpenAPI cover the same Registry paths; the committed reference matches
`EndpointReference()` after regeneration; the glossary defines
"sequencing data available" and "added to iRODS"). Depends on 5.1 (the
Registry must be complete before regenerating the reference).

- [ ] implemented
- [ ] reviewed

## Ordering and dependency notes

- This is the final phase; it depends on Phases 2, 3, and 4 being fully
  reviewed (all new endpoints implemented and incrementally wired).
- Items are sequential: 5.1 verifies/completes the Registry and wiring
  surface, then 5.2 regenerates the docs from the finalized Registry and
  confirms the drift guards pass. Regenerating docs before the Registry
  is complete would produce drift.
- Because most wiring was done in Phases 2-4, 5.1 is primarily a
  completeness/consistency pass plus the cross-cutting assertions
  (Summary/Description present, round-trip parity, recency-timestamp
  wording); reviewers should still exercise every new Method's handler
  and remote round-trip.
