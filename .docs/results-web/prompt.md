# Results Web UI

## Feature Description

Build a web UI for the results sub-product described in `.docs/proposal.md`,
integrating with the REST API specified in `.docs/results-rest/spec.md`.

### Core Requirements

1. **Next.js 16 App Router frontend** following the exact same patterns as the
   "hello world" implementation at https://github.com/wtsi-hgi/llm-knowledge-base:
   - Server Actions call the Go backend server-to-server (backend URL never
     exposed to browser)
   - `lib/backend-client.ts` thin fetch wrapper used by Server Actions
   - `lib/contracts.ts` Zod schemas mirroring Go API JSON responses
   - `run-dev.sh` script at repo root that starts frontend + backend together,
     runs lint/format/tests before starting, writes logs to `./logs/`
   - `.env` / `.env.example` configuration for ports and backend URL
   - Same ThemeProvider, TooltipProvider, Toaster pattern from layout.tsx
   - Health check proxy route at `/api/health` for external monitors
   - Vitest for unit tests, same vitest.config.ts pattern
   - pnpm as package manager
   - shadcn/ui components (new-york style, neutral base colour, lucide icons)
   - Tailwind CSS v4 with `@theme` design tokens

2. **Backend is the Go `wa results serve` command** (not FastAPI). The frontend
   communicates with it via the REST API endpoints defined in
   `.docs/results-rest/spec.md`:
   - `POST /results` — register a result set
   - `GET /results` — search with query params
   - `GET /results/{id}` — get one result set
   - `GET /results/{id}/files` — get file list
   - `PUT /results/{id}/files` — replace output files
   - `DELETE /results/{id}` — hard delete

3. **Optional seqmeta integration** for enhanced searching. When a seqmeta
   service URL is configured, the UI can resolve identifiers to find related
   results. For example, searching for a study ID queries seqmeta to find all
   samples in that study, then searches results for any that match those sample
   identifiers via `seqmeta_sampleid` metadata. The seqmeta REST API is
   specified in `.docs/seqmeta/spec.md`:
   - `GET /validate/{identifier}` — classify an identifier (study_id,
     sanger_sample_id, etc.)
   - `GET /diff/study/{id}` — get samples for a study

4. **Advanced search UI** that is easy and intuitive:
   - Search by all result set fields: requester, operator, pipeline_name,
     pipeline_version, pipeline_identifier, run_key, output_dir_prefix
   - Search by metadata key-value pairs (meta_X=V, seqmeta_X=V)
   - When seqmeta is available, search by study name/ID to find results with
     matching sample identifiers
   - Filters are combinable (AND logic)
   - Clear, sortable results table

5. **Folder tree view** — one of the primary views:
   - Build a tree from the file list of a result set
   - Expandable/collapsible folders
   - Quick summary of file types per folder (without displaying massive file
     lists for large folders)
   - Ability to expand to show full folder contents with file details

6. **Rich file preview/display** when viewing full folder contents:
   - **Images** (png, jpg, gif, svg, webp): clickable thumbnails
   - **CSV/TSV**: "thumbnail" table of first few rows, clickable to show full
     table with sorting/filtering
   - **Markdown**: rendered via markdown parser
   - **Code files** (py, go, js, ts, sh, etc.): syntax highlighting
   - **HTML**: rendered inline in an iframe or similar
   - **Gzipped files**: transparently decompressed before preview
   - **PDF**: embedded viewer or thumbnail
   - Wide variety of common file format support with sensible fallbacks

7. **File organisation** for multi-frontend repo:
   - Frontend code lives under a path that supports multiple sub-product
     frontends (e.g. `frontend/results/` or similar)
   - Shared components and utilities (like seqmeta search, backend-client,
     common UI patterns) live in a shared location that other frontends
     (e.g. samplepicker) can reuse
   - Each frontend has its own App Router pages but shares components/lib

8. **Testing**:
   - Unit tests via Vitest for contracts, components, and utilities
   - Mock REST API server for most testing (MSW or similar)
   - **Real integration tests** that test the frontend works with a real
     compiled `wa results serve` binary — start the Go server, start the
     frontend, exercise key flows

9. **UI quality**: Take full advantage of Tailwind CSS v4 + shadcn/ui for
   beauty, interactivity, and consistency. Prefer pre-existing shadcn/ui
   components over custom implementations. Dark mode support. Responsive layout.

### Reference Implementation Patterns

From https://github.com/wtsi-hgi/llm-knowledge-base:

- **run-dev.sh**: Starts both frontend and backend. Runs lint/format on changed
  files, runs unit tests, starts both servers via setsid, waits for health
  checks, writes logs to `./logs/`. Accepts `-f`/`-b` port flags.
- **.env.example** (root): `FRONTEND_PORT`, `BACKEND_PORT`, `BACKEND_URL`,
  `CORS_ORIGINS`
- **.env.example** (frontend): `BACKEND_URL`, `BACKEND_PORT`, `FRONTEND_PORT`
- **frontend/lib/backend-client.ts**: Thin fetch wrapper that reads
  `BACKEND_URL` or constructs from `BACKEND_PORT`, validates response with Zod
- **frontend/lib/contracts.ts**: Zod schemas matching backend JSON responses
- **frontend/app/actions.ts**: Server Actions using `backendJson()` helper
- **frontend/app/layout.tsx**: ThemeProvider + TooltipProvider + Toaster
- **frontend/app/api/health/route.ts**: Health check proxy for external monitors
- **frontend/components.json**: shadcn/ui config (new-york style, neutral base)
- **frontend/vitest.config.ts**: Vitest with path aliases
- **frontend/package.json**: Next.js 16, React 19, shadcn/ui deps, Tailwind v4,
  Zod, vitest, pnpm scripts (dev, build, start, lint, format, test)

## Notes

- File content access for preview: add a new
  `GET /results/{id}/file?path=/absolute/path` content-serving endpoint to the
  Go server that streams raw file bytes. The Next.js server proxies content from
  Go via Server Actions or API routes. This keeps the Go server as the single
  source of truth for file access (BFF pattern). This new endpoint must be
  specified in the spec as a required Go backend addition.
- The web UI is read-only: search, browse, and preview files. Registration,
  deletion, and rescan remain CLI-only operations.
- Multi-frontend structure: single Next.js app with route groups.
  `frontend/app/(results)/` for results pages, later `frontend/app/(samplepicker)/`
  for sample picker. Shared code in `frontend/lib/` and `frontend/components/`.
  This avoids pnpm workspace complexity while naturally sharing utilities.
- No server-side pagination for v1. Use client-side table pagination via
  shadcn/ui DataTable. Volumes are small initially; can add server-side
  pagination later.
- Seqmeta integration goes deep: validate identifier, get samples from study,
  search results for each sample, aggregate and deduplicate into a single
  results table with match indicators. Additionally, do reverse enrichment:
  when viewing a result set that has `seqmeta_sampleid` or similar metadata,
  show the study name and sample details inline by querying seqmeta.
- Seqmeta routing: the Next.js server talks to two separate backend URLs
  (`WA_RESULTS_BACKEND_URL`, `WA_SEQMETA_BACKEND_URL`). Server Actions call
  seqmeta directly (not proxied through the Go results server).
- The spec covers the full stack: Go backend endpoint additions (file-content
  serving) plus the Next.js frontend. One spec, one implementation.
- Untrusted file content safety: render HTML in heavily sandboxed iframes
  (`<iframe sandbox="allow-same-origin">`) with restrictive CSP. Display SVGs
  as `<img src=...>` only (prevents script execution). No inline SVG rendering.
- Go server enforces a configurable max preview size (e.g. 10 MB) on the
  file-content endpoint; returns a truncation header for files exceeding it.
  The UI shows "file too large for preview" with a download option.
- Landing page: dashboard with aggregate stats (count of result sets, pipelines,
  recent activity) plus a prominent search entry point.
- No authentication for v1. Internal network only, open access.
- Dashboard stats: add a new `GET /results/stats` Go endpoint that returns
  server-side aggregate counts (total result sets, distinct pipeline names,
  recent activity). Efficient; avoids fetching all results client-side.
- Binary file proxying: hybrid approach. Next.js API routes stream binary
  content (images, PDFs, raw downloads) from the Go file-content endpoint to
  the browser. Server Actions handle text-based previews (CSV data, code,
  markdown content).
- Integration tests: Playwright for critical browser-based E2E happy paths
  against a real compiled `wa results serve`. Vitest for API-level integration
  tests against the real Go server (testing Server Actions and API routes
  without a browser).
- File content endpoint serves only database-registered files for security. New
  files added after registration are invisible until a CLI rescan is performed.
  No rescan button in the UI (stays read-only).
- Dashboard recent activity includes both: a list of the last N result sets as
  mini summaries, plus time-bucketed counts (e.g. registrations per day/week)
  for sparkline/chart visualization.
- Reverse seqmeta enrichment is eager: when viewing a result set with seqmeta
  metadata, enrich all seqmeta values (call `/validate/{value}` for each).
  Cache enrichment results client-side to avoid redundant calls within the same
  session.
- Non-previewable files (.bam, .cram, .h5, etc.) show file metadata (name,
  size, mtime) plus the filesystem path so users can access files directly via
  their terminal. No download button — bioinformatics files need specialised
  tools anyway.
- Multi-value OR search uses repeated query params (standard HTTP convention):
  `?requester=alice&requester=bob` means match alice OR bob. No comma-separated
  values, so no escaping ambiguity. The Go REST API query parser must support
  repeated params for all search fields.
- Stats endpoint response is configurable: `GET /results/stats?recent=N&days=D`
  where N defaults to 10 (last N result sets as mini summaries) and D defaults
  to 30 (daily counts for the last D days). Also returns distinct pipeline names
  with per-pipeline result counts.
- URL routes: `/` is a combined SPA-style dashboard with stats, search bar, and
  results table all on one page. `/results/{id}` is the detail page with
  metadata, folder tree, and file previews.
- File content endpoint serves all registered file kinds (output, input,
  pipeline). The folder tree view organises files by kind, with separate
  "Outputs", "Inputs", "Pipeline" sections.
- SAGA data access: add non-diff data listing endpoints to the existing seqmeta
  REST server: `GET /studies` (wraps SAGAProvider.AllStudies()) and
  `GET /study/{id}/samples` (wraps SAGAProvider.AllSamplesForStudy()). These are
  thin passthrough handlers on seqmeta's existing chi router. The web UI talks
  to seqmeta for all SAGA-related data (validate, list studies, list samples).
  No SAGA credentials needed on the Next.js server. The seqmeta spec gets a
  small amendment for these 2 new endpoints.
- Environment variable naming: use specific names per backend, not generic
  BACKEND_URL. E.g. `WA_RESULTS_BACKEND_URL`, `WA_SEQMETA_BACKEND_URL`. Each
  frontend talks to different backends on different ports.
- Go Content-Type detection: file-content endpoint detects Content-Type from
  file extension (Go's `mime.TypeByExtension`) and sets the response header.
  Frontend relies on the header to choose a renderer.
- Dev fixtures: JSON file of Registration objects that run-dev.sh POSTs to the
  API, alongside a fixture directory of sample files (images, CSVs, code, HTML,
  etc.) committed to the repo. run-dev.sh compiles Go, starts server, and seeds
  with these fixtures.
- Dashboard chart: use `recharts` library for interactive sparklines/bar charts
  showing registrations per day.
- Seqmeta enrichment error handling: show raw metadata value with subtle error
  indicator tooltip ("enrichment unavailable"). Never block page render.
- Study name search: call seqmeta `GET /studies` once, cache the list in-memory
  on the Next.js server with a configurable TTL, and filter by name client-side.
  Total number of studies is small enough for this approach.
- Go backend additions: include full Go acceptance tests for the file-content
  endpoint, stats endpoint, and multi-value OR search in this spec alongside
  the frontend user stories. One unified spec.
- OR search semantics: OR within the same key (repeated query params), AND
  across different keys. E.g.
  `?seqmeta_sampleid=A&seqmeta_sampleid=B&meta_library=exon` means
  `(sampleid=A OR sampleid=B) AND library=exon`. Same logic for regular fields
  like `?requester=alice&requester=bob`.
- run-dev.sh: compiles the Go binary (`go build`), creates a temp SQLite
  database, starts `wa results serve`, AND seeds the DB with sample data from a
  fixture file for development convenience. Add `WA_RESULTS_DB_PATH` to
  `.env.example`.
- Spec structure: single spec with Go user stories (GoConvey, TDD, following
  go-conventions) first, then TypeScript user stories (Vitest, following
  nextjs-fastapi-conventions). One interleaved implementation order.
- Seqmeta endpoint additions (GET /studies, GET /study/{id}/samples) are
  included as user stories in this spec (it spans results/, seqmeta/, and
  frontend/ packages).
- Search URL state: URL-driven search. Parameters encoded in query string
  (e.g. `/?requester=alice&pipeline_name=nf`), enabling shareable/bookmarkable
  searches. Browser back/forward navigates search history.
- Results table: smart column defaults (hide long/technical fields like command,
  output_directory, pipeline_identifier by default) with a column visibility
  toggle (shadcn/ui DataTable pattern) for power users.
- Seqmeta enrichment display: minimal inline text (resolved type + primary name
  next to raw value) with hover tooltip showing key details from the SAGA
  object (study name, accession, sample count for studies; study name, library
  type, run ID for samples). Never blocks page render on failure; shows raw
  value with subtle "enrichment unavailable" indicator.
- Study search with many samples: add a `GET /results?study_id=X` server-side
  shortcut parameter on the Go results server. When `study_id` is provided, the
  Go server calls seqmeta to get all samples for the study, then searches
  internally for results matching any of those sample IDs. No URL length risk;
  single request from the frontend. The Go server needs `--seqmeta-url` for
  this (already has it).
- Seqmeta new endpoint response format: return raw saga types serialised
  directly as JSON. The frontend Zod schemas extract the fields they need; extra
  fields are harmless.
- Folder tree structure: tabs for each file kind (Outputs, Inputs, Pipeline),
  each tab containing its own folder tree. Users switch between tabs to browse
  different file categories.
- Playwright E2E: full v1 scope. Critical flows (search, view result, browse
  files, file preview) must be tested end-to-end in a real browser against a
  real compiled `wa results serve`.
- Landing page initial table state: show the most recent N result sets by
  default (e.g. latest 50). When the user searches, the results table is
  replaced with search results. The stats/dashboard section always stays visible.
- Download capability: download button for all previewable files alongside the
  preview. Users can download images, CSVs, PDFs, etc. Non-previewable files
  still show only the filesystem path (no download since they may be very large
  bioinformatics files).
- `study_id` search when seqmeta unavailable: return 400 "study search requires
  seqmeta configuration" when `--seqmeta-url` is not configured; return 502
  when seqmeta is configured but unreachable (matching existing POST validation
  behaviour).
- Missing files on disk: the file content endpoint returns 410 Gone (not 404)
  to distinguish from "file never existed in DB". The folder tree UI does not
  need to proactively detect missing files; the 410 is shown when the user tries
  to preview.
- File download vs preview truncation: add `?download=true` query parameter to
  the file-content endpoint. When `download=true`, disable the size cap and set
  `Content-Disposition: attachment` with the original filename. Preview requests
  (default, no param) remain truncated at the configured limit.
- Search query param naming: keep `user` as the query parameter (matching the
  existing REST API spec). The frontend maps its "Requester" column label to
  `?user=...` internally. No rename needed.
- Stats endpoint mini summaries: return full `ResultSet` objects (same shape as
  `GET /results/{id}`). The frontend picks which fields to display on dashboard
  cards. Simpler to implement, no extra DTO needed.
- Search UI widget pattern: filter builder (Linear/Notion style). "Add filter"
  button opens a popover where the user picks a field from a dropdown, then
  enters a value. Compact, scales to many fields. Natural fit for shadcn/ui
  Command + Popover components. Supports arbitrary metadata key-value pairs via
  a dynamic add-row pattern.
- Gzip handling: for downloads (`?download=true`), serve the original compressed
  `.gz` file as-is (users expect the actual file from disk). For previews
  (default), decompress the content and set Content-Type based on the inner file
  extension (e.g. `text/csv` for `data.csv.gz`) so the frontend picks the
  correct renderer.
- Dev environment seqmeta availability: require real SAGA credentials in `.env`
  for dev when seqmeta features are needed. `run-dev.sh` starts
  `wa seqmeta serve` if SAGA credentials are configured, and sets
  `WA_SEQMETA_BACKEND_URL` automatically. If credentials are absent,
  `run-dev.sh` skips starting seqmeta and does not set
  `WA_SEQMETA_BACKEND_URL`; the UI gracefully degrades (seqmeta features show
  "enrichment unavailable").
- Study name search UX: autocomplete combobox with typeahead. When the user
  selects "Study" as a filter field in the filter builder, a combobox fetches
  the cached studies list from the Next.js server and offers typeahead
  suggestions by name. Selecting a study resolves to `?study_id=X`.
- Metadata key discovery: add a `GET /results/meta-keys` endpoint to the Go
  results server that returns distinct metadata keys across all result sets. The
  filter builder uses this to populate a key dropdown/autocomplete when the user
  adds a metadata filter. Users can also type custom keys as free text.
