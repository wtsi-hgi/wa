# Phase 1: Foundation

Ref: [spec.md](spec.md) sections A1, A2, A3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 1.1: A1 - Embedded per-dialect cache schema with parity test

spec.md section: A1

Create `mlwh/cache_schema.go` with `embed.FS` over
`cache_schema/sqlite/*.sql` and `cache_schema/mysql/*.sql`, plus
`loadSchema(dialect)`, `schemaShape`, and `parseSchemaShape` helpers.
Author the per-dialect SQL files for `study_mirror`, `sample_mirror`,
`library_samples`, `donor_samples`, `negative_cache`, `watermarks`,
`enrich_cache`, `sync_state`, and `schema_version`. Covers all 4
acceptance tests from A1.

- [x] implemented
- [x] reviewed

### Item 1.2: A2 - Cache Open and schema versioning

spec.md section: A2

Implement `mlwh/cache.go` with `Cache` interface, `OpenCache`,
`CacheConfig`, `CacheSchemaVersion`, WAL pragma for SQLite, password
rejection for MySQL DSNs, separate read-only and read-write handles,
and `GET_LOCK`/`sync.Mutex` sync serialisation. Depends on Item 1.1.
Covers all 9 acceptance tests from A2.

- [x] implemented
- [x] reviewed

### Item 1.3: A3 - Sync engine with watermarks

spec.md section: A3

Implement `mlwh/sync.go` with `SyncReport` and
`(*Client).Sync(ctx, tables...)`. Sync `sample` and `study` tables
filtered by `id_lims = 'SQSCP'`, plus `iseq_flowcell`, all filtered by
`last_updated >= high_water`. Advance `sync_state` only after commit,
roll back cleanly on partial failure. Depends on Item 1.2. Covers all
6 acceptance tests from A3.

- [x] implemented
- [x] reviewed
