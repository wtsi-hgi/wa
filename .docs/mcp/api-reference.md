# wa mlwh API endpoint reference

This catalogue lists every endpoint of the cache-backed, read-only `wa mlwh
serve` REST API. It is **generated** from the same enriched `Registry`
metadata in `mlwh/registry.go` that produces the machine-readable OpenAPI
document at `GET /openapi.json`, so it cannot drift from the served API. Do
not edit it by hand; refresh it with `go test ./mlwh -run
TestWriteEndpointReference` after changing the `Registry`.

All endpoints are HTTP `GET`, return JSON, and are unauthenticated by
default. Path parameters are shown as `:name`; paginated list and search
endpoints accept the `limit` and `offset` query parameters described per
entry. On failure every endpoint returns the shared `{code, message}` error
envelope (see the OpenAPI document for the full set of error codes and
statuses). Field-level descriptions of each response type are in the OpenAPI
document; the domain entities are defined in `glossary.md`.

## Endpoints

### `GET /classify/:id`

Classify an identifier

Detects the kind of the given raw identifier and returns its canonical form with any directly matching study, sample, run, or library.

- Path parameters: `id`
- Query parameters: none
- Response: `Match`

### `GET /resolve/sample/:id`

Resolve a sample identifier

Resolves any supported sample identifier (UUID, LIMS id, Sanger sample name or id, supplier name, accession, or donor id) to its canonical sample Match.

- Path parameters: `id`
- Query parameters: none
- Response: `Match`

### `GET /resolve/sample-name/:id`

Resolve a Sanger sample name

Resolves a Sanger sample name to its canonical sample Match, disambiguating it from other sample identifier forms.

- Path parameters: `id`
- Query parameters: none
- Response: `Match`

### `GET /resolve/study/:id`

Resolve a study identifier

Resolves any supported study identifier (UUID, LIMS id, accession, or name) to its canonical study Match.

- Path parameters: `id`
- Query parameters: none
- Response: `Match`

### `GET /resolve/run/:id`

Resolve a run identifier

Resolves a sequencing run identifier to its canonical run Match.

- Path parameters: `id`
- Query parameters: none
- Response: `Match`

### `GET /resolve/library/:id`

Resolve a library by type

Resolves a library identified by its library type to its canonical library Match.

- Path parameters: `id`
- Query parameters: none
- Response: `Match`

### `GET /resolve/library-identifier/:id`

Resolve a library identifier

Resolves a library identifier (library id or LIMS library id) to its canonical library Match.

- Path parameters: `id`
- Query parameters: none
- Response: `Match`

### `GET /studies`

List all studies

Lists every study mirrored in the cache, ordered by LIMS study id. Defaults to returning all studies; use limit/offset to page.

- Path parameters: none
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]Study`

### `GET /study/:id/samples`

List samples in a study

Lists the distinct samples linked to the given study via its libraries. Defaults to returning all samples; use limit/offset to page.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]Sample`

### `GET /run/:id/samples`

List samples on a run

Lists the samples sequenced on the given run. Defaults to returning all samples; use limit/offset to page.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]Sample`

### `GET /library/:pipeline/study/:study/samples`

List samples in a library

Lists the samples in the library identified by its pipeline LIMS id within the given study. Defaults to returning all samples; use limit/offset to page.

- Path parameters: `pipeline`, `study`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]Sample`

### `GET /library-id/:id/samples`

List samples by library id

Lists the samples in the library with the given library id. Defaults to returning all samples; use limit/offset to page.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]Sample`

### `GET /library-lims-id/:id/samples`

List samples by LIMS library id

Lists the samples in the library with the given LIMS library id. Defaults to returning all samples; use limit/offset to page.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]Sample`

### `GET /library-type/:id/samples`

List samples by library type

Lists the samples in libraries of the given library type. Defaults to returning all samples; use limit/offset to page.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]Sample`

### `GET /study/:id/libraries`

List libraries in a study

Lists the libraries belonging to the given study. Defaults to returning all libraries; use limit/offset to page.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]Library`

### `GET /study/:id/runs`

List runs for a study

Lists the sequencing runs associated with the given study. Defaults to returning all runs; use limit/offset to page.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]Run`

### `GET /sample/:id/lanes`

List lanes for a sample

Lists the run/lane/tag combinations on which the given sample (by Sanger sample name) was sequenced. Defaults to returning all lanes; use limit/offset to page.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]Lane`

### `GET /sample/:id/irods`

List iRODS paths for a sample

Lists the iRODS data-object paths exported for the given sample (by Sanger sample name). Defaults to returning all paths; use limit/offset to page.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]IRODSPath`

### `GET /study/:id/irods`

List iRODS paths for a study

Lists the iRODS data-object paths exported for the given study. Defaults to returning all paths; use limit/offset to page.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]IRODSPath`

### `GET /sample/:id/studies`

List studies for a sample

Lists the studies the given sample (by Sanger sample name) belongs to.

- Path parameters: `id`
- Query parameters: none
- Response: `[]Study`

### `GET /find/sample/sanger-id/:id`

Find samples by Sanger sample id

Returns the samples whose Sanger sample id exactly matches the given value.

- Path parameters: `id`
- Query parameters: none
- Response: `[]Sample`

### `GET /find/sample/lims-id/:id`

Find samples by LIMS sample id

Returns the samples whose LIMS sample id exactly matches the given value.

- Path parameters: `id`
- Query parameters: none
- Response: `[]Sample`

### `GET /find/sample/accession/:id`

Find samples by accession number

Returns the samples whose accession number exactly matches the given value.

- Path parameters: `id`
- Query parameters: none
- Response: `[]Sample`

### `GET /find/sample/supplier-name/:id`

Find samples by supplier name

Returns the samples whose supplier name exactly matches the given value.

- Path parameters: `id`
- Query parameters: none
- Response: `[]Sample`

### `GET /find/sample/library-type/:id`

Find samples by library type

Returns the samples whose library type exactly matches the given value.

- Path parameters: `id`
- Query parameters: none
- Response: `[]Sample`

### `GET /expand/:kind/:id`

Expand an identifier to related identifiers

Expands the given identifier of the named kind into the set of related canonical identifiers (kind and canonical value) reachable from it.

- Path parameters: `kind`, `id`
- Query parameters: none
- Response: `[]TaggedID`

### `GET /expand-search/:kind/:id`

Expand an identifier to result-search values

Expands the given identifier into the sample, run, and lane values used to search downstream results.

- Path parameters: `kind`, `id`
- Query parameters: none
- Response: `SearchValues`

### `GET /expand-sample-search/:kind/:id`

Expand an identifier to sample search values

Expands the given identifier into the list of sample values used to search downstream results.

- Path parameters: `kind`, `id`
- Query parameters: none
- Response: `[]string`

### `GET /enrich/:id`

Enrich an identifier

Classifies the given identifier and walks the MLWH graph to assemble its related studies, samples, and libraries, reporting any missing or truncated hops.

- Path parameters: `id`
- Query parameters: none
- Response: `EnrichmentResult`

### `GET /sample/:id/detail`

Get sample detail

Returns the given sample (by Sanger sample name) with its study, lanes, libraries, and iRODS paths.

- Path parameters: `id`
- Query parameters: none
- Response: `SampleDetail`

### `GET /study/:id/detail`

Get study detail

Returns the given study with the detail of each of its libraries and their samples.

- Path parameters: `id`
- Query parameters: none
- Response: `StudyDetail`

### `GET /run/:id/detail`

Get run detail

Returns the given run with its related samples, studies, and per-study detail.

- Path parameters: `id`
- Query parameters: none
- Response: `RunDetail`

### `GET /library/:pipeline/study/:study/detail`

Get library detail

Returns the library identified by its pipeline LIMS id within the given study, together with the samples it covers.

- Path parameters: `pipeline`, `study`
- Query parameters: none
- Response: `LibraryDetail`

### `GET /search/study/:term`

Search studies by substring

Returns studies whose name, title, programme, or faculty sponsor contains the term (case-insensitive substring, minimum 3 characters). Defaults to a page of 100, maximum 1000.

- Path parameters: `term`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to 100, maximum 1000 (a larger limit is rejected); `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]Study`

### `GET /search/sample/:term`

Search samples by word prefix

Returns samples having a word in name, supplier name, common name, or donor id that starts with the term (case-insensitive word-prefix match, minimum 3 characters), backed by a word-token prefix index for the large sample table. So "musculus" and "mus" both match "Mus Musculus"; a substring inside a word does not. Defaults to a page of 100, maximum 1000.

- Path parameters: `term`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to 100, maximum 1000 (a larger limit is rejected); `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]Sample`

### `GET /search/study/:term/count`

Count studies matching a substring

Returns the number of studies matching the same substring search as /search/study/:term, without transferring rows.

- Path parameters: `term`
- Query parameters: none
- Response: `Count`

### `GET /search/sample/:term/count`

Count samples matching a word prefix

Returns the number of samples matching the same word-prefix search as /search/sample/:term, without transferring rows. The count is exact up to a bound and reports that bound as a floor for very common terms.

- Path parameters: `term`
- Query parameters: none
- Response: `Count`

### `GET /studies/count`

Count all studies

Returns the total number of studies mirrored in the cache, the count counterpart of /studies.

- Path parameters: none
- Query parameters: none
- Response: `Count`

### `GET /study/:id/samples/count`

Count samples in a study

Returns the number of distinct samples linked to the given study, the count counterpart of /study/:id/samples.

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /freshness`

Report cache freshness

Reports, per mirrored sync table, its high-water mark and last sync run time (UTC RFC3339) and whether it has ever synced. Succeeds even on a never-synced cache so callers can degrade gracefully.

- Path parameters: none
- Query parameters: none
- Response: `Freshness`
