# Phase 7: frontend casing + posture docs + reachability

Ref: [spec.md](spec.md) sections E3, G2, G3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills for the Go
and documentation items.

IMPORTANT - mixed stack: Item 7.1 (E3) changes the Next.js frontend
(`frontend/lib/contracts.ts` and its vitest tests), NOT Go. For Item 7.1
the orchestrator MUST coordinate subagents with the
`nextjs-fastapi-implementor` and `nextjs-fastapi-reviewer` skills (not
`go-implementor`/`go-reviewer`). Items 7.2 (G2) and 7.3 (G3) use
`go-implementor`/`go-reviewer` as normal (G2 is documentation, G3 is Go
tests). Wire the reviewer per item accordingly.

E3 is the frontend half of the one coordinated casing break and depends on
E1 (Phase 3) shipping in the same overall change set. G3 depends on
Phase 4 (all new endpoints are registered). The three items touch disjoint
files and proceed in parallel.

## Items

### Batch 1 (parallel)

#### Item 7.1: E3 - frontend schema to snake_case [parallel with 7.2, 7.3]

spec.md section: E3

Stack: Next.js frontend - use `nextjs-fastapi-implementor` /
`nextjs-fastapi-reviewer`.

Rewrite `mlwhMatchSchema` in `frontend/lib/contracts.ts` to keys `kind`,
`canonical`, `sample`, `study`, `run`, `library` (dropping the PascalCase
shape entirely); `mlwhMatchObject` reads `match.sample ?? match.study ??
match.run ?? match.library`; the `identifierResultSchema` union maps
`{canonical->identifier, kind->type, object}`. Update the dependent test
fixtures to emit snake_case (`frontend/tests/contracts.test.ts`,
`frontend/tests/actions.test.ts`, `frontend/tests/seqmeta-stub.test.ts`,
`frontend/tests/integration/setup.ts`). Audit confirms no other raw-JSON
consumer of `/classify`, `/resolve/*`, or `/expand/:kind/:id` beyond
`validateIdentifier` and the Go `RemoteClient`. Frontend testing
conventions: nextjs-fastapi-conventions; vitest `expect()`. Covers all 4
acceptance tests from E3.

- [x] implemented
- [x] reviewed

#### Item 7.2: G2 - security-posture documentation [parallel with 7.1, 7.3]

spec.md section: G2

Stack: documentation (Go repo) - use `go-implementor` / `go-reviewer`.

Document in `.docs/mcp/` (and/or `DEVELOPING.md`): the API is
unauthenticated plain HTTP by default; it exposes all mirrored MLWH
metadata including the governance study fields (`data_access_group`,
`study_visibility`, `contains_human_dna`, `ega_dac_accession_number`,
`ega_policy_accession_number`) with no per-user authorisation, so the MCP
server / network boundary is the access-control boundary; the search
surface returns the same full rows; the data-freshness model (Goal 3); and
the known limitation that secured gas mode moves endpoints under
`/rest/v1/auth` and the current `RemoteClient` does not add that prefix or
perform JWT login (documented, not fixed here). No test file (acceptance
checks read the document). Covers all 3 acceptance tests from G2.

- [x] implemented
- [x] reviewed

#### Item 7.3: G3 - unauthenticated reachability [parallel with 7.1, 7.2]

spec.md section: G3

Stack: Go - use `go-implementor` / `go-reviewer`.

Confirm the unauthenticated wiring (`server.RegisterRoutes(
authServer.Router(), nil)` in `cmd/mlwh.go`) registers all new `Registry`
endpoints plus `/health` and `/openapi.json` on the public router (no
auth/TLS added). Tests in `cmd/mlwh_test.go` and `mlwh/server_test.go`:
unauthenticated 200 for `GET /search/study/malar`, `/studies/count`,
`/freshness`, `/health`, `/openapi.json`; secured mode returns 401 without
a Bearer token; `/freshness` against a never-synced cache returns 200 (not
503). Covers all 3 acceptance tests from G3.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item. Launch review
subagents using the reviewer skill matching each item's stack:
`nextjs-fastapi-reviewer` for Item 7.1, `go-reviewer` for Items 7.2 and
7.3.
