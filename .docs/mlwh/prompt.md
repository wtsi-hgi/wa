# Feature: Direct MLWH access (replacing Saga) for sequencing metadata

## Why we are deleting the `saga` package

The `saga` Go package and the supporting `seqmeta` flows built on top of it
do not work for the lookups `wa results` actually needs. Concrete bug:
`wa results register --sample 7607STDY14643771` (a real sample's Sanger
name) fails with `"no matching sample found"`. The same physical sample
also has a Sanger ID `9575305`, supplier name `Hek_R1`, sample UUID
`b7daafb8-c59f-11ee-8fba-024224dd57f4`, donor ID `7607STDY14643771`.
None of these inputs resolve correctly, and `--sample SQSCP` (the LIMS
constant) is silently mismatched to an unrelated sample
(`4861STDY7387184`). The same class of failure applies to `--run`,
`--study`, and `--library`.

Root causes, verified against the live Saga API at
`https://saga.cellgeni.sanger.ac.uk/api`:

- `/api/integrations/mlwh/samples` only supports four filters
  (`sample_id` as a _prefix_ match against `sanger_id` OR `supplier_name`,
  `study_id`, `run_id`, `library_type` as a list). It does **not** filter
  on `id_sample_lims`, `uuid_sample_lims`, `donor_id`, `accession_number`,
  `sanger_sample_id`, or any other column.
- `/api/integrations/irods/samples` returns 0 items even unfiltered for
  the data we care about, and `/api/integrations/irods/samples/{sanger_id}`
  returns null for valid Sanger IDs that exist in MLWH.
- `/api/samples/MLWH/{id}` returns HTTP 500.
- The `saga.MLWHClient.AllSamples()` path returns HTTP 500 and is the
  only fallback for every non-study identifier in the current
  `seqmeta.Validate` cascade. Five of the eight `IdentifierType`s are
  therefore broken end-to-end.
- Today's `cmd/saga.go` `inspector` "resolves" identifiers by listing the
  iRODS catalogue and **scoring** entries on substring overlap, which is
  why `--sample SQSCP` returns a wrong sample and why nothing else
  resolves.

The Saga API is therefore not a fit. The MLWH database it wraps **does**
hold every column we need, indexed for direct lookup. We will talk to
MLWH directly and delete the Saga client entirely.

## Scope of removal

Delete with no backwards compatibility, no compatibility shims, no
mention of the old design in user-facing text, env vars, or code
comments:

- The `saga/` Go package and all its tests.
- `cmd/saga.go`, `cmd/saga_test.go`, and the `inspector`
  scoring helpers that the old `--run/--study/--sample/--library` paths
  call into.
- All `SAGA_*` env vars (`SAGA_API_TOKEN`, `SAGA_API_BASE_URL`,
  `SAGA_TEST_API_TOKEN`) from `.env.development`, `.env.production`,
  `.env.test`, `.env.development.local` documentation, `Makefile`,
  `run-dev.sh`, README, DEVELOPING, frontend tests, and CI configuration.
- The `--token` and `--base-url` Saga flags on
  `wa seqmeta diff|validate|serve` and any other Saga-typed CLI flags.
- Any "Saga"-typed names, error messages, comments, doc strings, and
  references throughout the Go and TypeScript code (e.g. "via Saga",
  "Saga is required", `requestedSagaToken`, `seqmeta.SAGAProvider`,
  the `saga.Client` field on the seqmeta server, etc.).
- Project entities everywhere: the `HopProject` / `HopUsers` /
  `IdentifierProjectName` Go symbols, the `projectSchema` /
  `projectUserSchema` schemas and the `project` / `users` fields on
  `enrichmentResultSchema` in `frontend/lib/contracts.ts`, the
  "Project" and "Project users" rows in
  `frontend/components/seqmeta-badge.tsx`, and every vitest fixture
  or test that references projects.
- The `.docs/saga/` and `.docs/results-seqmeta/` historical specs and
  their phase plans become obsolete; this spec supersedes them. Move
  what's still useful from `.docs/seqmeta/` into the new spec instead
  of cross-referencing it.

The rest of `seqmeta/` (the SQLite watermark/diff machinery, the
`/diff/*` HTTP routes, the validate/enrich responses consumed by the
frontend) **is kept**, but its upstream is changed from "the Saga
client" to "the new `mlwh` package". Where seqmeta carried Saga shapes
(`saga.Study`, `saga.MLWHSample`, `saga.IRODSFile`, `saga.Project`),
it now carries shapes defined by the `mlwh` package. The HTTP contract
exposed to the frontend (`/validate/{id}`, `/diff/study/{id}`,
`/diff/sample/{id}`, `/enrich/{id}`) is preserved so the existing
frontend (`seqmeta-badge`, `result-metadata*`, `seqmeta-enrichment`,
`seqmeta-cache*`, etc.) keeps working. Field-name parity must hold
for everything except project entities (removed) and iRODS file
metadata (tightened — see _seqmeta package_ below).

## What we want

A new Go package `mlwh/` that is the _single_ source of sequencing
metadata for the `wa` codebase, with `seqmeta/` rebuilt on top of it.
The combined system must be able to answer every lookup the existing
`saga`/`seqmeta`/`results-seqmeta` specs called for, plus the
hierarchical search and JIT-expansion needs proven necessary by
`.docs/bugfixes/260501-3.md`, `260501-4.md`, `260501-5.md`,
`260503-1.md`, and `260506-1.md`. Specifically:

### Identifier resolution (used by `wa results register --run/--study/--sample/--library`)

For every input form in the bug report, `wa results register --sample X`
must either resolve to the canonical Sanger ID and store it as
`seqmeta_sampleid`, or fail with a precise error that names the input
and the dimension. The five inputs from the bug — `7607STDY14643771`
(Sanger sample name), `Hek_R1` (supplier name), `9575305`
(`id_sample_lims`), `b7daafb8-c59f-11ee-8fba-024224dd57f4` (sample
UUID), and `7607STDY14643771` (donor ID) — must all resolve to the
same canonical sample. `SQSCP` (the LIMS constant) must be **rejected**,
not silently mismatched. Equivalent rules apply to `--study` (LIMS ID,
accession, study UUID, name/title), `--run` (numeric `id_run` only),
and `--library` (canonical `pipeline_id_lims` exact match).

### Hierarchical and graph queries (used by `seqmeta` enrich/diff/validate)

The combined `mlwh` + `seqmeta` system must serve these queries
efficiently and with caching:

- Sample → its full record + parent study + library/libraries it was
  prepped into + run(s)/lane(s) it was sequenced on + iRODS file paths.
- Study → list of samples + distinct libraries used + (lazily) runs.
- Run → samples sequenced on it + the studies they belong to + libraries.
- Library type → samples that used it + their studies + their runs.
- The "Libraries" sub-section of seqmeta details for a study must
  enumerate distinct libraries with their sample counts (regression
  from `260501-4.md`).
- A click on a library in seqmeta details must JIT-load the samples for
  that library scoped to the parent study (regression from
  `260501-4.md` / `260503-1.md`).
- Hierarchical search (`260501-5.md`, `260503-1.md`): a search for
  `study=6568` must match result sets tagged with the study **or** with
  any of its samples or lanes; `library=…` must expand to its samples
  and lanes; `sample=…` must expand to its lanes. The combined
  full-stack query must still complete in well under a second.

### Identifier classification (preserve `seqmeta /validate` behaviour)

Given an arbitrary string, decide whether it is a study ID, study
accession, study UUID, study name match, Sanger sample ID,
`id_sample_lims`, sample accession, sample UUID, supplier name,
donor ID, run ID, or library type, **using one indexed query per
candidate column at most**. Return the type and the matched canonical
record. Distinguish "not found" (404) from "upstream impaired" (5xx).
Project lookup is dropped; the validator must not pretend project
names exist.

## What this spec must deliver

### A new `mlwh/` Go package

- `database/sql` connection to the read-only MLWH MySQL replica using
  `github.com/go-sql-driver/mysql`. The DSN is supplied by a single
  passwordless env var `WA_MLWH_DSN` (e.g.
  `mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse`); the password comes
  from `WA_MLWH_PASSWORD`. This mirrors the existing
  `WA_RESULTS_DB_PATH` / `WA_RESULTS_DB_PASSWORD` pattern documented
  in README and DEVELOPING. The password must never appear on a
  process command line, on a CLI flag, or inside `WA_MLWH_DSN` itself
  (rejected at startup), per `260505-2.md`. `.env.*` files and docs
  are updated accordingly.
- A small `Querier` interface so tests can swap in `sqlmock` (pattern
  cribbed from `~/src/cellgeni/gst/db`).
- Domain types modelled on what MLWH actually stores, scoped to
  `id_lims = 'SQSCP'` everywhere unless explicitly broadened:
  `Sample`, `Study`, `Library`, `Run`, `Lane`, `IRODSPath`, plus a
  graph-shaped `SampleDetail` / `StudyDetail` / `RunDetail` /
  `LibraryDetail` aggregate that mirrors the seqmeta enrichment
  contract one-for-one.
- Resolver entry points returning a typed
  `Match{Kind, Canonical, Record}` or a typed sentinel
  (`ErrNotFound`, `ErrAmbiguous`, `ErrUnsupportedIdentifier`):
    - `ResolveSample(ctx, raw)` — runs the following fixed cascade
      and returns the **first column's hit**, stopping immediately
      (the resolver does not look at later columns to detect
      cross-column conflicts):
      `uuid_sample_lims` →
      `id_sample_lims` (with `id_lims='SQSCP'`) →
      `name` (Sanger name) →
      `sanger_sample_id` →
      `supplier_name` →
      `accession_number` →
      `donor_id` (cache-backed; cold-cache rules below).
      Returns the canonical Sanger ID (`sample.name`) and the full
      sample row. `donor_id` is the only step served from the local
      cache because MLWH has no usable index for it.
    - `ResolveStudy(ctx, raw)` — tries `uuid_study_lims`,
      `id_study_lims`, `accession_number`, `name`. Ambiguous text
      matches must error; case-insensitive match modes must be opt-in
      via a flag on the resolver, not the default.
    - `ResolveRun(ctx, raw)` — accepts a numeric `id_run`; verifies it
      has at least one row in `iseq_product_metrics`.
    - `ResolveLibrary(ctx, raw)` — `pipeline_id_lims` exact match
      (canonical), backed by the local cache because
      `pipeline_id_lims` is **not** indexed in MLWH; cold-cache rules
      apply as for `donor_id`.
    - `ClassifyIdentifier(ctx, raw)` — used by seqmeta `Validate`.
      Dispatches by input shape first: UUID-shaped strings only query
      UUID columns; pure-integer strings only query integer-keyed
      columns (`id_run`, `id_sample_lims`, `id_study_lims`); other
      strings only query text columns. Within whichever shape's
      candidate columns remain, the same fixed priority applies as
      `ResolveSample` / `ResolveStudy` (study identifiers before
      sample identifiers before run before library) and the first hit
      wins. It does not return `ErrAmbiguous` on cross-column matches.
- LIMS-provider constants (e.g. `SQSCP`, `GCLP`) are rejected via an
  explicit, hard-coded rejection set in `mlwh/`. Inputs in this set
  short-circuit any resolver call with a typed
  `ErrUnsupportedIdentifier` whose message says the value looks like a
  LIMS provider constant rather than a sample identifier. The set is
  small, reviewed in code, and is not derived dynamically from MLWH.
- Hierarchy expansion methods:
  `SamplesForStudy`, `SamplesForRun`, `SamplesForLibrary`,
  `LibrariesForStudy`, `RunsForStudy`, `LanesForSample`,
  `IRODSPathsForSample`, `IRODSPathsForStudy`, `StudyForSample`,
  each returning the smallest record set the consumer needs, and
  each backed by an indexed query or by the local cache where MLWH
  has no usable index (see _Local cache database_ below).
- A single hierarchical-search entry point
  `ExpandIdentifier(ctx, kind, canonical) → []TaggedID`. The results
  search layer calls it once per filter token and assembles the union
  of the returned tagged IDs against the results store. The
  per-dimension helpers above remain available for direct use by
  `seqmeta` enrichment.
- Strict rules: every method takes a `context.Context`; queries use
  parameterised statements (no string interpolation); every method
  bounds the result set or paginates explicitly; every method that
  could in principle return many rows accepts a limit and an offset.

### Schema notes the implementation must respect

Verified by `EXPLAIN` against the live MLWH replica
(`mlwh-db-ro:3435`, `mlwarehouse`):

- `sample` is a 10.3M-row table; 99.9% of rows are `id_lims='SQSCP'`.
  Indexed columns relevant to lookup: `id_sample_lims` (composite with
  `id_lims`), `uuid_sample_lims`, `name`, `supplier_name`,
  `sanger_sample_id`, `accession_number`. **Not indexed:** `donor_id`,
  `description`, `cohort`, etc.
- `study` is small (8k rows) but still has indexes on `id_study_lims`
  (composite with `id_lims`), `uuid_study_lims`, `accession_number`,
  `name`.
- `iseq_flowcell` has indexes on `id_sample_tmp` (FK),
  `id_study_tmp` (FK), `id_library_lims`, `flowcell_barcode`,
  `id_flowcell_lims`. **Not indexed:** `pipeline_id_lims` (the column
  the API exposes as `library_type`). 117 distinct values across
  ~8M rows.
- `iseq_product_metrics` is indexed on `id_iseq_flowcell_tmp` (FK) and
  on a composite that allows efficient `id_run` lookup. The
  `(id_run, position)` composite serves run/lane queries.
- `seq_product_irods_locations` joins on `id_product = id_iseq_product`
  and is indexed on that FK.
- `pac_bio_*`, `oseq_flowcell` (ONT), and `eseq_*` (Element) tables
  parallel the `iseq_*` ones for non-Illumina platforms; in scope is
  Illumina (`iseq_*`) by default; long-read platforms must be
  designed-for but may be implemented as a follow-up.

### Local cache database (replacement for `activecache` and the `/diff` watermarks)

A local cache backed by **either SQLite or MySQL**, chosen the same
way the results database is chosen today (see `WA_RESULTS_DB_PATH` /
`WA_RESULTS_DB_PASSWORD` in `cmd/results.go`, README, and
DEVELOPING). The cache backend is selected from a single env var
`WA_MLWH_CACHE_PATH` (overridable via a `--mlwh-cache` flag on the
long-running server commands and the sync command):

- A bare filesystem path → `modernc.org/sqlite` cache file. This is
  the default for tests (always ephemeral under `.tmp/`) and is
  permitted for development and production deployments where a
  single-node SQLite file is acceptable. The development default is
  `${XDG_CACHE_HOME:-~/.cache}/wa/mlwh.sqlite`; production has no
  default, so the operator must opt in to a backend explicitly.
- A Go-MySQL DSN of the form `user@tcp(host:port)/dbname` →
  MySQL-backed cache, suitable for development and production. The
  password comes from `WA_MLWH_CACHE_PASSWORD`. Specifying a password
  inside the DSN, or via the `--mlwh-cache` flag, is **rejected**
  (same rule as `260505-2.md`).
- The env vars stay named `WA_MLWH_CACHE_PATH` /
  `WA_MLWH_CACHE_PASSWORD` even when the value is a MySQL DSN, for
  parity with `WA_RESULTS_DB_PATH`.
- The same scenario guards apply as for the results DB: `make test`
  refuses an inherited `WA_MLWH_CACHE_PATH`, `make prod` refuses
  inherited test/development values, etc.

Both backends share a single Go-level cache interface and an
identical schema. The schema is defined as embedded `.sql` files per
dialect (`mlwh/cache_schema/sqlite/*.sql`,
`mlwh/cache_schema/mysql/*.sql`) loaded via `embed.FS`; a parity test
in `mlwh/cache_schema_test.go` parses both sets and asserts they
declare the same tables, columns, and indexes. Tests run against both
backends via a small matrix harness so we never ship a SQL feature
that works in one dialect and not the other. Switching backends must
require no code change in callers.

The cache serves three purposes:

1. **Denormalised lookup tables** for every dimension where MLWH has
   no usable index — at minimum `library_type → sample list` and
   `donor_id → sample list`. Other columns we want to search on
   (e.g. exact `supplier_name`, sample `accession_number`,
   `id_run → samples`) can be served straight from MLWH.
2. **A negative cache** for "not in MLWH" verdicts so repeat
   resolution attempts of misspelt identifiers do not hit the
   database. Short TTL (minutes), keyed by raw string.
3. **The existing seqmeta watermark/tombstone table**, unchanged in
   shape, with hashes recomputed against MLWH-derived records
   instead of Saga-derived ones.

Sync strategy:

- A new top-level command `wa mlwh sync` runs a one-shot refresh and
  exits when done. It pulls rows where
  `last_updated >= cache_high_water_mark` from `sample`, `study`, and
  `iseq_flowcell` and replays them into the local denormalised tables
  transactionally, advancing the watermark to `MAX(last_updated)`
  only after the transaction commits. The first sync is a full table
  scan and is documented as such. Operators are expected to schedule
  it via cron; there is no `wa results sync-mlwh` subcommand.
- `wa results serve` and `wa seqmeta serve` accept an opt-in
  `--mlwh-sync-interval` flag (default off / zero) that runs the
  same sync logic on a goroutine timer at the requested interval.
- Read paths consult the cache first; on cache miss for a query the
  cache is supposed to cover, they fall back to a single direct
  MLWH query and write the result back into the cache.
- Cache schema versioning lives alongside the embedded DDL; the
  schema version is checked at startup and the cache is rebuilt from
  scratch on bump, in either backend.
- Concurrency: SQLite uses `journal_mode=WAL` with a process-level
  mutex around the sync transaction; MySQL relies on a row-level
  advisory lock (or a single `INSERT … ON DUPLICATE KEY UPDATE`
  marker row) to serialise sync across processes. Read paths use
  read-only connections in both backends.

#### Cold-cache behaviour

When a cache-backed resolver (`ResolveLibrary`, the `donor_id` step
of `ResolveSample`) is called against a cold or empty cache (first
install, or after a schema-version rebuild), the resolver lazily
triggers a one-shot full sync of just the table it needs
(`iseq_flowcell` for `ResolveLibrary`, `sample` for `donor_id`) and
holds the request until the sync commits, with no timeout and no
fallback. Subsequent calls are served from the warm cache. Help text
for `wa results register --library`, the frontend hierarchical-search
docs, and the resolver's own doc string warn operators about
first-call latency and recommend running `wa mlwh sync` ahead of
time.

### `seqmeta/` package, rebuilt on `mlwh`

- `seqmeta/provider.go`'s `SAGAProvider` interface is replaced by an
  `mlwh.Querier` (or equivalent) interface defined in `mlwh/`. All
  references to the `saga` package, including in test fakes, are
  removed.
- `IdentifierType` enum is updated to the kinds `mlwh` resolves.
  Project ID/name kinds are removed.
- `Validate(ctx, raw)` calls `mlwh.ClassifyIdentifier` and returns
  the same `IdentifierResult` shape (type label + matched record)
  that the frontend already consumes.
- `/enrich/{id}` graph builder consumes `mlwh.SampleDetail` /
  `StudyDetail` / etc. directly; it must populate every field the
  current frontend reads (asserted by the existing vitest suites
  against fixed enrichment fixtures, which must continue to pass
  after the project-removal and iRODS-tightening updates below).
- The diff machinery is repointed at `mlwh.AllStudies` /
  `mlwh.SamplesForStudy` / `mlwh.IRODSPathsForSample`, which read
  through the cache. Hash comparison logic and the SQLite watermark
  table layout do not change.
- The HTTP server's options struct loses any Saga-typed fields and
  gains an `mlwh.Querier`.
- The seqmeta enrichment contract for iRODS files is tightened to
  only the fields `seq_product_irods_locations` actually supplies
  (path / collection / data-object / product id and any other column
  the table provides directly). Frontend types
  (`frontend/lib/contracts.ts`, `seqmeta-enrichment.ts`),
  `result-detail-files.tsx`, and the affected vitest fixtures and
  tests are updated to that subset; fields the old Saga `IRODSFile`
  exposed but MLWH cannot supply (checksums, sizes, AVUs) are
  removed entirely.

### `cmd/results.go` integration

- Replace `resolveResultsRegisterRunID/StudyID/SampleID/LibraryType`
  and the inspector call sites with calls to `mlwh.Resolve*`.
- Help text for `--run`, `--study`, `--sample`, `--library` is
  rewritten to enumerate the input forms each accepts (per the
  resolver implementations) and the forms it cannot.
- Error messages name the offending value and the dimension and
  distinguish "not found" from "ambiguous" from "unsupported form".

### `cmd/seqmeta.go` integration

- `wa seqmeta diff|validate|serve` is kept as a command. Its
  `--token` and `--base-url` Saga flags are removed; upstream
  configuration comes from `WA_MLWH_DSN` / `WA_MLWH_PASSWORD` only.
- `wa seqmeta serve` keeps the existing `--mlwh-cache` flag (cache
  override) and gains an opt-in `--mlwh-sync-interval` flag matching
  the one on `wa results serve`.

### Tests

- Unit tests use `sqlmock` for the `mlwh` package and a real
  `modernc.org/sqlite` cache; they assert that every resolver
  exercises the indexed query that `EXPLAIN` confirms is fast
  (verified via captured SQL strings).
- A regression suite covers each of the five sample inputs from the
  bug report, study UUID/accession/LIMS-ID/title, run lookup, library
  exact match, and the SQSCP rejection.
- Hierarchical-search regressions from `260501-5.md` and
  `260503-1.md` (study expanding to samples/lanes; library expanding
  to samples/lanes; sample expanding to lanes) must continue to pass
  end-to-end.
- Live-MLWH integration tests are gated solely on `WA_MLWH_DSN` (and,
  if needed, `WA_MLWH_PASSWORD`) being present in the environment;
  when unset they skip cleanly. They exercise the live replica with
  the same identifiers as the regression suite.
- All existing seqmeta and frontend tests continue to pass with the
  upstream swapped from `saga` to `mlwh`, after the project-removal
  and iRODS-tightening updates.

## Lessons from prior attempts

The `.docs/saga/`, `.docs/seqmeta/`, and `.docs/results-seqmeta/`
specs all _mentioned_ sample/study/run/library lookups but never
nailed down which column was authoritative for each input form, which
filters the upstream actually supported, and which inputs had to be
rejected as unsupported. The result was a chain of "best guess"
fallbacks (`AllSamples`, iRODS-AVU substring scoring, library-type
`icontains`) that were silently wrong. This spec must close every
one of those gaps with an explicit per-column query, an indexed plan
verified by `EXPLAIN`, and a test that fails today and passes once
the implementation is in place.

## Out of scope

- Long-read platform tables (`pac_bio_*`, `oseq_*`, `eseq_*`) —
  designed-for but not implemented in this iteration.
- iRODS metadata enrichment beyond what
  `seq_product_irods_locations` already gives us.
- Any change to the frontend beyond removing project entities,
  tightening the iRODS file contract, removing "Saga" from
  user-facing strings, and the test updates those changes require.
- Project entities. Removed entirely.
