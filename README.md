# wa — Workflow Automation

A system for tracking pipeline results, caching sequencing metadata, and
(eventually) automating pipeline execution. It comprises a Go backend exposing
REST APIs and CLIs, and a Next.js web UI for browsing results.

## Current Sub-Products

| Sub-product     | What it does                                                                                                                                              |
| --------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **results**     | REST API + CLI for registering, searching, and browsing pipeline output files. Deterministic IDs, file previews, aggregate stats.                         |
| **mlwh**        | Go client library and cache sync CLI for MLWH-backed study, sample, library, run, and iRODS lookups.                                                      |
| **seqmeta**     | Sequence metadata API built on MLWH-backed caches. Hash-based change detection with watermarks in SQLite, a REST polling API, and a CLI for ad-hoc diffs. |
| **results-web** | Next.js web UI for the results API — searchable table, file browser with inline preview, dashboard stats, and study-based search via seqmeta.             |

Planned sub-products (notify, jobrun, watchtower, samplepicker) are described
in [.docs/proposal.md](.docs/proposal.md).

## Install

### Pre-built binary

Download from the GitHub releases page and place `wa` on your `PATH`.

### From source

```bash
go install github.com/wtsi-hgi/wa@latest
```

Requires **Go 1.25+**.

## Usage

`wa` is a single binary with subcommands:

```
wa results   — Pipeline results tracker
wa mlwh      — MLWH cache sync and inspector
wa seqmeta   — Sequence metadata cache
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

The `--run`, `--study`, `--sample`, and `--library` flags are resolved by the
results server through its configured MLWH cache and store canonical
`seqmeta_*` metadata entries for search and validation. Normal CLI users do not
need `WA_MLWH_CACHE_PATH` or MLWH cache credentials locally.

### Search results

```bash
wa results search --pipeline my-pipeline --user jdoe
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
wa --env development results search --pipeline my-pipeline
wa --env production results register /path/to/output --user jdoe
```

For a beta tester using a development server from another machine, give them
the full Results API URL. Use the `Results` / `Results public` URL printed by
`make dev`, not the frontend URL:

```bash
export WA_RESULTS_SERVER_URL=https://dev-host.example.org:3672
wa results register /path/to/output --user jdoe --operator jdoe --workflow nf-core/sarek --unique run-001
```

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
Add `--seqmeta-url http://host:8091` to enable seqmeta validation of
`seqmeta_*` metadata fields.
To accept connections from another machine, bind the API to a reachable address
with `--url 0.0.0.0:8090`. When using scenario envs, admins can instead set
`WA_ENV=development`, `WA_DEV_RESULTS_HOST=0.0.0.0`, and
`WA_DEV_RESULTS_PORT=3672`, then run `make dev` or
`wa --env development results serve`; production uses the matching
`WA_PROD_RESULTS_HOST` and `WA_PROD_RESULTS_PORT`. Tell remote CLI users the
public HTTPS URL via `WA_RESULTS_SERVER_URL`.

### Start the seqmeta server

```bash
export WA_MLWH_DSN='mlwh_user@tcp(host:3306)/mlwarehouse'
export WA_MLWH_CACHE_PATH=.tmp/mlwh-cache.sqlite
wa seqmeta serve --port 8091
```

### Poll for metadata changes

```bash
wa seqmeta diff --study 12345
wa seqmeta diff --sample SANG001
```

### Validate an identifier

```bash
wa seqmeta validate SomeIdentifier
```

## Development

See [DEVELOPING.md](DEVELOPING.md) for full setup, testing, and deployment
instructions.

Quick start:

```bash
# Run the dev stack (MLWH-backed seqmeta, persistent SQLite DB, no fixtures)
make dev

# Same, but seed demo fixtures into the dev DB for browsing
make dev-fixtures

# Run all tests (Go + Vitest + Playwright). Hermetic — never touches dev/prod.
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

`make test` skips live MLWH integration tests by default. Set
`WA_LIVE_MLWH_TESTS=1` explicitly to run live MLWH checks; real cold-sync
performance tests also require `MLWH_SYNC_PERF_TEST=1`.

## Licence

[MIT](LICENSE)
