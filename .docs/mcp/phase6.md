# Phase 6: startup backend refusal

Ref: [spec.md](spec.md) sections H1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

Depends on Phase 1 (the search index exists) and Phase 4 (the `serve`
wiring). Independent of Phase 5.

## Items

### Item 6.1: H1 - refuse MariaDB / MySQL < 8

spec.md section: H1

After `mlwh.OpenCacheOnly` and before wiring routes, `wa mlwh serve`
inspects the cache backend via a small exported helper on `*Client` (e.g.
`SupportsFullTextSearch(ctx) (bool, error)` or a backend-flavor accessor)
in `mlwh/cache.go` (or `mlwh/search.go`), reusing the existing `VERSION()`
logic (`mySQLServerVersion`/`mySQLMajorVersion`, `mlwh/cache.go:1378-1398`).
For a MySQL cache whose version string contains `mariadb`
(case-insensitive) or whose major version is < 8, `cmd/mlwh.go` exits
non-zero with an error like `wa mlwh serve requires SQLite or MySQL >= 8
for full-text search` and never calls `server.RegisterRoutes` / never
binds a listener; SQLite and MySQL >= 8 proceed. Tests in
`mlwh/cache_test.go` (or `mlwh/search_test.go`) and `cmd/mlwh_test.go`.
Covers all 5 acceptance tests from H1.

- [x] implemented
- [x] reviewed
