# MLWH Overhaul Specification

## Overview

`mlwh` becomes the single source of current-state MLWH answers, both as a Go
package and as a new `wa mlwh serve` REST server. `seqmeta` is renamed
`mlwhdiff` and narrowed to change-tracking only (watermark/tombstone diffing).
Every current-state route that `seqmeta` exposed today (`/validate/*`,
`/enrich/*`, `/studies`, `/study/{id}/samples`) is deleted from the renamed
server and reappears as natural `mlwh` server endpoints with a 1:1
method-to-endpoint correspondence.

A single Go interface `mlwh.Queryer` describes the full read/query surface. It
is implemented by the existing local, cache-backed `*mlwh.Client` and by a new
`*mlwh.RemoteClient` that talks to `wa mlwh serve`. `results`, `mlwhdiff`, and
the CLI depend on `mlwh.Queryer`, so each is wired to a local `Client` or a
remote server by configuration alone (`WA_MLWH_SERVER_URL`). A single
declarative registry maps each `Queryer` method to one HTTP endpoint
(`{verb, path template, query-param names, response type}`); the gin handler
and the `RemoteClient` method are both derived from that one entry.

Caching lives inside the `mlwh` package (the synced cache and the existing
5-minute `ExpandIdentifier` memo), never in the HTTP handlers, so direct Go
callers and the server benefit identically. The server is read-only, serves
cache-only (no live-MLWH fallback, no sync), and surfaces
`ErrCacheNeverSynced`. Both server stacks standardise on gin + go-authserver
(gas); the `mlwh` server is unauthenticated by default with optional gas
JWT/Bearer + TLS for cross-cluster exposure.

## Architecture

### Packages, files, and HTTP stack

- `mlwh/` gains: `queryer.go` (the `Queryer` interface), `registry.go` (the
  declarative method-to-endpoint registry + encode/decode helpers),
  `server.go` (gin server building handlers from the registry),
  `remote.go` (`RemoteClient`), `enrich.go` (moved enrich graph + detail
  builders), `errors_http.go` (sentinel <-> status/envelope mapping).
- `seqmeta/` -> `mlwhdiff/`: directory and package renamed. Surviving files:
  `diff.go`, `store.go`, `server.go` (migrated chi -> gin), `types.go`
  (diff-only types), `provider.go` (narrowed), `client_adapter.go`
  (narrowed). Deleted: `enrich.go`, `validate.go`, `enrich_cache_test.go`,
  `enrich_*_test.go`, `server_enrich_test.go`, `validate_test.go`, and the
  current-state handlers/tests in `server.go`.
- `cmd/`: `mlwh.go` gains `wa mlwh serve`; `seqmeta.go` -> `mlwhdiff.go`
  (`wa mlwhdiff diff`, `wa mlwhdiff serve`); `results.go` rewired.
- `frontend/`: `lib/seqmeta-*.ts` -> `lib/mlwh-*.ts` (cache + enrichment
  client helpers), `components/seqmeta-badge.tsx` ->
  `components/mlwh-badge.tsx`,
  `backend-client.ts` env var change, `actions.ts`/`studies-cache.ts`
  repointed.

The project standardises on gin + gas. The bare-chi server in `seqmeta/
server.go` is migrated to gin. `wa mlwh serve` is built on gin and reuses the
gas auth wiring pattern from `wa results serve`.

### `mlwh.Queryer` interface

`queryer.go`. Covers the full read/query surface and nothing else. Excludes
lifecycle/admin/internal helpers (`Open`, `OpenCacheOnly`, `Close`, `Sync`,
`ReadDB`, `SetSyncReportWriter`, cache internals). `*mlwh.Client` already
implements every method except `Enrich`, `SampleDetail`, `StudyDetail`,
`RunDetail`, `LibraryDetail` (added in section C), and the reshaped
`ExpandSearchValues` (section E).

```go
type Queryer interface {
    // Classification and resolution.
    ClassifyIdentifier(ctx context.Context, raw string) (Match, error)
    ResolveSample(ctx context.Context, raw string) (Match, error)
    ResolveSampleName(ctx context.Context, raw string) (Match, error)
    ResolveStudy(ctx context.Context, raw string) (Match, error)
    ResolveRun(ctx context.Context, raw string) (Match, error)
    ResolveLibrary(ctx context.Context, raw string) (Match, error)
    ResolveLibraryIdentifier(ctx context.Context, raw string) (Match, error)

    // Enumeration and hierarchy.
    AllStudies(ctx context.Context, limit, offset int) ([]Study, error)
    SamplesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Sample, error)
    SamplesForRun(ctx context.Context, idRun string, limit, offset int) ([]Sample, error)
    SamplesForLibrary(ctx context.Context, pipelineIDLims, studyLimsID string, limit, offset int) ([]Sample, error)
    SamplesForLibraryID(ctx context.Context, libraryID string, limit, offset int) ([]Sample, error)
    SamplesForLibraryLimsID(ctx context.Context, idLibraryLims string, limit, offset int) ([]Sample, error)
    SamplesForLibraryType(ctx context.Context, pipelineIDLims string, limit, offset int) ([]Sample, error)
    LibrariesForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Library, error)
    RunsForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]Run, error)
    LanesForSample(ctx context.Context, sangerName string, limit, offset int) ([]Lane, error)
    IRODSPathsForSample(ctx context.Context, sangerName string, limit, offset int) ([]IRODSPath, error)
    IRODSPathsForStudy(ctx context.Context, studyLimsID string, limit, offset int) ([]IRODSPath, error)
    StudiesForSample(ctx context.Context, sangerName string) ([]Study, error)

    // Sample finders.
    FindSamplesBySangerID(ctx context.Context, sangerID string) ([]Sample, error)
    FindSamplesByIDSampleLims(ctx context.Context, idSampleLims string) ([]Sample, error)
    FindSamplesByAccessionNumber(ctx context.Context, accessionNumber string) ([]Sample, error)
    FindSamplesBySupplierName(ctx context.Context, supplierName string) ([]Sample, error)
    FindSamplesByLibraryType(ctx context.Context, libraryType string) ([]Sample, error)

    // Expansion (search support).
    ExpandIdentifier(ctx context.Context, kind IdentifierKind, canonical string) ([]TaggedID, error)
    ExpandSearchValues(ctx context.Context, kind IdentifierKind, canonical string) (SearchValues, error)
    ExpandSampleSearchValues(ctx context.Context, kind IdentifierKind, canonical string) ([]string, error)

    // Enrichment graph and detail aggregates.
    Enrich(ctx context.Context, identifier string) (EnrichmentResult, error)
    SampleDetail(ctx context.Context, sangerName string) (SampleDetail, error)
    StudyDetail(ctx context.Context, studyLimsID string) (StudyDetail, error)
    RunDetail(ctx context.Context, idRun string) (RunDetail, error)
    LibraryDetail(ctx context.Context, pipelineIDLims, studyLimsID string) (LibraryDetail, error)
}
```

Interface members: 33. Each maps 1:1 to exactly one REST endpoint via the
registry. `SearchValues` and `EnrichmentResult` are new `mlwh` types (sections
E and C).

### Method <-> endpoint registry

`registry.go`. All endpoints are GET; identifiers in the path; `limit`/`offset`
and filters as query params; JSON response bodies. Path prefix omitted below
(server mounts under `/` for the unauthenticated case, mirroring today's
seqmeta routes; under gas auth groups when secured).

```go
type Endpoint struct {
    Method     string   // Queryer method name, e.g. "SamplesForStudy"
    Verb       string   // always "GET"
    Path       string   // gin path template, e.g. "/study/:id/samples"
    PathParams []string // path param names in call order, e.g. ["id"]
    Query      []string // query-param names beyond limit/offset
    Paginated  bool     // true if method takes (limit, offset)
    NewResult  func() any // zero response value for decoding
}

var Registry = []Endpoint{ ... } // one entry per Queryer method
```

| Queryer method               | Verb | Path                                    | Query params (+limit,offset) | Response type    |
| ---------------------------- | ---- | --------------------------------------- | ---------------------------- | ---------------- |
| ClassifyIdentifier           | GET  | /classify/:id                           | -                            | Match            |
| ResolveSample                | GET  | /resolve/sample/:id                     | -                            | Match            |
| ResolveSampleName            | GET  | /resolve/sample-name/:id                | -                            | Match            |
| ResolveStudy                 | GET  | /resolve/study/:id                      | -                            | Match            |
| ResolveRun                   | GET  | /resolve/run/:id                        | -                            | Match            |
| ResolveLibrary               | GET  | /resolve/library/:id                    | -                            | Match            |
| ResolveLibraryIdentifier     | GET  | /resolve/library-identifier/:id         | -                            | Match            |
| AllStudies                   | GET  | /studies                                | limit, offset                | []Study          |
| SamplesForStudy              | GET  | /study/:id/samples                      | limit, offset                | []Sample         |
| SamplesForRun                | GET  | /run/:id/samples                        | limit, offset                | []Sample         |
| SamplesForLibrary            | GET  | /library/:pipeline/study/:study/samples | limit, offset                | []Sample         |
| SamplesForLibraryID          | GET  | /library-id/:id/samples                 | limit, offset                | []Sample         |
| SamplesForLibraryLimsID      | GET  | /library-lims-id/:id/samples            | limit, offset                | []Sample         |
| SamplesForLibraryType        | GET  | /library-type/:id/samples               | limit, offset                | []Sample         |
| LibrariesForStudy            | GET  | /study/:id/libraries                    | limit, offset                | []Library        |
| RunsForStudy                 | GET  | /study/:id/runs                         | limit, offset                | []Run            |
| LanesForSample               | GET  | /sample/:id/lanes                       | limit, offset                | []Lane           |
| IRODSPathsForSample          | GET  | /sample/:id/irods                       | limit, offset                | []IRODSPath      |
| IRODSPathsForStudy           | GET  | /study/:id/irods                        | limit, offset                | []IRODSPath      |
| StudiesForSample             | GET  | /sample/:id/studies                     | -                            | []Study          |
| FindSamplesBySangerID        | GET  | /find/sample/sanger-id/:id              | -                            | []Sample         |
| FindSamplesByIDSampleLims    | GET  | /find/sample/lims-id/:id                | -                            | []Sample         |
| FindSamplesByAccessionNumber | GET  | /find/sample/accession/:id              | -                            | []Sample         |
| FindSamplesBySupplierName    | GET  | /find/sample/supplier-name/:id          | -                            | []Sample         |
| FindSamplesByLibraryType     | GET  | /find/sample/library-type/:id           | -                            | []Sample         |
| ExpandIdentifier             | GET  | /expand/:kind/:id                       | -                            | []TaggedID       |
| ExpandSearchValues           | GET  | /expand-search/:kind/:id                | -                            | SearchValues     |
| ExpandSampleSearchValues     | GET  | /expand-sample-search/:kind/:id         | -                            | []string         |
| Enrich                       | GET  | /enrich/:id                             | -                            | EnrichmentResult |
| SampleDetail                 | GET  | /sample/:id/detail                      | -                            | SampleDetail     |
| StudyDetail                  | GET  | /study/:id/detail                       | -                            | StudyDetail      |
| RunDetail                    | GET  | /run/:id/detail                         | -                            | RunDetail        |
| LibraryDetail                | GET  | /library/:pipeline/study/:study/detail  | -                            | LibraryDetail    |

Path identifiers are URL-path-escaped by the `RemoteClient` and unescaped by
the handler (matching today's `decodeWildcardIdentifier`). `kind` is the
`IdentifierKind` string. The `ExpandIdentifier` registry entry returns a single
slice (unchanged); `ExpandSearchValues` returns a single named struct
(`SearchValues`) so no positional re-splitting crosses the wire.

### Error envelope and sentinel round-tripping

`errors_http.go`. Every error response carries
`{"code": "<stable-string>", "message": "<text>"}`.

| Sentinel                 | Status | code                   |
| ------------------------ | ------ | ---------------------- |
| ErrNotFound              | 404    | not_found              |
| ErrAmbiguous             | 409    | ambiguous              |
| ErrUnsupportedIdentifier | 422    | unsupported_identifier |
| ErrCacheNeverSynced      | 503    | cache_never_synced     |
| ErrUpstreamImpaired      | 502    | upstream_impaired      |

The handler maps the sentinel (via `errors.Is`) to status+code. The
`RemoteClient` maps `code` back to the exact sentinel and wraps it
(`fmt.Errorf("%s: %w", message, sentinel)`). `ErrCacheNeverSynced` keeps its
distinct 503/`cache_never_synced`; today the local `Client` wraps it so
`errors.Is(err, ErrNotFound)` is also true on read-list paths -- the
reconstructed client-side error preserves the same wrapping (the `RemoteClient`
joins `ErrCacheNeverSynced` with `ErrNotFound` when the server signalled it
from a list endpoint, matching `Client` behaviour for that endpoint). An
unrecognised code, or any non-2xx without a parseable envelope, becomes a
generic error wrapping `ErrUpstreamImpaired`.

### `mlwh.RemoteClient`

`remote.go`.

```go
type RemoteClient struct { ... } // baseURL, *http.Client, optional gas token, optional cache

type RemoteConfig struct {
    BaseURL   string
    Timeout   time.Duration // default 30s
    Token     string        // optional gas Bearer token
    CACert    string        // optional TLS CA path
    CacheTTL  time.Duration // optional client-side cache TTL; 0 disables
}

func NewRemoteClient(cfg RemoteConfig) (*RemoteClient, error)
func (rc *RemoteClient) Close() error
```

`*RemoteClient` implements `Queryer`. Each method looks up its `Registry`
entry, builds the path (escaped path params) + query string (limit/offset +
filters), issues GET, and decodes into `entry.NewResult()` on 2xx or maps the
error envelope on non-2xx. A `RemoteClient` MAY keep an optional client-side
TTL cache; the authoritative cache stays in `Client`.

### Enrich and detail builders moved into `mlwh`

`enrich.go` (new in `mlwh`). The enrich classifier cascade and graph assembly
move wholesale from `seqmeta/enrich.go` into `mlwh`. `EnrichmentResult`,
`EnrichmentGraph`, `MissingHop`, and the hop/reason constants move into
`mlwh/types.go` (the existing `mlwh.SampleDetail`/`StudyDetail`/`RunDetail`/
`LibraryDetail` result structs stay; the new builder methods return them). The
graph JSON contract is preserved field-for-field so existing frontend vitest
enrichment fixtures pass unchanged.

```go
// In mlwh/types.go (moved from seqmeta, field tags preserved exactly):
type EnrichmentGraph struct {
    Study        *Study        `json:"study,omitempty"`
    Studies      []Study       `json:"studies,omitempty"`
    Sample       *Sample       `json:"sample,omitempty"`
    Samples      []Sample      `json:"samples,omitempty"`
    Library      *Library      `json:"library,omitempty"`
    Libraries    []Library     `json:"libraries,omitempty"`
    StudyDetail  *StudyDetail  `json:"study_detail,omitempty"`
    StudyDetails []StudyDetail `json:"study_details,omitempty"`
    SampleDetail *SampleDetail `json:"sample_detail,omitempty"`
}

type MissingHop struct {
    Hop    string `json:"hop"`
    Reason string `json:"reason"`
    Status int    `json:"status"`
}

type EnrichmentResult struct {
    Identifier string          `json:"identifier"`
    Type       IdentifierKind  `json:"type"`
    Graph      EnrichmentGraph `json:"graph"`
    Partial    bool            `json:"partial"`
    Missing    []MissingHop    `json:"missing,omitempty"`
}
```

`Client.Enrich` owns the classifier cascade (study-id, study-accession,
sanger-sample-name, library-type, etc., exactly as `seqmeta/enrich.go` ordered
them) and composes the detail builders internally. The `enrichDetailProvider`
fallback and the `buildStudyDetailFromProvider`-style functions are deleted;
`Enrich` calls the promoted `StudyDetail`/`SampleDetail`/`RunDetail`/
`LibraryDetail` methods directly. `MaxSamplesPerHop` truncation and `partial` /
`MissingHop` semantics are preserved.

### `SearchValues` named struct

`hierarchy.go`. `ExpandSearchValues` returns one struct instead of three
slices:

```go
type SearchValues struct {
    Samples []string `json:"samples"`
    Runs    []string `json:"runs"`
    Lanes   []string `json:"lanes"`
}

func (c *Client) ExpandSearchValues(ctx context.Context, kind IdentifierKind, canonical string) (SearchValues, error)
```

### `mlwhdiff` (renamed `seqmeta`) narrowed surface

`mlwhdiff/provider.go` shrinks to exactly the `mlwh` surface diffing needs,
expressed via `mlwh.Queryer` so it runs against a local or remote `mlwh` with
no code change:

```go
// mlwhdiff depends on mlwh.Queryer; it carries mlwh types and defines no MLWH
// domain shapes of its own. The diff machinery uses only:
//   AllStudies, SamplesForStudy, IRODSPathsForSample
type DiffSource = mlwh.Queryer
```

Removed from the old `Provider`/`ClientAdapter`: `ClassifyIdentifier`,
`Resolve*`, `Find*`, `GetStudy`, `AllSamplesForStudy`, the `SamplesFor*`
family beyond `SamplesForStudy`, `LibrariesForStudy`, `StudiesForSample`,
`LanesForSample`, `GetSampleFiles`, and the `mlwh.Querier` embed. `store.go`'s
watermark/tombstone shape and `diff.go`'s algorithm are unchanged. The
`enrich_cache` table, `WithEnrichTTL`, `SaveEnrichCache`/`LoadEnrichCache`/
`DeleteEnrichCache`/`invalidateEnrichFor`, and the `DELETE /enrich/{id}` route
are deleted outright.

### Config surface

| Env var / flag                         | Mode     | Purpose                                                                             |
| -------------------------------------- | -------- | ----------------------------------------------------------------------------------- |
| WA_MLWH_SERVER_URL / --mlwh-server-url | both     | When set, consumers build a RemoteClient; when unset, OpenCacheOnly a local Client. |
| WA_MLWH_CACHE_PATH                     | local    | Cache backend path/DSN. Required only in local mode.                                |
| WA_MLWH_CACHE_PASSWORD                 | local    | Cache MySQL/SQLCipher password.                                                     |
| WA_MLWH_DSN/PASSWORD                   | sync     | Upstream MLWH for `wa mlwh sync` only (unchanged).                                  |
| WA_MLWHDIFF_BACKEND_URL                | frontend | Only if the frontend makes change-tracking calls (else dropped).                    |
| WA_MLWH_BACKEND_URL                    | frontend | Replaces WA_SEQMETA_BACKEND_URL (dropped). Frontend -> mlwh server.                 |

`wa mlwh serve` flags mirror `wa results serve` security flags
(`--cert`, `--key`, `--server-token`, `--url`/`--port`) plus `--mlwh-cache`.
Auth is off unless a server token / cert is configured.

### Server auth/transport

`wa mlwh serve` uses gas exactly as `wa results serve`: `gas.New`,
`EnableAuthWithServerToken` (only when secured), `authServer.Start(addr, cert,
key)`. Unauthenticated default registers every registry endpoint on the public
router (`gas.EndPointREST` group with no auth callback enforced) and binds
plain HTTP. Secured mode (any of `--server-token`/`--cert`/`--key` or their
`WA_MLWH_SERVER_*` env equivalents set) enables gas JWT/Bearer + TLS and
registers the same endpoints behind the auth group. Passwords never appear in a
DSN, flag, or command line.

---

## A. Eliminate seqmeta misuse for current-state queries

### A1: results validator calls mlwh directly

As the results server, I want identifier classification to use my in-process
`mlwh.Queryer` instead of an HTTP hop to seqmeta, so validation needs no second
service.

`SeqmetaValidator` (HTTP client to `/validate/*`) is replaced by an
`MLWHValidator` that holds an `mlwh.Queryer` and calls `ClassifyIdentifier`.
`ValidateMetadataValues` keeps its signature and the `seqmeta_*` metadata-key
scanning (the persisted metadata-key prefix is unchanged; see Key Decisions).
Type mismatch -> `ErrSeqmetaRejected`-equivalent (renamed `ErrMLWHRejected`);
`ErrNotFound` -> rejected; `ErrUpstreamImpaired`/`ErrCacheNeverSynced` ->
failed. `NewSeqmetaValidator(baseURL, timeout)` is replaced by
`NewMLWHValidator(q mlwh.Queryer)`.

**Package:** `results/`
**File:** `results/validate.go`
**Test file:** `results/validate_test.go`

```go
type MLWHValidator struct { q mlwh.Queryer }
func NewMLWHValidator(q mlwh.Queryer) *MLWHValidator
func (v *MLWHValidator) ValidateMetadataValues(ctx context.Context, metadata map[string][]string) error
```

**Acceptance tests:**

1. Given a fake `mlwh.Queryer` whose `ClassifyIdentifier("6568")` returns
   `Match{Kind: KindStudyLimsID}` and metadata
   `{"seqmeta_id_study_lims": ["6568"]}`, when `ValidateMetadataValues` runs,
   then it returns nil and no HTTP request is made (verified by the absence of
   any `*http.Client` field on `MLWHValidator`).
2. Given the queryer returns `Match{Kind: KindSangerSampleName}` for value
   `"X"` under key `"seqmeta_id_study_lims"`, when validate runs, then the
   error wraps `ErrMLWHRejected` and names expected vs actual type.
3. Given the queryer returns `mlwh.ErrNotFound`, when validate runs, then the
   error wraps `ErrMLWHRejected` with "identifier not found".
4. Given the queryer returns `mlwh.ErrUpstreamImpaired`, when validate runs,
   then the error wraps `ErrMLWHFailed` (not rejected).
5. Given metadata key `"seqmeta_unknown"`, when validate runs, then the error
   wraps `ErrInvalidInput` naming the unknown field, and the queryer is never
   called.
6. Given a nil validator, when `ValidateMetadataValues` runs, then it returns
   nil (auth-optional behaviour preserved).

### A2: results sample resolver calls mlwh directly

As the results search/expansion layer, I want sample/lane resolution to use the
in-process `mlwh.Queryer`, retiring the `SeqmetaSampleResolver` HTTP client.

`SeqmetaSampleResolver` and its `/study/{id}/samples` + `/enrich/*` HTTP calls
are deleted. `MLWHSearchResolver` (already wrapping `mlwh` directly) becomes the
sole `SearchResolver`. Its `Expand` keeps returning `([]string samples,
[]string runs, []string lanes, error)` to its results-server callers, but
internally calls `ExpandSearchValues` (now returning `SearchValues`) and reads
`.Samples/.Runs/.Lanes`. Study-sample and library-sample resolution route
through `SamplesForStudy` / `SamplesForLibrary*` / `LanesForSample` on the
`mlwh.Queryer`.

**Package:** `results/`
**File:** `results/server.go`, `results/mlwh_search_resolver.go`
**Test file:** `results/server_test.go`, `results/mlwh_search_resolver_test.go`

**Acceptance tests:**

1. Given a `grep` of `results/` after the change, when run, then there is no
   `SeqmetaSampleResolver` type and no occurrence of `/study/` or `/enrich/`
   built as an outbound HTTP path in `results/server.go`.
2. Given an `MLWHSearchResolver` over a fake `mlwh.Queryer` whose
   `ExpandSearchValues(KindStudyLimsID, "6568")` returns
   `SearchValues{Samples: ["A","B"], Runs: ["100"], Lanes: ["100_1_0"]}`, when
   `Expand(ctx, KindStudyLimsID, "6568")` runs, then it returns
   `(["A","B"], ["100"], ["100_1_0"], nil)`.
3. Given the queryer returns `mlwh.ErrNotFound` from `ExpandSearchValues`, when
   `Expand` runs, then it returns empty slices and nil error (current
   not-found-is-empty behaviour preserved).
4. Given the queryer returns `mlwh.ErrCacheNeverSynced`, when `Expand` runs,
   then the error wraps `mlwh.ErrCacheNeverSynced`.

### A3: frontend Server Actions hit the mlwh server

As the frontend, I want validate/enrich/studies/study-samples to call the
`mlwh` server via `WA_MLWH_BACKEND_URL`, retiring the seqmeta current-state
calls.

`backend-client.ts`: `BackendEnvVar` gains `"WA_MLWH_BACKEND_URL"` and drops
`"WA_SEQMETA_BACKEND_URL"`; `seqmetaJson` -> `mlwhJson(path, schema)` using
service `"mlwh"` and `WA_MLWH_BACKEND_URL`. `actions.ts`:
`validateIdentifier` -> `GET /classify/:id`; `enrichIdentifier` /
`enrichIdentifiers` -> `GET /enrich/:id`; `fetchStudySamples` ->
`GET /study/:id/samples`; `fetchStudyLibrarySamples` chooses the endpoint by
which filter is set (A4). `studies-cache.ts`: `GET /studies`. 404 still maps to
null; the error envelope `{code, message}` is parsed (frontend may ignore
`code`, surfacing 404 vs other as today).

**Package:** `frontend/`
**File:** `frontend/lib/backend-client.ts`, `frontend/app/(results)/actions.ts`,
`frontend/lib/studies-cache.ts`
**Test file:** `frontend/tests/actions.test.ts`,
`frontend/tests/backend-client.test.ts`, `frontend/tests/studies-cache.test.ts`

**Acceptance tests:**

1. Given `WA_MLWH_BACKEND_URL=https://mlwh:9000` and a mock fetch, when
   `validateIdentifier("6568")` runs, then the request URL is
   `https://mlwh:9000/classify/6568` and the parsed result satisfies
   `identifierResultSchema`.
2. Given a mock 404 from `/classify/:id`, when `validateIdentifier` runs, then
   it returns null.
3. Given a mock fetch, when `enrichIdentifier("6568")` runs, then the request
   URL ends `/enrich/6568` and the body parses as `enrichmentResultSchema`.
4. Given a mock fetch, when `fetchStudySamples("6568")` runs, then the request
   URL ends `/study/6568/samples` and it returns the `sanger_id`s.
5. Given a `grep` of `frontend/` (excluding `.next/`), when run, then there is
   no `WA_SEQMETA_BACKEND_URL` and no `seqmetaJson`.

### A4: split study-samples endpoint selection in the frontend

As the frontend library-filter, I want each filter to call its own endpoint, so
no single route multiplexes methods.

`fetchStudyLibrarySamples(studyId, libraryType, {libraryId, idLibraryLims})`
selects: `idLibraryLims` set -> `GET /library-lims-id/:idLibraryLims/samples`;
else `libraryId` set -> `GET /library-id/:libraryId/samples`; else
`GET /library/:libraryType/study/:studyId/samples`. Response parses as
`enrichmentSamplesSchema`.

**Package:** `frontend/`
**File:** `frontend/app/(results)/actions.ts`
**Test file:** `frontend/tests/actions.test.ts`

**Acceptance tests:**

1. Given `fetchStudyLibrarySamples("6568","Standard",{})`, then the request URL
   ends `/library/Standard/study/6568/samples`.
2. Given `fetchStudyLibrarySamples("6568","Standard",{libraryId:"L1"})`, then
   the URL ends `/library-id/L1/samples`.
3. Given `fetchStudyLibrarySamples("6568","Standard",{idLibraryLims:"DN1"})`,
   then the URL ends `/library-lims-id/DN1/samples` (lims id wins over
   libraryId when both set).

---

## B. mlwh REST server (gin) with registry-derived handlers

### B1: declarative registry

As a developer, I want one declarative list mapping each `Queryer` method to
its endpoint, so the handler and `RemoteClient` derive from one source.

**Package:** `mlwh/`
**File:** `mlwh/registry.go`
**Test file:** `mlwh/registry_test.go`

**Acceptance tests:**

1. Given `Registry`, when iterated, then it has exactly 33 entries and the set
   of `Method` names equals the method set of the `Queryer` interface
   (asserted by reflecting over a `Queryer`-typed nil and comparing names).
2. Given `Registry`, when checked, then every `Path` is unique and every
   `Verb` is `"GET"`.
3. Given each entry with `Paginated == true`, when checked, then its
   corresponding `Queryer` method has trailing `(limit, offset int)` params
   (asserted via reflection on method signatures).
4. Given the `SamplesForStudy` entry, then `Path == "/study/:id/samples"`,
   `PathParams == ["id"]`, `Query == []`, `Paginated == true`, and
   `NewResult()` is `*[]Sample`.
5. Given the `SamplesForLibrary` entry, then
   `Path == "/library/:pipeline/study/:study/samples"` and
   `PathParams == ["pipeline","study"]`.

### B2: gin handler builds from the registry

As an operator, I want `wa mlwh serve` to answer every endpoint by dispatching
to the local `Client`, cache-only.

`server.go` exposes `NewServer(q Queryer, opts...) *Server` and
`(*Server).RegisterRoutes(router *gin.Engine, auth *gin.RouterGroup)`. For each
`Registry` entry it registers a handler that: extracts path params (unescaped)
and `limit`/`offset`+filters, calls the `Queryer` method by name via a generated
dispatch (a per-entry closure, not reflection at request time), and writes the
result as JSON 200 or the error envelope. `limit`/`offset` default to a large
fetch-all limit and 0 when absent, matching today's `providerFetchLimit`.

**Package:** `mlwh/`
**File:** `mlwh/server.go`
**Test file:** `mlwh/server_test.go`

**Acceptance tests:**

1. Given a server over a fake `Queryer` whose `SamplesForStudy("6568",N,0)`
   returns two samples, when `GET /study/6568/samples` is served, then status
   is 200 and the body is a 2-element `[]Sample` JSON array.
2. Given `GET /study/6568/samples?limit=2&offset=1`, then the queryer receives
   `limit=2, offset=1`.
3. Given the queryer returns `mlwh.ErrNotFound`, when
   `GET /study/6568/samples` is served, then status is 404 and the body is
   `{"code":"not_found","message":...}`.
4. Given the queryer returns `mlwh.ErrCacheNeverSynced`, then status is 503 and
   `code` is `"cache_never_synced"`.
5. Given the queryer returns `mlwh.ErrAmbiguous` from `ResolveStudy`, when
   `GET /resolve/study/x` is served, then status is 409, code `"ambiguous"`.
6. Given the queryer returns `mlwh.ErrUnsupportedIdentifier` from
   `ClassifyIdentifier`, when `GET /classify/SQSCP` is served, then status is
   422, code `"unsupported_identifier"`.
7. Given `GET /enrich/6568` and a queryer returning an `EnrichmentResult` with
   `Partial=false`, then status is 200 and the JSON has a top-level `graph`
   object with no `project`/`users` keys.
8. Given the server handler source, when audited, then no handler reads or
   writes any cache or in-memory map of its own (caching is the `Client`'s
   responsibility); the only state a handler closes over is the `Queryer`.

### B3: RemoteClient round-trips Queryer

As a Go consumer, I want a `RemoteClient` that satisfies `Queryer` against
`wa mlwh serve` and round-trips the sentinels.

**Package:** `mlwh/`
**File:** `mlwh/remote.go`
**Test file:** `mlwh/remote_test.go`

**Acceptance tests:**

1. Given `var _ Queryer = (*RemoteClient)(nil)`, then it compiles.
2. Given a `RemoteClient` pointed at an `httptest.Server` returning a 2-element
   `[]Sample`, when `SamplesForStudy(ctx,"6568",100,0)` runs, then the request
   path is `/study/6568/samples?limit=100&offset=0` and it returns the two
   samples.
3. Given the test server returns 404 `{"code":"not_found"}`, when
   `ResolveStudy` runs, then the returned error satisfies
   `errors.Is(err, ErrNotFound)`.
4. Given 503 `{"code":"cache_never_synced"}` from a list endpoint, when
   `SamplesForStudy` runs, then the error satisfies both
   `errors.Is(err, ErrCacheNeverSynced)` and `errors.Is(err, ErrNotFound)`.
5. Given 409 `{"code":"ambiguous"}`, then `errors.Is(err, ErrAmbiguous)`.
6. Given 422 `{"code":"unsupported_identifier"}`, then
   `errors.Is(err, ErrUnsupportedIdentifier)`.
7. Given 502 `{"code":"upstream_impaired"}`, then
   `errors.Is(err, ErrUpstreamImpaired)`.
8. Given a path identifier containing `/` or spaces, when any method runs, then
   the outbound path segment is URL-escaped.
9. Given `RemoteConfig.Token` set, when any method runs, then the request
   carries an `Authorization: Bearer <token>` header.

### B4: RemoteClient <-> Client parity

As a maintainer, I want proof that both `Queryer` implementations return
identical results against the same cache and round-trip the same sentinels.

A parity harness opens a real SQLite cache via `OpenCacheOnly`, seeds it,
constructs a `Client` and a `RemoteClient` pointed at an `httptest.Server`
wrapping the same `Client`, and asserts equal results per method.

**Package:** `mlwh/`
**File:** `mlwh/parity_test.go`
**Test file:** `mlwh/parity_test.go`

**Acceptance tests:**

1. Given a seeded cache with studies/samples/libraries/lanes/irods, when each of
   the 33 `Queryer` methods is invoked on both `Client` and `RemoteClient` with
   identical args, then `reflect.DeepEqual(localResult, remoteResult)` holds for
   every method (JSON round-trip equality; counts asserted, not per-row `So` in
   a loop).
2. Given a never-synced cache, when `SamplesForStudy` is invoked on both, then
   both errors satisfy `errors.Is(_, ErrCacheNeverSynced)` and
   `errors.Is(_, ErrNotFound)`.
3. Given an identifier the cache cannot resolve, when `ResolveStudy` is invoked
   on both, then both errors satisfy `errors.Is(_, ErrNotFound)`.
4. Given a study-name with two matches, when `ResolveStudy` is invoked on both,
   then both satisfy `errors.Is(_, ErrAmbiguous)`.

---

## C. Detail builders, Enrich, and the enrich contract

### C1: detail-builder methods on Client

As a caller, I want `SampleDetail`/`StudyDetail`/`RunDetail`/`LibraryDetail` as
real cache-backed methods, each backed by indexed reads, each its own endpoint.

The builders move from `seqmeta/enrich.go` into `mlwh` and become methods that
compose existing hierarchy reads (`SamplesForStudy`, `LibrariesForStudy`,
`LanesForSample`, `IRODSPathsForSample`, `StudiesForSample`, `SamplesForRun`).
No new columns are needed (all backing reads already exist and are indexed; see
the read-path audit in `.docs/mlwh-sync/spec.md`).

**Package:** `mlwh/`
**File:** `mlwh/enrich.go`
**Test file:** `mlwh/enrich_test.go`

**Acceptance tests:**

1. Given a warm cache where study `6568` has libraries `Standard` (x10) and
   `Bespoke` (x3), when `StudyDetail(ctx,"6568")` runs, then the result has
   `Study.IDStudyLims == "6568"`, `len(Libraries) == 2`, and total samples
   across libraries is 13, with zero MLWH queries (cache-only).
2. Given a sample with 3 lanes, when `SampleDetail(ctx, sangerName)` runs, then
   `len(Lanes) == 3` and `Sample.Name == sangerName`.
3. Given a missing study, when `StudyDetail` runs, then the error is
   `ErrNotFound`.
4. Given a never-synced cache, when any detail method runs, then the error
   satisfies `errors.Is(_, ErrCacheNeverSynced)`.
5. Given a run `100` with samples on two studies, when `RunDetail(ctx,"100")`
   runs, then `len(Samples) >= 1`, `len(Studies) >= 1`, and the run id is 100.

### C2: Enrich composite method preserves the contract

As the frontend, I want `Enrich` to return the same graph the seqmeta
`/enrich/{id}` returned, field-for-field.

`Client.Enrich(ctx, identifier)` runs the same classifier cascade and graph
assembly as the old `seqmeta.Enrich`, composing the C1 detail methods. Returns
`EnrichmentResult` value (not pointer). Truncation at `MaxSamplesPerHop` sets
`Partial=true` and appends a `MissingHop{Reason: ReasonSamplesTruncated}`.

**Package:** `mlwh/`
**File:** `mlwh/enrich.go`
**Test file:** `mlwh/enrich_test.go`

**Acceptance tests:**

1. Given study `6568` with 2 libraries, 5 samples, 2 runs, when
   `Enrich(ctx,"6568")` runs, then `Graph.StudyDetail.Study.IDStudyLims ==
"6568"`, `len(Graph.StudyDetail.Libraries) == 2`, total samples 5,
   `Partial == false`.
2. Given sample `7607STDY14643771` with 3 lanes, when `Enrich` on it runs, then
   `Graph.SampleDetail.Lanes` has length 3 and the JSON has no `project`/`users`
   keys.
3. Given a library hop returning `MaxSamplesPerHop+500` rows, when `Enrich`
   runs, then `Partial == true`, exactly one
   `MissingHop{Reason: ReasonSamplesTruncated}` is present, and the emitted
   samples length is `MaxSamplesPerHop`.
4. Given an unknown identifier, when `Enrich` runs, then the error wraps
   `ErrNotFound` (the server maps it to 404).
5. Given the existing frontend enrichment fixtures
   (`frontend/tests/*enrich*`), when run against the `mlwh` server JSON output,
   then they pass unchanged (the graph shape is byte-compatible with the old
   seqmeta output).

---

## D. Rename seqmeta -> mlwhdiff and narrow it

### D1: package, CLI, store, env rename

As a maintainer, I want no `seqmeta` service string surviving except genuine
change-tracking under `mlwhdiff`.

Renames: package `seqmeta` -> `mlwhdiff`; directory `seqmeta/` -> `mlwhdiff/`;
`wa seqmeta` -> `wa mlwhdiff` (`cmd/seqmeta.go` -> `cmd/mlwhdiff.go`); default
store file `seqmeta.db` -> `mlwhdiff.db`; `WA_SEQMETA_BACKEND_URL` dropped;
sentinel/text `seqmeta:` -> `mlwhdiff:`. Subcommands: `diff`, `serve` only
(`validate` removed). The persisted `seqmeta_*` metadata-key prefix in
`results`/`frontend` is NOT renamed (Key Decisions).

**Package:** `mlwhdiff/`, `cmd/`
**File:** all `mlwhdiff/*.go`, `cmd/mlwhdiff.go`
**Test file:** existing tests renamed

**Acceptance tests:**

1. Given `go vet ./...` and `grep -rn "package seqmeta" .`, when run, then
   there are zero matches.
2. Given a `grep -rn "wa seqmeta" --include=*.go .`, when run (excluding
   migration/changelog docs), then there are zero matches.
3. Given `wa mlwhdiff --help`, when run, then it lists subcommands `diff` and
   `serve` and no `validate`.
4. Given `wa mlwhdiff serve` with no `--db`, then the default store path
   basename is `mlwhdiff.db`.
5. Given a `grep -rn "WA_SEQMETA_BACKEND_URL" .` (excluding `.next/`), when
   run, then there are zero matches in source.

### D2: narrowed provider and deleted current-state code

As `mlwhdiff`, I want only change-tracking concerns, depending on
`mlwh.Queryer`.

`provider.go` is replaced by a dependency on `mlwh.Queryer` (alias
`DiffSource`). `client_adapter.go` is reduced to whatever thin adapter remains
(or deleted if `mlwh.Queryer` is used directly). `enrich.go`, `validate.go`,
`types.go`'s enrich types, the `enrich_cache` store code, and the current-state
handlers are deleted.

**Package:** `mlwhdiff/`
**File:** `mlwhdiff/provider.go`, `mlwhdiff/diff.go`, `mlwhdiff/store.go`
**Test file:** `mlwhdiff/diff_test.go`, `mlwhdiff/store_test.go`

**Acceptance tests:**

1. Given the `mlwhdiff` package, when listed, then there is no `enrich.go`,
   `validate.go`, `enrich_cache_test.go`, `server_enrich_test.go`, or
   `validate_test.go`, and `grep -rn "enrich" mlwhdiff/` returns zero matches.
2. Given `mlwhdiff.DiffSource`, when its method set is checked, then it equals
   the `mlwh.Queryer` method set (it is an alias), and the diff machinery uses
   only `AllStudies`, `SamplesForStudy`, `IRODSPathsForSample`.
3. Given a fresh store and a `DiffSource` returning two studies, when the study
   diff runs, then `added` has length 2, `modified` and `removed` empty.
4. Given a second poll with one study's name changed, when the diff runs, then
   `modified` has length 1 with the changed `IDStudyLims`.
5. Given a study removed between polls, when the diff runs, then `removed`
   includes its `IDStudyLims` and the tombstone is set.
6. Given the store schema, when inspected, then it has no `enrich_cache` table
   and `WithEnrichTTL`/`SaveEnrichCache`/`DeleteEnrichCache` symbols do not
   exist.

### D3: mlwhdiff server on gin, diff routes only

As an operator, I want `wa mlwhdiff serve` to expose only the diff routes, on
gin.

`mlwhdiff/server.go` migrates from chi to gin. Routes: `GET /diff/study/:id`,
`GET /diff/sample/:id`. The current-state handlers and the `DELETE /enrich/*`
route are gone.

**Package:** `mlwhdiff/`
**File:** `mlwhdiff/server.go`
**Test file:** `mlwhdiff/server_test.go`

**Acceptance tests:**

1. Given the server, when `GET /validate/x`, `GET /enrich/x`, `GET /studies`,
   `GET /study/6568/samples`, or `DELETE /enrich/x` are requested, then each
   returns 404 (route absent).
2. Given a `DiffSource` returning two studies, when `GET /diff/study/all`
   runs, then status is 200 and `added` has length 2.
3. Given `GET /diff/sample/:id` where `IRODSPathsForSample` returns
   `ErrNotFound`, then status is 404.
4. Given the server source, when checked, then it imports `gin` and not
   `go-chi/chi`.

---

## E. Update remaining consumers and wiring

### E1: ExpandSearchValues returns SearchValues end to end

As a caller, I want one named response struct so the interface, handler, and
`RemoteClient` share one shape.

`(*Client).ExpandSearchValues` returns `(SearchValues, error)`. Its results
callers (`results/mlwh_search_resolver.go` `Expand`, and
`cmd/results.go` `resultsServeMLWHRuntime.ExpandSearchValues` and the
`resultsServeSyncClient` / `mlwhSearchExpander` interfaces) are updated to the
new signature, reading `.Samples/.Runs/.Lanes`. `ExpandIdentifier` (returns
`[]TaggedID`) and `ExpandSampleSearchValues` (returns `[]string`) are unchanged.

**Package:** `mlwh/`, `results/`, `cmd/`
**File:** `mlwh/hierarchy.go`, `results/mlwh_search_resolver.go`,
`cmd/results.go`
**Test file:** `mlwh/hierarchy_test.go`, `results/mlwh_search_resolver_test.go`

**Acceptance tests:**

1. Given a warm cache, when `ExpandSearchValues(ctx, KindStudyLimsID, "6568")`
   runs, then it returns `SearchValues{Samples,Runs,Lanes}` equal (by set) to
   the three slices the old three-return version produced for the same input.
2. Given a `grep` for `[]string, []string, []string, error` across `mlwh/`,
   `results/`, `cmd/`, when run, then the only matches are
   `ExpandSampleSearchValues`-unrelated; `ExpandSearchValues` returns
   `(SearchValues, error)` everywhere (interfaces in
   `results/mlwh_search_resolver.go` and `cmd/results.go` updated).
3. Given the `mlwh` server registry entry for `ExpandSearchValues`, then its
   `NewResult()` is `*SearchValues` and `GET /expand-search/:kind/:id` returns
   the struct JSON.

### E2: results serve wiring selects local or remote mlwh

As `wa results serve`, I want to build an `mlwh.Queryer` (local `Client` or
`RemoteClient`) by config, passing it to the validator and resolver.

`resultsServeSyncClient` is split: sync stays on the local-only path; the
query surface is `mlwh.Queryer`. When `WA_MLWH_SERVER_URL`/`--mlwh-server-url`
is set, build a `RemoteClient` (no `--mlwh-cache` required, no `Sync`); else
`OpenCacheOnly` a local `Client`. `NewServer(store, validator, resolver, ...)`
gets `validator = NewMLWHValidator(q)` and
`resolver = NewMLWHSearchResolver(q)`. The `--seqmeta-url`/`--seqmeta-timeout`
flags and `WA_SEQMETA_BACKEND_URL` default are removed.

**Package:** `cmd/`
**File:** `cmd/results.go`
**Test file:** `cmd/results_serve_test.go`

**Acceptance tests:**

1. Given `WA_MLWH_SERVER_URL=https://mlwh:9000` set and `WA_MLWH_CACHE_PATH`
   unset, when `wa results serve` builds its mlwh handle, then it constructs a
   `RemoteClient` and does not error on the missing cache path.
2. Given `WA_MLWH_SERVER_URL` unset and `WA_MLWH_CACHE_PATH` set, when serve
   builds its handle, then it calls `OpenCacheOnly`.
3. Given neither set, when serve runs, then it errors with a message naming
   `WA_MLWH_SERVER_URL` or `WA_MLWH_CACHE_PATH`.
4. Given `wa results serve --help`, when run, then there is no `--seqmeta-url`
   flag.
5. Given the built server, when a register request validates a `seqmeta_*`
   metadata value, then validation uses the `mlwh.Queryer` (no outbound HTTP to
   a seqmeta service).

### E3: mlwhdiff serve and CLI wiring selects local or remote mlwh

As `wa mlwhdiff serve`, I want the same local/remote selection.

`cmd/mlwhdiff.go` builds an `mlwh.Queryer` the same way (E2) and passes it as
the `DiffSource`. `--mlwh-cache` is required only in local mode.

**Package:** `cmd/`
**File:** `cmd/mlwhdiff.go`
**Test file:** `cmd/mlwhdiff_test.go`

**Acceptance tests:**

1. Given `WA_MLWH_SERVER_URL` set, when `wa mlwhdiff serve` builds its source,
   then it is a `RemoteClient`.
2. Given `WA_MLWH_SERVER_URL` unset and `WA_MLWH_CACHE_PATH` set, then it is a
   local cache-only `Client`.
3. Given a `DiffSource` (either impl) returning two studies, when
   `GET /diff/study/all` is served, then `added` has length 2.

### E4: wa mlwh serve command

As an operator, I want `wa mlwh serve` to run the registry-backed server,
cache-only, with optional gas auth.

`cmd/mlwh.go` adds `newMLWHServeCommand`: `OpenCacheOnly` a local `Client`,
build `mlwh.NewServer(client)`, wire gas (auth off by default; secured when
token/cert configured), and `Start`. It never triggers sync;
`--mlwh-sync-interval` is not added here (sync stays `wa mlwh sync`).

**Package:** `cmd/`
**File:** `cmd/mlwh.go`
**Test file:** `cmd/mlwh_test.go`

**Acceptance tests:**

1. Given `wa mlwh serve` with `WA_MLWH_CACHE_PATH` set to a never-synced cache,
   when `GET /studies` is requested, then status is 503 with code
   `"cache_never_synced"`.
2. Given a synced cache, when `GET /studies` is requested with no auth
   configured, then status is 200 (unauthenticated by default).
3. Given `wa mlwh serve` with a server token and cert configured, when an
   endpoint is requested without a Bearer token, then status is 401.
4. Given `wa mlwh serve` with no `WA_MLWH_CACHE_PATH` and no
   `--mlwh-cache`, then it errors naming the missing cache configuration.
5. Given the serve command source, when audited, then it never calls
   `client.Sync`.

### E5: frontend file/component/contract renames

As the frontend, I want surviving seqmeta-named files renamed to reflect that
current state comes from `mlwh`.

`lib/seqmeta-cache-core.ts` -> `lib/mlwh-cache-core.ts`,
`lib/seqmeta-cache.ts` -> `lib/mlwh-cache.ts`,
`lib/seqmeta-cache-server.ts` -> `lib/mlwh-cache-server.ts`,
`lib/seqmeta-enrichment.ts` -> `lib/mlwh-enrichment.ts`,
`components/seqmeta-badge.tsx` -> `components/mlwh-badge.tsx`. The
`SeqmetaCache` class -> `MLWHCache`; cookie name constant value left unchanged
(persisted-cookie compat) but the exported symbol renamed. `seqmeta-keys.ts`
and the `seqmeta_*` metadata-key strings are NOT renamed (Key Decisions); the
file may stay `seqmeta-keys.ts`. Imports updated repo-wide.

**Package:** `frontend/`
**File:** the renamed `lib/mlwh-*.ts`, `components/mlwh-badge.tsx`
**Test file:** corresponding vitest suites

**Acceptance tests:**

1. Given the renamed files, when `npx tsc --noEmit` runs, then there are zero
   unresolved-import errors.
2. Given a `grep -rn "seqmeta-cache\|seqmeta-enrichment\|seqmeta-badge"
frontend/` (excluding `.next/`), when run, then there are zero matches.
3. Given the enrichment vitest fixtures, when run against the renamed module,
   then they pass unchanged.

---

## F. Extension mechanism

### F1: add-a-query checklist and parity discipline

As an LLM or developer, I want a documented start-to-finish checklist to add a
new MLWH query: schema column+index (both dialects) if needed, one `Client`
method, one `Queryer` member, one `Registry` entry.

`DEVELOPING.md` gains an "Add a new MLWH query" section and `mlwh/registry.go`
carries a package doc comment with the same 4 steps. The checklist references
the read-path-audit discipline of `.docs/mlwh-sync/spec.md` (every served
column traceable to an indexed read path) and the schema parity test.

**Package:** `mlwh/`, repo root
**File:** `DEVELOPING.md`, `mlwh/registry.go`
**Test file:** `mlwh/registry_test.go`, `mlwh/cache_schema_test.go` (existing
parity test reused)

**Acceptance tests:**

1. Given `DEVELOPING.md`, when read, then it contains an "Add a new MLWH query"
   section listing exactly the 4 steps (schema+index in both dialects ->
   `Client` method -> `Queryer` member -> `Registry` entry) and references the
   read-path audit and the parity test.
2. Given the `mlwh/registry.go` package/var doc comment, when read, then it
   states the registry is the single source from which the handler and
   `RemoteClient` derive, and that adding a method requires adding a registry
   entry.
3. Given the existing schema parity test, when run, then it still asserts
   identical table/column/index sets across `cache_schema/sqlite/*.sql` and
   `cache_schema/mysql/*.sql` (regression guard for step 1).
4. Given a hypothetical `Queryer` method with no `Registry` entry (simulated in
   the test by removing one entry), when `TestRegistryCoversQueryer` runs, then
   it fails -- proving the registry/interface parity is enforced (B1.1).

---

## Implementation Order

Phases build on tested foundations. Within a phase, stories may be done in
parallel unless noted.

1. **Phase 1 - mlwh query surface foundations.** E1 (`SearchValues` reshape),
   C1 (detail-builder methods), C2 (`Enrich` moved into `mlwh`). These are
   pure `mlwh`-package additions/moves with no server yet. (E1 before C2 if
   any builder uses search values; otherwise parallel.)
2. **Phase 2 - Queryer + registry + server + remote.** B1 (registry), then B2
   (gin server) and B3 (`RemoteClient`) in parallel, then B4 (parity). Defines
   `mlwh.Queryer` (depends on Phase 1 method set). F1 (docs/parity guard) lands
   with B1.
3. **Phase 3 - mlwh serve command.** E4 (`wa mlwh serve`). Depends on Phase 2.
4. **Phase 4 - repoint results.** A1 (validator), A2 (resolver), E2 (serve
   wiring). Depends on Phases 1-3 (needs `Queryer`, `RemoteClient`, server).
5. **Phase 5 - rename + narrow mlwhdiff.** D1, D2, D3, E3. Depends on Phase 2
   (`mlwh.Queryer`). Independent of frontend.
6. **Phase 6 - frontend.** A3, A4, E5. Depends on Phase 3 (server endpoints
   exist). Independent of Phase 5.

---

## Appendix: Key Decisions

- **Why a registry.** A single declarative list makes the
  method<->endpoint correspondence mechanical (a reader/LLM sees a method and
  knows its route and vice-versa) and ensures the handler and `RemoteClient`
  never drift, satisfying Goals 3 and 4. `TestRegistryCoversQueryer` (B1.1)
  enforces 1:1 parity at build time.

- **Caching stays in `Client`.** Per the constraints, handlers add no cache;
  the synced cache and the existing `ExpandIdentifier` memo serve both
  in-process and HTTP callers. Per-query audit: every endpoint is backed by an
  indexed cache read already audited in `.docs/mlwh-sync/spec.md`; no new
  in-memory cache is justified, so none is added (the default of relying on the
  synced schema + indexes holds). The persisted `enrich_cache` is dropped;
  the frontend's client-side enrichment cache may remain.

- **The persisted `seqmeta_*` metadata-key prefix is intentionally NOT
  renamed.** These keys (`seqmeta_id_study_lims`, `seqmeta_sample_name`, ...)
  are a stable on-disk (results store) and on-the-wire (search/filter) contract
  for MLWH-derived registration metadata, not the seqmeta _service_. They are
  current-state, not change-tracking, but renaming them would require a results
  DB data migration and a coordinated search/filter contract change that none
  of the 5 goals require. The `seqmeta` _service_, package, CLI, env var, store
  file, and frontend client files are all renamed; the metadata-key naming
  convention and `frontend/lib/seqmeta-keys.ts` are out of scope. This is the
  one place "seqmeta" survives that is neither change-tracking nor the service.
  Implementors must not "fix" it. (If a future task wants it renamed, that is a
  separate migration spec.)

- **gas auth, off by default.** The `mlwh` server mirrors `wa results serve`'s
  gas wiring but defaults to unauthenticated (MLWH contents are fine for
  internal users). Secured (gas JWT + TLS) mode is mandatory only for
  cross-cluster (HPC<->OpenStack) exposure, per institutional policy. The
  `RemoteClient` carries an optional Bearer token + CA cert for the secured
  case.

- **Sentinel round-tripping.** A stable `code` string in the JSON envelope is
  the contract; status codes are for HTTP-layer consumers. `ErrCacheNeverSynced`
  keeps 503 while the reconstructed client error preserves today's
  `errors.Is(_, ErrNotFound)` wrapping for list endpoints, so consumers that
  currently treat never-synced as not-found keep working.

- **Reads stay cache-only.** The server never triggers sync and never falls
  back to live MLWH, inheriting `ErrCacheNeverSynced`. Sync stays `wa mlwh sync`
  (manual/cron) plus the existing `--mlwh-sync-interval` on `wa results serve`.

- **Testing.** GoConvey throughout. `mlwh` server handler tests
  (`httptest`), `RemoteClient` tests (`httptest`), and the `RemoteClient`<->
  `Client` parity test (real `modernc.org/sqlite` cache). `results` tests drop
  the seqmeta HTTP fakes and use a fake `mlwh.Queryer`. `mlwhdiff` tests keep
  the diff coverage and drop all enrich/validate/current-state tests. Frontend
  vitest points at `mlwh` endpoints; enrichment fixtures are unchanged
  (contract preserved). Live-MLWH integration tests stay gated on
  `WA_MLWH_DSN`. Cache-schema parity test is the regression guard for step 1 of
  the extension checklist. See `go-implementor` and `go-reviewer` skills
  (and `nextjs-fastapi-*` for the frontend, treated as Next.js TS) for the TDD
  loop.
