# Results Seqmeta Enrichment — Feature Prompt

## What we want

Given any sequencing-related name or identifier, we want to resolve it to as
much related information as possible, using the **smallest and fewest possible
upstream queries**, with appropriate caching, and behaving sensibly when the
upstream SAGA/MLWH backend is partially impaired.

Concretely, the use cases we must support end-to-end:

- **Study identifier** in (id_study_tmp, id_study_lims, study accession
  number): resolve to the full Study record **plus** the set of
  samples in the study **plus** the set of distinct libraries represented by
  those samples.
- **Sample identifier** in (sanger_sample_id, id_sample_lims, sample accession
  number): resolve to the full sample row **plus** its library information
  **plus** the full Study record of the study it belongs to.
- **Run identifier** (id_run): resolve to the set of samples on the run, and
  from there to their study(ies) and libraries as above.
- **Library identifier** (library_type string today; should be treated as a
  first-class entity where possible): resolve to the set of samples using that
  library and the studies they belong to.
- **Project name**: resolve to the SAGA Project, including its linked studies,
  samples, and users, so those can be traversed to full study/sample detail as
  above.

"Full metadata" means every field the SAGA/MLWH schema exposes for the matched
object — the existing `saga.Study`, `saga.MLWHSample`, `saga.Project`, and
`saga.IRODSFile` fields, plus any aggregated joins we compute (e.g.
`libraries_in_study`, `study_for_sample`).

## Why this is needed

`results-web` renders a `seqmeta_*` metadata value on a result set by calling
`GET /validate/{identifier}` on the seqmeta server, which calls
`seqmeta.Validate`. Today this works end-to-end **only** for study IDs and
study accession numbers. Every other identifier kind (sanger_sample_id,
id_sample_lims, sample accession, id_run, library_type) resolves via a
fall-through to `saga.MLWHClient.AllSamples()`, which returns HTTP 500 from
the production MLWH backend when called without a study filter. The frontend
shows "enrichment unavailable" for these identifiers even though the
identifier may be perfectly valid. We worked around this in seed data by
swapping `seqmeta_sampleid` for `seqmeta_studyid` with real study IDs
(`5993`, `5994`, `6591`), but the underlying capability is missing.

Even if `AllSamples()` were fixed upstream, the current design is wrong for
this use case: it downloads the entire MLWH samples table (hundreds of
thousands of rows) and linear-scans it per identifier. This does not scale
and burns cache capacity on data we do not need.

## Investigation findings (24 Apr 2026)

Confirmed by reading the current implementation against
`.docs/saga/spec.md`, `.docs/seqmeta/spec.md`, and
`.docs/results-web/spec.md`:

- **`saga.MLWHClient.AllSamples()` is broken against production MLWH.**
  `/integrations/mlwh/samples` with no filter returns HTTP 500. Only
  `AllSamplesForStudy(studyID)` (which passes a `study_id` filter) works.
  This is a SAGA/MLWH upstream constraint we must live with.
- **No targeted sample lookup in the saga package.** There is no
  `FindSample(identifier)`, no `GetSample(sangerID)`, no `SamplesByRunID`,
  no `SamplesByLibraryType`. Every non-study identifier path therefore
  degenerates to `AllSamples()` and the O(n) scan in `seqmeta.Validate`.
- **`seqmeta.Validate` cascade is 5/8 broken.** Of the eight
  `IdentifierType`s in `.docs/seqmeta/spec.md`, only `IdentifierStudyID`
  (via `GetStudy`), `IdentifierStudyAccession` (via `AllStudies`), and
  `IdentifierProjectName` (via `ListProjects`) resolve against real SAGA.
  `IdentifierSangerSampleID`, `IdentifierSampleLimsID`,
  `IdentifierSampleAccession`, `IdentifierRunID`, and
  `IdentifierLibraryType` all depend on `AllSamples()` and therefore
  currently 5xx out.
- **`MLWHSample` → `Study` hop is not straightforward.** `MLWHSample`
  carries only `IDStudyLims`; `MLWHClient.GetStudy` takes the study
  identifier used in `/integrations/mlwh/studies/{id}` (which today is
  satisfied by `id_study_tmp` string form or `id_study_lims`; this needs
  pinning down). We need a verified "sample → full study" lookup.
- **No library entity.** The only trace of "library" in saga is
  `MLWHSample.LibraryType` (a string). To offer a library view, we either
  synthesise it by aggregating across samples in a study, or rely on
  whatever SAGA may expose server-side.
- **Cascade is O(entire MLWH) per unknown identifier.** Even if
  `AllSamples()` worked, the linear scan would not scale as MLWH grows.
- **Caching is coarse.** `activecache` in saga caches raw GET bodies by
  URL. There is no identifier→resolved-object cache, no cross-request
  join cache, no TTL separate from the raw response cache. The frontend
  has an in-browser cookie cache keyed by identifier string, but the
  server side re-does every cascade on every distinct identifier per
  process.
- **Production SAGA/MLWH quirks we have already hit.**
  `GetStudy(non_study_id)` can return arbitrary 4xx codes, not just 404,
  so we treat any client-error as "not a study" and fall through
  (`seqmeta/validate.go` `isClientError`). That behaviour should be
  preserved and generalised.

## Desired outcome

A redesigned enrichment capability (implemented across the `saga` and
`seqmeta` packages, consumed by `results-web` through the same
`/validate/{identifier}` surface or a replacement) that:

1. Can classify **any** of the supported identifiers by making **at most a
   small bounded number of targeted SAGA calls** — never `AllSamples()`,
   never a full-table scan.
2. Returns, in a single response, the full object graph relevant to the
   identifier (sample → study + library; study → samples + libraries;
   run → samples + study; library → samples + studies; project →
   studies + samples + users) using the smallest upstream queries that
   cover each hop.
3. Is cached with sensible TTLs — resolved identifiers and their graph
   expansions should survive across requests and processes as far as is
   safe, with a clear invalidation strategy and short-circuit for known
   misses.
4. Degrades gracefully: if a hop is impaired upstream (e.g. the production
   `AllSamples` stays broken forever), we should surface the partial
   graph we could compute, with a clear machine-readable indication of
   which hops are missing, rather than turning the whole response into a
   5xx.
5. Distinguishes unambiguously between "identifier not known upstream"
   (→ 404) and "upstream impaired" (→ 5xx with actionable detail).
6. Keeps the existing `seqmeta` watermark / diff machinery intact — this
   work extends lookup/enrichment, it does not replace diff-based
   polling.
7. Is driven by acceptance tests that cover each identifier kind and each
   known upstream-failure mode (using `httptest.Server`-backed SAGA
   fakes), plus integration tests gated on `SAGA_TEST_API_TOKEN` that
   exercise real endpoints where safe.

Out of scope:

- Fixing the upstream SAGA/MLWH `/integrations/mlwh/samples` 500 itself.
- Any change to `results-web` beyond what is required to consume the new
  shape of `IdentifierResult` (that should still happen, but this spec
  is about `saga`/`seqmeta`).

## Inputs to the spec-writer

- This prompt.
- `.docs/saga/spec.md`, `.docs/seqmeta/spec.md`,
  `.docs/results-web/spec.md`, `.docs/results-rest/spec.md`.
- The bugfix notes in `.docs/bugfixes/260423-2.md`, specifically the
  "seqmeta_sampleid not showing additional info" item.
- The current implementation under `saga/`, `seqmeta/`, and the
  consumers in `frontend/lib/seqmeta-*.ts`.

## Notes

These are direct decisions captured during clarification. Treat them as
requirements.

- **Sample lookup strategy.** The `saga` package must add per-kind targeted
  sample lookups built on the MLWH samples endpoint's existing `filters`
  query parameter (the same mechanism `AllSamplesForStudy` already uses
  with `study_id`). Add `FindSamplesBy*` methods covering `sanger_id`,
  `id_sample_lims`, `id_run`, `library_type`, and `accession_number`. The
  spec must include an early-probe acceptance test, runnable against live
  SAGA under `SAGA_TEST_API_TOKEN`, that verifies each of these filters
  actually works upstream; if any filter turns out to be unsupported, the
  corresponding `IdentifierType` degrades to partial (see partial-graph
  handling) rather than scanning `AllSamples()`.
- **REST surface.** Keep `GET /validate/{identifier}` exactly as it is
  today (thin classifier returning `IdentifierResult` with a single
  matched object). Add a new `GET /enrich/{identifier}` route on the
  `seqmeta` server that returns the full enrichment graph. The frontend
  migrates its detail-view enrichment call from `/validate/` to
  `/enrich/`; the classification-only badge on list/search rows may keep
  using `/validate/`.
- **Library modelling.** A library is a `(library_type, id_study_lims)`
  tuple, scoped to a study. A bare `library_type` string input resolves
  to the set of libraries (one per study the type appears in), and from
  there to the samples that belong to each library. Study enrichment
  rolls up the distinct libraries present in the study's samples;
  sample enrichment names the single library the sample belongs to
  (its `library_type` + its study).
- **Caching.** Add a cross-process identifier cache to the `seqmeta`
  package, backed by a new SQLite table alongside the existing
  watermarks table. Cache keys are the identifier string. Store the
  full enrichment graph JSON plus resolved `IdentifierType`, fetched-at
  timestamp, and a negative-cache flag. TTLs: ~24 hours for successful
  resolutions, ~15 minutes for negative results (identifier not known)
  and for partial results (some hops failed upstream). Provide an
  explicit invalidation route (`DELETE /enrich/{identifier}`) for
  operational use. Also bust the cache entry for an identifier whenever
  an existing `seqmeta` diff endpoint mutates any row contributing to
  that identifier's graph. The existing `saga/activecache` URL-level
  cache stays as-is underneath.
- **Partial-graph response shape and status codes.** When the primary
  hop (classification) succeeds, return HTTP 200 with a body of the
  form `{ "identifier": "...", "type": "...", "graph": { ... populated
hops ... }, "partial": true|false, "missing": [{ "hop":
"<hop_name>", "reason": "<machine_code>", "status": <upstream_http>
}] }`. Use HTTP 404 only when the identifier cannot be classified on
  any working hop. Use HTTP 502 only when every attempted hop failed
  with a transient/5xx error (no primary classification possible). The
  spec must enumerate the hop names and failure `reason` codes.
- **Frontend contract updates are in scope.** Update
  `frontend/lib/contracts.ts`, `frontend/lib/seqmeta-enrichment.ts`,
  and the result-detail enrichment badge so they consume the new
  `/enrich/{identifier}` response, display partial graphs sensibly
  (showing what was retrieved plus a subdued "some details
  unavailable" note listing the missing hops), and distinguish 404
  ("unknown identifier") from 502 ("upstream impaired"). Do not touch
  unrelated frontend concerns.
- **Integration-test identifiers.** Pin into the spec's integration-test
  matrix: study IDs `5993`, `5994`, `6591`; sanger/sample IDs
  `5835STDY8046554`, `WTSI_wEMB10524782`, `6591STDY10735392`; library
  types `RNA PolyA` and `Agilent Pulldown`; run IDs `34134` and
  `40121`. Tag each as "expected end-to-end" or "expected partial
  (upstream impaired)" based on which hops work today. Tests are
  gated on `SAGA_TEST_API_TOKEN` and must not fail CI when the token
  is absent.
- **Graph envelope shape.** The enrichment response body uses a flat
  typed struct with optional pointer / slice fields (e.g.
  `Study *saga.Study`, `Sample *saga.MLWHSample`, `Samples []saga.MLWHSample`,
  `Studies []saga.Study`, `Libraries []Library`, `Project *saga.Project`,
  `Users []saga.ProjectUser`) under `graph`. Drop the legacy `Object`
  field — `/enrich/` responses do not include it. `/validate/{identifier}`
  keeps its existing `IdentifierResult.Object` contract unchanged.
- **Provider interface.** Add the new `FindSamplesBy*` methods (and any
  other new hops) directly to the existing `seqmeta.SAGAProvider`
  interface and the `ClientAdapter`, and update all existing mocks in
  `seqmeta/*_test.go`. No new provider interface.
- **Cache process model.** Preserve the existing single-writer
  assumption: one `seqmeta` process owns the SQLite file. Document this
  explicitly. The enrichment cache table lives in the same SQLite
  database as the watermarks table, reuses `Store.WithLock`, and does
  not add cross-process coordination.
- **iRODS files in graph.** The enrichment graph does not include iRODS
  files inline. Files remain available through existing routes (the
  `/diff/sample/{id}` endpoint and `saga.GetSampleFiles`) and are
  fetched on demand by the frontend.
- **DELETE auth model.** `DELETE /enrich/{identifier}` shares the same
  trust boundary as the existing seqmeta routes: no additional authn
  or authz. The spec should note this explicitly and rely on the
  network policy / reverse-proxy rules governing the seqmeta server.
- **Spec scope.** Keep `.docs/results-seqmeta/spec.md` self-contained.
  It is the authoritative source for the new saga lookup methods, the
  new seqmeta routes and cache table, and the frontend contract
  changes. Cross-reference `.docs/saga/spec.md`,
  `.docs/seqmeta/spec.md`, and `.docs/results-web/spec.md` where
  useful but do not edit those specs as part of this work.
- **TTL configuration.** Enrichment-cache TTL values are set via
  functional options on the `seqmeta` server/store constructor (for
  example `seqmeta.WithEnrichTTL(success, negative time.Duration)`),
  following the existing functional-option style in `saga` and
  `seqmeta`. Do not read environment variables from inside the
  package. Defaults remain ~24 h for success and ~15 min for
  negative/partial results. Tests configure short TTLs via the option.
- **Expired-cache behaviour.** Never serve stale. When a cache entry
  is expired, always re-fetch. If the re-fetch fails, respond per the
  normal 200-with-partial / 404 / 502 contract; do not carry stale
  data through.
- **Library-type fan-out cap.** For identifiers that classify as a
  bare `library_type`, cap the `graph.samples` slice at 1000 entries.
  When the cap triggers, keep `graph.libraries` and `graph.studies`
  complete and set `partial: true` with a `missing` entry of
  `{ "hop": "samples", "reason": "samples_truncated", "status": 200 }`.
  The cap is a named constant in the `seqmeta` package (not
  configurable) and documented in the spec.
