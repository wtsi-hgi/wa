# Feature: Complete header-aware pagination in the MLWH remote client

Use the spec-writer skill workflow to create `.docs/headerpage/spec.md`, to
specify a proper `wa`-side solution for exposing REST pagination metadata through
the MLWH Go remote client.

## Summary

The MLWH REST server already sets list-sizing response headers on paged endpoints:

- `X-Total-Count`: total number of rows matching the request.
- `X-Next-Offset`: offset for the next page, or `-1` when the returned page is
  the last page.

The `RemoteClient` already has the internal plumbing to read those headers:
`RemoteClient.do` returns the decoded body and response headers,
`remoteCallPage` converts bare JSON-array endpoints into `Page[T]`, and
`Page[T]` already exposes `items`, `total`, and `next_offset`.

However, the exported `RemoteClient` API only exposes page-aware variants for a
subset of paged endpoints. Several paged endpoints and filtered variants still
return only the decoded body, discarding sizing headers. Dynamic callers using
`RemoteClient.Call` also cannot see the response headers. This forces downstream
Go clients to choose between fetching lists without sizing metadata, issuing
separate count calls, or reaching around the typed client.

The feature should complete the `wa` remote-client contract so any Go caller can
use the typed client as the authoritative way to request one bounded page and
receive the server's sizing metadata from the same HTTP response.

## Current Code To Inspect

Use the current repository code as the authority:

- `mlwh/registry.go`: endpoint paths, query params, `Paginated` flags,
  descriptions, and result shapes.
- `mlwh/types.go`: `Page[T]`, `StudyManifest`, `StudyDetail`, `RunDetail`, and
  JSON field tags.
- `mlwh/remote.go`: `RemoteClient`, `remoteCall`, `remoteCallPage`,
  `RemoteClient.Call`, and the currently exported typed methods.
- `mlwh/server.go`: REST handlers, `writeMLWHPaginatedResult`,
  `writeMLWHStudyManifest`, `writeListSizingHeaders`, and detail
  `limit`/`offset`/`lean` handling.
- `mlwh/queryer.go`: local cache `Queryer` surface. Do not expand the local
  `Queryer` interface just to model HTTP headers unless the spec finds a strong
  reason; this feature is primarily about the remote client.
- Existing tests in `mlwh/*_test.go`, especially remote-client and server tests
  around pagination headers.

## Existing Header Semantics

Keep the existing REST semantics:

- Successful paged list endpoints return a bare JSON array body and set
  `X-Total-Count` / `X-Next-Offset`.
- `Page[T].Items` must equal the bare-slice method result for the same endpoint
  and query args.
- `Page[T].Total` must come from `X-Total-Count`.
- `Page[T].NextOffset` must come from `X-Next-Offset`, where `-1` means no next
  page.
- If the server omits or cannot calculate headers, the current
  `remoteHeaderInt` fallback behavior may remain: total defaults to `0` and
  next offset defaults to `-1`.
- Error responses should continue to map through the existing remote error
  handling. Header-aware methods must not mask upstream 400, not-found,
  never-synced, or impaired-upstream errors.

Do not change response bodies merely to expose headers. The REST contract should
remain body-compatible.

## Page-Aware Typed Methods To Add Or Complete

Add exported page-aware `RemoteClient` methods for every bare-array endpoint
that is paged by REST and does not already have a complete page variant.

Keep existing bare-slice methods for backwards compatibility. A page-aware
method should call the same Registry method and use the same path/query
parameters as its bare-slice counterpart, but route through `remoteCallPage`.

The spec should cover, at minimum, these missing page variants:

- `AllStudiesPage(ctx, limit, offset) (Page[Study], error)`.
- `SamplesForRunPage(ctx, idRun, limit, offset) (Page[Sample], error)`.
- `SamplesForLibraryPage(ctx, pipelineIDLims, studyLimsID, limit, offset)
  (Page[Sample], error)`.
- `SamplesForLibraryIDPage(ctx, libraryID, limit, offset)
  (Page[Sample], error)`.
- `SamplesForLibraryLimsIDPage(ctx, idLibraryLims, limit, offset)
  (Page[Sample], error)`.
- `SamplesForLibraryTypePage(ctx, pipelineIDLims, limit, offset)
  (Page[Sample], error)`.
- `LibrariesForStudyPage(ctx, studyLimsID, limit, offset)
  (Page[Library], error)`.
- `RunsForStudyPage(ctx, studyLimsID, limit, offset) (Page[Run], error)`.
- `SamplesWithDataSincePage(ctx, studyLimsID, since, until, limit, offset)
  (Page[SampleWithData], error)`.
- `LanesForSamplePage(ctx, sangerName, limit, offset) (Page[Lane], error)`.
- `IRODSPathsForSamplePage(ctx, sangerName, limit, offset)
  (Page[IRODSPath], error)`.
- `IRODSPathsForSampleByFileTypePage(ctx, sangerName, fileType, limit, offset)
  (Page[IRODSPath], error)`.
- `IRODSPathsForStudyByFileTypePage(ctx, studyLimsID, fileType, limit, offset)
  (Page[IRODSPath], error)`.
- A filter-aware run iRODS page method. Prefer adding
  `IRODSPathsForRunByFileTypePage(ctx, idRun, fileType, limit, offset)
  (Page[IRODSPath], error)` and keeping the existing
  `IRODSPathsForRunPage(ctx, idRun, limit, offset)` as the unfiltered
  convenience wrapper.
- `StudiesForFacultySponsorPage`, `StudiesForUserPage`, and
  `ResolvePersonPage` already exist; keep them and ensure tests cover their
  header behavior, including filters such as `role`.
- `SearchStudiesPage(ctx, term, limit, offset) (Page[Study], error)`.
- `SearchSamplesPage(ctx, term, limit, offset) (Page[Sample], error)`.

Existing page methods such as `SamplesForStudyPage`, `SamplesWithDataPage`,
`SamplesWithoutDataPage`, `IRODSPathsForStudyPage`,
`IRODSPathsForRunPage`, `StudiesForFacultySponsorPage`,
`StudiesForUserPage`, and `ResolvePersonPage` should continue working.

## Filtered Variant Requirements

Header-aware variants must preserve the exact query semantics of the
corresponding bare methods:

- `SamplesWithDataSincePage` must send `since` and `until` exactly as
  `SamplesWithDataSince` does. The window remains half-open `[since, until)`.
  Invalid timestamps and `until` without `since` remain upstream 400 errors.
- iRODS `ByFileTypePage` methods must send `file_type` exactly as the
  bare `ByFileType` and count methods do. Empty file type means no filter.
  Invalid values remain upstream 400 errors.
- `StudiesForUserPage` must send the optional `role` override exactly as the
  bare method does.
- Search page methods must use the search endpoint pagination defaults and
  maximums enforced by the server.

The page result's `total` must reflect the same filtered request as the returned
items. For example, `IRODSPathsForStudyByFileTypePage(..., "cram", ...)` must
report the total number of matching `.cram` rows, not the unfiltered total.

## Study Manifest Envelope

`StudyManifest` is a paged endpoint, but its body is an envelope object rather
than a bare array. Do not force it through `Page[T]`.

Add a dedicated exported remote-client result type and method for the paged
manifest, for example:

```go
type PagedStudyManifest struct {
    StudyManifest StudyManifest `json:"study_manifest"`
    Total         int           `json:"total"`
    NextOffset    int           `json:"next_offset"`
}

func (rc *RemoteClient) StudyManifestPage(
    ctx context.Context,
    studyLimsID, fileType string,
    withIRODS bool,
    limit, offset int,
) (PagedStudyManifest, error)
```

The exact type name can be refined in the spec, but the public contract must
carry:

- the existing `StudyManifest` envelope exactly as decoded from the response
  body;
- `total` from `X-Total-Count`, equal to `/study/:id/manifest/count`;
- `next_offset` from `X-Next-Offset`, computed over `StudyManifest.Rows`;
- the same `with_irods`, `file_type`, `limit`, and `offset` query behavior as
  `StudyManifest`.

Keep the existing `StudyManifest` method for backwards compatibility.

## Detail Endpoint Options And Headers

The REST handlers for `StudyDetail` and `RunDetail` support `limit`, `offset`,
and `lean`, and they set the same sizing headers for the nested collection.
The exported `RemoteClient.StudyDetail` and `RemoteClient.RunDetail` methods
currently expose only the default full, all-rows, non-lean shape.

Add an exported remote-client way to call the detail endpoints with options and
read their sizing headers. The spec should decide the final API shape, but it
must satisfy these requirements:

- callers can request `StudyDetail` with `limit`, `offset`, and `lean`;
- callers can request `RunDetail` with `limit`, `offset`, and `lean`;
- the returned value includes the decoded detail object plus `total` and
  `next_offset` from the response headers;
- existing `StudyDetail(ctx, id)` and `RunDetail(ctx, id)` methods remain
  backwards-compatible default wrappers;
- no `lean` option is added to `SampleDetail`, because there is no landed
  `lean` query param for `/sample/:id/detail`;
- invalid query values continue to surface as upstream 400 errors.

## Dynamic Callers

`RemoteClient.Call` currently returns only the decoded body and discards headers.
Add an exported dynamic-call API that preserves response headers for callers that
dispatch through Registry method names.

Prefer a simple shape such as:

```go
func (rc *RemoteClient) CallWithHeaders(
    ctx context.Context,
    method string,
    pathParams []string,
    query url.Values,
) (any, http.Header, error)
```

`CallWithHeaders` should use the same request and decode path as `Call`.
Existing `Call` should remain as a backwards-compatible wrapper that discards
headers.

## Non-Goals

- Do not change the MLWH REST endpoint paths, query params, or response bodies
  just to expose pagination headers to Go callers.
- Do not implement client-side count calls to synthesize `total`; the purpose of
  this feature is to expose the server's existing headers from the same list
  response.
- Do not add page methods for non-paged lookup, resolve, aggregate, freshness,
  count, or expansion endpoints unless the endpoint actually sets list-sizing
  headers.
- Do not add `lean` support to `/sample/:id/detail`.
- Do not remove or change existing bare-slice/bare-detail methods.

## Testing Requirements

Use hermetic tests only.

The spec should require tests that prove:

- every new page-aware method sends the expected path and query string;
- every new page-aware method returns the decoded items/body and the
  `X-Total-Count` / `X-Next-Offset` header values;
- filtered page methods preserve their filters in the request and report the
  filtered totals returned by the server;
- `StudyManifestPage` preserves the manifest envelope and attaches sizing
  metadata from headers;
- study/run detail option methods send `limit`, `offset`, and `lean` correctly
  and expose header metadata;
- `CallWithHeaders` returns the same decoded result as `Call` plus headers;
- upstream error responses are still mapped to the existing sentinels;
- existing bare methods continue to pass their current tests.

## Documentation Requirements

Update public Go documentation/comments so users can discover when to use:

- bare methods for backwards-compatible body-only calls;
- `Page[T]` methods for one-page list calls with sizing metadata;
- the manifest page result for `StudyManifest.Rows`;
- detail option/header methods for paged or lean study/run detail;
- `CallWithHeaders` for Registry-driven dynamic clients.

If generated API reference or human docs include remote-client guidance, update
them accordingly. The HTTP API documentation should continue to describe the
existing REST headers and body shapes.
