# Phase 5: REST API

Ref: [spec.md](spec.md) sections E1, E2, E3, E4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Batch 1 (parallel)

#### Item 5.1: E1 - Study diff endpoint [parallel with 5.2, 5.3]

spec.md section: E1

Implement `NewServer`, `Server.Handler`, and chi route
`GET /diff/study/{id}` in `seqmeta/server.go`. Returns
`DiffResult[saga.MLWHSample]` as JSON. Status 404 on upstream not
found and 502 on other saga errors, with `{"error":"<msg>"}`
body. Depends on Phase 3
(DiffStudySamples). Covering all 4 acceptance tests from E1.

- [x] implemented
- [x] reviewed

#### Item 5.2: E2 - Sample diff endpoint [parallel with 5.1, 5.3]

spec.md section: E2

Add chi route `GET /diff/sample/{id}` in `seqmeta/server.go`.
Returns `DiffResult[saga.IRODSFile]` as JSON. Depends on Phase 3
(DiffSampleFiles). Upstream not found returns 404. Covering all 3
acceptance tests from E2.

- [x] implemented
- [x] reviewed

#### Item 5.3: E3 - Validate endpoint [parallel with 5.1, 5.2]

spec.md section: E3

Add chi route `GET /validate/{identifier}` in
`seqmeta/server.go`. Returns `IdentifierResult` as JSON. Status
404 for unknown identifiers. Use whatever chi-compatible route
pattern is needed internally to preserve URL-encoded special
characters, including encoded slashes. Depends on Phase 4
(Validate). Covering all 3 acceptance tests from E3.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).

### Item 5.4: E4 - Error responses

spec.md section: E4

Ensure consistent JSON error bodies across all endpoints:
`{"error":"<message>"}` with `Content-Type: application/json`.
Status 404 for upstream not found, 502 for other saga provider
failures, and 500 for store failures.
Covering all 2 acceptance tests from E4. Depends on items
5.1-5.3.

- [x] implemented
- [x] reviewed
