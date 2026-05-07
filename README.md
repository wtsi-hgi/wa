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
  --nextflow-workflow /path/to/main.nf \
  --runid my-run-001 \
	--study 6568 \
	--sample SANG123

```

The `--run`, `--study`, `--sample`, and `--library` flags resolve through MLWH
and store canonical `seqmeta_*` metadata entries for search and validation.

### Search results

```bash
wa results search --pipeline my-pipeline --user jdoe
```

### Get a result set (with files)

```bash
wa results get --files <id>
```

When you run the CLI against a stack started via the scenario env files, select
the matching environment with `--env` or `WA_ENV`. `wa results ...` then
defaults `--server` to `http://127.0.0.1:<active results port>` from that
scenario's `WA_*_RESULTS_PORT`.

```bash
wa --env development results search --pipeline my-pipeline
wa --env production results register /path/to/output --user jdoe
```

### Start the results API server

```bash
wa results serve --port 8090 --db results.db
```

For MySQL, either export `WA_RESULTS_DB_PATH='user:pass@tcp(host:3306)/dbname'`
and run `wa results serve --port 8090`, or pass a passwordless DSN with
`--db 'user@tcp(host:3306)/dbname'` and export `WA_RESULTS_DB_PASSWORD`.
Password-bearing DSNs are rejected on the command line.
Add `--seqmeta-url http://host:8091` to enable seqmeta validation of
`seqmeta_*` metadata fields.

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

## Licence

[MIT](LICENSE)
