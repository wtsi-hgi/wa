# Developing wa

## Prerequisites

| Dependency  | Version   | Purpose                                                      |
| ----------- | --------- | ------------------------------------------------------------ |
| **Go**      | 1.25+     | Backend, CLI, all server components                          |
| **Node.js** | 22+       | Frontend dev server and build                                |
| **pnpm**    | 10+       | Frontend package management                                  |
| **SQLite**  | (bundled) | Dev/test database via `modernc.org/sqlite` (pure Go, no CGo) |
| **MySQL**   | 8+        | Production database (optional for dev)                       |

A SAGA API token (`SAGA_API_TOKEN`) is needed for saga/seqmeta features but is
not required for results-only development.

## Repository Layout

```
wa/
├── main.go              # Entrypoint — unified `wa` binary
├── cmd/                 # Cobra command definitions
├── results/             # Results REST API + store
├── saga/                # SAGA API client library
├── seqmeta/             # Sequence metadata cache
├── frontend/            # Next.js web UI
├── run-dev.sh           # Bring-up script used by `make dev`, `make prod`, and Playwright
├── scripts/
│   └── wa-env.sh        # Loads exactly one of .env.test/.env.dev/.env.prod
├── .docs/               # Specs and proposal
│   ├── proposal.md
│   ├── results-rest/spec.md
│   ├── results-web/spec.md
│   ├── saga/spec.md
│   └── seqmeta/spec.md
├── .env.test            # Committed defaults for `make test`
├── .env.dev.example     # Template for `.env.dev` (gitignored, holds SAGA token)
└── .env.prod.example    # Template for `.env.prod` (gitignored, production values)
```

## Quick Start

The repository ships three isolated scenarios — **test**, **dev**, **prod** —
each driven by exactly one env file. The `scripts/wa-env.sh` wrapper loads the
right file per `make` target and refuses cross-scenario contamination, so you
cannot accidentally run tests against your dev database or bring up the dev
stack with production credentials.

```bash
# Install frontend dependencies first
cd frontend && pnpm install && cd ..

# Copy the dev env template (committed) to the gitignored .env.dev and edit it
cp .env.dev.example .env.dev
# Open .env.dev and set SAGA_API_TOKEN, ports, and WA_RESULTS_DB_PATH

# Run the dev stack — persistent DB, real SAGA, NO fixtures
make dev

# Same but also seed demo fixtures (.docs/results-web/fixtures/seed.json)
make dev-fixtures
# (or `make dev FIXTURES=1`)
```

`make dev` builds the `wa` binary, starts the results and seqmeta servers
against the persistent SQLite database at `WA_RESULTS_DB_PATH` (creating the
file and parent directory if missing — never deleting it on shutdown), and
starts the Next.js dev server. Logs go to `logs/`.

The seqmeta server only starts if `SAGA_API_TOKEN` is set in `.env.dev`.

## Make targets

| Target              | Env file loaded   | Purpose                                                                                                  |
| ------------------- | ----------------- | -------------------------------------------------------------------------------------------------------- |
| `make dev`          | `.env.dev`        | Bring up the dev stack with a persistent DB and no fixtures.                                              |
| `make dev FIXTURES=1` | `.env.dev`      | Same as `make dev`, plus seed demo fixtures into the dev DB.                                              |
| `make dev-fixtures` | `.env.dev`        | Alias for `make dev FIXTURES=1`.                                                                          |
| `make prod`         | `.env.prod`       | Bring up the production stack. Refuses to start without `.env.prod` or with any test/dev port set.        |
| `make test`         | `.env.test`       | Run Go + Vitest + Playwright. Always uses ephemeral DBs and refuses an inherited `WA_RESULTS_DB_PATH`.    |
| `make test-go`      | `.env.test`       | Just the Go suite.                                                                                        |
| `make test-frontend`| `.env.test`       | Just Vitest.                                                                                              |
| `make test-e2e`     | `.env.test`       | Just Playwright. Internally drives `run-dev.sh --mode test`.                                              |
| `make lint`         | _(none)_          | `golangci-lint` and `pnpm lint`.                                                                          |
| `make format`       | _(none)_          | `gofmt`, `cleanorder`, and `prettier`.                                                                    |

Defaults applied when an env file does not pin a port:

| Service     | Test | Dev (`.env.dev`)        | Prod (`.env.prod`)       |
| ----------- | ---- | ----------------------- | ------------------------ |
| Frontend    | 3000 | `WA_DEV_FRONTEND_PORT`  | `WA_PROD_FRONTEND_PORT`  |
| Results API | 8090 | `WA_DEV_RESULTS_PORT`   | `WA_PROD_RESULTS_PORT`   |
| Seqmeta API | 8091 | `WA_DEV_SEQMETA_PORT`   | `WA_PROD_SEQMETA_PORT`   |

## Environment files

Only one env file is loaded per scenario. Each file pins `WA_ENV` to the
canonical value for its scenario, and `scripts/wa-env.sh` validates that
`WA_ENV` matches the requested target before running anything. `WA_TEST_*`
ports are exported only in test mode; `WA_DEV_*` only in dev mode; and
`WA_PROD_*` only in prod mode. Backend URLs (`WA_RESULTS_BACKEND_URL`,
`WA_SEQMETA_BACKEND_URL`) are derived from these ports inside `run-dev.sh`,
not hand-edited.

| File              | Tracked? | Loaded by   | Holds                                                                          |
| ----------------- | -------- | ----------- | ------------------------------------------------------------------------------ |
| `.env.test`       | yes      | `make test` | Test ports + `WA_ENV=test`. No DB path. No SAGA token.                          |
| `.env.dev.example`| yes      | _(template)_| Dev ports, dev DB path, `SAGA_API_TOKEN=` placeholder, `WA_ENV=development`.    |
| `.env.dev`        | no       | `make dev`  | Your local copy of the above with real values, including a real SAGA token.     |
| `.env.prod.example`| yes     | _(template)_| Prod ports, prod DB DSN/path placeholder, `WA_ENV=production`.                  |
| `.env.prod`       | no       | `make prod` | Your production values. Required for `make prod`; absent ⇒ clear error.         |

### Safety guarantees

The wrapper and `run-dev.sh` enforce the following:

- `make test` and `make dev` refuse to start if `WA_ENV=production` is
  inherited from the shell.
- `make test` refuses to start if `WA_RESULTS_DB_PATH` is set in the
  environment, so a stray export cannot point tests at a configured dev or
  prod database.
- `make prod` refuses to start when `.env.prod` is missing, when the loaded
  file does not set `WA_ENV=production`, when any `WA_TEST_*` or `WA_DEV_*`
  port var is inherited, or when `--fixtures` is requested.
- `run-dev.sh --mode test` always uses an ephemeral `mktemp` SQLite DB
  beneath `.tmp/` and removes it on shutdown.
- `run-dev.sh --mode dev` and `--mode prod` use the persistent
  `WA_RESULTS_DB_PATH` and never delete the database on shutdown.

### Runtime variables (consumed by the Go binaries and the Next.js runtime)

These are set by the scenario `.env*` files and are not scenario-specific:
`WA_RESULTS_BACKEND_URL`, `WA_SEQMETA_BACKEND_URL`, `WA_RESULTS_DB_PATH`,
`SAGA_API_TOKEN`, `WA_STUDIES_CACHE_TTL_SECONDS`, `WA_DEV_ALLOWED_ORIGINS`.
Backend URLs are derived inside `run-dev.sh` from the active scenario's
`WA_TEST_*` / `WA_DEV_*` / `WA_PROD_*` ports — do not set them by hand.

### Accessing `make dev` from a remote host

Next.js 16 blocks cross-origin requests to dev resources (`/_next/*`, HMR,
RSC payloads) by default. If you browse the dev server from a machine whose
hostname differs from the one running `run-dev.sh` — e.g. opening a browser
on your laptop to `http://<dev-host>:3671` — the browser loads the HTML but
every JS chunk is blocked, React never hydrates, and filters, tabs,
file-preview clicks and similar controls silently do nothing. Only plain
`<a>` links keep working.

`run-dev.sh` prevents this by auto-populating `WA_DEV_ALLOWED_ORIGINS` with:

- `localhost`, `127.0.0.1`
- the current machine's `hostname -f` and `hostname -s`
- anything you already set in `WA_DEV_ALLOWED_ORIGINS`

Set `WA_DEV_ALLOWED_ORIGINS` in `.env.dev` (comma separated) if you need to
add hostnames that are not covered by the defaults. Wildcards follow the
Next.js 16 `allowedDevOrigins` syntax (`*` matches one DNS segment). The
setting is ignored when `WA_ENV=production`.

## Manual Setup

If you prefer to run services individually:

### Go backend

```bash
# Build
go build -o wa .

# Start results server (SQLite for dev)
./wa results serve --port 8090 --db dev.db

# Start seqmeta server (requires SAGA token)
./wa seqmeta serve --port 8091 --db seqmeta.db --token "$SAGA_API_TOKEN"
```

### Frontend

```bash
cd frontend
pnpm install
WA_RESULTS_BACKEND_URL=http://localhost:8090 pnpm dev   # Starts on http://localhost:3000
```

Normally you do not need to run the frontend by hand — `make dev` / `make prod`
start it for you and source the right env file. Use the manual command only
when you are debugging the frontend in isolation against an already-running Go
backend.

Frontend environment variables (read from the process environment):

| Variable                       | Default                 | Description                                                                                                                                            |
| ------------------------------ | ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `WA_RESULTS_BACKEND_URL`       | `http://localhost:8090` | Results API base URL                                                                                                                                   |
| `WA_SEQMETA_BACKEND_URL`       | _(empty)_               | Seqmeta API base URL (omit to disable)                                                                                                                 |
| `WA_STUDIES_CACHE_TTL_SECONDS` | `300`                   | Study list cache lifetime                                                                                                                              |
| `WA_DEV_ALLOWED_ORIGINS`       | _(empty)_               | Dev-only; extra hostnames merged into Next.js `allowedDevOrigins` (see "Accessing `make dev` from a remote host" above). Ignored in production builds. |

## Testing

### Go tests

```bash
# All tests
go test -tags netgo --count 1 ./...

# Specific package
go test -tags netgo --count 1 ./results/...
go test -tags netgo --count 1 ./saga/...
go test -tags netgo --count 1 ./seqmeta/...
go test -tags netgo --count 1 ./cmd/...

# With verbose output
go test -tags netgo --count 1 -v ./results/...
```

Tests use in-memory SQLite — no external database needed. External API calls
(SAGA) are mocked via interfaces.

### Frontend tests

```bash
cd frontend

# Unit tests (Vitest)
pnpm test

# Lint
pnpm lint

# Format check
pnpm format
```

For a single entry point across both halves of the repo, use:

```bash
make lint
make format
make test
```

`make test` now includes Playwright e2e coverage in addition to Go and Vitest.
Install Playwright browsers once before the first run:

```bash
cd frontend && pnpm exec playwright install
```

### End-to-end tests

```bash
cd frontend

# Install Playwright browsers (first time only)
pnpm exec playwright install

# Run e2e tests (requires backend running)
pnpm exec playwright test
```

## Database

### Development

SQLite is used for development and testing. The `results serve` and
`seqmeta serve` commands create tables automatically on first run — no
migrations needed.

### Production (MySQL)

Pass a MySQL DSN to `--db` instead of a file path:

```bash
wa results serve --port 8090 --db 'user:pass@tcp(db-host:3306)/wa_results'
```

The DSN is detected by the presence of `@tcp(` or `@unix(` in the string.
Tables are auto-created on startup, same as SQLite.

Seqmeta uses SQLite only (local watermark state).

## Architecture

The Go backend exposes JSON REST APIs via chi. The Next.js frontend consumes
them through Server Actions (server-to-server calls), so the Go API can live
on a private network — backend URLs are never exposed to the browser.

```
Browser  →  Next.js (Server Actions)  →  Go REST API  →  SQLite / MySQL
                                              ↓
                                         SAGA API (via saga library)
```

### Key patterns

- **Deterministic IDs**: Result set IDs are SHA256 of
  `pipeline_identifier + run_key`. Re-posting the same key upserts.
- **Interface mocking**: All external dependencies (SAGA, database) are behind
  Go interfaces for testable, isolated unit tests.
- **Zod contracts**: The frontend validates API responses with Zod schemas,
  catching backend regressions at the boundary.
- **Change detection**: Seqmeta uses per-entity SHA256 watermarks with
  tombstones for removals. First poll returns all data as "added".

## Deploying to Production

### Build

```bash
# Go binary
go build -o wa .

# Frontend (standalone Node.js app)
cd frontend
pnpm install --frozen-lockfile
pnpm build
```

The `pnpm build` output goes to `frontend/.next/standalone/` — a self-contained
Node.js server.

### Run

Start the Go backend and Next.js frontend as separate processes. Use a process
manager (systemd, supervisor, etc.) to keep them running.

```bash
# Results API (MySQL for production)
wa results serve \
  --port 8090 \
  --db 'user:pass@tcp(db-host:3306)/wa_results' \
  --seqmeta-url http://localhost:8091

# Seqmeta API
wa seqmeta serve \
  --port 8091 \
  --db /var/lib/wa/seqmeta.db \
  --token "$SAGA_API_TOKEN"

# Frontend
cd frontend
WA_RESULTS_BACKEND_URL=http://localhost:8090 \
WA_SEQMETA_BACKEND_URL=http://localhost:8091 \
  node .next/standalone/server.js
```

### Environment variables for production

| Variable                       | Required     | Description                          |
| ------------------------------ | ------------ | ------------------------------------ |
| `SAGA_API_TOKEN`               | For seqmeta  | SAGA API authentication token        |
| `WA_RESULTS_BACKEND_URL`       | For frontend | Results API URL (server-side only)   |
| `WA_SEQMETA_BACKEND_URL`       | For frontend | Seqmeta API URL (server-side only)   |
| `WA_STUDIES_CACHE_TTL_SECONDS` | No           | Study list cache TTL (default: 300)  |
| `PORT`                         | No           | Frontend listen port (default: 3000) |
