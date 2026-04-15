# Results REST API — Feature Description

## What

Go backend REST API for the "results" sub-product (Pipeline Results Tracker)
described in `.docs/proposal.md`. This spec covers ONLY the Go backend REST
API and CLI — NOT the web UI.

A REST API and CLI for recording and searching pipeline outputs, providing a
central answer to "what datasets do we have, what work has been done on them,
and by whom?" Pipelines (or scripts, or humans) POST result metadata — output
paths, pipeline name, input files, exact command lines, sequence metadata, and
contacts (operator who ran the work, requestor who asked for it). The API
provides searchable, filterable results.

## Core Concept: Result Sets

A "result set" is a group of output files from a single pipeline run, stored
together with metadata. Each result set includes:

- **Output files:** all files in a containing directory (recursively), each with
  its file path and mtime.
- **User (requestor):** the person who requested the work.
- **Operator:** the person who actually ran the pipeline.
- **Command line:** the exact command used (e.g. the nextflow command).
- **Run identifier:** a Run ID or Sample name/ID that identifies this run.
- **Input files:** paths to input file(s) used (e.g. sample_sheet.tsv), each
  with mtime metadata.
- **Pipeline:** name/repo of the pipeline workflow, or a file path to the
  main.nf entry point.
- **Arbitrary key-value metadata:** additional user-defined metadata fields
  (e.g. library type, flags like `--do_spliced_products true`).

All file path entries (output files, input files, pipeline path) have mtime
metadata associated with them.

## Real-World Example

From the splicing analysis pipeline (see `.tmp/splicing_files.txt`):

Directory structure:
```
lehner_splicing/
  1_irods_to_lustre/   (user creates, runs irods_to_lustre to get fastqs)
  2_barcode_association/ (user creates barcode association files)
  3_splicing_analysis/
    input_files/
      <library_type>/      (e.g. random_exon, muta_exon, random_intron)
        <run_id>/           (e.g. run_48522, hek_22)
          sample_sheet.tsv
          barcodes_*.tsv.gz
          ref_*.fa
    output_files/
      <library_type>/
        <run_id>/
          run_log_*.txt        (contains the nextflow command)
          sample_sheet_*.tsv   (copy of input sample sheet)
          canonical_splicing_results/
            <sample>/
              *.canonical_barcodes.tsv
          splicing_counts/
            *.splicing_counts.tsv
          splicing_reports/
            <sample>/
              *.splicing_report.html
              *.corrected_psi.tsv
              *.junctions_category.tsv
          novel_splicing_results/
            <sample>/
              *.classified_junctions.tsv
              *.junctions.bed
              *.novel_barcodes.tsv
```

The operator runs a command like:
```
nextflow run -resume /lustre/.../nf_splicing/main.nf \
  -with-trace run_48522.nf_trace.txt \
  --sanger_module true \
  --sample_sheet /lustre/.../input_files/random_exon/run_48522/sample_sheet.tsv \
  --outdir /lustre/.../output_files/random_exon/run_48522 \
  --library random_exon \
  --do_spliced_products true
```

This creates all files under `output_files/random_exon/run_48522/` which form
a single result set. The result set metadata would be:
- user: the scientist who requested the analysis
- operator: the person who ran the nextflow command
- command: the full nextflow command line
- run_id: "run_48522"
- input_files: [sample_sheet.tsv path (with mtime)]
- pipeline: "/lustre/.../nf_splicing/main.nf" (with mtime, or commit hash if in a repo)
- output_files: all files recursively under output_files/random_exon/run_48522/
  (each with mtime)
- metadata: {"library": "random_exon", ...}

Library types are user-defined (e.g. muta_exon, random_exon,
random_HEK_MCF7_SHSY, random_intron). The run identifier within each library
type varies (e.g. run_48522, hek_22, I2, i6_4).

## Optional seqmeta Integration

The system should optionally integrate with the seqmeta component (specified in
`.docs/seqmeta/spec.md`, not yet implemented) to validate incoming sequence
metadata. When seqmeta is configured:
- Validate identifiers (study IDs, sample IDs, etc.) against the seqmeta
  validation endpoint before accepting result sets.
- If seqmeta is not configured or unavailable, skip validation and accept
  the data as-is (graceful degradation).

## CLI

A CLI (using Cobra) that makes it easy for a human or simple script to use
the REST API to:
- Register a new result set (providing all the metadata fields).
- Scan a directory to automatically discover output files and their mtimes.
- List/search existing result sets with filters.
- Get details of a specific result set.

The CLI should be practical for scripting — e.g. an operator can run a single
command after a pipeline completes to register all outputs.

## Notes

- Store every output and input file path + mtime in the DB at registration
  time, and provide a "rescan" endpoint to refresh the file list for staleness.
- Result set identity must NOT contain filesystem paths (directories may
  change if results are restored from backup to a new location). The key must
  be at least partially user-supplied. If the pipeline comes from a git repo,
  a commit hash can help identify the pipeline version, but the unique
  descriptor for the pipeline run's inputs must be operator-supplied (e.g.
  library type + Run ID or sample name — not file paths). For re-registration
  (upsert), the same user-supplied key updates the existing set. Validation
  of user-supplied identifiers is possible via seqmeta if the identifier is
  something seqmeta knows about (e.g. a Run ID); otherwise trust the operator.
- No authentication for the MVP (internal-network-only service).
- Search: query-string filter params on pre-defined fields AND on arbitrary
  operator-supplied metadata keys in a `meta_` namespace, with exact value
  matching. Example: `?pipeline_name=nf_splicing&pipeline_version=commithash
  &user=alice&operator=joe&meta_librarytype=muta_exon&seqmeta_runid=48522`.
- Package structure: `results/` package with CLI subcommands in a unified
  `wa` binary (e.g. `wa results register ...`, `wa results search ...`),
  not a separate binary. The main binary lives in `cmd/` or project root.
- Result set natural key: operator supplies `--nextflow_workflow /path/to/main.nf
  --runid 48522 --additional_unique random_exon`. The CLI can detect if main.nf
  is in a git checkout and auto-derive pipeline_repo_url, pipeline_name,
  pipeline_version (commit hash). The system builds a consistent composite key
  like `(pipeline_repo_url, "seqmeta:runid=48522&unique=random_exon")`.
  The key is a (pipeline_identifier, run_key) pair where pipeline_identifier
  is either a repo URL or a normalised path, and run_key is a structured
  string built from operator-supplied identifiers. The CLI also auto-fills
  properties like pipeline_name, pipeline_version, and seqmeta_runid when
  derivable. Re-registration with the same key performs an upsert.
- Database: use `database/sql` with dialect-aware raw SQL, accepting a
  `*sql.DB`. Single implementation handles both SQLite and MySQL.
- Rescan is client-side: the CLI re-scans the directory locally and PUTs
  the updated file list. The server never touches the filesystem directly.
- No pagination for MVP. Search returns all matching result sets (metadata
  only). File lists fetched separately via `GET /results/{id}/files`.
- Upsert + delete. No partial updates (PATCH) for MVP.
- Create the unified `wa` Cobra root binary AND migrate the existing seqmeta
  CLI into it as part of this feature. Results becomes the first new
  subcommand tree (`wa results register ...`), and seqmeta moves in too
  (`wa seqmeta ...`).
- Result set ID in REST URLs is a deterministic SHA256 hash of the composite
  natural key (pipeline_identifier + run_key). Stable, URL-safe, no need
  for server-side auto-increment.
- SQL strategy: write a compatible SQL subset that works unmodified in both
  SQLite and MySQL. No dialect switching, no query builder library.
- Registration is a single atomic POST with metadata + full file list.
- Seqmeta validation happens server-side during POST. Config via
  `--seqmeta-url` flag on the server; empty means skip validation.
- Fields with a `seqmeta_` prefix in metadata are validated against the
  seqmeta validation endpoint. Other metadata fields are not validated.
- The root main.go saga inspector tool migrates into the unified wa binary
  as well (e.g. `wa saga inspect <identifier>`), so the repo has one binary.
- Seqmeta validation is strict: invalid data returns 422; unreachable seqmeta
  returns 502. No lenient/warning mode.
- Store path + mtime + size (bytes) for every file (output and input).
- DELETE is hard delete (rows removed permanently, no soft delete for MVP).
- Rescan scope: output files only. Input files and pipeline path are
  historical facts about the run and are immutable after registration.
- Seqmeta validation does type-matching: `seqmeta_runid` must resolve as
  `run_id` in seqmeta, not just any known identifier. Catches operator typos.
- CLI registration flags: --user, --operator, --command (string flags),
  repeatable --input-file (CLI auto-fills mtime/size via os.Stat), repeatable
  --meta key=value pairs. Also support --json to read full registration
  payload from stdin (for programmatic callers). Output directory is a
  positional argument. Key-building flags: --nextflow_workflow, --runid,
  --additional_unique.
- Directory scanning: follow symlinks (record target's mtime/size), exclude
  hidden files/dirs by default (--include-hidden to override), warn above
  10,000 files (no hard limit for MVP).
- Rescan PUT semantics: full replacement. PUT body is the complete current
  file list; absent files are removed from DB. No per-file history tracking.
- Must support the possibility of a mass migration of result file locations. Ie.
  we must be able to search for result output directories that are decendent
  directories of a given directory, and then upsert them at their new location
  relative to a new parent output directory, without having to know anything
  else about them. Migration CLI does not need to be implemented in MVP, but
  database and search must support the possibility.
- Database schema: normalized separate tables — result_sets (composite key
  fields + SHA256 ID), result_files (result_id + path + mtime + size),
  result_metadata (result_id + key + value for arbitrary meta_* fields).
- Git auto-detection fallback: if pipeline file is in a git checkout, extract
  repo URL + commit hash for pipeline_identifier and pipeline_version. If not
  in git, use the file's normalised absolute path as pipeline_identifier and
  a SHA256 hash of the file's content as the version. Always succeeds — never
  fails registration.
- Seqmeta timeout: synchronous blocking call with configurable timeout;
  if seqmeta is unreachable or times out, fail registration with HTTP 502.
  Strict only, no lenient/warning mode.
- Metadata search: exact-match only on meta_* fields. No wildcards,
  substring matching, or composite operators for MVP.
- File list delivery: GET /results/{id}/files returns a full JSON array
  response. No streaming or pagination for MVP.
- Concurrent registration: last-write-wins. If two requests register the
  same composite key simultaneously, whichever completes second overwrites
  the first atomically. No conflict detection for MVP.
- No hard limit on file list size. GET /results/{id}/files returns all files
  regardless of count. CLI warns above 10,000 but no server-side cap.
- Symlink cycles: during directory scanning, detect and silently skip cyclic
  symlinks with a logged warning. Continue scanning non-cyclic paths.
- Pipeline version without git: compute SHA256 hash of the pipeline file's
  content at registration time. This is stable across file migrations
  (unlike mtime). Update the git fallback: use normalised path as
  pipeline_identifier and content hash as pipeline_version.
- REST endpoints: RESTful resource paths — POST /results (upsert),
  GET /results (search), GET /results/{id} (detail),
  GET /results/{id}/files (file list), PUT /results/{id}/files (rescan),
  DELETE /results/{id}.
- All pre-defined result set fields are searchable via query params: user,
  operator, pipeline_name, pipeline_version, pipeline_identifier, run_key.
  Also searchable: seqmeta_* namespace and meta_* namespace, all exact match.
- CLI subcommands under `wa results`: register, search, get, delete, rescan.
  Each maps 1:1 to a REST endpoint.
- Entry point: root main.go defers to a cmd/ package containing eg.
  results.go for each subcommand tree. results/ package parallel to saga/
  and seqmeta/. Server flags: --db (DSN), --port, --seqmeta-url.

## Tech Stack

Per `.docs/proposal.md`:
- Go backend with chi router
- SQLite for tests/local dev, MySQL for production
- GoConvey testing
- Cobra CLI
