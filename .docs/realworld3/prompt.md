# Feature: fast study→iRODS TSV export, iRODS recency, correct prefix/contains/exact search, manual-QC & target-deliverable filtering, and global run aggregation (MLWH realworld3)

## Summary

A third wave of real user questions must become **fast (web-responsive, ideally <1s,
and never a silent truncation), cheap (one call, bounded response), and CORRECT
without asking the caller to hand-write SQL**. These extend the "realworld" (API
1.6.0) and "realworld2" (API 1.7.0) work already merged. The questions, taken from
real agent transcripts that currently fail, are slow, or give wrong answers:

1. **"Write a TSV of the iRODS cram files for study X with columns
   `[supplier_sample_name, study_accession_number, sanger_sample_id, manual_qc,
   irods_path]`, primary/target deliverables only."** — today there is no
   column-selectable TSV path; the closest (`wa mlwh manifest --with-irods`) has no
   `manual_qc`, no target filter, no TSV/column control, and is **~3 s for a large
   study** (below). This is the flagship deliverable and must get a **dedicated,
   very fast `wa mlwh` subcommand**.
2. **"Give me all samples that start with `hek_r`."** — should return the **4**
   samples whose supplier name literally starts with `Hek_R` (`Hek_R1`..`Hek_R4`).
   Today `/search/sample/hek_r` returns **55**, because sample search is a
   word-prefix AND across four fields.
3. **"Samples whose name contains `<substring>`."** — a true "contains" search.
   Today `/search/sample/usculus` returns **0**, even though **237,879** samples have
   `common_name` containing "usculus" (Mus musculus), because word-prefix cannot
   match a substring inside a word.
4. **"The most recently sequenced sample / the latest iRODS data for study (or lab)
   X."** — one call returning the row(s) behind the recency, with the iRODS `created`
   timestamp, sorted newest-first. Today `IRODSPath` omits `created` and the listings
   sort by product id, so recency is unanswerable from a path listing.
5. **"Plot runs per month by manufacturer and platform for the last 3 years."** — one
   aggregate call. Today there is no global/grouped run endpoint at all.

Two cross-cutting rules run through all of the above:

- **No "figure out your own SQL" escape hatch as the answer.** Each of these must be
  a first-class, correctly-scoped, indexed endpoint whose semantics (manual QC,
  target/deliverable, "cram", recency, run date basis, prefix-vs-contains) are baked
  into the server and stated in the registry description — not left to a caller/LLM
  to reconstruct in ad-hoc SQL, where it silently gets the target rule wrong, caps at
  a page, or picks the wrong timestamp.
- **Correct the mirror where it is wrong or slow.** Comparing our cache to a
  source-direct implementation surfaced concrete schema/query defects (missing
  discriminators, a mis-typed join key, an un-surfaced timestamp, big-study
  slowness). Fix them here.

This is the **upstream half**. A companion downstream feature in the `mlwh-mcp-server`
repo (`.docs/realworld3/`) wraps these endpoints as MCP tools; keep the registry
`Description`/`Summary` text accurate and self-documenting because the MCP layer
surfaces it verbatim. **Additionally, every new capability must be exposed through the
`wa mlwh` CLI** (see "CLI exposure").

**Scope rule: everything below is firm and in scope to build.** The "Design decisions"
and "Notes" sections settle _how_, never _whether_.

## Authority, and what already landed (reuse — do NOT rebuild)

The only authority is **this repo's Go code** (`mlwh/registry.go`, `mlwh/types.go`,
`mlwh/manifest.go`, `mlwh/hierarchy.go`, `mlwh/search.go`, `mlwh/people.go`,
`mlwh/availability.go`, `mlwh/progress.go`, `mlwh/count.go`, `mlwh/sync.go`,
`mlwh/sync_platform_coverage.go`, `mlwh/cache.go`, `mlwh/cache_schema/{sqlite,mysql}/*.sql`,
`mlwh/remote.go`, `mlwh/server.go`, `mlwh/openapi.go`, and the `cmd/mlwh_*.go`
commands). The `.docs/realworld*` specs are background only — verify against code.
**Current MLWH API version: 1.7.0** (`mlwh/openapi.go` `APIVersion`); **current
`CacheSchemaVersion`: 12** (`mlwh/cache.go`).

Already merged and **reused as-is** (confirm in code before relying on it):

- **`Study`/`study_mirror`** carry `id_study_lims`, `name`, `accession_number`,
  `study_title`, `faculty_sponsor`, `data_access_group`, `programme`, `state`, etc.
  Indexed: `id_study_lims`, `uuid_study_lims`, `accession_number`, `name`,
  `faculty_sponsor`.
- **`Sample`/`sample_mirror`** carry `name`, `sanger_sample_id`, `supplier_name`,
  `accession_number`, `donor_id`, `common_name`, `taxon_id`, `description`. Indexed:
  those text columns individually (ci collation).
- **`sample_search_token`** (derived word-token index; `(token, id_sample_tmp)`)
  backs `/search/sample` word-prefix matching.
- **`seq_product_irods_locations_mirror`** already denormalises `id_iseq_product`,
  `irods_root_collection`, `irods_data_relative_path`, `irods_collection`,
  `irods_file_name`, **`id_sample_tmp`**, **`id_study_lims`**, `last_updated`,
  **`created`** (nullable), `platform`; indexed incl. `(id_study_lims, created)`,
  `(id_study_lims, id_iseq_product)`, `(id_sample_tmp)`.
- **`iseq_product_metrics_mirror`** mirrors `id_iseq_product`, `id_iseq_flowcell_tmp`,
  `id_run`, `position`, `tag_index`, `id_sample_tmp`, `id_study_lims`, **`qc`**,
  `qc_lib`, `qc_seq`.
- **`StudyManifest`** (`/study/:id/manifest`, `with_irods`, `file_type`) +
  `CountStudyManifest`; **`/study|sample|run/:id/irods`** (+ counts, `file_type`);
  **`StudyOverview`/`StatusBreakdown`/`SampleProgress`/`RunOverview`/`RunStatus`**;
  people→studies (`/studies/faculty-sponsor/:name`, `/studies/user/:person`,
  `/resolve-person/:term`); the QC roll-up (`qc.go`: `qc` 1/0/NULL → pass/fail/pending,
  precedence fail > pending > pass).
- CLI already has `wa mlwh {info,search,irods,manifest,studies,sync,serve}`.

## Verified facts (checked against the live source `mlwarehouse` and our MySQL mirror)

These were confirmed directly, so the work is grounded, not guessed. **Cite these in
the spec; do not silently contradict them.**

- **There is NO scalar `target` column in MLWH.** `iseq_product_metrics` has a family
  of `target_*`/`mean_bait_target_coverage` columns, but every one is a **bait/capture
  coverage metric** (`target_length`, `target_mapped_reads`,
  `target_percent_gt_coverage_threshold`, …), NOT a target-vs-control flag. The
  "target=1" a user means is the **iRODS metadata AVU** on the CRAM (the primary
  deliverable, excluding PhiX/controls and non-primary sub-products) — it is not a
  warehouse column. In the warehouse, the discriminators are: `iseq_flowcell.entity_type`
  (values incl. `library`, `library_control`, `library_indexed`, `library_indexed_spike`),
  `iseq_flowcell.is_spiked` / `spiked_phix_*`, and (Element/Ultima)
  `{eseq,useq}_product_metrics.is_sequencing_control`; product structure is in
  `iseq_composition_tmp` / `iseq_product_components`. **The mirror does not currently
  mirror `iseq_flowcell` as a table at all** (only the derived `library_samples`), so
  it presently CANNOT exclude controls/spikes.
- **`manual_qc` = `iseq_product_metrics.qc`** (`tinyint(1)`; **1 = pass, 0 = fail,
  NULL = pending/undecided**; DBIx comment: "Overall QC assessment outcome, a logical
  product of qc_seq and qc_lib …"). Already mirrored as `qc`. `qc_seq`, `qc_lib`,
  `qc_user` also exist upstream (`qc_user` is NOT currently mirrored). Per-platform:
  `pac_bio_product_metrics.qc`, `eseq_product_metrics.qc`, `useq_product_metrics.qc`
  (same 0/1/NULL); ONT (`oseq_flowcell`) has no product/qc.
- **`seq_product_irods_locations`** has `created` AND `last_changed` (both `datetime`),
  and **NO `id_run`, sample, study, or file-type column**; its unique key is
  `(irods_root_collection, id_product)`, so **`id_product` is not unique** — one
  product can have several iRODS rows. Its `id_product varchar(64)` joins to
  `iseq_product_metrics.id_iseq_product char(64)` (the SHA256 product id).
- **Mirror type/key defects:** `iseq_product_metrics_mirror.id_iseq_product` is
  `varchar(255)` (source is `char(64)`) and the mirror table has **no PRIMARY KEY**
  (only a secondary `KEY` on `id_iseq_product`). Joining the 7.3 M-row iRODS mirror to
  it on this long ci-collated varchar is the root cause of the big-study manifest
  slowness below.
- **`IRODSPath` omits `created`.** The column is mirrored and indexed
  (`(id_study_lims, created)`), used only by the aggregates (`StudyOverview`
  `newest_data_added`, since/until). The three iRODS list queries select neither
  `created` nor order by it — they `ORDER BY id_iseq_product`. So a path listing
  cannot answer "latest".
- **Measured performance (source-direct vs our mirror vs current HTTP endpoint),
  2026-07-02:**
  | Query | small study 7556 | big study 7699 |
  | --- | --- | --- |
  | cram manifest — source-direct 5-table join | 0.07 s | **1.7 s** |
  | cram manifest — raw mirror join (as shaped today) | 0.07 s | **15.8 s** |
  | cram manifest — current `GET /study/:id/manifest?with_irods&file_type=cram` (page 1) | 0.07 s | **3.04 s** |
  | `GET /study/:id/overview` | fast | **3.04 s** |
  | `GET /study/:id/status-breakdown` | fast | **3.24 s** |

  Study 7699 has ~52 k cram iRODS rows / **102,763** product×irods manifest rows.
  So the flagship listing and the two "one-call" aggregates all **fail the ~1 s
  target at study scale today**, and the mirror-shaped manifest join is *slower than
  the source*.
- **Where the mirror genuinely wins** (denormalised `id_study_lims` → single indexed
  scan, no join): samples-with-data count for 7699 = **0.10 s (mirror) vs 1.05 s
  (source join)**; overview iRODS aggregate (count + MIN/MAX(created)) = **0.085 s vs
  0.93 s**; runs-for-study = **0.11 s vs 0.34 s**. These are the queries that justify
  the mirror; keep and extend that pattern, and make the compound endpoints actually
  realise it (they don't, at 3 s).
- **Search reality:** `_` and `%` are already correctly escaped (`escapeLIKELiteral`,
  `ESCAPE '!'`) — there is NO underscore-wildcard bug. The problems are semantic:
  `/search/sample/:term` is **word-prefix AND across `name`/`supplier_name`/`common_name`/`donor_id`**,
  so `hek_r` → `{hek…} AND {r…}` → **55** rows (e.g. `HEK293T_D2_R1`, and a sample
  named `HGRNA…` whose supplier is `10X_Automation_HEK` with donor `RD_…`), burying
  the 4 the user wanted. There is **no literal-whole-value prefix mode** and **no
  substring/contains mode**, and the 3-char free-text minimum blocks controlled short
  tokens like `gt`. Study/person search uses leading-wildcard `%term%` (full scan of
  the small `study_mirror`/`study_users_mirror`; acceptable there, but not a model to
  copy onto large tables).

## Per-question verdict

| Q | Question | Verdict | What's needed |
| --- | --- | --- | --- |
| 1 | study cram TSV w/ chosen columns + manual_qc + target, fast | **GAP + PERF** | D1: dedicated fast TSV subcommand + backing export path; D4 manual_qc/target |
| 2 | "starts with hek_r" → 4 | **WRONG** | D3: literal-prefix mode |
| 3 | "contains \<substring\>" | **WRONG (0 results)** | D3: indexed contains mode |
| 4 | most-recent sample / latest iRODS for study/lab | **GAP** | D2: expose `created`, recency sort, latest endpoints |
| 5 | runs/month by manufacturer & platform | **GAP** | D5: global run aggregation |
| — | big-study aggregates <1 s; per_platform=null bug | **PERF/BUG** | D6 |

## Deliverables (all firm)

### D1 — Fast study→iRODS TSV export (the flagship; dedicated `wa mlwh` subcommand)

Add a **dedicated `wa mlwh` subcommand** whose single job is: *produce a TSV of a
study's iRODS data files, with the caller's chosen columns, filtered to a file type
and to primary/target deliverables, very fast.* Recommended name **`wa mlwh tsv
<study>`** (the spec may pick `export`/`files`, but it must be a distinct, purpose-built
command, not a `--format` flag bolted onto the generic `manifest`). Requirements:

- **Column selection.** A `--columns` (ordered, comma-separated) selector over a
  documented vocabulary, at minimum: `supplier_name` (accept the alias
  `supplier_sample_name`), `sanger_sample_id`, `name` (sample name),
  `study_accession_number`, `id_study_lims`, `manual_qc`, `id_run`, `lane`
  (position), `tag_index`, `platform`, `irods_path` (the full path =
  `CONCAT(irods_root_collection, '/', irods_data_relative_path)`). Default column set
  should cover the flagship query (Q1). Output is real TSV: a header row then
  tab-separated rows, deterministic order (settle: by `id_run, lane, tag_index,
  name`), stable across pages.
- **Filters.** Required study; `--file-type` (default `cram`, filename-suffix
  semantics as elsewhere); **target/deliverable-only** (D4) **on by default**, with a
  flag to include controls/sub-products. `manual_qc` should be filterable
  (`--qc pass|fail|pending`) so "target=1 AND qc pass" is one switch, but filtering
  and the returned column are independent (Q1 wanted the column, not necessarily the
  filter).
- **Speed: <1 s for a bounded page even for the largest studies** (7699-scale). The
  first page must be web-responsive; a `/count` counterpart and sizing headers are
  required. **No silent row cap**: a full-study export MUST be able to stream/emit
  *every* matching row (7699 → 100 k+ rows) via **keyset pagination** (not `LIMIT
  OFFSET`, which degrades on deep pages) or a streaming response. State explicitly in
  the CLI when output is a bounded page vs the complete set.
- **Fix the perf.** Rebuild the backing query/schema so the export is a
  near-single-index-ordered scan:
  - Give `iseq_product_metrics_mirror` a real **PRIMARY KEY** and change
    `id_iseq_product` to **`char(64)`** (match source) so the iRODS→product join is a
    fixed-width key lookup, not a `varchar(255)` ci comparison.
  - Add the composite index(es) the export orders/filters on (e.g. covering
    `(id_study_lims, id_run, position, tag_index)` on the iRODS mirror or a purpose
    table). Prove every path is index-served with **EXPLAIN** (no full scans of the
    9 M/7.3 M mirrors, no per-row correlated subqueries — same discipline as the
    per-platform-breakdown fix).
  - If a clean single-scan is not achievable over the current tables, add a
    **denormalised deliverable/export structure** (a mirror table or materialised
    projection keyed by `(id_study_lims, id_run, position, tag_index)` carrying
    supplier/sanger id/accession/qc/target-flag/irods path) populated during sync, so
    the TSV is one ordered range scan. Decide in the spec; whichever route, the target
    is <1 s per page at 7699 scale and correct totals.
- **Correctness.** `manual_qc` is the `qc` roll-up per product (D4). `irods_path`
  concatenation must be verified against real paths (e.g. `.../lane6/plex45/51945_6#45.cram`).
  A study's target cram row count must match the known deliverable count (7556 → 886).

### D2 — iRODS recency ("latest data" / "most recently sequenced")

- **Add `created` to `IRODSPath`** (RFC3339 UTC; the column is already mirrored). Keep
  the "added to iRODS" wording discipline (it is `created`, never `last_changed`).
- **Recency ordering + window** on the iRODS list endpoints: an `order_by=created_desc`
  (and the default stays as-is) plus `since`/`until` (half-open `[since, until)` over
  `created`, matching `SamplesWithData`). The study-scoped case is already
  index-served by `(id_study_lims, created)`; add sample-/run-scoped recency support
  and index as needed.
- **A "latest data" path** so "the most recently sequenced sample for study/lab X" is
  one call, returning the actual row(s): `created`, `irods_path`, `id_study_lims`,
  study name, sample `name`, `supplier_name`, `id_run`, lane/tag, platform — sorted
  `created DESC`, returning all rows tied at the max (or a documented tie-break).
  Support a **faculty-sponsor-scoped** variant (join `study_mirror.faculty_sponsor`)
  so "latest data for the Anderson lab" does not require the caller to fan out over 91
  studies and then fail to bridge the timestamp back to a row (the exact realworld
  trap). Decide the cheap cross-study path (the `(id_study_lims, created)` index is
  per-study; a small per-study MAX then merge may beat a global filesort — settle with
  EXPLAIN).
- Reconcile `StudyOverview.newest_data_added` with this: it must be **followable** to
  the row(s) that produced it (today it reports a max that `samples-with-data` windows
  could not reproduce). Document the join basis.

### D3 — Correct, fast search: prefix, contains, and exact as first-class distinct modes

Make search intent explicit and self-documenting so the MCP layer/agent chooses
correctly, and so none of it depends on the caller crafting `LIKE` patterns.

- **Literal whole-value prefix.** "Starts with `hek_r`" must return **exactly** the
  samples whose selected field (name and/or supplier_name — settle which fields, and
  whether per-field) literally starts with `hek_r` (the **4** `Hek_R1..4`), fast — an
  index range seek on the ci text columns (`col LIKE 'hek!_r%' ESCAPE '!'`), NOT the
  word-prefix AND. This is a **distinct mode** from the existing word-prefix search
  (keep that available; it is useful, just not what "starts with" means).
- **True contains / substring.** "Contains `usculus`" must match the substring
  anywhere, including mid-word (→ 237,879 for Mus musculus), and must be
  **web-responsive on ~1.9 M samples** — a naive leading-wildcard `%term%` full-scans
  and is not acceptable at that scale. Provide an **indexed** path: an n-gram/trigram
  token table populated during sync (mirroring the `sample_search_token` approach but
  for substrings), MySQL `FULLTEXT`, or an equivalent — settle the mechanism in the
  spec and prove it index-served with EXPLAIN. Bound it (a count cap like the existing
  `sampleSearchCountCap`).
- **Exact / short controlled tokens.** Allow an **exact** lookup (already exists for
  the `find/sample/*` fields) to be reachable for controlled short tokens like `gt`
  that the 3-char free-text minimum blocks (e.g. exact `library_type=gt`, exact
  tag/library ids). Keep the 3-char minimum only on the free-text prefix/contains
  paths, and say so.
- **Expose the mode** via CLI (`wa mlwh search --prefix|--contains|--exact`, or
  scope-specific subcommands) and via endpoint/param, with the registry description
  stating precisely what each does and over which fields. Do NOT overload one endpoint
  such that the caller must know to add `%`/anchors.

### D4 — manual_qc & target-deliverable semantics (feeds D1; also the manifest)

- **Expose `manual_qc`** (the `qc` roll-up: pass/fail/pending, or the raw 1/0/NULL —
  settle the surface form and document it) wherever product/iRODS rows are listed:
  the D1 TSV, `StudyManifest` rows, and optionally the iRODS listings. Reuse
  `qc.go`'s roll-up so it can never disagree with `SampleProgress.qc` /
  `StatusBreakdown`.
- **Define and implement a correct "primary/target deliverable" filter** in the
  server (not in caller SQL). It must exclude spiked-in PhiX and control products and
  non-primary sub-products. Use warehouse-native discriminators — mirror the needed
  `iseq_flowcell` columns (`entity_type`, `is_spiked`, and enough of the composition/
  control signal) and the Element/Ultima `is_sequencing_control` — so the filter is a
  **fast indexed column**, not a runtime heuristic. **Verify** the definition against
  real studies so the target cram count matches the known deliverable count (7556 →
  886) and controls are actually dropped where present.
- **Document the semantics** in the registry text: that `iseq_product_metrics.target_*`
  are coverage metrics, that there is no scalar warehouse `target` column, that the
  authoritative ground truth is the iRODS `target=1` AVU which this filter
  approximates, and exactly what "deliverable-only" includes/excludes. If capturing
  the true iRODS AVU is feasible within the sync, prefer it and say so; otherwise
  state the approximation and its known edge cases.

### D5 — Global run aggregation (runs per month by manufacturer & platform)

- **A grouped-count endpoint** answering "runs per month by manufacturer and platform"
  in one call over a `since`/`until` window: rows of `{month, manufacturer, platform,
  count, date_basis, cache_synced_at}`, across all platforms (Illumina/Element/
  Ultima/PacBio/ONT). **Manufacturer** is derived from platform (Illumina→Illumina,
  Elembio→Element Biosciences, Ultimagen→Ultima Genomics, PacBio→PacBio,
  ONT→Oxford Nanopore); state the mapping. **`date_basis`** must be defined per
  platform (e.g. Illumina/Element "run complete", Ultima/PacBio "run archived"/well
  complete, ONT best-available) — reuse the run dates already mirrored
  (`iseq_run_status_mirror.date`, `pac_bio_run_well_metrics_mirror.run_*`,
  `eseq_run_mirror.run_*`, `useq_run_metrics_mirror.run_*`) and make the chosen basis
  explicit in the response and description.
- **A global run listing** for drill-down: one row per run — stable cross-platform run
  identifier, native run id, platform, manufacturer, run-start / run-complete /
  archived timestamps where available — bounded, paged, with `/count`. This also
  removes the "there is no all-runs endpoint" gap.
- Index the run/status mirrors to serve the monthly grouping and the listing without
  full scans; EXPLAIN.

### D6 — Mirror correctness/perf fixes surfaced by this wave

- **`StatusBreakdown` empty-study bug:** `per_platform` returns `null` for studies with
  no products (observed on studies 5990, 8338), violating the array schema and
  breaking downstream validation. Return **`[]`**.
- **Make `StudyOverview` and `StatusBreakdown` <1 s for big studies** (7699 is ~3 s
  today). Apply the same denormalisation/index discipline as D1; the pure iRODS
  aggregate is already 0.085 s on the mirror, so the slowness is in the sample-
  membership / per-platform / QC-rollup arms — profile and fix (EXPLAIN), do not
  regress the per-platform-breakdown fix.
- **`iseq_product_metrics_mirror` PK + `char(64)` key** (from D1) is a general
  correctness/perf fix; apply it once.

## CLI exposure (REQUIRED — part of every deliverable)

Every new capability must be reachable from `wa mlwh`, in both local-cache and
`--server` modes, with graceful degradation (not-found/empty/not-tracked render
cleanly, exit 0), matching existing `wa mlwh` behaviour:

- **D1:** a dedicated **`wa mlwh tsv <study> [--file-type cram] [--columns …]
  [--deliverables-only] [--qc pass] [--limit/--offset|--all] [--server]`** emitting
  TSV to stdout (and `--json` for the structured form). This is the command a user
  runs to get the exact file from Q1/Q2 of the realworld problem set.
- **D2:** recency on `wa mlwh irods <scope> <id> [--sort created-desc] [--since/--until]`
  and a `wa mlwh latest <study|--faculty-sponsor NAME> [--file-type cram]` (or an
  `info`/manifest section) for "most recent data".
- **D3:** `wa mlwh search <term> [--prefix|--contains|--exact] [--type study|sample]`;
  keep the current default behaviour available but make the modes selectable.
- **D5:** `wa mlwh runs --monthly [--since/--until] [--platform …]` (grouped counts)
  and a `wa mlwh runs` listing.
- **D4:** `manual_qc` column/section wherever product rows render (`info <study>`,
  `manifest`, `tsv`); a `--deliverables-only` toggle where iRODS/product rows list.

## HARD REQUIREMENTS

1. **Web-responsive (<1 s) for bounded pages, including the largest studies**, and one
   cheap call for every count/overview/aggregate. Prove index-served paths with
   EXPLAIN. No per-row correlated subqueries or full scans of the large mirrors.
2. **No silent truncation.** Any "give me all …" (a full study TSV, a full path list)
   must return the complete set via keyset paging/streaming, or clearly report that
   output is a bounded page plus a total and next cursor. (The comparison
   implementation's silent 1000-row cap is exactly the failure mode to avoid.)
3. **Correctness baked in, not delegated to caller SQL.** manual_qc, target/deliverable,
   "cram" (filename suffix), recency basis (`created`), run `date_basis`, and
   prefix-vs-contains-vs-exact are defined and enforced server-side and stated in the
   registry text. The generic dynamic-call path is a fallback, never the intended way
   to answer these five questions.
4. **Source-true semantics.** `manual_qc` = `iseq_product_metrics.qc` (1/0/NULL),
   rolled up as `qc.go` does. There is no scalar `target` column; deliverable-only is
   an explicit, documented, verified filter. iRODS `created` = data added;
   `last_changed` = sync key (not surfaced as "new data"); `cache_synced_at` /
   `/freshness` = freshness caveat.
5. **Platform-aware; never a false "no data".** Keep uniform multi-platform treatment.
   Where a platform lacks a capability (ONT has no product/qc/iRODS-cram), say so
   explicitly; never collapse to a bare zero. Run aggregation must cover all platforms
   with an explicit per-platform date basis.
6. **Cache correctness.** New mirror columns/tables (`iseq_flowcell` discriminators;
   any substring/n-gram token table; run-aggregation indexes; the
   `iseq_product_metrics_mirror` PK/`char(64)` change; any deliverable/export
   structure) must be added to BOTH `sqlite` and `mysql` schema dialects (kept in
   parity), to `mlwh/sync.go`'s source selection, AND to the cold-load sparse
   read-index set where the mirror is large, with a **`CacheSchemaVersion` bump (12 →
   13; full resync acceptable)**.
7. **Tests.** TDD with behavioural tests; preserve all existing regressions. Add
   **real-MySQL integration tests** (`mlwh/cache_mysql_integration_test.go` pattern:
   throwaway DB, dropped on cleanup, skipped without creds) asserting the new paths
   execute on MySQL, are index-served (EXPLAIN), and return correct counts/rows
   (assert 7556 cram TSV = 886 rows; `hek_r` prefix = 4; `usculus` contains = the
   real count; deliverable-only drops controls where present); and a **source
   integration test** (`mlwh/sync_source_integration_test.go` pattern) covering the
   new source columns/tables (`iseq_flowcell` discriminators, `qc`, run dates) so the
   schema these tests assume stays true.
8. **Registry descriptions are the contract.** Each new/changed endpoint's
   `Description`/`Summary`/`Query` must precisely state the definition used
   (manual_qc, deliverable/target, "cram" suffix, `created` recency, run date_basis,
   prefix vs contains vs exact and the fields searched, and the freshness caveat),
   because the downstream MCP server surfaces this text verbatim.
9. **API version bump** (1.7.0 → 1.8.0) alongside the `CacheSchemaVersion` bump, per
   `openapi.go`'s documented lineage.

## Design decisions for the spec to settle (HOW, not WHETHER)

- The dedicated TSV subcommand's exact name/flags and column vocabulary + defaults;
  whether the backing path reuses/refactors `manifest.go` or adds an export
  structure; the deterministic row order; keyset vs streaming for the full export.
- The precise definition of "primary/target deliverable" and which `iseq_flowcell`/
  control columns to mirror to implement it; the surface form of `manual_qc`.
- The contains-search index mechanism (n-gram token table vs FULLTEXT vs other) and
  its count cap; which fields prefix/contains apply to; how the mode is expressed
  (flags vs subcommands vs param).
- The recency "latest" endpoint shape (per study, per sample, per faculty sponsor)
  and the cheap cross-study path; the tie-break at the max timestamp.
- The run-aggregation endpoint shape, the manufacturer mapping, and the per-platform
  `date_basis`; the global run listing's stable run identifier.
- Index choices for each new path and the additive-vs-recreate migration detail.

## Pointers / prior art

- Endpoints + descriptions: `mlwh/registry.go`. Result types/json tags: `mlwh/types.go`.
- Manifest: `mlwh/manifest.go`. iRODS + list SQL: `mlwh/hierarchy.go`. Counts:
  `mlwh/count.go`. Availability/recency/overview: `mlwh/availability.go`. QC/breakdown/
  progress: `mlwh/progress.go`, `mlwh/qc.go`. Search: `mlwh/search.go`. People:
  `mlwh/people.go`.
- Sync source selection + cold-load read-index sets + tokeniser: `mlwh/sync.go`,
  `mlwh/sync_platform_coverage.go`, `mlwh/cache.go`. Schema:
  `mlwh/cache_schema/{sqlite,mysql}/*.sql`, `mlwh/cache_schema.go`. Client + paging:
  `mlwh/remote.go`. CLI: `cmd/mlwh_manifest.go`, `cmd/mlwh_irods.go`,
  `cmd/mlwh_search.go`, `cmd/mlwh_studies.go`, `cmd/mlwh.go`.
- Integration-test patterns: `mlwh/cache_mysql_integration_test.go`,
  `mlwh/sync_source_integration_test.go`. Bugfix checklists: `.docs/bugfixes/`.
- Source schema (authoritative, for the source integration tests): DBIx::Class result
  classes at `wtsi-npg/ml_warehouse`
  (`lib/WTSI/DNAP/Warehouse/Schema/Result/{Sample,Study,IseqFlowcell,IseqProductMetric,SeqProductIrodsLocation,...}.pm`);
  SQLAlchemy mirror at `wtsi-npg/ml-warehouse-python` (no Element/Ultima tables — use
  Perl for those). The first/second waves (reused, not rebuilt): `.docs/realworld/`,
  `.docs/realworld2/` (background only — code is authority).

## Notes (settled decisions that govern the spec)

These resolve the important open choices; they are instructions, not questions.

### Versions and migration
- Bump `APIVersion` 1.7.0 → **1.8.0** and `CacheSchemaVersion` 12 → **13** together. A
  full resync is acceptable; the recreate-tables migration creates the new
  `iseq_flowcell`-discriminator columns/table, the substring token table, the
  run-aggregation indexes, the `iseq_product_metrics_mirror` PK/`char(64)` change, and
  any export structure. Do NOT take the additive no-version-bump path for the
  key-type change.

### D1 TSV
- It is a **dedicated subcommand**, not a format flag on `manifest`. Default columns
  cover Q1 (`supplier_name, study_accession_number, sanger_sample_id, manual_qc,
  irods_path`); accept `supplier_sample_name` as an alias for `supplier_name`.
  Deliverable-only defaults **on** for the cram TSV. `manual_qc` is the pass/fail/
  pending roll-up string (raw-value column may be offered additionally). Full-study
  export uses **keyset pagination**; the CLI prints the complete set with `--all` and a
  bounded page otherwise, and always states which it gave.

### D3 search
- Provide **three explicit modes**: literal-prefix (the fix for "starts with"),
  contains (indexed via a substring/n-gram token table populated in sync), and exact
  (reachable for short controlled tokens). Keep today's word-prefix as the search
  default's behaviour but make "starts with" and "contains" selectable and
  self-documented. Contains is bounded by a count cap.

### D4 target
- The "target/deliverable" filter is server-side and indexed (mirror the required
  `iseq_flowcell`/control columns). Document it as an approximation of the iRODS
  `target=1` AVU, verified so 7556 → 886 crams. `iseq_product_metrics.target_*`
  columns are NOT mirrored for this purpose (they are coverage metrics) — do not
  confuse them.

### D5 run aggregation
- One grouped monthly-count endpoint (`{month, manufacturer, platform, count,
  date_basis, cache_synced_at}`) plus one global run listing. Manufacturer derived
  from platform (state the map). `date_basis` chosen per platform and made explicit.

### Perf posture
- Every study-scoped answer stays a single indexed scan on a denormalised column (the
  proven mirror win); the manifest/TSV and the compound aggregates must be brought to
  <1 s at 7699 scale before this ships, verified by EXPLAIN and the integration tests.
