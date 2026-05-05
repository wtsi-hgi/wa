# Results Seqmeta Enrichment Specification

## Overview

Extends `saga` with targeted MLWH sample lookups and extends `seqmeta`
with a graph-shaped enrichment endpoint (`GET /enrich/{identifier}`).
Replaces the current 5/8-broken cascade in `seqmeta.Validate` that
relies on `AllSamples()` (HTTP 500 upstream) with bounded, filter-based
queries, a cross-process SQLite enrichment cache alongside the existing
watermarks table, and a partial-graph response model that distinguishes
"unknown identifier" (404) from "upstream impaired" (502).

The existing `/validate/{identifier}` endpoint, `IdentifierResult`
shape, and all `seqmeta` diff machinery (`/diff/study/{id}`,
`/diff/sample/{id}`, watermarks, tombstones, `DiffStudySamples`,
`DiffSampleFiles`) are preserved unchanged. `/validate/` remains the
thin classifier used by list/search rows; `/enrich/` is consumed by the
result-detail view only.

## Architecture

### Packages and files

- `saga/mlwh.go` - add `FindSamplesBy*` methods (new MLWH filter keys).
- `saga/usecases.go` - add helpers for study-from-sample resolution.
- `saga/integration_test.go` - add early-probe filter-support test.
- `seqmeta/provider.go` - extend `SAGAProvider` with new methods.
- `seqmeta/client_adapter.go` - implement new methods.
- `seqmeta/provider_mock_test.go` - extend `MockProvider`.
- `seqmeta/types.go` - add enrichment types and sentinel errors.
- `seqmeta/enrich.go` (new) - enrichment algorithm per `IdentifierType`.
- `seqmeta/enrich_test.go` (new) - unit tests for enrichment.
- `seqmeta/enrich_cache.go` (new) - SQLite enrichment cache.
- `seqmeta/enrich_cache_test.go` (new) - cache tests.
- `seqmeta/server.go` - register `/enrich/*` routes; TTL options.
- `seqmeta/server_enrich_test.go` (new) - server-level enrichment tests.
- `seqmeta/integration_test.go` (new) - gated real-API matrix.
- `frontend/lib/contracts.ts` - add enrichment schemas.
- `frontend/lib/seqmeta-enrichment.ts` - graph-aware state.
- `frontend/app/(results)/actions.ts` - `enrichIdentifier` action.
- `frontend/components/result-metadata-enrichment.tsx` - consume graph.
- `frontend/components/seqmeta-badge.tsx` - partial-graph banner.
- `frontend/tests/seqmeta-enrichment.test.ts` (new) - Vitest.
- `frontend/tests/seqmeta-badge.test.ts` - extend with partial cases.

### New saga types

No new exported types. New MLWH filter-key constants are unexported
in `saga/mlwh.go`:

```go
const (
    mlwhFilterSangerID        = "sanger_id"
    mlwhFilterIDSampleLims    = "id_sample_lims"
    mlwhFilterIDRun           = "id_run"
    mlwhFilterLibraryType     = "library_type"
    mlwhFilterAccessionNumber = "accession_number"
)
```

### New saga MLWH methods

All use the same `filters` JSON query parameter
`AllSamplesForStudy` uses, reusing `collectAllPages`:

```go
func (m *MLWHClient) FindSamplesBySangerID(ctx context.Context,
    sangerID string) ([]MLWHSample, error)
func (m *MLWHClient) FindSamplesByIDSampleLims(ctx context.Context,
    idSampleLims string) ([]MLWHSample, error)
func (m *MLWHClient) FindSamplesByRunID(ctx context.Context,
    idRun int) ([]MLWHSample, error)
func (m *MLWHClient) FindSamplesByLibraryType(ctx context.Context,
    libraryType string) ([]MLWHSample, error)
func (m *MLWHClient) FindSamplesByAccessionNumber(ctx context.Context,
    accessionNumber string) ([]MLWHSample, error)
```

Empty upstream responses return `([]MLWHSample{}, nil)` - never
`ErrNotFound`. Upstream 5xx / transport errors propagate unchanged.

### Study-from-sample helper

```go
// StudyForSample returns the MLWH study referenced by sample.IDStudyLims.
// Returns ErrNotFound if the study cannot be retrieved.
func (c *Client) StudyForSample(ctx context.Context,
    sample MLWHSample) (*Study, error)
```

Implemented as `c.MLWH().GetStudy(ctx, sample.IDStudyLims)`.

### Extended SAGAProvider

```go
type SAGAProvider interface {
    // ... existing methods unchanged ...
    FindSamplesBySangerID(ctx context.Context,
        sangerID string) ([]saga.MLWHSample, error)
    FindSamplesByIDSampleLims(ctx context.Context,
        idSampleLims string) ([]saga.MLWHSample, error)
    FindSamplesByRunID(ctx context.Context,
        idRun int) ([]saga.MLWHSample, error)
    FindSamplesByLibraryType(ctx context.Context,
        libraryType string) ([]saga.MLWHSample, error)
    FindSamplesByAccessionNumber(ctx context.Context,
        accessionNumber string) ([]saga.MLWHSample, error)
    StudyForSample(ctx context.Context,
        sample saga.MLWHSample) (*saga.Study, error)
    ListProjectStudies(ctx context.Context,
        projectID int) ([]saga.ProjectStudy, error)
    ListProjectSamples(ctx context.Context,
        projectID int) ([]saga.ProjectSample, error)
    ListProjectUsers(ctx context.Context,
        projectID int) ([]saga.ProjectUser, error)
}
```

`ClientAdapter` implements each by delegation to `c.client.MLWH()`
or `c.client.Projects()`. The existing `MockProvider` in
`seqmeta/provider_mock_test.go` gains function fields
`FindSamplesBySangerIDFn`, `FindSamplesByIDSampleLimsFn`,
`FindSamplesByRunIDFn`, `FindSamplesByLibraryTypeFn`,
`FindSamplesByAccessionNumberFn`, `StudyForSampleFn`,
`ListProjectStudiesFn`, `ListProjectSamplesFn`, and
`ListProjectUsersFn` with the same pattern as existing fields (nil
returns `([]..., nil)` or `nil, nil`).

### New seqmeta types

```go
// Library is a (library_type, id_study_lims) tuple scoped to a study.
type Library struct {
    LibraryType string `json:"library_type"`
    IDStudyLims string `json:"id_study_lims"`
}

// EnrichmentGraph is the flat graph envelope returned under "graph".
// Zero-valued fields are omitted from JSON.
type EnrichmentGraph struct {
    Study     *saga.Study        `json:"study,omitempty"`
    Studies   []saga.Study       `json:"studies,omitempty"`
    Sample    *saga.MLWHSample   `json:"sample,omitempty"`
    Samples   []saga.MLWHSample  `json:"samples,omitempty"`
    Library   *Library           `json:"library,omitempty"`
    Libraries []Library          `json:"libraries,omitempty"`
    Project   *saga.Project      `json:"project,omitempty"`
    Users     []saga.ProjectUser `json:"users,omitempty"`
}

// MissingHop records a hop that failed or was truncated.
type MissingHop struct {
    Hop    string `json:"hop"`
    Reason string `json:"reason"`
    Status int    `json:"status"`
}

// EnrichmentResult is the /enrich/{identifier} response body.
type EnrichmentResult struct {
    Identifier string          `json:"identifier"`
    Type       IdentifierType  `json:"type"`
    Graph      EnrichmentGraph `json:"graph"`
    Partial    bool            `json:"partial"`
    Missing    []MissingHop    `json:"missing,omitempty"`
}

// MaxLibrarySamples caps graph.samples fan-out for library-type
// identifiers. Not configurable.
const MaxLibrarySamples = 1000

var (
    ErrAllHopsFailed = errors.New("seqmeta: all enrichment hops failed")
)
```

### Hop names

Exported constants in `seqmeta/enrich.go`:

```go
const (
    HopClassify  = "classify"   // primary classification
    HopStudy     = "study"      // resolve study record
    HopSamples   = "samples"    // samples in study / run / library
    HopLibraries = "libraries"  // distinct libraries in study
    HopProject   = "project"    // project record
    HopUsers     = "users"      // users of project
    HopStudies   = "studies"    // studies via project or library
)
```

### Failure reason codes

Exported constants in `seqmeta/enrich.go`:

```go
const (
    ReasonUpstreamError     = "upstream_error"     // 5xx / transport
    ReasonNotFound          = "not_found"          // hop returned 404
    ReasonFilterUnsupported = "filter_unsupported" // probe says no
    ReasonSamplesTruncated  = "samples_truncated"  // library-type cap
)
```

### Status-code rules

- **200** - classification (`HopClassify`) succeeded. `partial` is
  true when one or more secondary hops populated `missing`; otherwise
  false. `graph` always contains the classified primary object.
- **404** - no hop could classify the identifier as any
  `IdentifierType`. Body `{"error":"seqmeta: unknown identifier"}`.
- **502** - every classification hop attempted failed with a
  transient/5xx upstream error (no primary resolution possible). Body
  `{"error":"seqmeta: all enrichment hops failed"}` plus a `missing`
  array describing which probes failed.
- **500** - internal/store error.
- **200** for `DELETE /enrich/{identifier}` when the cache entry is
  removed (or was absent); body `{"identifier":"<id>"}`. **500** on
  store error.

### Enrichment cache

Cross-process SQLite table in the existing database, managed via the
existing `Store`. Single-writer assumption: exactly one `seqmeta`
process owns the SQLite file (documented in Appendix).

```sql
CREATE TABLE IF NOT EXISTS enrich_cache (
    identifier    TEXT    NOT NULL PRIMARY KEY,
    type          TEXT    NOT NULL,          -- "" for negative cache
    body          BLOB    NOT NULL,          -- JSON EnrichmentResult
    fetched_at    TEXT    NOT NULL,          -- RFC3339Nano UTC
    ttl_seconds   INTEGER NOT NULL,
    negative      INTEGER NOT NULL DEFAULT 0,
    partial       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS enrich_cache_fetched_at_idx
    ON enrich_cache(fetched_at);
```

`OpenStore` creates the table alongside `watermarks` in the same
transaction path it already uses. All enrich cache reads/writes go
through `Store.WithLock`.

```go
type enrichCacheEntry struct {
    Identifier string
    Type       IdentifierType
    Body       []byte
    FetchedAt  time.Time
    TTL        time.Duration
    Negative   bool
    Partial    bool
}

func (s *Store) LoadEnrichCache(identifier string) (
    *enrichCacheEntry, error) // sql.ErrNoRows on miss
func (s *Store) SaveEnrichCache(e enrichCacheEntry) error
func (s *Store) DeleteEnrichCache(identifier string) error
```

**Expired entries:** the server never serves stale. On expiry
(`time.Since(FetchedAt) > TTL`), the entry is treated as a miss and
re-fetched. If the re-fetch fails, the normal 200/404/502 contract
applies; stale data is never returned.

**Invalidation on diff mutations:** `handleStudyDiff` and
`handleSampleDiff` call
`s.store.InvalidateEnrichFor(queryKind, queryID)` after a successful
watermark commit, which runs:

- `queryKind == "study_samples"`: delete entries whose `type` is
  `study_id`/`study_accession` and identifier matches `queryID`, and
  delete entries whose body's `graph.study.id_study_lims == queryID`
  (JSON extraction via a precomputed `id_study_lims` index column is
  out of scope; implement as `WHERE identifier = ? OR body LIKE ?`
  with the substring `"id_study_lims":"<queryID>"`).
- `queryKind == "sample_files"`: delete the entry where `identifier
== queryID` only (sample-file diffs do not invalidate other
  samples' graphs).

### TTL configuration

```go
type ServerOption func(*Server)

func WithEnrichTTL(success, negativeOrPartial time.Duration) ServerOption
func NewServer(provider SAGAProvider, store *Store,
    opts ...ServerOption) *Server
```

Defaults: `successTTL = 24 * time.Hour`, `negativeTTL = 15 * time.Minute`.
Partial responses (any `MissingHop`) use `negativeTTL`. Tests set
short TTLs (e.g. 10 ms) via the option. No env-var reads inside the
package.

### REST routes (extended)

- `GET /validate/{identifier}` - unchanged. Returns `IdentifierResult`.
- `GET /enrich/{identifier}` - new. Returns `EnrichmentResult`.
  Chi-wildcard-routed like `/validate/*` so URL-encoded slashes in the
  identifier are preserved.
- `DELETE /enrich/{identifier}` - new. Deletes the cache entry.
- `GET /diff/study/{id}`, `GET /diff/sample/{id}`, `GET /studies`,
  `GET /study/{id}/samples` - unchanged.

### Identifier -> algorithm

Each `IdentifierType` follows one path. "Classification" is the
primary hop that determines the type; all other hops are secondary.
Hops that fail with a client error (404 / 4xx-not-auth) produce a
`missing` entry with `reason: not_found`; hops that fail with 5xx or
transport errors produce `reason: upstream_error`. A hop that
returns an empty list is not a failure.

**1. study_id** (classify via `GetStudy(id)` succeeding):

1. `GetStudy(id)` -> `graph.study`. [classify hop]
2. `AllSamplesForStudy(study.IDStudyLims)` -> `graph.samples`.
3. Derive `graph.libraries` from `distinct(sample.LibraryType)`
   paired with `study.IDStudyLims` - no upstream call.

**2. study_accession** (classify via `AllStudies()` scan):

1. `AllStudies()` matches on `AccessionNumber`. [classify hop]
   2-3. Same as study_id.

**3. sanger_sample_id** (classify via
`FindSamplesBySangerID(id)` returning >=1 sample):

1. `FindSamplesBySangerID(id)` -> `graph.sample = samples[0]`,
   `graph.samples = samples`. [classify hop]
2. `StudyForSample(samples[0])` -> `graph.study`. [study hop]
3. `graph.library = {sample.LibraryType, sample.IDStudyLims}` -
   no upstream call.

**4. sample_lims_id** - same as sanger_sample_id using
`FindSamplesByIDSampleLims`.

**5. sample_accession** - same as sanger_sample_id using
`FindSamplesByAccessionNumber`.

**6. run_id** (classify via
`FindSamplesByRunID(strconv.Atoi(id))` returning >=1 sample):

1. `FindSamplesByRunID(idRun)` -> `graph.samples`. [classify hop]
2. For each distinct `IDStudyLims` across samples,
   `GetStudy(idStudyLims)` -> append to `graph.studies`.
   [studies hop]
3. `graph.libraries = distinct({LibraryType, IDStudyLims})`
   across samples - no upstream call.

**7. library_type** (classify via
`FindSamplesByLibraryType(id)` returning >=1 sample):

1. `FindSamplesByLibraryType(id)` -> samples. Cap at
   `MaxLibrarySamples` (1000); if the cap triggers, emit
   `MissingHop{Hop: HopSamples, Reason: ReasonSamplesTruncated,
Status: 200}` and set `partial: true`. [classify hop]
2. `graph.libraries = distinct({LibraryType, IDStudyLims})`
   across samples (unaffected by cap). [libraries hop]
3. For each `IDStudyLims` in `graph.libraries`, `GetStudy`
   -> `graph.studies`. [studies hop]

**8. project_name** (classify via `ListProjects()` scan):

1. `ListProjects()` matches on `Name` -> `graph.project`.
   [classify hop]
2. `c.client.Projects().ListStudies(project.ID)` in the adapter
   (expose via `SAGAProvider.ListProjectStudies`) then
   `GetStudy` per returned `IDStudyLims` -> `graph.studies`.
   [studies hop]
3. `c.client.Projects().ListSamples(project.ID)` (expose via
   `SAGAProvider.ListProjectSamples`) then
   `FindSamplesBySangerID` per sample -> `graph.samples`.
   [samples hop]
4. `c.client.Projects().ListUsers(project.ID)` (expose via
   `SAGAProvider.ListProjectUsers`) -> `graph.users`. [users hop]

Classification attempts the hops in the order
`study_id -> study_accession -> sanger_sample_id ->
sample_lims_id -> sample_accession -> run_id -> library_type ->
project_name`. Client-error (4xx) failures move to the next
candidate. 5xx / transport errors on a classification hop record a
`MissingHop` for that hop but continue to the next candidate. If
every classification hop errors with 5xx/transport, the server
returns 502 with `ErrAllHopsFailed` and the assembled `missing`
list. If all hops returned empty/not-found, the server returns 404
with `ErrUnknownIdentifier`.

### Goal: smallest number of upstream calls

The algorithm is explicit: each identifier path lists a bounded,
minimal number of upstream calls (1-3 for sample/run/library/study;
project is the only multi-hop fan-out). No path calls `AllSamples()`
or otherwise does O(MLWH) work. Tests assert exact call counts per
path (see C1-C8).

### Preservation

- `/validate/{identifier}` keeps its current `IdentifierResult`
  contract (single matched object under `object`, no graph).
- `seqmeta.Validate` is updated only to use the new
  `FindSamplesBy*` hops instead of `AllSamples()` for the same
  five identifier types; its return value and priority order are
  unchanged. Existing `seqmeta.Validate` tests continue to pass.
- `DiffStudySamples`, `DiffSampleFiles`, watermarks, tombstones,
  and the `/diff/*` routes are not modified apart from the
  post-commit enrich-cache invalidation call.

---

## A. SAGA Find-Samples Methods

### A1: FindSamplesBySangerID

As a developer, I want to fetch MLWH samples by Sanger ID via the
upstream `filters` parameter, so that sample lookups do not require
scanning `AllSamples()`.

**Package:** `saga/`
**File:** `saga/mlwh.go`
**Test file:** `saga/mlwh_test.go`

```go
func (m *MLWHClient) FindSamplesBySangerID(ctx context.Context,
    sangerID string) ([]MLWHSample, error)
```

**Acceptance tests:**

1. Given an `httptest.Server` asserting the request path is
   `/integrations/mlwh/samples` and the `filters` query parameter
   decodes to `{"sanger_id":"WTSI_wEMB10524782"}`, returning
   `{"items":[{"sanger_id":"WTSI_wEMB10524782","id_study_lims":"6568"}],
"total":1}`, when `FindSamplesBySangerID(ctx, "WTSI_wEMB10524782")`
   is called, then the result has length 1 and
   `result[0].IDStudyLims == "6568"`.
2. Given an `httptest.Server` returning `{"items":[],"total":0}`,
   when called, then the result is an empty slice (not nil) and the
   error is nil.
3. Given an `httptest.Server` returning HTTP 500, when called, then
   `errors.Is(err, ErrServerError)` is true.
4. Given two pages (`items` len 100 then len 0), when called, then
   auto-pagination yields 100 samples.

### A2: FindSamplesByIDSampleLims

As A1 for filter key `id_sample_lims`.

```go
func (m *MLWHClient) FindSamplesByIDSampleLims(ctx context.Context,
    idSampleLims string) ([]MLWHSample, error)
```

**Acceptance tests:**

1. Given mock asserting `filters == {"id_sample_lims":"LIMS456"}`
   and returning one matching sample, when called, then result
   has 1 entry with `IDSampleLims == "LIMS456"`.
2. Given empty response, then result is empty slice, error nil.

### A3: FindSamplesByRunID

```go
func (m *MLWHClient) FindSamplesByRunID(ctx context.Context,
    idRun int) ([]MLWHSample, error)
```

**Acceptance tests:**

1. Given mock asserting `filters == {"id_run":"34134"}` (JSON string
   value) and returning 3 samples, when
   `FindSamplesByRunID(ctx, 34134)` is called, then result has
   length 3.
2. Given mock returning empty, then result is empty slice.

### A4: FindSamplesByLibraryType

```go
func (m *MLWHClient) FindSamplesByLibraryType(ctx context.Context,
    libraryType string) ([]MLWHSample, error)
```

**Acceptance tests:**

1. Given mock asserting `filters == {"library_type":"RNA PolyA"}`
   and returning 2 samples, when called with `"RNA PolyA"`, then
   result has length 2.

### A5: FindSamplesByAccessionNumber

```go
func (m *MLWHClient) FindSamplesByAccessionNumber(ctx context.Context,
    accessionNumber string) ([]MLWHSample, error)
```

**Acceptance tests:**

1. Given mock asserting `filters == {"accession_number":"SAM789"}`
   and returning 1 sample, when called, then result has length 1
   with `AccessionNumber == "SAM789"`.

### A6: StudyForSample helper

**File:** `saga/usecases.go`

```go
func (c *Client) StudyForSample(ctx context.Context,
    sample MLWHSample) (*Study, error)
```

**Acceptance tests:**

1. Given `sample.IDStudyLims == "6568"` and mock
   `GetStudy("6568")` returning a study, when called, then the
   study is returned.
2. Given empty `sample.IDStudyLims`, then
   `errors.Is(err, ErrNotFound)` is true and no HTTP request is
   made.

---

## B. SAGAProvider Extension

### B1: Extended interface and adapter

As a developer, I want the seqmeta provider interface to expose the
new targeted lookups and the study-from-sample helper, so that
enrichment can be written against a mockable surface.

**Package:** `seqmeta/`
**Files:** `seqmeta/provider.go`, `seqmeta/client_adapter.go`
**Test file:** `seqmeta/client_adapter_test.go`

Extend `SAGAProvider` with the nine new methods listed under
Architecture. `ClientAdapter` delegates each to the corresponding
`saga.MLWHClient`, `saga.Client`, or `saga.ProjectsClient` method.

**Acceptance tests:**

1. Given an `httptest.Server` returning 1 MLWH sample for filter
   `sanger_id=SANG1`, when `NewClientAdapter(client).
FindSamplesBySangerID(ctx, "SANG1")` is called, then the result
   has length 1.
2. Given servers covering each of the other 4 filter-based lookups
   and the 3 project helpers, when each adapter method is called,
   then the expected result is returned (happy path each).
3. Given an adapter variable assigned to a `SAGAProvider`, then it
   compiles (interface satisfaction check).

### B2: Mock provider fields

**File:** `seqmeta/provider_mock_test.go`

Extend the existing `MockProvider` with function fields mirroring
the new interface methods. Slice-returning methods return
`([]..., nil)` when their Fn is nil; `StudyForSample` returns
`(nil, nil)` when `StudyForSampleFn` is nil.

**Acceptance tests:**

1. Given a zero-value `MockProvider`, when
   `FindSamplesBySangerID(ctx, "x")` is called, then the result is
   an empty slice and error is nil.
2. Given `mp.FindSamplesByRunIDFn = func(_ context.Context, id int)
(...)` returning one sample, when called with 42, then the
   sample is returned.
3. Given `mp.StudyForSampleFn` returns `saga.ErrNotFound`, when
   invoked, then `errors.Is(err, saga.ErrNotFound)` is true.

---

## C. Enrichment Algorithm

### C1: study_id enrichment

As a developer, I want a study-ID identifier to be enriched with
its samples and distinct libraries using the smallest upstream hop
count, so that the graph is complete without full-table scans.

**Package:** `seqmeta/`
**File:** `seqmeta/enrich.go`
**Test file:** `seqmeta/enrich_test.go`

```go
func Enrich(ctx context.Context, provider SAGAProvider,
    identifier string) (*EnrichmentResult, error)
```

**Acceptance tests:**

1. Given a `MockProvider` where `GetStudy("6568")` returns a Study
   with `IDStudyLims == "6568"` and `AllSamplesForStudy("6568")`
   returns 3 samples with `LibraryType`s `{"A","A","B"}` (same
   `IDStudyLims`), when `Enrich(ctx, mp, "6568")` is called, then:
    - `Type == IdentifierStudyID`
    - `Graph.Study.IDStudyLims == "6568"`
    - `len(Graph.Samples) == 3`
    - `len(Graph.Libraries) == 2` with entries `{A,6568}` and
      `{B,6568}`
    - `Partial == false`, `Missing == nil`
    - Call counts: `GetStudyCalls == 1`,
      `AllSamplesForStudyCalls == 1`, all other Fn call counts 0.
2. Given `GetStudy` succeeds but `AllSamplesForStudy` returns
   `saga.ErrServerError`, when called, then:
    - `Type == IdentifierStudyID`
    - `Graph.Study` non-nil
    - `len(Graph.Samples) == 0`, `len(Graph.Libraries) == 0`
    - `Partial == true`
    - `Missing` contains exactly
      `{Hop: HopSamples, Reason: ReasonUpstreamError, Status: 502}`.

### C2: study_accession enrichment

**Acceptance tests:**

1. Given `GetStudy("ERP001")` returns 4xx-not-found and
   `AllStudies` returns one study with
   `AccessionNumber == "ERP001"` and `IDStudyLims == "6568"`, and
   `AllSamplesForStudy("6568")` returns 2 samples, when called
   with `"ERP001"`, then `Type == IdentifierStudyAccession` and
   `len(Graph.Samples) == 2`.
2. Given every study-lookup hop returns 5xx but the cascade then
   finds the identifier as a sanger ID (see C3), then it classifies
   accordingly and records the 502 hops in `Missing`.

### C3: sanger_sample_id enrichment

**Acceptance tests:**

1. Given study lookups fail client-side (4xx), and
   `FindSamplesBySangerID("S1")` returns one sample with
   `IDStudyLims == "6568"` and `LibraryType == "RNA PolyA"`, and
   `StudyForSample(sample)` returns a study, when called, then: - `Type == IdentifierSangerSampleID` - `Graph.Sample.SangerID == "S1"` - `len(Graph.Samples) == 1` - `Graph.Study.IDStudyLims == "6568"` - `Graph.Library == &Library{LibraryType:"RNA PolyA",
IDStudyLims:"6568"}` - `Partial == false` - `StudyForSampleCalls == 1`,
   `FindSamplesBySangerIDCalls == 1`, `AllSamplesCalls == 0`.
2. Given `FindSamplesBySangerID` returns one sample but
   `StudyForSample` returns `saga.ErrServerError`, then
   `Partial == true`, `Graph.Study == nil`, `Missing` contains
   `{Hop: HopStudy, Reason: ReasonUpstreamError, Status: 502}`.
3. Given every classification hop returns empty/not-found, then
   `errors.Is(err, ErrUnknownIdentifier)` is true.

### C4: sample_lims_id enrichment

**Acceptance tests:**

1. As C3.1 with `FindSamplesByIDSampleLims` returning the sample
   and `Type == IdentifierSampleLimsID`.

### C5: sample_accession enrichment

**Acceptance tests:**

1. As C3.1 with `FindSamplesByAccessionNumber` and
   `Type == IdentifierSampleAccession`.

### C6: run_id enrichment

**Acceptance tests:**

1. Given numeric identifier `"34134"`, and study lookups fail 4xx,
   `FindSamplesBySangerID`/`ByIDSampleLims`/`ByAccessionNumber`
   return empty, and `FindSamplesByRunID(34134)` returns 2 samples
   with `IDStudyLims`s `{"6568","6568"}`, and `GetStudy("6568")`
   returns one study, when called, then:
    - `Type == IdentifierRunID`
    - `len(Graph.Samples) == 2`
    - `len(Graph.Studies) == 1`
    - `len(Graph.Libraries) == 1`
    - `Partial == false`
    - `FindSamplesByRunIDCalls == 1`, `GetStudyCalls == 2` (one for
      the initial study_id classify attempt that 4xx'd, one for the
      studies hop).
2. Given the samples span two studies `"A"` and `"B"`, and
   `GetStudy("A")` returns a study while `GetStudy("B")` returns
   5xx, when called, then `Partial == true`, `len(Graph.Studies)
== 1`, and `Missing` contains `{HopStudies, ReasonUpstreamError,
502}`.
3. Given non-numeric identifier `"abc"`, then run-id classification
   is skipped (no call to `FindSamplesByRunIDFn`).

### C7: library_type enrichment and fan-out cap

**Acceptance tests:**

1. Given classification hops before library_type return empty, and
   `FindSamplesByLibraryType("RNA PolyA")` returns 5 samples across
   `IDStudyLims` `{"A","A","B","B","C"}`, and `GetStudy` for each
   distinct lims returns a study, when called, then:
    - `Type == IdentifierLibraryType`
    - `len(Graph.Samples) == 5`
    - `len(Graph.Libraries) == 3`
    - `len(Graph.Studies) == 3`
    - `Partial == false`.
2. Given `FindSamplesByLibraryType` returns 1500 samples, when
   called, then `len(Graph.Samples) == 1000`, `Partial == true`,
   `Missing` contains exactly one
   `{Hop: HopSamples, Reason: ReasonSamplesTruncated, Status: 200}`,
   and `Graph.Libraries` still reflects distinct libraries across
   all 1500 samples (computed before truncation).
3. Given `FindSamplesByLibraryType` returns empty, then
   `errors.Is(err, ErrUnknownIdentifier)` is true.

### C8: project_name enrichment

**Acceptance tests:**

1. Given every other classification hop returns empty/4xx, and
   `ListProjects` returns a project named `"MyProject"` with
   `ID == 7`, and `ListProjectStudies(7)` returns 2 linked studies,
   `GetStudy` for each returns the study, `ListProjectSamples(7)`
   returns 3 linked samples, `FindSamplesBySangerID` for each
   returns one sample, and `ListProjectUsers(7)` returns 2 users,
   when called, then:
    - `Type == IdentifierProjectName`
    - `Graph.Project.ID == 7`
    - `len(Graph.Studies) == 2`
    - `len(Graph.Samples) == 3`
    - `len(Graph.Users) == 2`
    - `Partial == false`.
2. Given `ListProjectUsers` returns 5xx but the other project hops
   succeed, then `Partial == true` and `Missing` contains
   `{HopUsers, ReasonUpstreamError, 502}`.

### C9: All classification hops fail with 5xx

**Acceptance tests:**

1. Given every hop that could classify `"xyz"` returns
   `saga.ErrServerError`, when called, then
   `errors.Is(err, ErrAllHopsFailed)` is true and the returned
   `*EnrichmentResult` is nil.

### C10: Unknown identifier

**Acceptance tests:**

1. Given every hop returns empty list / 4xx-not-found for `"xyz"`,
   then `errors.Is(err, ErrUnknownIdentifier)` is true.
2. Given empty string, then `errors.Is(err, ErrUnknownIdentifier)`
   is true with no upstream calls.

---

## D. Enrichment Cache

### D1: Cache schema and LoadEnrichCache

As a developer, I want the enrichment cache table co-located with
watermarks, so that one SQLite file owns all seqmeta persistence.

**Package:** `seqmeta/`
**File:** `seqmeta/enrich_cache.go`
**Test file:** `seqmeta/enrich_cache_test.go`

```go
func (s *Store) LoadEnrichCache(identifier string) (
    *enrichCacheEntry, error)
func (s *Store) SaveEnrichCache(e enrichCacheEntry) error
func (s *Store) DeleteEnrichCache(identifier string) error
func (s *Store) InvalidateEnrichFor(queryKind, queryID string) error
```

**Acceptance tests:**

1. Given a fresh in-memory store, when `LoadEnrichCache("x")` is
   called, then it returns `(nil, sql.ErrNoRows)` (or wraps it).
2. Given `SaveEnrichCache(entry{Identifier:"x", Type:"study_id",
Body:[]byte("{}"), FetchedAt:time.Now(), TTL:time.Hour})`, when
   `LoadEnrichCache("x")` is called, then the returned entry has
   `Type == IdentifierStudyID`, `Body` matches, `TTL == 1h`,
   `Negative == false`.
3. Given a saved entry, when
   `DeleteEnrichCache("x")` is called, then `LoadEnrichCache("x")`
   again returns no-rows.
4. Given entries for identifiers
   `["abc", "def"]`, when `DeleteEnrichCache("abc")` is called,
   then `LoadEnrichCache("def")` still returns the entry.
5. Given a negative-cache entry (`Negative: true, Type: ""`), when
   loaded, then `Negative == true` and `Type == ""`.

### D2: Expiry is never served

**Acceptance tests:**

1. Given an entry saved with `TTL == 10 * time.Millisecond` and
   `FetchedAt = time.Now().Add(-time.Hour)`, when the server
   handler checks freshness (via
   `entry.FetchedAt.Add(entry.TTL).Before(time.Now())`), then it
   treats the entry as absent.

### D3: Invalidation on diff mutations

**Acceptance tests:**

1. Given a cached enrichment for `"6568"` with
   `type == "study_id"`, when
   `InvalidateEnrichFor("study_samples", "6568")` is called, then
   `LoadEnrichCache("6568")` returns no-rows.
2. Given a cached enrichment for `"SANG1"` with
   `type == "sanger_sample_id"` and body JSON containing
   `"id_study_lims":"6568"`, when
   `InvalidateEnrichFor("study_samples", "6568")` is called, then
   `LoadEnrichCache("SANG1")` returns no-rows.
3. Given a cached enrichment for `"SANG1"`, when
   `InvalidateEnrichFor("sample_files", "SANG1")` is called, then
   `LoadEnrichCache("SANG1")` returns no-rows.
4. Given a cached enrichment for `"OTHER"`, when
   `InvalidateEnrichFor("sample_files", "SANG1")` is called, then
   `"OTHER"` is still present.

---

## E. REST API

### E1: GET /enrich/{identifier} happy path

As a consumer, I want `GET /enrich/{identifier}` to return a typed
`EnrichmentResult` with a flat graph envelope.

**Package:** `seqmeta/`
**File:** `seqmeta/server.go`
**Test file:** `seqmeta/server_enrich_test.go`

**Acceptance tests:**

1. Given an in-memory store, a `MockProvider` where
   `GetStudy("6568")` returns a study and
   `AllSamplesForStudy("6568")` returns 3 samples, when
   `GET /enrich/6568` is called, then:
    - Status 200
    - `Content-Type: application/json`
    - Body `type == "study_id"`,
      `graph.study.id_study_lims == "6568"`,
      `len(graph.samples) == 3`, `len(graph.libraries) >= 1`,
      `partial == false`, `missing` absent.
2. Given identifier with URL-special character (`foo/bar`), when
   `GET /enrich/foo%2Fbar` is called, then the raw identifier
   `"foo/bar"` is passed to `Enrich`.

### E2: GET /enrich partial and 502

**Acceptance tests:**

1. Given `GetStudy("6568")` succeeds and
   `AllSamplesForStudy` returns `saga.ErrServerError`, when
   `GET /enrich/6568` is called, then status is 200 with
   `partial == true` and `missing` contains exactly one
   `{hop:"samples", reason:"upstream_error", status:502}`.
2. Given every classification hop returns
   `saga.ErrServerError`, when called with `"xyz"`, then status
   is 502 and body is
   `{"error":"seqmeta: all enrichment hops failed"}` plus a
   `missing` key listing the failed probes.
3. Given every hop returns empty/404, then status is 404 and body
   is `{"error":"seqmeta: unknown identifier"}`.

### E3: Cache hits and TTL via functional option

**Acceptance tests:**

1. Given `NewServer(provider, store, WithEnrichTTL(time.Hour,
10*time.Minute))` and two consecutive `GET /enrich/6568`
   calls, when the second is issued, then the provider's
   `GetStudyCalls` count is still 1 (cache hit).
2. Given `WithEnrichTTL(10*time.Millisecond, ...)` and a first
   call populating the cache, when a second call is issued after
   20 ms, then `GetStudyCalls` is 2 (re-fetch on expiry).
3. Given a partial result was cached, when inspected via
   `LoadEnrichCache`, then `TTL` equals the negative TTL and
   `Partial == true`.
4. Given a negative (unknown-identifier) result was served, when
   the same identifier is requested again within the negative
   TTL, then status is 404 and the provider's call counts do not
   increase from the first call.

### E4: DELETE /enrich/{identifier}

**Acceptance tests:**

1. Given a cache entry for `"6568"`, when
   `DELETE /enrich/6568` is called, then status is 200, body is
   `{"identifier":"6568"}`, and `LoadEnrichCache("6568")` returns
   no-rows.
2. Given no cache entry for `"missing"`, when
   `DELETE /enrich/missing` is called, then status is still 200.
3. Given a closed store, when `DELETE /enrich/x` is called, then
   status is 500 with an `error` key.

### E5: Diff invalidation integration

**Acceptance tests:**

1. Given a cached enrichment for `"6568"`, when
   `GET /diff/study/6568` is called successfully, then after the
   diff response, `LoadEnrichCache("6568")` returns no-rows.
2. Given a cached enrichment for `"SANG1"` referencing
   `"id_study_lims":"6568"`, when `GET /diff/study/6568` is
   called, then `LoadEnrichCache("SANG1")` returns no-rows.
3. Given a cached enrichment for `"SANG1"`, when
   `GET /diff/sample/SANG1` is called, then
   `LoadEnrichCache("SANG1")` returns no-rows.

### E6: /validate preservation

**Acceptance tests:**

1. Given a server configured as above, when `GET /validate/6568`
   is called with `GetStudy("6568")` returning a study, then the
   status is 200 with `type == "study_id"` and `object` is the
   Study JSON (no `graph` key present; shape is unchanged).
2. Given a mock provider where `FindSamplesBySangerID("S1")`
   returns 1 sample and all study lookups fail 4xx, when
   `GET /validate/S1` is called, then status 200 with
   `type == "sanger_sample_id"` and `object` is the MLWHSample
   (validated path uses the new targeted hop, not `AllSamples`).

---

## F. Early-Probe Integration Test

### F1: MLWH filter probe

As an operator, I want a gated integration test that verifies each
MLWH filter key is supported upstream, so that a filter becoming
unsupported is detected early and enrichment paths can degrade
consciously.

**Package:** `saga/`
**File:** `saga/integration_test.go` (new `TestFilterProbes` func)

Test runs when `SAGA_TEST_API_TOKEN` is set; skipped otherwise.

**Acceptance tests:**

1. Given a valid token, when `FindSamplesBySangerID(ctx,
"WTSI_wEMB10524782")` is called, then it returns at least 1
   sample without error.
2. Given a valid token, when `FindSamplesByIDSampleLims(ctx,
"<a known id_sample_lims from MLWH>")` is called with a value
   harvested from `AllSamplesForStudy("3361")`, then it returns
   at least 1 sample.
3. Given a valid token, when `FindSamplesByRunID(ctx, 34134)` is
   called, then it returns at least 1 sample.
4. Given a valid token, when `FindSamplesByLibraryType(ctx,
"RNA PolyA")` is called, then it returns at least 1 sample.
5. Given a valid token, when `FindSamplesByAccessionNumber(ctx,
"<accession harvested from the same seed study>")` is called,
   then it returns at least 1 sample.
6. Given any of the above returns HTTP 500 / `ErrServerError`,
   the test records the filter as unsupported (via `t.Log`) and
   fails - this forces the spec's degraded-behaviour contract
   (see below) to be explicitly acknowledged in a subsequent
   code change before the enrichment path is re-enabled.

### Degraded behaviour contract

If `FindSamplesBy<K>` returns 5xx reproducibly against production
MLWH, the corresponding `IdentifierType` classification hop records
`{Hop: HopClassify, Reason: ReasonFilterUnsupported, Status: 502}`
and skips to the next candidate without contributing to the
classification result. It never falls back to `AllSamples()`.

---

## G. Integration Test Matrix

### G1: Real-API enrichment matrix

**Package:** `seqmeta/`
**File:** `seqmeta/integration_test.go` (new)

Skip unless `SAGA_TEST_API_TOKEN` is set. Each case calls
`Enrich(ctx, adapter, id)` and asserts per its tag.

| Identifier          | Type             | Tag        |
| ------------------- | ---------------- | ---------- |
| `5993`              | study_id         | end-to-end |
| `5994`              | study_id         | end-to-end |
| `6591`              | study_id         | end-to-end |
| `5835STDY8046554`   | sanger_sample_id | end-to-end |
| `WTSI_wEMB10524782` | sanger_sample_id | end-to-end |
| `6591STDY10735392`  | sanger_sample_id | end-to-end |
| `RNA PolyA`         | library_type     | end-to-end |
| `Agilent Pulldown`  | library_type     | end-to-end |
| `34134`             | run_id           | end-to-end |
| `40121`             | run_id           | end-to-end |

Tag meaning:

- **end-to-end**: expect `err == nil`, `Graph.Study != nil`
  (or `Graph.Studies` non-empty for run/library cases),
  `Partial == false`, `len(Missing) == 0`.
- **partial**: expect `err == nil`, `Partial == true`,
  `len(Missing) >= 1`. Identifiers start un-tagged as partial only
  when `TestFilterProbes` has confirmed a filter is unsupported;
  default is end-to-end.

**Acceptance tests:**

1. Given a valid token, when each row is evaluated with
   `Enrich(ctx, adapter, id)`, then the assertion for its tag
   holds (see mapping above).
2. Given no token, then the whole test is skipped without failing.
3. Given a `context.DeadlineExceeded` per-call timeout of 20 s,
   then each row either completes within that budget or returns
   a context-deadline error (asserted per case).

---

## H. Frontend Contracts and UI

### H1: Schemas

**File:** `frontend/lib/contracts.ts`
**Test file:** `frontend/tests/contracts.test.ts`

Add:

```ts
export const libraryLinkSchema = z.object({
    library_type: z.string(),
    id_study_lims: z.string(),
});
export type LibraryLink = z.infer<typeof libraryLinkSchema>;

export const enrichmentGraphSchema = z.object({
    study: studySchema.optional(),
    studies: z.array(studySchema).optional(),
    sample: sampleSchema.optional(),
    samples: z.array(sampleSchema).optional(),
    library: libraryLinkSchema.optional(),
    libraries: z.array(libraryLinkSchema).optional(),
    project: z
        .object({ id: z.number(), name: z.string() })
        .passthrough()
        .optional(),
    users: z
        .array(z.object({ id: z.number(), username: z.string() }).passthrough())
        .optional(),
});
export type EnrichmentGraph = z.infer<typeof enrichmentGraphSchema>;

export const missingHopSchema = z.object({
    hop: z.string(),
    reason: z.string(),
    status: z.number(),
});
export type MissingHop = z.infer<typeof missingHopSchema>;

export const enrichmentResultSchema = z.object({
    identifier: z.string(),
    type: z.string(),
    graph: enrichmentGraphSchema,
    partial: z.boolean(),
    missing: z.array(missingHopSchema).optional(),
});
export type EnrichmentResult = z.infer<typeof enrichmentResultSchema>;
```

**Acceptance tests:**

1. Given a JSON payload matching the Go `EnrichmentResult` for a
   study, when parsed with `enrichmentResultSchema`, then
   `success === true`.
2. Given a payload with `partial: true` and one `missing` entry,
   when parsed, then `missing[0].reason === "samples_truncated"`
   is preserved.
3. Given a payload missing `graph`, when parsed, then
   `success === false`.

### H2: enrichIdentifier action

**File:** `frontend/app/(results)/actions.ts`
**Test file:** `frontend/tests/backend-client.test.ts`

```ts
export async function enrichIdentifier(
    value: string,
): Promise<EnrichmentResult | null>;
```

- Returns `null` on 404.
- Throws on network error and on 5xx (surfaced as
  `BackendRequestError` with status 502 when upstream-impaired).
- Uses `seqmetaJson(`/enrich/${encodeURIComponent(trimmed)}`,
enrichmentResultSchema)`.

**Acceptance tests:**

1. Given a mock seqmeta backend returning a valid
   `EnrichmentResult` for `"6568"`, when `enrichIdentifier("6568")`
   is called, then the result has `type === "study_id"` and
   `partial === false`.
2. Given the backend returns 404, then result is `null`.
3. Given the backend returns 502 with `missing`, then the promise
   rejects with `BackendRequestError` whose `status === 502`.

### H3: Enrichment state and badge

**Files:** `frontend/lib/seqmeta-enrichment.ts`,
`frontend/components/result-metadata-enrichment.tsx`,
`frontend/components/seqmeta-badge.tsx`
**Test files:** `frontend/tests/seqmeta-enrichment.test.ts`,
`frontend/tests/seqmeta-badge.test.ts`

Extend `SeqmetaEnrichmentState` to carry enrichment results
keyed by identifier:

```ts
export type SeqmetaEnrichmentState = {
    enrichments: Record<string, EnrichmentResult | null>;
    errors: Record<string, "not_found" | "upstream_impaired">;
};
```

`result-metadata-enrichment.tsx` calls `enrichIdentifier` for each
seqmeta value not in cache. 404 -> `errors[value] = "not_found"`.
Other error (including 502) -> `errors[value] = "upstream_impaired"`.

`SeqmetaBadge` renders:

- Primary classification label (same as today, derived from
  `result.type` and graph fields).
- When `result.partial === true`: a subdued banner line
  `"Some details unavailable"` followed by a bullet list of
  human-readable hop labels derived from `result.missing` (map
  hop+reason to text: e.g.
  `samples + samples_truncated -> "Showing first 1000 samples"`,
  `study + upstream_error -> "Study record unavailable"`).
- When error is `"not_found"`: unchanged "enrichment unavailable"
  glyph.
- When error is `"upstream_impaired"`: a distinct marker
  (`aria-label="enrichment backend impaired"`) with tooltip
  `"Backend could not reach upstream"` - visually distinct from
  the 404 glyph.

**Acceptance tests:**

1. Given a mocked `enrichIdentifier` returning a full graph for
   `"6568"`, when the component renders with
   `metadata: { seqmeta_studyid: "6568" }`, then the badge shows
   the study name and no banner.
2. Given a mocked `enrichIdentifier` returning
   `partial: true, missing: [{hop:"samples",
reason:"samples_truncated", status:200}]`, when rendered, then
   the banner text `"Showing first 1000 samples"` is present in
   the accessibility tree.
3. Given a mocked `enrichIdentifier` returning
   `partial: true, missing: [{hop:"study",
reason:"upstream_error", status:502}]`, then the banner text
   `"Study record unavailable"` is present.
4. Given the action resolves to `null` (per the H2 404 -> null
   contract), then the error glyph
   `aria-label="enrichment unavailable"` renders.
5. Given the action rejects with
   `new BackendRequestError(502, ...)`, then the marker
   `aria-label="enrichment backend impaired"` renders.

### H4: Playwright coverage

**File:** `frontend/e2e/results.spec.ts`

Extend the existing detail-view scenario with a stub seqmeta
response that returns a partial graph and assert the banner text.

**Acceptance tests:**

1. Given the stub returns a partial graph with
   `samples_truncated`, when the detail view is rendered, then
   Playwright finds the banner text `"Showing first 1000 samples"`.
2. Given the stub returns 502, then the impaired marker is
   visible.

---

## Implementation Order

### Phase 1: SAGA find-samples methods

Stories: A1, A2, A3, A4, A5, A6

Foundation. Sequential for A1 (establishes filter-key pattern),
then A2-A5 parallel. A6 independent.

### Phase 2: Provider extension and mock

Stories: B1, B2

Depends on Phase 1. Sequential (B1 before B2).

### Phase 3: Enrichment cache

Stories: D1, D2, D3

Depends on existing `Store` only. Parallelisable with Phases 1-2.

### Phase 4: Enrichment algorithm

Stories: C1, C2, C3, C4, C5, C6, C7, C8, C9, C10

Depends on Phase 2. C1-C2 sequential (establish pattern); C3-C8
parallel; C9-C10 parallel last.

### Phase 5: REST API

Stories: E1, E2, E3, E4, E5, E6

Depends on Phases 3-4. E1 first, then E2-E6 parallel.

### Phase 6: Early-probe and integration matrix

Stories: F1, G1

Depends on all prior Go phases. Gated on
`SAGA_TEST_API_TOKEN`. F1 before G1.

### Phase 7: Frontend contracts and action

Stories: H1, H2

Depends on Phase 5 shape only. Sequential (H1 before H2).

### Phase 8: Frontend UI and E2E

Stories: H3, H4

Depends on Phase 7. H3 before H4.

---

## Appendix: Key Decisions

- **`/validate/` unchanged.** `IdentifierResult.Object` stays a
  single matched object. All graph concerns live on `/enrich/`.
  Prevents breaking list/search badge consumers.
- **Diff machinery unchanged.** Watermarks, tombstones, and
  `DiffStudySamples`/`DiffSampleFiles` are not touched except for
  the post-commit `InvalidateEnrichFor` call.
- **Targeted lookups only.** `AllSamples()` is never called from
  the enrichment path. Production MLWH returns 500 for that
  endpoint; targeted `filters` lookups are the supported mechanism.
- **Graph is flat.** Pointer/slice fields in `EnrichmentGraph`
  with `omitempty` keep JSON small and the shape stable for zod
  parsing. No nested "hop" tree.
- **Partial is default posture.** When a non-classification hop
  fails upstream (5xx/transport), the handler emits a
  `MissingHop` rather than failing the whole request. This limits
  blast radius when MLWH or iRODS is flaky.
- **404 vs 502 split.** 404 requires every hop to say "empty /
  not-found"; 502 requires every classification hop to have a
  5xx/transport failure.
- **SQLite single-writer.** The enrich cache reuses `Store.WithLock`
  and the existing DB file. Exactly one `seqmeta` process per DB;
  operators running multiple replicas must use independent stores.
- **TTL via functional options only.** No env-var reads inside the
  package. Tests pass short TTLs; production passes defaults.
- **Library fan-out cap.** `MaxLibrarySamples = 1000` is a package
  constant, not configurable. When triggered, `Graph.Libraries`
  and `Graph.Studies` stay complete; only `Graph.Samples` is
  truncated. `samples_truncated` uses `status: 200` since it is
  a capacity decision, not an upstream failure.
- **DELETE auth.** `DELETE /enrich/{identifier}` shares the
  existing seqmeta trust boundary (no auth in-process; relies on
  network policy / reverse proxy). Documented but not enforced
  in code.
- **iRODS files out of graph.** Files keep flowing through
  `/diff/sample/{id}` and `saga.GetSampleFiles`; the frontend
  fetches them on demand. Prevents the enrichment call from
  being O(files) for large studies.
- **Goal restated.** The enrichment algorithm lists an explicit,
  minimal per-type upstream call budget (1-3 calls for sample /
  run / library / study; 4 hops for project). Tests assert the
  exact call counts so regressions are caught at review time.
- **Testing.** GoConvey `So()` per go-conventions; Vitest /
  Playwright per existing frontend patterns. Reference
  go-implementor and go-reviewer skills for TDD workflow.
