# Phase 5: REST API

Ref: [spec.md](spec.md) sections E1, E2, E3, E4, E5, E6

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 5.1: E1 - GET /enrich/{identifier} happy path

spec.md section: E1

Wire `GET /enrich/{identifier}` into `seqmeta.Server`, returning
the `EnrichmentGraph` JSON with `omitempty` pointer/slice fields.
Establishes the route, handler, and response shape used by E2-E6.
Covers all 2 acceptance tests from E1.

- [x] implemented
- [x] reviewed

### Batch 2 (parallel, after batch 1 is reviewed)

#### Item 5.2: E2 - GET /enrich partial and 502 [parallel with 5.3, 5.4, 5.5, 5.6]

spec.md section: E2

Implement partial responses (non-classification hop failures become
`MissingHop`) and the all-hops-5xx -> 502 case at the handler
layer. Covers all 3 acceptance tests from E2.

- [x] implemented
- [x] reviewed

#### Item 5.3: E3 - Cache hits and TTL via functional option [parallel with 5.2, 5.4, 5.5, 5.6]

spec.md section: E3

Integrate `LoadEnrichCache`/`SaveEnrichCache` with the handler and
expose a `WithEnrichTTL` functional option on the server (no env
reads). Covers all 4 acceptance tests from E3.

- [x] implemented
- [x] reviewed

#### Item 5.4: E4 - DELETE /enrich/{identifier} [parallel with 5.2, 5.3, 5.5, 5.6]

spec.md section: E4

Implement `DELETE /enrich/{identifier}` invoking
`InvalidateEnrichFor`. Covers all 3 acceptance tests from E4.

- [x] implemented
- [x] reviewed

#### Item 5.5: E5 - Diff invalidation integration [parallel with 5.2, 5.3, 5.4, 5.6]

spec.md section: E5

End-to-end test that `DiffStudySamples`/`DiffSampleFiles`
mutations invalidate cached enrichment entries through the HTTP
server. Covers all 3 acceptance tests from E5.

- [x] implemented
- [x] reviewed

#### Item 5.6: E6 - /validate preservation [parallel with 5.2, 5.3, 5.4, 5.5]

spec.md section: E6

Verify `/validate` endpoint behaviour is unchanged:
`IdentifierResult.Object` is still a single matched object and no
graph shape leaks in. Covers all 2 acceptance tests from E6.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).
