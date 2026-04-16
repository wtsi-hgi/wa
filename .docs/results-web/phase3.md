# Phase 3: Frontend scaffold

Ref: [spec.md](spec.md) sections G1, H1, H2, I1

## Instructions

Use the `orchestrator` skill to complete this phase,
coordinating subagents with the `nextjs-fastapi-implementor`
and `nextjs-fastapi-reviewer` skills.

## Items

### Item 3.1: G1 - Next.js 16 app with shared layout and config

spec.md section: G1

Scaffold the `frontend/` directory with Next.js 16, React
19, pnpm, shadcn/ui, Tailwind v4, and all config files.
Create: `package.json`, `components.json` (new-york style),
`tsconfig.json`, `next.config.ts`, `vitest.config.ts`,
`eslint.config.mjs`, `postcss.config.cjs`,
`app/globals.css`, `app/layout.tsx` (Inter font,
ThemeProvider, TooltipProvider, Toaster),
`components/theme-provider.tsx`,
`components/ui/toaster.tsx`, `lib/utils.ts` (cn helper),
`frontend/.env.example`, and root `.env.example`. Route
group `(results)/` for results pages. Tests in
`frontend/tests/scaffold.test.ts`. Covering all 4
acceptance tests from G1.

- [ ] implemented
- [ ] reviewed

### Batch 2 (parallel, after item 3.1 is reviewed)

#### Item 3.2: H1 - Dual backend client [parallel with 3.3]

spec.md section: H1

Create `frontend/lib/backend-client.ts` with
`resultsJson<T>(path, schema)`,
`seqmetaJson<T>(path, schema)`, and
`resultsRaw(path)` functions. Define
`BackendRequestError` and `BackendUnavailableError` error
classes. `resultsJson` fetches from
`WA_RESULTS_BACKEND_URL`, validates with Zod schema.
`seqmetaJson` fetches from `WA_SEQMETA_BACKEND_URL`,
throws `BackendUnavailableError` if URL not configured.
Tests in `frontend/tests/backend-client.test.ts`. Covering
all 6 acceptance tests from H1.

- [ ] implemented
- [ ] reviewed

#### Item 3.3: H2 - Zod contract schemas [parallel with 3.2]

spec.md section: H2

Create `frontend/lib/contracts.ts` with Zod schemas for
all Go API response types: `fileEntrySchema`,
`resultSetSchema`, `searchResultSchema`,
`dailyCountSchema`, `pipelineCountSchema`,
`statsResultSchema`, `metaKeysSchema`,
`identifierResultSchema`, `studySchema`,
`studiesSchema`, `sampleSchema`, `samplesSchema`,
`errorSchema`, `healthSchema`. Export inferred TypeScript
types. Tests in `frontend/tests/contracts.test.ts`.
Covering all 7 acceptance tests from H2.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `nextjs-fastapi-reviewer`
skill (review all items in the batch together in a single
review pass).

### Item 3.4: I1 - Health check API route

spec.md section: I1

Depends on items 3.2 (H1) for `resultsJson` and
3.3 (H2) for `statsResultSchema`.

Create `frontend/app/api/health/route.ts` with
`export const dynamic = 'force-dynamic'` and
`export async function GET()`. Calls
`resultsJson('/results/stats', statsResultSchema)`. Returns
`{"status":"healthy"}` with 200 on success,
`{"status":"unhealthy"}` with 503 on failure. Tests in
`frontend/tests/health.test.ts`. Covering all 2
acceptance tests from I1.

- [ ] implemented
- [ ] reviewed
