# Phase 8: Frontend UI and E2E

Ref: [spec.md](spec.md) sections H3, H4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `nextjs-fastapi-implementor` and
`nextjs-fastapi-reviewer` skills.

## Items

### Item 8.1: H3 - Enrichment state and badge

spec.md section: H3

Render enrichment results in the results UI: add the enrichment
badge, study/library/sample panels, and
loading/partial/missing/404/502 states driven by the H2 action.
Covers all 5 acceptance tests from H3.

- [x] implemented
- [x] reviewed

### Item 8.2: H4 - Playwright coverage

spec.md section: H4

Add Playwright E2E coverage in `frontend/e2e/` exercising the
enrichment happy path and at least one partial/missing-hop
scenario. Depends on item 8.1. Covers all 2 acceptance tests from
H4.

- [x] implemented
- [x] reviewed
