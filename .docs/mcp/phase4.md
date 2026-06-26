# Phase 4: Queryer + Registry + handlers + RemoteClient

Ref: [spec.md](spec.md) sections A4, F3, D1, D2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

This phase wires the Phase 2 `*Client` methods into the declarative
surface: new `Queryer` members, `Registry` entries (search, counts,
freshness), the gin handler cases, the search `limit` guard, the plain
`/health` route, and the `RemoteClient` methods. Depends on Phase 2 (the
`Client` methods exist) and Phase 3 (snake_case is final). Items are
sequenced rather than parallelised because they all edit the shared
`mlwh/queryer.go`, `mlwh/registry.go`, `mlwh/server.go`, and
`mlwh/remote.go`. Item 4.1 establishes the `Queryer`/`Registry` contract
the handler and `RemoteClient` items build on.

## Items

### Item 4.1: Queryer members + Registry entries

spec.md sections: A4, F3, D2

Add the new `Queryer` members in `mlwh/queryer.go` (`SearchStudies`,
`SearchSamples`, `CountStudySearch`, `CountSampleSearch`, `CountStudies`,
`CountSamplesForStudy`, `Freshness`) and their `Registry` entries in
`mlwh/registry.go` per the spec's Registry table (paths, `PathParams`,
`Paginated`, `NewResult`; the `Summary`/`Description`/`QueryParams`
enrichment is Phase 5 C1). Keep `var _ Queryer = (*Client)(nil)` and
`var _ Queryer = (*RemoteClient)(nil)` compiling. Tests in
`mlwh/registry_test.go`. This item is the contract foundation for Items
4.2-4.4; it has no acceptance tests of its own (the route, RemoteClient,
and coverage assertions land with A4/F3/D2 and Phase 5 C2).

- [ ] implemented
- [ ] reviewed

### Item 4.2: A4 - search routes, pagination guard, and RemoteClient

spec.md section: A4

Add the `SearchStudies`/`SearchSamples` handler cases in `mlwh/server.go`
with a search-specific pagination reader (default `limit` 100, reject
`limit` > 1000 with the `bad_request` 400 envelope; the non-search
`mlwhServerFetchAllLimit` default is untouched), and the matching
`RemoteClient.SearchStudies`/`SearchSamples` in `mlwh/remote.go` (send
`limit`/`offset`, URL-escape the term path segment). Tests in
`mlwh/server_test.go` and `mlwh/remote_test.go`. Covers all 6 acceptance
tests from A4.

Depends on Item 4.1.

- [ ] implemented
- [ ] reviewed

### Item 4.3: F3 - count routes and RemoteClient

spec.md section: F3

Add the four count handler cases (`/studies/count`,
`/study/:id/samples/count`, `/search/study/:term/count`,
`/search/sample/:term/count`) in `mlwh/server.go` and the four
`RemoteClient` count methods (no pagination params) in `mlwh/remote.go`,
each returning `{count: N}` and mapping `ErrCacheNeverSynced` to 503
`cache_never_synced`. Tests in `mlwh/server_test.go` and
`mlwh/remote_test.go`. Covers all 5 acceptance tests from F3.

Depends on Item 4.1.

- [ ] implemented
- [ ] reviewed

### Item 4.4: D1 + D2 - /health route and freshness route/RemoteClient

spec.md sections: D1, D2

Add the plain `GET /health` route in `mlwh/server.go` returning
`{"status":"ok"}` with no cache read (registered in both the
unauthenticated and secured wiring). Add the `Freshness` handler case in
`mlwh/server.go` and `RemoteClient.Freshness` in `mlwh/remote.go`. Tests in
`mlwh/server_test.go` and `mlwh/freshness_test.go` (or `remote_test.go`).
Covers acceptance tests 1 and 2 from D1 (test 3, the OpenAPI presence of
`/health`, lands in Phase 5), and acceptance test 4 from D2 (the
`RemoteClient.Freshness` round-trip equalling the local result).

Depends on Item 4.1.

- [ ] implemented
- [ ] reviewed
