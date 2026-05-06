# Direct MLWH Access Specification

## Overview

Replace the broken `saga` HTTP client and the `seqmeta` flows built on it with
a new `mlwh/` Go package that talks directly to the Multi-LIMS Warehouse MySQL
read replica. `seqmeta/` is kept but rebuilt on `mlwh`. The Saga client and
every `SAGA_*` env var, flag, comment, fixture, and frontend reference are
deleted with no shims.

The new system must resolve every input form listed in the bug report
(Sanger name, supplier name, `id_sample_lims`, sample UUID, donor ID, study
accession/UUID/LIMS ID, numeric run, exact `pipeline_id_lims`) to canonical
records via indexed MLWH queries, reject LIMS-provider constants such as
`SQSCP`, expand identifiers hierarchically for the `wa results` search layer,
and serve enrichment graphs for the existing seqmeta HTTP contract. Project
entities are removed from the API entirely.

## Architecture

### New package: `mlwh/`

```
mlwh/
  mlwh.go              package doc, sentinels, IdentifierKind constants
  config.go            DSN/password resolution, password rejection rules
  config_test.go
  querier.go           Querier interface + sqlOpenFunc indirection for tests
  resolver.go          ResolveSample/Study/Run/Library/ClassifyIdentifier
  resolver_test.go     sqlmock-driven indexed-query assertions
  resolver_reject.go   LIMS provider constant rejection set
  resolver_reject_test.go
  hierarchy.go         SamplesForStudy/Run/Library, Libraries/RunsForStudy,
                       LanesForSample, IRODSPathsFor*, StudyForSample,
                       ExpandIdentifier
  hierarchy_test.go
  types.go             Sample, Study, Library, Run, Lane, IRODSPath,
                       SampleDetail, StudyDetail, RunDetail, LibraryDetail,
                       Match, TaggedID, IdentifierKind
  cache.go             Cache interface, sqlite/mysql backends, sync engine,
                       schema versioning, embedded DDL loader
  cache_test.go        backend-matrix harness (sqlite + mysql via sqlmock)
  cache_schema.go      embed.FS of cache_schema/{sqlite,mysql}/*.sql
  cache_schema_test.go parity test: same tables/columns/indexes per dialect
  cache_schema/sqlite/*.sql
  cache_schema/mysql/*.sql
  sync.go              one-shot sync, watermarks, cold-cache lazy sync
  sync_test.go
  integration_test.go  live-MLWH gated on WA_MLWH_DSN
```

### Core types

```go
package mlwh

type IdentifierKind string

const (
    KindSampleUUID        IdentifierKind = "sample_uuid"
    KindSampleLimsID      IdentifierKind = "sample_lims_id"
    KindSangerSampleName  IdentifierKind = "sanger_sample_name"
    KindSangerSampleID    IdentifierKind = "sanger_sample_id"
    KindSupplierName      IdentifierKind = "supplier_name"
    KindSampleAccession   IdentifierKind = "sample_accession"
    KindDonorID           IdentifierKind = "donor_id"
    KindStudyUUID         IdentifierKind = "study_uuid"
    KindStudyLimsID       IdentifierKind = "study_lims_id"
    KindStudyAccession    IdentifierKind = "study_accession"
    KindStudyName         IdentifierKind = "study_name"
    KindRunID             IdentifierKind = "run_id"
    KindLibraryType       IdentifierKind = "library_type"
)

type Match struct {
    Kind      IdentifierKind
    Canonical string // sample.name, study.id_study_lims, id_run, pipeline_id_lims
    Sample    *Sample
    Study     *Study
    Run       *Run
    Library   *Library
}

type Sample struct {
    IDSampleTmp     int64
    IDLims          string // always "SQSCP" in scope
    IDSampleLims    string
    UUIDSampleLims  string
    Name            string // canonical Sanger name
    SangerSampleID  string
    SupplierName    string
    AccessionNumber string
    DonorID         string
    TaxonID         int
    CommonName      string
    Description     string
    // Plus every column read by the existing enrichmentSampleSchema in
    // frontend/lib/contracts.ts, enumerated at implementation time and
    // asserted by existing vitest fixtures.
}

type Study struct {
    IDStudyTmp      int64
    IDLims          string
    IDStudyLims     string
    UUIDStudyLims   string
    Name            string
    AccessionNumber string
    StudyTitle      string
    FacultySponsor  string
    State           string
    // Plus every column read by the existing enrichmentStudySchema in
    // frontend/lib/contracts.ts, enumerated at implementation time and
    // asserted by existing vitest fixtures.
}

type Library struct {
    PipelineIDLims string // canonical "library_type"
    SampleCount    int    // populated by hierarchy queries
}

type Run struct {
    IDRun int
}

type Lane struct {
    IDRun    int
    Position int  // lane
    TagIndex int
}

type IRODSPath struct {
    IDProduct  string
    Collection string
    DataObject string
    IRODSPath  string // collection + "/" + data_object
}

type TaggedID struct {
    Kind      IdentifierKind
    Canonical string
}

type SampleDetail struct {
    Sample     Sample
    Study      *Study
    Lanes      []Lane
    Libraries  []Library
    IRODSPaths []IRODSPath
}

type StudyDetail struct {
    Study     Study
    Libraries []LibraryDetail
}

type LibraryDetail struct {
    Library Library
    Samples []Sample
}

type RunDetail struct {
    Run     Run
    Samples []Sample
    Studies []Study
}
```

### Sentinels

```go
var (
    ErrNotFound              = errors.New("mlwh: identifier not found")
    ErrAmbiguous             = errors.New("mlwh: identifier matches multiple records")
    ErrUnsupportedIdentifier = errors.New("mlwh: identifier form not supported")
    ErrUpstreamImpaired      = errors.New("mlwh: upstream database impaired")
    ErrPasswordInDSN         = errors.New("mlwh: password must not appear in DSN")
)
```

### Querier interface

```go
type Querier interface {
    QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
    QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
    ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}
```

`*sql.DB` satisfies it; tests pass an `sqlmock` connection wrapped by the same
interface. The package exposes `Open(ctx, cfg Config) (*Client, error)` where
`Config` carries the resolved DSN, the cache backend, and a `Querier`-typed
override for tests.

### Configuration and env vars

| Env var                  | Purpose                                  |
| ------------------------ | ---------------------------------------- |
| `WA_MLWH_DSN`            | Read-only MLWH DSN, no password embedded |
| `WA_MLWH_PASSWORD`       | MLWH password (never on CLI/DSN)         |
| `WA_MLWH_CACHE_PATH`     | Cache backend: SQLite path or MySQL DSN  |
| `WA_MLWH_CACHE_PASSWORD` | Cache MySQL password (never on CLI/DSN)  |

`mlwh.ResolveDSN` rejects `WA_MLWH_DSN` whose parsed `Passwd` is non-empty
with `ErrPasswordInDSN`. Same for `WA_MLWH_CACHE_PATH` when it parses as a
MySQL DSN. CLI flags `--mlwh-cache` accept the same forms with the same
rejection rule.

The `WA_MLWH_CACHE_PATH` / password pair is the SQLite/MySQL chooser, mirroring
`WA_RESULTS_DB_PATH` / `WA_RESULTS_DB_PASSWORD`. SQLite default for `wa
mlwh sync` and `wa seqmeta serve` in dev is
`${XDG_CACHE_HOME:-~/.cache}/wa/mlwh.sqlite`. Production has no default; the
operator must set the env var.

### Cache schema (per-dialect, parity-tested)

Tables (every column declared in both `sqlite/*.sql` and `mysql/*.sql`):

- `study_mirror` mirrors MLWH `study` filtered to `id_lims = 'SQSCP'`. One
  column per field of `mlwh.Study` (`id_study_tmp INT PRIMARY KEY`,
  `id_lims TEXT`, `id_study_lims TEXT`, `uuid_study_lims TEXT`, `name TEXT`,
  `accession_number TEXT`, `study_title TEXT`, `faculty_sponsor TEXT`,
  `state TEXT`, plus every other column read by
  `enrichmentStudySchema`), plus `last_updated TEXT NOT NULL` for
  watermarking. `INDEX(id_study_lims)`, `INDEX(id_lims)`,
  `INDEX(last_updated)`.
- `sample_mirror` mirrors MLWH `sample` filtered to `id_lims = 'SQSCP'`.
  One column per field of `mlwh.Sample` (`id_sample_tmp INT PRIMARY KEY`,
  `id_lims TEXT`, `id_sample_lims TEXT`, `uuid_sample_lims TEXT`,
  `name TEXT`, `sanger_sample_id TEXT`, `supplier_name TEXT`,
  `accession_number TEXT`, `donor_id TEXT`, `taxon_id INT`,
  `common_name TEXT`, `description TEXT`, plus every other column read by
  `enrichmentSampleSchema`), plus `last_updated TEXT NOT NULL` for
  watermarking. `INDEX(id_sample_lims)`, `INDEX(uuid_sample_lims)`,
  `INDEX(name)`, `INDEX(sanger_sample_id)`, `INDEX(supplier_name)`,
  `INDEX(accession_number)`, `INDEX(donor_id)`, `INDEX(last_updated)`.
- `library_samples(pipeline_id_lims TEXT, id_sample_tmp INT, id_study_lims
TEXT, INDEX(pipeline_id_lims), INDEX(id_study_lims), INDEX(id_sample_tmp))`.
  Sample columns are deliberately not duplicated here; consumers join
  `library_samples.id_sample_tmp` to `sample_mirror.id_sample_tmp`.
- `donor_samples(donor_id TEXT, id_sample_tmp INT, id_study_lims TEXT,
INDEX(donor_id))`
- `negative_cache(raw TEXT PRIMARY KEY, reason TEXT, fetched_at TEXT,
ttl_seconds INT)`
- `watermarks` (existing seqmeta shape, retained for diff)
- `enrich_cache` (existing seqmeta shape, retained)
- `sync_state(table_name TEXT PRIMARY KEY, high_water TEXT, last_run TEXT)`
- `schema_version(version INT PRIMARY KEY, applied_at TEXT)`

Negative-cache TTL: 15 minutes. Schema-version mismatch drops and recreates
all tables.

### Cache concurrency and read/write handle separation

- SQLite caches are opened with the `_pragma=journal_mode(WAL)` connection
  string parameter. `Sync` acquires a process-level `sync.Mutex` (a single
  `*sync.Mutex` field on `*Client`) for the entire transaction.
- MySQL caches acquire `GET_LOCK('wa_mlwh_sync', n)` (with `n` a finite
  timeout, default 30 seconds) at the start of `Sync` and release it on
  commit or rollback. Lock-acquisition failure surfaces as
  `ErrUpstreamImpaired`.
- Each `*Client` opens two `*sql.DB` handles against the cache: one
  read-write handle used by `Sync`, and a separate read-only handle used by
  every read path (resolvers, hierarchy methods, `AllStudies`,
  `ExpandIdentifier`, `negative_cache` reads). The read handle for SQLite
  is opened with `?mode=ro`; for MySQL it is opened against a dedicated
  read-only user (or, if unavailable, every read transaction begins with
  `START TRANSACTION READ ONLY`).

### Read-through write-back on cache miss

Read paths consult the cache first. On a miss for a query the cache is
supposed to cover (notably `AllStudies`, `SamplesForStudy`,
`LibrariesForStudy`, `SamplesForLibrary`, the `donor_samples` lookup, and
`ExpandIdentifier`), the read path falls back to a single direct MLWH
query against the read-only MLWH connection and, on a non-empty result,
upserts the rows into the corresponding cache mirror: `study_mirror` for
study-shaped rows, `sample_mirror` for sample-shaped rows,
`library_samples` for library/sample/study triples, `donor_samples` for
donor-keyed lookups. This write-back never advances any
`sync_state.high_water` value (only `Sync` does), and it never fires when
the cache row already exists. A subsequent identical call within the
process lifetime is therefore served from the cache without a further
MLWH query.

### Truncation policy for hierarchy hops

Hierarchy methods that fan out from one identifier to many samples
(`SamplesForStudy`, `SamplesForLibrary`, the library hop in
`seqmeta.Enrich`) are bounded by the constant `MaxSamplesPerHop = 1000`,
exported from `mlwh/`. The seqmeta enrichment layer is the sole enforcer:
when a hop's underlying call returns `MaxSamplesPerHop` rows, seqmeta
sets `partial = true` on the response, appends a
`MissingHop{Hop, Reason: ReasonSamplesTruncated}` entry, and truncates the
emitted samples array to `MaxSamplesPerHop`. The flag is exposed on the
`EnrichmentGraph` JSON shape consumed by `frontend/lib/contracts.ts`.

### ExpandIdentifier result cache

`ExpandIdentifier` results are cached in-process in `mlwh/` keyed by
`(IdentifierKind, canonical)` with TTL `expandIdentifierTTL = 5 *
time.Minute`. The cache is invalidated whenever `Sync` commits any table
(the sync engine clears the entire expand cache after a successful
commit). Cache hits skip every MLWH query and every cache-DB query.

### Resolver behaviour

`ResolveSample` cascade (each step is one indexed query against `sample` with
`id_lims = 'SQSCP'` unless noted; first non-empty hit wins):

1. UUID-shape -> `uuid_sample_lims = ?`
2. Pure-int shape -> `id_sample_lims = ?`
3. Otherwise:
    1. `name = ?`
    2. `sanger_sample_id = ?`
    3. `supplier_name = ?`
    4. `accession_number = ?`
    5. `donor_id = ?` via `donor_samples` cache (lazy full sync of `sample`
       table on cold cache)

Cross-column matches are not detected; the first hit returns immediately.
LIMS-provider constants in the rejection set (`SQSCP`, `GCLP` initially)
short-circuit before any query with `ErrUnsupportedIdentifier`.

`ResolveStudy` cascade:

1. UUID-shape -> `uuid_study_lims = ?`
2. Pure-int shape -> `id_study_lims = ?` with `id_lims = 'SQSCP'`
3. Otherwise:
    1. `accession_number = ?`
    2. `name = ?` (case-sensitive; `WithCaseInsensitive` opt makes this
       `LOWER(name) = LOWER(?)`)

Multiple text-matches return `ErrAmbiguous`.

`ResolveRun(raw)`:

- `strconv.Atoi`; non-numeric -> `ErrUnsupportedIdentifier`.
- `SELECT 1 FROM iseq_product_metrics WHERE id_run = ? LIMIT 1`. Hit ->
  `Match{Kind: KindRunID, Canonical: "<n>"}`. Miss -> `ErrNotFound`.

`ResolveLibrary(raw)`:

- Look up `pipeline_id_lims = ?` exact in `library_samples` cache.
- Cold cache -> trigger lazy full sync of `iseq_flowcell` first.
- Hit -> `Match{Kind: KindLibraryType, Canonical: raw}`. Miss -> `ErrNotFound`.

`ClassifyIdentifier(raw)` is the seqmeta `Validate` entry point. Dispatches by
input shape, then within shape applies study-before-sample-before-run-
before-library priority. Returns the first hit; never `ErrAmbiguous` from
cross-column matches; distinguishes `ErrNotFound` from `ErrUpstreamImpaired`.

### Hierarchy methods

All take `ctx context.Context` and `(limit, offset int)`.

```go
func (c *Client) SamplesForStudy(ctx, studyLimsID, limit, offset) ([]Sample, error)
func (c *Client) SamplesForRun(ctx, idRun, limit, offset) ([]Sample, error)
func (c *Client) SamplesForLibrary(ctx, pipelineIDLims, studyLimsID, limit, offset) ([]Sample, error)
func (c *Client) LibrariesForStudy(ctx, studyLimsID, limit, offset) ([]Library, error)
func (c *Client) RunsForStudy(ctx, studyLimsID, limit, offset) ([]Run, error)
func (c *Client) LanesForSample(ctx, sangerName, limit, offset) ([]Lane, error)
func (c *Client) IRODSPathsForSample(ctx, sangerName, limit, offset) ([]IRODSPath, error)
func (c *Client) IRODSPathsForStudy(ctx, studyLimsID, limit, offset) ([]IRODSPath, error)
func (c *Client) StudyForSample(ctx, sangerName) (*Study, error)
func (c *Client) AllStudies(ctx, limit, offset) ([]Study, error)
func (c *Client) ExpandIdentifier(ctx, kind IdentifierKind, canonical string) ([]TaggedID, error)
```

`AllStudies` reads from `study_mirror` (populated by `Sync("study")`) when
warm, falls back to a single indexed `SELECT * FROM study WHERE id_lims =
'SQSCP' ORDER BY id_study_lims LIMIT ? OFFSET ?` against MLWH on cold
cache and upserts the resulting rows into `study_mirror` (read-through
write-back, no watermark advance), and is the upstream for the seqmeta
study-diff route.

`SamplesForStudy` is cache-backed: it joins `library_samples` (filtered by
`id_study_lims = ?`) to `sample_mirror` on `id_sample_tmp` and returns
distinct `Sample` rows ordered by `sample_mirror.name`. No MLWH query is
issued on warm cache. On cold cache it falls back to a single MLWH join
and upserts both `library_samples` and `sample_mirror` (read-through
write-back, no watermark advance).

`SamplesForLibrary` requires `studyLimsID` because the `library_samples`
cache is keyed by `(pipeline_id_lims, id_study_lims)`. Sample columns come
from `sample_mirror` joined on `id_sample_tmp`.

All hierarchy methods (`SamplesForStudy`, `SamplesForRun`,
`SamplesForLibrary`, `LibrariesForStudy`, `RunsForStudy`, `LanesForSample`,
`IRODSPathsForSample`, `IRODSPathsForStudy`, `StudyForSample`) follow the
same parent-existence contract: if the parent identifier exists but has no
children, they return an empty slice and a nil error; if the parent
identifier does not exist (no row in `study_mirror` /
`iseq_product_metrics` / `sample_mirror` for the given key), they return
`ErrNotFound`. `StudyForSample` returns `(nil, ErrNotFound)` on missing
sample and `(nil, nil)` is never produced.

`ExpandIdentifier` semantics:

| Input kind                  | Output (in addition to itself)    |
| --------------------------- | --------------------------------- |
| `study_lims_id` (canonical) | samples + lanes for those samples |
| `library_type`              | samples + lanes for those samples |
| `sanger_sample_name`        | lanes for that sample             |
| `run_id`                    | sample list                       |

Each row in the output is `TaggedID{Kind, Canonical}` so callers can build a
results-store query like `WHERE meta_value IN (...)` per dimension.

### `seqmeta/` package, rebuilt

- `seqmeta/provider.go` redefines `Provider` as `mlwh.Querier` plus the
  hierarchy/resolver methods seqmeta uses. All `saga` imports removed.
- `seqmeta/types.go` replaces `saga.Study`/`saga.MLWHSample`/`saga.IRODSFile`
  with `mlwh.Study`/`mlwh.Sample`/`mlwh.IRODSPath`. The `Project`, `Users`,
  `HopProject`, `HopUsers`, `IdentifierProjectName` symbols are deleted.
- `IdentifierType` constants are renamed to mirror `IdentifierKind` exactly
  (`study_lims_id`, `study_accession`, `study_uuid`, `study_name`,
  `sanger_sample_name`, `sanger_sample_id`, `sample_lims_id`, `sample_uuid`,
  `sample_accession`, `supplier_name`, `donor_id`, `run_id`, `library_type`).
- `Validate` calls `mlwh.ClassifyIdentifier` and returns the existing
  `IdentifierResult{Identifier, Type, Object}` shape.
- `/enrich/{id}` builds `EnrichmentGraph` directly from
  `mlwh.SampleDetail/StudyDetail/RunDetail/LibraryDetail`.
- `/diff/study/{id}` and `/diff/sample/{id}` route through the cache:
  `mlwh.AllStudies` / `mlwh.SamplesForStudy` / `mlwh.IRODSPathsForSample`.
  Watermark/hash logic in `seqmeta/store.go` is unchanged.
- `Server` config struct loses `SAGAProvider`, gains an `*mlwh.Client`.

### Frontend changes (zod + components only)

- `frontend/lib/contracts.ts`: drop `projectSchema`, `projectUserSchema`,
  `Project`, `ProjectUser`, and the `project` / `users` fields on
  `enrichmentGraphSchema`.
- `frontend/components/seqmeta-badge.tsx`: drop "Project" and "Project users"
  rows and any "via Saga" / "Saga" strings.
- `frontend/lib/contracts.ts` enrichment iRODS shape (new): only
  `{ id_product, collection, data_object, irods_path }`. This four-field
  set is the deliberate minimum subset of `seq_product_irods_locations`
  columns; columns the table also has but we do not surface (e.g.
  `pid_lock`, `deleted_at`) are intentionally excluded. Sizes, checksums,
  AVUs are removed.
- `frontend/components/result-detail-files.tsx` updated to consume the new
  iRODS shape.
- All vitest fixtures updated; project-related test cases deleted.

### CLI changes

- `cmd/saga.go`, `cmd/saga_test.go` deleted.
- `cmd/results.go` register: replace `resolveResultsRegister*` with
  `mlwh.Resolve*`; rewrite the `--run/--study/--sample/--library` help text;
  rewrite error messages with dimension and offending value.
- `cmd/seqmeta.go`: drop `--token`, `--base-url`. Keep `--db`. Add
  `--mlwh-cache` (override `WA_MLWH_CACHE_PATH`). Add
  `--mlwh-sync-interval` (default zero, opt-in). Drop `prefetchedProvider`.
- `cmd/results.go`: `wa results serve` gains `--mlwh-cache` and
  `--mlwh-sync-interval` (matching `wa seqmeta serve`).
- New top-level command `wa mlwh sync`: one-shot sync of `sample`, `study`,
  `iseq_flowcell` rows where `last_updated >= cache_high_water_mark`.

### Env scenario rules

`make test`/`make dev`/`make prod` enforce the same scenario guards on
`WA_MLWH_DSN`, `WA_MLWH_PASSWORD`, `WA_MLWH_CACHE_PATH`,
`WA_MLWH_CACHE_PASSWORD` as on the existing results-DB equivalents:
inherited values from the wrong scenario are refused at startup. The
`SAGA_*` block in every `.env*` file is removed.

### Testing strategy

- Unit: `sqlmock` in `mlwh/`, real `modernc.org/sqlite` for the cache, plus a
  `sqlmock`-driven MySQL cache path. Each resolver test asserts the exact SQL
  string and the index-using `WHERE` clause.
- Cache schema parity: parser-level test asserting identical table/column/
  index sets across `cache_schema/sqlite/*.sql` and `cache_schema/mysql/*.sql`.
- Regression suite (`mlwh/resolver_test.go`): the five sample inputs from the
  bug report all resolve to the same canonical Sanger name; `SQSCP` is
  rejected; study UUID/accession/LIMS ID/title resolve; numeric run resolves;
  library exact match resolves; ambiguous study name returns `ErrAmbiguous`.
- Hierarchical-search regressions in `results/server_test.go` and
  `seqmeta/server_test.go`: study expansion -> samples + lanes; library
  expansion -> samples + lanes; sample expansion -> lanes; full search
  end-to-end completes.
- Live integration (`mlwh/integration_test.go`,
  `seqmeta/integration_test.go`): gated on `WA_MLWH_DSN` only; skip cleanly
  when unset; same identifiers as the regression suite.
- Frontend vitest suites updated for the project removal and the iRODS
  contract tightening; existing seqmeta-badge / result-detail-files / dashboard
  / filter-builder tests must continue to pass after fixture swap from
  Saga shapes to MLWH shapes.

## A. Cache and Sync Foundation

### A1: Embedded per-dialect cache schema with parity test

**Package:** `mlwh/`
**File:** `mlwh/cache_schema.go`, `mlwh/cache_schema/{sqlite,mysql}/*.sql`
**Test file:** `mlwh/cache_schema_test.go`

```go
//go:embed cache_schema/sqlite/*.sql cache_schema/mysql/*.sql
var cacheSchemaFS embed.FS

func loadSchema(dialect string) ([]string, error)

type schemaShape struct {
    Tables map[string]map[string]string // table -> column -> type-family
    Index  map[string][]string           // table -> []index-cols
}

func parseSchemaShape(stmts []string) (schemaShape, error)
```

**Acceptance tests:**

1. Given the SQLite schema files, when `loadSchema("sqlite")` runs, then it
   returns 9 statements: `study_mirror`, `sample_mirror`,
   `library_samples`, `donor_samples`, `negative_cache`, `watermarks`,
   `enrich_cache`, `sync_state`, `schema_version`, in that order.
2. Given both dialect directories, when `parseSchemaShape` runs on each and
   they are compared, then the table set is equal, every table has the same
   column names, every text-typed column maps to TEXT/VARCHAR families and
   every integer-typed column maps to INTEGER/INT families, and every index
   declaration covers identical column lists.
3. Given a SQLite cache file opened with `OpenCache(":memory:")`, when the
   schema loader runs, then `SELECT name FROM sqlite_master WHERE
type='table'` returns the nine table names above.
4. Given the parsed `study_mirror` and `sample_mirror` shapes, when
   inspected, then each contains a `last_updated` column and the column
   set is a superset of the fields declared on `mlwh.Study` and
   `mlwh.Sample` respectively (every Go struct field has a matching SQL
   column).

### A2: Cache `Open` and schema versioning

**Package:** `mlwh/`
**File:** `mlwh/cache.go`
**Test file:** `mlwh/cache_test.go`

```go
const CacheSchemaVersion = 1

type Cache interface {
    DB() *sql.DB
    Dialect() string // "sqlite" or "mysql"
    Close() error
}

func OpenCache(ctx context.Context, cfg CacheConfig) (Cache, error)

type CacheConfig struct {
    Path     string // SQLite path or MySQL DSN
    Password string // injected into MySQL DSN if present
}
```

**Acceptance tests:**

1. Given a fresh SQLite path under `t.TempDir()`, when `OpenCache` runs, then
   `Dialect()` returns `"sqlite"` and `schema_version` table contains exactly
   one row with `version = 1`.
2. Given a SQLite cache opened at version 1, when re-opened, then no schema
   reset occurs (existing row count unchanged).
3. Given a SQLite cache containing `schema_version = 0`, when `OpenCache`
   runs, then every existing table is dropped and recreated and
   `schema_version` is updated to 1.
4. Given a MySQL DSN with non-empty `Passwd`, when `OpenCache` runs, then it
   returns `ErrPasswordInDSN`.
5. Given a MySQL DSN with empty `Passwd` and `cfg.Password = "secret"`, when
   `OpenCache` runs, then the resolved DSN passed to `sql.Open` has
   `Passwd = "secret"` and is never logged.
6. Given a fresh SQLite cache opened by `OpenCache`, when `PRAGMA
journal_mode` is queried on the resulting handle, then the returned
   value is `"wal"` (case-insensitive).
7. Given a `*Client` with a SQLite cache, when two goroutines call
   `Sync(ctx, "sample")` concurrently against an `sqlmock` MLWH whose
   first transaction blocks on a channel until released, then the second
   call's `BEGIN` is observed only after the first call's `COMMIT`
   (verified by `sqlmock` ordered expectations on a single connection).
8. Given a `*Client` with a MySQL cache, when two goroutines call
   `Sync(ctx, "sample")` concurrently and the first holds
   `GET_LOCK('wa_mlwh_sync', n)`, then the second call issues
   `GET_LOCK('wa_mlwh_sync', n)` and proceeds only after the first call
   issues `RELEASE_LOCK('wa_mlwh_sync')` (verified via `sqlmock` ordered
   expectations).
9. Given a warm cache and the read-only handle exposed by the `*Client`,
   when an `INSERT INTO library_samples ...` is attempted on that handle,
   then it returns a non-nil error (SQLite: `attempt to write a readonly
database`; MySQL: equivalent permission/transaction-mode error).

### A3: Sync engine with watermarks

**Package:** `mlwh/`
**File:** `mlwh/sync.go`
**Test file:** `mlwh/sync_test.go`

```go
type SyncReport struct {
    Table     string
    Inserted  int
    Updated   int
    HighWater time.Time
}

func (c *Client) Sync(ctx context.Context, tables ...string) ([]SyncReport, error)
```

**Acceptance tests:**

1. Given a cold cache and an `sqlmock` MLWH that returns 3 sample rows with
   `last_updated` values `t1 < t2 < t3`, when `Sync(ctx, "sample")` runs,
   then `sample_mirror` has 3 rows (one per source row, full sample
   columns), `donor_samples` has 3 rows, `sync_state` row for `sample`
   has `high_water = t3`, the watermark advance happens after the
   transaction commits (verified by mock ordering: `COMMIT` before the
   `UPDATE sync_state` of the new high water), and the returned report
   shows `Inserted = 3`.
2. Given a warm cache with `sync_state.sample.high_water = t2`, when MLWH
   returns one new row at `t3` and `Sync` runs, then the SQL passed to
   `sqlmock` includes `WHERE last_updated >= ?` bound to `t2` and
   `id_lims = 'SQSCP'`, and one row is upserted into `sample_mirror`
   (and `donor_samples`).
3. Given a sync that fails part-way (mocked `Exec` returns error after one
   row), when the transaction rolls back, then `sync_state.sample.high_water`
   is unchanged from its prior value and `sample_mirror` row count is
   unchanged.
4. Given a successful sync of `iseq_flowcell`, then `library_samples` is
   populated and includes one row per `(pipeline_id_lims, id_sample_tmp,
id_study_lims)` triple distinct in the source.
5. Given a cold cache and an `sqlmock` MLWH that returns 2 study rows
   with `last_updated` values `t1 < t2` (both `id_lims = 'SQSCP'`), when
   `Sync(ctx, "study")` runs, then `study_mirror` has 2 rows (full study
   columns), the SQL passed to `sqlmock` includes `WHERE last_updated >=
?` and `id_lims = 'SQSCP'`, and `sync_state.study.high_water = t2`
   only after the transaction commits.
6. Given an MLWH source containing a non-`SQSCP` study row alongside
   `SQSCP` rows, when `Sync(ctx, "study")` runs, then only `SQSCP` rows
   are written to `study_mirror` (the source query is filtered).

### A4: Cold-cache lazy sync for resolver-backed tables

**Package:** `mlwh/`
**File:** `mlwh/sync.go`
**Test file:** `mlwh/sync_test.go`

**Acceptance tests:**

1. Given a cold cache, when `ResolveLibrary(ctx, "Standard")` runs, then a
   full sync of `iseq_flowcell` runs first, the result is cached, the call
   returns a `Match` for `Standard`, and the request blocks until the sync
   transaction commits (verified by ordering of mock calls).
2. Given a cold cache, when `ResolveSample(ctx, "DONOR-X")` reaches the
   `donor_id` step, then a full sync of `sample` runs first, then the donor
   query returns the canonical Sanger name.
3. Given a warm cache, when either resolver runs again, then `Sync` is not
   invoked (verified by zero new mock expectations).

## B. Identifier Resolvers

### B1: ResolveSample cascade

**Package:** `mlwh/`
**File:** `mlwh/resolver.go`
**Test file:** `mlwh/resolver_test.go`

```go
func (c *Client) ResolveSample(ctx context.Context, raw string) (Match, error)
```

**Acceptance tests:**

1. Given `raw = "b7daafb8-c59f-11ee-8fba-024224dd57f4"` and `sqlmock`
   matching `^SELECT .* FROM sample WHERE uuid_sample_lims = \? LIMIT 1$`
   returning a row with `name = "7607STDY14643771"`, when `ResolveSample`
   runs, then the result has `Kind = KindSampleUUID`, `Canonical =
"7607STDY14643771"`, and `Sample.UUIDSampleLims` matches the input.
2. Given `raw = "9575305"` and the UUID query returning zero rows,
   when `ResolveSample` runs, then the next executed SQL matches the
   regex
   `^SELECT .* FROM sample WHERE id_sample_lims = \? AND id_lims = 'SQSCP' LIMIT 1$`
   and on a hit `Kind = KindSampleLimsID`, `Canonical =
"7607STDY14643771"`.
3. Given `raw = "7607STDY14643771"` and prior steps returning zero rows,
   when the cascade reaches `name`, then the executed SQL matches `WHERE
name = ? AND id_lims = 'SQSCP'` and the result has `Kind =
KindSangerSampleName`, `Canonical = "7607STDY14643771"`.
4. Given `raw = "Hek_R1"` and prior steps returning zero rows, when the
   cascade reaches `supplier_name`, then `Kind = KindSupplierName`,
   `Canonical` equals the matched `sample.name`.
5. Given `raw = "7607STDY14643771"` matched only via `donor_id` on the
   cache, when `ResolveSample` runs against a warm cache, then the query
   path uses `donor_samples` (not `sample`), and `Kind = KindDonorID`.
6. Given `raw = "SQSCP"`, when `ResolveSample` runs, then no SQL is executed
   and the error is `ErrUnsupportedIdentifier` whose message contains
   `"SQSCP"` and the word `"LIMS provider constant"`.
7. Given `raw = "missing-id"` and every cascade step returning zero rows,
   when `ResolveSample` runs, then the error is `ErrNotFound`, the negative
   cache is populated, and a second call within the TTL executes zero MLWH
   queries.
8. Given the MySQL replica returning a non-client error on the first step,
   when `ResolveSample` runs, then the error wraps `ErrUpstreamImpaired`
   (not `ErrNotFound`).

### B2: ResolveStudy cascade

**Package:** `mlwh/`
**File:** `mlwh/resolver.go`
**Test file:** `mlwh/resolver_test.go`

```go
type ResolveStudyOption func(*resolveStudyOpts)
func WithCaseInsensitiveStudyName() ResolveStudyOption
func (c *Client) ResolveStudy(ctx context.Context, raw string, opts ...ResolveStudyOption) (Match, error)
```

**Acceptance tests:**

1. Given a UUID-shaped input matching `uuid_study_lims`, when `ResolveStudy`
   runs, then `Kind = KindStudyUUID`, `Canonical` equals the matched
   `id_study_lims`.
2. Given `raw = "6568"` matching `id_study_lims = '6568'`, when
   `ResolveStudy` runs, then the SQL uses `WHERE id_study_lims = ? AND
id_lims = 'SQSCP'` and `Kind = KindStudyLimsID`.
3. Given `raw = "EGAS00001005445"`, when `ResolveStudy` runs, then the SQL
   uses `WHERE accession_number = ?` and `Kind = KindStudyAccession`.
4. Given `raw = "Some Title"` matching exactly two distinct studies on
   `name`, when `ResolveStudy` runs, then the error is `ErrAmbiguous` and
   the message names both `id_study_lims` values.
5. Given `raw` matching exactly one study on `name`, when `ResolveStudy`
   runs, then `Kind = KindStudyName` and `Match.Canonical` equals the
   matched study's `id_study_lims` (not its name).
6. Given `raw = "some title"` and a study named `Some Title`, when
   `ResolveStudy(ctx, raw)` (no opts) runs, then the error is `ErrNotFound`.
7. Given the same input and `WithCaseInsensitiveStudyName()`, when
   `ResolveStudy` runs, then the SQL uses `LOWER(name) = LOWER(?)` and the
   match succeeds.

### B3: ResolveRun and ResolveLibrary

**Package:** `mlwh/`
**File:** `mlwh/resolver.go`
**Test file:** `mlwh/resolver_test.go`

**Acceptance tests:**

1. Given `raw = "abc"`, when `ResolveRun` runs, then it returns
   `ErrUnsupportedIdentifier` and no SQL is executed.
2. Given `raw = "12345"` and `iseq_product_metrics` containing one row with
   `id_run = 12345`, when `ResolveRun` runs, then `Kind = KindRunID` and
   `Canonical = "12345"`.
3. Given `raw = "12345"` and zero matching metrics rows, when `ResolveRun`
   runs, then the error is `ErrNotFound`.
4. Given `raw = "Standard"` and `library_samples` cache containing rows
   for `pipeline_id_lims = 'Standard'`, when `ResolveLibrary` runs, then
   `Kind = KindLibraryType`, `Canonical = "Standard"`, and no MLWH query
   is executed.
5. Given `raw = "Bespoke"` and a cold cache, when `ResolveLibrary` runs,
   then `iseq_flowcell` is fully synced first (verified by mock ordering)
   before the cache lookup runs.
6. Given `raw = "Unknown"` and a warm cache with no matching row, when
   `ResolveLibrary` runs, then the error is `ErrNotFound`.
7. Given the Go doc comment immediately above `func (c *Client)
ResolveLibrary`, when read, then it contains the substring `"first
call"` and the substring `"wa mlwh sync"` (warning operators about
   cold-cache full-sync latency).

### B4: ClassifyIdentifier dispatch

**Package:** `mlwh/`
**File:** `mlwh/resolver.go`
**Test file:** `mlwh/resolver_test.go`

```go
func (c *Client) ClassifyIdentifier(ctx context.Context, raw string) (Match, error)
```

**Acceptance tests:**

1. Given `raw = "b7daafb8-c59f-11ee-8fba-024224dd57f4"`, when `Classify`
   runs, then only UUID-keyed queries are executed (study UUID first, then
   sample UUID), no integer-keyed or text-keyed queries are issued, and the
   returned `Kind` is whichever UUID column matched.
2. Given `raw = "12345"`, when `Classify` runs, then queries hit
   `id_study_lims` then `id_sample_lims` then `id_run` in that order, no
   text-only columns are queried, and the first hit wins.
3. Given `raw = "EGAS00001005445"` (text), when `Classify` runs, then it
   tries `accession_number` on `study` first; on a hit `Kind =
KindStudyAccession` and no further queries run.
4. Given `raw = "SQSCP"`, when `Classify` runs, then no SQL executes and
   the error is `ErrUnsupportedIdentifier`.
5. Given a text input that matches both `study.name` and a `donor_id`,
   when `Classify` runs, then the study match returns and donor_id is never
   queried (study-before-sample priority).
6. Given `raw = "12345"` and the MySQL replica returning a 5xx-class
   server error (e.g. ER_LOCK_WAIT_TIMEOUT or a connection-reset error)
   on the dispatched `id_study_lims` query, when `Classify` runs, then
   the returned error wraps `ErrUpstreamImpaired` and not `ErrNotFound`,
   and no fallback to lower-priority columns is attempted.

## C. Hierarchy and Expansion

### C1: SamplesForStudy / SamplesForRun / SamplesForLibrary

**Package:** `mlwh/`
**File:** `mlwh/hierarchy.go`
**Test file:** `mlwh/hierarchy_test.go`

**Acceptance tests:**

1. Given `studyLimsID = "6568"`, a warm cache where `study_mirror`
   contains a row for `6568` and `library_samples` contains rows linking
   3 distinct `id_sample_tmp` values to `6568`, and `sample_mirror`
   carries the corresponding sample rows, when `SamplesForStudy(ctx,
"6568", 100, 0)` runs, then it returns those 3 `Sample` records
   ordered by `sample_mirror.name`, the SQL is a join of
   `library_samples` to `sample_mirror` on `id_sample_tmp` filtered by
   `library_samples.id_study_lims = ?`, and zero queries reach the MLWH
   replica (verified by `sqlmock` zero expectations on the MLWH handle).
2. Given `studyLimsID = "6568"` does not exist in `study_mirror` and the
   cold-cache MLWH fallback returns zero study rows, when
   `SamplesForStudy` runs, then the error is `ErrNotFound`.
3. Given `studyLimsID = "6568"` exists in `study_mirror` but has zero
   linked rows in `library_samples`, when `SamplesForStudy` runs, then
   it returns an empty slice and a nil error.
4. Given `idRun = 12345` and `iseq_product_metrics` joined to
   `iseq_flowcell` and `sample`, when `SamplesForRun` runs, then the SQL
   uses `iseq_product_metrics.id_run = ?` and returns distinct samples.
5. Given `pipelineIDLims = "Standard"` and `studyLimsID = "6568"` and a
   warm `library_samples` cache, when `SamplesForLibrary` runs, then the
   SQL reads only from `library_samples` and `sample_mirror`, never from
   `iseq_flowcell` or MLWH `sample`, and is bounded by the supplied
   `limit`.
6. Given `limit = 2, offset = 1` against a 5-row source, when each method
   runs, then exactly 2 rows are returned and the SQL contains `LIMIT 2
OFFSET 1`.
7. Given a parent identifier that does not exist in the corresponding
   cache mirror (`study_mirror` for `SamplesForStudy`,
   `iseq_product_metrics` for `SamplesForRun`, `library_samples` for
   `SamplesForLibrary`), when each method runs, then the error is
   `ErrNotFound`.
8. Given a parent identifier that exists but has no children, when each
   method runs, then it returns an empty slice and a nil error.

### C2: LibrariesForStudy, RunsForStudy, LanesForSample, IRODSPathsFor\*

**Package:** `mlwh/`
**File:** `mlwh/hierarchy.go`
**Test file:** `mlwh/hierarchy_test.go`

**Acceptance tests:**

1. Given study `6568` with samples spanning two distinct
   `pipeline_id_lims` values (`Standard` x10, `Bespoke` x3), when
   `LibrariesForStudy` runs, then it returns two `Library` rows with
   `SampleCount = 10` and `3` respectively, ordered by `pipeline_id_lims`.
   `SampleCount` is the number of distinct `id_sample_tmp` values per
   `(pipeline_id_lims, id_study_lims)` (regression from
   `260501-4.md`).
2. Given study `6568` with `iseq_product_metrics` rows for runs `100` and
   `101`, when `RunsForStudy` runs, then the result is two `Run` rows with
   `IDRun` 100 and 101 (presence of metrics rows is implicit in any
   `Run` returned by hierarchy and resolver methods, since `ResolveRun`
   requires a metrics row).
3. Given `sangerName = "7607STDY14643771"` and three product-metrics rows
   with `(id_run, position, tag_index) = (100,1,0), (100,2,0), (101,1,5)`,
   when `LanesForSample` runs, then it returns three `Lane` rows in that
   order.
4. Given a sample with two `seq_product_irods_locations` rows, when
   `IRODSPathsForSample` runs, then it returns two `IRODSPath` records each
   with `IRODSPath` equal to `collection + "/" + data_object` and the SQL
   joins via `id_iseq_product`.
5. Given a parent identifier that does not exist (study not in
   `study_mirror`, sample not in `sample_mirror`), when any of
   `LibrariesForStudy`, `RunsForStudy`, `LanesForSample`,
   `IRODSPathsForSample`, `IRODSPathsForStudy`, `StudyForSample` runs,
   then the error is `ErrNotFound`.
6. Given a parent identifier that exists but has no children, when any
   of `LibrariesForStudy`, `RunsForStudy`, `LanesForSample`,
   `IRODSPathsForSample`, `IRODSPathsForStudy` runs, then the result is
   an empty slice and a nil error.

### C3: ExpandIdentifier

**Package:** `mlwh/`
**File:** `mlwh/hierarchy.go`
**Test file:** `mlwh/hierarchy_test.go`

**Acceptance tests:**

1. Given `kind = KindStudyLimsID, canonical = "6568"` and 2 samples (`A`,
   `B`) each with one lane, when `ExpandIdentifier` runs, then the result
   contains `{KindStudyLimsID, "6568"}`, `{KindSangerSampleName, "A"}`,
   `{KindSangerSampleName, "B"}`, and one `{KindRunID, "<n>"}` entry per
   distinct lane's run, in deterministic sorted order.
2. Given `kind = KindLibraryType, canonical = "Standard"`, when
   `ExpandIdentifier` runs, then the result includes the original tag plus
   one tag per sample plus one tag per distinct run those samples were
   sequenced on.
3. Given `kind = KindSangerSampleName, canonical = "A"`, when
   `ExpandIdentifier` runs, then the result contains the original plus one
   `KindRunID` tag per distinct lane.
4. Given `kind = KindRunID, canonical = "100"`, when `ExpandIdentifier`
   runs, then the result contains the original plus one
   `KindSangerSampleName` per distinct sample on that run.
5. Given a study with 1 sample and 1 lane, when `ExpandIdentifier` is
   measured against an in-memory cache, then it completes in under
   100 milliseconds and emits no more than 4 SQL statements total.
6. Given `ExpandIdentifier(ctx, KindStudyLimsID, "6568")` has just
   returned a result, when it is called again with the same
   `(kind, canonical)` within `expandIdentifierTTL` (5 minutes), then
   zero MLWH and zero cache-DB queries are issued (verified by
   `sqlmock` zero expectations on both handles) and the returned slice
   equals the first call's slice.
7. Given a cached `ExpandIdentifier` result, when `Sync(ctx, "sample")`
   commits successfully, then a subsequent identical
   `ExpandIdentifier` call re-issues queries (cache invalidated on
   sync commit).

### C4: AllStudies (cache-backed enumeration for diff)

**Package:** `mlwh/`
**File:** `mlwh/hierarchy.go`
**Test file:** `mlwh/hierarchy_test.go`

```go
func (c *Client) AllStudies(ctx context.Context, limit, offset int) ([]Study, error)
```

**Acceptance tests:**

1. Given a warm cache mirror of `study` (`study_mirror`) containing 3
   rows (`id_study_lims` = `"6566"`, `"6567"`, `"6568"`, all `id_lims =
'SQSCP'`), when `AllStudies(ctx, 100, 0)` runs, then it returns those
   3 `Study` records ordered by `id_study_lims` ascending and no SQL is
   issued against the MLWH replica (verified by `sqlmock` zero
   expectations).
2. Given a cold cache and an `sqlmock` MLWH that returns 2 study rows,
   when `AllStudies` runs, then the executed SQL matches `^SELECT .* FROM
study WHERE id_lims = 'SQSCP' ORDER BY id_study_lims LIMIT \? OFFSET
\?$`, the rows are returned in `id_study_lims` ascending order, and
   the watermark in `sync_state` for `study` is unchanged (read-through,
   not a sync).
3. Given a warm cache where `Sync(ctx, "study")` has just advanced
   `sync_state.study.high_water = t3`, when `AllStudies` runs, then the
   returned set includes the row inserted at `t3` (read-after-write
   ordering with `Sync`).
4. Given `limit = 2, offset = 1` against a 5-row cache, when
   `AllStudies` runs, then exactly 2 rows are returned and the executed
   SQL contains `LIMIT 2 OFFSET 1`.
5. Given the cache contains a non-`SQSCP` study row alongside `SQSCP`
   rows, when `AllStudies` runs, then only `SQSCP` rows are returned.
6. Given a cold cache and an `sqlmock` MLWH that returns 2 study rows on
   the first call, when `AllStudies(ctx, 100, 0)` runs and then is
   called a second time with identical arguments, then the first call
   issues exactly one MLWH `SELECT` and upserts both rows into
   `study_mirror` (the cache's study mirror) without advancing
   `sync_state.study.high_water`, and the second call issues zero MLWH
   queries and returns the same two rows from `study_mirror`
   (read-through write-back).

## D. seqmeta Repointing

### D1: Provider interface and types swap

**Package:** `seqmeta/`
**File:** `seqmeta/provider.go`, `seqmeta/types.go`
**Test file:** `seqmeta/provider_test.go`, `seqmeta/types_test.go`

**Acceptance tests:**

1. Given the `seqmeta` package source after the swap, when `go vet ./...`
   runs, then there are zero `import` lines mentioning
   `github.com/wtsi-hgi/wa/saga`.
2. Given `seqmeta.IdentifierType` constants, when listed, then the set is
   exactly `{study_lims_id, study_accession, study_uuid, study_name,
sanger_sample_name, sanger_sample_id, sample_lims_id, sample_uuid,
sample_accession, supplier_name, donor_id, run_id, library_type}`;
   `project_name` is absent.
3. Given `seqmeta.EnrichmentGraph`, when reflected, then it has no
   `Project` or `Users` fields and its `Sample`/`Samples` fields are typed
   `mlwh.Sample`.

### D2: Validate via mlwh.ClassifyIdentifier

**Package:** `seqmeta/`
**File:** `seqmeta/validate.go`
**Test file:** `seqmeta/validate_test.go`

```go
func Validate(ctx context.Context, p Provider, identifier string) (*IdentifierResult, error)
```

**Acceptance tests:**

1. Given a fake `Provider` whose `ClassifyIdentifier` returns
   `Match{Kind: KindStudyLimsID, Canonical: "6568", Study: &mlwh.Study{...}}`,
   when `Validate(ctx, p, "6568")` runs, then the result has `Type =
IdentifierStudyLimsID`, `Identifier = "6568"`, and `Object` is the
   `mlwh.Study` value.
2. Given a `Provider` whose `ClassifyIdentifier` returns
   `ErrUnsupportedIdentifier` for `"SQSCP"`, when `Validate` runs, then the
   error wraps `ErrUnsupportedIdentifier` and the message names `"SQSCP"`.
3. Given a `Provider` whose `ClassifyIdentifier` returns `ErrNotFound`,
   when `Validate` runs, then the error wraps `ErrUnknownIdentifier` (HTTP
   404 mapping preserved).
4. Given a `Provider` returning `ErrUpstreamImpaired`, when the seqmeta
   server's `/validate/{id}` handler invokes `Validate`, then the response
   status is 502.
5. Given a `Provider` whose `ClassifyIdentifier` returns
   `Match{Kind: KindSangerSampleName, Canonical: "7607STDY14643771",
Sample: &mlwh.Sample{...}}`, when `Validate` runs, then `Type =
IdentifierSangerSampleName`, `Identifier = "7607STDY14643771"`, and
   `Object` is the `*mlwh.Sample` value.
6. Given a `Provider` whose `ClassifyIdentifier` returns
   `Match{Kind: KindRunID, Canonical: "12345", Run: &mlwh.Run{IDRun:
12345}}`, when `Validate` runs, then `Type = IdentifierRunID`,
   `Identifier = "12345"`, and `Object` is the `*mlwh.Run` value.
7. Given a `Provider` whose `ClassifyIdentifier` returns
   `Match{Kind: KindLibraryType, Canonical: "Standard", Library:
&mlwh.Library{PipelineIDLims: "Standard"}}`, when `Validate` runs,
   then `Type = IdentifierLibraryType`, `Identifier = "Standard"`, and
   `Object` is the `*mlwh.Library` value.

### D3: Enrichment graph from mlwh details

**Package:** `seqmeta/`
**File:** `seqmeta/enrich.go`
**Test file:** `seqmeta/enrich_test.go`

**Acceptance tests:**

1. Given a `Provider` whose study lookup returns `mlwh.StudyDetail` with
   2 libraries (`Standard`, `Bespoke`), 5 samples total, and 2 runs, when
   `Enrich(ctx, "6568")` runs, then the JSON response under
   `graph.study_detail.library_details` has length 2, total samples across
   the libraries is 5, and `graph.study_detail.study.id_study_lims` is
   `"6568"`.
2. Given a `Provider` whose sample lookup returns `mlwh.SampleDetail` with
   3 lanes, when `Enrich(ctx, "7607STDY14643771")` runs, then
   `graph.sample_detail.lanes` has length 3 and `graph.sample_detail.sample.
sanger_id` is `"7607STDY14643771"`.
3. Given an enrichment response, when JSON-marshalled, then the top-level
   `graph` object has no `project` or `users` keys.
4. Given `Provider.SamplesForLibrary` returns 1500 rows, when the library
   enrichment hop runs, then the response sets `partial = true` and adds
   one `MissingHop{Hop: HopLibraries, Reason: ReasonSamplesTruncated}`,
   and the returned `samples` array has length exactly 1000.

### D4: Diff routes through cache

**Package:** `seqmeta/`
**File:** `seqmeta/diff.go`, `seqmeta/server.go`
**Test file:** `seqmeta/diff_test.go`

**Acceptance tests:**

1. Given a fresh watermarks table and `Provider.AllStudies` returning two
   studies, when `GET /diff/study/all` (or its existing route) runs, then
   the response `added` has length 2, `modified` is empty, `removed` is
   empty.
2. Given the same studies on a second poll with one study's name changed,
   when the diff route runs, then `modified` has length 1 with the changed
   `IDStudyLims`.
3. Given study `6568` removed from `Provider.AllStudies` between polls,
   when the diff route runs, then `removed` includes `"6568"` and the
   underlying tombstone is set.
4. Given `Provider.SamplesForStudy(ctx, "6568")` is implemented over
   `library_samples` joined to `sample_mirror`, when `GET /diff/study/6568`
   runs, then no MLWH query is issued (zero expectations on the MLWH
   `sqlmock`).
5. Given a fresh watermarks table and `Provider.IRODSPathsForSample(ctx,
"7607STDY14643771")` returning two `mlwh.IRODSPath` rows, when `GET
/diff/sample/7607STDY14643771` runs, then the response `added` has
   length 2 (each entry shaped `{id_product, collection, data_object,
irods_path}` - the deliberate minimum subset of
   `seq_product_irods_locations` columns; columns the table also has but
   we do not surface, e.g. `pid_lock`, `deleted_at`, are intentionally
   excluded), `modified` and `removed` are empty, and the route resolved
   the sample via `Provider.SamplesForStudy` /
   `Provider.IRODSPathsForSample` (no `saga.IRODSFile` shape, no AVU /
   checksum / size fields).
6. Given the same sample on a second poll where one iRODS path was
   replaced (different `id_product`, same `irods_path`), when the diff
   route runs, then `removed` has length 1 with the prior `id_product`,
   `added` has length 1 with the new `id_product`, and `modified` is
   empty.
7. Given `Provider.IRODSPathsForSample(ctx, "missing")` returns
   `mlwh.ErrNotFound`, when `GET /diff/sample/missing` runs, then the
   HTTP status is 404 and the response body names the identifier.

## E. CLI Integration

### E1: `wa results register` uses mlwh resolvers

**Package:** `cmd/`
**File:** `cmd/results.go`
**Test file:** `cmd/results_register_test.go`

**Acceptance tests:**

1. Given `wa results register --sample 7607STDY14643771` and an `mlwh`
   client whose `ResolveSample` returns `Match{Canonical:
"7607STDY14643771"}`, when the command runs, then the registered result
   set has `meta["seqmeta_sampleid"] = "7607STDY14643771"`.
2. Given `--sample SQSCP` and an `mlwh` client, when the command runs,
   then the exit code is non-zero, stderr names `"--sample"`, `"SQSCP"`,
   and contains `"LIMS provider constant"`, and no result set is
   registered.
3. Given `--sample missing-id` and `mlwh.ResolveSample` returns
   `ErrNotFound`, when the command runs, then stderr contains `"--sample
\"missing-id\""` and `"not found"`, and the exit code is non-zero.
4. Given `--study EGAS00001005445`, `--run 12345`, `--library Standard`,
   `--sample 7607STDY14643771`, when the command runs with all four
   resolvers succeeding, then the registered metadata equals
   `{seqmeta_studyid:"6568", seqmeta_runid:"12345",
seqmeta_librarytype:"Standard", seqmeta_sampleid:"7607STDY14643771"}`.
5. Given the help output of `wa results register`, when printed, then it
   contains the input forms each shorthand accepts (Sanger name, supplier
   name, `id_sample_lims`, sample UUID, donor ID for `--sample`; LIMS ID,
   accession, UUID, name for `--study`; numeric for `--run`; exact for
   `--library`) and contains no occurrence of the substring `"Saga"` or
   `"SAGA"`.
6. Given the help output of `wa results register`, when printed, then
   the description of `--library` contains the substring `"first call"`
   and the substring `"wa mlwh sync"` (warning operators about the
   cold-cache full-sync latency on the first `--library` lookup).

### E2: `wa seqmeta serve` flag rewiring

**Package:** `cmd/`
**File:** `cmd/seqmeta.go`
**Test file:** `cmd/seqmeta_test.go`

**Acceptance tests:**

1. Given the registered `seqmeta` command, when `--help` is rendered, then
   the output contains `--mlwh-cache` and `--mlwh-sync-interval` and does
   not contain `--token` or `--base-url`.
2. Given `WA_MLWH_DSN`, `WA_MLWH_PASSWORD`, `WA_MLWH_CACHE_PATH` set in
   the environment and no flag overrides, when `wa seqmeta serve`
   bootstraps, then the resolved MLWH DSN passed to `sql.Open` has the
   password injected from the env var, never appears in `os.Args`, and
   the cache backend uses the env-supplied path.
3. Given `--mlwh-cache "user:pass@tcp(host)/db"` on the command line,
   when the command parses flags, then it exits non-zero with an error
   matching `ErrPasswordInDSN` and `--mlwh-cache` is named in the
   message.
4. Given `--mlwh-sync-interval=5m`, when the server starts, then a sync
   goroutine is launched whose first sync runs within 5 minutes
   (verified with a fake clock and a mock `Sync`).

### E3: New `wa mlwh sync` command

**Package:** `cmd/`
**File:** `cmd/mlwh.go`
**Test file:** `cmd/mlwh_test.go`

**Acceptance tests:**

1. Given a configured `mlwh.Client` with mocked Sync returning reports for
   `sample`, `study`, `iseq_flowcell`, when `wa mlwh sync` runs, then the
   exit code is zero and stdout names each table and its inserted/updated
   counts.
2. Given a missing `WA_MLWH_DSN`, when `wa mlwh sync` runs, then the exit
   code is non-zero and stderr names `WA_MLWH_DSN`.
3. Given `wa mlwh sync --tables sample`, when it runs, then only the
   `sample` table is synced and the report list has length 1.

### E4: Deletion sweep

**Package:** repo-wide
**File:** all
**Test file:** `cmd/saga_test.go` deleted; `cmd/results_register_test.go`
adjusted

**Acceptance tests:**

1. Given the repo after the change, when
   `grep -rn "\"github.com/wtsi-hgi/wa/saga\"" -- '*.go'` runs, then the
   match count is 0.
2. Given the repo after the change, when
   `grep -rn -E "SAGA_API_TOKEN|SAGA_TEST_API_TOKEN|SAGA_API_BASE_URL"`
   runs (excluding `.docs/`), then the match count is 0.
3. Given the repo after the change, when
   `grep -rni "saga" -- 'cmd/*.go' 'seqmeta/*.go' 'results/*.go'
'frontend/components/**/*.{ts,tsx}' 'frontend/lib/**/*.{ts,tsx}'`
   runs, then the match count is 0.
4. Given the file tree after the change, when
   `ls saga/ cmd/saga.go cmd/saga_test.go` runs, then each path returns
   "No such file".
5. Given the file tree after the change, when `ls .docs/saga/`,
   `ls .docs/results-seqmeta/`, and `ls .docs/seqmeta/` run, then each
   path returns "No such file" (the historical specs and their phase
   plans are removed; this spec supersedes them and the useful content
   from `.docs/seqmeta/` has been folded into this spec, per the
   prompt).
6. Given the repo after the change, when
   `grep -rnE "\\b(HopProject|IdentifierProjectName|Project)\\b" --
'seqmeta/*.go'` runs, then the match count is 0 (no `Project`,
   `HopProject`, or `IdentifierProjectName` symbols remain in
   `seqmeta/`).
7. Given the repo after the change, when `grep -n "SAGA_"` is run
   against each of `.env.development`, `.env.test`, `.env.production`,
   `.env.development.local` (where present), `Makefile`, `run-dev.sh`,
   `README.md`, `DEVELOPING.md`, and every file under `.github/` (the
   CI configuration), then every file's match count is 0.
8. Given the frontend tree after the change, when
   `grep -rnE "projectSchema|projectUserSchema|HopProject|via Saga|SAGA"
-- 'frontend/'` runs (excluding `frontend/node_modules`, `pnpm-lock`,
   `e2e` snapshots, and generated build output), then the match count
   is 0.

### E5: `wa results serve` MLWH flag rewiring

**Package:** `cmd/`
**File:** `cmd/results.go`
**Test file:** `cmd/results_serve_test.go`

**Acceptance tests:**

1. Given the registered `wa results serve` command, when `--help` is
   rendered, then the output contains the flag names `--mlwh-cache` and
   `--mlwh-sync-interval` and a description of each.
2. Given `WA_MLWH_DSN`, `WA_MLWH_PASSWORD`, and `WA_MLWH_CACHE_PATH`
   set in the environment and no flag overrides, when `wa results
serve` bootstraps, then the resolved MLWH DSN passed to `sql.Open`
   has the password injected from the env var, the password never
   appears in `os.Args` or in any logged line, and the cache backend
   uses the env-supplied path.
3. Given `--mlwh-cache "user:pass@tcp(host)/db"` on the command line,
   when `wa results serve` parses flags, then it exits non-zero with
   an error wrapping `mlwh.ErrPasswordInDSN` and the message names
   `--mlwh-cache`.
4. Given `--mlwh-sync-interval=5m`, when `wa results serve` starts,
   then exactly one sync goroutine is launched whose first invocation
   of `mlwh.Client.Sync` happens at most 5 minutes after start
   (verified with a fake clock and a mock `Sync`), and stopping the
   server cancels the goroutine's context (verified by the goroutine
   exiting before `serve` returns).
5. Given `--mlwh-sync-interval=0` (default) and the server running,
   when `mlwh.Client.Sync` is observed for one minute against a mock
   clock, then it is invoked zero times (sync is opt-in only).

### E6: Env scenario guards for `WA_MLWH_*`

**Package:** `cmd/`
**File:** `run-dev.sh`, `cmd/root.go`
**Test file:** `cmd/run_dev_modes_test.go`

The same scenario guards that protect `WA_RESULTS_DB_*` apply to
`WA_MLWH_DSN`, `WA_MLWH_PASSWORD`, `WA_MLWH_CACHE_PATH`, and
`WA_MLWH_CACHE_PASSWORD`: `make test` / `--mode test` refuses inherited
dev/prod values, `make dev` / `--mode dev` requires its own values to
be present, `make prod` / `--mode prod` refuses inherited test/dev
values. New tests follow the existing pattern in
`cmd/run_dev_modes_test.go` (around lines 65 to 80) using
`runRunDevExpectingFailureForTest`.

**Acceptance tests:**

1. Given `--mode dev` and an environment without `WA_MLWH_DSN`, when
   `run-dev.sh` runs, then it exits non-zero and stderr contains
   `WA_MLWH_DSN`.
2. Given `--mode test` and `WA_MLWH_CACHE_PATH=/var/lib/wa/mlwh.sqlite`
   inherited in the environment, when `run-dev.sh` runs, then it
   exits non-zero and stderr contains `WA_MLWH_CACHE_PATH`.
3. Given `--mode test` and
   `WA_MLWH_DSN=mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse` inherited
   in the environment, when `run-dev.sh` runs, then it exits non-zero
   and stderr contains `WA_MLWH_DSN`.
4. Given `--mode prod` (`WA_ENV=production` set) and
   `WA_MLWH_CACHE_PATH=/tmp/wa-test-mlwh.sqlite` (a test-shaped value
   under `/tmp` or matching the test fixture path), when `run-dev.sh`
   runs, then it exits non-zero and stderr contains
   `WA_MLWH_CACHE_PATH`.
5. Given `--mode prod` and
   `WA_MLWH_DSN=mlwh_test@tcp(localhost:3306)/mlwarehouse_test`
   inherited (a development/test-shaped value), when `run-dev.sh` runs,
   then it exits non-zero and stderr contains `WA_MLWH_DSN`.
6. Given `--mode prod` and `WA_MLWH_PASSWORD` set to the literal value
   used in `.env.test` / `.env.development`, when `run-dev.sh` runs,
   then it exits non-zero and stderr names `WA_MLWH_PASSWORD`.
7. Given `--mode test` with no `WA_MLWH_*` inherited and the test
   `.env.test` values resolved, when `run-dev.sh` boots the test
   stack, then it succeeds and the resolved `WA_MLWH_CACHE_PATH`
   points under `.tmp/` (ephemeral test cache).

## F. Frontend Cleanup

### F1: Project schemas removed

**Package:** `frontend/`
**File:** `frontend/lib/contracts.ts`
**Test file:** `frontend/tests/contracts.test.ts`

**Acceptance tests:**

1. Given the updated module, when imported, then `projectSchema`,
   `projectUserSchema`, `Project`, `ProjectUser` are not exported (TS
   compile error if referenced).
2. Given an `enrichmentGraphSchema.parse({...})` call with a payload
   containing extra `project` or `users` fields, then they are stripped
   silently (zod default `.optional()` removed; not present at all in the
   schema).
3. Given the frontend build, when `pnpm tsc --noEmit` runs, then it
   completes with zero errors.

### F2: Seqmeta badge no longer renders project rows

**Package:** `frontend/`
**File:** `frontend/components/seqmeta-badge.tsx`
**Test file:** `frontend/tests/seqmeta-badge.test.ts`

**Acceptance tests:**

1. Given an enrichment payload with a study and libraries (no project),
   when the badge renders, then no row labelled "Project" or "Project
   users" appears in the DOM.
2. Given the badge source, when grepped, then no occurrence of "Saga" or
   "via Saga" remains.

### F3: iRODS file contract tightened

**Package:** `frontend/`
**File:** `frontend/lib/contracts.ts`,
`frontend/components/result-detail-files.tsx`
**Test file:** `frontend/tests/result-detail-files.test.tsx`

```ts
export const irodsPathSchema = z.object({
    id_product: z.string(),
    collection: z.string(),
    data_object: z.string(),
    irods_path: z.string(),
});
```

**Acceptance tests:**

1. Given an enrichment payload whose iRODS entries carry only the four
   fields above, when parsed, then it succeeds and the `IRODSPath` array
   has the expected length.
2. Given a payload where an iRODS entry includes legacy `metadata`,
   `checksum`, or `size` fields, when parsed, then those fields are
   stripped (not exposed on the typed object).
3. Given the rendered file detail card, when inspected, then no DOM node
   labelled "Checksum", "Size", or "AVU" appears.

### F4: Hierarchical-search docs warn about first-call latency

**Package:** `frontend/`
**File:** `frontend/components/filter-builder.tsx` (or the equivalent
hierarchical-search docs surface: the help text rendered next to the
search filter input, plus the JSDoc on the search-expansion helper)
**Test file:** `frontend/tests/filter-builder.test.tsx`

The frontend hierarchical-search documentation (the help/info text the
user sees when picking `library=` filters and the JSDoc on the
TypeScript helper that calls `mlwh.ExpandIdentifier` indirectly via the
results search API) must mirror the resolver doc string and the
register `--library` help: the first-ever lookup of a `library=` filter
warms a cold cache and may take noticeably longer than later lookups,
and operators are advised to run `wa mlwh sync` ahead of time.

**Acceptance tests:**

1. Given the rendered hierarchical-search help/info element for the
   `library=` filter dimension, when its text content is inspected,
   then it contains the substring `"first call"` and the substring
   `"wa mlwh sync"`.
2. Given the source of the helper module that wraps the search API
   call, when grepped for its JSDoc block above the exported function,
   then the JSDoc contains the substring `"first call"` and the
   substring `"wa mlwh sync"`.
3. Given the same docs surface, when grepped, then no occurrence of
   `"Saga"` or `"via Saga"` remains.

## G. Hierarchical Search Regressions

### G1: Study search expands via mlwh

**Package:** `results/`
**File:** `results/server.go`
**Test file:** `results/server_test.go`

**Acceptance tests:**

1. Given two registered result sets, A tagged `seqmeta_studyid=6568`, B
   tagged `seqmeta_sampleid=7607STDY14643771` where MLWH says that sample
   belongs to study 6568, when the search layer queries `study=6568`,
   then both A and B are returned. The SQL uses an `OR` group over the
   resolver-expanded sample IDs.
2. Given a third result set C tagged `seqmeta_lane=12345/1/0` where MLWH
   says lane `12345/1/0` belongs to study 6568 via its sample, when the
   same search runs, then C is also returned.
3. Given the search above is repeated within 5 minutes, when the second
   call runs, then `mlwh.ExpandIdentifier` is invoked at most once
   (verified by a counter on the mock client).
4. Given a search for `library=Standard` and result sets tagged with
   sample IDs whose libraries include `Standard`, when the search runs,
   then those result sets are returned within 1 second wall-clock against
   the in-memory cache fixture.

## Implementation Order

Phases are sequential unless marked parallel.

1. **Foundation:** A1, A2, A3. Build the cache, schema, sync engine. No
   resolver work yet.
2. **Resolvers:** A4, B1, B2, B3, B4. Build the resolver cascade and
   classifier on top of the foundation. **B1, B2, B3 may proceed in
   parallel** after A4.
3. **Hierarchy:** C1, C2, C3, C4. **C1, C2, and C4 in parallel**, then
   C3.
4. **seqmeta swap:** D1, D2, D3, D4. **D1 must come first**; D2, D3, D4
   in parallel afterwards.
5. **CLI:** E1, E2, E3, E5, E6. Parallel.
6. **Frontend:** F1, F2, F3, F4. Parallel.
7. **Final sweep:** E4 (deletion of `saga/`, `cmd/saga.go`, all
   `SAGA_*` env vars, the `.docs/saga/`, `.docs/results-seqmeta/`,
   and `.docs/seqmeta/` directories, project symbols in `seqmeta/`,
   and frontend project references). Performed after every package
   above is green.
8. **Search regressions:** G1 verified end-to-end against the new code
   path.

Live-MLWH integration tests run last and only when `WA_MLWH_DSN` is set
in the developer's environment.

## Appendix: Key Decisions

### Why a separate package, not a `seqmeta/mlwh` subpackage

`mlwh/` is a self-contained warehouse client with its own resolver and
cache. `seqmeta/` is a domain layer that consumes it. Keeping them
separate lets `cmd/results.go`'s `register` resolvers consume `mlwh/`
without pulling in seqmeta's HTTP server, watermarks, or enrich graphs.

### Why first-hit-wins in resolvers

Detecting cross-column conflicts (e.g. a string that matches both a
supplier name and a donor ID) would require touching every cascade
column on every call, doubling the query budget. The bug report's
inputs are all unambiguous in the canonical column. The classifier
applies a fixed priority instead.

### Why per-dialect SQL files plus a parity test

One in-memory abstraction risks shipping syntax that works in the dev
backend (SQLite) and breaks in production (MySQL). Embedded `.sql` per
dialect is reviewable, the parity test prevents drift, and the matrix
harness exercises both backends.

### Why `WA_MLWH_*` over a subset of `WA_RESULTS_DB_*`

The MLWH replica and the wa results database are separate physical
systems with independent credentials and backup policies. They share
the env-var pattern but not the variable.

### Cold-cache behaviour

Lazy full sync on first call to a cache-backed resolver was chosen over
fail-fast because the failure mode would otherwise be a brand-new
deployment refusing every `--library` lookup until cron caught up.
First-call latency is documented in three places per the prompt: the
resolver's own Go doc string (asserted in B3), the `wa results
register --library` help text (asserted in E1), and the frontend
hierarchical-search docs (asserted in F4).

### Testing references

Unit and integration tests follow `go-conventions` (GoConvey,
`So()` only, `t.TempDir()`, no shared mutable state). The implementor
follows `go-implementor`; the reviewer follows `go-reviewer`. Frontend
tests use Vitest as already established in `frontend/`.

### Lessons from prior attempts

Phase coverage maps directly onto the bugfix IDs in the prompt: B1/B2/B3
close `260501-3.md` (input-form regression for `--sample`/`--study`
register); C2's `LibrariesForStudy` test closes `260501-4.md` (distinct
libraries with sample counts); C3 + G1 close `260501-5.md` and
`260503-1.md` (hierarchical search expansion for study/library/sample);
D3/D4 close `260506-1.md` (enrich/diff routes serving the tightened
iRODS contract without project entities).
