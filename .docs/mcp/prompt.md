# Feature: `mlwh` server enhancements for an external MCP / natural-language query backend

## Background

A separate repository will host an **MCP (Model Context Protocol) server** that
lets users ask natural-language chat questions about data in the Sanger
Multi-LIMS Warehouse (MLWH). That MCP server will translate chat into calls
against the read-only HTTP API served by `wa mlwh serve`, then shape the JSON
responses back into answers. The MCP server itself is **out of scope here** —
this feature covers only the changes to **this** repo (`wa`) that make
`wa mlwh serve` a good, self-describing backend for it.

`wa mlwh serve` today (see `.docs/mlwh-overhaul/spec.md`, the governing prior
art) is a cache-backed, read-only REST API built on gin + go-authserver
("gas"). Its defining discipline is a single declarative source of truth —
`mlwh/registry.go` (`Registry`) — from which the gin handler
(`mlwh/server.go`) and the Go `RemoteClient` (`mlwh/remote.go`) both derive,
with every public `mlwh.Queryer` method (`mlwh/queryer.go`) mapping 1:1 to
exactly one endpoint. All endpoints are `GET`, return JSON, read only from the
synced SQLite/MySQL cache (no live-MLWH fallback), and surface a typed error
envelope `{code, message}` (`mlwh/errors_http.go`). It is unauthenticated by
default; every in-repo consumer (`wa mlwh info`, `wa mlwhdiff serve`,
`wa results serve`, the frontend) talks to it as plain HTTP at root paths.

Assessment of fit for the MCP use case:

- **Already a good fit** for "I have an identifier → classify, resolve, enrich,
  traverse its relations" questions. `GET /classify/:id` (accepts any raw
  string and infers its kind), `GET /enrich/:id` (returns a whole graph with
  `partial`/`missing` hop annotations), and the `*/detail` aggregates map
  naturally onto chat intents. The Registry + typed models + reference
  `RemoteClient` mean a competent implementor needs nothing from us to build
  that part.
- **The real functional gap is discovery / search.** Every existing "find"
  endpoint is an *exact* match (`/find/sample/supplier-name/:id`, etc.). There
  is no substring/fuzzy search and no server-side filtering, so open-ended
  natural-language questions ("studies about malaria", "studies sponsored by
  X", "samples whose supplier name contains …", "how many samples in study
  1234") either cannot be answered or force a fetch-everything-then-filter
  pattern in the MCP layer.
- **Secondary gaps that degrade the MCP experience:** no machine-readable API
  description (the only "doc" is the Go Registry plus one `curl` example in the
  README); no health or data-freshness endpoint (so chat cannot say "data
  current as of …" and cannot degrade gracefully); a JSON key-casing
  inconsistency on two directly-served types; an effectively unbounded
  pagination default that can dump huge payloads into an LLM context; and no
  external-facing endpoint/domain documentation.

This feature closes the search gap and addresses those secondary gaps, while
preserving the overhaul's Registry-driven, 1:1, cache-only, web-responsive
discipline. Two decisions are already fixed by the requester and recorded under
**Notes**: the API stays unauthenticated plain HTTP for now, and an OpenAPI
description is in scope.

## Goals

### 1. Search and discovery endpoints (the primary gap)

Add cache-backed search/filter queries so the MCP server can answer open-ended
"find / which / how many" questions instead of only exact-identifier lookups.

- Cover at least the high-value discovery intents:
  - **Study search** over human-meaningful fields — `name`, `study_title`,
    `programme`, `faculty_sponsor` (the spec should confirm the exact field
    set). The study table is comparatively small.
  - **Sample search** over human-meaningful fields — e.g. `name`,
    `supplier_name`, `common_name`, `donor_id` (confirm the set). The sample
    table is very large (millions of rows), so sample search needs special care
    for indexing and result bounding.
- Each new query follows the established 1:1 discipline and the "add a new MLWH
  query" checklist from `.docs/mlwh-overhaul/spec.md` §Goal 4: add any needed
  column/index to **both** dialect schemas
  (`mlwh/cache_schema/{sqlite,mysql}/*.sql`) and the parity test → add one
  cache-only `*mlwh.Client` method → add one `mlwh.Queryer` interface member →
  add one `Registry` entry (handler + `RemoteClient` derive from it).
- **Web-responsive speed is a hard requirement** (per the overhaul): every
  search must be served by an indexed read path, not a table scan. The spec
  must resolve the matching strategy with this in mind, since naive
  `LIKE '%term%'` is not index-backed. Candidate approaches the spec should
  weigh and choose between (per field / per table): index-backed **prefix**
  search (`term%`); full-text search (SQLite **FTS5** vs MySQL **FULLTEXT**);
  or accepting a bounded scan only where a table is provably small. Note the
  dialect-parity wrinkle: FTS5 and FULLTEXT differ in tokenisation and
  match semantics, which complicates the `RemoteClient`↔`Client` parity test
  and the two-dialect matrix — the spec must define behaviour precisely enough
  to test identically across dialects (or document and test the permitted
  divergence).
- Search results must be **bounded and count-aware** (see Goal 5): support
  `limit`/`offset` and a way to obtain a total match count, so "how many …"
  is answerable without transferring every row.
- Search is **additive** — it must not change or remove any existing endpoint.

### 2. Machine-readable API description (OpenAPI) derived from the Registry

Serve a complete, accurate OpenAPI description of the API so the MCP
implementor can generate tools (and stay in sync as endpoints are added)
rather than hand-transcribing Go source.

- Serve an **OpenAPI 3.x document** at a stable, unauthenticated path (e.g.
  `GET /openapi.json`), and optionally a small human-friendly capability index
  at `GET /` listing the endpoints.
- The document must be **derived from the single source of truth** so it cannot
  drift: paths, verbs, path params, query params (`limit`/`offset` and any
  search filters), and pagination come from `Registry`; response and error
  schemas come from the `mlwh` Go types and the `{code, message}` envelope. The
  spec chooses the mechanism (e.g. reflection over the registered types at
  startup, a generated artifact, or registry metadata) but must include a test
  asserting **every `Registry` entry and every `Queryer` method appears** in
  the document with the correct shape.
- Make the document genuinely useful to an LLM: each endpoint carries a
  human-readable **summary and description**, and the key response types carry
  **per-field descriptions**. This implies enriching `Registry`/types with doc
  metadata; that same metadata feeds the human reference in Goal 6. Document
  the error envelope and the stable `code` values
  (`not_found`, `ambiguous`, `unsupported_identifier`, `cache_never_synced`,
  `upstream_impaired`) and their HTTP statuses.

### 3. Health and data-freshness endpoints

Let the MCP server check liveness and tell users how current the data is.

- A **liveness/health** endpoint (e.g. `GET /health`) returning a cheap OK
  status for readiness checks.
- A **data-freshness/status** endpoint exposing, per mirrored table, the
  last-sync high-water timestamp from the existing `sync_state` data (the
  sync watermarks already tracked by `wa mlwh sync`; see `mlwh/cache.go` and
  the `SyncReport.HighWater` values), plus whether the cache has ever been
  synced. This lets chat answer "data current as of …" and lets the MCP server
  degrade gracefully instead of surfacing a raw 503 / `cache_never_synced`.
- The spec should decide placement consistent with the architecture: freshness
  is a cache read and may fit the `Queryer`/`Registry` 1:1 pattern, whereas
  health is an operational server route; either way it must be covered by the
  OpenAPI document (Goal 2) and tests.

### 4. Consistent JSON field casing

Make every response body consistently `snake_case`.

- Two directly-served types currently have **no JSON tags** and therefore
  serialise with Go's `PascalCase` field names, producing mixed casing in the
  same payload (e.g. `/classify` returns `{"Kind":…,"Canonical":…,"Study":{…
  snake_case …}}`):
  - `Match` (`mlwh/mlwh.go`) — returned by `/classify/:id`, `/resolve/*`.
  - `TaggedID` (`mlwh/types.go`) — returned by `/expand/:kind/:id`.
- Add `snake_case` JSON tags to both so all endpoints are uniform.
- This is a **breaking wire change** with a defined blast radius that must be
  updated in lockstep:
  - Frontend: `validateIdentifier` in `frontend/app/(results)/actions.ts`
    parses `/classify` through the Zod `identifierResultSchema` — update that
    schema (and any dependent TS type / `studies-cache` usage) to the new keys.
  - `RemoteClient` decodes into the **same** Go structs as the server encodes,
    so it stays automatically consistent; the `RemoteClient`↔`Client` parity
    test must still pass. `wa mlwh info` (which consumes `Match` via
    `RemoteClient`) is unaffected at the Go level.
  - Audit for any other consumer of the affected endpoints' raw JSON.

### 5. Bounded, count-aware pagination for LLM contexts

Prevent a single tool call from pulling an unbounded payload into the model
context, and make counts cheap.

- Current behaviour to preserve awareness of: the handler default when `?limit`
  is absent is `mlwhServerFetchAllLimit = 1_000_000` (fetch-all)
  (`mlwh/server.go`). `RemoteClient` always sends explicit `limit`/`offset`;
  `mlwhdiff` passes an explicit `providerFetchLimit = 1_000_000`
  (`mlwhdiff/provider.go`); but the **frontend omits `limit`** on
  `/study/:id/samples` and the library-samples routes and relies on the
  fetch-all default. So a naive change to the default would silently truncate
  the frontend and any other param-omitting caller.
- The spec must deliver bounded, count-aware listing **without breaking
  fetch-all callers**. It should choose and justify an approach such as:
  - a documented **maximum limit** guard high enough not to disturb the
    explicit-large-limit internal callers, plus a documented recommended page
    size for external/MCP clients;
  - **count** support (dedicated count endpoints, or a `count`-only mode, or a
    total exposed via response metadata / header) so "how many …" needs no full
    transfer — this also backs Goal 1;
  - if a richer list envelope (total + truncation flag) is considered, treat the
    change from today's bare-JSON-array bodies as breaking and coordinate it
    with the frontend and `RemoteClient` decoders, or keep it opt-in.
- Whatever is chosen must keep the existing frontend study-samples and
  `mlwhdiff` fetch-all behaviour correct.

### 6. Documentation for external / MCP implementors

Give an implementor with no Go or MLWH background everything needed to build
against the API.

- An **endpoint reference** (human-readable; the OpenAPI of Goal 2 may be the
  machine form, but a readable catalogue is also wanted) covering every
  endpoint, its params, and its response shape. Today README/DEVELOPING
  document deployment and env vars but not the endpoint catalogue.
- An **MLWH domain glossary** aimed at an LLM/implementor: what a study,
  sample, run, library, lane, and iRODS path are; what each `IdentifierKind`
  means (`study_lims_id`, `sanger_sample_name`, `run_id`, …) and how the
  entities relate. This is high-leverage for natural-language tool building.
- Document the **data-freshness model** (Goal 3) and the **security posture**:
  the API is unauthenticated and exposes all mirrored MLWH metadata —
  including governance-relevant study fields (`data_access_group`,
  `study_visibility`, `contains_human_dna`, `ega_dac_accession_number`,
  `ega_policy_accession_number`) — with no per-user authorisation, so the MCP
  server / network boundary is the access-control boundary (see Goal 7).
- Place docs where the project keeps them (`.docs/mcp/` and/or `DEVELOPING.md`),
  and consider serving the reference alongside `/openapi.json`.

### 7. Keep the unauthenticated HTTP posture; document its implications

Authentication is **not** being added in this feature (recorded decision under
Notes). The work here must:

- Leave `wa mlwh serve` unauthenticated plain HTTP by default, exactly as the
  overhaul established (auth/TLS remain supported-but-optional via gas, off by
  default; institutional policy still requires the secured gas JWT + TLS mode
  whenever the service is exposed across the HPC↔OpenStack cluster boundary).
  The MCP deployment is assumed same-cluster/internal.
- Ensure all new endpoints (search, OpenAPI, health, freshness) are reachable
  in the default unauthenticated mode at root paths, consistent with how
  existing endpoints and consumers work.
- Document the data-exposure consequence (Goal 6) so it is a deliberate,
  recorded posture rather than an oversight.
- Known limitation to note (not to fix here): when the optional secured mode is
  enabled, endpoints move under the gas `/rest/v1/auth` prefix and require a
  JWT minted via `/rest/v1/jwt`, and the current `RemoteClient` does not add
  that prefix or perform that login — so securing the MCP path later is a
  separate piece of work.

## Constraints

- **Preserve the overhaul's architecture.** New query endpoints must go through
  the schema-index → `Client` method → `Queryer` member → `Registry` entry
  pipeline; the handler, `RemoteClient`, and now the OpenAPI document all derive
  from the single `Registry`/types source. No bespoke per-endpoint marshalling
  hand-written in multiple places.
- **Cache-only, indexed, web-responsive.** Reads stay cache-only (no live-MLWH
  fallback) and inherit `ErrCacheNeverSynced` semantics. Every new query is
  backed by an index; a query needing a new index adds it (both dialects) per
  the read-path-audit discipline of `.docs/mlwh-sync/spec.md`.
- **Read-only.** The API gains no write/mutation endpoints.
- **gin + gas stack** is unchanged; the error envelope and sentinel↔status
  mapping are unchanged and reused by new endpoints.
- **Additive except one coordinated break.** Existing endpoints keep their
  paths and shapes. The only intentional breaking change is the JSON-casing fix
  (Goal 4), which must update its named consumers in lockstep. Goal 5 adds counts
  as **separate** endpoints and leaves existing list endpoints unchanged (no
  list-envelope and no default-limit change), so it is not a break.
- **Do not regress the enrich contract** the frontend consumes (the existing
  vitest enrichment fixtures must still pass) or any existing consumer
  (`wa mlwh info`, `wa mlwhdiff serve`, `wa results serve`, the frontend).
- **Tests follow project conventions** (GoConvey; `sqlmock` + `modernc.org/
  sqlite`; both cache dialects via the existing matrix harness). Add: handler
  tests for the new endpoints; `RemoteClient`↔`Client` parity for the new
  `Queryer` members; a test that the OpenAPI document covers every endpoint and
  type; updated frontend tests for the casing change. Live-MLWH integration
  tests stay gated on `WA_MLWH_DSN`.

## Out of scope

- The **MCP server itself** and all natural-language / LLM logic — it lives in
  a separate repo and only consumes this API.
- **Adding authentication or TLS** to the MCP path, and fixing the secured-mode
  `RemoteClient` prefix/login limitation (Goal 7 documents but does not change
  it).
- The **sync engine and cache-schema design** of `.docs/mlwh-sync/spec.md`,
  changed only additively (new columns/indexes when a new search query needs
  them).
- `mlwhdiff`'s diff/watermark/hash algorithm.
- Long-read platform tables (`pac_bio_*`, `oseq_*`, `eseq_*`) remain
  designed-for but not implemented.

## Reference points in the current codebase

- API surface and discipline: `mlwh/registry.go` (`Registry`, the single
  source), `mlwh/server.go` (gin handler derivation, `mlwhServerFetchAllLimit`),
  `mlwh/queryer.go` (`Queryer` interface), `mlwh/remote.go` (`RemoteClient`,
  always sends explicit `limit`/`offset`), `mlwh/errors_http.go` (envelope +
  sentinels), `cmd/mlwh.go` (`wa mlwh serve` command, unauth-by-default wiring).
- Types to make consistent / describe: `mlwh/types.go` (incl. `TaggedID`,
  `SearchValues`, the `*Detail` aggregates, `EnrichmentResult`) and
  `mlwh/mlwh.go` (`Match`, `IdentifierKind` constants, `Library`, `Run`).
- Existing read/search patterns to mirror for Goal 1: `mlwh/resolver.go`,
  `mlwh/resolver_sample.go`, `mlwh/hierarchy.go` (the `Find*` and `Expand*`
  families), `mlwh/all_studies.go`.
- Schema + freshness: `mlwh/cache_schema.go`,
  `mlwh/cache_schema/{sqlite,mysql}/*.sql`, `mlwh/cache.go` (sync state /
  high-water), and the parity test.
- Consumers affected / to keep working: `cmd/mlwh_info.go`, `cmd/mlwhdiff.go`
  + `mlwhdiff/provider.go` (`providerFetchLimit`), `cmd/results.go`, and the
  frontend `frontend/app/(results)/actions.ts` (`validateIdentifier` →
  `identifierResultSchema`, study/library sample fetches that omit `limit`)
  plus its Zod schemas and `frontend/lib/studies-cache`.
- Governing prior specs: `.docs/mlwh-overhaul/spec.md`, `.docs/mlwh-sync/spec.md`,
  `.docs/mlwh/spec.md`, `.docs/proposal.md`.

## Notes (resolved decisions)

These are already decided by the requester and override looser wording above.

- **Auth/transport:** keep `wa mlwh serve` **unauthenticated plain HTTP for
  now**. Do not add auth or TLS in this feature; do not treat the absence of
  auth as a gap. Document the data-exposure posture (Goal 6/7). The MCP
  deployment is assumed same-cluster/internal.
- **OpenAPI:** an OpenAPI description **is in scope** (Goal 2), derived from the
  Registry/types single source of truth and served unauthenticated.

### Clarifications — round 1

- **Search matching strategy (Goal 1):** *The final matching mechanism is fixed
  in round 3 below, which is authoritative for how search works.* What still
  holds at this level: search is index-backed, the index is built and maintained
  by `wa mlwh sync`, and adding it requires new columns/indexes in **both**
  dialect schemas plus extending the cache-schema parser and two-dialect parity
  model (`cache_schema.go` / `schemaShape` / `compareCacheSchemaShapes`) to
  represent the sample full-text index. (Round 3 replaces the earlier "FTS for
  both entities / natural-language / tokenisation-ranking divergence" framing
  with substring matching — FTS5 trigram + MySQL ngram for samples, a plain
  `LIKE` scan for studies, and an exact set-equality parity contract.)
- **Pagination and counts (Goal 5):** deliver counts via **dedicated count
  endpoints / `Queryer` methods** that return `{count: N}`, each its own
  `Registry` entry and honouring the same `id_lims = 'SQSCP'` filter as the
  corresponding reads. **Existing list endpoints keep their bare-array bodies
  and current fetch-all (1,000,000) default unchanged** — no list-envelope, no
  forced default change, so the frontend fetch-all calls and `mlwhdiff` are
  untouched. New **search** endpoints get their own **default page size 100 and
  maximum 1000**.
- **OpenAPI generation (Goal 2):** generate from **enriched `Registry` metadata
  plus reflection**. Add `summary`/`description` and structured query-param
  specs to the `Endpoint` type; reflect over each endpoint's result type for the
  response schemas; serve the document at `GET /openapi.json` via a plain route.
  Per-field descriptions come from a struct-tag convention (e.g. `doc:"…"`) or a
  co-located descriptor map, and the same metadata feeds the Goal 6 human
  reference. A coverage test asserts every `Registry` entry and every `Queryer`
  method appears in the document, and the schemas reflect the post-Goal-4
  (snake_case) JSON.
- **Health and freshness (Goal 3):** `GET /health` is a **plain server route**
  (outside `Registry`/`Queryer`) returning a cheap `{status: "ok"}`. **Freshness
  is a first-class `Queryer` method + `Registry` entry** (`GET /freshness`)
  returning a typed result with, per mirrored sync table (`study`, `sample`,
  `iseq_flowcell`, `iseq_product_metrics`, `seq_product_irods_locations`), its
  `high_water` and `last_run` timestamps (UTC RFC3339, e.g.
  `2006-01-02T15:04:05Z`) and an `ever_synced` bool (false when the `sync_state`
  row is absent). Both endpoints appear in the OpenAPI document.
- **JSON casing fix (Goal 4):** add `snake_case` JSON tags to `Match`
  (`kind`, `canonical`, `sample`, `study`, `run`, `library`) and `TaggedID`
  (`kind`, `canonical`), and perform a **single atomic coordinated break**:
  update the frontend `identifierResultSchema` (`frontend/lib/contracts.ts`) to
  the new keys and **drop the PascalCase branch** so server and frontend ship
  together. The spec lists the exact before/after keys and every dependent TS
  type / `studies-cache` touch, and requires an audit for any other raw-JSON
  consumer of `/classify`, `/resolve/*`, or `/expand/:kind/:id` beyond
  `validateIdentifier` and the Go `RemoteClient` (which stays consistent
  automatically because it decodes into the same structs).

### Clarifications — round 2

- **Search result shape (Goal 1):** the search endpoints return the **existing
  `[]Study` / `[]Sample` types unchanged**, reusing the same models, registry
  result helpers, `RemoteClient` decoders, and the frontend's existing
  study/sample Zod schemas (consistent with every existing `Find*` endpoint).
  This returns full rows — including the governance study fields
  (`data_access_group`, `study_visibility`, `contains_human_dna`, `ega_*`) — on
  the unauthenticated search surface, which is acceptable under the recorded
  unauthenticated/internal posture (Goal 7) and must be noted in the
  data-exposure documentation (Goal 6).
- **Search routes and term transport (Goal 1):** the term is a **path
  parameter**, mirroring the existing finders: `GET /search/study/:term` and
  `GET /search/sample/:term`, with `Queryer` methods **`SearchStudies`** and
  **`SearchSamples`** (names chosen to avoid colliding with the existing
  `SearchValues` / `ExpandSearchValues` / `ExpandSampleSearchValues` concept).
  Pagination uses the existing `?limit`/`?offset` (default 100, max 1000 from
  round 1). No new query-param plumbing is added to the handler or
  `RemoteClient`; the term is URL-encoded like other path params.
- **Search index design (Goal 1):** *Superseded by round 3 — see there for the
  final matching mechanism (substring), the per-entity strategy, the field sets,
  and the parity contract.* One testing detail from this round still applies:
  because `sqlmock` cannot evaluate the MySQL full-text predicate, MySQL unit
  tests assert query construction while real matching is exercised for SQLite
  (FTS5) in unit tests and for MySQL only under `WA_MLWH_DSN`.
- **Count endpoints scope (Goal 5):** provide dedicated count endpoints for
  **both** the search surfaces (`GET /search/study/:term/count`,
  `GET /search/sample/:term/count`; methods `CountStudySearch`,
  `CountSampleSearch`) **and** the high-value existing list relationships (at
  least `GET /studies/count` and `GET /study/:id/samples/count`) so the MCP
  server can answer "how many …" without a search term. Each count is its own
  `Queryer` method + `Registry` entry returning `{count: N}` and honouring the
  same `id_lims = 'SQSCP'` filter as its corresponding read. The spec enumerates
  the exact set of list endpoints that gain a `/count` counterpart, following
  the 1:1 method↔endpoint rule.
- **OpenAPI document identity (Goal 2):** the served document is **OpenAPI
  3.1.0** with `info.title = "wa mlwh API"` and `info.version` tied to a package
  constant (reuse/extend the existing cache schema-version lineage or introduce
  a dedicated `mlwhAPIVersion`). The Goal-6 human-readable endpoint reference is
  **generated from the same enriched metadata** (not hand-written) so it cannot
  drift from the served spec.

### Clarifications — round 3

These refine and, where noted, **supersede** the full-text specifics in rounds 1
and 2. The searchable field sets are unchanged (study: `name`, `study_title`,
`programme`, `faculty_sponsor`; sample: `name`, `supplier_name`, `common_name`,
`donor_id`); only the matching mechanism and parity contract below are revised.

- **Match semantics = true substring ("contains"), ≥3 chars, index-backed.**
  This supersedes the round-1/round-2 whole-token / natural-language framing.
  Verified against the real cache backend (Oracle **MySQL 8.4.7**;
  `sample_mirror` ≈ 10.3M rows, `study_mirror` ≈ 8.2k rows; `ngram_token_size`
  = 2, `innodb_ft_min_token_size` = 3):
  - **Sample search** (large table): index-backed substring via the SQLite FTS5
    **`trigram`** tokenizer and a MySQL **`FULLTEXT … WITH PARSER ngram`** index
    over the sample search fields, each used to narrow candidates and then
    **confirmed with a case-insensitive `LIKE '%term%'` post-filter**. The
    post-filter guarantees exact substring semantics. No server reconfiguration:
    rely on the default `ngram_token_size = 2` plus the post-filter; do not
    require changing global MySQL FTS variables.
  - **Study search** (small table): a plain `LIKE '%term%'` scan across the
    study search fields (OR'd), **no FTS index**.
- **Parity is now exact set-equality (supersedes the round-2 "set-only with
  documented ranking divergence").** Because every backend applies the same
  `LIKE '%term%'` post-filter, the search endpoints return **identical row sets
  across dialects**; results carry **no relevance score** and are **ordered by
  id** for stable pagination. The schema-shape parity model must still represent
  the SQLite FTS5 virtual table vs the MySQL FULLTEXT index on `sample_mirror`
  (the schema-parser/`schemaShape`/`compareCacheSchemaShapes` extension flagged
  in round 1). The only permitted divergence is **accent handling**: MySQL
  `utf8mb4_0900_ai_ci` is accent-insensitive while SQLite trigram is
  accent-sensitive, so the cross-dialect set-equality test uses **ASCII
  fixtures** and the spec documents accent-folding as backend-dependent.
- **Search backend requirement — the server refuses to start on unsupported
  backends.** The whole `wa mlwh serve` API requires **SQLite or MySQL ≥ 8**
  (the MySQL `ngram` parser is absent on MariaDB and MySQL < 8). At startup the
  server checks the cache backend flavor/version (reuse the existing `VERSION()`
  logic at `mlwh/cache.go:1378-1398`) and, on **MariaDB or MySQL < 8**, **exits
  non-zero with a clear error and does not serve at all** (e.g. "wa mlwh serve
  requires SQLite or MySQL >= 8 for full-text search"). There is **no
  per-endpoint disablement and no degraded mode**: because an unsupported
  backend never serves, the `Registry`, route registration, OpenAPI document,
  `RemoteClient`, and the coverage test all stay **unconditional** — every entry
  is always present. Do **not** build a portable trigram-table fallback.
- **Short terms (Goal 1):** the minimum effective term length is **3**; a
  shorter term returns an **empty set** (HTTP 200, `[]`) and **count 0** on both
  backends (consistent with trigram/ngram needing ≥3 chars). Empty terms are
  unreachable because `/search/{study,sample}/:term` 404s on an empty segment.
- **Over-maximum `limit` (Goal 5):** a search request with `limit` > 1000 is
  **rejected with the existing `bad_request` 400 envelope** (not silently
  clamped).
