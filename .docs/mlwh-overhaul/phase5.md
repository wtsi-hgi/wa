# Phase 5: rename + narrow mlwhdiff

Ref: [spec.md](spec.md) sections D1, D2, D3, E3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

Depends on Phase 2 (`mlwh.Queryer`). Independent of the frontend
(Phase 6). The rename (D1) lands first; the narrowing (D2), server
migration (D3), and CLI wiring (E3) build on the renamed package.

## Items

### Item 5.1: D1 - package, CLI, store, env rename

spec.md section: D1

Rename package `seqmeta` -> `mlwhdiff`, directory `seqmeta/` ->
`mlwhdiff/`, `wa seqmeta` -> `wa mlwhdiff` (`cmd/seqmeta.go` ->
`cmd/mlwhdiff.go`), default store file `seqmeta.db` -> `mlwhdiff.db`,
sentinel/text `seqmeta:` -> `mlwhdiff:`. Drop `WA_SEQMETA_BACKEND_URL`.
Subcommands reduce to `diff` and `serve` (`validate` removed). Do NOT
rename the persisted `seqmeta_*` metadata-key prefix in `results`/
`frontend`. Rename existing tests. Covering all 5 acceptance tests from
D1.

- [ ] implemented
- [ ] reviewed

### Item 5.2: D2 - narrowed provider and deleted current-state code

spec.md section: D2

Replace `mlwhdiff/provider.go` with a dependency on `mlwh.Queryer`
(alias `DiffSource`); reduce or delete `client_adapter.go`. Delete
`enrich.go`, `validate.go`, the enrich types in `types.go`, the
`enrich_cache` store code (`WithEnrichTTL`/`SaveEnrichCache`/
`DeleteEnrichCache`/`LoadEnrichCache`/`invalidateEnrichFor`), and the
current-state handlers. Keep `diff.go`'s algorithm and `store.go`'s
watermark/tombstone shape unchanged; diff uses only `AllStudies`,
`SamplesForStudy`, `IRODSPathsForSample`. Files `mlwhdiff/provider.go`,
`mlwhdiff/diff.go`, `mlwhdiff/store.go` and tests. Covering all 6
acceptance tests from D2.

Depends on Item 5.1.

- [ ] implemented
- [ ] reviewed

### Item 5.3: D3 - mlwhdiff server on gin, diff routes only

spec.md section: D3

Migrate `mlwhdiff/server.go` from chi to gin, exposing only
`GET /diff/study/:id` and `GET /diff/sample/:id`. The current-state
handlers and the `DELETE /enrich/*` route are gone (return 404). File
`mlwhdiff/server.go`, tests in `mlwhdiff/server_test.go`. Covering all 4
acceptance tests from D3.

Depends on Item 5.2.

- [ ] implemented
- [ ] reviewed

### Item 5.4: E3 - mlwhdiff serve and CLI wiring selects local or remote mlwh

spec.md section: E3

Update `cmd/mlwhdiff.go` to build an `mlwh.Queryer` the same way as E2
(local cache-only `Client` or `RemoteClient` by `WA_MLWH_SERVER_URL`) and
pass it as the `DiffSource`; `--mlwh-cache` required only in local mode.
Tests in `cmd/mlwhdiff_test.go`. Covering all 3 acceptance tests from E3.

Depends on Items 5.1-5.3.

- [ ] implemented
- [ ] reviewed
