# Developing wa

## Prerequisites

| Dependency  | Version   | Purpose                                                      |
| ----------- | --------- | ------------------------------------------------------------ |
| **Go**      | 1.27+     | Backend, CLI, all server components                          |
| **Node.js** | 22.18+    | Frontend dev server and build                                |
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

## Upstream MLWH schema changes

The MLWH team announces Unified Warehouse schema releases ahead of time. Ship
any wa change that a release needs before the release date, in a form that
works on both the old and the new schema, so nobody has to deploy wa at the
moment upstream changes. `.docs/upstream/` records past assessments; the
`mlwh-upstream-change` skill walks an agent through one.

### How wa reads the source

Only `wa mlwh sync` reads rows from the upstream MLWH. `wa mlwh check-source`
connects to it too but only PREPAREs the sync queries. Every other command
reads the cache.

- Every source query names its columns, so a new upstream column changes
  nothing until wa selects it. A dropped or renamed column that wa selects
  breaks the sync of that table.
- `AllSyncSourceQueries()` in `mlwh/sync.go` lists every source query. A new
  source query must be added there; `TestSyncSourceSchemaMatchesRealMLWH` and
  `wa mlwh check-source` prepare everything it lists.
- wa keeps only Sequencescape studies. Each source query that reads `study`
  filters on `id_lims = 'SQSCP'`, `syncStudyTable` drops any other row, and
  each cache read of `study_mirror` filters on `id_lims = 'SQSCP'` again. A
  study from another LIMS that shares a uuid or `id_study_lims` with a
  Sequencescape study therefore never reaches an answer. Sample sync keeps
  only `SQSCP` samples, but product and iRODS queries filter on the study
  alone, so a non-SQSCP sample's data in a Sequencescape study (today,
  Traction PacBio samples) appears in study-level answers with empty sample
  identity. This is intended. What this scope excludes, and what adding a
  LIMS involves, is in
  [`.docs/scope/lims-coverage.md`](.docs/scope/lims-coverage.md).
- The cache relies on no upstream uniqueness beyond the primary and `UNIQUE`
  keys in `mlwh/cache_schema/`.
- When the source schema differs between servers, sync probes and falls back
  instead of failing. The study query retries with the columns a
  `SELECT * ... LIMIT 0` probe finds, mapping upstream aliases onto the cache
  name (`studySourceColumnSpecs`, from `.docs/bugfixes/260509-2.md`), and the
  warm product queries fall back to their legacy variants when the server
  rejects `JSON_TABLE` (`isUnsupportedCompositionQueryError`).

### Assess an announcement

For each announced change:

1. Find whether wa reads the table or column: search `mlwh/*.go` for it, and
   check the queries in `AllSyncSourceQueries()` and the cache schema.
2. Check uniqueness: does any join, `LIMIT 1` lookup or cache key assume the
   old key or index?
3. Check filters: can new rows (another LIMS, a new platform, a new state)
   pass a filter that used to exclude them?
4. Classify it as no impact, compatible both ways, breaks sync, or gives wrong
   results.

### Make a change ship before the release

- Renamed or dropped column: probe the source and fall back, like
  `studySourceColumnSpecs`, so the same query works on both shapes.
- New column wa wants: select it only when a probe finds it, and leave the
  feature off until it does. Never make sync require a column that does not
  exist yet.
- Key or filter change: add the filter in the source query and in the cache
  reads, as the `id_lims = 'SQSCP'` filters are.

### Test

- Hermetic: seed both schema shapes on the SQLite source fixture
  (`openRealMLWHSchemaSource`, then `ALTER TABLE` or `CREATE INDEX` for the new
  shape) and on the cache, with a MySQL cache twin when the read SQL differs by
  dialect. `mlwh/upstream_schema_change_test.go` is an example. Show by
  mutation that each new filter or fallback is load-bearing.
- Live: `make test-go` loads `WA_MLWH_DSN` and `WA_MLWH_PASSWORD` from
  `.env.development.local` and runs the real-source tests, and
  `wa mlwh check-source` prepares every source query against the configured
  MLWH without executing any of them.

```bash
wa --env development mlwh check-source
wa --env development mlwh check-source --json
```

### Release and deploy

Release wa as usual, then restart the Kubernetes MLWH API (deployment
`mlwh-api` in namespace `apps` on the HGI apps cluster) so it pulls the new
image, then update the farm stack:

```bash
kubectl -n apps rollout restart deploy/mlwh-api
# on the farm host
git pull && make prod-restart
```

After the upstream release, check the production source from inside the pod,
then watch `/freshness` until every table is `ok` and has synced since the
release ([Monitoring MLWH sync](#monitoring-mlwh-sync)):

```bash
kubectl -n apps exec deploy/mlwh-api -- wa --env production mlwh check-source
WA_MLWH_SERVER_URL=https://apps.hgi.sanger.ac.uk/mlwh-api wa mlwh freshness
```

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
├── prod-ctl.sh          # Background lifecycle for `make prod-start/stop/status/restart`
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
| `make build`          | _(none)_                                                           | Build `./wa` with `git describe --tags --always --long --dirty` stamped as the version `wa version` prints.                                                    |
| `make install`        | _(none)_                                                           | Install `wa` into `$GOBIN` (or `$GOPATH/bin`) with the same version stamp.                                                                                     |
| `make dev`            | `.env`, `.env.development`, `.env.local`, `.env.development.local` | Bring up the development stack with a persistent DB and no fixtures.                                                                                           |
| `make dev FIXTURES=1` | `.env`, `.env.development`, `.env.local`, `.env.development.local` | Same as `make dev`, plus seed demo fixtures into the development DB.                                                                                           |
| `make dev-fixtures`   | `.env`, `.env.development`, `.env.local`, `.env.development.local` | Alias for `make dev FIXTURES=1`.                                                                                                                               |
| `make prod`           | `.env`, `.env.production`, `.env.local`, `.env.production.local`   | Bring up the production stack. Refuses inherited test/development ports and still requires `WA_ENV=production` plus a real `WA_RESULTS_DB_PATH` after loading. |
| `make prod-start`     | `.env`, `.env.production`, `.env.local`, `.env.production.local`   | Start `make prod` in the background unless it is healthy or starting. Quiet when there is nothing to do, so cron can run it. See "Production on a farm host".  |
| `make prod-stop`      | `.env`, `.env.production`, `.env.local`, `.env.production.local`   | Stop the background production stack.                                                                                                                          |
| `make prod-status`    | `.env`, `.env.production`, `.env.local`, `.env.production.local`   | Print the URLs and exit 0 when the background stack is healthy; exit non-zero otherwise.                                                                       |
| `make prod-restart`   | `.env`, `.env.production`, `.env.local`, `.env.production.local`   | Stop, then start, the background production stack.                                                                                                             |
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

Results and MLWH API bind hosts are separate from client URLs. Development and
production can set `WA_DEV_RESULTS_HOST` / `WA_PROD_RESULTS_HOST` for
`results serve`, and `WA_DEV_SEQMETA_HOST` / `WA_PROD_SEQMETA_HOST` for
`mlwh serve`, when a service should listen beyond loopback, for example
`0.0.0.0`. Normal remote CLI users should export the full client URL instead:
`WA_RESULTS_SERVER_URL` for results commands and `WA_MLWH_SERVER_URL` for
`wa mlwh info` / `mlwhdiff`.

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
- `make prod` refuses inherited `WA_TEST_*` or `WA_DEV_*` ports and bind
  host variables, and `run-dev.sh --mode prod` still requires
  `WA_ENV=production` plus a real `WA_RESULTS_DB_PATH` after loading.
- `run-dev.sh --mode test` always uses an ephemeral `mktemp` SQLite DB
  beneath `.tmp/` and removes it on shutdown.
- The Go `run-dev.sh` tests set `WA_RUN_DEV_TMP_DIR` to a per-test
  directory, so run-dev keeps its dev TLS key and certificate, test state,
  built binaries, `wa` link, and ephemeral DBs there instead of in `.tmp/`.
  They also set `WA_RUN_DEV_LOG_DIR` to a `logs` directory inside it, so the
  service logs stay out of `logs/`. They also give each test its own
  `XDG_STATE_HOME`, so a real results server's token, which go-authserver
  deletes on shutdown, never touches the caller's state directory or `$HOME`.
  They leave a stack running from the same checkout alone.
- Playwright does the same for its `run-dev.sh` stack with `.tmp/e2e/`,
  which also holds its port-allocation cache and the e2e helpers' CLI state.
- `run-dev.sh --mode dev` and `--mode prod` use the persistent
  `WA_RESULTS_DB_PATH` and never delete the database on shutdown.
- `make test` always deletes `.tmp/e2e/` (the built `wa` binary, the
  Playwright port-allocation cache, and any stray ephemeral SQLite DBs) on
  completion, regardless of whether the sub-suites passed or failed. Run
  `make clean-test-tmp` to perform the same cleanup manually.
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
`WA_DEV_SEQMETA_HOST` and `WA_PROD_SEQMETA_HOST` are the equivalent bind hosts
for managed `wa mlwh serve`.
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
wa results search --pipeline-name nf-pipe
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

### Using the MLWH CLI from another machine

Remote `wa mlwh info` users need a running `wa mlwh serve` API, not local MLWH
database credentials. Configure the server bind address on the machine running
the stack, then give users the public client URL.

For a development stack with the default local cache-backed MLWH server:

```bash
# On the machine running make dev, usually in .env.development.local
export WA_DEV_SEQMETA_HOST=0.0.0.0
export WA_DEV_SEQMETA_PORT=3673
make dev

# On remote CLI machines
export WA_MLWH_SERVER_URL=http://farm22-wrstat01:3673
wa mlwh info JAGUAR_Genomes15249455
```

The production equivalents are `WA_PROD_SEQMETA_HOST` and
`WA_PROD_SEQMETA_PORT`. This mirrors the results setup:
`WA_DEV_RESULTS_HOST` controls where the Results API listens, while
`WA_RESULTS_SERVER_URL` is the full URL remote results CLI users dial.
Likewise, `WA_DEV_SEQMETA_HOST` controls where the MLWH API listens, while
`WA_MLWH_SERVER_URL` is the full URL `wa mlwh info` dials.

`WA_MLWH_SERVER_URL` is not read as a bind address by `mlwh serve`. If local
MLWH backing is configured with `WA_MLWH_CACHE_PATH`, `WA_MLWH_DSN`, or
`WA_RUN_DEV_SEQMETA_CMD`, `make dev` still starts the local MLWH server even
when `WA_MLWH_SERVER_URL` is also set; the URL is only the public/client URL.

By default `wa mlwh serve` is plain HTTP, so remote users should use
`http://host:port`. Only publish an `https://` `WA_MLWH_SERVER_URL` when the
MLWH server is started with its TLS/token settings:
`WA_MLWH_SERVER_CERT`, `WA_MLWH_SERVER_KEY`, and `WA_MLWH_SERVER_TOKEN` (or the
equivalent flags). Results uses HTTPS by default in `run-dev.sh`; MLWH does not
unless those MLWH-specific settings are configured.

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
# Build (stamps the git describe version that `wa version` prints)
make build

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
./wa --env development results search --pipeline-name nf-pipe

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

## Production on a farm host

The results API and website run on a farm host, such as farm22-wrstat01 as
user mercury from a checkout at `/nfs/hgi/wa/repo`, because they need `/lustre`,
`/nfs`, and local NSS lookups. The MLWH API runs elsewhere, on Kubernetes
from the [container image](#container-image), so this stack starts no local
`wa mlwh serve` or `wa mlwh sync`.

`make prod` runs `run-dev.sh --mode prod` in the foreground. It runs `next
build`, then serves the build over HTTPS with `frontend/scripts/serve-https.mts`
(`next start` has no HTTPS option), using the same self-signed certificate
as `make dev`. The `make prod-*` targets run that same command in the
background.

### Configure

Put the deployment settings in `.env.production.local` (gitignored):

```bash
# Ports. These match `make dev`, so existing URLs keep working.
WA_PROD_FRONTEND_PORT=3671
WA_PROD_RESULTS_PORT=3672
WA_PROD_RESULTS_HOST=0.0.0.0

# Results database: a passwordless MySQL DSN plus WA_RESULTS_DB_PASSWORD, or
# an absolute SQLite path on local disk. Not SQLite on /nfs or /lustre: its WAL
# mode does not work on network filesystems.
WA_RESULTS_DB_PATH='wa@tcp(db-host:3306)/wa_results'
WA_RESULTS_DB_PASSWORD=

# LDAP for web and CLI logins.
WA_RESULTS_LDAP_SERVER=ldap.example.org
WA_RESULTS_LDAP_DN='uid=%s,ou=people,dc=example,dc=org'

# Remote MLWH API. A path prefix is fine. Leave WA_MLWH_DSN and
# WA_MLWH_CACHE_PATH unset: either one makes run-dev.sh start a local MLWH.
WA_MLWH_SERVER_URL=https://apps.hgi.sanger.ac.uk/mlwh-api

# MLWH admin token for the /feedback and /usage pages (see below).
WA_MLWH_SERVER_TOKEN=/nfs/hgi/wa/mlwh-admin.token

# Comma-separated LDAP usernames who may see the /feedback and /usage pages.
# The OS user running the stack (mercury) is always included.
WA_FEEDBACK_ADMINS=alice,bob
```

TLS: `run-dev.sh` reuses `.tmp/wa-dev-cert.pem` and `.tmp/wa-dev-key.pem`,
and regenerates them only when a required hostname is missing. Set
`WA_RESULTS_SERVER_CERT` and `WA_RESULTS_SERVER_KEY` to use other files.

MLWH TLS: `.env.production` sets `NODE_USE_SYSTEM_CA=1`, so the frontend
trusts the Genome Research Ltd. CA that signs `apps.hgi.sanger.ac.uk`, as
the Go code already does.

MLWH admin token: `WA_MLWH_SERVER_TOKEN` is a file path, not the token itself.
An absolute path is used as is; a bare name is looked up in `$XDG_STATE_HOME`,
else `$HOME`. The MLWH pod keeps its token at `/data/.wa-mlwh-server.token`.
Copy it to the farm host and keep it private:

```bash
(umask 077; kubectl exec <mlwh-pod> -- cat /data/.wa-mlwh-server.token >/nfs/hgi/wa/mlwh-admin.token)
```

Without a readable token the `/feedback` and `/usage` pages report that they
are unavailable; the rest of the site works.

Admins: the website shows the `/feedback` and `/usage` pages only to users
named in `WA_FEEDBACK_ADMINS` and to the OS user running the stack. Admins log
in to the website with their own LDAP account; the server reads the MLWH token
itself, so they never handle it.

### Run

```bash
make prod-start    # start in the background; prints the URLs
make prod-status   # exit 0 and print the URLs when healthy
make prod-stop     # stop the whole process group
make prod-restart  # stop, then start
```

`make prod-start` reports a start only once `run-dev.sh` prints
`Development environment is ready.`, which it does after its own services
pass their health checks. It gives up and stops the stack after
`WA_PROD_START_TIMEOUT_SECONDS` (default 1800), which covers the frontend
build.

`make prod-start` does nothing and prints nothing when the stack is already
healthy, or is still starting: its `run-dev.sh` is running but has not yet
printed the ready line, and started less than `WA_PROD_START_TIMEOUT_SECONDS`
ago. That covers a start you interrupted with Ctrl-C, which leaves the stack
building in the background. When a health check fails, it retries for about
30 seconds (`WA_PROD_HEALTH_ATTEMPTS` checks, `WA_PROD_HEALTH_RETRY_SECONDS`
apart) before it restarts the stack, so one slow response does not cost a
rebuild. `make prod-status` checks once, so it answers at once. While the
stack is still starting, `make prod-status` says so and exits 1, as it does
whenever the stack is not healthy.

The stack runs in its own process group. `.tmp/prod.pid` records the group's
ID, the host, and a random stack ID that every process in the group inherits
as `WA_PROD_STACK_ID`. The `make prod-*` targets signal a group only when its
processes carry that stack ID, so a stale pid file, for example after a
reboot, never stops an unrelated process. `.tmp` is on NFS, so the pid file
is visible from other hosts: there, every `make prod-*` target refuses, names
the host that runs the stack, and leaves the file alone. If `run-dev.sh`
itself dies, for example from `kill -9`, its services keep running.
`prod-status` then reports that, and `prod-start`, `prod-stop`, and
`prod-restart` stop the leftover group; `prod-start` and `prod-restart` then
start a new stack.

When `.tmp/prod.pid` records no running stack but a process is listening on
the frontend or results port, `prod-start` and `prod-restart` refuse and name
the port, without touching the logs, and `prod-stop` prints that the stack is
not running, names the port, and exits non-zero. That process may be a stack
whose pid file was deleted or moved; stop it by hand, or restore the file.

A lock in `.tmp/prod.lock` makes a second `prod-start` exit at once while
another start, stop, or restart is in progress. It exits silently, or with a
one-line note when run from a terminal. `prod-stop` and `prod-restart` instead
wait for the lock, with a one-line note when run from a terminal, so while a
start is building the frontend, for example one cron began, they wait until it
finishes. Run `prod-start` from mercury's crontab to bring the stack back after
a reboot or crash:

```text
PATH=/software/hgi/installs/go/bin:/software/hgi/installs/node-v24.11.1/bin:/software/hgi/installs/pnpm:/usr/bin:/bin
*/5 * * * * cd /nfs/hgi/wa/repo && make -s prod-start
```

Cron mails only errors and restarts. Do not also run `make prod` or `make dev`
on the same ports: `prod-start` would report the ports as in use.

With that cron line in place, the next cron run undoes a `make prod-stop`
within 5 minutes. To keep the stack stopped, comment out the cron line first,
and restore it when you want the stack back.

To update:

```bash
cd /nfs/hgi/wa/repo && git pull && make prod-restart
```

`prod-restart` stops the stack before it builds the new frontend, so the site
is down for the whole build.

Logs: `.tmp/prod.log` has the `run-dev.sh` output, and `logs/results.log`
and `logs/frontend.log` have the service output. Each start moves the
previous run's logs to `.1` files, such as `logs/frontend.log.1`, so a crash
that cron restarted leaves its evidence there. Only that one generation is
kept: the next start overwrites the `.1` files.

### CLI binary and certificate

Each start builds `wa` and links it from `.tmp/wa`. Users need that binary and
the certificate that signs the results API:

```bash
cp -L .tmp/wa .tmp/wa-dev-cert.pem /software/hgi/installs/wa/
chmod -R o+rX /software/hgi/installs/wa
```

CLI users then set `WA_RESULTS_SERVER_URL=https://<farm host>:<results port>`,
`WA_RESULTS_SERVER_CERT=/software/hgi/installs/wa/wa-dev-cert.pem`, and
`WA_MLWH_SERVER_URL` to the remote MLWH URL.

## Deploying to Production

### Build

```bash
# Go binary (stamps the git describe version that `wa version` prints)
make build

# Frontend (standalone Node.js app)
cd frontend
pnpm install --frozen-lockfile
pnpm build
```

The `pnpm build` output goes to `frontend/.next/standalone/` — a self-contained
Node.js server.

### Run

For a farm host, follow [Production on a farm host](#production-on-a-farm-host)
instead.

Start the Go backend and Next.js frontend as separate processes. Use a process
manager (systemd, supervisor, etc.) to keep them running.

```bash
# MLWH current-state API
export WA_ENV=production
export WA_PROD_SEQMETA_HOST=0.0.0.0
export WA_PROD_SEQMETA_PORT=8091
export WA_MLWH_DSN='user@tcp(mlwh-host:3306)/warehouse'
export WA_MLWH_PASSWORD='super-secret'
export WA_MLWH_CACHE_PATH=/var/lib/wa/mlwh-cache.sqlite
wa mlwh sync
wa mlwh serve

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

| Variable                       | Required                                | Description                                                                                              |
| ------------------------------ | --------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `WA_ENV`                       | For scenario CLI                        | Use `production` when loading `.env.production` or exporting `WA_PROD_*` defaults                        |
| `WA_PROD_RESULTS_PORT`         | For server/CLI                          | Production results API port used by `make prod`, `results serve`, and same-machine CLI defaults          |
| `WA_PROD_RESULTS_HOST`         | For server                              | Results API bind host for `make prod` / `results serve`; set `0.0.0.0` for remote connections            |
| `WA_PROD_SEQMETA_PORT`         | For server/CLI                          | Production MLWH API port used by `make prod`, `mlwh serve`, and same-machine `wa mlwh info` defaults     |
| `WA_PROD_SEQMETA_HOST`         | For server                              | MLWH API bind host for `make prod` / `mlwh serve`; set `0.0.0.0` for remote connections                  |
| `WA_RESULTS_SERVER_URL`        | For remote CLI                          | Full Results API URL equivalent to `--server`, for example `https://results.example.org:8090`            |
| `WA_RESULTS_DB_PATH`           | For results                             | SQLite path, full DSN, or passwordless MySQL DSN                                                         |
| `WA_RESULTS_DB_PASSWORD`       | Optional                                | MySQL password paired with a passwordless DSN                                                            |
| `WA_MLWH_DSN`                  | For `wa mlwh sync` and `check-source`   | Passwordless MLWH DSN                                                                                    |
| `WA_MLWH_PASSWORD`             | Optional                                | MLWH password paired with a passwordless DSN                                                             |
| `WA_MLWH_CACHE_PATH`           | For `wa mlwh serve` and local operators | SQLite path or passwordless MySQL cache DSN used by server-side MLWH lookups                             |
| `WA_MLWH_CACHE_PASSWORD`       | Optional                                | MLWH cache MySQL password                                                                                |
| `WA_MLWH_STALE_AFTER`          | Optional                                | Go duration after a table's last successful sync before `/freshness` reports it stale (default: `2h`)    |
| `WA_MLWH_TRUSTED_PROXIES`      | Optional                                | Comma-separated proxy IPs/CIDRs whose `X-Forwarded-For` `mlwh serve` trusts (default: none)              |
| `WA_MLWH_SERVER_URL`           | For results/mlwhdiff/mlwh info clients  | Remote `wa mlwh serve` URL, equivalent to `--mlwh-server-url` / `wa mlwh info --server`; not a bind host |
| `WA_RESULTS_SERVER_CERT`       | For results                             | TLS certificate path and CLI trust root; `run-dev.sh` resolves relative paths from the repo root         |
| `WA_RESULTS_SERVER_KEY`        | For results                             | TLS private key path for `wa results serve`; `run-dev.sh` resolves relative paths from the repo root     |
| `WA_RESULTS_BACKEND_URL`       | For frontend                            | Results API URL for the frontend; kept as a lower-precedence CLI default for compatibility               |
| `WA_RESULTS_BACKEND_CA_CERT`   | Optional                                | PEM certificate or CA bundle for frontend trust when results backend TLS is not system-trusted           |
| `WA_MLWH_BACKEND_URL`          | For frontend / compatibility            | MLWH query API URL for the frontend and lower-precedence `wa mlwh info` default                          |
| `WA_STUDIES_CACHE_TTL_SECONDS` | No                                      | Study list cache TTL (default: 300)                                                                      |
| `PORT`                         | No                                      | Frontend listen port (default: 3000)                                                                     |

### Container image

Pushing a `v*` tag publishes the Go binary (no frontend) as
`ghcr.io/wtsi-hgi/wa:<tag>`, for example `ghcr.io/wtsi-hgi/wa:v0.11.1`.
Non-prerelease tags also move `ghcr.io/wtsi-hgi/wa:latest`. Pull requests to
`develop` build and smoke-test the image without pushing it. The image runs
as UID/GID `10001` with `wa` as its entrypoint.

`.git` is not in the build context, so the version `wa version` prints comes
from the `VERSION` build argument: the workflow passes the tag, such as
`v1.0.0`. A local build without it prints `devel`; to stamp it, run
`docker build --build-arg VERSION="$(git describe --tags --always --long --dirty)" .`.

The working directory is `/app`, which holds the committed `.env.production`,
so `wa --env production` loads it from there. Mount deployment secrets as the
single file `/app/.env.production.local`, for example with a secret volume and
`subPath`. Mounting a secret over the whole of `/app` would hide
`.env.production`. A typical `.env.production.local`:

```bash
WA_PROD_SEQMETA_HOST=0.0.0.0
WA_MLWH_DSN='user@tcp(mlwh-host:3306)/warehouse'
WA_MLWH_PASSWORD='...'
WA_MLWH_CACHE_PATH='user@tcp(cache-host:3306)/wa_mlwh_cache'
WA_MLWH_CACHE_PASSWORD='...'
WA_MLWH_FEEDBACK_PATH=/data/mlwh-feedback.sqlite
WA_MLWH_USAGE_PATH=/data/mlwh-usage.sqlite
# Behind a Kubernetes ingress, the cluster's pod CIDR (see below), such as
# WA_MLWH_TRUSTED_PROXIES=10.42.0.0/16
```

Run the MLWH API and a periodic sync in one container by overriding the
command:

```yaml
command: ["sh", "-c"]
args:
    - >-
        (while :; do
        wa --env production mlwh sync ||
        { rc=$?; echo "$(date -Is) wa mlwh sync failed (exit $rc)" >&2; };
        sleep 1800; done) &
        exec wa --env production mlwh serve
```

The first sync starts alongside serve. Opening the cache is safe from both at
once: creating or migrating the schema takes a cross-process lock (a MySQL
named lock, or a SQLite write transaction), so whichever opens second waits
and then finds the schema current, and opening a cache that is already current
takes no lock. The loop waits 30 minutes after each sync finishes before
starting the next.
A failed sync adds one timestamped line with its exit code to the pod log,
such as `2026-10-21T10:50:00+00:00 wa mlwh sync failed (exit 1)`, after the
sync's own error. The line saves `$?` before running `date`, because bash
resets `$?` after a command substitution.

`wa mlwh serve` listens on port `8091` and answers `GET /health` without
reading the cache, so use that for probes. A failed sync is logged and retried
on the next loop, and serve keeps running. `GET /health` stays ok while syncs
fail; watch sync health as described in
[Monitoring MLWH sync](#monitoring-mlwh-sync).

Behind the cluster's ingress controller every request reaches serve from an
ingress pod, so feedback `remote_addr` and usage rows record the ingress
pod's IP rather than the client's, and the `/usage` clients-by-IP count is
the number of ingress pods. To record the client instead, set
`WA_MLWH_TRUSTED_PROXIES` in `/app/.env.production.local` to the addresses
the ingress pods connect from, then restart the deployment. No helm change is
needed. Serve then reads the client from the `X-Forwarded-For` (else
`X-Real-IP`) header that ingress-nginx sets, but only on connections from
those addresses. It walks `X-Forwarded-For` from the right and takes the
first address that is not a trusted proxy, so a client cannot spoof its IP by
sending the header itself. Unset, no proxy is trusted and the socket IP is
stored, as before.

Use the cluster's pod CIDR, which covers the ingress pods wherever they are
rescheduled, for example `WA_MLWH_TRUSTED_PROXIES=10.42.0.0/16`. Trust only
addresses that clients cannot connect from directly: a client inside the pod
CIDR could otherwise claim any IP. To find the value:

```bash
# Per-node pod ranges; the cluster pod CIDR contains them all
kubectl get nodes -o jsonpath='{range .items[*]}{.spec.podCIDR}{"\n"}{end}'
# The ingress controller pods and their IPs
kubectl get pods -A -l app.kubernetes.io/name=ingress-nginx -o wide
```

The current IPs in the usage database are the ingress pods' IPs and should
fall inside that range. If the ingress controller uses `hostNetwork`, its
pods have node IPs: trust the node IPs instead. If a load balancer in front of
the ingress rewrites source addresses, ingress-nginx sees the load balancer,
and the ingress itself must be configured to pass the client on; serve cannot
recover it. After the restart, new feedback reports and usage rows show client
IPs; existing rows keep the ingress IPs.

### Monitoring MLWH sync

Each sync records, per table, when the table last synced without error, and
the error from its most recent failed attempt. `GET /freshness`, `wa mlwh
freshness`, and the website's admin MLWH sync page report them with a status
per table:

- `failing`: the most recent sync of the table failed, or was skipped because
  a table it depends on failed. `blocked_by` names those tables.
- `never_synced`: the table has neither synced data nor a successful sync.
- `stale`: its last successful sync is older than `WA_MLWH_STALE_AFTER`, or
  it has synced data but no recorded success yet.
- `ok`: otherwise.

The overall status is the worst table status, in that order. A table's error
is cleared by its next successful sync, and a failure keeps the time of the
last success. When the MLWH source is unreachable, every table records that
error. Staleness uses the last successful sync, not `high_water`, so tables
whose source data rarely changes stay `ok`.

A single failed sync makes its tables `failing` until their next successful
run, even when nothing is wrong with MLWH. That includes a sync that was
interrupted, for example by a pod rollout or a restart of `wa mlwh serve`: the
interrupted tables report `failing` with a `context canceled` error until the
next sync succeeds.

`WA_MLWH_STALE_AFTER` is a Go duration read by `wa mlwh serve`, default `2h`,
which allows for several missed runs of the 30-minute sync loop. Set it in
`.env.production.local` to change it.

To be mailed when sync needs attention, add this line to mercury's crontab:

```text
*/30 * * * * WA_MLWH_SERVER_URL=https://apps.hgi.sanger.ac.uk/mlwh-api /software/hgi/installs/wa/wa mlwh freshness --check
```

`wa mlwh freshness --check` prints nothing and exits 0 when the overall status
is `ok`. Otherwise it prints one line per table needing attention, then an
`Error:` summary line, all to stderr, and exits 1, so cron mails the lines.
Tables with the same status and error share one line, such as
`17 tables failing (sample, study, ...): connect to MLWH source: ...` when the
source is unreachable. It also exits 1 with one line when the server cannot be
reached or is older than MLWH API 1.12.0. Without `--check`, `wa mlwh
freshness` prints a table of every table's status, and `--json` prints the
`/freshness` document.

Caches created before these records existed gain them on the first open by
this version, with no re-sync. Each table whose `sync_state` row has no resume
cursor, so it is not part-way through a cold load, starts with its last
successful sync set to its `last_run`. A table part-way through a cold load
starts with none and reports `stale` until that load completes. Opening a
cache that already has these records writes nothing, so `wa mlwh serve` and
`wa mlwh freshness` start without waiting for a running sync. Older wa
releases ignore the new `sync_outcome` table, so rolling back is safe.

Mount a persistent volume at `/data` for the feedback and usage SQLite files.
The image sets `XDG_STATE_HOME=/data`, so the admin token for `/feedback` and
`/usage`, `.wa-mlwh-server.token`, is created there on first start and
survives restarts. The `fsGroup` ownership change below adds group
permissions to the token on every mount; serve restores mode `0600` and keeps
the token, logging a warning. Read it with
`kubectl exec <pod> -- cat /data/.wa-mlwh-server.token`. A freshly provisioned
volume is usually owned by root, so it must be writable by UID `10001`: set
the pod's `securityContext.fsGroup: 10001` or fix ownership another way, such
as an init container that runs `chown 10001:10001 /data`. Use a `Recreate`
deployment strategy so that an update never runs an old and a new pod against
the same `ReadWriteOnce` volume and its SQLite files at once.
