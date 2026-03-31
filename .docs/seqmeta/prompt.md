# Feature: Sequence Metadata Cache (seqmeta)

## Overview

Build a CLI and REST API (`seqmeta` package) built on the existing `saga`
library that adds "what's new since last check" diffing over study/sample/
library/run metadata and iRODS file paths. Stores watermarks per query in a
local SQLite database so consumers can efficiently poll for new data without
re-processing everything. Delegates all MLWH and iRODS queries to the `saga`
package, focusing solely on change detection, watermark storage, and exposing a
stable polling API.

It should also expose an ability to validate a string as a valid sequencing
identifier of some kind (e.g. study ID, sample ID/Sanger ID, run ID, library
type, etc.).

## Context

This is sub-product 3 ("seqmeta") from the workflow automation proposal. It
builds on the `saga` package already implemented in this repository. The `saga`
package provides typed access to MLWH study, sample, library, and run metadata
and iRODS file paths via the SAGA REST API.

## Key Requirements

### Identifier Validation

Provide a function or endpoint that accepts a string and determines whether it
is a valid sequencing identifier, and if so what kind (study ID, Sanger sample
ID, run ID, etc.). This is useful for downstream tools that receive
user-supplied identifiers and need to validate/classify them before querying.

### Change Detection / Diffing

Given a query (e.g. "all samples for study X"), compare the current result from
saga against the last-seen state stored locally, and return only what has
changed (new, modified, or removed entries).

### Watermark Storage

Persist per-query watermarks in a local SQLite database so that repeated polls
resume from where they left off rather than re-fetching everything.

### REST API

Expose the diffing and validation functionality via a REST API (chi router) so
that downstream services (watchtower, samplepicker) can poll for changes over
HTTP.

### CLI

Expose the same functionality via a Cobra CLI for ad-hoc use and scripting.

## Tech Stack

- Go (same as saga)
- SQLite via pure Go driver (embedded, zero-ops, in-memory for tests)
- chi for HTTP routing
- Cobra for CLI
- GoConvey for testing
- saga package for all upstream SAGA API access

## Architecture

- New `seqmeta/` package (not inside `saga/`)
- Depends on the `saga` package via its exported interface
- All saga interaction goes through a mockable interface so seqmeta can be
  tested without a real SAGA instance
- SQLite database for watermark persistence
- Server mode for REST API, single-shot mode for CLI queries

## Notes

- Identifier validation supports the extended set: Study ID (IDStudyLims),
  Sanger Sample ID, IDSampleLims, Run ID, Library Type, sample accession
  number, study accession number, and project name/ID.
- Change detection uses hash-based comparison: SHA256 of JSON-marshalled
  entries. The hash is stored as the watermark; entries are flagged as changed
  when the hash differs.
- seqmeta is a generic diffing layer over all existing saga client methods, not
  limited to a subset. Any saga call that returns data can be tracked for
  changes.
- Watermark storage uses fine-grained per-entity tracking: one SQLite row per
  result entry with (query_hash, entry_id, entry_hash, timestamp). This enables
  per-entity change detection (new, modified, removed).
- REST API uses resource-based paths: GET /diff/study/{id},
  GET /diff/sample/{id}, GET /validate/{identifier}, with responses containing
  added/modified/removed arrays.
- Identifier validation uses live SAGA lookup: call relevant saga endpoints to
  check existence; the identifier type is whichever endpoint succeeds. No regex
  pre-filtering.
- Entry identity for per-entity watermarking uses a single canonical field per
  type: Study → IDStudyLims, Sample → SangerID, iRODS file → collection path,
  etc.
- CLI uses Cobra subcommands: `seqmeta diff --study <id>`,
  `seqmeta validate <id>`, `seqmeta serve --port 8080`.
- Saga dependency uses a curated minimal interface defined in the seqmeta
  package, containing only methods seqmeta actually calls. This makes testing
  easy and decouples seqmeta from saga internals.
- SQLite driver is modernc.org/sqlite (most popular pure-Go driver).
- Query identity is logical: endpoint + entity ID only. Filters and pagination
  variants do not affect the query hash.
- Removed entries are kept as tombstones in the watermark table indefinitely.
  When a previously-seen entry_id no longer appears in results, it is flagged
  as removed in the diff response and remains in the DB as a tombstone.
- Diff responses return full objects (complete Study/Sample/File structs); the
  watermark DB stores only hashes, not full payloads.
- Single-process access only; no concurrency safeguards needed for SQLite.
- No separate watermark pruning or cleanup policy is needed.
- On the first poll for a query (no existing watermark), all current results are
  returned as "added" entries.
- Identifier validation returns both the type classification and the full
  matched object (e.g. the complete Study or Sample struct).
- Polls fail atomically: if any part of a saga query fails, store nothing in
  the watermark DB. The next poll retries from scratch.
