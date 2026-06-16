# Phase 4: repoint results

Ref: [spec.md](spec.md) sections A1, A2, E2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

Depends on Phases 1-3 (needs `mlwh.Queryer`, `RemoteClient`, and the
server). A1 and A2 are independent `results`-package changes and may run
in parallel; E2 wires them together and depends on both.

## Items

### Batch 1 (parallel)

#### Item 4.1: A1 - results validator calls mlwh directly [parallel with A2]

spec.md section: A1

Replace `SeqmetaValidator` (HTTP client to `/validate/*`) with
`MLWHValidator` holding an `mlwh.Queryer` and calling
`ClassifyIdentifier`. Keep `ValidateMetadataValues`'s signature and the
`seqmeta_*` metadata-key scanning (prefix NOT renamed). Rename
`ErrSeqmetaRejected` -> `ErrMLWHRejected`; map `ErrNotFound` -> rejected
and `ErrUpstreamImpaired`/`ErrCacheNeverSynced` -> failed
(`ErrMLWHFailed`). Replace `NewSeqmetaValidator` with
`NewMLWHValidator(q mlwh.Queryer)`. Files `results/validate.go`,
`results/validate_test.go`. Covering all 6 acceptance tests from A1.

- [ ] implemented
- [ ] reviewed

#### Item 4.2: A2 - sample resolver calls mlwh directly [parallel with A1]

spec.md section: A2

Delete `SeqmetaSampleResolver` and its `/study/{id}/samples` + `/enrich/*`
HTTP calls; make `MLWHSearchResolver` the sole `SearchResolver`. Keep
`Expand` returning `([]string, []string, []string, error)` to its
callers but internally call `ExpandSearchValues` (returning
`SearchValues`) and read `.Samples/.Runs/.Lanes`. Route study-/library-
sample resolution through `SamplesForStudy`/`SamplesForLibrary*`/
`LanesForSample`. Files `results/server.go`,
`results/mlwh_search_resolver.go` and their tests. Covering all 4
acceptance tests from A2.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

### Item 4.3: E2 - results serve wiring selects local or remote mlwh

spec.md section: E2

Split `resultsServeSyncClient` so sync stays local-only and the query
surface is `mlwh.Queryer`. When `WA_MLWH_SERVER_URL`/`--mlwh-server-url`
is set, build a `RemoteClient` (no `--mlwh-cache`, no `Sync`); else
`OpenCacheOnly` a local `Client`. Pass `NewMLWHValidator(q)` and
`NewMLWHSearchResolver(q)` into `NewServer`. Remove
`--seqmeta-url`/`--seqmeta-timeout` flags and the
`WA_SEQMETA_BACKEND_URL` default. File `cmd/results.go`, tests in
`cmd/results_serve_test.go`. Covering all 5 acceptance tests from E2.

Depends on Items 4.1 and 4.2.

- [ ] implemented
- [ ] reviewed
