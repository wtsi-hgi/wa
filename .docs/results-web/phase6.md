# Phase 6: Integration

Ref: [spec.md](spec.md) sections R1, Q2, Q1

## Instructions

Use the `orchestrator` skill to complete this phase,
coordinating subagents with the `nextjs-fastapi-implementor`
and `nextjs-fastapi-reviewer` skills.

## Items

### Item 6.1: R1 - run-dev.sh and dev fixtures

spec.md section: R1

Create `run-dev.sh` at repo root. Parse flags:
`-f`/`--frontend-port` (default 3000),
`-r`/`--results-port` (default 8090),
`-s`/`--seqmeta-port` (default 8091). Steps: compile
Go to `.tmp/wa`, create temp DB, start results server,
seed fixtures from
`.docs/results-web/fixtures/seed.json` via POST to
`/results`, optionally start seqmeta server if
`SAGA_API_TOKEN` is set, run frontend lint and tests,
start Next.js dev server, wait for health checks, clean
up on SIGINT/SIGTERM. Logs to `./logs/`. Create
`.docs/results-web/fixtures/seed.json` with >= 3
Registration objects (varied pipelines, requesters,
metadata including `seqmeta_sampleid`). Create fixture
files in `.docs/results-web/fixtures/files/`:
`report.csv`, `image.png`, `pipeline.nf`, `summary.md`,
`results.html`, `plot.svg`, `config.json`. Covering all
5 acceptance tests from R1.

- [x] implemented
- [x] reviewed

### Batch 2 (parallel, after item 6.1 is reviewed)

#### Item 6.2: Q2 - Vitest API-level integration tests [parallel with 6.3]

spec.md section: Q2

Create `frontend/tests/integration/setup.ts` global
setup: compile Go, create temp SQLite DB, start
`wa results serve` on random port, seed fixtures, set
`WA_RESULTS_BACKEND_URL` env var. Tear down: kill Go
process, remove temp DB. Create
`frontend/tests/integration/api.test.ts` exercising
real Server Actions (`fetchStats`, `searchResults`,
`fetchFiles`, `fetchFileContent`) and the
`/api/file` route handler against the running Go server.
Covering all 7 acceptance tests from Q2.

- [x] implemented
- [x] reviewed

#### Item 6.3: Q1 - Playwright E2E tests [parallel with 6.2]

spec.md section: Q1

Create `frontend/playwright.config.ts` with
`webServer` compiling Go and starting both Go and
Next.js servers via `run-dev.sh`. Create
`frontend/e2e/results.spec.ts` covering critical browser
flows: search flow (add filter, verify table updates),
result detail (click row, verify metadata and file
browser), file browsing (expand folder tree), file
preview (CSV table preview, PNG image rendering).
Covering all 6 acceptance tests from Q1.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `nextjs-fastapi-reviewer`
skill (review all items in the batch together in a single
review pass).
