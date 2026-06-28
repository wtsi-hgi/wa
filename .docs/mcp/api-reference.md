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

### `GET /study/:id/overview`

Get a study's sequencing overview

Returns one fixed-size aggregate answering "what is in this study, how much sequencing data, and was anything added recently", so callers avoid the large per-sample fan-outs. samples_total is the distinct samples linked via library_samples. A sample has sequencing data available for this study iff it has at least one row in the iRODS locations mirror scoped by id_study_lims = the study (real data objects in iRODS); scoping is by the study the data is under, NOT data the sample has anywhere. samples_with_data, samples_sequenced_no_data and the implied registered bucket form the distinct-sample partition by most-advanced phase (precedence with_data > sequenced_no_data > registered), so each sample counts once and samples_without_data = samples_total - samples_with_data. samples_sequenced_no_data is the distinct samples with product-metrics in this study (scoped by the product-metrics id_study_lims) but no study-scoped iRODS rows; registered (= samples_total - samples_with_data - samples_sequenced_no_data) is the linked samples with no product-metrics, including ONT. data_objects is the study-scoped iRODS data-object count; runs, libraries and the sorted library_types come from the study's product-metrics and library tables. sequencing_date_range and newest_data_added are the earliest/latest iRODS creation timestamp (the created column, NEVER last_updated or last_run); added_last_7_days counts the distinct samples whose study-scoped iRODS data was added in the half-open window [now-7d, now) on that created column (created >= now-7d AND created < now). cache_synced_at is the oldest last_run across the feeding tables (study, sample, the product-metrics mirrors and the iRODS locations mirror), distinct from any data timestamp; every figure is read from the cache mirrors, so the overview is complete only up to that sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `StudyOverview`

### `GET /run/:id/overview`

Get a run's sequencing overview

Returns one fixed-size aggregate answering "what is on this run and how much sequencing data", so callers need neither /run/:id/detail nor per-sample calls. :id is the Illumina NPG run id (the existing run/ResolveRun identifier space; no new resolver): a non-Illumina or otherwise invalid run yields the existing not-found / unsupported-identifier error, and a numeric run absent from the synced cache yields not_found. samples is the distinct samples on the run and studies the distinct studies on the run, both taken from the run's iseq_product_metrics rows (the same source as /run/:id/samples). data_objects is the iRODS data objects for the run: the run's iseq_product_metrics rows joined to the iRODS locations mirror by the shared id_iseq_product (the run's real data files in iRODS). sequencing_date_range is the earliest/latest iRODS creation timestamp for those data objects (the created column, NEVER last_updated or last_run); it is omitted when the run has no iRODS rows. This is a separate small aggregate, NOT folded into /run/:id/detail. cache_synced_at is the oldest last_run across the feeding tables (the iseq product-metrics mirror and the iRODS locations mirror), distinct from any data timestamp; every figure is read from the cache mirrors, so the overview is complete only up to that sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `RunOverview`

### `GET /study/:id/samples-with-data`

List a study's samples that have sequencing data

Lists the distinct samples linked to the given study (via library_samples) that have sequencing data available for this study, each qualified by the platforms it has products on. A sample has data for this study iff it has at least one row in the iRODS locations mirror scoped by id_study_lims = the study (real data objects in iRODS), so scoping is by the study the data is under, NOT data the sample has anywhere. Together with /study/:id/samples-without-data this partitions the study's linked samples (with_data + without_data = samples_total). platforms lists the canonical platform names the sample has products on in this study (e.g. Illumina, PacBio, Elembio, Ultimagen); it is ["ONT"] for an ONT sample (Oxford Nanopore is not tracked for availability/QC, only identity and study) and empty for a registered-only sample with no products. The optional since and until RFC3339 query params restrict the list to samples whose study-scoped data was ADDED to iRODS in the half-open window [since, until): the filter is on the iRODS creation timestamp (the created column), NEVER on last_updated or last_run (last_updated conflates newly-added with later-modified rows, and last_run is only when wa synced), so it answers "added since X"; since is inclusive and until is exclusive (created >= since AND created < until), comparison is in normalised UTC, and until is optional (the window is open-ended when omitted). The in-window list and /study/:id/samples-with-data/count with the same since/until stay the exact count<->list cross-check. Without since the list is all-time. A malformed since or until is rejected with a 400 bad_request before the query runs. Membership is read from the cache mirrors, so results are complete only up to the feeding tables' last sync (see /freshness). Defaults to returning all samples; use limit/offset to page.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0; `since` (string): RFC3339 timestamp; when set, counts only samples whose study-scoped data was added to iRODS at or after this instant (created >= since, inclusive), filtering on the iRODS creation timestamp and never on last_updated/last_run; omit for an all-time count; `until` (string): RFC3339 timestamp; when set with since, the upper bound of the half-open window (created < until, exclusive); optional and open-ended when omitted
- Response: `[]SampleWithData`

### `GET /study/:id/samples-without-data`

List a study's samples that lack sequencing data

Lists the distinct samples linked to the given study (via library_samples) that have NO sequencing data available for this study: the complement of /study/:id/samples-with-data, so with_data + without_data = samples_total. A sample has data for this study iff it has at least one row in the iRODS locations mirror scoped by id_study_lims = the study; scoping is by the study the data is under, NOT data the sample has anywhere, so a sample with data only under another study appears here. This list includes samples sequenced in this study but not yet in iRODS, registered-only samples, and ONT samples. platforms qualifies each negative: the canonical platform names the sample has products on in this study (e.g. Illumina, PacBio, Elembio, Ultimagen), ["ONT"] for an ONT sample (Oxford Nanopore is not tracked for availability/QC, only identity and study), and empty for a registered-only sample with no products. Membership is read from the cache mirrors, so results are complete only up to the feeding tables' last sync (see /freshness). Defaults to returning all samples; use limit/offset to page.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0
- Response: `[]SampleWithData`

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

Returns the given study with the detail of each of its libraries and their samples. The response is de-duplicated to stay bounded: each distinct study and library is carried once in the study_lookup / library_lookup tables (keyed by id) and the nested sample rows under library_details reference them by id rather than re-embedding the same study and library objects under every sample. The optional limit/offset query params paginate the nested sample collection (defaulting to every sample), and X-Total-Count reports the full nested sample count while X-Next-Offset gives the offset of the next page (offset+returned, or -1 on the last page), exactly as the paginated list endpoints do. The optional lean query param (a boolean) drops the heavy library_details and lookup tables and returns only the top-level study plus the flat sample_ids and library_ids lists, so the response is strictly smaller.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0; `lean` (boolean): when true, drops the heavy nested objects (library_details / samples / studies / study_details and the lookup tables) and returns only the top-level entity plus flat id lists, so the response is strictly smaller; defaults to false
- Response: `StudyDetail`

### `GET /run/:id/detail`

Get run detail

Returns the given run with its related samples, studies, and per-study detail. The response is de-duplicated to stay bounded: each distinct study and library is carried once in the study_lookup / library_lookup tables (keyed by id) and the nested sample rows (both samples and study_details) reference them by id rather than re-embedding the same study and library objects under every sample. The optional limit/offset query params paginate the nested sample collection (defaulting to every sample, with studies and study_details rebuilt from the page), and X-Total-Count reports the full nested sample count while X-Next-Offset gives the offset of the next page (offset+returned, or -1 on the last page), exactly as the paginated list endpoints do. The optional lean query param (a boolean) drops the heavy samples, studies, study_details and lookup tables and returns only the run plus the flat sample_ids and study_ids lists, so the response is strictly smaller.

- Path parameters: `id`
- Query parameters: `limit` (integer): maximum number of rows to return; defaults to a fetch-all page that returns every matching row; `offset` (integer): number of leading rows to skip before returning results; defaults to 0; `lean` (boolean): when true, drops the heavy nested objects (library_details / samples / studies / study_details and the lookup tables) and returns only the top-level entity plus flat id lists, so the response is strictly smaller; defaults to false
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

### `GET /study/:id/samples-with-data/count`

Count a study's samples that have sequencing data

Returns the number of distinct samples linked to the given study (via library_samples) that have sequencing data available for this study, the count counterpart of /study/:id/samples-with-data (count == the length of that list when all rows are fetched). A sample has data for this study iff it has at least one row in the iRODS locations mirror scoped by id_study_lims = the study (real data objects in iRODS); scoping is by the study the data is under, NOT data the sample has anywhere. The figure counts distinct samples, never iRODS data objects, so a sample with many study-scoped iRODS rows is counted once. The optional since and until RFC3339 query params restrict the count to samples whose study-scoped data was ADDED to iRODS in the half-open window [since, until): the filter is on the iRODS creation timestamp (the created column), NEVER on last_updated or last_run (last_updated conflates newly-added with later-modified rows, and last_run is only when wa synced), so it answers "added since X"; since is inclusive and until is exclusive (created >= since AND created < until), comparison is in normalised UTC, and until is optional (the window is open-ended when omitted). Without since the count is all-time. A malformed since or until is rejected with a 400 bad_request before the query runs. Membership is read from the cache mirrors, so the count is complete only up to the feeding tables' last sync (see /freshness).

- Path parameters: `id`
- Query parameters: `since` (string): RFC3339 timestamp; when set, counts only samples whose study-scoped data was added to iRODS at or after this instant (created >= since, inclusive), filtering on the iRODS creation timestamp and never on last_updated/last_run; omit for an all-time count; `until` (string): RFC3339 timestamp; when set with since, the upper bound of the half-open window (created < until, exclusive); optional and open-ended when omitted
- Response: `Count`

### `GET /run/:id/samples/count`

Count samples on a run

Returns the number of distinct samples sequenced on the given run, the count counterpart of /run/:id/samples (count == the length of that list when all rows are fetched), via the same iseq_product_metrics_mirror join on id_run with no LIMIT. :id is the run id; a non-numeric id is rejected as an unsupported identifier, and a run absent from the synced cache yields not_found. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /library/:pipeline/study/:study/samples/count`

Count samples in a library

Returns the number of distinct samples in the library identified by its pipeline LIMS id within the given study, the count counterpart of /library/:pipeline/study/:study/samples (count == the length of that list when all rows are fetched), via the same library_samples join filtered by pipeline_id_lims and id_study_lims with no LIMIT. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).

- Path parameters: `pipeline`, `study`
- Query parameters: none
- Response: `Count`

### `GET /library-id/:id/samples/count`

Count samples by library id

Returns the number of distinct samples in the library with the given library id, the count counterpart of /library-id/:id/samples (count == the length of that list when all rows are fetched), via the same library_samples filter on library_id with no LIMIT. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /library-lims-id/:id/samples/count`

Count samples by LIMS library id

Returns the number of distinct samples in the library with the given LIMS library id, the count counterpart of /library-lims-id/:id/samples (count == the length of that list when all rows are fetched), via the same library_samples filter on id_library_lims with no LIMIT. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /library-type/:id/samples/count`

Count samples by library type

Returns the number of distinct samples in libraries of the given library type, the count counterpart of /library-type/:id/samples (count == the length of that list when all rows are fetched), via the same library_samples filter on pipeline_id_lims with no LIMIT. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /study/:id/runs/count`

Count runs for a study

Returns the number of distinct sequencing runs associated with the given study, the count counterpart of /study/:id/runs (count == the length of that list when all rows are fetched), via the same iseq_product_metrics_mirror filter on id_study_lims with no LIMIT. An unknown study yields not_found. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /study/:id/libraries/count`

Count libraries in a study

Returns the number of distinct libraries belonging to the given study, the count counterpart of /study/:id/libraries (count == the length of that list when all rows are fetched), counting the distinct (pipeline_id_lims, library_id, id_library_lims) library_samples groupings the list returns with no LIMIT. An unknown study yields not_found. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /sample/:id/lanes/count`

Count lanes for a sample

Returns the number of distinct run/lane/tag combinations on which the given sample (by Sanger sample name) was sequenced, the count counterpart of /sample/:id/lanes (count == the length of that list when all rows are fetched), counting the distinct (id_run, position, tag_index) rows the list returns with no LIMIT. An unknown sample yields not_found. The count is read from the cache mirror, so it is complete only up to that table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /sample/:id/irods/count`

Count iRODS paths for a sample

Returns the number of distinct iRODS data objects exported for the given sample (by Sanger sample name), the count counterpart of /sample/:id/irods (count == the length of that list when all rows are fetched), counting the distinct iRODS data objects the list returns with no LIMIT. An unknown sample yields not_found. The count is read from the iRODS locations mirror, so it is complete only up to that table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /study/:id/irods/count`

Count iRODS paths for a study

Returns the number of distinct iRODS data objects exported for the given study, the count counterpart of /study/:id/irods (count == the length of that list when all rows are fetched), counting the distinct iRODS rows the list returns (scoped by id_study_lims) with no LIMIT. An unknown study yields not_found. The count is read from the iRODS locations mirror, so it is complete only up to that table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /find/sample/sanger-id/:id/count`

Count samples by Sanger sample id

Returns the number of samples whose Sanger sample id exactly matches the given value, the count counterpart of /find/sample/sanger-id/:id: it equals that list's length for a unique match and reports the true multiplicity when the list would instead report an ambiguous match. The count is read from the cache mirror, so it is complete only up to the sample table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /find/sample/lims-id/:id/count`

Count samples by LIMS sample id

Returns the number of samples whose LIMS sample id exactly matches the given value, the count counterpart of /find/sample/lims-id/:id: it equals that list's length for a unique match and reports the true multiplicity when the list would instead report an ambiguous match. The count is read from the cache mirror, so it is complete only up to the sample table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /find/sample/accession/:id/count`

Count samples by accession number

Returns the number of samples whose accession number exactly matches the given value, the count counterpart of /find/sample/accession/:id: it equals that list's length for a unique match and reports the true multiplicity when the list would instead report an ambiguous match. The count is read from the cache mirror, so it is complete only up to the sample table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /find/sample/supplier-name/:id/count`

Count samples by supplier name

Returns the number of samples whose supplier name exactly matches the given value, the count counterpart of /find/sample/supplier-name/:id: it equals that list's length for a unique match and reports the true multiplicity when the list would instead report an ambiguous match. The count is read from the cache mirror, so it is complete only up to the sample table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /find/sample/library-type/:id/count`

Count samples by library type

Returns the number of distinct samples whose library type exactly matches the given value, the count counterpart of /find/sample/library-type/:id: it equals that list's length for a unique match and reports the true multiplicity when the list would instead report an ambiguous match. The count is read from the cache mirror, so it is complete only up to the sample table's last sync (see /freshness).

- Path parameters: `id`
- Query parameters: none
- Response: `Count`

### `GET /freshness`

Report cache freshness

Reports, per mirrored sync table, its high-water mark and last sync run time (UTC RFC3339) and whether it has ever synced. Succeeds even on a never-synced cache so callers can degrade gracefully.

- Path parameters: none
- Query parameters: none
- Response: `Freshness`
