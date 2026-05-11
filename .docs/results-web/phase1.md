# Phase 1: Go foundation

Ref: [spec.md](spec.md) sections A1, B1, C1

## Instructions

Use the `orchestrator` skill to complete this phase,
coordinating subagents with the `go-implementor` and
`go-reviewer` skills.

## Items

### Item 1.1: A1 - Serve registered file content

spec.md section: A1

Implement `GET /results/{id}/file?path=<abs-path>` handler
in new file `results/server_file.go` with test file
`results/server_file_test.go`. Introduce `ServerOption`
function type, `WithMaxPreviewBytes` option, and update
`NewServer` to accept variadic `...ServerOption`. Add
`DefaultMaxPreviewBytes` constant (10 MB). Add
`ErrFileGone` and `ErrFileTooLarge` sentinel errors to
`results/types.go`. Handler logic: lookup result set by ID,
verify path is in registered file list, stat the file,
detect Content-Type via `mime.TypeByExtension`, decompress
`.gz` for preview, enforce size cap, support
`?download=true` bypass. Register route as
`router.Get("/results/{id}/file",
server.handleGetFile)`. Covering all 12 acceptance tests
from A1.

- [x] implemented
- [x] reviewed

### Batch 2 (parallel, after item 1.1 is reviewed)

#### Item 1.2: B1 - Aggregate statistics [parallel with 1.3]

spec.md section: B1

Add `Store.Stats(ctx, recent, days)` method to
`results/store.go` returning `*StatsResult`. Add
`StatsResult`, `DailyCount`, and `PipelineCount` types to
`results/types.go`. Add `handleGetStats` handler to
`results/server.go`. Register route
`GET /results/stats` BEFORE `/{id}` so chi does not treat
`"stats"` as an ID. Query params: `recent` (default 10),
`days` (default 30). Covering all 7 acceptance tests
from B1.

- [x] implemented
- [x] reviewed

#### Item 1.3: C1 - Distinct metadata keys [parallel with 1.2]

spec.md section: C1

Add `Store.MetaKeys(ctx)` method to `results/store.go`
returning `([]string, error)`. SQL:
`SELECT DISTINCT meta_key FROM result_metadata ORDER BY
meta_key`. Add `handleGetMetaKeys` handler to
`results/server.go`. Register route
`GET /results/meta-keys` BEFORE `/{id}`. Covering all 3
acceptance tests from C1.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
