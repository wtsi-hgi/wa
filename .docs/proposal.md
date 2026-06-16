# Workflow Automation: Sub-Product Proposal

## Overview

The workflow automation system is decomposed into seven independent
sub-products. Each is individually useful, independently deployable, and
communicates with the others via HTTP APIs (or, where a Go caller wants no
network hop, by importing the relevant library directly). No sub-product has a
compile-time dependency on a *running* peer — if a dependency isn't up, the
caller degrades gracefully (e.g. logs instead of emailing).

All sub-products share a common tech stack — Go for backends and CLIs, Next.js +
shadcn/ui for web UIs — for consistency, testability, and maintainability.

---

## Sub-Products

### 1. results — Pipeline Results Tracker

**Standalone value:** Any pipeline team can register and browse output files
today, with no other sub-products required.

A REST API and web UI for recording and searching pipeline outputs, providing a
central answer to "what datasets do we have, what work has been done on them,
and by whom?" — essential for onboarders and anyone needing an overview of
current state. Pipelines (or scripts, or humans) POST result metadata — output
paths, pipeline name, input files, exact command lines, sequence metadata, and
contacts (operator who ran the work, requestor who asked for it). The web UI
provides searchable, filterable results with clickable file paths that render
HTML inline, display CSVs as tables, and transparently decompress gzipped files.

### 2. mlwh — Sequencing Metadata Access (Library + REST Service)

**Standalone value:** Any Go service or tool — or any external system over
REST — can ask current-state sequencing-metadata questions with no MLWH SQL
boilerplate and without suffering the warehouse's size and latency.

The single source of current-state sequencing metadata for the system. A Go
library and a standalone REST service (`wa mlwh serve`) that answer identifier
resolution, hierarchy walks (study ↔ sample ↔ library ↔ run/lane ↔ iRODS path),
search expansion, and graph "enrichment" — all from a **local synced cache** of
the relevant MLWH tables (SQLite or MySQL), refreshed by `wa mlwh sync`. Reads
are cache-only and index-backed; there is no live MLWH round-trip on the read
path. A single Go query interface is implemented by both an in-process
cache-backed client and a remote HTTP client, so a caller wires to a local cache
or a shared server by configuration alone, and every public query method maps
1:1 to a documented REST endpoint.

### 3. mlwhdiff — Sequencing Metadata Change Tracking

**Standalone value:** Anyone driving incremental automation gets "what's new
since last check" over sequencing metadata without re-processing everything.

A narrow change-tracking layer on top of mlwh. Stores per-query watermarks and
tombstones in a local SQLite database and computes diffs over study/sample
metadata and iRODS file paths so consumers (notably watchtower) can efficiently
poll for new data. It delegates all current-state queries to mlwh and focuses
solely on change detection — it defines no MLWH domain shapes of its own.

### 4. notify — Notification Service

**Standalone value:** Generic email notification service usable by any internal
tool.

Accepts notification requests (recipient, template, data) via API or CLI and
sends templated emails via the institutional SMTP relay. Includes rate limiting
and deduplication to prevent spam from flapping jobs. Extensible to Slack/Teams
in future.

### 5. jobrun — Job Completion Side-Effects

**Standalone value:** Any tool (or external system) that submits commands or
nextflow pipelines to the cluster gets pushed the moment they finish, and gets
their outputs registered and webhooks fired, without writing that glue itself.

The wr Go client already does job submission and monitoring well: connect, add
jobs under a `RepGroup`, query state. jobrun therefore does **not** re-wrap wr —
callers use the wr client directly to submit (commands or nextflow pipelines,
bsub'd to the oversubscribed queue). jobrun is the thin layer for what wr leaves
to the caller: **subscribe** to a submitted `RepGroup` and, on completion or
failure, POST outputs to the results tracker and/or fire a webhook. Completion
is delivered by push, not polling — see the wr enhancement below. A Go library
and CLI, usable in-process by other sub-products and importable directly by an
external system's Go backend (e.g. a web app that runs a known command to
produce graphical outputs and shows them the instant they are ready).

This depends on one wr change: today the wr Go client can only *poll* for
completion, even though the manager already pushes live state changes to its
web-UI websocket. wr should expose that existing push mechanism to the Go client
so callers can subscribe to a `RepGroup` and be notified immediately on
completion. Requirements for that change are written up in
[`wr_changes.md`](wr_changes.md) for the wr team. Until it lands, jobrun falls
back to polling wr's existing `Get*` methods.

### 6. watchtower — Watch Configuration & Trigger Engine

**Standalone value:** Generic "watch for new sequencing data and do something"
automation engine.

The core automation product. Users register watches: "when a sample cram appears
for study X, create a result subdirectory, download it, generate a pipeline
config from a template, and submit the pipeline via jobrun." Runs as a daemon
polling mlwhdiff, with idempotent trigger tracking to prevent re-runs. Includes a
web dashboard showing watches, triggered runs, and their statuses.

### 7. samplepicker — Sample Selection Web UI

**Standalone value:** Scientists can browse available samples, curate subsets,
add metadata, and export selections as JSON/TSV for use with any tool.

Web app for manually selecting samples from mlwh, annotating them with
additional metadata, and either exporting the selection or submitting it to
watchtower for pipeline execution. Supports importing supplementary metadata
from spreadsheets.

---

## How They Fit Together

```
                         ┌──────────┐
                         │   mlwh   │  (synced cache; library + REST)
                         └────┬─────┘
              ┌───────────────┼───────────────┬──────────────┐
              ▼               ▼               ▼              ▼
        ┌──────────┐  ┌──────────────┐ ┌────────────┐ ┌──────────┐
        │ mlwhdiff │  │   results    │ │samplepicker│ │   ...    │
        └────┬─────┘  └──────▲───────┘ └────────────┘ └──────────┘
             │               │
        ┌────▼─────┐         │ register outputs
        │watchtower│         │
        └────┬─────┘    ┌────┴─────┐
             └─────────▶│  jobrun  │──▶ (wr / LSF)
                        └────┬─────┘
                             │
                        ┌────▼─────┐
                        │  notify  │
                        └──────────┘
```

The typical automated flow:

1. **mlwh** keeps a local synced cache of MLWH/iRODS state; **mlwhdiff** detects
   new data against stored watermarks
2. **watchtower** matches it against registered watches and triggers the
   configured action
3. **watchtower** submits the pipeline to wr/LSF (via the wr client) and uses
   **jobrun** to be pushed on completion
4. On completion, **jobrun** registers outputs with **results** and/or fires a
   webhook
5. **notify** emails the requester at each stage

The manual flow: a user browses samples in **samplepicker** (served by **mlwh**),
selects a subset, and submits it to **watchtower** (or runs a command directly,
with **jobrun** handling the completion side-effects) for processing.

---

## Recommended Build Order

| Phase | Sub-product      | Rationale                                                     |
| ----- | ---------------- | ------------------------------------------------------------- |
| 1     | **results**      | Immediate need; zero external dependencies; instantly useful  |
| 2     | **mlwh**         | Cached current-state MLWH access; library + REST service      |
| 3     | **mlwhdiff**     | Foundation for automation; thin change-tracking layer on mlwh |
| 4     | **notify**       | Small scope, quick to build, needed by later products         |
| 5     | **jobrun**       | Required before watchtower; independently useful              |
| 6     | **watchtower**   | Core automation — needs mlwhdiff + jobrun                     |
| 7     | **samplepicker** | Manual curation workflow; needs mlwh                          |

Each phase delivers working software. Phases 1–2 can proceed in parallel if
resources allow.

---

## Tech Stack (all sub-products)

### Backend & CLI

| Concern        | Choice                     | Rationale                                                                                                                    |
| -------------- | -------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| Language       | Go                         | Consistent with wr; single language for all backend logic                                                                    |
| CLI            | Cobra                      | De-facto Go CLI framework                                                                                                    |
| HTTP routing   | gin + go-authserver (gas)  | One standardised server stack across sub-products; gas adds optional JWT/Bearer auth + TLS (off by default, required across the HPC↔OpenStack boundary) |
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
| Testing    | Vitest                       | Unit tests for contracts and component logic; no browser required                                 |

Each sub-product with a web UI follows the same pattern: the Go backend exposes
a JSON API via gin, and a Next.js frontend consumes it through Server Actions.
Server Actions run on the Node.js server, so the Go API can live on a private
network — reducing attack surface and keeping credentials server-side. The
frontend is built and deployed as a standalone Node.js app alongside the Go
binary.

This stack keeps every sub-product simple to build, test, and deploy — no
message queues, SQLite for local development and testing, and a shared
institutional MySQL instance for production. Sub-products without a web UI of
their own (mlwh, mlwhdiff, notify, jobrun) are pure Go backends/CLIs with zero
frontend dependencies.
