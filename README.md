# wa — Workflow Automation

A system for tracking pipeline results, caching sequencing metadata, and
(eventually) automating pipeline execution. It comprises a Go backend exposing
REST APIs and CLIs, and a Next.js web UI for browsing results.

## Current Sub-Products

| Sub-product     | What it does                                                                                                                                                                                             |
| --------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **results**     | REST API + CLI for registering, searching, and browsing pipeline output files. Deterministic IDs, file previews, aggregate stats.                                                                        |
| **mlwh**        | Go client library, cache sync CLI, current-state REST query server, and reporting commands for MLWH-backed studies, samples, libraries, runs, iRODS paths, product exports, latest data, and programmes. |
| **mlwhdiff**    | Hash-based MLWH change detection with watermarks in SQLite, a REST polling API, and a CLI for ad-hoc diffs.                                                                                              |
| **results-web** | Next.js web UI for the results API - searchable table, file browser with inline preview, dashboard stats, and study-based search via the MLWH server.                                                    |

Planned sub-products (notify, jobrun, watchtower, samplepicker) are described
in [.docs/proposal.md](.docs/proposal.md).

## Install

### Pre-built binary

Download from the GitHub releases page and place `wa` on your `PATH`.

### From source

```bash
go install github.com/wtsi-hgi/wa@latest
```

Requires **Go 1.27+**.

## Usage

`wa` is a single binary with subcommands:

```
wa results   — Pipeline results tracker
wa mlwh      — MLWH cache sync, query server, inspector, and reporting tools
wa mlwhdiff  — MLWH change detection
```

### Register a result set

```bash
wa results register /path/to/output \
  --user jdoe \
  --operator jdoe \
  --command "nextflow run pipeline" \
  --workflow nf-core/sarek \
  --unique my-run-001 \
  --study 6568 \
  --sample SANG123

```

`--workflow` is the workflow identity used to key the result set. It may be an
arbitrary string; for Nextflow it also accepts a local `.nf` workflow file, a
GitHub URL such as `https://github.com/nf-core/sarek`, or an `owner/repo`
shorthand such as `seqeralabs/nf-hello-world`.

The results server resolves the `--run`, `--study`, `--sample`, and `--library`
flags through its configured MLWH queryer and stores canonical `seqmeta_*`
metadata entries for search and validation. Normal CLI users do not need
`WA_MLWH_CACHE_PATH` or MLWH cache credentials locally.

### Search results

```bash
wa results search --pipeline-name my-pipeline --user jdoe
```

### Get a result set (with files)

```bash
wa results get --files <id>
```

When you run the CLI against a stack started via the scenario env files, select
the matching environment with `--env` or `WA_ENV`. Explicit `--server` always
wins. If it is omitted, `wa results ...` uses `WA_RESULTS_SERVER_URL` as a full
client URL, then `WA_RESULTS_BACKEND_URL` as a lower-precedence compatibility
default, then `https://127.0.0.1:<active results port>` from the active
scenario.

```bash
wa --env development results search --pipeline-name my-pipeline
wa --env production results register /path/to/output --user jdoe
```

For a beta tester using a development server from another machine, give them
the full Results API URL. Use the `Results` / `Results public` URL printed by
`make dev`, not the frontend URL:

```bash
export WA_RESULTS_SERVER_URL=https://dev-host.example.org:3672
wa results register /path/to/output --user jdoe --operator jdoe --workflow nf-core/sarek --unique run-001
```

Do not point the CLI at the frontend URL or port, even if browser login works
there. The web UI logs in through its own `/api/auth/login` route, while the CLI
posts directly to the Results API `/rest/v1/jwt` endpoint. If
`wa results register` prompts for `Password:` and then reports
`authentication failed`, check that `WA_RESULTS_SERVER_URL` / `--server` is the
Results API URL, not the frontend URL. In the default development stack this is
usually port `3672` for Results, not port `3671` for the frontend.

If the server uses the self-signed development certificate, also pass `--cert`
or export `WA_RESULTS_SERVER_CERT` pointing at the certificate file they should
trust. `run-dev.sh` creates that certificate for loopback, the hostnames of the
machine running `make dev`, and any `WA_RESULTS_SERVER_URL` hostname it prints.
MLWH lookup flags on `wa results register` are resolved on the results server;
remote CLI users do not need `WA_MLWH_CACHE_PATH`.

### Start the results API server

```bash
wa results serve --port 8090 --db results.db \
  --cert .tmp/wa-dev-cert.pem \
  --key .tmp/wa-dev-key.pem \
  --ldap_server ldap.example.org \
  --ldap_dn 'uid=%s,ou=people,dc=example,dc=org'
```

`--cert` defaults to `WA_RESULTS_SERVER_CERT`, and `results serve --key`
defaults to `WA_RESULTS_SERVER_KEY`.
For MySQL, either export `WA_RESULTS_DB_PATH='user:pass@tcp(host:3306)/dbname'`
and run `wa results serve` with the TLS and LDAP flags above, or pass a
passwordless DSN with `--db 'user@tcp(host:3306)/dbname'` and export
`WA_RESULTS_DB_PASSWORD`.
Password-bearing DSNs are rejected on the command line.
Set `WA_MLWH_SERVER_URL` or pass `--mlwh-server-url http://host:8091` to use a
remote `wa mlwh serve` instance for MLWH validation and search expansion. If no
server URL is set, `wa results serve` reads a local MLWH cache from
`WA_MLWH_CACHE_PATH` or `--mlwh-cache`.
To accept connections from another machine, bind the API to a reachable address
with `--url 0.0.0.0:8090`. When using scenario envs, admins can instead set
`WA_ENV=development`, `WA_DEV_RESULTS_HOST=0.0.0.0`, and
`WA_DEV_RESULTS_PORT=3672`, then run `make dev` or
`wa --env development results serve`; production uses the matching
`WA_PROD_RESULTS_HOST` and `WA_PROD_RESULTS_PORT`. Tell remote CLI users the
public HTTPS URL via `WA_RESULTS_SERVER_URL`.

### Start the MLWH query server

```bash
export WA_MLWH_DSN='mlwh_user@tcp(host:3306)/mlwarehouse'
export WA_MLWH_CACHE_PATH=.tmp/mlwh-cache.sqlite
wa mlwh sync
wa mlwh serve --port 8091
```

To accept `wa mlwh info` connections from another machine, bind the MLWH API
to a reachable address on the server side and give users the public client URL.
With scenario envs this mirrors the results API setup:

```bash
# On the server running make dev
export WA_DEV_SEQMETA_HOST=0.0.0.0
export WA_DEV_SEQMETA_PORT=3673
make dev

# On remote CLI machines
export WA_MLWH_SERVER_URL=http://farm22-wrstat01:3673
```

Use `WA_PROD_SEQMETA_HOST` and `WA_PROD_SEQMETA_PORT` for production. Do not
use `WA_MLWH_SERVER_URL` as a bind setting; it is the URL clients dial. The
default MLWH server is plain HTTP, so publish an `https://` URL only when
`wa mlwh serve` is configured with `WA_MLWH_SERVER_CERT`,
`WA_MLWH_SERVER_KEY`, and `WA_MLWH_SERVER_TOKEN`.

For the current REST API contract, see the generated
[MLWH API endpoint reference](MLWH_API_REFERENCE.md). A running MLWH query
server exposes the same contract as machine-readable OpenAPI at
`GET /openapi.json`.

Normal CLI users can query that server without local MLWH database or cache
credentials. The MLWH CLI covers identifier summaries, sample/study search,
relationship exports, latest data, run aggregation, study-owner/programme
lookups, and product-grained exports:

```bash
export WA_MLWH_SERVER_URL=http://host:8091
wa mlwh info DN1234
wa mlwh info 5901 --type study --json
wa mlwh search hek_r --type sample --library-type Standard --organism "Homo sapiens"
wa mlwh export irods study 5901 --file-type cram --columns supplier_name,manual_qc,irods_path
wa mlwh export sample-crams study 7568 --json
wa mlwh export products study 7568 --file-type cram --columns name,supplier_name,accession_number,sanger_sample_id,id_run,lane,tag_index,manual_qc,irods_path,irods_unmatched,reason
wa mlwh latest 5901 --file-type cram
wa mlwh runs --monthly --group-by platform,programme --since 2024-01-01
wa mlwh studies --programme "Human Genetics"
wa mlwh programmes
```

When using the local development scenario, `wa --env development mlwh info
DN1234` defaults to the MLWH API port from the scenario env file.
Use `wa mlwh export products study <id>` when you need one row per sequencing
product. It is product-grained, keyed by run/lane/tag, and includes products
with no matching iRODS path; those rows keep `irods_path` blank and can explain
known gaps with `irods_unmatched` and `reason`.

Use `wa mlwh export irods ...` for file-object-grained exports: each row is one
iRODS data object/path. The older standalone `wa mlwh irods` command has been
replaced by the generic export surface. Use `wa mlwh export sample-crams ...`
for sample-grained CRAM exports: each row is one selected, merged-aware CRAM for
a sample.

A supplier name or donor id often names several Sanger samples, for example a
top-up or a sample that was registered again and never sequenced. Sample
exports cover all of them:

```bash
wa mlwh export irods sample PD42478a
wa mlwh export lanes sample PD42478a
```

Each row names its sample (`supplier_name` and `sanger_sample_id` are in the
default `irods` and `lanes` columns). A sample with no matching rows adds none,
so samples that were never sequenced drop out. `runs` and `studies` exports
list each run or study once across the samples. Identifiers that must be unique
(Sanger sample name or id, UUID, LIMS id, accession) still fail as ambiguous if
they ever match several samples. This needs a server at MLWH API 1.11.0 or
later; an older server makes the export fail with a message saying so.

MLWH API 1.11.0 changed the default `lanes` export columns for every caller,
including single-sample exports, from `id_run,lane,tag_index` to
`supplier_name,sanger_sample_id,id_run,lane,tag_index`. Scripts that parse the
old three-column output should ask for it explicitly:

```bash
wa mlwh export lanes sample <sample> --columns id_run,lane,tag_index
```

`wa mlwh info` on such an identifier lists the matching samples, their studies
and whether each has deliverable data, then suggests the export and
`wa mlwh search --type sample <id> --deliverables-only` commands. It exits
non-zero because the identifier did not resolve to one record; `--json` prints
the list as `candidates`. The `--json` output of `wa mlwh info` and
`wa mlwh search` carries `cache_synced_at`, the cache sync time the human
`info` output shows. Export output stays data only.

#### Agent feedback

LLM agents using the MLWH MCP server can report problems with a user request to
`POST /feedback`. Feedback is off by default. To turn it on, name a SQLite file:

```bash
wa mlwh serve --port 8091 --feedback-db /var/lib/wa/mlwh-feedback.sqlite
```

`--feedback-db` defaults to `WA_MLWH_FEEDBACK_PATH`. The server creates the
file at mode 0600 and its parent directory, and rejects a MySQL DSN,
`:memory:`, and `file:` URIs. The feedback database is separate from the MLWH
cache. Without a feedback database, every feedback route answers 503
`feedback_disabled`.

The admin routes (`GET /feedback`, `PATCH /feedback/:id`, and
`DELETE /feedback/:id`) sit at the server root in both modes and require
`Authorization: Bearer <token>`. The token is the contents of a token file in
`$XDG_STATE_HOME`, or your home directory when that is unset:

- Plain mode: `.wa-mlwh-server.token`. `wa mlwh serve` creates it at mode 0600
  the first time it starts with feedback on, and reuses it afterwards.
- Secured mode: the `--server-token` / `WA_MLWH_SERVER_TOKEN` file that secured
  mode already creates. An absolute value is used as-is.

With feedback off, `wa mlwh serve` neither creates nor reads a token file for
feedback.

The frontend `/feedback` page lists, filters, acknowledges, and deletes
reports. To use it:

- Run Next.js as the same OS user as `wa mlwh serve`, with the same
  `XDG_STATE_HOME` (or home directory), so it can read the token file. Next.js
  sends the token from the server side; the browser never receives it.
- Set `WA_MLWH_BACKEND_URL` to the MLWH server root. The page calls
  `${WA_MLWH_BACKEND_URL}/feedback`.
- Log in through the account menu with your LDAP username and password.
- Be an admin: your username must appear in the comma-separated
  `WA_FEEDBACK_ADMINS` list (exact, case-sensitive match), or be the OS user
  that runs Next.js. Admins see a Feedback link in the account menu.

Known limits:

- `WA_MLWH_SERVER_TOKEN` is for secured mode only. `wa mlwh serve` reads it as
  its `--server-token` default, and any non-empty value makes the server
  secured, so it then also needs `--cert` and `--key`. In secured mode, set it
  to the same value in the environment of both `wa mlwh serve` and Next.js:
  Next.js cannot see a token path given only as the `--server-token` flag. In
  plain mode, leave it unset for both, and both use `.wa-mlwh-server.token`.
- The MCP server sends no credentials, so it can submit feedback only to a
  plain-mode `wa mlwh serve`. Secured mode serves TLS only and moves the submit
  route to `POST /rest/v1/auth/feedback`, which needs a gas JWT like every
  data endpoint.
- The frontend's other MLWH reads send no JWT, so against a secured server
  with `WA_MLWH_BACKEND_URL` at the root they get 404 and only the feedback
  page works.
- Next.js has no CA setting for the MLWH server. If a secured server's
  certificate is signed by a private CA, start Next.js with
  `NODE_EXTRA_CA_CERTS=<ca.pem>`.
- Each report stores the TCP peer host as its remote address. For MCP traffic
  that is the MCP server host, not the end user's machine.
- There is no rate limiting and no notification of new reports. Reports are
  kept until an admin deletes them.

`make dev` passes `--feedback-db` only to an MLWH server it starts itself. Test
mode always enables feedback with a throwaway database under `.tmp/` that is
removed on shutdown. Dev and production modes enable it when
`WA_MLWH_FEEDBACK_PATH` is set; a relative path resolves from the repo root.
In dev mode, if an MLWH server is already healthy on the MLWH port, `make dev`
reuses it, starts nothing, and does not apply `--feedback-db`. Feedback then
depends on how that server was started, so stop it first if you want
`make dev` to start one with feedback on. A remote `WA_MLWH_SERVER_URL` or a
`WA_RUN_DEV_SEQMETA_CMD` server never gets the flag.

#### Usage metrics

`wa mlwh serve` can record one row per request in its own SQLite file, so an
admin can see who uses the MLWH API and whether use is rising or falling.
Usage tracking is off by default. To turn it on, name a SQLite file:

```bash
wa mlwh serve --port 8091 --usage-db /var/lib/wa/mlwh-usage.sqlite
```

`--usage-db` defaults to `WA_MLWH_USAGE_PATH`. As with `--feedback-db`, the
server creates the file at mode 0600 and its parent directory, and rejects a
MySQL DSN, `:memory:`, and `file:` URIs. The usage database is separate from
the MLWH cache and the feedback database. The server deletes rows
older than `--usage-retention-days` days at startup and every hour. The flag
defaults to `WA_MLWH_USAGE_RETENTION_DAYS`, else 365, and must be at least 1.
An explicit flag overrides the env var.

`GET /usage?window=24h|7d|30d|all` (default `7d`) returns the aggregates. It
sits at the server root in both modes and needs the same
`Authorization: Bearer <token>` as the feedback admin routes, read from the
same token file. With usage on and feedback off, `wa mlwh serve` still
creates or reads that token file. With usage off, `GET /usage` answers 503
`usage_disabled`.

The frontend `/usage` page shows the aggregates: totals, a per-category
table, a trend over consecutive windows back to the earliest row, endpoints
with latency percentiles, MCP tools and agents, other API user agents,
unmatched paths, top users and clients, and requests per UTC day. It has the
same setup and admin rule as the `/feedback` page above: a user in
`WA_FEEDBACK_ADMINS` or the OS user running Next.js. Admins see a Usage link
in the account menu.

Callers identify themselves with optional request headers. Servers older than
MLWH API 1.10.0 ignore them.

| Header               | Sent by             | Value                                                                    |
| -------------------- | ------------------- | ------------------------------------------------------------------------ |
| `User-Agent`         | all wa clients      | `<product>/<version>` (products below)                                   |
| `X-WA-Client-ID`     | CLI, web, MCP       | CLI: per-process run ID. Web: hashed browser cookie. MCP: hashed session |
| `X-WA-Client-User`   | CLI, web, MCP stdio | self-declared username                                                   |
| `X-WA-MCP-Transport` | MCP                 | `stdio` or `http`                                                        |
| `X-WA-MCP-Agent`     | MCP                 | stdio: clientInfo `<name>/<version>`. HTTP: the agent's User-Agent       |
| `X-WA-MCP-Tool`      | MCP                 | MCP tool name                                                            |

The User-Agent product, the text before the first `/` or space, decides the
category:

| Product              | Category          | Sender                                                                                 |
| -------------------- | ----------------- | -------------------------------------------------------------------------------------- |
| `wa-cli`             | `cli`             | `wa mlwh info/latest/search/runs/export/studies/programmes/people`, `wa mlwhdiff diff` |
| `mlwh-mcp-server`    | `mcp`             | the MLWH MCP server                                                                    |
| `wa-web`             | `web`             | the Next.js server                                                                     |
| `wa-results-server`  | `results_server`  | `wa results serve`, which also carries `wa results register` traffic                   |
| `wa-mlwhdiff-server` | `mlwhdiff_server` | `wa mlwhdiff serve`                                                                    |
| `wa-run-dev`         | not counted       | `run-dev.sh` readiness checks                                                          |
| anything else, none  | `other`           | unidentified callers, such as `curl`                                                   |

The CLI sends the local OS username, the web sends the logged-in username,
and the MCP stdio server sends its user's name. These usernames are
self-declared: the server does not check them. CLI and MCP users cannot opt
out of sending theirs. `wa results serve` and `wa mlwhdiff serve` send no
client ID or username, so their rows do not name the end user.

The web client ID comes from the `wa_usage_session` cookie: a random UUID
that Next.js sets for a year on each browser. It is anonymous, holds nothing
else, and is used only for usage counting. Next.js sends a hash of it, never
the cookie itself.

Requests that are counted: every data endpoint, `GET /openapi.json`,
`POST /feedback`, and requests that match no route (stored as unmatched
paths). Not counted: `GET /health`, the feedback admin routes, `GET /usage`,
requests with User-Agent product `wa-run-dev`, and, in secured mode, requests
the gas JWT middleware rejects. Web lookups served from the browser's
`wa-seqmeta-cache` cookie or the Next.js `/studies` cache never reach the
server, so these cache hits are not counted.

`make dev` wires `--usage-db` the same way as `--feedback-db` (see Agent
feedback above). Test mode always enables usage with a throwaway database
under `.tmp/` that is removed on shutdown. Dev and production modes enable it
when `WA_MLWH_USAGE_PATH` is set. A reused, remote, or
`WA_RUN_DEV_SEQMETA_CMD` MLWH server never gets the flag. The readiness
probes in `run-dev.sh` send User-Agent `wa-run-dev`, so they are not counted.

Known limits:

- Usernames, client IDs, and User-Agents are self-declared and spoofable. Any
  caller can send `wa-run-dev` to avoid being counted.
- Web counts are requests that reached the MLWH server, not page views.
- The Next.js `/studies` cache fetch carries no session or user.
- Clients by IP are distinct socket IPs among rows with no client ID, so
  callers behind one host merge. All MCP HTTP users share the MCP host's IP.
- Each row stores the socket IP, never `X-Forwarded-For`.
- The server queues rows for one writer goroutine. If the queue of 4096 rows
  fills, new rows are dropped; the page reports how many since server start.
- gin answers a trailing-slash request such as `GET /studies/` with a
  redirect before any handler runs, so the redirect records no row. A client
  that follows it is counted under the target route.
- The trend has one bar per window back to the earliest stored row, on every
  category and endpoint row, so the page grows with stored history. The 24h
  window grows most: a year of rows gives about 366 bars per row, several
  megabytes of HTML that load slowly. Lower `--usage-retention-days` to bound
  it.

### Poll for metadata changes

```bash
export WA_MLWH_SERVER_URL=http://localhost:8091
wa mlwhdiff diff --study 12345
wa mlwhdiff diff --sample SANG001
```

### Start the change-tracking API

```bash
export WA_MLWH_SERVER_URL=http://localhost:8091
wa mlwhdiff serve --port 8092 --db mlwhdiff.db
```

### Classify an identifier

```bash
curl http://localhost:8091/classify/SomeIdentifier
```

## Development

See [DEVELOPING.md](DEVELOPING.md) for full setup, testing, and deployment
instructions.

Quick start:

```bash
# Run the dev stack (MLWH-backed query server, persistent SQLite DB, no fixtures)
make dev

# Same, but seed demo fixtures into the dev DB for browsing
make dev-fixtures

# Run all tests (Go + Vitest + Playwright). Live MLWH checks skip unless configured.
make test

# Run the production stack (uses .env.production + .env.production.local)
make prod
```

`run-dev.sh` creates or reuses self-signed development certificates at
the `WA_RESULTS_SERVER_CERT` and `WA_RESULTS_SERVER_KEY` paths from the active
env file, defaulting to `.tmp/wa-dev-cert.pem` and `.tmp/wa-dev-key.pem`.
Relative paths are resolved from the repo root before child processes are
started. If an existing certificate is missing a required hostname, it is
regenerated with SANs for loopback, the current machine's `hostname -f` and
`hostname -s`, and any configured public results hostname. It exports
`WA_RESULTS_BACKEND_URL=https://127.0.0.1:<port>` and
`WA_RESULTS_BACKEND_CA_CERT` pointing at the same certificate for the Next.js
server, and starts `next dev` with matching experimental HTTPS key/cert flags. Development
mode still requires real `WA_RESULTS_LDAP_SERVER` and `WA_RESULTS_LDAP_DN`
values, usually in `.env.development.local`; only test mode uses the committed
test LDAP placeholders.
For remote CLI users, put the public development URL in
`WA_RESULTS_SERVER_URL`, for example
`WA_RESULTS_SERVER_URL=https://dev-host.example.org:3672`. This must be the
Results API port, not the frontend port. To make the development server listen
beyond loopback, set `WA_DEV_RESULTS_HOST=0.0.0.0` in
`.env.development.local`; keep `WA_DEV_RESULTS_PORT` as the single source of the
port.

Most tests use in-memory SQLite, temporary on-disk SQLite caches, fake HTTP
servers, or throwaway databases. When `.env.development.local` contains
`WA_MLWH_DSN` / `WA_MLWH_PASSWORD`, `make test` surfaces those only to the
`./mlwh` package so its source-schema integration checks can run locally. When
the same file contains `WA_MLWH_CACHE_PATH` / `WA_MLWH_CACHE_PASSWORD`, the
real-MySQL cache integration tests create and drop a unique throwaway database
on that server; they do not use the configured cache database itself. If those
values are absent, the tests skip.

The broader live MLWH checks are still opt-in. Set `WA_LIVE_MLWH_TESTS=1`
explicitly to run them; real cold-sync performance tests also require
`MLWH_SYNC_PERF_TEST=1`.

## Licence

[MIT](LICENSE)
