# Feature: MLWH overhaul — `mlwh` as the single current-state service, `mlwhdiff` as a narrow change-tracking layer

## Background

`.docs/proposal.md` originally proposed a `saga` package to query MLWH,
wrapped by a `seqmeta` package that did MLWH "diffing" (change tracking).
`saga` was replaced by a direct `mlwh` package (`.docs/mlwh/spec.md`,
improved by `.docs/mlwh-sync/spec.md` and subsequent bugfixes). `mlwh`
now keeps a local synced SQLite/MySQL cache of the relevant MLWH tables
and answers identifier resolution, hierarchy walks, and search expansion
entirely from that cache (cache-only reads, no live-MLWH fallback). It
also has a small in-process memo (the 5-minute `ExpandIdentifier` cache
in `mlwh/hierarchy.go`).

Two facts now drive this overhaul:

1. We do **not** yet have a broad need for `seqmeta`'s change-tracking
   ("what's new since last check") diffing. But the `results` package and
   the frontend **do** need plain "current state" MLWH queries.
2. There is a desire for **external systems** (outside this repo) to be
   able to ask quick, cached, current-state MLWH questions over a simple
   REST API, without knowing the underlying MLWH SQL schema and without
   suffering its size/slowness.

Today `seqmeta` is in the way of both. Its HTTP server exposes
current-state routes that are really `mlwh` queries with an extra hop and
an extra cache bolted on top of the cache `mlwh` already maintains:

- `GET /validate/*` — identifier classification (== `mlwh.ClassifyIdentifier`).
- `GET /enrich/*` and `DELETE /enrich/*` — graph enrichment built from
  `mlwh` detail aggregates, plus a TTL cache in `seqmeta/store.go`.
- `GET /studies` — list studies (== `mlwh.AllStudies`).
- `GET /study/{id}/samples` — samples for a study with library filters
  (== `mlwh.SamplesForStudy` + the `SamplesForLibrary*` family).

Only `GET /diff/study/{id}` and `GET /diff/sample/{id}` are genuine
`seqmeta` value (watermark/tombstone change detection). Meanwhile:

- `results/validate.go`'s `SeqmetaValidator` calls `seqmeta`'s
  `/validate/*` over HTTP purely to classify identifiers, even though the
  `results` server already imports `mlwh` directly and could call
  `mlwh.ClassifyIdentifier`.
- `results/server.go`'s `SeqmetaSampleResolver`
  (`SamplesAndLanesForStudy`, `SamplesAndLanesForLibrary`) calls
  `seqmeta`'s `/study/{id}/samples` over HTTP for data `mlwh`
  (`SamplesForStudy`, `LanesForSample`, the `SamplesForLibrary*` family)
  already serves.
- The frontend Server Actions in `frontend/app/(results)/actions.ts`
  (`validateIdentifier`, `enrichIdentifier`, `enrichIdentifiers`,
  `fetchStudySamples`, `fetchStudyLibrarySamples`) call `seqmeta`
  current-state routes via `WA_SEQMETA_BACKEND_URL`.
- `results/mlwh_search_resolver.go` and `results/registration_lookup.go`
  already call `mlwh` directly (the correct pattern). `cmd/results.go`
  resolves `--run/--study/--sample/--library` via `mlwh.Resolve*` on the
  server side (also correct).

This overhaul removes the confusion: `mlwh` becomes the single source of
current-state MLWH answers (as a Go package **and** as a REST server),
and `seqmeta` is renamed and narrowed to be unmistakably a thin
change-tracking layer on top of `mlwh`.

## Goals

### 1. Eliminate `seqmeta` misuse for current-state queries

- Every "current state" MLWH question is answered by `mlwh` — either by
  importing the `mlwh` package and calling its methods directly (Go
  callers) or by calling the new `mlwh` REST server. `seqmeta` (renamed
  `mlwhdiff`, below) must never be the path for current-state queries.
- Remove the current-state routes from the renamed-`seqmeta` server
  entirely (no compatibility shims): `/validate/*`, `/enrich/*`
  (GET and DELETE), `/studies`, `/study/{id}/samples`. These become
  `mlwh` server endpoints (Goal 3).
- Repoint `results`:
    - Replace `results/validate.go`'s `SeqmetaValidator` with a direct
      `mlwh` classification call (the `results` server already holds an
      `mlwh` query handle; use it).
    - Replace `results/server.go`'s `SeqmetaSampleResolver` with direct
      `mlwh` calls (`SamplesForStudy`, `LanesForSample`,
      `SamplesForLibrary` / `SamplesForLibraryID` /
      `SamplesForLibraryLimsID`).
    - `results/mlwh_search_resolver.go`, `results/registration_lookup.go`
      and the `cmd/results.go` register path already use `mlwh` directly;
      keep them, but route them through the shared `mlwh` query interface
      defined in Goal 3 so the same code works against a local or remote
      `mlwh`.
- Repoint the frontend Server Actions to the `mlwh` server endpoints and
  retire the current-state `seqmeta` calls.

### 2. Rename `seqmeta` → `mlwhdiff` and narrow its scope

- Rename the Go package `seqmeta/` → `mlwhdiff/`, the CLI
  `wa seqmeta …` → `wa mlwhdiff …`, the default store file
  `seqmeta.db` → `mlwhdiff.db`, the env var `WA_SEQMETA_BACKEND_URL` →
  `WA_MLWHDIFF_BACKEND_URL` (or drop it if no consumer needs it after the
  repointing in Goal 1), and the corresponding frontend `seqmeta-*`
  files/components/contracts that survive (see Goal 5). No "seqmeta"
  string survives in code, env vars, docs, or user-facing text except
  where it is genuinely about change tracking under the new name.
- `mlwhdiff` keeps **only** change-tracking concerns:
    - the watermark/tombstone store (`store.go`, unchanged in shape, hashes
      still computed over `mlwh`-derived records),
    - the diff machinery (`diff.go`),
    - the `/diff/study/{id}` and `/diff/sample/{id}` routes,
    - the `wa mlwhdiff diff` and `wa mlwhdiff serve` commands.
  It loses `enrich`, `validate`, and the current-state list/sample
  routes and their backing code, which move to `mlwh`.
- `mlwhdiff`'s public API and naming must make it obvious it is a narrow
  change-tracking system layered on `mlwh`, which already does its own
  caching. `mlwhdiff` must not define its own MLWH domain shapes; it
  carries `mlwh` types. Its dependency on `mlwh` is expressed through the
  shared query interface from Goal 3, so `mlwhdiff` can run against an
  in-process `mlwh` or a remote `mlwh` server with no code change.
- `seqmeta/provider.go` and `seqmeta/client_adapter.go` shrink to exactly
  the `mlwh` surface that diffing needs (e.g. `AllStudies`,
  `SamplesForStudy`, `IRODSPathsForSample`); the duplicated resolver /
  classifier / enrichment methods are removed.

### 3. An `mlwh` REST server with a 1:1 method↔endpoint correlation and shared in-memory caching

- Add a new `wa mlwh serve` command alongside `wa mlwh sync` and
  `wa mlwh info`. It is standalone and independently deployable (chi
  router, consistent with the project stack and with `wa results serve`).
  Its clients are: external systems, `mlwhdiff`, `results`, the CLI, and
  the frontend.
- **Every public `mlwh` query method maps to exactly one REST endpoint,
  and the correspondence is mechanical and documented.** A reader (or an
  LLM) must be able to look at a public method and know its endpoint, and
  vice-versa, without guessing. Provide the full method↔endpoint table in
  the spec. The current-state routes removed from `seqmeta` reappear here
  as the natural endpoints (`ClassifyIdentifier`, the enrich graph,
  `AllStudies`, `SamplesForStudy`, etc.).
- Define a single Go interface (e.g. `mlwh.Queryer`) covering the public
  query surface. It is implemented by:
    - `*mlwh.Client` — the existing local, cache-backed implementation
      (Go callers who want no HTTP hop use this directly), and
    - a new `mlwh` HTTP client (e.g. `*mlwh.RemoteClient`) that talks to
      `wa mlwh serve` and implements the **same** interface, the same
      types, and the same error sentinels (`ErrNotFound`,
      `ErrCacheNeverSynced`, `ErrAmbiguous`, `ErrUnsupportedIdentifier`,
      `ErrUpstreamImpaired`) round-tripped over HTTP via status codes and
      a typed error envelope.
  `results`, `mlwhdiff`, and the CLI depend on `mlwh.Queryer`, not on a
  concrete type, so each can be wired to a local `Client` or a remote
  server by configuration alone. This interface IS the "direct
  correlation between REST endpoints and public methods" made concrete.
- **Caching lives inside the `mlwh` package layer, never in the HTTP
  handlers**, so direct Go callers and the server benefit identically.
  The server must not add a caching layer that in-process callers would
  miss. Today the only in-process cache is the 5-minute
  `ExpandIdentifier` memo. The spec must, per query, justify whether any
  in-memory cache adds value over the already-optimised synced cache
  schema, and add one only where it measurably helps; the default is to
  rely on the synced schema and its indexes. A `RemoteClient` MAY add its
  own optional client-side cache, but the authoritative server-side
  caching belongs to `Client`.
- The server is read-only with respect to MLWH: it serves from the synced
  cache exactly as every other read path does, and does not trigger sync.
  Sync stays `wa mlwh sync` (manual/cron) plus the existing
  `--mlwh-sync-interval` option on long-running servers. The server
  surfaces `ErrCacheNeverSynced` as an actionable hint.
- Server auth/transport conventions follow `wa results serve` (the spec
  should state the concrete approach).

### 4. Make extending `mlwh` trivial: "new question" → "endpoint at web speed"

Optimise the path from "I want to ask this new question about something
in MLWH" to "there is a public method and a REST endpoint that answers it
at web-responsive speed", so a short LLM-driven change can add a query
end to end. The architecture must make the steps few, obvious, and
copy-paste shaped:

1. If the synced cache schema does not already carry the needed columns,
   add them — and the index that serves the new query — to **both**
   dialect schemas (`mlwh/cache_schema/{sqlite,mysql}/*.sql`) and update
   the parity test, following the read-path-audit discipline of
   `.docs/mlwh-sync/spec.md` (every served column traceable to an indexed
   read path).
2. Add one cache-only query method on `*mlwh.Client`, following the
   established method/query pattern.
3. Add the method to the shared `mlwh.Queryer` interface (so
   `RemoteClient`, mocks, and consumers pick it up).
4. Register one REST route.

Steps 3–4 must be near-mechanical: provide shared scaffolding (a single
declarative place that lists each query's method name, route, request
shape, and response shape) so the server handler and the `RemoteClient`
method derive from one source rather than being hand-written three times
(handler + client + interface). Avoid bespoke per-endpoint marshalling
boilerplate. "Web-responsive speed" is a hard requirement: every endpoint
must be backed by an indexed cache query; a new question that needs a new
index must add it (step 1) rather than scanning. Ship a documented
"add a new MLWH query" checklist (in `DEVELOPING.md` and/or a package doc
comment) that an LLM can follow start to finish.

### 5. Update existing MLWH consumers to the clarified `mlwh`

- `results` server: replace `SeqmetaValidator` and `SeqmetaSampleResolver`
  with direct `mlwh.Queryer` calls; keep the existing direct `mlwh`
  register/search paths but route them through `mlwh.Queryer`.
- CLI: `wa results serve` and `wa mlwhdiff serve` wire an `mlwh.Queryer`
  — a local `mlwh.Client` by default, or a `RemoteClient` when an `mlwh`
  server URL is configured.
- Frontend: Server Actions hit the `mlwh` server's endpoints for
  validate / enrich / studies / study-samples; remove the current-state
  `seqmeta` calls; rename surviving `seqmeta-*` files, components, and Zod
  contracts (`frontend/lib/seqmeta-cache-core.ts`,
  `frontend/lib/seqmeta-enrichment.ts`,
  `frontend/components/seqmeta-*`, the `seqmeta_*`-prefixed metadata
  handling in `frontend/app/(results)/page.tsx`, and related fixtures) to
  reflect that current state comes from `mlwh` while only change tracking
  comes from `mlwhdiff`.
- Remove the now-dead `seqmeta`/`mlwhdiff` provider methods that merely
  duplicated `mlwh`.

## Constraints

- The enrichment graph contract the frontend consumes must be preserved
  field-for-field (the existing vitest enrichment fixtures must continue
  to pass after the move), now served by the `mlwh` server and built from
  `mlwh`'s existing `SampleDetail` / `StudyDetail` / `RunDetail` /
  `LibraryDetail` aggregates.
- `mlwh` reads stay cache-only (per `.docs/mlwh-sync/spec.md`): no live
  MLWH fallback on the read/server paths. The server inherits
  `ErrCacheNeverSynced` semantics.
- Passwords never appear in a DSN, a CLI flag, or a process command line
  (existing rule). The `mlwh` server auth is consistent with
  `wa results serve`.
- No backwards-compatibility shims for the removed `seqmeta`
  current-state routes; delete them outright.
- Tests follow project conventions (GoConvey; `sqlmock` + `modernc.org/
  sqlite` for `mlwh`; both cache dialects via the existing matrix
  harness). Add: `mlwh` REST server handler tests; a `RemoteClient`↔
  `Client` parity test proving both implementations of `mlwh.Queryer`
  return identical results against the same cache and round-trip the
  error sentinels; updated `results` tests with the `seqmeta` HTTP fakes
  removed; updated frontend vitest suites pointing at `mlwh` endpoints.
  Live-MLWH integration tests stay gated on `WA_MLWH_DSN`.

## Out of scope

- The sync engine and cache-schema design of `.docs/mlwh-sync/spec.md`
  are unchanged except additively: new columns/indexes only when a new
  query needs them.
- `mlwhdiff`'s diff/watermark/hash algorithm itself is unchanged; only
  its upstream (now the shared `mlwh` query interface) and its narrowed
  scope change.
- Long-read platform tables (`pac_bio_*`, `oseq_*`, `eseq_*`) remain
  designed-for but not implemented.
- No new current-state questions beyond those needed to repoint the
  existing consumers — but the extension mechanism of Goal 4 must exist
  and be demonstrated by at least the moved endpoints.

## Reference points in the current codebase

- `mlwh` public API: `mlwh/mlwh.go`, `mlwh/types.go`, `mlwh/config.go`
  (`Open`, `OpenCacheOnly`, `Config`, `CacheConfig`), `mlwh/cache.go`,
  `mlwh/resolver.go`, `mlwh/resolver_sample.go`, `mlwh/hierarchy.go`
  (hierarchy reads, `Find*`, `Expand*`, and the 5-minute
  `ExpandIdentifier` in-memory memo), `mlwh/all_studies.go`,
  `mlwh/sync.go`. No HTTP server exists in `mlwh` today.
- `seqmeta` → `mlwhdiff`: `seqmeta/server.go` (route list above),
  `seqmeta/diff.go`, `seqmeta/enrich.go`, `seqmeta/validate.go`,
  `seqmeta/store.go`, `seqmeta/provider.go`, `seqmeta/client_adapter.go`,
  `seqmeta/types.go`; `cmd/seqmeta.go` (`diff`, `validate`, `serve`,
  flags `--db`, `--mlwh-cache`).
- `results` consumers: `results/validate.go` (`SeqmetaValidator`),
  `results/server.go` (`SeqmetaSampleResolver`,
  `SamplesAndLanesForStudy`, `SamplesAndLanesForLibrary`),
  `results/mlwh_search_resolver.go`, `results/registration_lookup.go`,
  `cmd/results.go` (register lookups, `serve` wiring,
  `WA_SEQMETA_BACKEND_URL`).
- Frontend: `frontend/app/(results)/actions.ts`
  (`validateIdentifier`, `enrichIdentifier`, `enrichIdentifiers`,
  `fetchStudySamples`, `fetchStudyLibrarySamples`),
  `frontend/app/(results)/page.tsx` (`seqmeta_*` metadata handling),
  `frontend/lib/seqmeta-cache-core.ts`,
  `frontend/lib/seqmeta-enrichment.ts`,
  `frontend/components/seqmeta-*`.
- Prior specs: `.docs/proposal.md`, `.docs/mlwh/spec.md`,
  `.docs/mlwh/prompt.md`, `.docs/mlwh-sync/spec.md`.
