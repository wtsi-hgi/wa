# Results Web UI Specification

## Overview

Web UI for browsing pipeline result sets, backed by the existing
`wa results serve` Go REST API. The Go backend gains six
capabilities: file content serving, aggregate statistics, metadata
key enumeration, multi-value OR search, study-ID search shortcut,
and two new seqmeta listing endpoints. A Next.js 16 frontend
(part 2 of this spec) will consume these endpoints.

Key behaviours:

- File content endpoint streams registered files with Content-Type
  detection, gzip decompression for preview, configurable size cap,
  and `?download=true` bypass.
- Stats endpoint returns recent result set summaries, daily counts,
  and per-pipeline counts without fetching all result sets.
- Meta-keys endpoint returns distinct metadata keys for dynamic
  filter UI population.
- Multi-value OR search via repeated query params within a key,
  ANDed across keys.
- Study-ID search shortcut resolves study samples via seqmeta, then
  searches by `seqmeta_sampleid` metadata.
- Seqmeta server gains `GET /studies` and
  `GET /study/{id}/samples` listing endpoints.

## Architecture

### results/ package additions

**New file:** `results/server_file.go` - file content handler.

**Modified files:**

- `results/server.go` - register new routes, update
  `searchParamsFromRequest` for multi-value OR, add stats and
  meta-keys handlers, add study-ID search helper.
- `results/store.go` - add `Stats`, `MetaKeys`, and
  `SearchMulti` store methods.
- `results/types.go` - add `MultiSearchParams`, `StatsResult`,
  `DailyCounts`, `PipelineCounts` types; add new sentinel
  error `ErrFileTooLarge`; add `ErrFileGone`.

**New route table additions (chi):**

| Method | Path               | Action                 |
| ------ | ------------------ | ---------------------- |
| GET    | /results/{id}/file | Serve file content     |
| GET    | /results/stats     | Aggregate statistics   |
| GET    | /results/meta-keys | Distinct metadata keys |

**Existing route changes:**

| Method | Path     | Change                             |
| ------ | -------- | ---------------------------------- |
| GET    | /results | Multi-value OR + study_id shortcut |

### seqmeta/ package additions

**Modified file:** `seqmeta/server.go` - register 2 new routes.

**New route table additions (chi):**

| Method | Path                | Action                 |
| ------ | ------------------- | ---------------------- |
| GET    | /studies            | List all studies       |
| GET    | /study/{id}/samples | List samples for study |

### New types

```go
// MultiSearchParams supports multi-value OR within each field.
type MultiSearchParams struct {
    Requester          []string
    Operator           []string
    PipelineName       []string
    PipelineVersion    []string
    PipelineIdentifier []string
    RunKey             []string
    OutputDirPrefix    []string
    Meta               map[string][]string
}

// StatsResult is returned by GET /results/stats.
type StatsResult struct {
    Total     int             `json:"total"`
    Recent    []ResultSet     `json:"recent"`
    Daily     []DailyCount    `json:"daily"`
    Pipelines []PipelineCount `json:"pipelines"`
}

// DailyCount is registrations per day.
type DailyCount struct {
    Date  string `json:"date"`
    Count int    `json:"count"`
}

// PipelineCount is result sets per pipeline.
type PipelineCount struct {
    PipelineName string `json:"pipeline_name"`
    Count        int    `json:"count"`
}

// SearchResult wraps a ResultSet with optional match
// metadata. Returned by GET /results when study_id is used.
type SearchResult struct {
    ResultSet      ResultSet `json:"result_set"`
    MatchedSamples []string  `json:"matched_samples,omitempty"`
}
```

### Sentinel errors (additions to results/)

```go
var (
    ErrFileGone     = errors.New("results: file not found on disk")
    ErrFileTooLarge = errors.New("results: file exceeds preview limit")
)
```

### Configuration

The file content endpoint uses a configurable max preview size.
`NewServer` accepts an options struct or the max size as a
parameter (see story A1). Default: 10 MB. `?download=true`
bypasses the limit.

---

## A. File Content Endpoint

### A1: Serve registered file content

As a consumer, I want `GET /results/{id}/file?path=<abs-path>`
to stream the bytes of a registered file, so that the frontend
can preview or download it.

The handler:

1. Looks up the result set by `{id}` (404 if missing).
2. Calls `store.GetFiles` and checks the requested `path` is
   in the registered file list (403 if not registered).
3. Calls `os.Stat` on the path; if missing returns 410 Gone
   with JSON `{"error":"file not found on disk"}`.
4. Detects Content-Type via `mime.TypeByExtension` on the file
   extension. Falls back to `application/octet-stream`.
5. If the file extension is `.gz` and `download` query param
   is not `"true"`, wraps the reader in `gzip.NewReader` for
   transparent decompression and detects Content-Type from the
   inner filename (strip `.gz`).
6. If `download` is not `"true"` and file size exceeds
   `maxPreviewBytes` (default 10 MB), returns 413 with JSON
   `{"error":"file exceeds preview limit"}` and header
   `X-File-Size: <bytes>`.
7. If `download=true`, sets `Content-Disposition: attachment;
filename="<basename>"` and streams without size cap.
8. Sets `Content-Type` header and streams file bytes.

**Package:** `results/`
**File:** `results/server_file.go`
**Test file:** `results/server_file_test.go`

```go
const DefaultMaxPreviewBytes = 10 * 1024 * 1024

// ServerOption configures Server behaviour.
type ServerOption func(*Server)

// WithMaxPreviewBytes sets the file preview size limit.
func WithMaxPreviewBytes(n int64) ServerOption

// NewServer now accepts variadic options.
func NewServer(
    store *Store,
    validator *SeqmetaValidator,
    opts ...ServerOption,
) *Server
```

Route registered as:
`router.Get("/results/{id}/file", server.handleGetFile)`

**Acceptance tests:**

1. Given a result set with registered file
   `/tmp/out/report.html` (100 bytes) on disk, when
   `GET /results/{id}/file?path=/tmp/out/report.html`, then
   status 200, `Content-Type` is `text/html; charset=utf-8`,
   body equals file content.
2. Given a registered file `/tmp/out/data.csv` (50 bytes),
   when requested, then `Content-Type` is `text/csv`.
3. Given path `/tmp/out/unknown.xyz`, when requested, then
   `Content-Type` is `application/octet-stream`.
4. Given path `/tmp/out/not-registered.txt` that exists on
   disk but is NOT in the result set's file list, when
   requested, then status 403.
5. Given a registered file path that no longer exists on disk,
   when requested, then status 410 and body contains
   `"file not found on disk"`.
6. Given result set ID does not exist, when requested, then
   status 404.
7. Given no `path` query param, when requested, then status 400.
8. Given a registered `.gz` file containing gzipped CSV data
   (inner name `data.csv`), when requested without
   `download=true`, then status 200, `Content-Type` is
   `text/csv`, and body is the decompressed content.
9. Given the same `.gz` file with `?download=true`, then
   status 200, `Content-Type` is `application/gzip`,
   `Content-Disposition` contains `filename="data.csv.gz"`,
   and body is the raw compressed bytes.
10. Given a server with `WithMaxPreviewBytes(100)` and a
    registered file of 200 bytes, when requested without
    `download=true`, then status 413 and response includes
    `X-File-Size` header with value `"200"`.
11. Given the same oversized file with `?download=true`, then
    status 200 and full file is streamed.
12. Given a registered file `/tmp/out/image.png`, when
    requested, then `Content-Type` is `image/png`.

---

## B. Stats Endpoint

### B1: Aggregate statistics

As a consumer, I want `GET /results/stats?recent=N&days=D` to
return dashboard statistics, so that the frontend landing page
loads efficiently without fetching all result sets.

Query params:

- `recent` (int, default 10): number of most-recent result
  sets to return as mini summaries, ordered by `created_at`
  descending.
- `days` (int, default 30): number of days of daily
  registration counts.

Response JSON: `StatsResult` (defined in Architecture).

`Total` is `SELECT COUNT(*) FROM result_sets`.
`Recent` is the last N result sets as full `ResultSet`
objects (same shape as `GET /results/{id}`), ordered by
`created_at` descending.
`Daily` counts registrations per calendar day (UTC) for the
last D days, including days with zero count.
`Pipelines` is `SELECT pipeline_name, COUNT(*) ... GROUP BY
pipeline_name ORDER BY COUNT(*) DESC`.

**Package:** `results/`
**File:** `results/store.go` (Stats method)
**Test file:** `results/store_test.go`

Store method:

```go
func (s *Store) Stats(
    ctx context.Context, recent, days int,
) (*StatsResult, error)
```

Server handler in `results/server.go`:

Route: `router.Get("/results/stats", server.handleGetStats)`

Registered BEFORE the `/{id}` route so chi does not treat
`"stats"` as an ID.

**Acceptance tests:**

1. Given 5 result sets created on 3 different days, when
   `GET /results/stats` (defaults), then status 200,
   `total == 5`, `len(recent) == 5`, `recent[0]` is the
   newest, each recent entry is a full `ResultSet` with
   `id`, `pipeline_identifier`, `run_key`, `requester`,
   `operator`, `command`, `pipeline_name`,
   `pipeline_version`, `output_directory`, `metadata`,
   `created_at`, and `updated_at` fields.
2. Given 15 result sets, when `GET /results/stats?recent=3`,
   then `len(recent) == 3` and `total == 15`.
3. Given result sets created today and 2 days ago, when
   `GET /results/stats?days=3`, then `len(daily) == 3`,
   today's count >= 1, yesterday's count == 0, and the
   day-before-yesterday count >= 1.
4. Given 3 result sets with pipeline_name "nf-core/rnaseq"
   and 2 with "nf-core/sarek", then `pipelines` has 2
   entries, first has `count == 3`.
5. Given an empty store, then `total == 0`,
   `recent` is `[]`, `daily` has D entries all with
   `count == 0`, `pipelines` is `[]`.
6. Given `recent=0`, then `recent` is `[]` but `total` and
   other fields are still populated.
7. Given `recent=-1` or `days=abc`, then status 400.

---

## C. Meta-Keys Endpoint

### C1: Distinct metadata keys

As a consumer, I want `GET /results/meta-keys` to return all
distinct metadata keys, so that the frontend can populate
filter dropdowns dynamically.

Response JSON: `["library","seqmeta_runid","seqmeta_sampleid"]`
(sorted alphabetically).

**Package:** `results/`
**File:** `results/store.go` (MetaKeys method)
**Test file:** `results/store_test.go`

```go
func (s *Store) MetaKeys(
    ctx context.Context,
) ([]string, error)
```

SQL: `SELECT DISTINCT meta_key FROM result_metadata ORDER BY
meta_key`.

Server handler in `results/server.go`:

Route: `router.Get("/results/meta-keys",
server.handleGetMetaKeys)`

Registered BEFORE `/{id}` to avoid chi treating `"meta-keys"`
as an ID.

**Acceptance tests:**

1. Given result sets with metadata keys `"library"`,
   `"seqmeta_runid"`, and `"seqmeta_sampleid"`, when
   `GET /results/meta-keys`, then status 200 and body is
   `["library","seqmeta_runid","seqmeta_sampleid"]`.
2. Given no result sets, then status 200 and body is `[]`.
3. Given two result sets both having key `"library"`, then
   `"library"` appears once.

---

## D. Multi-Value OR Search

### D1: Repeated query params for OR within same key

As a consumer, I want
`GET /results?requester=alice&requester=bob` to return result
sets matching alice OR bob, so that I can search for multiple
values in one request.

Repeated query params for the same key produce OR within that
key. Different keys are ANDed. Metadata params also support
multi-value: `?meta_library=exon&meta_library=intron` matches
either.

This replaces the single-value `SearchParams` parsing with
`MultiSearchParams`. The `Store.SearchMulti` method builds SQL
with `IN (?, ?)` for multi-value scalar fields and `OR`
subqueries for multi-value metadata.

**Package:** `results/`
**File:** `results/store.go` (SearchMulti method),
`results/server.go` (updated parser)
**Test file:** `results/store_test.go`,
`results/server_test.go`

```go
func (s *Store) SearchMulti(
    ctx context.Context, params MultiSearchParams,
) ([]ResultSet, error)
```

For scalar fields with multiple values, generates
`field IN (?, ?, ...)`. For metadata with multiple values per
key, generates `EXISTS (SELECT 1 FROM result_metadata WHERE
result_id = result_sets.id AND meta_key = ? AND value IN
(?, ?))`. Single-value fields degrade to `= ?`.

The existing `Search` method and `SearchParams` remain
unchanged for backward compatibility. The server handler
switches to `SearchMulti`.

`searchParamsFromRequest` is updated to
`multiSearchParamsFromRequest`:

```go
func multiSearchParamsFromRequest(
    r *http.Request,
) MultiSearchParams
```

Maps `user` -> `Requester`, same as before. Collects all
values for each repeated param.

**Acceptance tests:**

1. Given 3 result sets (requesters "alice", "bob", "carol"),
   when `GET /results?user=alice&user=bob`, then status 200
   and 2 results returned.
2. Given `GET /results?user=alice&pipeline_name=nf`, then
   only result sets matching BOTH alice AND nf are returned
   (AND across keys).
3. Given `GET /results?meta_library=exon&meta_library=intron`,
   then result sets with library=exon OR library=intron are
   returned.
4. Given `GET /results?user=alice` (single value), then
   behaviour is identical to the original single-value search.
5. Given `GET /results` with no params, then all result sets
   returned.
6. Given `GET /results?user=nonexistent`, then `[]`.
7. Given `GET /results?seqmeta_sampleid=SANG1&seqmeta_sampleid=SANG2`,
   then result sets with seqmeta_sampleid matching either
   value are returned.
8. Given 3 result sets with output_dir_prefix `/data/a`,
   `/data/b`, `/data/c`, when
   `GET /results?output_dir_prefix=/data/a&output_dir_prefix=/data/b`,
   then 2 results returned (OR within output_dir_prefix).

---

## E. Study ID Search Shortcut

### E1: Server-side study_id resolution

As a consumer, I want `GET /results?study_id=6568` to
automatically resolve the study's samples via seqmeta and
search for result sets with matching `seqmeta_sampleid`
metadata, so that the frontend does not need to orchestrate
multi-step lookups.

When `study_id` query param is present and the server has a
configured seqmeta URL:

1. Call `GET <seqmeta-url>/study/<study_id>/samples` to get
   `[]saga.MLWHSample`.
2. Extract unique `SangerID` values from the sample list.
3. Add them as multi-value OR on `seqmeta_sampleid` metadata
   in the `MultiSearchParams.Meta` map (merged with any
   explicit `seqmeta_sampleid` values already in the query).
4. Proceed with normal multi-value search.
5. Wrap each result as `SearchResult`, populating
   `MatchedSamples` with the subset of resolved SangerIDs
   that appear in that result set's `seqmeta_sampleid`
   metadata. The response JSON is `[]SearchResult`.

When `study_id` is NOT present, the response remains
plain `[]ResultSet` (no wrapper).

If seqmeta URL is not configured and `study_id` is used,
return 400 with `"seqmeta not configured"`.

If seqmeta returns no samples for the study, return 200 with
`[]` (no matching results).

If seqmeta returns an error, return 502.

**Package:** `results/`
**File:** `results/server.go`
**Test file:** `results/server_test.go`

```go
// SeqmetaSampleResolver resolves study IDs to sample IDs.
type SeqmetaSampleResolver struct {
    baseURL string
    client  *http.Client
}

func NewSeqmetaSampleResolver(
    baseURL string, timeout time.Duration,
) *SeqmetaSampleResolver

func (r *SeqmetaSampleResolver) SamplesForStudy(
    ctx context.Context, studyID string,
) ([]string, error)
```

`NewServer` accepts an optional `SeqmetaSampleResolver`
(nil when seqmeta URL not configured):

```go
func NewServer(
    store *Store,
    validator *SeqmetaValidator,
    resolver *SeqmetaSampleResolver,
    opts ...ServerOption,
) *Server
```

**Acceptance tests:**

1. Given seqmeta mock returning 2 samples (SangerIDs "SANG1",
   "SANG2") for study "6568", and result sets with
   `seqmeta_sampleid=SANG1` and `seqmeta_sampleid=SANG3`,
   when `GET /results?study_id=6568`, then 1 result returned
   (the one with SANG1), response is `[]SearchResult`, and
   `matched_samples` for that result is `["SANG1"]`.
2. Given `study_id=6568` combined with `user=alice`, then
   only result sets matching BOTH alice AND a study sample
   are returned.
3. Given seqmeta returns empty sample list for the study,
   then status 200 and body is `[]`.
4. Given seqmeta URL not configured and `study_id=6568`, then
   status 400.
5. Given seqmeta mock returns error, then status 502.
6. Given `study_id=6568` combined with explicit
   `seqmeta_sampleid=SANG9`, then the sample IDs from
   seqmeta are merged with SANG9 (union), and result sets
   matching any of them are returned.
7. Given seqmeta returns 3 samples ("SANG1", "SANG2",
   "SANG3") and a result set has
   `seqmeta_sampleid=SANG1`, then that result's
   `matched_samples` is `["SANG1"]`, not the full list.
8. Given `GET /results?user=alice` (no `study_id`), then
   the response is plain `[]ResultSet` with no
   `matched_samples` wrapper.

---

## F. Seqmeta Listing Endpoints

### F1: List all studies

As a consumer, I want `GET /studies` on the seqmeta server to
return all SAGA studies, so that the frontend can populate a
study picker without needing SAGA credentials.

Handler calls `provider.AllStudies(ctx)` and returns the full
`[]saga.Study` as JSON.

**Package:** `seqmeta/`
**File:** `seqmeta/server.go`
**Test file:** `seqmeta/server_test.go`

Route: `router.Get("/studies", server.handleListStudies)`

**Acceptance tests:**

1. Given mock returning 3 studies, when `GET /studies`, then
   status 200 and JSON array has 3 entries with `name` and
   `id_study_lims` fields.
2. Given mock returning empty list, then status 200 and body
   is `[]`.
3. Given mock returning error, then status 502 and body has
   `"error"` key.
4. Response `Content-Type` is `application/json`.

### F2: List samples for a study

As a consumer, I want `GET /study/{id}/samples` on the seqmeta
server to return all samples for a study, so that the frontend
can resolve study-to-sample relationships.

Handler calls `provider.AllSamplesForStudy(ctx, id)` and
returns `[]saga.MLWHSample` as JSON.

**Package:** `seqmeta/`
**File:** `seqmeta/server.go`
**Test file:** `seqmeta/server_test.go`

Route: `router.Get("/study/{id}/samples",
server.handleStudySamples)`

**Acceptance tests:**

1. Given mock returning 5 samples for study "6568", when
   `GET /study/6568/samples`, then status 200 and JSON array
   has 5 entries with `sanger_id` fields.
2. Given mock returning `saga.ErrNotFound` for the study,
   then status 404.
3. Given mock returning other error, then status 502.
4. Given mock returning empty sample list, then status 200
   and body is `[]`.
5. Response `Content-Type` is `application/json`.

---

## Frontend Architecture

### Directory structure

```text
frontend/
  app/
    layout.tsx                  -- ThemeProvider, TooltipProvider, Toaster
    (results)/
      page.tsx                  -- dashboard: stats + search + table
      actions.ts                -- Server Actions (all backend calls)
      results/[id]/
        page.tsx                -- result detail + files
    api/
      health/route.ts           -- health check proxy
      file/route.ts             -- binary file streaming proxy
  components/
    ui/                         -- shadcn/ui primitives
    theme-provider.tsx
    stats-cards.tsx             -- stat number cards
    daily-chart.tsx             -- recharts bar chart
    results-table.tsx           -- DataTable wrapper
    results-columns.tsx         -- column definitions
    filter-builder.tsx          -- filter popover + chips
    study-combobox.tsx          -- study name typeahead
    file-browser.tsx            -- tabbed folder tree
    file-preview.tsx            -- content-type renderer
    result-metadata.tsx         -- metadata key-value display
    seqmeta-badge.tsx           -- enrichment badge + tooltip
  lib/
    backend-client.ts           -- resultsJson, seqmetaJson, resultsRaw
    contracts.ts                -- Zod schemas for all Go API types
    utils.ts                    -- cn() helper
    search-params.ts            -- URL <-> MultiSearchParams conversion
    studies-cache.ts            -- server-side studies list cache with TTL
    seqmeta-cache.ts            -- client-side enrichment cache
  tests/
    contracts.test.ts
    backend-client.test.ts
    search-params.test.ts
    studies-cache.test.ts
    seqmeta-cache.test.ts
  tests/integration/
    setup.ts                    -- globalSetup: compile Go, start server
    api.test.ts                 -- Vitest integration against real Go
  e2e/
    results.spec.ts             -- Playwright E2E
  .env.example
  components.json               -- shadcn/ui config
  vitest.config.ts
  playwright.config.ts
  package.json
  tsconfig.json
  next.config.ts
  eslint.config.mjs
  postcss.config.cjs
```

### Data flow

```text
Browser
  |
  |  (no direct backend access)
  v
Next.js Server
  |-- Server Components read searchParams, call Server Actions
  |-- Server Actions call resultsJson() / seqmetaJson()
  |-- API Routes stream binary via resultsRaw()
  |
  +---> Go results server (WA_RESULTS_BACKEND_URL)
  +---> Go seqmeta server (WA_SEQMETA_BACKEND_URL, optional)
```

Server Components fetch data via Server Actions in
`(results)/actions.ts`. Client Components handle
interactivity (filter builder, chart tooltips, file tree
expand/collapse). Binary content (images, PDFs, downloads)
flows through `/api/file/route.ts` API Route, which streams
from the Go file-content endpoint.

### Key adaptations from reference

The reference repo uses FastAPI; this project uses Go.
Differences:

- No `/api/v1/` prefix. Go routes are `/results`,
  `/results/stats`, etc.
- Two backend URLs instead of one (`WA_RESULTS_BACKEND_URL`,
  `WA_SEQMETA_BACKEND_URL`).
- `backendJson` becomes `resultsJson` and `seqmetaJson`.
- `resultsRaw` added for binary streaming.
- Health check proxies to Go's root endpoint (Go chi serves
  200 on any valid route; use `GET /results/stats` as the
  health signal).

---

## G. Project Scaffolding

### G1: Next.js 16 app with shared layout and config

As a developer, I want a scaffolded Next.js 16 app with
pnpm, shadcn/ui, Tailwind v4, and proper config, so that
frontend development can begin.

Files created:

- `frontend/package.json` with Next.js 16, React 19,
  shadcn/ui deps, Tailwind v4, Zod, recharts, sonner,
  next-themes, lucide-react, vitest, @tanstack/react-table.
  Scripts: `dev`, `build`, `start`, `lint`, `format`, `test`.
- `frontend/components.json` with new-york style, neutral
  base, lucide icons, `@/` import alias.
- `frontend/tsconfig.json` with `strict: true`, `@/*` path.
- `frontend/next.config.ts` with `reactStrictMode: true`.
- `frontend/vitest.config.ts` with `environment: 'node'`,
  `include: ['tests/**/*.test.ts']`, `@` alias.
- `frontend/eslint.config.mjs` extending next config.
- `frontend/postcss.config.cjs` with `@tailwindcss/postcss`.
- `frontend/app/globals.css` with `@import 'tailwindcss'`,
  `@theme inline` tokens (neutral palette), dark mode.
- `frontend/app/layout.tsx` with Inter font, ThemeProvider,
  TooltipProvider, Toaster.
- `frontend/components/theme-provider.tsx` wrapping
  next-themes.
- `frontend/components/ui/toaster.tsx` wrapping sonner.
- `frontend/lib/utils.ts` with `cn()` helper.
- `frontend/.env.example`:
    ```text
    FRONTEND_PORT=3000
    WA_RESULTS_BACKEND_URL=http://localhost:8090
    WA_SEQMETA_BACKEND_URL=
    WA_STUDIES_CACHE_TTL_SECONDS=300
    ```
- Root `.env.example`:
    ```text
    FRONTEND_PORT=3000
    WA_RESULTS_BACKEND_URL=http://localhost:8090
    WA_SEQMETA_BACKEND_URL=
    WA_STUDIES_CACHE_TTL_SECONDS=300
    WA_RESULTS_DB_PATH=./dev.db
    SAGA_API_TOKEN=
    SAGA_BASE_URL=
    ```

Route group `(results)/` for results pages. Later frontends
use separate route groups (e.g. `(samplepicker)/`).

**Package:** `frontend/`
**File:** multiple (see list above)
**Test file:** `frontend/tests/scaffold.test.ts`

**Acceptance tests:**

1. Given the scaffolded app, when `pnpm install && pnpm build`
   is run, then it exits 0 with no errors.
2. Given `frontend/.env.example`, then it contains
   `WA_RESULTS_BACKEND_URL`, `WA_SEQMETA_BACKEND_URL`, and
   `WA_STUDIES_CACHE_TTL_SECONDS`.
3. Given `frontend/components.json`, then `style` is
   `"new-york"` and `aliases.utils` is `"@/lib/utils"`.
4. Given `frontend/vitest.config.ts`, then `environment` is
   `"node"` and `@` alias resolves to the frontend root.

---

## H. Backend Clients and Contracts

### H1: Dual backend client

As a developer, I want type-safe fetch wrappers for both Go
backends, so that Server Actions can call them with Zod
validation.

`resultsJson<T>(path, schema)` fetches from
`WA_RESULTS_BACKEND_URL + path`, parses JSON, validates with
the Zod schema, returns typed `T`. Throws
`BackendRequestError` on non-2xx status or validation failure.

`seqmetaJson<T>(path, schema)` does the same for
`WA_SEQMETA_BACKEND_URL`. Throws `BackendUnavailableError`
if `WA_SEQMETA_BACKEND_URL` is not configured.

`resultsRaw(path)` fetches from the results backend and
returns the raw `Response` object (for binary streaming).

**Package:** `frontend/`
**File:** `frontend/lib/backend-client.ts`
**Test file:** `frontend/tests/backend-client.test.ts`

```typescript
export class BackendRequestError extends Error {
    constructor(
        public status: number,
        public body: unknown,
        message?: string,
    ) {
        super(message ?? `Backend request failed: ${status}`);
    }
}

export class BackendUnavailableError extends Error {
    constructor(service: string) {
        super(`${service} backend URL not configured`);
    }
}

export async function resultsJson<T>(
    path: string,
    schema: ZodSchema<T>,
): Promise<T>;

export async function seqmetaJson<T>(
    path: string,
    schema: ZodSchema<T>,
): Promise<T>;

export async function resultsRaw(path: string): Promise<Response>;
```

**Acceptance tests:**

1. Given `WA_RESULTS_BACKEND_URL=http://localhost:8090` and
   a mock returning `{"total":5}` at `/results/stats`, when
   `resultsJson('/results/stats', statsSchema)` is called,
   then it returns `{total: 5}` validated by the schema.
2. Given the mock returns status 404 with
   `{"error":"not found"}`, when called, then it throws
   `BackendRequestError` with `status === 404`.
3. Given the mock returns `{"bad":"shape"}`, when validated
   against a schema expecting `{total: z.number()}`, then it
   throws `BackendRequestError`.
4. Given `WA_SEQMETA_BACKEND_URL` is empty/undefined, when
   `seqmetaJson` is called, then it throws
   `BackendUnavailableError`.
5. Given `WA_SEQMETA_BACKEND_URL=http://localhost:8091` and
   a mock returning studies JSON, when `seqmetaJson` is
   called, then it returns validated data.
6. Given `resultsRaw('/results/1/file?path=/tmp/x.png')`,
   when the mock returns 200 with binary body, then the
   returned `Response` has the raw body streamable.

### H2: Zod contract schemas

As a developer, I want Zod schemas matching all Go API
response types, so that contract breaks are caught at the
boundary.

**Package:** `frontend/`
**File:** `frontend/lib/contracts.ts`
**Test file:** `frontend/tests/contracts.test.ts`

```typescript
// Core types
export const fileEntrySchema = z.object({
    path: z.string(),
    mtime: z.string(),
    size: z.number(),
    kind: z.enum(["output", "input", "pipeline"]),
});
export type FileEntry = z.infer<typeof fileEntrySchema>;

export const resultSetSchema = z.object({
    id: z.string(),
    pipeline_identifier: z.string(),
    run_key: z.string(),
    requester: z.string(),
    operator: z.string(),
    command: z.string(),
    pipeline_name: z.string(),
    pipeline_version: z.string(),
    output_directory: z.string(),
    metadata: z.record(z.string(), z.string()),
    created_at: z.string(),
    updated_at: z.string(),
});
export type ResultSet = z.infer<typeof resultSetSchema>;

export const searchResultSchema = z.object({
    result_set: resultSetSchema,
    matched_samples: z.array(z.string()).optional(),
});
export type SearchResult = z.infer<typeof searchResultSchema>;

// Stats
export const dailyCountSchema = z.object({
    date: z.string(),
    count: z.number(),
});

export const pipelineCountSchema = z.object({
    pipeline_name: z.string(),
    count: z.number(),
});

export const statsResultSchema = z.object({
    total: z.number(),
    recent: z.array(resultSetSchema),
    daily: z.array(dailyCountSchema),
    pipelines: z.array(pipelineCountSchema),
});
export type StatsResult = z.infer<typeof statsResultSchema>;

// Meta keys
export const metaKeysSchema = z.array(z.string());

// Seqmeta
export const identifierResultSchema = z.object({
    identifier: z.string(),
    type: z.string(),
    object: z.unknown(),
});
export type IdentifierResult = z.infer<typeof identifierResultSchema>;

export const studySchema = z
    .object({
        id_study_lims: z.number(),
        name: z.string(),
    })
    .passthrough();
export type Study = z.infer<typeof studySchema>;

export const studiesSchema = z.array(studySchema);

export const sampleSchema = z
    .object({
        sanger_id: z.string(),
    })
    .passthrough();

export const samplesSchema = z.array(sampleSchema);

// Error
export const errorSchema = z.object({
    error: z.string(),
});

// Health
export const healthSchema = z.object({
    status: z.string(),
});
```

**Acceptance tests:**

1. Given a valid ResultSet JSON object, when
   `resultSetSchema.parse(obj)` is called, then it returns
   the parsed object with all fields typed.
2. Given a ResultSet missing `id`, when `.safeParse()` is
   called, then `success === false`.
3. Given a valid StatsResult JSON, when
   `statsResultSchema.parse(obj)`, then it returns with
   `total` as number, `recent` as array, `daily` as array,
   `pipelines` as array.
4. Given a FileEntry with `kind: "output"`, when parsed,
   then it succeeds. Given `kind: "unknown"`, then it fails.
5. Given a Study JSON with extra fields beyond
   `id_study_lims` and `name`, when parsed with
   `studySchema`, then it succeeds (passthrough).
6. Given `metaKeysSchema.parse(["lib","run"])`, then it
   returns `["lib","run"]`.
7. Given `identifierResultSchema` with `object` containing
   arbitrary nested data, then it succeeds (`z.unknown()`).

---

## I. Health Check Proxy

### I1: Health check API route

As an operator, I want `GET /api/health` on the frontend to
proxy the Go backend health status, so that external monitors
can check the full stack.

The route calls `resultsJson('/results/stats', statsSchema)`.
On success returns `{"status":"healthy"}` with 200. On
failure returns `{"status":"unhealthy"}` with 503.

**Package:** `frontend/`
**File:** `frontend/app/api/health/route.ts`
**Test file:** `frontend/tests/health.test.ts`

```typescript
export const dynamic = "force-dynamic";
export async function GET(): Promise<NextResponse>;
```

**Acceptance tests:**

1. Given the Go backend is reachable, when
   `GET /api/health`, then status 200 and body is
   `{"status":"healthy"}`.
2. Given the Go backend is unreachable, when
   `GET /api/health`, then status 503 and body is
   `{"status":"unhealthy"}`.

---

## J. Dashboard Page

### J1: Dashboard with stats, search, and recent results

As a user, I want the landing page to show aggregate stats,
a search entry point, and recent results, so that I can
quickly understand the system state and start searching.

Server Component at `(results)/page.tsx`. Reads URL
`searchParams`. If no search params, fetches stats via
`fetchStats()` Server Action and displays:

- Stat cards: total result sets, distinct pipelines count,
  registrations today.
- Bar chart (recharts `BarChart`) showing daily registration
  counts for the last 30 days.
- Recent results table showing last 10 result sets (full
  `ResultSet` objects from stats `recent` array; the table
  displays the same columns as the search results table).

If search params are present, calls `searchResults(params)`
Server Action instead and displays matching results in the
table. Stats section remains visible above.

The filter builder (K1) and results table (L1) are embedded
on this page.

**Package:** `frontend/`
**File:** `frontend/app/(results)/page.tsx`
**Test file:** `frontend/tests/dashboard.test.ts`

Server Actions in `frontend/app/(results)/actions.ts`:

```typescript
"use server";

export async function fetchStats(
    recent?: number,
    days?: number,
): Promise<StatsResult>;

export async function searchResults(
    params: Record<string, string[]>,
): Promise<ResultSet[] | SearchResult[]>;

export async function fetchMetaKeys(): Promise<string[]>;

export async function fetchStudies(): Promise<Study[]>;

export async function fetchResult(id: string): Promise<ResultSet>;

export async function fetchFiles(id: string): Promise<FileEntry[]>;

export async function fetchFileContent(
    id: string,
    path: string,
): Promise<{ content: string; contentType: string }>;

export async function validateIdentifier(
    value: string,
): Promise<IdentifierResult | null>;
```

`StatsCards` component:

**File:** `frontend/components/stats-cards.tsx`

```typescript
interface StatsCardsProps {
    total: number;
    pipelineCount: number;
    todayCount: number;
}
export function StatsCards(props: StatsCardsProps): JSX.Element;
```

`DailyChart` component:

**File:** `frontend/components/daily-chart.tsx`

```typescript
interface DailyChartProps {
    data: DailyCount[];
}
export function DailyChart(props: DailyChartProps): JSX.Element;
```

**Acceptance tests:**

1. Given stats with `total=42`, 3 pipelines, and today's
   daily count = 5, when the page renders, then stat cards
   show "42", "3", and "5".
2. Given `daily` array with 30 entries, when the chart
   renders, then it has 30 bars.
3. Given `recent` has 10 entries, when no search params,
   then the table shows 10 rows.
4. Given URL `/?user=alice`, when the page loads, then
   `searchResults` is called with `{user: ["alice"]}` and
   results replace the recent table.
5. Given stats returns an error, then a toast shows the
   error and the page renders gracefully with empty state.

---

## K. Filter Builder and Search

### K1: Filter builder component

As a user, I want to build search filters with an "Add
filter" popover, so that I can combine multiple criteria
without a complex query language.

Client component. "Add filter" button opens a shadcn/ui
`Popover` containing a `Command` (combobox) listing
available fields: `requester`, `operator`, `pipeline_name`,
`pipeline_version`, `pipeline_identifier`, `run_key`,
`output_dir_prefix`, `study_id` (if seqmeta configured),
plus dynamic metadata keys fetched via `fetchMetaKeys()`.

After selecting a field:

- For `study_id`: show `StudyCombobox` (K2).
- For other fields: show a text `Input`.
- User enters value, presses Enter or clicks "Add".
- Filter appears as a removable chip/badge.

Multiple values for the same field create OR. Different
fields are ANDed. Displayed as grouped chips.

On filter change, updates the URL query string via
`router.push()` with the new params. This triggers a
server-side re-render with the new searchParams.

URL encoding: `?user=alice&user=bob&pipeline_name=nf`.
Repeated params for OR within a field.

**Package:** `frontend/`
**File:** `frontend/components/filter-builder.tsx`
**Test file:** `frontend/tests/filter-builder.test.ts`

```typescript
interface FilterBuilderProps {
    metaKeys: string[];
    seqmetaAvailable: boolean;
    currentFilters: Record<string, string[]>;
}
export function FilterBuilder(props: FilterBuilderProps): JSX.Element;
```

**File:** `frontend/lib/search-params.ts`
**Test file:** `frontend/tests/search-params.test.ts`

```typescript
// Parse URLSearchParams into grouped filter map.
export function parseSearchFilters(
    params: URLSearchParams,
): Record<string, string[]>;

// Build URLSearchParams from filter map.
export function buildSearchQuery(
    filters: Record<string, string[]>,
): URLSearchParams;
```

**Acceptance tests:**

1. Given `parseSearchFilters` with URL
   `?user=alice&user=bob&pipeline_name=nf`, then result is
   `{user: ["alice","bob"], pipeline_name: ["nf"]}`.
2. Given `buildSearchQuery({user: ["alice","bob"]})`, then
   result string is `user=alice&user=bob`.
3. Given empty filters, `buildSearchQuery({})` returns empty
   string.
4. Given `parseSearchFilters` with
   `?meta_library=exon&seqmeta_sampleid=SANG1`, then result
   is `{meta_library: ["exon"], seqmeta_sampleid: ["SANG1"]}`.
5. Given current filters `{user: ["alice"]}` and user adds
   `pipeline_name=nf`, then URL updates to
   `?user=alice&pipeline_name=nf`.
6. Given user removes the `user=alice` chip, then URL
   updates to `?pipeline_name=nf`.
7. Given user adds `user=bob` when `user=alice` already
   exists, then URL has `?user=alice&user=bob` (OR).

### K2: Study name typeahead combobox

As a user, I want to search by study name with autocomplete,
so that I can find studies without knowing their numeric IDs.

Client component. Fetches study list from `fetchStudies()`
Server Action (which uses the server-side studies cache,
K3). Filters client-side by name substring. On selection,
adds a `study_id=<id>` filter to the filter builder.

Only rendered when `seqmetaAvailable` is true.

**Package:** `frontend/`
**File:** `frontend/components/study-combobox.tsx`
**Test file:** `frontend/tests/study-combobox.test.ts`

```typescript
interface StudyComboboxProps {
    onSelect: (studyId: string) => void;
}
export function StudyCombobox(props: StudyComboboxProps): JSX.Element;
```

**Acceptance tests:**

1. Given studies `[{id_study_lims: 6568, name: "RNA Seq"}]`
   and user types "RNA", then "RNA Seq" appears in the
   dropdown.
2. Given user selects "RNA Seq", then `onSelect("6568")` is
   called.
3. Given studies fetch fails, then combobox shows
   "Studies unavailable" placeholder and is disabled.
4. Given empty string typed, then all studies are shown.

### K3: Server-side studies cache with configurable TTL

As a developer, I want the studies list cached in-memory on
the Next.js server with a configurable TTL, so that
`fetchStudies()` does not call seqmeta on every request.

**Package:** `frontend/`
**File:** `frontend/lib/studies-cache.ts`
**Test file:** `frontend/tests/studies-cache.test.ts`

The cache stores the last `Study[]` response and its fetch
timestamp. `WA_STUDIES_CACHE_TTL_SECONDS` env var controls
the TTL (default 300). When `getStudies()` is called:

1. If cached data exists and age < TTL, return cached data.
2. Otherwise call `seqmetaJson('/studies', studiesSchema)`,
   store the result, and return it.

```typescript
// Server-side module-level cache for studies list.
// TTL is read from WA_STUDIES_CACHE_TTL_SECONDS (default
// 300 seconds).

export function getStudies(): Promise<Study[]>;

// For testing: reset the cache.
export function resetStudiesCache(): void;
```

The `fetchStudies()` Server Action in
`(results)/actions.ts` delegates to `getStudies()`.

**Acceptance tests:**

1. Given `WA_STUDIES_CACHE_TTL_SECONDS=60` and a mock
   seqmeta returning 3 studies, when `getStudies()` is
   called twice within 60 seconds, then seqmeta is called
   exactly once and both calls return the same 3 studies.
2. Given `WA_STUDIES_CACHE_TTL_SECONDS=1` and a mock
   seqmeta returning 3 studies, when `getStudies()` is
   called, then after waiting >1 second a second call
   re-fetches from seqmeta (2 total seqmeta calls).
3. Given `WA_STUDIES_CACHE_TTL_SECONDS` is not set, then
   the default TTL is 300 seconds.
4. Given seqmeta returns an error on the first call, then
   `getStudies()` throws and no stale data is cached.
5. Given a cached result exists and seqmeta returns an
   error on re-fetch after TTL expiry, then `getStudies()`
   throws (does not serve stale data).

---

## L. Results Table

### L1: DataTable with pagination and column control

As a user, I want a sortable results table with column
visibility toggle and client-side pagination, so that I can
browse results efficiently.

Uses shadcn/ui DataTable pattern with `@tanstack/react-table`.
Columns: `pipeline_name`, `requester`, `created_at`,
`output_directory`. Hidden by default: `operator`, `command`,
`pipeline_version`, `pipeline_identifier`, `run_key`,
`id`. Column visibility toggle via
`DropdownMenuCheckboxItem`.

Client-side pagination (10/25/50 rows per page).
Sortable by clicking column headers.
Rows link to `/results/{id}`.

**Package:** `frontend/`
**File:** `frontend/components/results-table.tsx`,
`frontend/components/results-columns.tsx`
**Test file:** `frontend/tests/results-table.test.ts`

```typescript
interface ResultsTableProps {
    data: ResultSet[] | SearchResult[];
    studyActive?: boolean;
}
export function ResultsTable(props: ResultsTableProps): JSX.Element;
```

When `studyActive` is true and data items are
`SearchResult`, the table displays an additional
"Matched Samples" column showing the comma-separated
`matched_samples` values for each row. The column is
hidden when `studyActive` is false or data is plain
`ResultSet[]`.

**Acceptance tests:**

1. Given 25 results, when table renders with default page
   size 10, then 10 rows visible and pagination shows
   "Page 1 of 3".
2. Given column visibility toggle, when user hides
   `requester`, then that column disappears from the table.
3. Given user clicks "pipeline_name" header, then rows are
   sorted by pipeline name ascending; click again for
   descending.
4. Given 0 results, then table shows "No results found."
5. Given default column visibility, then `command`,
   `pipeline_version`, `pipeline_identifier`, `run_key`,
   `operator`, and `id` columns are hidden.
6. Given `studyActive=true` and data is `SearchResult[]`
   where one entry has `matched_samples: ["SANG1",
"SANG2"]`, then a "Matched Samples" column is visible
   and that row shows "SANG1, SANG2".
7. Given `studyActive=false`, then no "Matched Samples"
   column is shown.

---

## M. Result Detail Page

### M1: Result metadata with seqmeta enrichment

As a user, I want to view a result set's full metadata with
enriched seqmeta values, so that I understand the context of
a pipeline run.

Server Component at `(results)/results/[id]/page.tsx`.
Fetches result via `fetchResult(id)` and files via
`fetchFiles(id)`.

Displays: all ResultSet fields in a structured layout
(cards or definition list). Metadata key-value pairs in a
table.

For `seqmeta_*` metadata values, eagerly calls
`validateIdentifier(value)` for each via Server Action.
Results are passed to `SeqmetaBadge` components.

`SeqmetaBadge` displays: resolved type + short label inline
next to the raw value. Hover tooltip shows key details from
the resolved object (study name/accession for studies;
study name/library type/run ID for samples). On enrichment
failure, shows raw value with subtle "?" indicator and
tooltip "enrichment unavailable".

**Package:** `frontend/`
**File:** `frontend/app/(results)/results/[id]/page.tsx`,
`frontend/components/result-metadata.tsx`,
`frontend/components/seqmeta-badge.tsx`
**Test file:** `frontend/tests/seqmeta-badge.test.ts`

```typescript
interface SeqmetaBadgeProps {
    rawValue: string;
    enrichment: IdentifierResult | null;
    error?: boolean;
}
export function SeqmetaBadge(props: SeqmetaBadgeProps): JSX.Element;
```

Seqmeta enrichment cache (client-side):

**File:** `frontend/lib/seqmeta-cache.ts`

```typescript
// Client-side cache for seqmeta enrichment results.
// Keyed by raw identifier value. Shared across pages
// via React context to avoid redundant validateIdentifier
// calls when navigating between result sets.
export const SeqmetaCacheContext = React.createContext<SeqmetaCache>(
    new SeqmetaCache(),
);

export class SeqmetaCache {
    private cache: Map<string, IdentifierResult | null>;
    get(value: string): IdentifierResult | null | undefined;
    set(value: string, result: IdentifierResult | null): void;
    has(value: string): boolean;
}
```

The detail page checks `cache.has(value)` before calling
`validateIdentifier`. Hits return the cached result
immediately. The context provider wraps the `(results)/`
layout so the cache persists across client-side navigations.

**Acceptance tests:**

1. Given a result set with metadata
   `{seqmeta_sampleid: "SANG001"}` and enrichment returns
   type `"sanger_sample_id"` with object
   `{sanger_id: "SANG001", study_name: "RNA Seq"}`, then
   badge shows "sanger_sample_id: SANG001" with tooltip
   containing "RNA Seq".
2. Given enrichment fails (seqmeta unreachable), then badge
   shows "SANG001" with "?" indicator and tooltip
   "enrichment unavailable".
3. Given metadata `{library: "exon"}` (no `seqmeta_`
   prefix), then no enrichment attempted; plain value shown.
4. Given result set has 5 `seqmeta_*` values, then 5
   enrichment calls are made in parallel.
5. Given result set has 0 metadata entries, then metadata
   section shows "No metadata".
6. Given result set A has `seqmeta_sampleid=SANG001` and
   the user views it (enrichment fetched), when the user
   navigates to result set B which also has
   `seqmeta_sampleid=SANG001`, then the enrichment for
   `SANG001` is served from the client-side cache and no
   additional `validateIdentifier` call is made.

---

## N. File Browser

### N1: Tabbed folder tree

As a user, I want to browse a result set's files in a tree
organised by file kind, so that I can navigate large file
sets efficiently.

Client component with tabs: "Outputs", "Inputs", "Pipeline"
(shadcn/ui `Tabs`). Each tab shows a collapsible folder tree
built from the file paths of that kind.

Tree construction: split each `FileEntry.path` by `/`,
build nested nodes. Each folder node shows: name, file count,
expand/collapse chevron. Each file leaf shows: name, size
(human-readable), mtime. Clicking a file selects it for
preview (O1).

Folders are collapsed by default. Root-level folders auto-
expand if there is only one.

**Package:** `frontend/`
**File:** `frontend/components/file-browser.tsx`
**Test file:** `frontend/tests/file-browser.test.ts`

```typescript
interface FileBrowserProps {
    files: FileEntry[];
    onSelectFile: (file: FileEntry) => void;
    selectedPath?: string;
}
export function FileBrowser(props: FileBrowserProps): JSX.Element;
```

Tree node type (internal):

```typescript
interface TreeNode {
    name: string;
    path: string;
    isDir: boolean;
    size?: number;
    mtime?: string;
    children: TreeNode[];
    fileCount: number;
    typeCounts: Record<string, number>;
}
```

```typescript
// Build tree from flat file list.
export function buildFileTree(files: FileEntry[]): TreeNode[];
```

**Acceptance tests:**

1. Given files `[{path:"/out/a/1.txt", kind:"output"},
{path:"/out/a/2.txt", kind:"output"},
{path:"/in/b.fastq", kind:"input"}]`, when "Outputs"
   tab is active, then tree shows folder "out" > "a" with
   2 files. "Inputs" tab shows "in" with 1 file.
2. Given "Pipeline" tab with 0 files, then tab shows
   "No pipeline files".
3. Given `buildFileTree` with 3 files under `/out/a/` and
   1 under `/out/b/`, then root node "out" has 2 children
   folders with `fileCount` 3 and 1 respectively.
4. Given a single root folder `/results/`, then it auto-
   expands on render.
5. Given user clicks a file leaf, then `onSelectFile` is
   called with that `FileEntry`.
6. Given file size 1048576, then display shows "1.0 MB".
7. Given `buildFileTree` with folder `/out/a/` containing
   `1.csv`, `2.csv`, `3.png`, then the folder node has
   `typeCounts` equal to `{csv: 2, png: 1}`.
8. Given folder `/out/` containing subfolders `a/` (3 csv,
   1 png) and `b/` (2 txt), then `/out/` node has
   `typeCounts` `{csv: 3, png: 1, txt: 2}` (aggregated
   from all descendants).
9. Given a folder node with `typeCounts {csv: 3, png: 1}`,
   then the UI displays "3 csv, 1 png" as a summary next
   to the folder name.

---

## O. File Preview

### O1: Content-type renderer selection

As a user, I want files previewed with an appropriate
renderer based on content type, so that I can inspect results
without leaving the browser.

Client component. When a file is selected in the file
browser, fetches content:

- Text-based types (code, CSV, markdown, HTML, SVG): calls
  `fetchFileContent(id, path)` Server Action which uses
  `resultsRaw` to fetch the raw bytes and read the
  `Content-Type` response header.
- Binary types (images, PDFs): renders `<img>` or
  `<iframe>` pointing to `/api/file?id={id}&path={path}`
  (the binary proxy route, P1).

Renderer selection by Content-Type:

| Content-Type pattern                      | Renderer                      |
| ----------------------------------------- | ----------------------------- |
| `image/*`                                 | `<img>` via proxy URL         |
| `text/csv`, `text/tab-*`                  | Table (first 100 rows)        |
| `text/markdown`                           | Rendered markdown             |
| `text/html`                               | Sandboxed `<iframe>`          |
| `image/svg+xml`                           | `<img src=...>` (no inline)   |
| `application/pdf`                         | `<iframe>` via proxy URL      |
| `text/*`, `application/json`, source code | Syntax-highlighted code block |
| `application/octet-stream`                | File info + path display      |

CSV/TSV: parse with simple split, render in a shadcn/ui
`Table`. Show first 100 rows with "Showing 100 of N rows"
indicator.

Markdown: render via `react-markdown` or similar.

Code: syntax highlighting via `shiki` or `highlight.js`.

HTML: `<iframe sandbox="allow-same-origin">` with the proxy
URL as `src`. No `allow-scripts`.

SVG: always rendered as `<img src=...>`, never inline
(prevents script execution).

PDF: `<iframe src="proxy-url">` relying on browser PDF
viewer.

Truncation: if Go server returns 413, show "File too large
for preview" with file size from `X-File-Size` header.

Download button: for all previewable files. Links to
`/api/file?id={id}&path={path}&download=true`.

Non-previewable files (`.bam`, `.cram`, `.h5`, etc.): show
file metadata (name, size, mtime) and filesystem path. No
download button.

**Package:** `frontend/`
**File:** `frontend/components/file-preview.tsx`
**Test file:** `frontend/tests/file-preview.test.ts`

```typescript
interface FilePreviewProps {
    resultId: string;
    file: FileEntry;
    content?: { content: string; contentType: string };
    proxyUrl: string;
}
export function FilePreview(props: FilePreviewProps): JSX.Element;
```

```typescript
// Select renderer type from Content-Type header.
export function selectRenderer(
    contentType: string,
): "image" | "csv" | "markdown" | "html" | "svg" | "pdf" | "code" | "binary";
```

**Acceptance tests:**

1. Given `selectRenderer("image/png")`, then returns
   `"image"`.
2. Given `selectRenderer("text/csv")`, then `"csv"`.
3. Given `selectRenderer("text/markdown")`, then `"markdown"`.
4. Given `selectRenderer("text/html")`, then `"html"`.
5. Given `selectRenderer("image/svg+xml")`, then `"svg"`.
6. Given `selectRenderer("application/pdf")`, then `"pdf"`.
7. Given `selectRenderer("text/x-python")`, then `"code"`.
8. Given `selectRenderer("application/json")`, then `"code"`.
9. Given `selectRenderer("application/octet-stream")`, then
   `"binary"`.
10. Given `selectRenderer("text/plain")`, then `"code"`.
11. Given renderer `"html"`, then the rendered iframe has
    `sandbox="allow-same-origin"` and no `allow-scripts`.
12. Given renderer `"svg"`, then it renders as `<img>` tag,
    not inline SVG.
13. Given content fetch returns 413, then "File too large
    for preview" is shown with download link.
14. Given renderer `"binary"`, then file path, size, and
    mtime are displayed; no download button.
15. Given renderer `"csv"` with 200-row content, then table
    shows first 100 rows with "Showing 100 of 200 rows".
16. Given any previewable file, then a download button is
    visible linking to `proxyUrl + "&download=true"`.
17. Given renderer `"csv"` showing first 100 rows, when user
    clicks "Show all rows", then all 200 rows are rendered
    in the table.
18. Given renderer `"csv"` with a fully expanded table, when
    user clicks a column header, then the rows are sorted by
    that column ascending; clicking again sorts descending.
19. Given renderer `"csv"` with a fully expanded table, when
    user types "foo" in the filter input, then only rows
    containing "foo" in any column are shown.
20. Given renderer `"image"`, then the image is displayed as
    a thumbnail constrained to max-width 320px and
    max-height 240px.
21. Given renderer `"image"` with a thumbnail, when user
    clicks the thumbnail, then a lightbox overlay opens
    showing the full-size image centered on a dimmed
    backdrop.
22. Given an open lightbox, when user clicks the backdrop or
    presses Escape, then the lightbox closes.

---

## P. Binary Proxy Routes

### P1: File content streaming API route

As a user, I want Next.js to proxy binary file content from
the Go backend, so that images, PDFs, and downloads render
in the browser without exposing the Go server.

API route at `/api/file`. Query params: `id` (result set
ID), `path` (absolute file path), optional `download=true`.

Calls `resultsRaw` to fetch from Go's
`GET /results/{id}/file?path=...&download=...`. Streams the
response body back to the browser with the same
`Content-Type` and `Content-Disposition` headers.

On Go server error, returns the error status and JSON body.

**Package:** `frontend/`
**File:** `frontend/app/api/file/route.ts`
**Test file:** `frontend/tests/file-proxy.test.ts`

```typescript
export const dynamic = "force-dynamic";
export async function GET(request: NextRequest): Promise<NextResponse>;
```

**Acceptance tests:**

1. Given Go backend returns 200 with `Content-Type:
image/png` and binary body, when
   `GET /api/file?id=abc&path=/out/img.png`, then response
   has same Content-Type and body is streamed through.
2. Given `download=true`, then response includes
   `Content-Disposition: attachment` from Go.
3. Given Go returns 403, then proxy returns 403 with the
   error JSON.
4. Given Go returns 410, then proxy returns 410.
5. Given Go returns 413, then proxy returns 413 with
   `X-File-Size` header preserved.
6. Given missing `id` or `path` param, then returns 400.

---

## Q. E2E and Integration Tests

### Q1: Critical flow end-to-end tests

As a developer, I want Playwright tests against a real
compiled `wa results serve`, so that critical browser flows
are verified end-to-end.

Playwright config:

- `webServer` compiles Go (`go build`), starts
  `wa results serve` with a temp SQLite DB seeded from
  fixtures, and starts Next.js dev server.
- Tests run in Chromium.

Test file covers:

1. **Search flow**: load `/`, add a filter for requester,
   verify table updates with matching results.
2. **Result detail**: click a result row, verify
   `/results/{id}` page shows metadata and file browser.
3. **File browsing**: expand folder tree, verify files
   listed.
4. **File preview**: click a `.csv` file, verify table
   preview renders. Click a `.png`, verify image renders.

**Package:** `frontend/`
**File:** `frontend/e2e/results.spec.ts`
**Test file:** (this IS the test file)

```typescript
// playwright.config.ts
export default defineConfig({
    testDir: "./e2e",
    webServer: [
        {
            command: "bash ../run-dev.sh",
            port: 3000,
            reuseExistingServer: true,
            timeout: 120_000,
        },
    ],
});
```

**Acceptance tests:**

1. Given seeded fixtures with 3 result sets, when browser
   loads `/`, then stats show `total >= 3` and recent table
   has >= 3 rows.
2. Given user clicks "Add filter", selects "requester",
   types "alice", then table shows only result sets with
   requester "alice".
3. Given user clicks a result row, then browser navigates
   to `/results/{id}` and page shows pipeline_name and
   metadata.
4. Given the detail page, when user clicks "Outputs" tab
   and expands a folder, then file names are visible.
5. Given user clicks a CSV file in the tree, then a table
   preview renders with column headers.
6. Given user clicks a PNG file, then an `<img>` element
   is visible with a valid src URL.

### Q2: Vitest API-level integration tests

As a developer, I want Vitest-based integration tests that
compile Go, start a real `wa results serve`, and exercise
Server Actions and API routes WITHOUT a browser, so that
backend integration is tested cheaply.

Test setup (Vitest `globalSetup`):

1. `go build -o .tmp/wa .` from the repo root.
2. Create temp SQLite DB, start `wa results serve` on a
   random port, seed fixtures from
   `.docs/results-web/fixtures/seed.json`.
3. Set `WA_RESULTS_BACKEND_URL=http://localhost:<port>` in
   `process.env`.
4. Tear down: kill the Go process, remove temp DB.

Tests import and call the real Server Action functions and
the `/api/file` route handler directly (using
`next/test-utils` or direct function invocation with env
set).

**Package:** `frontend/`
**File:** `frontend/tests/integration/setup.ts` (global
setup), `frontend/tests/integration/api.test.ts`
**Test file:** `frontend/tests/integration/api.test.ts`

**Acceptance tests:**

1. Given the Go server is running with seeded fixtures,
   when `fetchStats()` is called, then it returns
   `total >= 3` and `recent.length >= 3`.
2. Given seeded fixtures include requester "alice", when
   `searchResults({user: ["alice"]})` is called, then it
   returns only result sets with requester "alice".
3. Given a seeded result set ID, when `fetchFiles(id)` is
   called, then it returns a non-empty array of
   `FileEntry` objects.
4. Given a seeded result set with a text file, when
   `fetchFileContent(id, path)` is called, then it
   returns `{content, contentType}` with non-empty content
   and a valid MIME type string.
5. Given a seeded result set with a registered image file,
   when a request is made to
   `/api/file?id={id}&path={path}`, then the response
   has status 200 and `Content-Type` starting with
   `image/`.
6. Given a path not registered in any result set, when
   `/api/file?id={id}&path={bad}` is requested, then
   the response has status 403.
7. Given `searchResults({user: ["nonexistent"]})`, then
   it returns `[]`.

---

## R. Dev Environment

### R1: run-dev.sh and dev fixtures

As a developer, I want a single script to build Go, start
servers, seed data, and start Next.js, so that the dev
environment is ready with one command.

**File:** `run-dev.sh` (repo root)

`run-dev.sh` behaviour:

1. Parse flags: `-f`/`--frontend-port` (default 3000),
   `-r`/`--results-port` (default 8090),
   `-s`/`--seqmeta-port` (default 8091).
2. `go build -o .tmp/wa .` to compile the binary.
3. Create temp DB: `export WA_RESULTS_DB_PATH=$(mktemp)`.
4. Start results server:
   `.tmp/wa results serve --port $RESULTS_PORT --db $DB`.
5. Seed fixtures: for each Registration in
   `.docs/results-web/fixtures/seed.json`, POST to
   `/results`.
6. If `SAGA_API_TOKEN` is set, start seqmeta server:
   `.tmp/wa seqmeta serve --port $SEQMETA_PORT`. Set
   `WA_SEQMETA_BACKEND_URL=http://localhost:$SEQMETA_PORT`.
7. Run frontend lint/format checks on changed files.
8. Run frontend unit tests.
9. Start Next.js dev server with env vars.
10. Wait for health checks. Clean up on SIGINT/SIGTERM.

Logs written to `./logs/results.log`, `./logs/seqmeta.log`,
`./logs/frontend.log`.

**File:** `.docs/results-web/fixtures/seed.json`

Array of Registration objects for seeding. Minimum 3 result
sets with varied pipelines, requesters, and metadata
(including `seqmeta_sampleid`). File paths point to
`.docs/results-web/fixtures/files/`.

**File:** `.docs/results-web/fixtures/files/`

Sample files committed to the repo:

- `report.csv` -- 20-row CSV
- `image.png` -- small PNG
- `pipeline.nf` -- Nextflow code
- `summary.md` -- markdown
- `results.html` -- small HTML page
- `plot.svg` -- SVG chart
- `config.json` -- JSON config

**Test file:** (tested via Playwright E2E, Q1)

**Acceptance tests:**

1. Given `run-dev.sh` is run, when Go compiles successfully,
   then `.tmp/wa` binary exists.
2. Given seed.json has 3 registrations, when seeding
   completes, then `GET /results` returns 3 results.
3. Given `SAGA_API_TOKEN` is not set, then seqmeta server
   is not started and `WA_SEQMETA_BACKEND_URL` is unset.
4. Given `SAGA_API_TOKEN` is set, then seqmeta server
   starts and `WA_SEQMETA_BACKEND_URL` is set.
5. Given SIGINT is sent, then all child processes are
   killed and temp DB is removed.

---

## Dev Fixtures

Fixture data at `.docs/results-web/fixtures/`:

```text
fixtures/
  seed.json                 -- Registration[] for seeding
  files/
    report.csv
    image.png
    pipeline.nf
    summary.md
    results.html
    plot.svg
    config.json
```

`seed.json` format (array of Registration objects):

```json
[
    {
        "pipeline_identifier": "https://github.com/org/nf-rnaseq",
        "run_key": "runid=48522&unique=exon_lib",
        "requester": "alice",
        "operator": "bob",
        "command": "nextflow run nf-rnaseq",
        "pipeline_name": "nf-core/rnaseq",
        "pipeline_version": "3.14.0",
        "output_directory": ".docs/results-web/fixtures/files",
        "files": [
            {
                "path": "report.csv",
                "mtime": "...",
                "size": 500,
                "kind": "output"
            },
            {
                "path": "image.png",
                "mtime": "...",
                "size": 1024,
                "kind": "output"
            }
        ],
        "metadata": {
            "seqmeta_sampleid": "SANG001",
            "library": "exon"
        }
    }
]
```

`run-dev.sh` reads this file, resolves relative file paths
to absolute, and POSTs each registration to the seeded Go
server. The fixture files directory is committed to the repo
so file-content preview works in development.

---

## Implementation Order

Interleaved Go and frontend phases. Each frontend phase
builds on tested Go endpoints from prior phases.

### Phase 1: Go foundation (sequential)

1. **A1** -- file content endpoint
2. **B1** -- stats endpoint
3. **C1** -- meta-keys endpoint

### Phase 2: Go search enhancements (sequential)

4. **D1** -- multi-value OR search
5. **E1** -- study_id search shortcut
6. **F1** -- seqmeta list studies endpoint
7. **F2** -- seqmeta list samples endpoint

### Phase 3: Frontend scaffold (sequential)

8. **G1** -- project scaffolding
9. **H1** -- backend clients
10. **H2** -- Zod contracts
11. **I1** -- health check proxy

### Phase 4: Dashboard and search (sequential)

12. **J1** -- dashboard page with stats
13. **K1** -- filter builder
14. **K3** -- studies cache
15. **K2** -- study name typeahead
16. **L1** -- results table

### Phase 5: Detail and files (sequential)

17. **M1** -- result detail + seqmeta enrichment
18. **N1** -- file browser (folder tree)
19. **P1** -- binary proxy routes
20. **O1** -- file preview renderers

### Phase 6: Integration (sequential)

21. **R1** -- run-dev.sh + fixtures
22. **Q2** -- Vitest API-level integration tests
23. **Q1** -- Playwright E2E tests

---

## Appendix: Key Decisions

### BFF pattern

Browser never contacts Go directly. All data flows through
Next.js Server Actions (JSON) or API Routes (binary streams).
Backend URLs are server-side environment variables only.

### Two backend URLs

`WA_RESULTS_BACKEND_URL` and `WA_SEQMETA_BACKEND_URL` are
separate because the services run on different ports and may
be deployed to different hosts. The frontend has no SAGA
credentials; it delegates to seqmeta for all SAGA data.

### Server Actions vs API Routes

Server Actions handle all JSON data fetching (results, stats,
search, seqmeta enrichment). API Routes handle binary
streaming (images, PDFs, downloads) and the health check
endpoint. This follows the reference implementation pattern.

### No server-side pagination

Client-side pagination via `@tanstack/react-table` for v1.
Expected volumes are small (hundreds to low thousands of
result sets). Server-side pagination can be added later.

### Seqmeta graceful degradation

When `WA_SEQMETA_BACKEND_URL` is not configured:

- Study search filter is hidden.
- Seqmeta enrichment badges show raw values.
- No errors or blocked renders.

### File content security

- HTML rendered in `<iframe sandbox="allow-same-origin">`.
  No `allow-scripts`.
- SVG rendered as `<img src=...>` only. No inline SVG.
- Go server only serves database-registered files (no
  arbitrary path traversal).
- Go server returns 403 for unregistered paths.

### Testing strategy

- **Vitest** (unit): contracts, backend client, search param
  parsing, component logic. Mock HTTP via `vi.mock` or MSW.
- **Playwright** (E2E): critical browser flows against real
  Go server + Next.js. Seeded with dev fixtures.
- Reference: `go-conventions` for Go tests,
  `nextjs-fastapi-conventions` for frontend tests.
