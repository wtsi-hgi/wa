# SAGA API Go Client Library Specification

## Overview

A Go library (`saga` package) providing an idiomatic client for the
SAGA API at `https://saga.cellgeni.sanger.ac.uk/api`. Caches GET
responses with `github.com/wtsi-hgi/activecache`, retries transient
failures with `github.com/wtsi-ssg/wr/retry`, and provides high-level
convenience methods for 4 key research use cases that merge data across
MLWH and iRODS sources.

All code lives in a single `saga` package. Sub-clients are receiver
structs on the main Client (e.g. `client.MLWH()` returns a struct),
not separate Go packages. This spec covers Core, MLWH, iRODS,
Projects, and Saga Samples & Studies endpoints. Future integrations
(STAN, FreezerPro, HuMFre, OMERO, SlideTracker) are out of scope,
but the architecture accommodates them.

## Architecture

**Package:** `saga/`

### Types

```go
// Client is the top-level SAGA API client.
type Client struct {
    apiKey   string
    baseURL  string
    http     *http.Client
    cache    *activecache.Cache[string, []byte]
    mlwh     *MLWHClient
    irods    *IRODSClient
    projects *ProjectsClient
    samples  *SamplesClient
}

// Option configures Client via functional options.
type Option func(*Client)

// APIError represents an HTTP error from the SAGA API.
type APIError struct {
    StatusCode int
    Message    string
}

// Sentinel errors.
var (
    ErrNoAPIKey    = errors.New("saga: API key required")
    ErrUnauthorized = errors.New("saga: unauthorized (401)")
    ErrNotFound     = errors.New("saga: not found (404)")
    ErrServerError  = errors.New("saga: server error (5xx)")
)

// PaginatedResponse is the generic paginated envelope.
type PaginatedResponse[T any] struct {
    Items  []T  `json:"items"`
    Total  *int `json:"total"`
    Offset int  `json:"offset"`
    Limit  int  `json:"limit"`
}

// PageOptions controls per-page requests.
// Note: the API also accepts an `export` query parameter for
// server-side CSV/Excel export. This is excluded because the Go
// client always consumes JSON responses.
type PageOptions struct {
    Page      int
    PageSize  int
    SortField string
    SortOrder string // "asc" or "desc"
}

// FilterOptions for key use-case filtering (client-side).
// Metadata keys match iRODS AVU attribute names (e.g.
// "library_type") and MLWH field names (e.g. "common_name")
// when cross-referencing sources.
type FilterOptions struct {
    AnalysisType AnalysisType
    Metadata     map[string][]string // field -> acceptable values
}

// AnalysisType is a typed string for known analysis types.
type AnalysisType string

const (
    AnalysisCellrangerMulti    AnalysisType = "cellranger multi"
    AnalysisSpacerangerCount   AnalysisType = "spaceranger count"
    AnalysisCellrangerCount    AnalysisType = "cellranger count"
    AnalysisCellrangerATAC     AnalysisType = "cellranger-atac count"
    AnalysisCellrangerARC      AnalysisType = "cellranger-arc count"
)
```

### MLWH types

```go
type Study struct {
    IDStudyTmp              int     `json:"id_study_tmp"`
    IDLims                  string  `json:"id_lims"`
    IDStudyLims             string  `json:"id_study_lims"`
    Name                    string  `json:"name"`
    FacultySponsor          string  `json:"faculty_sponsor"`
    State                   string  `json:"state"`
    Abstract                string  `json:"abstract"`
    Abbreviation            string  `json:"abbreviation"`
    AccessionNumber         string  `json:"accession_number"`
    Description             string  `json:"description"`
    DataReleaseStrategy     string  `json:"data_release_strategy"`
    StudyTitle              string  `json:"study_title"`
    DataAccessGroup         string  `json:"data_access_group"`
    HMDMCNumber             string  `json:"hmdmc_number"`
    Programme               string  `json:"programme"`
    Created                 string  `json:"created"`
    ReferenceGenome         string  `json:"reference_genome"`
    EthicallyApproved       bool    `json:"ethically_approved"`
    StudyType               string  `json:"study_type"`
    ContainsHumanDNA        bool    `json:"contains_human_dna"`
    ContaminatedHumanDNA    bool    `json:"contaminated_human_dna"`
    StudyVisibility         string  `json:"study_visibility"`
    EGADACAccession         string  `json:"ega_dac_accession_number"`
    EGAPolicyAccession      string  `json:"ega_policy_accession_number"`
    DataReleaseTiming       string  `json:"data_release_timing"`
}

type MLWHSample struct {
    IDStudyLims          string  `json:"id_study_lims"`
    IDSampleLims         string  `json:"id_sample_lims"`
    SangerID             string  `json:"sanger_id"`
    SampleName           string  `json:"sample_name"`
    TaxonID              int     `json:"taxon_id"`
    CommonName           string  `json:"common_name"`
    LibraryType          string  `json:"library_type"`
    IDRun                int     `json:"id_run"`
    Lane                 int     `json:"lane"`
    TagIndex             int     `json:"tag_index"`
    IRODSPath            string  `json:"irods_path"`
    StudyAccessionNumber string  `json:"study_accession_number"`
    AccessionNumber      string  `json:"accession_number"`
}

type FacultySponsor struct {
    Name string `json:"name"`
}

type Programme struct {
    Name string `json:"name"`
}

type DataReleaseStrategy struct {
    Name string `json:"name"`
}
```

### iRODS types

```go
type IRODSSample struct {
    ID       int               `json:"id"`
    Name     string            `json:"name"`
    Source   string            `json:"source"`
    SourceID string            `json:"source_id"`
    Data     map[string]any    `json:"data"`
    Curated  map[string]any    `json:"curated"`
    Parent   *int              `json:"parent"`
}

type IRODSFile struct {
    ID         int              `json:"id"`
    Collection string           `json:"collection"`
    Metadata   []IRODSMetadata  `json:"metadata"`
}

type IRODSMetadata struct {
    Name  string `json:"name"`
    Value string `json:"value"`
}

type IRODSAnalysisType struct {
    Name string `json:"name"`
}
```

### Projects types

```go
type Project struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

type ProjectSample struct {
    ID       int    `json:"id"`
    SangerID string `json:"sanger_id"`
}

type ProjectStudy struct {
    ID          int    `json:"id"`
    IDStudyLims string `json:"id_study_lims"`
}

type ProjectUser struct {
    ID       int    `json:"id"`
    Username string `json:"username"`
}
```

### Saga Samples & Studies types

```go
// SagaSample is the internal Saga sample entity. Same shape as
// IRODSSample; Source distinguishes origin (e.g. "IRODS").
type SagaSample struct {
    ID       int            `json:"id"`
    Name     string         `json:"name"`
    Source   string         `json:"source"`
    SourceID string         `json:"source_id"`
    Data     map[string]any `json:"data"`
    Curated  map[string]any `json:"curated"`
    Parent   *int           `json:"parent"`
}

// SagaStudy is the internal Saga study entity.
type SagaStudy struct {
    ID          int    `json:"id"`
    IDStudyLims string `json:"id_study_lims"`
    Name        string `json:"name"`
}

// CreateSampleRequest is the POST body for creating a Saga sample.
type CreateSampleRequest struct {
    Source   string `json:"source"`
    SourceID string `json:"source_id"`
}
```

### Core types

```go
type VersionInfo struct {
    Rev *string `json:"rev"`
}

type UserInfo struct {
    Username string `json:"username"`
}

type TokenResponse struct {
    Token string `json:"token"`
}

type User struct {
    ID       int    `json:"id"`
    Username string `json:"username"`
}
```

### Domain types (key use cases)

```go
// SampleMetadata merges MLWH sample data with iRODS AVU metadata.
type SampleMetadata struct {
    SangerID    string
    SampleName  string
    TaxonID     int
    CommonName  string
    LibraryType string
    StudyID     string
    MLWH        []MLWHSample       // all MLWH rows for this sample
    IRODSFiles  []IRODSFile        // iRODS collections/files
    AVUs        map[string][]string // merged iRODS AVU metadata
}

// StudySamples holds all samples associated with a study.
type StudySamples struct {
    StudyID string
    Samples []MLWHSample
}

// SampleFiles holds iRODS files for a sample.
type SampleFiles struct {
    SangerID string
    Files    []IRODSFile
}

// StudyFiles holds iRODS files for all samples in a study.
type StudyFiles struct {
    StudyID string
    Files   []IRODSFile
}
```

### Sub-clients

```go
type MLWHClient struct{ c *Client }
type IRODSClient struct{ c *Client }
type ProjectsClient struct{ c *Client }
type SamplesClient struct{ c *Client }
```

### Constructor & Options

```go
func NewClient(apiKey string, opts ...Option) (*Client, error)
func WithBaseURL(url string) Option
func WithCacheDuration(d time.Duration) Option
func (c *Client) Close()            // calls cache.Stop()

func (c *Client) MLWH() *MLWHClient
func (c *Client) IRODS() *IRODSClient
func (c *Client) Projects() *ProjectsClient
func (c *Client) Samples() *SamplesClient
```

Defaults: base URL `https://saga.cellgeni.sanger.ac.uk/api`,
cache duration 5 minutes, HTTP timeout 30s. Retries use
`github.com/wtsi-ssg/wr/retry` with `backoff.Backoff{Min: 250ms,
Max: 3s, Factor: 1.5}` and `UntilLimit{Max: 3}` combined with
`UntilNoError{}`.

### HTTP layer

All requests include:
- `X-Api-Key: <apiKey>` header
- `User-Agent: wtsi-hgi/wa` header

Cache key = HTTP method + full URL (including query). Only GET
responses are cached. POST/DELETE are never cached and invalidate
related entries.

### Error handling

`APIError` implements `error`. `Error()` returns
`"saga: HTTP <StatusCode>: <Message>"`. Sentinel errors are returned
for 401, 404, and 5xx via `errors.Is()` support (`Unwrap` returns
the sentinel).

### Testing

- Mock tests: `net/http/httptest.Server` returning canned JSON.
- Integration tests: skip unless `SAGA_TEST_API_TOKEN` is set. Use
  study `6568`, sample `WTSI_wEMB10524782`, study `3361`, sample
  `3361STDY5994718`.

---

## A. Client Lifecycle

### A1: Create client with defaults

As a developer, I want to create a SAGA client with just an API key,
so that sensible defaults apply.

**Package:** `saga/`
**File:** `saga/client.go`
**Test file:** `saga/client_test.go`

```go
func NewClient(apiKey string, opts ...Option) (*Client, error)
```

**Acceptance tests:**

1. Given API key `"test-key"`, when `NewClient("test-key")` is called,
   then error is nil, the returned Client is non-nil, `Close()` does
   not panic, base URL is
   `"https://saga.cellgeni.sanger.ac.uk/api"`.
2. Given API key `""`, when `NewClient("")` is called, then
   `errors.Is(err, ErrNoAPIKey)` is true and Client is nil.

### A2: Create client with custom options

As a developer, I want to override base URL and cache duration, so
that I can target staging or adjust caching.

**Acceptance tests:**

1. Given `WithBaseURL("http://localhost:8080")`, when creating a
   client with that option, then requests target that base URL.
2. Given `WithCacheDuration(10 * time.Minute)`, when creating a
   client, then the cache uses that duration.
3. Given both options combined, when creating a client, then both
   are applied.

### A3: Client Close

As a developer, I want to call `Close()` to stop cache goroutines,
so that resources are released cleanly.

**Acceptance tests:**

1. Given a client, when `Close()` is called, then the cache stops
   refreshing (no goroutine leak).
2. Given a closed client, when `Close()` is called again, then it
   does not panic.

---

## B. HTTP Layer & Error Handling

### B1: Request headers

As a developer, I want all requests to include correct auth and
user-agent headers, so that the API accepts them.

**Package:** `saga/`
**File:** `saga/http.go`
**Test file:** `saga/http_test.go`

```go
// Internal method; tested via mock server header inspection.
func (c *Client) doGet(ctx context.Context, path string,
    query url.Values) ([]byte, error)
func (c *Client) doPost(ctx context.Context, path string,
    body any) ([]byte, error)
func (c *Client) doDelete(ctx context.Context,
    path string) error
```

**Acceptance tests:**

1. Given a mock server, when any GET request is made, then the
   request has `X-Api-Key: test-key` and
   `User-Agent: wtsi-hgi/wa` headers.
2. Given a mock server, when a POST request is made, then the same
   headers are present and Content-Type is `application/json`.

### B2: API error handling

As a developer, I want structured errors with HTTP status codes, so
that I can handle errors programmatically.

**Acceptance tests:**

1. Given a mock server returning 401, when a request is made, then
   the error wraps `ErrUnauthorized`, and
   `errors.Is(err, ErrUnauthorized)` is true.
2. Given 404 response, then `errors.Is(err, ErrNotFound)` is true.
3. Given 500 response, then `errors.Is(err, ErrServerError)` is true.
4. Given 200 response, then error is nil.
5. Given `APIError{StatusCode: 401, Message: "bad key"}`, then
   `err.Error()` is `"saga: HTTP 401: bad key"`.

### B3: Retry with backoff

As a developer, I want transient errors (5xx, timeouts) to be retried
automatically, so that flaky network issues are tolerated.

**Acceptance tests:**

1. Given a mock server that returns 500 twice then 200, when a GET
   request is made, then the response is the 200 body and no error.
2. Given a mock server that returns 500 four times, when a GET
   request is made (max 3 retries), then `ErrServerError` is returned.
3. Given a mock server that returns 401, when a GET request is made,
   then no retry occurs (only 1 request total) and `ErrUnauthorized`
   is returned.

### B4: Caching

As a developer, I want GET responses cached, so that repeated calls
avoid redundant API requests.

**File:** `saga/cache.go`
**Test file:** `saga/cache_test.go`

**Acceptance tests:**

1. Given a mock server, when the same GET path is called twice, then
   only 1 HTTP request is made to the server.
2. Given a cached response, when a POST to a related resource is made,
   then the cached entry is invalidated and the next GET hits the
   server.
3. Given a POST/DELETE request, then it is never served from cache.

---

## C. Core Endpoints

### C1: Health check (GET /)

As a developer, I want to check if the API is reachable.

**Package:** `saga/`
**File:** `saga/core.go`
**Test file:** `saga/core_test.go`

```go
func (c *Client) Ping(ctx context.Context) error
```

**Acceptance tests:**

1. Given a healthy mock server returning 200, when `Ping()` is called,
   then error is nil.
2. Given a mock server returning 500, when `Ping()` is called, then
   error is non-nil.

### C2: Version (GET /version)

```go
func (c *Client) Version(ctx context.Context) (*VersionInfo, error)
```

**Acceptance tests:**

1. Given mock returning `{"rev":"abc123"}`, when `Version()` is called,
   then `Rev` is `"abc123"`.
2. Given mock returning `{"rev":null}`, then `Rev` is nil.

### C3: Current user (GET /auth/me)

```go
func (c *Client) AuthMe(ctx context.Context) (*UserInfo, error)
```

**Acceptance tests:**

1. Given mock returning `{"username":"alice"}`, when `AuthMe()` is
   called, then `Username` is `"alice"`.

### C4: Generate token (POST /auth/token)

```go
func (c *Client) GenerateToken(ctx context.Context) (
    *TokenResponse, error)
```

**Acceptance tests:**

1. Given mock returning `{"token":"new-tok"}`, when `GenerateToken()`
   is called, then `Token` is `"new-tok"`.

### C5: List users (GET /users/)

```go
func (c *Client) ListUsers(ctx context.Context) ([]User, error)
```

**Acceptance tests:**

1. Given mock returning 2 users, when `ListUsers()` is called, then
   the slice has length 2 with correct usernames.

---

## D. MLWH Endpoints

### D1: List studies (paginated)

**Package:** `saga/`
**File:** `saga/mlwh.go`
**Test file:** `saga/mlwh_test.go`

```go
func (m *MLWHClient) ListStudies(ctx context.Context,
    opts PageOptions) (*PaginatedResponse[Study], error)
func (m *MLWHClient) AllStudies(ctx context.Context) (
    []Study, error)
```

**Acceptance tests:**

1. Given mock returning page 1 of 2 studies with `total: 5`, when
   `ListStudies(ctx, PageOptions{Page: 1, PageSize: 2})` is called,
   then Items has 2 elements, Total is 5.
2. Given mock returning 3 pages (2+2+1 items), when `AllStudies()` is
   called, then result has 5 studies.
3. Given mock returning page 1 OK then page 2 errors, when
   `AllStudies()` is called, then partial results (page 1 items) are
   returned along with the error.
4. Given empty result (0 items, total 0), when `ListStudies()` is
   called, then Items is empty slice (not nil), Total is 0.

### D2: Get study

```go
func (m *MLWHClient) GetStudy(ctx context.Context,
    studyID string) (*Study, error)
```

**Acceptance tests:**

1. Given mock returning study JSON for ID `"3361"`, when
   `GetStudy(ctx, "3361")` is called, then Name is
   `"IHTP_ISC_IBDCA_Edinburgh"` and FacultySponsor is
   `"David Adams"`.
2. Given mock returning 404, then `ErrNotFound` is returned.

### D3: List samples (paginated)

```go
func (m *MLWHClient) ListSamples(ctx context.Context,
    opts PageOptions) (*PaginatedResponse[MLWHSample], error)
func (m *MLWHClient) AllSamples(ctx context.Context) (
    []MLWHSample, error)
```

Note: total may be null. Auto-pagination continues until an empty
page is returned.

**Acceptance tests:**

1. Given mock returning `{"items":[...], "total":null}` with 2 items
   on page 1 and 0 on page 2, when `AllSamples()` is called, then 2
   samples are returned.
2. Given mock with `total: null`, when `ListSamples()` is called,
   then Total field is nil.
3. Given the same sample appears twice (different runs), when
   `AllSamples()` is called, then both rows are returned (no
   deduplication).

### D4: List faculty sponsors

```go
func (m *MLWHClient) ListFacultySponsors(ctx context.Context,
    opts PageOptions) (
    *PaginatedResponse[FacultySponsor], error)
func (m *MLWHClient) AllFacultySponsors(ctx context.Context) (
    []FacultySponsor, error)
```

**Acceptance tests:**

1. Given mock returning 2 sponsors, when `ListFacultySponsors()` is
   called, then Items has 2 entries.

### D5: List programmes

```go
func (m *MLWHClient) ListProgrammes(ctx context.Context) (
    []Programme, error)
```

**Acceptance tests:**

1. Given mock returning 3 programmes, when called, then result has 3
   entries with correct names.

### D6: List data release strategies

```go
func (m *MLWHClient) ListDataReleaseStrategies(
    ctx context.Context) ([]DataReleaseStrategy, error)
```

**Acceptance tests:**

1. Given mock returning `[{"name":"managed"},{"name":"open"}]`,
   when called, then 2 strategies are returned.

---

## E. iRODS Endpoints

### E1: iRODS root (GET /integrations/irods/)

**Package:** `saga/`
**File:** `saga/irods.go`
**Test file:** `saga/irods_test.go`

```go
func (i *IRODSClient) Ping(ctx context.Context) error
```

**Acceptance tests:**

1. Given mock returning 200, when called, then error is nil.
2. Given mock returning 500, then error wraps `ErrServerError`.

### E2: List iRODS samples (paginated)

```go
func (i *IRODSClient) ListSamples(ctx context.Context,
    opts PageOptions) (*PaginatedResponse[IRODSSample], error)
func (i *IRODSClient) AllSamples(ctx context.Context) (
    []IRODSSample, error)
```

**Acceptance tests:**

1. Given mock returning 2 iRODS samples with nested `data` maps,
   when `ListSamples(ctx, PageOptions{Page: 1, PageSize: 10})` is
   called, then Items has 2 elements and Data maps are populated.
2. Given mock returning `data` with `avu:study_id: ["6568"]`, when
   parsed, then `Data["avu:study_id"]` is `["6568"]`.
3. Given mock returning all 3 pages, when `AllSamples()` is called,
   then all items are collected.

### E3: Get sample files (GET /integrations/irods/samples/{sanger_id})

```go
func (i *IRODSClient) GetSampleFiles(ctx context.Context,
    sangerID string) ([]IRODSFile, error)
```

**Acceptance tests:**

1. Given mock returning 1 file with collection path and metadata,
   when `GetSampleFiles(ctx, "WTSI_wEMB10524782")` is called, then
   result has 1 IRODSFile with correct Collection and Metadata.
2. Given mock returning empty items, then result is empty slice.
3. Given mock returning 404, then `ErrNotFound` is returned.

### E4: Web summary (GET /integrations/irods/web-summary/{collection})

```go
func (i *IRODSClient) GetWebSummary(ctx context.Context,
    collection string) ([]byte, error)
```

**Acceptance tests:**

1. Given mock returning HTML body, when called, then raw bytes are
   returned.

### E5: List analysis types

```go
func (i *IRODSClient) ListAnalysisTypes(ctx context.Context) (
    []IRODSAnalysisType, error)
```

**Acceptance tests:**

1. Given mock returning 5 types, when called, then 5 items are
   returned including `"cellranger count"` and `"spaceranger count"`.

---

## F. Projects Endpoints

### F1: List projects

**Package:** `saga/`
**File:** `saga/projects.go`
**Test file:** `saga/projects_test.go`

```go
func (p *ProjectsClient) List(ctx context.Context) (
    []Project, error)
```

**Acceptance tests:**

1. Given mock returning 2 projects, when called, then 2 projects
   with correct IDs and names.

### F2: Add project

```go
func (p *ProjectsClient) Add(ctx context.Context,
    name string) (*Project, error)
```

**Acceptance tests:**

1. Given mock accepting POST and returning `{"id":1, "name":"proj"}`,
   when `Add(ctx, "proj")` is called, then returned project has
   ID 1, Name `"proj"`.
2. After adding, the projects list cache is invalidated.

### F3: Get project

```go
func (p *ProjectsClient) Get(ctx context.Context,
    projectID int) (*Project, error)
```

**Acceptance tests:**

1. Given mock returning project 1, when called, then correct project.
2. Given 404, then `ErrNotFound`.

### F4: Project samples (list + add + remove)

```go
func (p *ProjectsClient) ListSamples(ctx context.Context,
    projectID int) ([]ProjectSample, error)
func (p *ProjectsClient) AddSample(ctx context.Context,
    projectID int, sampleID string) (*ProjectSample, error)
func (p *ProjectsClient) RemoveSample(ctx context.Context,
    projectID int, sampleID int) error
```

**Acceptance tests:**

1. Given mock returning 3 samples for project 1, when
   `ListSamples(ctx, 1)` is called, then 3 samples returned.
2. Given POST adding sample, when `AddSample(ctx, 1, "ABC123")` is
   called, then returned sample has correct SangerID, and the
   project-1 samples cache is invalidated.
3. Given DELETE removing sample, when `RemoveSample(ctx, 1, 42)` is
   called, then no error and project-1 samples cache is invalidated.

### F5: Project studies (list + add + remove)

```go
func (p *ProjectsClient) ListStudies(ctx context.Context,
    projectID int) ([]ProjectStudy, error)
func (p *ProjectsClient) AddStudy(ctx context.Context,
    projectID int, studyID string) (*ProjectStudy, error)
func (p *ProjectsClient) RemoveStudy(ctx context.Context,
    projectID int, studyID int) error
```

**Acceptance tests:**

1. Given mock returning 2 studies for project 1, then 2 returned.
2. After `AddStudy`, project-1 studies cache is invalidated.
3. After `RemoveStudy`, project-1 studies cache is invalidated.

### F6: Project users (list + add + remove)

```go
func (p *ProjectsClient) ListUsers(ctx context.Context,
    projectID int) ([]ProjectUser, error)
func (p *ProjectsClient) AddUser(ctx context.Context,
    projectID int, username string) (*ProjectUser, error)
func (p *ProjectsClient) RemoveUser(ctx context.Context,
    projectID int, userID int) error
```

**Acceptance tests:**

1. Given mock returning 1 user, then 1 returned.
2. After `AddUser`, project-1 users cache is invalidated.
3. After `RemoveUser`, project-1 users cache is invalidated.

---

## G. Key Use Cases

### G1: All metadata for a sample

**Package:** `saga/`
**File:** `saga/usecases.go`
**Test file:** `saga/usecases_test.go`

```go
func (c *Client) SampleAllMetadata(ctx context.Context,
    sangerID string) (*SampleMetadata, error)
```

Merges MLWH sample rows (all runs/lanes) with iRODS file metadata.
AVUs are collected from all iRODS files into a single deduplicated
`map[string][]string`.

**Acceptance tests:**

1. Given mock MLWH returning 2 rows for sample `"S1"` (different
   runs) and mock iRODS returning 1 file with metadata
   `[{name:"study_id", value:"100"}, {name:"library_type",
   value:"Chromium"}]`, when `SampleAllMetadata(ctx, "S1")` is
   called, then:
   - `SangerID` is `"S1"`
   - `MLWH` has 2 entries
   - `IRODSFiles` has 1 entry
   - `AVUs["study_id"]` is `["100"]`
   - `AVUs["library_type"]` is `["Chromium"]`
   - `SampleName`, `TaxonID`, `CommonName` are from the first MLWH
     row.
2. Given MLWH returns samples but iRODS returns 404, then result
   still contains MLWH data with empty IRODSFiles and AVUs.
3. Given neither MLWH nor iRODS has the sample, then `ErrNotFound`
   is returned.

### G2: All samples in a study

```go
func (c *Client) StudyAllSamples(ctx context.Context,
    studyID string) (*StudySamples, error)
```

Auto-paginates MLWH samples, collecting those with matching
`id_study_lims`. Fetches all MLWH samples and filters client-side.

**Acceptance tests:**

1. Given mock MLWH returning 5 samples across 2 pages where 3 have
   `id_study_lims: "100"`, when `StudyAllSamples(ctx, "100")` is
   called, then `StudySamples.Samples` has 3 entries.
2. Given no samples match, then `Samples` is empty slice (not nil).
3. Given pagination fails on page 2, then partial results from
   page 1 that match are returned with the error.

### G3: iRODS files for a sample

```go
func (c *Client) SampleIRODSFiles(ctx context.Context,
    sangerID string, filter *FilterOptions) (
    *SampleFiles, error)
```

Calls `IRODS().GetSampleFiles()` then applies client-side filtering.

**Acceptance tests:**

1. Given mock returning 3 files where 2 have metadata
   `analysis_type: "cellranger count"`, when called with
   `FilterOptions{AnalysisType: AnalysisCellrangerCount}`, then
   result has 2 files.
2. Given `FilterOptions{Metadata: map[string][]string{
   "library_type": {"Chromium single cell 3 prime v3"}}}`, when
   called, then only files with that library_type are returned.
3. Given nil FilterOptions, then all files are returned unfiltered.
4. Given empty result from iRODS, then `Files` is empty slice.

### G4: iRODS files for a study

```go
func (c *Client) StudyIRODSFiles(ctx context.Context,
    studyID string, filter *FilterOptions) (
    *StudyFiles, error)
```

Finds all iRODS samples with `avu:study_id` containing the study ID,
then fetches files for each matching sample. Falls back to MLWH
cross-reference if iRODS metadata is insufficient: fetches MLWH
samples for the study, extracts sanger_ids, and fetches iRODS files
for each. Applies client-side filtering.

**Acceptance tests:**

1. Given mock iRODS AllSamples returning 3 samples where 2 have
   `data["avu:study_id"]` containing `"100"`, and GetSampleFiles
   returns 1 file each, when `StudyIRODSFiles(ctx, "100", nil)` is
   called, then result has 2 files.
2. Given iRODS samples have no `avu:study_id` matching, but MLWH
   returns 2 samples for study `"100"` with sanger_ids `"S1"` and
   `"S2"`, and iRODS GetSampleFiles returns files for both, when
   called, then result has files from both samples.
3. Given `FilterOptions{AnalysisType: AnalysisSpacerangerCount}`,
   then only matching files are returned.
4. Given no samples found anywhere, then `Files` is empty slice.
5. Given MLWH cross-reference path is used and
   `FilterOptions{Metadata: map[string][]string{
   "common_name": {"Homo Sapien"}}}` is applied, when called, then
   only files whose MLWH sample has `CommonName == "Homo Sapien"`
   are returned.

---

## H. Saga Samples & Studies

### H1: List Saga samples (paginated)

**Package:** `saga/`
**File:** `saga/samples.go`
**Test file:** `saga/samples_test.go`

```go
func (s *SamplesClient) List(ctx context.Context,
    opts PageOptions) (*PaginatedResponse[SagaSample], error)
func (s *SamplesClient) All(ctx context.Context) (
    []SagaSample, error)
```

**Acceptance tests:**

1. Given mock returning 2 saga samples with `total: 5`, when
   `List(ctx, PageOptions{Page: 1, PageSize: 2})` is called, then
   Items has 2 elements, Total is 5.
2. Given mock returning 2 pages (2+1 items), when `All()` is
   called, then result has 3 samples.
3. Given empty result, then Items is empty slice (not nil).

### H2: Create Saga sample

```go
func (s *SamplesClient) Create(ctx context.Context,
    source, sourceID string) (*SagaSample, error)
```

**Acceptance tests:**

1. Given mock accepting POST with body
   `{"source":"IRODS","source_id":"123"}` and returning
   `{"id":1,"source":"IRODS","source_id":"123",...}`, when
   `Create(ctx, "IRODS", "123")` is called, then returned sample
   has ID 1 and Source `"IRODS"`.
2. After creating, the samples list cache is invalidated.

### H3: Get sample by source

```go
func (s *SamplesClient) GetBySource(ctx context.Context,
    source, sourceID string) (*SagaSample, error)
```

**Acceptance tests:**

1. Given mock returning sample for source `"IRODS"` and source_id
   `"456"`, when `GetBySource(ctx, "IRODS", "456")` is called,
   then returned sample has correct Source and SourceID.
2. Given 404 response, then `ErrNotFound` is returned.

### H4: Get samples for study by source

```go
func (s *SamplesClient) GetStudySamples(ctx context.Context,
    source, studySourceID string) ([]SagaSample, error)
```

**Acceptance tests:**

1. Given mock returning 3 samples for source `"IRODS"` and study
   source_id `"6568"`, when
   `GetStudySamples(ctx, "IRODS", "6568")` is called, then result
   has 3 samples.
2. Given empty result, then result is empty slice (not nil).
3. Given 404 response, then `ErrNotFound` is returned.

### H5: List Saga studies

```go
func (s *SamplesClient) ListStudies(ctx context.Context) (
    []SagaStudy, error)
```

**Acceptance tests:**

1. Given mock returning 2 studies, when `ListStudies()` is called,
   then result has 2 entries with correct IDs and names.
2. Given empty result, then result is empty slice (not nil).

---

## I. Integration Tests

### I1: Real API integration

**Package:** `saga/`
**File:** `saga/integration_test.go`

Tests skip unless `SAGA_TEST_API_TOKEN` env var is set.

```go
func TestIntegration(t *testing.T) {
    token := os.Getenv("SAGA_TEST_API_TOKEN")
    if token == "" {
        t.Skip("SAGA_TEST_API_TOKEN not set")
    }
    // ...
}
```

**Acceptance tests:**

1. Given a valid token, when `Ping()` is called, then no error.
2. Given a valid token, when `Version()` is called, then Rev is
   non-nil string.
3. Given study `"6568"`, when `MLWH().GetStudy()` is called, then
   the study name contains `"HCA"` or `"Embryo"` or similar known
   value.
4. Given sample `"WTSI_wEMB10524782"`, when
   `IRODS().GetSampleFiles()` is called, then at least 1 file is
   returned with non-empty Collection.
5. Given sample `"WTSI_wEMB10524782"`, when
   `SampleAllMetadata()` is called, then result has non-empty AVUs.
6. Given study `"3361"`, when `StudyAllSamples()` is called, then
   at least 1 sample is returned.
7. Given study `"6568"`, when `StudyIRODSFiles()` is called, then
   at least 1 file is returned.
8. Given sample `"WTSI_wEMB10524782"`, when
   `SampleIRODSFiles(ctx, "WTSI_wEMB10524782", nil)` is called,
   then at least 1 file is returned.
9. Given a valid token, when `Samples().ListStudies()` is called,
   then at least 1 study is returned.

---

## Implementation Order

### Phase 1: Core HTTP + Client

Stories: A1, A2, A3, B1, B2, B3, B4

Foundation: Client struct, functional options, HTTP layer with
headers, error types, retry, caching. All tested with mock HTTP
server. Sequential.

### Phase 2: Core Endpoints

Stories: C1, C2, C3, C4, C5

Simple endpoints using the HTTP layer from Phase 1. Each independent;
can be parallel.

### Phase 3: MLWH Endpoints

Stories: D1, D2, D3, D4, D5, D6

Paginated response handling, auto-pagination with partial error
returns. D1 first (establishes pagination pattern), then rest
parallel.

### Phase 4: iRODS Endpoints

Stories: E1, E2, E3, E4, E5

Similar pagination pattern reused from Phase 3. Can be parallel.

### Phase 5: Projects Endpoints

Stories: F1, F2, F3, F4, F5, F6

POST/DELETE methods, cache invalidation on mutations. F1 first (read
pattern), then F2-F6 parallel.

### Phase 6: Saga Samples & Studies

Stories: H1, H2, H3, H4, H5

Saga-internal entities. Same pagination/cache patterns as prior
phases. H1 first (establishes list pattern), then rest parallel.

### Phase 7: Key Use Cases

Stories: G1, G2, G3, G4

Cross-source data merging, client-side filtering. Depends on Phases
3, 4, 5, 6. G1 and G2 can be parallel; G3 and G4 can be parallel.

### Phase 8: Integration Tests

Stories: I1

Real API tests. Depends on all prior phases. Sequential.

---

## Appendix: Key Decisions

- **Single package:** All code in `saga/` to keep imports simple.
  Sub-clients are method-returned structs, not subpackages.
- **Client-side filtering:** Server-side `filters` parameter causes
  500 errors. All filtering is done post-fetch. Documented as a
  current limitation.
- **activecache keying:** Cache key is `"GET:<fullURL>"`. POST/DELETE
  never cached. Mutations invalidate specific keys (e.g. adding a
  project sample invalidates
  `"GET:<baseURL>/projects/<id>/samples"`).
- **Auto-pagination loads all results into a slice.** Memory caveat
  documented on AllX methods. Callers needing control use per-page
  methods. If pagination fails mid-way, partial results + error are
  returned.
- **MLWH samples total:null:** Auto-pagination stops on empty page.
- **AnalysisType:** Typed string constants for known values; arbitrary
  strings accepted via `AnalysisType("custom value")`.
- **FilterOptions.Metadata:** `map[string][]string` where a file
  matches if for every key, at least one of the file's metadata
  values for that key is in the acceptable values list. Keys match
  both iRODS AVU attribute names and MLWH struct field names when
  cross-referencing.
- **Context-first:** All public methods take `context.Context` as
  first parameter for cancellation/timeout support.
- **Testing:** GoConvey with `So()` assertions per go-conventions.
  Mock tests use `httptest.Server`. Integration tests gated by env
  var. Reference go-implementor and go-reviewer skills for TDD
  workflow.
- **Retry policy:** Only 5xx and timeout errors are retried. 4xx
  errors (401, 404, etc.) are not retried.
- **HTTP timeout:** 30s default via `http.Client.Timeout`.
