# Developing wa

## Prerequisites

| Dependency  | Version   | Purpose                                                      |
| ----------- | --------- | ------------------------------------------------------------ |
| **Go**      | 1.25+     | Backend, CLI, all server components                          |
| **Node.js** | 22+       | Frontend dev server and build                                |
| **pnpm**    | 10+       | Frontend package management                                  |
| **SQLite**  | (bundled) | Dev/test database via `modernc.org/sqlite` (pure Go, no CGo) |
| **MySQL**   | 8+        | Production database (optional for dev)                       |

MLWH settings (`WA_MLWH_DSN`, `WA_MLWH_PASSWORD`, and cache settings) are only
required when you want `wa mlwh sync` to read real MLWH data or a local/dev
server to use that cache.

## Add a new MLWH query

1. Add any required schema column and index in both `mlwh/cache_schema/sqlite/` and `mlwh/cache_schema/mysql/`.
2. Add one `Client` method.
3. Add one `Queryer` member.
4. Add one `Registry` entry.

Use the read-path audit in `.docs/mlwh-sync/spec.md` for every served column:
it must be traceable to an indexed read path. Keep `TestParseSchemaShapeParity`
passing so SQLite and MySQL table, column, and index sets stay aligned.

## Repository Layout

```
wa/
├── main.go              # Entrypoint — unified `wa` binary
├── cmd/                 # Cobra command definitions
├── results/             # Results REST API + store
├── mlwh/                # MLWH cache, query surface, and current-state server
├── mlwhdiff/            # MLWH change detection store and API
├── frontend/            # Next.js web UI
├── run-dev.sh           # Bring-up script used by `make dev`, `make prod`, and Playwright
├── .docs/               # Specs and proposal
│   ├── proposal.md
│   ├── mlwh-overhaul/spec.md
│   ├── results-rest/spec.md
│   └── results-web/spec.md
├── .env.test            # Committed defaults for `make test`
├── .env.development     # Committed development defaults
└── .env.production      # Committed production defaults
```

## Quick Start

The repository ships three isolated scenarios — **test**, **development**,
**production** — using dotenv-style files. `make` targets source the matching
scenario files directly, and `wa --env <name> ...` loads the same file set in
the binary before Cobra builds command defaults.

```bash
# Install frontend dependencies first
cd frontend && pnpm install && cd ..

# Create a machine-local development override and add any secrets there
cp .env.development .env.development.local
# Open .env.development.local and set WA_MLWH_DSN or any per-machine overrides

# Run the dev stack - persistent DB, MLWH-backed query server, NO fixtures
make dev

# Same but also seed demo fixtures (.docs/results-web/fixtures/seed.json)
make dev-fixtures
# (or `make dev FIXTURES=1`)
```

`make dev` builds the `wa` binary, starts the results server against the
persistent SQLite database at `WA_RESULTS_DB_PATH`, starts the MLWH query server
against `WA_MLWH_CACHE_PATH`, and starts the Next.js dev server. SQLite files
and parent directories are created if missing and never deleted on shutdown.
Logs go to `logs/`.

Development mode requires an MLWH query source: `WA_MLWH_SERVER_URL` for an
existing server, `WA_MLWH_CACHE_PATH` for a local cache-backed `wa mlwh serve`,
or `WA_MLWH_DSN` / `WA_RUN_DEV_SEQMETA_CMD` for operator-managed local sources.
`wa mlwh serve` is cache-only, so run `wa mlwh sync` when you need to seed or
refresh the cache explicitly. Normal CLI users should query the server with
`wa mlwh info --server ...` or `WA_MLWH_SERVER_URL`; they do not need local MLWH
database or cache credentials.

## Make targets

| Target                | Env files loaded                                                   | Purpose                                                                                                                                                        |
| --------------------- | ------------------------------------------------------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `make dev`            | `.env`, `.env.development`, `.env.local`, `.env.development.local` | Bring up the development stack with a persistent DB and no fixtures.                                                                                           |
| `make dev FIXTURES=1` | `.env`, `.env.development`, `.env.local`, `.env.development.local` | Same as `make dev`, plus seed demo fixtures into the development DB.                                                                                           |
| `make dev-fixtures`   | `.env`, `.env.development`, `.env.local`, `.env.development.local` | Alias for `make dev FIXTURES=1`.                                                                                                                               |
| `make prod`           | `.env`, `.env.production`, `.env.local`, `.env.production.local`   | Bring up the production stack. Refuses inherited test/development ports and still requires `WA_ENV=production` plus a real `WA_RESULTS_DB_PATH` after loading. |
| `make test`           | `.env`, `.env.test`, `.env.test.local`                             | Run Go + Vitest + Playwright. Always uses ephemeral DBs and refuses an inherited `WA_RESULTS_DB_PATH`.                                                         |
| `make test-go`        | `.env`, `.env.test`, `.env.test.local`                             | Just the Go suite.                                                                                                                                             |
| `make test-frontend`  | `.env`, `.env.test`, `.env.test.local`                             | Just Vitest.                                                                                                                                                   |
| `make test-e2e`       | `.env`, `.env.test`, `.env.test.local`                             | Just Playwright. Internally drives `run-dev.sh --mode test`.                                                                                                   |
| `make lint`           | _(none)_                                                           | `golangci-lint` and `pnpm lint`.                                                                                                                               |
| `make format`         | _(none)_                                                           | `gofmt`, `cleanorder`, and `prettier`.                                                                                                                         |

Defaults applied when an env file does not pin a port:

| Service     | Test | Development (`.env.development`) | Production (`.env.production`) |
| ----------- | ---- | -------------------------------- | ------------------------------ |
| Frontend    | 3000 | `WA_DEV_FRONTEND_PORT`           | `WA_PROD_FRONTEND_PORT`        |
| Results API | 8090 | `WA_DEV_RESULTS_PORT`            | `WA_PROD_RESULTS_PORT`         |
| MLWH API    | 8091 | `WA_DEV_SEQMETA_PORT`            | `WA_PROD_SEQMETA_PORT`         |

The MLWH API port variables still carry the historical `SEQMETA` name for
scenario-file compatibility; the managed service is `wa mlwh serve`.

Results API bind hosts are separate from client URLs. Development and
production can set `WA_DEV_RESULTS_HOST` or `WA_PROD_RESULTS_HOST` when
`results serve` should listen beyond loopback, for example `0.0.0.0`. Normal
remote CLI users should export one full `WA_RESULTS_SERVER_URL` instead, using
the Results API port rather than the frontend port.

## Environment files

Each scenario keeps `WA_ENV` pinned to the canonical value for that runtime.
`WA_TEST_*` ports are exported only in test mode; `WA_DEV_*` only in
development mode; and `WA_PROD_*` only in production mode. `wa results ...`
derives its default `--server` URL from `WA_RESULTS_SERVER_URL`,
`WA_RESULTS_BACKEND_URL`, or the active scenario's results port on loopback,
while `run-dev.sh` derives the frontend backend URLs from the scenario ports.

| File                     | Tracked? | Loaded by                               | Holds                                                                                        |
| ------------------------ | -------- | --------------------------------------- | -------------------------------------------------------------------------------------------- |
| `.env.test`              | yes      | `make test` / `wa --env test ...`       | Test ports + `WA_ENV=test`. No DB path. No committed MLWH credentials.                       |
| `.env.test.local`        | no       | optional test override                  | Personal test-only overrides.                                                                |
| `.env.development`       | yes      | `make dev` / `wa --env development ...` | Development ports/host placeholders, dev DB path, blank MLWH settings, `WA_ENV=development`. |
| `.env.development.local` | no       | local development override              | Your local development secrets and machine-specific overrides, including real MLWH settings. |
| `.env.production`        | yes      | `make prod` / `wa --env production ...` | Production ports/host placeholders, blank DB settings, `WA_ENV=production`.                  |
| `.env.production.local`  | no       | local production override               | Deployment-specific production secrets and DB settings.                                      |

### Safety guarantees

The `make` recipes, `wa` startup path, and `run-dev.sh` enforce the following:

- `make test` and `make dev` refuse to start if `WA_ENV=production` is
  inherited from the shell.
- `make test` refuses to start if `WA_RESULTS_DB_PATH` is set in the
  environment, so a stray export cannot point tests at a configured dev or
  prod database.
- `make prod` refuses inherited `WA_TEST_*` or `WA_DEV_*` ports and results
  host variables, and `run-dev.sh --mode prod` still requires
  `WA_ENV=production` plus a real `WA_RESULTS_DB_PATH` after loading.
- `run-dev.sh --mode test` always uses an ephemeral `mktemp` SQLite DB
  beneath `.tmp/` and removes it on shutdown.
- `run-dev.sh --mode dev` and `--mode prod` use the persistent
  `WA_RESULTS_DB_PATH` and never delete the database on shutdown.
- `make test` always cleans up its `.tmp/` artefacts (the built `wa`
  binary, the Playwright port-allocation cache, and any stray ephemeral
  SQLite DBs) on completion, regardless of whether the sub-suites passed
  or failed. Run `make clean-test-tmp` to perform the same cleanup
  manually.
- Live MLWH integration tests are skipped unless `WA_LIVE_MLWH_TESTS=1` is
  set. Tests that run a real cold `Client.Sync` also require
  `MLWH_SYNC_PERF_TEST=1`, `WA_MLWH_DSN`, and `WA_MLWH_PASSWORD`; they sync to
  a temporary cache under `t.TempDir()`. Do not put these opt-ins in
  `.env.test` or `.env.test.local` for ordinary `make test` runs.

### Runtime variables

The scenario `.env*` files supply `WA_RESULTS_DB_PATH`,
`WA_RESULTS_DB_PASSWORD`, `WA_RESULTS_SERVER_CERT`, `WA_RESULTS_SERVER_KEY`,
`WA_MLWH_DSN`, `WA_MLWH_PASSWORD`, `WA_MLWH_CACHE_PATH`,
`WA_MLWH_CACHE_PASSWORD`, `WA_STUDIES_CACHE_TTL_SECONDS`,
`WA_DEV_ALLOWED_ORIGINS`, and the relevant
`WA_*_RESULTS_PORT` / `WA_*_SEQMETA_PORT` / `WA_*_FRONTEND_PORT` variables.
`wa results ...` chooses its default `--server` in this order: explicit
`--server`, `WA_RESULTS_SERVER_URL` as a full client URL,
`WA_RESULTS_BACKEND_URL` as a lower-precedence compatibility URL, then
`WA_ENV` plus the active `WA_*_RESULTS_PORT` on `127.0.0.1`. If no active port
is set, it falls back to `https://localhost:8080`.
`WA_DEV_RESULTS_HOST` and `WA_PROD_RESULTS_HOST` are server bind hosts used by
`results serve` when `--url` is unset; they are not client dial hosts.
`wa results --cert` / `wa results serve --key` default from the TLS env vars.
`run-dev.sh` uses the scenario ports to export `WA_RESULTS_BACKEND_URL` and
`WA_MLWH_BACKEND_URL` for the frontend; `wa mlwh info` uses
`WA_MLWH_SERVER_URL` first and keeps `WA_MLWH_BACKEND_URL` only as a
lower-precedence compatibility default.
The MLWH lookup flags on `wa results register` are resolved by the results
server. Remote CLI users do not need `WA_MLWH_CACHE_PATH`,
`WA_MLWH_CACHE_PASSWORD`, or MLWH cache credentials on their machine.

### Using the results CLI from another machine

Normal users only need the full API URL:

```bash
export WA_RESULTS_SERVER_URL=https://dev-host.example.org:3672
wa results search --pipeline nf-pipe
wa results register /shared/results/run42 --user alice --workflow nf-pipe --unique run42 --sample SANG001
```

Use the `Results` / `Results public` line printed by `make dev` for this URL.
The frontend URL is for the browser UI and is not a results CLI endpoint.
With the default development ports, that means remote CLI users should use
`https://<dev-host>:3672`, not `https://<dev-host>:3671`.

If a remote user can log in to the web UI but `wa results register` prompts for
`Password:` and then returns `authentication failed`, first check the CLI target:

```bash
env | grep '^WA_RESULTS'
```

Unset any stale `WA_RESULTS_BACKEND_URL` pointing at the frontend and use
`WA_RESULTS_SERVER_URL` for the Results API instead. The frontend can log in
successfully through `/api/auth/login` even when the CLI is pointed at the wrong
service, because the CLI posts directly to `/rest/v1/jwt`.

If the results API uses the self-signed development certificate created by
`run-dev.sh`, the user also needs to trust that certificate with `--cert` or
`WA_RESULTS_SERVER_CERT=/path/to/wa-dev-cert.pem`. The generated certificate is
valid for loopback, the machine's `hostname -f` and `hostname -s`, and the
hostname from `WA_RESULTS_SERVER_URL`; `run-dev.sh` regenerates stale
localhost-only certs when these SANs are missing.

For operators, scenario host vars are bind hosts for `results serve`. To let
another machine connect to a development API without duplicating the port,
set the development environment, bind host, and port once:

```bash
export WA_ENV=development
export WA_DEV_RESULTS_HOST=0.0.0.0
export WA_DEV_RESULTS_PORT=3672
make dev
```

The equivalent production vars are `WA_ENV=production`,
`WA_PROD_RESULTS_HOST=0.0.0.0`, and `WA_PROD_RESULTS_PORT=8090`. An explicit
`results serve --url host:port` still overrides the bind host and port.

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

Set `WA_DEV_ALLOWED_ORIGINS` in `.env.development.local` (comma separated) if you need to
add hostnames that are not covered by the defaults. Wildcards follow the
Next.js 16 `allowedDevOrigins` syntax (`*` matches one DNS segment). The
setting is ignored when `WA_ENV=production`.

## Manual Setup

If you prefer to run services individually:

### Go backend

Run long-lived `serve` commands in separate terminals or under a process
manager.

```bash
# Build
go build -o wa .

# Security defaults for manual `results serve` examples below. `--cert`,
# `--key`, `--ldap_server`, and `--ldap_dn` default from these env vars.
# `--server-token` defaults to .wa-results-server.token in the auth token dir.
mkdir -p .tmp
openssl req -x509 -newkey rsa:2048 -nodes -days 30 \
  -keyout .tmp/wa-dev-key.pem \
  -out .tmp/wa-dev-cert.pem \
  -subj '/CN=localhost' \
  -addext 'subjectAltName=DNS:localhost,IP:127.0.0.1'
chmod 600 .tmp/wa-dev-key.pem
export WA_RESULTS_SERVER_CERT=.tmp/wa-dev-cert.pem
export WA_RESULTS_SERVER_KEY=.tmp/wa-dev-key.pem
export WA_RESULTS_LDAP_SERVER=ldap.example.org
export WA_RESULTS_LDAP_DN='uid=%s,ou=people,dc=example,dc=org'

# Start MLWH query server (requires a synced MLWH cache)
export WA_MLWH_DSN='mlwh_user@tcp(db-host:3306)/mlwarehouse'
export WA_MLWH_CACHE_PATH=.tmp/mlwh-cache.sqlite
./wa mlwh sync
./wa mlwh serve --port 8091

# Start results server (SQLite for dev)
./wa results serve --port 8090 --db dev.db --mlwh-server-url http://localhost:8091

# Run the results CLI against the development stack described by .env.development
./wa --env development results search --pipeline nf-pipe

# Start results server (MySQL without exposing the password on argv)
export WA_RESULTS_DB_PATH='user@tcp(db-host:3306)/wa_results'
export WA_RESULTS_DB_PASSWORD='super-secret'
./wa results serve --port 8090 --mlwh-server-url http://localhost:8091
```

### Frontend

```bash
cd frontend
pnpm install
WA_RESULTS_BACKEND_URL=https://localhost:8090 \
WA_RESULTS_BACKEND_CA_CERT=../.tmp/wa-dev-cert.pem \
WA_MLWH_BACKEND_URL=http://localhost:8091 \
  pnpm dev   # Starts on http://localhost:3000
```

Normally you do not need to run the frontend by hand — `make dev` / `make prod`
start it for you with the matching env files already loaded. Use the manual command only
when you are debugging the frontend in isolation against an already-running Go
backend.

If the results backend certificate is not trusted by the system, also export
`WA_RESULTS_BACKEND_CA_CERT=/path/to/results-ca-or-cert.pem` before starting the
frontend.

Frontend environment variables (read from the process environment):

| Variable                       | Default                  | Description                                                                                                                                            |
| ------------------------------ | ------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `WA_RESULTS_BACKEND_URL`       | `https://localhost:8090` | Results API base URL                                                                                                                                   |
| `WA_RESULTS_BACKEND_CA_CERT`   | _(empty)_                | Optional PEM certificate or CA bundle for non-system-trusted results backend TLS                                                                       |
| `WA_MLWH_BACKEND_URL`          | _(empty)_                | MLWH query API base URL (omit to disable MLWH-backed enrichment/study lookup in the frontend)                                                          |
| `WA_STUDIES_CACHE_TTL_SECONDS` | `300`                    | Study list cache lifetime                                                                                                                              |
| `WA_DEV_ALLOWED_ORIGINS`       | _(empty)_                | Dev-only; extra hostnames merged into Next.js `allowedDevOrigins` (see "Accessing `make dev` from a remote host" above). Ignored in production builds. |

## Testing

### Go tests

```bash
# All tests
go test -tags netgo --count 1 ./...

# Specific package
go test -tags netgo --count 1 ./results/...
go test -tags netgo --count 1 ./mlwhdiff/...
go test -tags netgo --count 1 ./cmd/...

# With verbose output
go test -tags netgo --count 1 -v ./results/...
```

Tests use in-memory SQLite or temporary on-disk SQLite caches by default — no
external database needed. External API calls (MLWH and related services) are
mocked via interfaces.

Live MLWH-backed Go integration tests are opt-in:

```bash
WA_LIVE_MLWH_TESTS=1 \
WA_MLWH_DSN='mlwh_user@tcp(host:3306)/mlwarehouse' \
WA_MLWH_PASSWORD='...' \
go test -tags netgo --count 1 ./mlwh -run TestLiveMLWH
```

The cold-sync performance test is intentionally a second opt-in because it runs
`Client.Sync` against real MLWH:

```bash
WA_LIVE_MLWH_TESTS=1 MLWH_SYNC_PERF_TEST=1 \
WA_MLWH_DSN='mlwh_user@tcp(host:3306)/mlwarehouse' \
WA_MLWH_PASSWORD='...' \
go test -tags netgo --count 1 ./mlwh -run TestLiveMLWHSyncPerTableColdSyncBudget
```

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

# Run e2e tests
pnpm exec playwright test
```

Playwright coverage is hermetic. `make test-e2e` and `pnpm exec playwright test`
start the app through `run-dev.sh --mode test`, which refuses `WA_MLWH_DSN` and
uses a stub MLWH backend instead of a real MLWH connection. Missing MLWH
settings in `.env.development.local` do not change Playwright skip counts.

There is currently no Playwright suite that exercises a real MLWH database.
Live MLWH-backed integration coverage belongs in the Go integration tests, not
the browser harness.

## Database

### Development

SQLite is used for development and testing. The `results serve`, `mlwh sync`,
and `mlwhdiff serve` commands create their tables automatically on first run -
no migrations needed.

### Production (MySQL)

Use either an env-supplied full DSN or a passwordless `--db` value plus
`WA_RESULTS_DB_PASSWORD`:

```bash
export WA_RESULTS_SERVER_CERT=.tmp/wa-dev-cert.pem
export WA_RESULTS_SERVER_KEY=.tmp/wa-dev-key.pem
export WA_RESULTS_LDAP_SERVER=ldap.example.org
export WA_RESULTS_LDAP_DN='uid=%s,ou=people,dc=example,dc=org'

export WA_RESULTS_DB_PATH='user:pass@tcp(db-host:3306)/wa_results'
wa results serve --port 8090

export WA_RESULTS_DB_PASSWORD='super-secret'
wa results serve --port 8090 --db 'user@tcp(db-host:3306)/wa_results'
```

Password-bearing DSNs are rejected on the command line.
The DSN is detected by the presence of `@tcp(` or `@unix(` in the string.
Tables are auto-created on startup, same as SQLite.

`mlwhdiff` persists local watermark state in SQLite and reads sequencing data
via the MLWH-backed query surface.

## Architecture

The Go backend exposes JSON REST APIs via gin/gas-backed handlers. The Next.js
frontend consumes them through Server Actions (server-to-server calls), so the
Go API can live on a private network — backend URLs are never exposed to the
browser.

```
Browser  →  Next.js (Server Actions)  →  Go REST API  →  SQLite / MySQL caches
                                              ↓
                                         MLWH replica
```

### Key patterns

- **Deterministic IDs**: Result set IDs are SHA256 of
  `pipeline_identifier + run_key`. Re-posting the same key upserts.
- **Interface mocking**: All external dependencies (MLWH, database) are behind
  Go interfaces for testable, isolated unit tests.
- **Zod contracts**: The frontend validates API responses with Zod schemas,
  catching backend regressions at the boundary.
- **Change detection**: `mlwhdiff` uses per-entity SHA256 watermarks with
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
# MLWH current-state API
export WA_MLWH_DSN='user@tcp(mlwh-host:3306)/warehouse'
export WA_MLWH_PASSWORD='super-secret'
export WA_MLWH_CACHE_PATH=/var/lib/wa/mlwh-cache.sqlite
wa mlwh sync
wa mlwh serve --port 8091

# Results API (MySQL for production)
export WA_ENV=production
export WA_PROD_RESULTS_HOST=0.0.0.0
export WA_PROD_RESULTS_PORT=8090
export WA_RESULTS_DB_PATH='user@tcp(db-host:3306)/wa_results'
export WA_RESULTS_DB_PASSWORD='super-secret'
export WA_RESULTS_SERVER_CERT=/etc/wa/results.crt
export WA_RESULTS_SERVER_KEY=/etc/wa/results.key
export WA_RESULTS_LDAP_SERVER=ldap.example.org
export WA_RESULTS_LDAP_DN='uid=%s,ou=people,dc=example,dc=org'
wa results serve \
  --port "$WA_PROD_RESULTS_PORT" \
  --mlwh-server-url http://localhost:8091

# Optional MLWH change-tracking API
export WA_MLWH_SERVER_URL=http://localhost:8091
wa mlwhdiff serve \
  --port 8092 \
  --db /var/lib/wa/mlwhdiff.db

# Frontend
cd frontend
WA_RESULTS_BACKEND_URL=https://localhost:8090 \
WA_MLWH_BACKEND_URL=http://localhost:8091 \
  node .next/standalone/server.js
```

Set `WA_RESULTS_BACKEND_CA_CERT=/path/to/results-ca-or-cert.pem` for the
frontend when the results API certificate is self-signed or otherwise not
trusted by the system.

### Environment variables for production

| Variable                       | Required                                | Description                                                                                          |
| ------------------------------ | --------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `WA_ENV`                       | For scenario CLI                        | Use `production` when loading `.env.production` or exporting `WA_PROD_*` defaults                    |
| `WA_PROD_RESULTS_PORT`         | For server/CLI                          | Production results API port used by `make prod`, `results serve`, and same-machine CLI defaults      |
| `WA_PROD_RESULTS_HOST`         | For server                              | Results API bind host for `make prod` / `results serve`; set `0.0.0.0` for remote connections        |
| `WA_RESULTS_SERVER_URL`        | For remote CLI                          | Full Results API URL equivalent to `--server`, for example `https://results.example.org:8090`        |
| `WA_RESULTS_DB_PATH`           | For results                             | SQLite path, full DSN, or passwordless MySQL DSN                                                     |
| `WA_RESULTS_DB_PASSWORD`       | Optional                                | MySQL password paired with a passwordless DSN                                                        |
| `WA_MLWH_DSN`                  | For `wa mlwh sync`                      | Passwordless MLWH DSN                                                                                |
| `WA_MLWH_PASSWORD`             | Optional                                | MLWH password paired with a passwordless DSN                                                         |
| `WA_MLWH_CACHE_PATH`           | For `wa mlwh serve` and local operators | SQLite path or passwordless MySQL cache DSN used by server-side MLWH lookups                         |
| `WA_MLWH_CACHE_PASSWORD`       | Optional                                | MLWH cache MySQL password                                                                            |
| `WA_MLWH_SERVER_URL`           | For results/mlwhdiff/mlwh info clients  | Remote `wa mlwh serve` URL, equivalent to `--mlwh-server-url` / `wa mlwh info --server`              |
| `WA_RESULTS_SERVER_CERT`       | For results                             | TLS certificate path and CLI trust root; `run-dev.sh` resolves relative paths from the repo root     |
| `WA_RESULTS_SERVER_KEY`        | For results                             | TLS private key path for `wa results serve`; `run-dev.sh` resolves relative paths from the repo root |
| `WA_RESULTS_BACKEND_URL`       | For frontend                            | Results API URL for the frontend; kept as a lower-precedence CLI default for compatibility           |
| `WA_RESULTS_BACKEND_CA_CERT`   | Optional                                | PEM certificate or CA bundle for frontend trust when results backend TLS is not system-trusted       |
| `WA_MLWH_BACKEND_URL`          | For frontend / compatibility            | MLWH query API URL for the frontend and lower-precedence `wa mlwh info` default                      |
| `WA_STUDIES_CACHE_TTL_SECONDS` | No                                      | Study list cache TTL (default: 300)                                                                  |
| `PORT`                         | No                                      | Frontend listen port (default: 3000)                                                                 |
