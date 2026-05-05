# wa — Workflow Automation

A system for tracking pipeline results, caching sequencing metadata, and
(eventually) automating pipeline execution. It comprises a Go backend exposing
REST APIs and CLIs, and a Next.js web UI for browsing results.

## Current Sub-Products

| Sub-product     | What it does                                                                                                                                                                         |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| **results**     | REST API + CLI for registering, searching, and browsing pipeline output files. Deterministic IDs, file previews, aggregate stats.                                                    |
| **saga**        | Go client library for the [SAGA API](https://saga.cellgeni.sanger.ac.uk/api). Typed access to MLWH studies, samples, libraries, runs, and iRODS file paths with caching and retries. |
| **seqmeta**     | Sequence metadata cache built on saga. Hash-based change detection with watermarks in SQLite, a REST polling API, and a CLI for ad-hoc diffs.                                        |
| **results-web** | Next.js web UI for the results API — searchable table, file browser with inline preview, dashboard stats, and study-based search via seqmeta.                                        |

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
wa saga      — SAGA API inspector
wa seqmeta   — Sequence metadata cache
```

### Register a result set

```bash
wa results register /path/to/output \
  --user jdoe \
  --operator jdoe \
  --command "nextflow run pipeline" \
  --nextflow-workflow /path/to/main.nf \
  --runid my-run-001
```

### Search results

```bash
wa results search --pipeline my-pipeline --user jdoe
```

### Get a result set (with files)

```bash
wa results get --files <id>
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

### Inspect SAGA metadata

```bash
export SAGA_API_TOKEN=your-token
wa saga inspect 12345          # study ID, sample ID, accession, etc.
```

### Start the seqmeta server

```bash
wa seqmeta serve --port 8091 --db seqmeta.db --token "$SAGA_API_TOKEN"
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
# Run the dev stack (real SAGA, persistent SQLite DB, no fixtures)
make dev

# Same, but seed demo fixtures into the dev DB for browsing
make dev-fixtures

# Run all tests (Go + Vitest + Playwright). Hermetic — never touches dev/prod.
make test

# Run the production stack (requires .env.prod)
make prod
```

## Licence

[MIT](LICENSE)
