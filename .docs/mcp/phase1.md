# Phase 1: search schema + parity

Ref: [spec.md](spec.md) sections B1, B2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

These are pure `mlwh` schema/parser changes with no endpoints yet. B1
(declare and migrate the sample search index) must land before B2 (teach
the parity model to represent it).

## Items

### Item 1.1: B1 - sample search index in both dialect schemas

spec.md section: B1

Add the embedded DDL `mlwh/cache_schema/sqlite/sample_search.sql` (a
`CREATE VIRTUAL TABLE ... USING fts5(name, supplier_name, common_name,
donor_id, content='sample_mirror', content_rowid='id_sample_tmp',
tokenize='trigram')` external-content table) and
`mlwh/cache_schema/mysql/sample_search.sql` (a single `CREATE FULLTEXT
INDEX sample_mirror_search_ftx ON sample_mirror (name, supplier_name,
common_name, donor_id) WITH PARSER ngram`). Bump `CacheSchemaVersion` by
1 in `mlwh/cache_schema.go`; wire the FTS5 table into the SQLite
drop/recreate migration path in `mlwh/cache.go` and keep it populated by
the existing sample sync (cold-load index discipline), printing the
existing one-line migration message. Tests in `mlwh/cache_schema_test.go`
and `mlwh/cache_test.go`. Covers all 4 acceptance tests from B1.

- [x] implemented
- [x] reviewed

### Item 1.2: B2 - schema-shape parity represents the search index

spec.md section: B2

Extend `parseSchemaShape`/`schemaShape`/`compareCacheSchemaShapes` in
`mlwh/cache_schema.go` to recognise `CREATE VIRTUAL TABLE ... USING
fts5(...)` (SQLite) and MySQL `FULLTEXT (...) WITH PARSER ngram` (inline
in `CREATE TABLE` or a separate `CREATE FULLTEXT INDEX`), recording a
normalised "full-text search index over columns {name, supplier_name,
common_name, donor_id} on sample_mirror" entry that compares equal across
dialects and is sensitive to the search column set. Tests in
`mlwh/cache_schema_test.go`. Covers all 3 acceptance tests from B2.

Depends on Item 1.1 (the search index must exist in both schemas first).

- [x] implemented
- [x] reviewed
