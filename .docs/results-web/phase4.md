# Phase 4: Dashboard and search

Ref: [spec.md](spec.md) sections J1, K1, K2, K3, L1

## Instructions

Use the `orchestrator` skill to complete this phase,
coordinating subagents with the `nextjs-fastapi-implementor`
and `nextjs-fastapi-reviewer` skills.

## Items

### Item 4.1: J1 - Dashboard with stats, search, and recent results

spec.md section: J1

Create Server Component at
`frontend/app/(results)/page.tsx`. Create Server Actions
in `frontend/app/(results)/actions.ts`: `fetchStats`,
`searchResults`, `fetchMetaKeys`, `fetchStudies`,
`fetchResult`, `fetchFiles`, `fetchFileContent`,
`validateIdentifier`. Create `StatsCards` component in
`frontend/components/stats-cards.tsx` (total, pipeline
count, today count). Create `DailyChart` component in
`frontend/components/daily-chart.tsx` (recharts BarChart
for 30-day daily counts). Page shows stats + recent table
when no search params; shows search results when params
present. Tests in `frontend/tests/dashboard.test.ts`.
Covering all 5 acceptance tests from J1.

- [ ] implemented
- [ ] reviewed

### Batch 2 (parallel, after item 4.1 is reviewed)

#### Item 4.2: K1 - Filter builder component [parallel with 4.3, 4.4]

spec.md section: K1

Create `frontend/components/filter-builder.tsx` client
component with shadcn/ui `Popover` + `Command` for field
selection. Fields: `requester`, `operator`,
`pipeline_name`, `pipeline_version`,
`pipeline_identifier`, `run_key`, `output_dir_prefix`,
`study_id` (if seqmeta configured), plus dynamic metadata
keys. Removable chips for active filters. URL update via
`router.push()`. Create
`frontend/lib/search-params.ts` with
`parseSearchFilters` and `buildSearchQuery` utilities.
Tests in `frontend/tests/filter-builder.test.ts` and
`frontend/tests/search-params.test.ts`. Covering all 7
acceptance tests from K1.

- [ ] implemented
- [ ] reviewed

#### Item 4.3: K3 - Studies cache [parallel with 4.2, 4.4]

spec.md section: K3

Create `frontend/lib/studies-cache.ts` with module-level
in-memory cache for `Study[]`. `getStudies()` returns
cached data if age < TTL, otherwise calls
`seqmetaJson('/studies', studiesSchema)`. TTL controlled
by `WA_STUDIES_CACHE_TTL_SECONDS` env var (default 300).
Export `resetStudiesCache()` for testing. The
`fetchStudies()` Server Action in `actions.ts` delegates
to `getStudies()`. Tests in
`frontend/tests/studies-cache.test.ts`. Covering all 5
acceptance tests from K3.

- [ ] implemented
- [ ] reviewed

#### Item 4.4: L1 - Results table [parallel with 4.2, 4.3]

spec.md section: L1

Create `frontend/components/results-table.tsx` and
`frontend/components/results-columns.tsx` using shadcn/ui
DataTable with `@tanstack/react-table`. Visible columns:
`pipeline_name`, `requester`, `created_at`,
`output_directory`. Hidden by default: `operator`,
`command`, `pipeline_version`, `pipeline_identifier`,
`run_key`, `id`. Column visibility toggle via
`DropdownMenuCheckboxItem`. Client-side pagination
(10/25/50 per page). Sortable headers. Rows link to
`/results/{id}`. Conditional "Matched Samples" column
when `studyActive` is true. Tests in
`frontend/tests/results-table.test.ts`. Covering all 7
acceptance tests from L1.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `nextjs-fastapi-reviewer`
skill (review all items in the batch together in a single
review pass).

### Item 4.5: K2 - Study name typeahead combobox

spec.md section: K2

Depends on item 4.2 (K1) for filter builder integration
and item 4.3 (K3) for `fetchStudies()`.

Create `frontend/components/study-combobox.tsx` client
component. Fetches study list via `fetchStudies()` Server
Action. Filters client-side by name substring. On
selection, calls `onSelect(studyId)` to add
`study_id=<id>` filter. Only rendered when
`seqmetaAvailable` is true. Shows "Studies unavailable"
on fetch failure. Tests in
`frontend/tests/study-combobox.test.ts`. Covering all 4
acceptance tests from K2.

- [ ] implemented
- [ ] reviewed
