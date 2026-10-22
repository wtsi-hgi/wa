# Workflow Automation: Sub-Product Proposal

## Overview

The workflow automation system is decomposed into eight sub-products. Each is
individually useful, independently deployable, and communicates with the others
via HTTP APIs. Go consumers of MLWH data code against one query interface
(`mlwh.Queryer`), so they run unchanged against an in-process cache or a remote
`wa mlwh serve`.

Four sub-products are built and ship in the `wa` binary and web UI: results,
results-web, mlwh, and mlwhdiff. The other four (notify, jobrun, watchtower,
samplepicker) are planned.

All sub-products share a common tech stack — Go for backends and CLIs, Next.js +
shadcn/ui for web UIs — for consistency, testability, and maintainability.

---

## Sub-Products

### 1. results — Pipeline Results Tracker (built)

**Standalone value:** Any pipeline team can register and browse output files
today, with no other sub-products required.

A REST API and CLI for recording and searching pipeline outputs, providing a
central answer to "what datasets do we have, what work has been done on them,
and by whom?" — essential for onboarders and anyone needing an overview of
current state. Pipelines (or scripts, or humans) POST result metadata — output
paths, pipeline name, input files, exact command lines, sequence metadata, and
contacts (operator who ran the work, requestor who asked for it). Result set
IDs are deterministic, so re-registering the same run updates it. The server
resolves `--run`, `--study`, `--sample`, and `--library` through mlwh, so CLI
users need no MLWH credentials.

### 2. results-web — Results Web UI (built)

**Standalone value:** A browsable view of every registered result set.

Next.js web UI for the results API: a searchable, filterable table, dashboard
stats, study-based search via the mlwh server, and a file browser with inline
previews that render HTML, display CSVs as tables, and transparently decompress
gzipped files.

### 3. mlwh — MLWH Cache and Query Server (built)

**Standalone value:** Any tool or person can ask quick, current-state questions
about MLWH studies, samples, libraries, runs, and iRODS paths over a simple REST
API or CLI, without database credentials or knowledge of the MLWH schema.

`wa mlwh sync` copies the relevant MLWH tables into a local SQLite or MySQL
cache, and every read is answered from that cache. `wa mlwh serve` exposes it as
a read-only REST API in which each public query method maps to one endpoint,
with a generated endpoint reference and `GET /openapi.json`. A Go client library
implements the same interface over HTTP. CLI commands cover identifier lookup
(`info`), search, relationship and product exports, latest data, run
aggregates, studies by sponsor, programme, or user, and cache freshness.

This replaced the originally proposed saga (SAGA API client) and seqmeta
(metadata cache) pair: the SAGA API could not resolve the sample, study, run,
and library identifiers that results needs, so wa reads MLWH directly instead.
Its consumers are results, results-web, mlwhdiff, the CLI, and external systems
such as the MLWH MCP server
([wtsi-hgi/llm-knowledge-base](https://github.com/wtsi-hgi/llm-knowledge-base)),
which answers natural-language questions about MLWH data by calling this API.

### 4. mlwhdiff — MLWH Change Detection (built)

**Standalone value:** Anyone who needs "what's new since last check" over
sequencing metadata can poll for changes instead of re-processing everything.

A narrow change-tracking layer on top of mlwh. It hashes each study's samples
and each sample's iRODS files, stores per-entity SHA256 watermarks with
tombstones for removals in SQLite, and reports what was added, changed, or
removed since the last poll. The first poll returns everything as added.
`wa mlwhdiff diff` runs ad-hoc diffs; `wa mlwhdiff serve` exposes
`/diff/study/{id}` and `/diff/sample/{id}` as a polling API. All MLWH data comes
from mlwh, in-process or remote.

### 5. notify — Notification Service (planned)

**Standalone value:** Generic email notification service usable by any internal
tool.

Accepts notification requests (recipient, template, data) via API or CLI and
sends templated emails via the institutional SMTP relay. Includes rate limiting
and deduplication to prevent spam from flapping jobs. Extensible to Slack/Teams
in future.

### 6. jobrun — Job Submission & Monitoring via wr (planned)

**Standalone value:** Programmatic wr/LSF job submission and tracking, useful
for any tool that needs to run jobs on the cluster.

Go library and CLI that wraps wr to submit individual commands or nextflow
pipelines (bsub'd to the oversubscribed queue) and poll for completion. On job
completion or failure, can POST results to the results tracker and/or fire a
webhook.

### 7. watchtower — Watch Configuration & Trigger Engine (planned)

**Standalone value:** Generic "watch for new sequencing data and do something"
automation engine.

The core automation product. Users register watches: "when a sample cram appears
for study X, create a result subdirectory, download it, generate a pipeline
config from a template, and submit the pipeline via jobrun." Runs as a daemon
polling mlwhdiff, with idempotent trigger tracking to prevent re-runs. Includes
a web dashboard showing watches, triggered runs, and their statuses.

### 8. samplepicker — Sample Selection Web UI (planned)

**Standalone value:** Scientists can browse available samples, curate subsets,
add metadata, and export selections as JSON/TSV for use with any tool.

Web app for selecting samples using the mlwh server's search and export
endpoints, annotating them with additional metadata, and either exporting the
selection or submitting it to watchtower for pipeline execution. Supports
importing supplementary metadata from spreadsheets.

---

## How They Fit Together

Arrows point from a provider to its consumer. Planned sub-products are marked
with `*`.

```
       MLWH replica
            │ sync
       ┌────▼─────┐
       │   mlwh   ├──────────────► MCP server (llm-knowledge-base)
       └────┬─────┘
            │
    ┌───────┴─────┬───────────────┬────────────────┐
    ▼             ▼               ▼                ▼
┌────────┐ ┌─────────────┐ ┌─────────────┐  ┌─────────────┐
│mlwhdiff│ │samplepicker*│ │   results   ├─►│ results-web │
└───┬────┘ └──────┬──────┘ └──────▲──────┘  └─────────────┘
    │             │               │
┌───▼─────────────▼┐              │
│   watchtower*    │              │
└────────┬─────────┘              │
    ┌────▼─────┐                  │
    │ jobrun*  ├──────────────────┘
    └────┬─────┘
    ┌────▼─────┐
    │ notify*  │
    └──────────┘
```

mlwh also feeds results-web directly for study-based search.

The typical automated flow:

1. **mlwh** syncs current MLWH/iRODS state into its cache
2. **watchtower** polls **mlwhdiff** for new data and matches it against
   registered watches, triggering the configured action
3. **jobrun** submits the pipeline to LSF via wr
4. On completion, **jobrun** registers outputs with **results**
5. **notify** emails the requester at each stage

The manual flow: a user finds samples in **samplepicker** via the **mlwh**
server's search and export, selects a subset, and submits it to **watchtower**
(or directly to **jobrun**) for processing.

---

## Build Order

| Phase | Sub-product      | Status  | Rationale                                                           |
| ----- | ---------------- | ------- | ------------------------------------------------------------------- |
| 1     | **results**      | Built   | Immediate need; instantly useful                                    |
| 2     | **results-web**  | Built   | Makes results browsable by anyone                                   |
| 3     | **mlwh**         | Built   | Current-state MLWH answers for results, the web UI, and others      |
| 4     | **mlwhdiff**     | Built   | Change detection for automation; builds on mlwh                     |
| 5     | **notify**       | Next    | Small scope, quick to build, needed by later products               |
| 6     | **jobrun**       | Next    | Required before watchtower; independently useful                    |
| 7     | **watchtower**   | Planned | Core automation — needs mlwhdiff + jobrun                           |
| 8     | **samplepicker** | Planned | Manual curation workflow; needs the mlwh server's search and export |

Each phase delivers working software. notify and jobrun are independent of each
other and can proceed in parallel.

---

## Tech Stack (all sub-products)

### Backend & CLI

| Concern        | Choice                     | Rationale                                                                                                                    |
| -------------- | -------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Language       | Go                         | Consistent with wr; single language for all backend logic                                                                    |
| CLI            | Cobra                      | De-facto Go CLI framework                                                                                                    |
| HTTP routing   | Gin                        | Lightweight, idiomatic                                                                                                       |
| Database       | SQLite + MySQL             | SQLite for tests and local dev; shared MySQL instance for production (no self-hosted DB — uses institutional infrastructure) |
| Job submission | wr Go client library       | Native integration with LSF                                                                                                  |
| Testing        | GoConvey + interface mocks | BDD-style tests; all external deps behind interfaces                                                                         |
| Email          | net/smtp                   | Standard SMTP to institutional relay                                                                                         |

### Web UI

| Concern    | Choice                       | Rationale                                                                                         |
| ---------- | ---------------------------- | ------------------------------------------------------------------------------------------------- |
| Framework  | Next.js (App Router) + React | Server Actions call the Go API server-to-server; backend URLs never exposed to browser            |
| Components | shadcn/ui                    | Accessible, composable primitives (tables, forms, dialogs, comboboxes) with no custom design work |
| Styling    | Tailwind CSS v4              | Utility-first CSS with `@theme` design tokens; dark mode and responsive layout for free           |
| Contracts  | Zod                          | Validates Go API responses on the frontend; catches regressions at the boundary                   |
| Testing    | Vitest + Playwright          | Unit tests for contracts and component logic; browser tests for user flows                        |

Each sub-product with a web UI follows the same pattern: the Go backend exposes
a JSON API via Gin, and a Next.js frontend consumes it through Server Actions.
Server Actions run on the Node.js server, so the Go API can live on a private
network — reducing attack surface and keeping credentials server-side. The
frontend is built and deployed as a standalone Node.js app alongside the Go
binary.

This stack keeps every sub-product simple to build, test, and deploy — no
message queues, SQLite for local development and testing, and a shared
institutional MySQL instance for production. Sub-products without a web UI
(mlwh, mlwhdiff, notify, jobrun) are pure Go with zero frontend dependencies.
