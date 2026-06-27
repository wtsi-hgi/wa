# MLWH Server MCP-Backend Enhancements Specification

## Overview

`wa mlwh serve` is a cache-backed, read-only REST API (gin + go-authserver)
whose defining discipline is a single declarative source of truth -
`mlwh/registry.go` (`Registry`) - from which the gin handler (`mlwh/server.go`)
and the Go `RemoteClient` (`mlwh/remote.go`) both derive, with every public
`mlwh.Queryer` method (`mlwh/queryer.go`) mapping 1:1 to exactly one endpoint.
This feature makes that server a good, self-describing backend for an external
MCP (natural-language) server that lives in a separate repo and only consumes
this API.

Seven additions, all preserving the Registry-driven, 1:1, cache-only,
indexed, web-responsive discipline of `.docs/mlwh-overhaul/spec.md` and
`.docs/mlwh-sync/spec.md`:

1. **Substring search** endpoints for studies and samples
   (`GET /search/study/:term`, `GET /search/sample/:term`).
2. **OpenAPI 3.1.0** document at `GET /openapi.json`, generated from enriched
   `Registry` metadata plus reflection over result types.
3. **Health and freshness**: `GET /health` (plain route) and `GET /freshness`
   (a Registry/Queryer endpoint).
4. **JSON casing fix**: add `snake_case` tags to `Match` and `TaggedID`; one
   atomic coordinated break with the frontend Zod schema.
5. **Count endpoints**: dedicated `/count` endpoints for the two searches and
   high-value list relationships, each returning `{count: N}`.
6. **Generated docs** for external implementors (endpoint reference + MLWH
   domain glossary + data-exposure/security posture), placed in `.docs/mcp/`.
7. **Unauthenticated HTTP posture** preserved and documented.

> Round-4 update: the former startup backend refusal is removed. Sample search
> now uses a word-token prefix index with no full-text dependency, so
> `wa mlwh serve` runs on any supported cache backend (including MariaDB and
> MySQL < 8) with no flavor or version check.

The work is additive except for the single coordinated casing break (Goal 4).
Existing endpoints keep their paths, bodies, and the `mlwhServerFetchAllLimit
= 1_000_000` fetch-all default unchanged; counts are separate endpoints.

## Architecture

### Search matching (Goal 1, authoritative per prompt round 3)

Match semantics are **true substring ("contains"), case-insensitive,
minimum effective term length 3, index-backed**. No relevance score; results
ordered by id for stable pagination.

- **Searchable fields (fixed):**
    - Study: `name`, `study_title`, `programme`, `faculty_sponsor`.
    - Sample: `name`, `supplier_name`, `common_name`, `donor_id`.
- **Study search** (small table, ~8k rows): plain `LIKE '%term%'` scan OR'd
  across the four study fields. No FTS index.
- **Sample search** (large table, ~10M rows): an index narrows candidates,
  then a case-insensitive `LIKE '%term%'` post-filter over the four sample
  fields guarantees exact substring semantics.
    - SQLite: an FTS5 external-content virtual table over the four fields using
      the `trigram` tokenizer; `MATCH` narrows, `LIKE` confirms.
    - MySQL: a single `FULLTEXT (...) WITH PARSER ngram` index over the four
      fields; `MATCH ... AGAINST` (boolean mode) narrows, `LIKE` confirms.
    - No MySQL server reconfiguration; rely on default `ngram_token_size = 2`
      plus the post-filter. Do NOT build a portable trigram-table fallback.
- **Parity is exact set-equality across dialects** because every backend
  applies the same `LIKE '%term%'` post-filter. The only permitted divergence
  is **accent handling** (MySQL `utf8mb4_0900_ai_ci` is accent-insensitive;
  SQLite trigram is accent-sensitive); cross-dialect set-equality tests use
  ASCII fixtures, and accent-folding is documented as backend-dependent.
- **Short terms:** term length < 3 returns HTTP 200 `[]` (and count 0) on both
  backends without touching the index. Empty terms are unreachable
  (`/search/{study,sample}/:term` 404s on an empty path segment).
- **Pagination:** the search endpoints use the existing `?limit`/`?offset`
  with **default page size 100, maximum 1000**. `limit` > 1000 is rejected
  with the existing `bad_request` 400 envelope (not clamped).

### Packages, files, types

- `mlwh/registry.go`: extend `Endpoint` with doc metadata and query-param
  specs; add the new entries (search, counts, freshness).
- `mlwh/queryer.go`: add the new `Queryer` members.
- `mlwh/search.go` (new): `*Client` substring-search and search-count methods
  (study and sample), dialect-branched via `c.cache.Dialect()`.
- `mlwh/count.go` (new): `*Client` count methods for the list relationships.
- `mlwh/freshness.go` (new): `Freshness` type + `*Client.Freshness`.
- `mlwh/server.go`: add per-method handler cases; add the plain `/health`
  route and the search-`limit` max guard; register `GET /openapi.json`.
- `mlwh/remote.go`: add `RemoteClient` methods for every new `Queryer` member.
- `mlwh/openapi.go` (new): assemble the OpenAPI 3.1.0 document from `Registry`
    - reflection over result types + the `{code,message}` envelope.
- `mlwh/docs.go` (new): generate the human endpoint reference from the same
  metadata.
- `mlwh/cache_schema/{sqlite,mysql}/sample_search.sql` (new): the FTS5 virtual
  table (SQLite) and the FULLTEXT index (MySQL).
- `mlwh/cache_schema.go`: bump `CacheSchemaVersion`; extend the schema-shape
  parser to represent the FTS5 virtual table and the MySQL FULLTEXT index;
  add them to the migration table lists.
- `mlwh/types.go`: add `json` tags to `TaggedID`; add `Count`, `Freshness`
  helper types; add `doc:` tags to directly-served fields.
- `mlwh/mlwh.go`: add `json` tags to `Match`.
- `cmd/mlwh.go`: add the startup backend-version refusal before serving.
- `frontend/lib/contracts.ts`: rewrite `identifierResultSchema` to snake_case;
  drop the PascalCase branch.

### New types

```go
// mlwh/count.go (or types.go): bare count envelope, snake_case.
type Count struct {
    Count int `json:"count" doc:"number of matching rows"`
}

// mlwh/freshness.go: per-table freshness, returned by GET /freshness.
type TableFreshness struct {
    Table      string `json:"table" doc:"mirrored MLWH table name"`
    HighWater  string `json:"high_water" doc:"latest synced last_updated (UTC RFC3339), empty if never synced"`
    LastRun    string `json:"last_run" doc:"timestamp of the last sync run (UTC RFC3339), empty if never synced"`
    EverSynced bool   `json:"ever_synced" doc:"false when no sync_state row exists for the table"`
}

type Freshness struct {
    Tables []TableFreshness `json:"tables" doc:"freshness per mirrored sync table"`
}
```

`SearchStudies`/`SearchSamples` return the existing `[]Study`/`[]Sample`
types **unchanged** (same models, registry helpers, `RemoteClient` decoders,
and frontend Zod schemas as every existing `Find*` endpoint). Full rows are
returned, including governance study fields (`data_access_group`,
`study_visibility`, `contains_human_dna`, `ega_*`); this is acceptable under
the unauthenticated/internal posture (Goal 7) and is noted in the
data-exposure docs (Goal 6).

### Queryer additions

```go
// Search (term is a path param; limit/offset pagination).
SearchStudies(ctx context.Context, term string, limit, offset int) ([]Study, error)
SearchSamples(ctx context.Context, term string, limit, offset int) ([]Sample, error)

// Counts (each returns Count; honour id_lims = 'SQSCP' like its read).
CountStudySearch(ctx context.Context, term string) (Count, error)
CountSampleSearch(ctx context.Context, term string) (Count, error)
CountStudies(ctx context.Context) (Count, error)
CountSamplesForStudy(ctx context.Context, studyLimsID string) (Count, error)

// Freshness (cache read; first-class Queryer method).
Freshness(ctx context.Context) (Freshness, error)
```

`var _ Queryer = (*Client)(nil)` and `var _ Queryer = (*RemoteClient)(nil)`
must continue to compile.

### Registry additions

All GET, JSON bodies, `{code,message}` envelope. Path identifiers
URL-path-escaped by `RemoteClient`, unescaped by the handler (existing
`mlwhPathParam`).

| Queryer method       | Path                       | PathParams | Paginated | Response  |
| -------------------- | -------------------------- | ---------- | --------- | --------- |
| SearchStudies        | /search/study/:term        | [term]     | true      | []Study   |
| SearchSamples        | /search/sample/:term       | [term]     | true      | []Sample  |
| CountStudySearch     | /search/study/:term/count  | [term]     | false     | Count     |
| CountSampleSearch    | /search/sample/:term/count | [term]     | false     | Count     |
| CountStudies         | /studies/count             | []         | false     | Count     |
| CountSamplesForStudy | /study/:id/samples/count   | [id]       | false     | Count     |
| Freshness            | /freshness                 | []         | false     | Freshness |

The `Endpoint` struct gains doc/param metadata (Goal 2); existing entries are
backfilled with summaries/descriptions. `/health` and `/openapi.json` are
**plain routes**, not `Registry`/`Queryer` entries.

### Enriched Endpoint metadata (Goal 2)

```go
type QueryParam struct {
    Name        string // e.g. "limit"
    Type        string // OpenAPI type, e.g. "integer"
    Required    bool
    Description string
}

type Endpoint struct {
    Method      string
    Verb        string
    Path        string
    PathParams  []string
    Query       []string
    Paginated   bool
    NewResult   func() any
    Summary     string       // short, human-readable (required, non-empty)
    Description string       // longer description (required, non-empty)
    QueryParams []QueryParam // structured specs for limit/offset and any filters
}
```

Per-field response descriptions come from a `doc:"..."` struct-tag convention
on the directly-served `mlwh` types; the OpenAPI generator and the human
reference both read them. Paginated entries auto-include `limit`/`offset`
`QueryParam`s.

### Startup backend refusal (prompt round 3)

`wa mlwh serve` checks the cache backend flavor/version at startup, after
`OpenCacheOnly`, reusing the existing `VERSION()` logic
(`mlwh/cache.go:1378-1398`, `mySQLServerVersion`/`mySQLMajorVersion`). On a
MySQL cache that is MariaDB or major version < 8, the command exits non-zero
with a clear error and never serves; SQLite and MySQL >= 8 proceed. Because an
unsupported backend never serves, the `Registry`, route registration, OpenAPI
document, `RemoteClient`, and coverage tests stay **unconditional** - every
entry is always present; there is no per-endpoint disablement and no degraded
mode.

### Error envelope (unchanged, reused)

`{code, message}` with sentinels mapped by `httpStatusAndErrorCode`
(`errors_http.go`): `not_found`/404, `ambiguous`/409,
`unsupported_identifier`/422, `cache_never_synced`/503,
`upstream_impaired`/502, plus the handler's `bad_request`/400 for bad
`limit`/`offset` and over-max `limit`. New endpoints reuse this unchanged.

---

## A. Substring search endpoints

### A1: study substring search

As the MCP server, I want `GET /search/study/:term` to return studies whose
`name`, `study_title`, `programme`, or `faculty_sponsor` contains `term`
(case-insensitive substring), so open-ended study questions are answerable.

`SearchStudies` runs a single SQL query against `study_mirror` with
`id_lims = 'SQSCP'` and `LIKE '%term%'` OR'd across the four fields, ordered by
`id_study_lims` with `id_study_tmp` tie-breaker, `LIMIT ? OFFSET ?`. The `term`
is escaped for `LIKE` wildcards (`%`, `_`, and the escape char) before binding.
Term length < 3 returns `[]Study{}` without querying. Returns `[]Study`
(full rows). Never-synced -> `errors.Join(ErrCacheNeverSynced, ErrNotFound)`
with an empty slice (matching `AllStudies`).

**Package:** `mlwh/`
**File:** `mlwh/search.go`
**Test file:** `mlwh/search_test.go`

```go
func (c *Client) SearchStudies(ctx context.Context, term string, limit, offset int) ([]Study, error)
```

**Acceptance tests:**

1. Given a synced SQLite cache with studies whose titles are
   `"Malaria genomics"`, `"Malaria vaccine"`, and `"Cancer atlas"`, when
   `SearchStudies(ctx, "malar", 100, 0)` runs, then it returns exactly the two
   malaria studies, ordered by `id_study_lims`.
2. Given the same cache, when `SearchStudies(ctx, " malaria ", ...)` matching
   only via `programme = "Malaria Programme"` runs, then the study with that
   programme is returned (search covers all four fields).
3. Given a term `"ma"` (length 2), when `SearchStudies(ctx, "ma", 100, 0)`
   runs, then it returns `[]Study{}` and issues no `LIKE` query (verified by a
   query-recording DB or by zero rows with a non-empty cache that would
   otherwise match).
4. Given >3 matching studies, when `SearchStudies(ctx, "study", 2, 1)` runs,
   then exactly 2 rows are returned starting at offset 1, in `id_study_lims`
   order.
5. Given a study whose title contains a literal `"50%"`, when
   `SearchStudies(ctx, "50%", 100, 0)` runs, then that study is returned and a
   study titled `"5024"` is not (the `%` is treated as a literal, not a
   wildcard).
6. Given a never-synced cache, when `SearchStudies(ctx, "abc", 100, 0)` runs,
   then it returns an empty slice and an error satisfying both
   `errors.Is(err, ErrCacheNeverSynced)` and `errors.Is(err, ErrNotFound)`.

### A2: sample word-prefix search (word-token prefix index)

> Superseded the round-3 FTS5/`LIKE`-post-filter design: see "Clarifications -
> round 4" in `prompt.md`. Sample search is now a **word-prefix** match served
> by the `sample_search_token` prefix index, identical across dialects (no FTS5
> trigram / MySQL ngram FULLTEXT, no `LIKE` post-filter on the sample hot path).

As the MCP server, I want `GET /search/sample/:term` to return samples having a
word in `name`, `supplier_name`, `common_name`, or `donor_id` that starts with
`term`, served by a word-token prefix index so it is fast on the ~10M-row table.

`SearchSamples` lowercases `term`, escapes its `LIKE` wildcards (`%`, `_`, and
the escape char), and pages the `sample_search_token` index in index order:
`WHERE token LIKE 'prefix%' ESCAPE '!' ORDER BY token, id_sample_tmp
LIMIT ? OFFSET ?`. Because the index covers `(token, id_sample_tmp)`, the page
streams from the index with no global sort (measured 48-62ms at any
cardinality, vs 4-21s for `SELECT DISTINCT ... ORDER BY id_sample_tmp`). A
sample can own several prefix-matching tokens, so ids are de-duplicated app-side
over the index-ordered stream (bounded over-fetch), then the matching
`sample_mirror` rows are fetched by id (`id_lims = 'SQSCP'`) and the fan-out is
populated as in `Find*`. Term length < 3 returns `[]Sample{}` without querying.
Matching is start-of-word: `musculus` and `mus` both match "Mus Musculus", but a
mid-word substring (`usculus`) does not - an accepted trade-off (the exact
`Find*` finders cover precise lookups). Returns `[]Sample` (full rows).

**Package:** `mlwh/`
**File:** `mlwh/search.go`
**Test file:** `mlwh/search_test.go`

```go
func (c *Client) SearchSamples(ctx context.Context, term string, limit, offset int) ([]Sample, error)
```

**Acceptance tests:**

1. Given a synced SQLite cache with samples whose `supplier_name`s are
   `"ACME-001"`, `"ACME-002"`, and `"OTHER-1"`, when
   `SearchSamples(ctx, "acme", 100, 0)` runs, then exactly the two ACME samples
   are returned, ordered by `id_sample_tmp`.
2. Given a sample whose only match is in `common_name = "Homo sapiens"`, when
   `SearchSamples(ctx, "sapien", 100, 0)` runs, then that sample is returned
   (search covers all four sample fields, matching the `sapiens` word prefix).
3. Given a sample whose `common_name` is "Mus Musculus", when
   `SearchSamples(ctx, "musculus", ...)` and `SearchSamples(ctx, "mus", ...)`
   run, then the sample matches both; when `SearchSamples(ctx, "usculus", ...)`
   runs (a mid-word substring), then it does not match.
4. Given term `"ac"` (length 2), when `SearchSamples(ctx, "ac", 100, 0)` runs,
   then it returns `[]Sample{}` and issues no query.
5. Given >3 matching samples, when `SearchSamples(ctx, "acme", 2, 1)` runs,
   then exactly 2 rows are returned starting at offset 1, in `id_sample_tmp`
   order.
6. Given a never-synced cache, when `SearchSamples(ctx, "abc", 100, 0)` runs,
   then the result is an empty slice and the error satisfies both
   `errors.Is(err, ErrCacheNeverSynced)` and `errors.Is(err, ErrNotFound)`.

### A3: sample search query construction and bounded count

> Superseded the round-3 MySQL ngram `MATCH ... AGAINST` design. The token-prefix
> SQL is identical across dialects (one code path, no dialect branch), so the
> MySQL unit tests assert the same `sample_search_token` SQL the SQLite path
> emits.

As a maintainer, I want the `SearchSamples` page query and the
`CountSampleSearch` query to be built correctly, so a sqlmock MySQL `Client`
(which cannot evaluate a real index) proves the SQL shape. The page query scans
`sample_search_token` by `token LIKE 'prefix%' ESCAPE '!' ORDER BY token,
id_sample_tmp LIMIT ? OFFSET ?` then fetches `sample_mirror` rows by id; it is
**not** a `SELECT DISTINCT ... ORDER BY id_sample_tmp`. `CountSampleSearch` is a
`SELECT COUNT(*) FROM (SELECT DISTINCT id_sample_tmp FROM sample_search_token
WHERE token LIKE ? ESCAPE '!' LIMIT ?)` bounded by a cap, so a mega-term counts
to the cap quickly and reports it as a floor. Real index matching is exercised
under a writable MySQL cache (see B3).

**Package:** `mlwh/`
**File:** `mlwh/search.go`
**Test file:** `mlwh/search_mysql_test.go`

**Acceptance tests:**

1. Given a `sqlmock` MySQL `Client`, when `SearchSamples(ctx, "acme", 100, 0)`
   runs, then the captured page SQL scans `sample_search_token` with
   `token LIKE ? ESCAPE '!' ORDER BY token, id_sample_tmp LIMIT ? OFFSET ?`
   (prefix `"acme%"`) and the by-id fetch selects `sample_mirror` rows
   `WHERE id_lims = 'SQSCP' AND id_sample_tmp IN (...)`.
2. Given a `sqlmock` MySQL `Client`, when `SearchSamples(ctx, "ab", 100, 0)`
   runs (term length 2), then no query is sent to the mock and the result is
   `[]Sample{}`.
3. Given a token page that repeats an id across prefix-matching tokens (e.g.
   `mus`, `musculus` for the same sample), when `SearchSamples` runs, then the
   id is de-duplicated and the sample fetched once.
4. Given a `sqlmock` MySQL `Client`, when `CountSampleSearch(ctx, "acme")` runs,
   then the captured SQL is the bounded `COUNT(*)` over a `SELECT DISTINCT
id_sample_tmp ... LIMIT ?` (cap), with the prefix and cap bound.

### A4: search routes, pagination guard, and RemoteClient

As the MCP server, I want `GET /search/study/:term` and `GET
/search/sample/:term` reachable unauthenticated with `?limit`/`?offset`
(default 100, max 1000), and `RemoteClient` methods that round-trip them.

The handler adds `SearchStudies`/`SearchSamples` cases. A search-specific
pagination reader defaults `limit` to 100 and rejects `limit` > 1000 with the
`bad_request` 400 envelope (the existing `mlwhServerFetchAllLimit` default is
untouched for non-search endpoints). `RemoteClient.SearchStudies`/
`SearchSamples` send `limit`/`offset` like other paginated methods and escape
the term path segment.

**Package:** `mlwh/`
**File:** `mlwh/server.go`, `mlwh/remote.go`
**Test file:** `mlwh/server_test.go`, `mlwh/remote_test.go`

**Acceptance tests:**

1. Given a server over a fake `Queryer` whose `SearchStudies("malar",100,0)`
   returns two studies, when `GET /search/study/malar` is served with no auth,
   then status is 200 and the body is a 2-element `[]Study` JSON array.
2. Given `GET /search/sample/acme?limit=2&offset=1`, then the fake queryer
   receives `term="acme", limit=2, offset=1`.
3. Given `GET /search/study/malar?limit=1001`, then status is 400 and the body
   is `{"code":"bad_request","message":...}` and the queryer is not called.
4. Given `GET /search/study/malar?limit=abc`, then status is 400 with code
   `bad_request`.
5. Given a `RemoteClient` pointed at an `httptest.Server` returning two
   samples, when `SearchSamples(ctx,"acme",100,0)` runs, then the request path
   is `/search/sample/acme?limit=100&offset=0` and it returns the two samples.
6. Given a term containing `/` or spaces, when `SearchSamples` runs on the
   `RemoteClient`, then the term path segment is URL-escaped.

---

## B. Search index schema and dialect parity

### B1: sample search token index in both dialect schemas

> Superseded the round-3 FTS5/FULLTEXT schema. The search index is now a normal
> `sample_search_token(token, id_sample_tmp)` table+index in both dialects.

As a developer, I want the sample search token index declared in both dialect
schemas and built/maintained by `wa mlwh sync`, so search is index-backed.

- Both dialects (`cache_schema/{sqlite,mysql}/sample_search_token.sql`): a
  normal table `sample_search_token(token, id_sample_tmp)` with index
  `(token, id_sample_tmp)`. Tokens are the distinct lowercased `[a-z0-9]+` words
  of `name`, `supplier_name`, `common_name`, `donor_id`. Built/maintained by the
  sample sync: cold-load bulk build with the covering index added after
  (mirroring the secondary-index discipline of `.docs/mlwh-sync/spec.md`), and
  incremental upsert/delete on sample writes. Measured ~4 tokens/row, ~1.7GB,
  ~7.5min to build at 10.35M samples.
- `CacheSchemaVersion` bumps by 1; `OpenCache` migration drops/recreates the
  affected tables (`sample_search_token` is in the migration drop/recreate set)
  and prints the existing one-line migration message.

**Package:** `mlwh/`
**File:** `mlwh/cache_schema/{sqlite,mysql}/sample_search_token.sql`,
`mlwh/cache_schema.go`, `mlwh/cache.go`
**Test file:** `mlwh/cache_schema_test.go`, `mlwh/cache_test.go`

**Acceptance tests:**

1. Given the SQLite schema, when applied via `OpenCache` on an empty cache,
   then a `sample_search_token(token, id_sample_tmp)` table with a
   `(token, id_sample_tmp)` index exists and is queryable by token prefix.
2. Given both dialect schema strings, when parsed, then each declares the token
   table and its `(token, id_sample_tmp)` index (no FTS5 / FULLTEXT).
3. Given a cache at the previous `CacheSchemaVersion`, when `OpenCache` runs at
   the new version, then it prints exactly one
   `mlwh cache: schema vX->vY, recreated tables: [...]` line (now including
   `sample_search_token`) and the token table is present afterwards.
4. Given a populated SQLite `sample_mirror` and a cold-load sync, when the
   token rebuild runs, then `sample_search_token` is populated (one row per
   distinct word token per sample), proving the index is built, not empty.

### B2: schema-shape parity represents the token index

> Superseded the FTS5/FULLTEXT shape model. `sample_search_token` is an ordinary
> table+index, so the parity model represents it like any other table - the
> `FullText` shape field and the FTS5/FULLTEXT/`CREATE TRIGGER` parsing special
> cases are removed.

As a developer, I want the two-dialect parity model to represent the
`sample_search_token` table+index, so the dialects cannot silently diverge on
search support.

`parseSchemaShape`/`schemaShape`/`compareCacheSchemaShapes` (`cache_schema.go`)
record `sample_search_token` in `Tables`/`Index` like every other table, so its
columns and `(token, id_sample_tmp)` index compare equal across dialects through
the existing table/index parity checks.

**Package:** `mlwh/`
**File:** `mlwh/cache_schema.go`
**Test file:** `mlwh/cache_schema_test.go`

**Acceptance tests:**

1. Given the SQLite and MySQL schemas at the new version, when
   `parseSchemaShape` runs on each, then both shapes record `sample_search_token`
   as a normal table with a `(token, id_sample_tmp)` index.
2. Given the parity comparison (`compareCacheSchemaShapes` or the existing
   parity test), when run, then the table sets, column sets, indexes, and unique
   constraints (including `sample_search_token`) all match across dialects.

### B3: cross-dialect search set-equality (ASCII fixtures)

As a maintainer, I want proof that SQLite and MySQL search return identical row
sets for the same term and data, using ASCII fixtures.

Seed identical study/sample rows (ASCII only) into a SQLite cache and, under
`WA_MLWH_DSN`, a MySQL cache; assert `SearchStudies`/`SearchSamples` and their
counts return identical id sets across backends. When `WA_MLWH_DSN` is unset,
the MySQL half skips with `t.Skip`.

**Package:** `mlwh/`
**File:** `mlwh/search_parity_test.go`
**Test file:** `mlwh/search_parity_test.go`

**Acceptance tests:**

1. Given identical ASCII sample fixtures in SQLite and (gated) MySQL, when
   `SearchSamples(ctx, "acme", 1000, 0)` runs on each, then the returned
   `id_sample_tmp` sets are equal.
2. Given the same fixtures, when `CountSampleSearch(ctx, "acme")` runs on each,
   then both `Count.Count` values are equal and equal to the row set size.
3. Given `WA_MLWH_DSN` unset, when the test runs, then the MySQL half is
   skipped (`t.Skip`) and the SQLite assertions still run.

---

## C. OpenAPI 3.1.0 document

### C1: enriched Registry metadata

As an OpenAPI generator, I want each `Endpoint` to carry a non-empty
`Summary`, `Description`, and structured `QueryParams`, and each directly-served
field to carry a `doc:` tag, so the document is useful to an LLM.

Backfill every existing `Registry` entry and every new entry with `Summary`/
`Description`. Paginated entries declare `limit`/`offset` `QueryParam`s
(`integer`; `limit` default 100 for search, documented). Add `doc:` tags to the
fields of `Match`, `TaggedID`, `Study`, `Sample`, `Lane`, `IRODSPath`,
`Library`, `Run`, the `*Detail` types, `EnrichmentResult`/`EnrichmentGraph`/
`MissingHop`, `SearchValues`, `Count`, and `Freshness`/`TableFreshness`.

**Package:** `mlwh/`
**File:** `mlwh/registry.go`, `mlwh/types.go`, `mlwh/mlwh.go`
**Test file:** `mlwh/registry_test.go`

**Acceptance tests:**

1. Given `Registry`, when iterated, then every entry has a non-empty `Summary`
   and non-empty `Description`.
2. Given each `Paginated` entry, when checked, then its `QueryParams` include
   `limit` and `offset` of type `integer`.
3. Given the `Study` and `Match` structs, when reflected, then every
   JSON-serialised field has a non-empty `doc:` tag.

### C2: OpenAPI document generation and coverage

As the MCP implementor, I want `GET /openapi.json` to serve a complete OpenAPI
3.1.0 document covering every endpoint and response type, so I can generate
tools without reading Go source.

`mlwh/openapi.go` builds the document from `Registry` + reflection over each
entry's `NewResult()` type + the `{code, message}` envelope. Identity:
`openapi: "3.1.0"`, `info.title = "wa mlwh API"`, `info.version` from a package
constant `mlwhAPIVersion`. Schemas reflect the **post-Goal-4 snake_case** JSON
field names and use the `doc:` tags as field descriptions. Paths include path
params, `limit`/`offset` query params (where paginated), and the error
responses with their `code` values and statuses (`not_found`/404,
`ambiguous`/409, `unsupported_identifier`/422, `cache_never_synced`/503,
`upstream_impaired`/502, `bad_request`/400). The document is served via a
plain route (not a `Registry` entry) and is reachable unauthenticated.

**Package:** `mlwh/`
**File:** `mlwh/openapi.go`, `mlwh/server.go`
**Test file:** `mlwh/openapi_test.go`, `mlwh/server_test.go`

**Acceptance tests:**

1. Given the generated document, when parsed, then `openapi == "3.1.0"`,
   `info.title == "wa mlwh API"`, and `info.version == mlwhAPIVersion`.
2. Given the document, when its `paths` are compared to `Registry`, then every
   `Registry` entry's `Path` and `Verb` appears with the correct path params
   and (for paginated entries) `limit`/`offset` query params.
3. Given the `Queryer` interface and the document, when checked, then every
   `Queryer` method name maps to exactly one documented path (1:1), asserted by
   reflecting over a `Queryer`-typed nil (mirrors
   `TestRegistryCoversQueryer`).
4. Given the `Match` schema in the document, when inspected, then it has
   properties `kind`, `canonical`, `sample`, `study`, `run`, `library`
   (snake_case, post-Goal-4) and no `Kind`/`Canonical` PascalCase keys.
5. Given the document, when inspected, then it defines the error envelope
   schema with `code` and `message` and documents the six stable `code`
   strings with their HTTP statuses.
6. Given `GET /openapi.json` served with no auth, then status is 200, the
   `Content-Type` is JSON, and the body parses as the same document.
7. Given the `Freshness`, `Count`, `[]Study` (search), and `[]Sample` (search)
   responses, when looked up by their paths, then each path's 200 response
   references the correct schema.

---

## D. Health and freshness endpoints

### D1: health plain route

As the MCP server, I want `GET /health` to return a cheap `{status: "ok"}` for
readiness checks, reachable unauthenticated and present in the OpenAPI
document.

`/health` is a plain gin route (outside `Registry`/`Queryer`) registered in
both the unauthenticated and secured router wiring. It does not touch the
cache. It is added to the OpenAPI `paths` by the generator as a documented
operational route.

**Package:** `mlwh/`
**File:** `mlwh/server.go`, `mlwh/openapi.go`
**Test file:** `mlwh/server_test.go`, `mlwh/openapi_test.go`

**Acceptance tests:**

1. Given a server with any `Queryer` (even a never-synced cache), when `GET
/health` is served with no auth, then status is 200 and the body is
   `{"status":"ok"}`.
2. Given the server handler source, when audited, then `/health` performs no
   cache read (it does not call the `Queryer`).
3. Given the OpenAPI document, when inspected, then `/health` appears with a
   200 `{status}` response.

### D2: freshness Queryer endpoint

As the MCP server, I want `GET /freshness` to report, per mirrored sync table,
its `high_water` and `last_run` (UTC RFC3339) and `ever_synced`, so chat can
say "data current as of ..." and degrade gracefully.

`Client.Freshness` reads `sync_state` for the five tables (`study`, `sample`,
`iseq_flowcell`, `iseq_product_metrics`, `seq_product_irods_locations`),
returning one `TableFreshness` each. `ever_synced` is false when the row is
absent; when absent, `high_water` and `last_run` are empty strings. Timestamps
are formatted UTC RFC3339 (`2006-01-02T15:04:05Z`). A never-synced cache
returns all five tables with `ever_synced=false` and **does not** error
(freshness is the graceful-degradation signal, so it must succeed even when no
sync has run). This requires a `sync_state` read that also selects `last_run`
(the existing `readSyncStateFromDB` selects only `high_water, resume_cursor,
indexes_dropped`).

**Package:** `mlwh/`
**File:** `mlwh/freshness.go`
**Test file:** `mlwh/freshness_test.go`

```go
func (c *Client) Freshness(ctx context.Context) (Freshness, error)
```

**Acceptance tests:**

1. Given a cache where only `sample` and `study` have `sync_state` rows with
   `high_water = 2026-06-01T10:00:00Z` and `last_run = 2026-06-01T10:05:00Z`,
   when `Freshness(ctx)` runs, then it returns five `TableFreshness` entries;
   `sample` and `study` have `ever_synced=true` with those exact timestamps,
   and the other three have `ever_synced=false` with empty timestamps.
2. Given a never-synced cache (no `sync_state` rows), when `Freshness(ctx)`
   runs, then it returns five entries all with `ever_synced=false` and empty
   timestamps, and no error.
3. Given a `sync_state` row with a non-UTC stored time, when `Freshness` runs,
   then the emitted `high_water` is normalised to UTC RFC3339 ending in `Z`.
4. Given the `RemoteClient`, when `Freshness(ctx)` runs against an
   `httptest.Server` wrapping a `Client`, then the decoded `Freshness` equals
   the local result (`reflect.DeepEqual`).

---

## E. JSON casing fix (coordinated break)

### E1: snake_case tags on Match and TaggedID

As a consumer, I want every response body consistently `snake_case`, so
`/classify`, `/resolve/*`, and `/expand/:kind/:id` no longer emit PascalCase
keys.

Add JSON tags:

- `Match` (`mlwh/mlwh.go`): `Kind->kind`, `Canonical->canonical`,
  `Sample->sample` (omitempty), `Study->study` (omitempty), `Run->run`
  (omitempty), `Library->library` (omitempty).
- `TaggedID` (`mlwh/types.go`): `Kind->kind`, `Canonical->canonical`.

`RemoteClient` decodes into the same structs, so it stays consistent
automatically; the parity test must still pass. `cmd/mlwh_info.go` reads
`match.Kind`/`match.Canonical` as Go fields and is unaffected at the Go level.

**Package:** `mlwh/`
**File:** `mlwh/mlwh.go`, `mlwh/types.go`
**Test file:** `mlwh/server_test.go`, `mlwh/types_test.go`

**Acceptance tests:**

1. Given `json.Marshal(Match{Kind: KindStudyLimsID, Canonical: "6568"})`, when
   serialised, then the JSON keys are `kind` and `canonical` (no `Kind`/
   `Canonical`), and absent pointers are omitted.
2. Given `GET /classify/6568` returning a `Match` with a `Study`, when served,
   then the body's top-level keys are `kind`, `canonical`, `study` (all
   snake_case), and the nested study uses its existing snake_case keys.
3. Given `json.Marshal(TaggedID{Kind: KindRunID, Canonical: "100"})`, then the
   JSON keys are `kind` and `canonical`.
4. Given `GET /expand/run_id/100`, when served, then each array element has
   keys `kind` and `canonical`.

### E2: parity preserved across the casing change

As a maintainer, I want the `RemoteClient`<->`Client` parity test to still pass
after the casing change, proving both sides agree on the new wire shape.

**Package:** `mlwh/`
**File:** `mlwh/parity_test.go`
**Test file:** `mlwh/parity_test.go`

**Acceptance tests:**

1. Given the seeded parity cache, when `ClassifyIdentifier`, `ResolveStudy`,
   and `ExpandIdentifier` are invoked on both `Client` and `RemoteClient`, then
   `reflect.DeepEqual(localResult, remoteResult)` holds for each (the
   round-trip through snake_case JSON is lossless).
2. Given the full parity table including the new methods (search, counts,
   freshness), when run, then the asserted method count equals the new
   `Queryer` member count and all results match.

### E3: frontend identifierResultSchema switches to snake_case

As the frontend, I want `validateIdentifier` to parse the new snake_case
`/classify` body and the PascalCase branch removed, so server and frontend ship
together.

Rewrite `mlwhMatchSchema` in `frontend/lib/contracts.ts` to keys `kind`,
`canonical`, `sample`, `study`, `run`, `library` (was `Kind`, `Canonical`,
`Sample`, `Study`, `Run`, `Library`); `mlwhMatchObject` reads `match.sample ??
match.study ?? match.run ?? match.library`; the `identifierResultSchema` union
maps `{canonical->identifier, kind->type, object}`. Drop the old PascalCase
shape entirely. Update the dependent test fixtures
(`frontend/tests/contracts.test.ts`, `frontend/tests/actions.test.ts`,
`frontend/tests/seqmeta-stub.test.ts`,
`frontend/tests/integration/setup.ts`) to emit snake_case. Audit confirms no
other raw-JSON consumer of `/classify`, `/resolve/*`, or `/expand/:kind/:id`
beyond `validateIdentifier` and the Go `RemoteClient`.

**Package:** `frontend/`
**File:** `frontend/lib/contracts.ts`, the listed test files
**Test file:** `frontend/tests/contracts.test.ts`,
`frontend/tests/actions.test.ts`

(Frontend testing conventions: nextjs-fastapi-conventions; vitest `expect()`.)

**Acceptance tests:**

1. Given a `/classify` body
   `{"kind":"study_lims_id","canonical":"6568","study":{...}}`, when parsed by
   `identifierResultSchema`, then the result is
   `{identifier:"6568", type:"study_lims_id", object:{...study...}}`.
2. Given a PascalCase body `{"Kind":...,"Canonical":...}`, when parsed by
   `identifierResultSchema`, then parsing fails (the PascalCase branch is
   removed).
3. Given a mock fetch and `WA_MLWH_BACKEND_URL` set, when
   `validateIdentifier("6568")` runs against a snake_case `/classify` response,
   then it returns `{identifier:"6568", type:"study_lims_id", object}` and the
   request URL ends `/classify/6568`.
4. Given a `grep` of `frontend/` (excluding `.next/`, `node_modules/`) for
   `Canonical:` or `Kind:` as `/classify`/`Match` JSON keys, when run, then
   there are zero matches in non-test source and the test fixtures use
   snake_case.

---

## F. Count endpoints

### F1: search count methods

As the MCP server, I want `GET /search/study/:term/count` and `GET
/search/sample/:term/count` to return `{count: N}` for the same matching as the
search reads, so "how many studies/samples match ..." needs no row transfer.

`CountStudySearch`/`CountSampleSearch` run `SELECT COUNT(*)` with the identical
WHERE clause as `SearchStudies`/`SearchSamples` (same `LIKE` post-filter, same
`id_lims = 'SQSCP'`, same index narrowing for samples), no `LIMIT`. Term
length < 3 returns `Count{Count: 0}` without querying. Never-synced returns
`Count{}` wrapped with `errors.Join(ErrCacheNeverSynced, ErrNotFound)`.

**Package:** `mlwh/`
**File:** `mlwh/search.go`
**Test file:** `mlwh/search_test.go`

**Acceptance tests:**

1. Given a synced SQLite cache with two studies matching `"malar"`, when
   `CountStudySearch(ctx, "malar")` runs, then it returns `Count{Count: 2}`.
2. Given a synced SQLite cache with three samples matching `"acme"`, when
   `CountSampleSearch(ctx, "acme")` runs, then it returns `Count{Count: 3}`,
   equal to `len(SearchSamples(ctx, "acme", 1000, 0))`.
3. Given term `"ab"` (length 2), when `CountSampleSearch(ctx, "ab")` runs, then
   it returns `Count{Count: 0}` and issues no query.
4. Given a never-synced cache, when `CountStudySearch(ctx, "abc")` runs, then
   the error satisfies both `errors.Is(err, ErrCacheNeverSynced)` and
   `errors.Is(err, ErrNotFound)`.

### F2: list-relationship count methods

As the MCP server, I want `GET /studies/count` and `GET
/study/:id/samples/count` returning `{count: N}`, so "how many studies" and
"how many samples in study X" are answerable without a fetch-all.

`CountStudies` counts `study_mirror` rows with `id_lims = 'SQSCP'`.
`CountSamplesForStudy` counts the same join/filter as `SamplesForStudy`
(distinct samples for the study via `library_samples`), honouring
`id_lims = 'SQSCP'`. Each is its own
`Queryer` method + `Registry` entry returning `Count`. Existing list endpoints
keep their bare-array bodies and fetch-all default unchanged.

**Package:** `mlwh/`
**File:** `mlwh/count.go`
**Test file:** `mlwh/count_test.go`

**Acceptance tests:**

1. Given a synced cache with 7 SQSCP studies (and some non-SQSCP), when
   `CountStudies(ctx)` runs, then it returns `Count{Count: 7}`.
2. Given study `6568` with 13 distinct samples across its libraries, when
   `CountSamplesForStudy(ctx, "6568")` runs, then it returns
   `Count{Count: 13}`, equal to `len(SamplesForStudy(ctx,"6568",1_000_000,0))`.
3. Given a study with no samples, when `CountSamplesForStudy` runs on it, then
   it returns `Count{Count: 0}` (not an error) when the cache is synced.
4. Given a never-synced cache, when `CountStudies(ctx)` runs, then the error
   satisfies both `errors.Is(err, ErrCacheNeverSynced)` and
   `errors.Is(err, ErrNotFound)`.

### F3: count routes and RemoteClient

As the MCP server, I want the four count endpoints reachable unauthenticated
and round-tripped by `RemoteClient`, each returning `{count: N}`.

The handler adds the four count cases; `RemoteClient` adds the four methods
(no pagination params). The registry coverage test (C2.3) includes them.

**Package:** `mlwh/`
**File:** `mlwh/server.go`, `mlwh/remote.go`
**Test file:** `mlwh/server_test.go`, `mlwh/remote_test.go`

**Acceptance tests:**

1. Given a fake `Queryer` whose `CountStudies` returns `Count{Count: 7}`, when
   `GET /studies/count` is served, then status is 200 and the body is
   `{"count":7}`.
2. Given `GET /study/6568/samples/count`, then the fake queryer receives
   `id="6568"` and the body is `{"count":N}`.
3. Given `GET /search/sample/acme/count`, then the queryer receives
   `term="acme"` and the body is `{"count":N}`.
4. Given a `RemoteClient` over an `httptest.Server`, when
   `CountSampleSearch(ctx,"acme")` runs, then the path is
   `/search/sample/acme/count` and it returns the server's `Count`.
5. Given `GET /study/6568/samples/count` where the queryer returns
   `ErrCacheNeverSynced`, then status is 503 with code `cache_never_synced`.

---

## G. Generated docs and unauthenticated posture

### G1: generated human endpoint reference and domain glossary

As an external implementor with no Go/MLWH background, I want a readable
endpoint catalogue and MLWH glossary that cannot drift from the served API.

`mlwh/docs.go` generates a Markdown (or equivalent) endpoint reference from the
same enriched `Registry` metadata used by the OpenAPI generator (every
endpoint, its path/params, summary/description, response type). A
`.docs/mcp/`-resident document is produced/refreshed from it (a generator test
asserts coverage so it cannot silently drift). A hand-authored MLWH **domain
glossary** in `.docs/mcp/` defines study, sample, run, library, lane, iRODS
path, and each `IdentifierKind` (`study_lims_id`, `sanger_sample_name`,
`run_id`, ...) and how the entities relate.

**Package:** `mlwh/`, `.docs/mcp/`
**File:** `mlwh/docs.go`, `.docs/mcp/` reference + glossary
**Test file:** `mlwh/docs_test.go`

**Acceptance tests:**

1. Given the generated endpoint reference, when checked, then it contains an
   entry for every `Registry` entry (path + summary), asserted against
   `Registry` so a missing endpoint fails the test.
2. Given the reference generator and the OpenAPI generator, when both run, then
   they cover the same set of `Registry` paths (no drift between human and
   machine forms).
3. Given the glossary document, when read, then it defines study, sample, run,
   library, lane, iRODS path, and lists every `IdentifierKind` constant value.

### G2: data-exposure and security-posture documentation

As an operator, I want the unauthenticated posture and full-metadata exposure
recorded as a deliberate decision, so it is not an oversight.

`.docs/mcp/` (and/or `DEVELOPING.md`) documents: the API is unauthenticated
plain HTTP by default; it exposes all mirrored MLWH metadata including
governance study fields (`data_access_group`, `study_visibility`,
`contains_human_dna`, `ega_dac_accession_number`,
`ega_policy_accession_number`) with no per-user authorisation, so the MCP
server / network boundary is the access-control boundary; the search surface
returns these same full rows; the data-freshness model (Goal 3); and the known
limitation that the optional secured gas mode moves endpoints under
`/rest/v1/auth` and the current `RemoteClient` does not add that prefix or
perform JWT login (documented, not fixed here).

**Package:** `.docs/mcp/`, repo root
**File:** `.docs/mcp/` security/posture doc, `DEVELOPING.md`
**Test file:** none (documentation)

**Acceptance tests:**

1. Given the posture document, when read, then it states the API is
   unauthenticated by default and names the governance study fields exposed.
2. Given the document, when read, then it notes the search surface returns the
   same full rows and references the freshness model.
3. Given the document, when read, then it records the secured-mode
   `RemoteClient` prefix/login limitation as out of scope.

### G3: all new endpoints reachable unauthenticated

As the MCP server, I want search, counts, OpenAPI, health, and freshness all
reachable in the default unauthenticated mode at root paths, exactly like
existing endpoints.

The unauthenticated wiring (`server.RegisterRoutes(authServer.Router(), nil)`
in `cmd/mlwh.go`) registers all new `Registry` endpoints plus `/health` and
`/openapi.json` on the public router. No auth/TLS is added.

**Package:** `mlwh/`, `cmd/`
**File:** `mlwh/server.go`, `cmd/mlwh.go`
**Test file:** `cmd/mlwh_test.go`, `mlwh/server_test.go`

**Acceptance tests:**

1. Given `wa mlwh serve` over a synced SQLite cache with no auth configured,
   when `GET /search/study/malar`, `GET /studies/count`, `GET /freshness`,
   `GET /health`, and `GET /openapi.json` are each requested, then all return
   200 (unauthenticated).
2. Given `wa mlwh serve` secured (cert + key + server-token), when the same
   endpoints are requested without a Bearer token, then each returns 401
   (the new endpoints register behind the auth group like existing ones).
3. Given the unauthenticated server, when `GET /freshness` is requested against
   a never-synced cache, then status is 200 (freshness degrades gracefully; it
   does not surface 503).

---

## H. Startup backend handling

### H1: no backend refusal (runs on any supported cache backend)

> Superseded the round-3 startup refusal. The word-token prefix index has no
> full-text dependency and works on MariaDB and MySQL < 8, so `wa mlwh serve`
> no longer inspects the backend flavor/version. `SupportsFullTextSearch` and
> the `cmd/mlwh.go` serve guard are removed.

As an operator, I want `wa mlwh serve` to start on any supported cache backend,
so a MariaDB or MySQL < 8 cache is not gratuitously refused.

After `mlwh.OpenCacheOnly`, `wa mlwh serve` wires routes and starts directly -
there is no `VERSION()` probe and no flavor/version refusal.

**Package:** `mlwh/`, `cmd/`
**File:** `mlwh/search.go`, `cmd/mlwh.go`
**Test file:** `cmd/mlwh_test.go`

**Acceptance tests:**

1. Given a cache opened on any supported backend, when `wa mlwh serve` runs,
   then it registers routes, binds a listener, and serves requests, with no
   backend-flavor or version refusal.

---

## Implementation Order

Phases build on tested foundations. Within a phase, stories may proceed in
parallel unless noted.

1. **Phase 1 - search schema + parity.** B1 (sample search index, schema
   version bump), B2 (schema-shape parity extension). B1 before B2. These are
   pure `mlwh` schema/parser changes with no endpoints yet.
2. **Phase 2 - search + count + freshness Client methods.** A1, A2, A3
   (search), F1, F2 (counts), D2 (`Client.Freshness`). Depend on Phase 1
   (sample search needs the index). B3 (cross-dialect set-equality) lands with
   A2/A3/F1.
3. **Phase 3 - casing fix.** E1 (snake_case tags), E2 (parity preserved). E1
   before anything that reflects field names for OpenAPI. Independent of
   Phase 1/2 except it must precede C2 (OpenAPI reflects post-Goal-4 JSON).
4. **Phase 4 - Queryer + Registry + handlers + RemoteClient.** Add the new
   `Queryer` members, `Registry` entries (search, counts, freshness), handler
   cases, the search `limit` guard, and `RemoteClient` methods: A4, F3, D1
   (`/health` route), and the freshness/`RemoteClient` half of D2. Depends on
   Phases 2-3.
5. **Phase 5 - OpenAPI + docs.** C1 (enriched metadata + `doc:` tags), C2
   (OpenAPI document + coverage), G1 (generated reference + glossary). Depends
   on Phases 3-4 (registry entries and snake_case final).
6. **Phase 6 - startup refusal.** H1. (Round-4: the refusal is removed; the
   word-token prefix index has no full-text dependency, so `wa mlwh serve` runs
   on any supported backend.)
7. **Phase 7 - frontend + posture docs.** E3 (frontend Zod + tests), G2
   (security/posture doc), G3 (unauthenticated reachability tests). E3 depends
   on E1 shipping in the same change set (atomic break). G3 depends on
   Phase 4.

---

## Appendix: Key Decisions

- **Sample search is a word-token prefix index (round-4).** The FTS5 trigram /
  MySQL ngram full-text approach was abandoned: at 10.35M rows ngram FULLTEXT
  threw `Error 188` for most terms (and 20-32s otherwise), and a trigram inverted
  index cost ~16GB and 1-4s queries. Instead `sample_search_token(token,
id_sample_tmp)` stores the distinct lowercased `[a-z0-9]+` words of the four
  fields, and search pages it in `(token, id_sample_tmp)` index order
  (`token LIKE 'prefix%'`) - 48-62ms at any cardinality, identical SQL across
  dialects. Semantics are start-of-word (not mid-word substring); the >=3-char
  floor stands. `CountSampleSearch` is an exact `COUNT(DISTINCT id_sample_tmp)`
  bounded by a cap (reported as a floor for mega-terms) so it stays ~80ms.
- **Study search is an un-indexed substring scan.** `study_mirror` is ~8k rows,
  so an OR'd `LIKE '%term%'` scan (3-9ms) is web-responsive without an index.
  It is deliberately left as substring (unchanged by round-4), so only
  `sample_mirror` carries a derived search index.
- **Counts are separate endpoints, not a list envelope.** Existing list
  endpoints keep bare-array bodies and the `mlwhServerFetchAllLimit =
1_000_000` fetch-all default, so the frontend study-samples / library-samples
  fetch-all calls and `mlwhdiff` (`providerFetchLimit`) are untouched. Each
  count is its own `Queryer` method + `Registry` entry returning `{count:N}`,
  honouring `id_lims = 'SQSCP'` like its corresponding read.
- **Search pagination is its own contract.** Search endpoints default `limit`
  to 100 and reject `limit` > 1000 with `bad_request` 400; the global fetch-all
  default is unchanged for every non-search endpoint.
- **OpenAPI/reference are generated from one source.** Both derive from the
  enriched `Registry` metadata + `doc:` tags + reflection over result types, so
  they cannot drift from the served API; coverage tests assert every `Registry`
  entry and every `Queryer` method is documented, against the post-Goal-4
  snake_case JSON. No OpenAPI library is added; the document is hand-assembled
  in `mlwh/openapi.go`.
- **Health vs freshness placement.** `/health` is a plain operational route
  (no cache read) so readiness checks stay cheap and never 503. `Freshness` is
  a first-class `Queryer`/`Registry` endpoint (a cache read) and must succeed
  even on a never-synced cache so the MCP layer can degrade gracefully instead
  of seeing `cache_never_synced`.
- **The casing fix is the one coordinated break.** `Match` and `TaggedID` gain
  snake_case tags; the frontend `identifierResultSchema` switches to snake_case
  and drops the PascalCase branch in the same change set. `RemoteClient` and
  `cmd/mlwh info` consume the Go structs directly and stay consistent
  automatically; the parity test is the regression guard.
- **Unsupported backends never serve, so everything stays unconditional.**
  Refusing to start on MariaDB / MySQL < 8 means the `Registry`, routes,
  OpenAPI, `RemoteClient`, and coverage tests need no per-endpoint disablement
  or degraded mode, and no portable trigram fallback is built.
- **Testing.** GoConvey throughout (`So()` assertions; counts asserted, not
  per-row `So` in loops). Handler tests via `httptest`; `RemoteClient` tests
  via `httptest`; `RemoteClient`<->`Client` parity via a real
  `modernc.org/sqlite` cache (extended to the new methods). SQLite FTS5
  matching is exercised in unit tests; MySQL ngram matching only under
  `WA_MLWH_DSN` (sqlmock cannot evaluate the full-text predicate, so MySQL unit
  tests assert query construction). Schema-shape parity is the regression guard
  for the search index. Frontend vitest covers the snake_case schema; existing
  enrichment fixtures must still pass (the enrich contract is unchanged). See
  `go-implementor`/`go-reviewer` (and `nextjs-fastapi-implementor`/`-reviewer`
  for the frontend) for the TDD loop.
