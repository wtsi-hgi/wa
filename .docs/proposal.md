# Workflow Automation: Sub-Product Proposal

## Overview

The workflow automation system is decomposed into six independent sub-products.
Each is individually useful, independently deployable, and communicates with the
others via HTTP APIs. No sub-product has a compile-time dependency on another —
if a dependency isn't running, the caller degrades gracefully (e.g. logs instead
of emailing).

All sub-products share a common Go tech stack for consistency, testability, and
maintainability.

---

## Sub-Products

### 1. results — Pipeline Results Tracker

**Standalone value:** Any pipeline team can register and browse output files
today, with no other sub-products required.

A REST API and web UI for recording and searching pipeline outputs. Pipelines
(or scripts, or humans) POST result metadata — output paths, pipeline name,
input files, exact command lines, sequence metadata. The web UI provides
searchable, filterable results with clickable file paths that render HTML
inline, display CSVs as tables, and transparently decompress gzipped files.

### 2. saga — SAGA API Client Library

**Standalone value:** Any Go service or tool that needs sequencing metadata or
iRODS paths can import this library directly, with no SAGA API boilerplate.

A Go library wrapping the SAGA REST API, providing typed access to MLWH study,
sample, library, and run metadata and iRODS file paths. Handles authentication,
HTTP retries, and in-process response caching to avoid hammering the upstream
API. Exposes a clean, mockable interface so downstream code can be tested
without a real SAGA instance.

### 3. seqmeta — Sequence Metadata Cache

**Standalone value:** Replaces ad-hoc SAGA API integrations for anyone who needs
change-tracking and efficient polling over sequencing metadata.

CLI and REST API built on the saga library that adds "what's new since last
check" diffing over study/sample/library/run metadata and iRODS file paths.
Stores watermarks per query in a local SQLite database so consumers can
efficiently poll for new data without re-processing everything. Delegates all
MLWH and iRODS queries to saga, focusing solely on change detection, watermark
storage, and exposing a stable polling API.

### 4. notify — Notification Service

**Standalone value:** Generic email notification service usable by any internal
tool.

Accepts notification requests (recipient, template, data) via API or CLI and
sends templated emails via the institutional SMTP relay. Includes rate limiting
and deduplication to prevent spam from flapping jobs. Extensible to Slack/Teams
in future.

### 5. jobrun — Job Submission & Monitoring via wr

**Standalone value:** Programmatic wr/LSF job submission and tracking, useful
for any tool that needs to run jobs on the cluster.

Go library and CLI that wraps wr to submit individual commands or nextflow
pipelines (bsub'd to the oversubscribed queue) and poll for completion. On job
completion or failure, can POST results to the results tracker and/or fire a
webhook.

### 6. watchtower — Watch Configuration & Trigger Engine

**Standalone value:** Generic "watch for new sequencing data and do something"
automation engine.

The core automation product. Users register watches: "when a sample cram appears
for study X, create a result subdirectory, download it, generate a pipeline
config from a template, and submit the pipeline via jobrun." Runs as a daemon
polling seqmeta, with idempotent trigger tracking to prevent re-runs. Includes a
web dashboard showing watches, triggered runs, and their statuses.

### 7. samplepicker — Sample Selection Web UI

**Standalone value:** Scientists can browse available samples, curate subsets,
add metadata, and export selections as JSON/TSV for use with any tool.

Web app for manually selecting samples from seqmeta results, annotating them
with additional metadata, and either exporting the selection or submitting it to
watchtower for pipeline execution. Supports importing supplementary metadata
from spreadsheets.

---

## How They Fit Together

```
                    ┌──────────┐
                    │   saga   │
                    └────┬─────┘
                         │
                    ┌────▼─────┐
                    │ seqmeta  │
                    └────┬─────┘
                         │
           ┌─────────────┼──────────────┐
           ▼             ▼              ▼
    ┌────────────┐ ┌───────────┐ ┌──────────────┐
    │samplepicker│ │watchtower │ │   results    │
    └────────────┘ └─────┬─────┘ └──────────────┘
                         │
                    ┌────▼─────┐
                    │  jobrun  │
                    └────┬─────┘
                         │
                    ┌────▼─────┐
                    │  notify  │
                    └──────────┘
```

The typical automated flow:

1. **seqmeta** polls **saga** for current MLWH/iRODS state and detects new data against stored watermarks
2. **watchtower** matches it against registered watches and triggers the
   configured action
3. **jobrun** submits the pipeline to LSF via wr
4. On completion, **jobrun** registers outputs with **results**
5. **notify** emails the requester at each stage

The manual flow: a user browses samples in **samplepicker**, selects a subset,
and submits it to **watchtower** (or directly to **jobrun**) for processing.

---

## Recommended Build Order

| Phase | Sub-product | Rationale |
|-------|-------------|-----------|
| 1 | **results** | Immediate need; zero external dependencies; instantly useful |
| 2 | **saga** | Library dependency for seqmeta; wraps the SAGA API |
| 3 | **seqmeta** | Foundation for automation; builds on saga for MLWH/iRODS data |
| 4 | **notify** | Small scope, quick to build, needed by later products |
| 5 | **jobrun** | Required before watchtower; independently useful |
| 6 | **watchtower** | Core automation — needs seqmeta + jobrun |
| 7 | **samplepicker** | Manual curation workflow; needs seqmeta |

Each phase delivers working software. Phases 1–2 can proceed in parallel if
resources allow.

---

## Tech Stack (all sub-products)

| Concern | Choice | Rationale |
|---------|--------|-----------|
| Language | Go | Consistent with wr; single language across everything |
| CLI | Cobra | De-facto Go CLI framework |
| HTTP routing | chi | Lightweight, idiomatic |
| Web UI | Go html/template + htmx | Server-rendered, no JS build step, one language end-to-end |
| Database | SQLite (pure Go driver) | Embedded, zero-ops, in-memory for tests |
| Job submission | wr Go client library | Native integration with LSF |
| Testing | GoConvey + interface mocks | BDD-style tests; all external deps behind interfaces |
| Email | net/smtp | Standard SMTP to institutional relay |

This stack keeps every sub-product simple to build, test, and deploy — no
message queues, no JS toolchains, no external database servers.
