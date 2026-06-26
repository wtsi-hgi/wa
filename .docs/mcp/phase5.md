# Phase 5: OpenAPI + generated docs

Ref: [spec.md](spec.md) sections C1, C2, G1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

This phase makes the server self-describing. Depends on Phase 4 (all
`Registry` entries and routes exist) and Phase 3 (snake_case JSON is
final, since the schemas reflect post-Goal-4 field names). C1 (enrich the
metadata and add `doc:` tags) lands first because both the OpenAPI
document (C2) and the human reference (G1) read it; C2 and G1 then proceed
in parallel against different files.

## Items

### Item 5.1: C1 - enriched Registry metadata + doc tags

spec.md section: C1

Extend `Endpoint` with `Summary`, `Description`, and `[]QueryParam`
(`QueryParam{Name, Type, Required, Description}`) in `mlwh/registry.go`,
and backfill every existing and new entry with a non-empty `Summary`/
`Description`; paginated entries declare `limit`/`offset` `integer`
`QueryParam`s (search `limit` default 100, documented). Add `doc:"..."`
tags to the directly-served fields of `Match`, `TaggedID`, `Study`,
`Sample`, `Lane`, `IRODSPath`, `Library`, `Run`, the `*Detail` types,
`EnrichmentResult`/`EnrichmentGraph`/`MissingHop`, `SearchValues`,
`Count`, and `Freshness`/`TableFreshness` across `mlwh/registry.go`,
`mlwh/types.go`, `mlwh/mlwh.go`. Tests in `mlwh/registry_test.go`. Covers
all 3 acceptance tests from C1.

This item is the metadata foundation for Items 5.2 and 5.3.

- [ ] implemented
- [ ] reviewed

### Batch 1 (parallel, after item 5.1 is reviewed)

#### Item 5.2: C2 - OpenAPI document generation and coverage [parallel with 5.3]

spec.md section: C2

Implement `mlwh/openapi.go` to assemble an OpenAPI 3.1.0 document from
`Registry` + reflection over each entry's `NewResult()` type + the
`{code, message}` envelope: `openapi: "3.1.0"`, `info.title = "wa mlwh
API"`, `info.version = mlwhAPIVersion` (new package constant). Schemas
reflect the post-Goal-4 snake_case field names and use `doc:` tags as
field descriptions; paths carry path params, `limit`/`offset` (where
paginated), and the six stable error codes with their statuses. Serve it
via the plain `GET /openapi.json` route in `mlwh/server.go` (also add
`/health` to the document's paths). Tests in `mlwh/openapi_test.go` and
`mlwh/server_test.go`. Covers all 7 acceptance tests from C2 (and D1
acceptance test 3, `/health` present in the document).

- [ ] implemented
- [ ] reviewed

#### Item 5.3: G1 - endpoint reference + glossary [parallel with 5.2]

spec.md section: G1

Implement `mlwh/docs.go` to generate a human Markdown endpoint reference
from the same enriched `Registry` metadata (every endpoint: path/params,
summary/description, response type), producing/refreshing a `.docs/mcp/`
reference document with a generator test asserting coverage so it cannot
drift. Hand-author an MLWH domain glossary in `.docs/mcp/` defining study,
sample, run, library, lane, iRODS path, and every `IdentifierKind`
constant value (`study_lims_id`, `sanger_sample_name`, `run_id`, ...) and
how the entities relate. Tests in `mlwh/docs_test.go`. Covers all 3
acceptance tests from G1.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).
