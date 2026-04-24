# Phase 7: Frontend contracts and action

Ref: [spec.md](spec.md) sections H1, H2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `nextjs-fastapi-implementor` and
`nextjs-fastapi-reviewer` skills.

## Items

### Item 7.1: H1 - Schemas

spec.md section: H1

Add zod schemas for `EnrichmentGraph`, `MissingHop`, and the
related study/library/sample shapes to
`frontend/lib/contracts.ts`, matching the Go JSON output exactly
(including `omitempty` pointer/slice fields). Covers all 3
acceptance tests from H1.

- [ ] implemented
- [ ] reviewed

### Item 7.2: H2 - enrichIdentifier action

spec.md section: H2

Implement an `enrichIdentifier` server action in
`frontend/app/(results)/actions.ts` that calls
`GET /enrich/{identifier}` via `seqmetaJson` with the H1 schema,
and surfaces partial / 404 / 502 distinctly. Depends on item 7.1.
Covers all 3 acceptance tests from H2.

- [ ] implemented
- [ ] reviewed
