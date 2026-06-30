# Header-Aware Remote Pagination Specification

## Overview

`mlwh.RemoteClient` already receives REST response headers through `do` and
can turn bare JSON-array bodies into `Page[T]` through `remoteCallPage`.
Complete the exported remote-client API so every paged REST list can be called
as one bounded page with `X-Total-Count` and `X-Next-Offset` exposed.

Keep all existing REST paths, query params, response bodies, and body-only
remote methods. Do not widen `Queryer`; header metadata is an HTTP
remote-client concern.

## Architecture

- `mlwh/remote.go`: add typed page wrappers, `CallWithHeaders`, and shared
  helpers for body-plus-header result envelopes.
- `mlwh/types.go`: add public result envelopes for manifest and detail
  endpoints:

```go
type PagedStudyManifest struct {
    StudyManifest StudyManifest `json:"study_manifest"`
    Total         int           `json:"total"`
    NextOffset    int           `json:"next_offset"`
}

type PagedStudyDetail struct {
    StudyDetail StudyDetail `json:"study_detail"`
    Total       int         `json:"total"`
    NextOffset  int         `json:"next_offset"`
}

type PagedRunDetail struct {
    RunDetail  RunDetail `json:"run_detail"`
    Total      int       `json:"total"`
    NextOffset int       `json:"next_offset"`
}

type DetailOptions struct {
    Limit  int
    Offset int
    Lean   bool
}
```

- `remoteCallPage` remains the only `Page[T]` converter for bare-array
  endpoints. Header defaults stay `total=0`, `next_offset=-1`.
- Add a generic private helper over `rc.do` for non-array result envelopes:
  decode `*T`, read the two headers with `remoteHeaderInt`, and return the
  typed value plus totals.
- Query builders must reuse existing helpers:
  `remotePagination`, `remotePaginationWithAddedWindow`,
  `remotePaginationWithFileType`, `remotePaginationWithRole`, and
  `remoteManifestQuery`. Add `remoteDetailQuery(DetailOptions)` that always
  sends `limit` and `offset`, and sends `lean=true` only when `Lean` is true.
- Typed methods with `limit, offset` args always send both values literally.
  Omitted-query server defaults are available only through `CallWithHeaders` or
  another dynamic caller that supplies an empty query.
- Errors continue through `decodeRemoteError`; header-aware methods return zero
  values and the same sentinels as existing body-only methods.
- Update exported Go comments in `remote.go` and result-type comments in
  `types.go`. Do not change generated HTTP API docs unless a no-drift test
  requires it.

## A: Dynamic Header Access

### A1: Dynamic call with headers

As a Registry-driven caller, I want the decoded body and response headers, so
that dynamic clients can read pagination metadata without typed wrappers.

**Package:** `mlwh/`
**File:** `mlwh/remote.go`
**Test file:** `mlwh/remote_test.go`

```go
func (rc *RemoteClient) CallWithHeaders(
    ctx context.Context,
    method string,
    pathParams []string,
    query url.Values,
) (any, http.Header, error)
```

`CallWithHeaders` uses the same `rc.do` path as `Call`. `Call` becomes a
wrapper that discards headers.

**Acceptance tests:**

1. Given a stub server returning `[]Study{{IDStudyLims: "1"}}` with
   `X-Total-Count: 7` and `X-Next-Offset: 2`, when
   `CallWithHeaders(ctx, "AllStudies", nil,
url.Values{"limit": {"1"}, "offset": {"1"}})` runs, then the request URI
   is `/studies?limit=1&offset=1`, the result is `*[]Study` with one row,
   returned headers contain `X-Total-Count == "7"` and
   `X-Next-Offset == "2"`, and error is nil.
2. Given the same server, when `Call(ctx, "AllStudies", nil, query)` runs, then
   it returns the same `*[]Study` body and nil error.
3. Given an unknown method, path-param arity mismatch, or a remote error
   envelope with code `not_found`, when `CallWithHeaders` runs, then it returns
   nil result, non-nil error matching existing `Call` behavior, and does not
   mask `ErrNotFound`.

## B: Complete Bare-List Page Methods

### B1: Simple paged list wrappers

As a Go caller, I want every paged bare-list endpoint to have a typed
`Page[T]` method, so that I can get items and sizing headers from one call.

**Package:** `mlwh/`
**File:** `mlwh/remote.go`
**Test file:** `mlwh/remote_test.go`

Add these public methods:

```go
func (rc *RemoteClient) AllStudiesPage(
    ctx context.Context, limit, offset int,
) (Page[Study], error)

func (rc *RemoteClient) SamplesForRunPage(
    ctx context.Context, idRun string, limit, offset int,
) (Page[Sample], error)

func (rc *RemoteClient) SamplesForLibraryPage(
    ctx context.Context, pipelineIDLims, studyLimsID string,
    limit, offset int,
) (Page[Sample], error)

func (rc *RemoteClient) SamplesForLibraryIDPage(
    ctx context.Context, libraryID string, limit, offset int,
) (Page[Sample], error)

func (rc *RemoteClient) SamplesForLibraryLimsIDPage(
    ctx context.Context, idLibraryLims string, limit, offset int,
) (Page[Sample], error)

func (rc *RemoteClient) SamplesForLibraryTypePage(
    ctx context.Context, pipelineIDLims string, limit, offset int,
) (Page[Sample], error)

func (rc *RemoteClient) LibrariesForStudyPage(
    ctx context.Context, studyLimsID string, limit, offset int,
) (Page[Library], error)

func (rc *RemoteClient) RunsForStudyPage(
    ctx context.Context, studyLimsID string, limit, offset int,
) (Page[Run], error)

func (rc *RemoteClient) LanesForSamplePage(
    ctx context.Context, sangerName string, limit, offset int,
) (Page[Lane], error)

func (rc *RemoteClient) SearchStudiesPage(
    ctx context.Context, term string, limit, offset int,
) (Page[Study], error)

func (rc *RemoteClient) SearchSamplesPage(
    ctx context.Context, term string, limit, offset int,
) (Page[Sample], error)
```

Each method calls the same Registry method and path params as its body-only
counterpart, with `remotePagination(limit, offset)` and `remoteCallPage`.
`SearchStudiesPage` and `SearchSamplesPage` therefore always send explicit
pagination args; they rely on the server's search parser for default behavior
when query params are omitted by dynamic callers, and for the `SearchMaxLimit`
400 guard.

**Acceptance tests:**

1. For each method above, given a stub server returning two rows and headers
   `X-Total-Count: 5`, `X-Next-Offset: 4`, when the page method runs with
   `limit=2` and `offset=2`, then `Page.Items` has the two decoded rows,
   `Page.Total == 5`, `Page.NextOffset == 4`, and the request URI is exactly
   the body-only endpoint path with `?limit=2&offset=2`.
2. Given a parity server seeded by `newListSizingClientForTest`, when each new
   page method runs against a matching endpoint fixture, then `Page.Items`
   equals the body-only method result for the same args.
3. Given a server omitting headers, when any new page method succeeds, then
   `Page.Total == 0` and `Page.NextOffset == -1`.
4. Given a remote error envelope with code `cache_never_synced`, when any new
   page method runs, then it returns an empty `Page[T]` and an error satisfying
   the same sentinels as the body-only method.
5. Given stub servers, when `SearchStudiesPage(ctx, "malar", 0, 0)` and
   `SearchSamplesPage(ctx, "acme", 0, 0)` run, then URIs are
   `/search/study/malar?limit=0&offset=0` and
   `/search/sample/acme?limit=0&offset=0`; typed methods do not omit params to
   request search defaults.
6. Given a real MLWH server over a fake queryer, when `CallWithHeaders(ctx,
"SearchStudies", []string{"malar"}, nil)` runs, then URI is
   `/search/study/malar`, the queryer receives `limit == 100` and
   `offset == 0`, the decoded result is `*[]Study`, and returned headers are
   exposed.
7. Given a real MLWH server whose fake search queryer panics if called, when
   `SearchStudiesPage(ctx, "malar", SearchMaxLimit+1, 0)` and
   `SearchSamplesPage(ctx, "acme", SearchMaxLimit+1, 0)` run, then each request
   URI contains `limit=1001&offset=0`, each returns an empty `Page[T]`, and each
   error follows existing remote `bad_request` envelope handling.

## C: Filtered Page Methods

### C1: Windowed samples-with-data page

As a Go caller, I want windowed samples-with-data pages, so that the total
matches the same `[since, until)` request as the returned rows.

**Package:** `mlwh/`
**File:** `mlwh/remote.go`
**Test file:** `mlwh/remote_test.go`

```go
func (rc *RemoteClient) SamplesWithDataSincePage(
    ctx context.Context,
    studyLimsID, since, until string,
    limit, offset int,
) (Page[SampleWithData], error)
```

Use Registry method `SamplesWithData` with
`remotePaginationWithAddedWindow(limit, offset, since, until)`.

**Acceptance tests:**

1. Given a stub server returning one `SampleWithData` with headers
   `X-Total-Count: 3`, `X-Next-Offset: 2`, when
   `SamplesWithDataSincePage(ctx, "S1", "2026-06-01T00:00:00Z",
"2026-06-02T00:00:00Z", 1, 1)` runs, then URI path is
   `/study/S1/samples-with-data`, query values are exactly `limit=1`,
   `offset=1`, `since=2026-06-01T00:00:00Z`, and
   `until=2026-06-02T00:00:00Z`; `Total == 3`, `NextOffset == 2`, and
   `Items` has one row.
2. Given empty `since` and `until`, when the method runs, then the query string
   has only `limit` and `offset`.
3. Given upstream 400 for invalid timestamps or `until` without `since`, when
   the method runs, then the returned error matches existing remote bad-request
   handling and no `Page` metadata is fabricated.

### C2: iRODS file-type page variants

As a Go caller, I want file-type-filtered iRODS pages, so that totals are for
the filtered request and not the unfiltered list.

**Package:** `mlwh/`
**File:** `mlwh/remote.go`
**Test file:** `mlwh/remote_test.go`

```go
func (rc *RemoteClient) IRODSPathsForSamplePage(
    ctx context.Context, sangerName string, limit, offset int,
) (Page[IRODSPath], error)

func (rc *RemoteClient) IRODSPathsForSampleByFileTypePage(
    ctx context.Context, sangerName, fileType string, limit, offset int,
) (Page[IRODSPath], error)

func (rc *RemoteClient) IRODSPathsForStudyByFileTypePage(
    ctx context.Context, studyLimsID, fileType string, limit, offset int,
) (Page[IRODSPath], error)

func (rc *RemoteClient) IRODSPathsForRunByFileTypePage(
    ctx context.Context, idRun, fileType string, limit, offset int,
) (Page[IRODSPath], error)
```

`IRODSPathsForSamplePage` and existing `IRODSPathsForStudyPage` /
`IRODSPathsForRunPage` are unfiltered convenience wrappers. Filtered methods
use the same Registry methods as the body-only calls and
`remotePaginationWithFileType`.

**Acceptance tests:**

1. Given a stub server returning one `IRODSPath` with headers
   `X-Total-Count: 4`, `X-Next-Offset: 2`, when
   `IRODSPathsForSampleByFileTypePage(ctx, "S1", "cram", 1, 1)` runs, then URI
   is `/sample/S1/irods?file_type=cram&limit=1&offset=1`, `Total == 4`, and
   `NextOffset == 2`.
2. Given the same setup for study and run, when
   `IRODSPathsForStudyByFileTypePage(ctx, "ST1", "cram", 1, 1)` and
   `IRODSPathsForRunByFileTypePage(ctx, "52553", "cram", 1, 1)` run, then URIs
   are `/study/ST1/irods?file_type=cram&limit=1&offset=1` and
   `/run/52553/irods?file_type=cram&limit=1&offset=1`, with `Total == 4` and
   `NextOffset == 2`.
3. Given empty `fileType`, when any filtered page method runs, then `file_type`
   is omitted and the result equals the matching unfiltered page method.
4. Given upstream 400 for invalid `file_type`, when any filtered page method
   runs, then it returns the remote error and an empty page.

### C3: Existing filtered page coverage

As a maintainer, I want existing filtered page methods covered, so that they do
not regress while new methods are added.

**Package:** `mlwh/`
**File:** `mlwh/remote.go`
**Test file:** `mlwh/remote_test.go`

Existing methods remain:

```go
func (rc *RemoteClient) SamplesForStudyPage(
    ctx context.Context, studyLimsID string, limit, offset int,
) (Page[Sample], error)

func (rc *RemoteClient) SamplesWithDataPage(
    ctx context.Context, studyLimsID string, limit, offset int,
) (Page[SampleWithData], error)

func (rc *RemoteClient) SamplesWithoutDataPage(
    ctx context.Context, studyLimsID string, limit, offset int,
) (Page[SampleWithData], error)

func (rc *RemoteClient) IRODSPathsForStudyPage(
    ctx context.Context, studyLimsID string, limit, offset int,
) (Page[IRODSPath], error)

func (rc *RemoteClient) IRODSPathsForRunPage(
    ctx context.Context, idRun string, limit, offset int,
) (Page[IRODSPath], error)

func (rc *RemoteClient) StudiesForFacultySponsorPage(
    ctx context.Context, name string, limit, offset int,
) (Page[PersonStudy], error)

func (rc *RemoteClient) StudiesForUserPage(
    ctx context.Context, person, role string, limit, offset int,
) (Page[PersonStudy], error)

func (rc *RemoteClient) ResolvePersonPage(
    ctx context.Context, term string, limit, offset int,
) (Page[PersonCandidate], error)
```

**Acceptance tests:**

1. Given a stub server returning `[]PersonStudy{{Study: Study{IDStudyLims:
"S1", Name: "Alpha"}}}` with headers `X-Total-Count: 3`,
   `X-Next-Offset: 1`, when `StudiesForFacultySponsorPage(ctx, "carl", 1, 0)`
   runs, then URI is `/studies/faculty-sponsor/carl?limit=1&offset=0`,
   `Items` equals the decoded row, `Total == 3`, and `NextOffset == 1`.
2. Given a stub server returning `[]PersonStudy{{Study: Study{IDStudyLims:
"S2"}, Role: "follower"}}` with headers `X-Total-Count: 1`,
   `X-Next-Offset: -1`, when `StudiesForUserPage(ctx, "ca3", "follower", 1, 0)`
   runs, then URI is `/studies/user/ca3?limit=1&offset=0&role=follower`,
   `Items` equals the decoded row, `Total == 1`, and `NextOffset == -1`.
3. Given a stub server returning `[]PersonCandidate{{Source:
"faculty_sponsor", Name: "Rosa King", StudyCount: 2}}` with headers
   `X-Total-Count: 2`, `X-Next-Offset: 1`, when
   `ResolvePersonPage(ctx, "rosa", 1, 0)` runs, then URI is
   `/resolve-person/rosa?limit=1&offset=0`, `Items` equals the decoded row,
   `Total == 2`, and `NextOffset == 1`.
4. Given a parity server with people fixtures, when each people page method runs
   with the same args as its body-only counterpart, then `Items` equals the
   body-only method result, `Total` equals the matching server header/count, and
   `NextOffset` equals the returned header value.
5. Given remote `bad_request` or `cache_never_synced` envelopes, when any people
   page method runs, then it returns an empty `Page[T]`, no header metadata is
   fabricated, and the error matches the existing body-only method behavior.

## D: Manifest and Detail Header Envelopes

### D1: Study manifest page envelope

As a Go caller, I want manifest rows plus sizing metadata, so that the existing
manifest body shape is preserved.

**Package:** `mlwh/`
**Files:** `mlwh/types.go`, `mlwh/remote.go`
**Test files:** `mlwh/types_test.go`, `mlwh/manifest_test.go`

```go
func (rc *RemoteClient) StudyManifestPage(
    ctx context.Context,
    studyLimsID, fileType string,
    withIRODS bool,
    limit, offset int,
) (PagedStudyManifest, error)
```

Use Registry method `StudyManifest` and `remoteManifestQuery`. Keep existing
`StudyManifest` body-only method unchanged.

**Acceptance tests:**

1. Given a stub server returning a `StudyManifest` with `IDStudyLims: "S1"` and
   two rows and headers `X-Total-Count: 3`, `X-Next-Offset: 2`, when
   `StudyManifestPage(ctx, "S1", "cram", true, 2, 0)` runs, then URI is
   `/study/S1/manifest?file_type=cram&limit=2&offset=0&with_irods=true`,
   `StudyManifest.Rows` has length 2, `Total == 3`, and `NextOffset == 2`.
2. Given `fileType=""` and `withIRODS=false`, when `StudyManifestPage` runs,
   then `file_type` and `with_irods` are omitted.
3. Given the same server body, when existing `StudyManifest` runs, then it still
   returns only `StudyManifest` and ignores headers.
4. Given a stub server returning `StudyManifest{IDStudyLims: "S1",
Rows: []ManifestRow{{Name: "A"}}}` with no sizing headers, when
   `StudyManifestPage(ctx, "S1", "", false, 1, 0)` runs, then the manifest body
   is decoded, `Total == 0`, `NextOffset == -1`, and error is nil.
5. Given upstream `not_found` or `cache_never_synced` envelopes, when
   `StudyManifestPage` runs, then it returns `PagedStudyManifest{}` and the same
   sentinel behavior as `StudyManifest`.
6. Given a populated `PagedStudyManifest`, when marshaled to JSON, then keys
   are `study_manifest`, `total`, and `next_offset`.

### D2: Study and run detail options with headers

As a Go caller, I want remote detail calls with `limit`, `offset`, and `lean`,
so that nested detail collections can be paged and sized.

**Package:** `mlwh/`
**Files:** `mlwh/types.go`, `mlwh/remote.go`
**Test files:** `mlwh/types_test.go`, `mlwh/remote_test.go`

```go
func (rc *RemoteClient) StudyDetailWithOptions(
    ctx context.Context,
    studyLimsID string,
    opts DetailOptions,
) (PagedStudyDetail, error)

func (rc *RemoteClient) RunDetailWithOptions(
    ctx context.Context,
    idRun string,
    opts DetailOptions,
) (PagedRunDetail, error)
```

Use Registry methods `StudyDetail` and `RunDetail` with
`remoteDetailQuery(opts)`. Existing `StudyDetail(ctx, id)` and
`RunDetail(ctx, id)` stay body-only wrappers with no query params. Do not add a
sample detail options method.

**Acceptance tests:**

1. Given a stub server returning a non-lean `StudyDetail` with headers
   `X-Total-Count: 12`, `X-Next-Offset: 5`, when
   `StudyDetailWithOptions(ctx, "S1", DetailOptions{Limit: 5, Offset: 0})`
   runs, then URI is `/study/S1/detail?limit=5&offset=0`, returned detail
   matches the body, `Total == 12`, and `NextOffset == 5`.
2. Given a stub server returning `StudyDetail{Lean: true, SampleIDs:
[]string{"A"}}` with headers `X-Total-Count: 12`,
   `X-Next-Offset: -1`, when `StudyDetailWithOptions(ctx, "S1",
DetailOptions{Limit: 5, Offset: 0, Lean: true})` runs, then URI is
   `/study/S1/detail?lean=true&limit=5&offset=0`, `StudyDetail.Lean` is true,
   `Total == 12`, and `NextOffset == -1`.
3. Given equivalent run detail stubs, when `RunDetailWithOptions` runs with
   non-lean and lean options, then URIs are `/run/52553/detail?limit=5&offset=0`
   and `/run/52553/detail?lean=true&limit=5&offset=0`, and headers populate
   `PagedRunDetail.Total` / `NextOffset`.
4. Given existing `StudyDetail(ctx, "S1")` and `RunDetail(ctx, "52553")`, when
   they run, then URIs remain `/study/S1/detail` and `/run/52553/detail`, and
   their returned body types are unchanged.
5. Given stub servers returning `StudyDetail{Study: Study{IDStudyLims: "S1"}}`
   and `RunDetail{Run: Run{IDRun: 52553}}` with no sizing headers, when the
   options methods run, then decoded detail bodies match, `Total == 0`,
   `NextOffset == -1`, and errors are nil.
6. Given upstream 400 for invalid detail query values, when options methods run,
   then the returned error follows existing remote error handling and no header
   metadata is fabricated.
7. Given populated `PagedStudyDetail`, `PagedRunDetail`, and `DetailOptions`
   values, when compile-time checks and JSON marshal tests run, then result JSON
   keys are `study_detail` / `run_detail`, `total`, and `next_offset`; options
   are plain Go inputs and need no JSON contract.

## E: Implementation Order

1. Add response-envelope types and JSON casing tests for D1 and D2 in
   `mlwh/types_test.go`.
2. Implement A1: add `CallWithHeaders` and update `Call` to wrap it; test
   dynamic headers and error behavior.
3. Implement B1: add simple missing `Page[T]` wrappers and table-driven remote
   tests for paths, query strings, header values, missing-header fallback, and
   sentinels.
4. Implement C1, C2, and C3: add filtered page wrappers for since/until,
   file_type, and role coverage.
5. Implement D1 and D2: add manifest and detail envelope helpers/methods with
   tests.
6. Update exported comments and run focused tests:

```bash
CGO_ENABLED=1 go test -tags netgo --count 1 ./mlwh -v \
  -run 'Test(RemoteClient|Page|StudyManifest|.*Detail)'
```

Steps 2 and 3 can run in parallel after step 1. Steps 4 and 5 depend on the
shared helper behavior.

## Appendix: Key Decisions

- `Queryer` remains unchanged. The local cache has no HTTP headers, and existing
  body-only methods already satisfy local callers.
- `StudyManifest`, `StudyDetail`, and `RunDetail` do not use `Page[T]` because
  their bodies are envelopes, not bare arrays.
- `DetailOptions` is explicit and remote-only. Zero values mean the literal
  query `limit=0&offset=0`; callers wanting legacy all-rows detail use existing
  `StudyDetail` / `RunDetail`.
- Search page methods also send literal `limit` and `offset`; server defaults
  for omitted search query params are tested through `CallWithHeaders`.
- Header parsing remains forgiving: missing or malformed `X-Total-Count` gives
  `0`; missing or malformed `X-Next-Offset` gives `-1`.
- Tests use GoConvey, `httptest`, and existing parity fixtures. Follow
  `go-implementor`, `go-reviewer`, `go-conventions`, and
  `testing-principles`.
