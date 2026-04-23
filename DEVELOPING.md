# Developing wa

## Prerequisites

| Dependency | Version | Purpose |
|------------|---------|---------|
| **Go** | 1.25+ | Backend, CLI, all server components |
| **Node.js** | 22+ | Frontend dev server and build |
| **pnpm** | 10+ | Frontend package management |
| **SQLite** | (bundled) | Dev/test database via `modernc.org/sqlite` (pure Go, no CGo) |
| **MySQL** | 8+ | Production database (optional for dev) |

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
├── run-dev.sh           # One-command dev environment
├── .docs/               # Specs and proposal
│   ├── proposal.md
│   ├── results-rest/spec.md
│   ├── results-web/spec.md
│   ├── saga/spec.md
│   └── seqmeta/spec.md
└── .env.example         # Environment variable template
```

## Quick Start

The `run-dev.sh` script builds the Go binary, starts all backend servers with
a temporary SQLite database, seeds test fixtures, waits for the required
seqmeta validation path when enabled, and starts the Next.js dev server:

```bash
# Install frontend dependencies first
cd frontend && pnpm install && cd ..

# Optional: copy the root env template to override run/test ports locally
cp .env.example .env

# Start everything
make run
```

Default ports (configurable via flags):

| Service | Port | Flag |
|---------|------|------|
| Frontend | 3000 | `-f` / `--frontend-port` |
| Results API | 8090 | `-r` / `--results-port` |
| Seqmeta API | 8091 | `-s` / `--seqmeta-port` |

The seqmeta server only starts if `SAGA_API_TOKEN` is set.

Once ready, the script prints URLs for all services. Logs go to `logs/`.

## Makefile workflow

Use the top-level `Makefile` for repo-wide development commands:

```bash
# Optional: load local overrides for run/test ports and tokens
cp .env.example .env

# Start the dev environment
make run

# Lint Go and frontend code
make lint

# Apply formatting for Go and frontend code
make format

# Run Go and frontend tests
make test
```

Available targets:

| Target | Description |
|--------|-------------|
| `make run` | Calls `./run-dev.sh` to build and start the dev environment |
| `make lint` | Runs `golangci-lint run ./...` and `pnpm lint` |
| `make format` | Runs `gofmt`, `cleanorder`, and `prettier --write` |
| `make test` | Runs Go tests, frontend Vitest tests, and frontend Playwright e2e tests |

`run-dev.sh` is intentionally limited to bring-up only. Linting, formatting,
and testing now live behind `make` targets instead of blocking startup.

The Makefile automatically loads root `.env` if it exists. Use it to pin local
ports for `make run` and the Playwright-backed portion of `make test`, and to
provide `SAGA_API_TOKEN` for seqmeta-backed development runs.

SAGA endpoint overrides are flag-only: use `--base-url` with `wa saga inspect`
or `wa seqmeta ...` when you need a non-default SAGA host. There is no
`SAGA_BASE_URL` environment variable.

Root Makefile environment variables:

| Variable | Default | Used by |
|----------|---------|---------|
| `WA_TEST_FRONTEND_PORT` | `3000` | `make run` frontend port and `make test` Playwright frontend port override |
| `WA_TEST_RESULTS_PORT` | `8090` | `make run` results API port and `make test` Playwright results API port override |
| `WA_TEST_SEQMETA_PORT` | `8091` | `make run` seqmeta API port and `make test` Playwright seqmeta API port override |
| `WA_DEV_ALLOWED_ORIGINS` | *(empty)* | Extra comma-separated hostnames to allow cross-origin access to Next.js dev resources (`/_next/*`, HMR). See below. |

### Accessing `make run` from a remote host

Next.js 16 blocks cross-origin requests to dev resources (`/_next/*`, HMR,
RSC payloads) by default. If you browse the dev server from a machine whose
hostname differs from the one running `run-dev.sh` — e.g. opening Chrome on
your laptop to `http://<farm-host>.internal.sanger.ac.uk:3000` — the browser
loads the HTML but every JS chunk is blocked, React never hydrates, and
filters, tabs, file-preview clicks and similar controls silently do nothing.
Only plain `<a>` links keep working.

`run-dev.sh` prevents this for typical Sanger setups by auto-populating
`WA_DEV_ALLOWED_ORIGINS` with:

- `localhost`, `127.0.0.1`
- `*.sanger.ac.uk`, `*.internal.sanger.ac.uk`
- the current machine's `hostname -f` and `hostname -s`
- anything you already set in `WA_DEV_ALLOWED_ORIGINS`

Set `WA_DEV_ALLOWED_ORIGINS` in the root `.env` (comma separated) if you need
to add hostnames that are not covered by the defaults. Wildcards follow the
Next.js 16 `allowedDevOrigins` syntax (`*` matches one DNS segment). The
setting is ignored when `NODE_ENV=production`.

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
cp .env.example .env.local    # Edit if using non-default backend URLs
pnpm install
pnpm dev                      # Starts on http://localhost:3000
```

For `make run` and Playwright-backed `make test`, set `WA_TEST_*_PORT` values
in the repo root `.env` instead.

Frontend environment variables (set in `.env.local` or environment):

| Variable | Default | Description |
|----------|---------|-------------|
| `WA_RESULTS_BACKEND_URL` | `http://localhost:8090` | Results API base URL |
| `WA_SEQMETA_BACKEND_URL` | *(empty)* | Seqmeta API base URL (omit to disable) |
| `WA_STUDIES_CACHE_TTL_SECONDS` | `300` | Study list cache lifetime |
| `WA_DEV_ALLOWED_ORIGINS` | *(empty)* | Dev-only; extra hostnames merged into Next.js `allowedDevOrigins` (see "Accessing `make run` from a remote host" above). Ignored in production builds. |

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

| Variable | Required | Description |
|----------|----------|-------------|
| `SAGA_API_TOKEN` | For seqmeta | SAGA API authentication token |
| `WA_RESULTS_BACKEND_URL` | For frontend | Results API URL (server-side only) |
| `WA_SEQMETA_BACKEND_URL` | For frontend | Seqmeta API URL (server-side only) |
| `WA_STUDIES_CACHE_TTL_SECONDS` | No | Study list cache TTL (default: 300) |
| `PORT` | No | Frontend listen port (default: 3000) |
