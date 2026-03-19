# Feature: SAGA API Go Client Library

## Overview

Build a Go library (`saga` package) that provides a complete, idiomatic Go
client for the SAGA API at `https://saga.cellgeni.sanger.ac.uk/api`. The library
should cache results using `github.com/wtsi-hgi/activecache` and provide
easy-to-use methods for all API endpoints, with particular emphasis on
answering common research questions efficiently.

## API Details

Base URL: `https://saga.cellgeni.sanger.ac.uk/api`

Authentication: Pass API token via `X-Api-Key` header.

User-Agent: Always send `wtsi-hgi/wa` as the `User-Agent` header.

The API is a NinjaAPI (Django Ninja) REST API documented at
`/api/docs` with OpenAPI spec at `/api/openapi.json`.

## API Endpoints

### Core Endpoints
- `GET /api/` — Root (health check)
- `GET /api/version` — Returns `{ rev: string|null }`
- `GET /api/auth/me` — Current user info (requires session auth)
- `POST /api/auth/token` — Generate new API key
- `GET /api/users/` — List users

### MLWH (Sequencescape/Multi-LIMS Warehouse) Integration
- `GET /api/integrations/mlwh/studies` — Paginated list of studies
- `GET /api/integrations/mlwh/studies/{study_id}` — Single study detail
- `GET /api/integrations/mlwh/samples` — Paginated list of samples
- `GET /api/integrations/mlwh/faculty_sponsors` — Paginated faculty sponsors
- `GET /api/integrations/mlwh/programmes` — List programmes
- `GET /api/integrations/mlwh/data_release_strategy` — List data release strategies

### iRODS Integration
- `GET /api/integrations/irods/` — iRODS root
- `GET /api/integrations/irods/samples` — Paginated list of iRODS samples
- `GET /api/integrations/irods/samples/{sanger_id}` — Files for a specific sample
- `GET /api/integrations/irods/web-summary/{collection}` — Web summary for collection
- `GET /api/integrations/irods/analysis-types` — List of analysis types

### Projects
- `GET /api/projects/` — List projects
- `POST /api/projects/` — Add project
- `GET /api/projects/{project_id}` — Get project details
- `GET /api/projects/{project_id}/samples` — List project samples
- `POST /api/projects/{project_id}/samples` — Add sample to project
- `POST /api/projects/{project_id}/studies` — Add study to project
- `GET /api/projects/{project_id}/studies` — List project studies
- `DELETE /api/projects/{project_id}/studies/{study_id}` — Remove study
- `DELETE /api/projects/{project_id}/samples/{sample_id}` — Remove sample
- `POST /api/projects/{project_id}/users` — Add user to project
- `GET /api/projects/{project_id}/users` — List project users
- `DELETE /api/projects/{project_id}/users/{user_id}` — Remove user

### Saga Samples & Studies (internal Saga entities)
- `GET /api/samples/` — List saga samples
- `POST /api/samples/` — Create saga sample
- `GET /api/samples/{source}/{source_id}` — Get sample from source
- `GET /api/samples/{source}/study/{source_id}` — Get samples for study
- `GET /api/studies/` — List saga studies

### Other Integrations (STAN, FreezerPro, HuMFre, OMERO, SlideTracker)
- `GET /api/integrations/stan/samples` — STAN samples
- `GET /api/integrations/stan/samples/{sample_id}/slots/{slot_id}` — STAN detail
- `GET /api/integrations/stan/works` — STAN works
- `GET /api/integrations/stan/labware_types` — STAN labware types
- `GET /api/integrations/stan/panels` — STAN panels
- `GET /api/integrations/freezerpro/` — FreezerPro root
- `GET /api/integrations/freezerpro/samples` — FreezerPro samples
- `GET /api/integrations/freezerpro/sample_types` — FreezerPro sample types
- `GET /api/integrations/humfre/` — HuMFre root
- `GET /api/integrations/humfre/forms` — List HuMFre forms
- `GET /api/integrations/humfre/forms/{year}/{number}` — Get specific form
- `GET /api/integrations/omero/` — OMERO root
- `GET /api/integrations/omero/images/{image_id}` — Get image
- `GET /api/integrations/omero/images/{image_id}/thumbnail` — Get thumbnail
- `GET /api/integrations/omero/images/{image_id}/annotations` — Annotations
- `GET /api/integrations/omero/tags` — Tags
- `GET /api/integrations/omero/key-pairs/keys` — Annotation keys
- `GET /api/integrations/omero/groups` — Groups
- `GET /api/integrations/omero/images` — Search images
- `GET /api/integrations/slidetracker/` — SlideTracker root
- `GET /api/integrations/slidetracker/sheets` — Sheets
- `GET /api/integrations/slidetracker/data` — Data

## Response Structures (from real API exploration)

### Paginated Response
Most list endpoints return: `{ items: [...], total: N, offset: N, limit: N }`
Some return `{ items: [...], total: null }` (e.g. MLWH samples).

### MLWH Study (from GET /api/integrations/mlwh/studies/{id})
```json
{
  "id_study_tmp": 3328,
  "id_lims": "SQSCP",
  "id_study_lims": "3361",
  "name": "IHTP_ISC_IBDCA_Edinburgh",
  "faculty_sponsor": "David Adams",
  "state": "active",
  "abstract": "...",
  "abbreviation": "3361STDY",
  "accession_number": "EGAS00001001129",
  "description": "...",
  "data_release_strategy": "managed",
  "study_title": "IBDCA_Edinburgh",
  "data_access_group": "team145 as45 dr9 cancer",
  "hmdmc_number": "14/061",
  "programme": "Other",
  "created": "2014-10-20T08:23:32Z",
  "reference_genome": "Homo_sapiens (1000Genomes_hs37d5)",
  "ethically_approved": true,
  "study_type": "Exome Sequencing",
  "contains_human_dna": true,
  "contaminated_human_dna": false,
  "study_visibility": "Hold",
  "ega_dac_accession_number": "EGAC00001000000",
  "ega_policy_accession_number": "EGAP00001000001",
  "data_release_timing": "standard"
}
```

### MLWH Sample (from GET /api/integrations/mlwh/samples)
```json
{
  "id_study_lims": "3361",
  "id_sample_lims": "2153063",
  "sanger_id": "3361STDY5994718",
  "sample_name": "2L_tumour",
  "taxon_id": 9606,
  "common_name": "Homo Sapien",
  "library_type": "Agilent Pulldown",
  "id_run": 14966,
  "lane": 8,
  "tag_index": 50,
  "irods_path": "/seq/14966/14966_8#50.cram",
  "study_accession_number": "EGAS00001001129",
  "accession_number": "EGAN00001258081"
}
```
Note: The same sample can appear multiple times (different runs/lanes).

### iRODS Sample (from GET /api/integrations/irods/samples)
```json
{
  "id": 263,
  "name": "",
  "source": "IRODS",
  "source_id": "626137912",
  "data": {
    "path": "/seq/illumina/cellranger-arc/...",
    "avu:study": ["HCA Embryo Foetal WSSS Dev RNA Sanger"],
    "avu:id_run": ["40121", "40812"],
    "avu:sample": ["WTSI_wEMB10524782", "WTSI_wEMB10524686"],
    "avu:study_id": ["6568"],
    "avu:sample_id": ["6050954", "6136211"],
    "avu:library_type": ["Chromium single cell 3 prime v3", "Chromium single cell ATAC"],
    "avu:analysis_type": ["cellranger-arc count"],
    "avu:sample_common_name": ["human"],
    "avu:sample_supplier_name": ["C84-WEM-2-FO-1_S2_mA", "C84-WEM-2-FO-1_S2_mG"],
    "avu:study_accession_number": ["EGAS00001005445"],
    "avu:sample_accession_number": ["EGAN00003258234", "EGAN00003265974"]
  },
  "curated": {},
  "parent": null
}
```

### iRODS Sample Detail (from GET /api/integrations/irods/samples/{sanger_id})
```json
{
  "items": [{
    "id": 626137912,
    "collection": "/seq/illumina/cellranger-arc/...",
    "metadata": [
      {"name": "sample_common_name", "value": "human"},
      {"name": "library_type", "value": "Chromium single cell 3 prime v3"},
      {"name": "id_run", "value": "40121"},
      {"name": "sample", "value": "WTSI_wEMB10524782"},
      {"name": "study_id", "value": "6568"},
      {"name": "analysis_type", "value": "cellranger-arc count"}
    ]
  }],
  "total": 1
}
```

### Pagination Query Parameters
All paginated endpoints accept:
- `page` (default: 1)
- `pageSize` (default: 100)
- `sortField` (optional string)
- `sortOrder` (optional, default: "asc")
- `filters` (optional JSON string)
- `export` (optional string)

### Analysis Types
Available values: "cellranger multi", "spaceranger count", "cellranger count",
"cellranger-atac count", "cellranger-arc count".

## Caching

Use `github.com/wtsi-hgi/activecache` for caching API results. The activecache
library provides:
- `New[K, V](duration, fn func(K) (V, error)) *Cache[K, V]` — create cache
- `Get(key K) (V, error)` — get or fetch and cache
- `Remove(key K) bool` — remove from cache
- `Stop()` — stop background refresh goroutine

Cache keys should be the API request (endpoint + parameters). The cache
duration should be configurable by the caller when creating the client.

## Key Use Cases (must be particularly efficient)

1. **"What are all the metadata available for a sample?"** — Combine MLWH sample
   data (taxon, common_name, library_type, etc.) with iRODS AVU metadata for a
   comprehensive view.

2. **"What samples are there in this MLWH study?"** — Get all samples for a
   given study ID by paginating through MLWH samples with `id_study_lims`
   matching the study.

3. **"What files exist in iRODS for this sample?"** — Use the iRODS
   `/samples/{sanger_id}` endpoint to get all iRODS collections/files for a
   sample, filterable by analysis type and other metadata.

4. **"What files exist in iRODS for this study?"** — Find all iRODS samples
   belonging to a study (via `avu:study_id` metadata), filterable by analysis
   type and other metadata. If iRODS metadata is insufficient, cross-reference
   with MLWH sample data.

For use cases 3 and 4, filtering should support:
- File/analysis type (e.g. "cellranger count", "spaceranger count")
- Any iRODS AVU metadata field
- MLWH metadata fields (when cross-referencing)

## Testing

### Unit/Mock Tests
Standard mock HTTP server tests for all client methods.

### Integration Tests
Real API integration tests that:
- Skip when `SAGA_TEST_API_TOKEN` env var is not set
- Use the token from that env var for authentication
- Test against the real API with known sample/study data
- Example test samples: `WTSI_wEMB10524782` (has iRODS data in study 6568)
- Example test study: `3361` (IHTP_ISC_IBDCA_Edinburgh, has known samples like
  `3361STDY5994718`)
- Verify the higher-level convenience methods (metadata for sample,
  samples in study, files for sample, files for study)

## Notes
- The iRODS samples list endpoint has 116 total entries (manageable size).
- MLWH studies total: 8129.
- Some endpoints can be slow (>10s), so caching is important.
- The MLWH samples endpoint returns `total: null` (i.e. unknown total count), so
  pagination may need to continue until an empty page is returned.
- The iRODS list endpoint's `data` field uses `avu:` prefixed keys with array
  values, while the detail endpoint's `metadata` uses `name`/`value` pairs.
- The `filters` query parameter format may cause server errors (500) — the
  library should handle this gracefully and document the filter format if it
  works. Filtering may need client-side implementation for reliability.
- HTTP headers are case-insensitive; OpenAPI spec names it `X-API-Key`.

## Notes
- Phase 1 covers Core, MLWH, iRODS, and Projects endpoints. Phase 2 covers the
  remaining integrations (STAN, FreezerPro, HuMFre, OMERO, SlideTracker).
- The client uses sub-clients per integration (e.g. `client.MLWH().ListStudies()`,
  `client.IRODS().ListSamples()`, `client.Projects().List()`).
- Only GET responses are cached. POST/DELETE mutations are never cached, and
  mutations automatically invalidate related cache entries.
- The client exposes minimal configuration: API key, base URL, and cache
  duration. Sensible defaults are used for HTTP timeouts, retries, TLS, etc.
- Paginated endpoints provide both a per-page method (callers control paging)
  and an all-results convenience method (library auto-paginates).
- The 4 key use cases use a FilterOptions struct for filtering (e.g. by
  analysis type, metadata fields). Return types are domain-specific structs.
- Client initialization uses the functional options pattern:
  `NewClient(apiKey string, opts ...Option)` with sensible defaults for base
  URL, cache duration, etc.
- Phase 1 includes all Core, MLWH, iRODS, and Projects endpoints in detail,
  with deep test coverage on the 4 key use cases and basic tests on other
  endpoints. Phase 2 (other integrations) is mentioned for architecture only.
- FilterOptions is a single generic struct with FieldName/Value maps and a typed
  AnalysisType convenience field, shared across all use cases.
- Auto-paginated convenience methods load all results into a slice. Document the
  memory caveat; callers who need control use per-page methods.
- Cache invalidation uses precise per-mutation rules: e.g. adding a sample to a
  project invalidates that project's sample list cache. Shared cache across
  sub-clients.
- Filtering is always done client-side for reliability, since the server-side
  `filters` parameter may cause 500 errors. Document as a current limitation.
- All code lives in a single `saga` package. Sub-clients are receiver structs
  on the main Client (e.g. `client.MLWH()` returns a sub-struct), not separate
  Go packages.
- API errors use a custom APIError struct with HTTP status code and message,
  plus sentinel errors (ErrUnauthorized, ErrNotFound, etc.).
- HTTP defaults: 30s timeout. Use github.com/wtsi-ssg/wr/retry for configurable
  retries with backoff and max time.
- AnalysisType uses typed string constants for known values (cellranger count,
  spaceranger count, etc.) while still accepting arbitrary strings.
- The 4 key use cases return new merged domain structs combining data from
  multiple API sources (not raw API response wrappers).
- FilterOptions metadata map uses `map[string][]string` to support multiple
  values per field.
- All client methods accept `context.Context` as the first parameter.
- If auto-pagination fails mid-way, return partial results collected so far
  along with the error.
