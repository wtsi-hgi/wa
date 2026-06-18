# Phase 2: Queryer + registry + server + remote

Ref: [spec.md](spec.md) sections B1, B2, B3, B4, F1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

This phase defines `mlwh.Queryer` (depends on the Phase 1 method set),
the declarative registry, the gin server, the `RemoteClient`, and the
parity proof. F1 (docs/parity guard) lands alongside B1.

## Items

### Batch 1 (parallel)

#### Item 2.1: B1 - declarative registry [parallel with F1]

spec.md section: B1

Define `mlwh.Queryer` in `mlwh/queryer.go` (33 members, full read/query
surface) and the declarative `Registry` (`[]Endpoint`) in
`mlwh/registry.go` mapping each method 1:1 to a GET endpoint
(`Method`/`Verb`/`Path`/`PathParams`/`Query`/`Paginated`/`NewResult`).
Tests in `mlwh/registry_test.go`. Covering all 5 acceptance tests from
B1 (including `TestRegistryCoversQueryer` enforcing 33-entry/method-set
parity via reflection).

- [x] implemented
- [x] reviewed

#### Item 2.2: F1 - add-a-query checklist [parallel with B1]

spec.md section: F1

Add an "Add a new MLWH query" section to `DEVELOPING.md` listing the 4
steps (schema+index in both dialects -> `Client` method -> `Queryer`
member -> `Registry` entry) and add a package/var doc comment to
`mlwh/registry.go` stating the registry is the single source from which
the handler and `RemoteClient` derive. Tests reuse
`mlwh/registry_test.go` and the existing `mlwh/cache_schema_test.go`
parity test. Covering all 4 acceptance tests from F1.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

### Batch 2 (parallel, after batch 1 is reviewed)

#### Item 2.3: B2 - gin handler builds from the registry [parallel with B3]

spec.md section: B2

Implement `mlwh/server.go`: `NewServer(q Queryer, opts...) *Server` and
`(*Server).RegisterRoutes(router *gin.Engine, auth *gin.RouterGroup)`.
For each `Registry` entry, register a per-entry closure handler that
extracts unescaped path params and `limit`/`offset`+filters, calls the
`Queryer` method, and writes JSON 200 or the error envelope
(`{code, message}`). `limit`/`offset` default to the fetch-all limit and 0. Handlers hold no cache of their own (the only closed-over state is the
`Queryer`). Also implement the sentinel <-> status/code mapping in
`mlwh/errors_http.go`. Tests in `mlwh/server_test.go`. Covering all 8
acceptance tests from B2.

- [x] implemented
- [x] reviewed

#### Item 2.4: B3 - RemoteClient round-trips Queryer [parallel with B2]

spec.md section: B3

Implement `mlwh/remote.go`: `RemoteClient`/`RemoteConfig`,
`NewRemoteClient`, `Close`, and a `Queryer` implementation where each
method looks up its `Registry` entry, builds the escaped path + query
string, issues GET, and decodes `entry.NewResult()` on 2xx or maps the
error envelope on non-2xx (reconstructing sentinels via `code`, joining
`ErrCacheNeverSynced` with `ErrNotFound` on list endpoints, optional
Bearer token / CA cert). Shares the `errors_http.go` mapping with B2.
Tests in `mlwh/remote_test.go`. Covering all 9 acceptance tests from B3.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

### Item 2.5: B4 - RemoteClient <-> Client parity

spec.md section: B4

Add a parity harness in `mlwh/parity_test.go` that opens a real SQLite
cache via `OpenCacheOnly`, seeds it, constructs a `Client` and a
`RemoteClient` pointed at an `httptest.Server` wrapping the same
`Client`, and asserts equal results across all 33 `Queryer` methods plus
sentinel round-tripping. Covering all 4 acceptance tests from B4.

Depends on Batch 2 (B2 and B3 reviewed).

- [x] implemented
- [x] reviewed
