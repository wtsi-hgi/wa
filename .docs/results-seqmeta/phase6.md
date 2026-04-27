# Phase 6: Early-probe and integration matrix

Ref: [spec.md](spec.md) sections F1, G1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

Both items are gated on `SAGA_TEST_API_TOKEN`; tests must skip
cleanly when the token is absent.

## Items

### Item 6.1: F1 - MLWH filter probe

spec.md section: F1

Add an early integration probe that, when
`SAGA_TEST_API_TOKEN` is set, hits the real MLWH `find_samples`
endpoint for each of the five supported filter keys and asserts
the response shape. Covers all 6 acceptance tests from F1.

- [x] implemented
- [x] reviewed

### Item 6.2: G1 - Real-API enrichment matrix

spec.md section: G1

Add a real-API integration test that drives `/enrich/{identifier}`
through each classification hop against live MLWH/iRODS with
`SAGA_TEST_API_TOKEN`. Depends on item 6.1. Covers all 3
acceptance tests from G1.

- [x] implemented
- [x] reviewed
