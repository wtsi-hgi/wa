# Feature: fast study→iRODS TSV export, iRODS recency, correct literal-prefix search + a shared exact filter family (organism / library-type / QC / deliverable), and global run aggregation (MLWH realworld3)

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
   `manual_qc`, no deliverable filter, no TSV/column control, and is **~3 s for a large
   study** (below). This is the flagship deliverable and must get a **dedicated,
   very fast `wa mlwh` subcommand**.
2. **"Give me all samples that start with `hek_r`."** — should return the **4**
   samples whose supplier name literally starts with `Hek_R` (`Hek_R1`..`Hek_R4`).
   Today `/search/sample/hek_r` returns **55**, because sample search is a
   word-prefix AND across four fields. The fix is to make **literal whole-value prefix
   the DEFAULT** sample search.
3. **"The Mus musculus samples" (constrain a search to an organism).** — the mouse
   samples a user reaches for with the organism word `musculus` (or `mus`, or
   `mus musculus`). There is no clean, composable way to constrain a search to an
   organism, nor to use organism as a filter on the TSV. This is an **organism lookup**,
   not a general free-text substring search: `common_name` is low-cardinality (**16,234**
   distinct values), so it is served by a dedicated **`--organism` filter** that matches
   whole words / full names over that small vocabulary — NOT by a general substring
   index. (A mid-word fragment like `usculus` is deliberately NOT a supported query.)
4. **"The most recently sequenced sample / the latest iRODS data for study (or lab)
   X."** — one call returning the row(s) behind the recency, with the iRODS `created`
   timestamp, sorted newest-first. Today `IRODSPath` omits `created` and the listings
   sort by product id, so recency is unanswerable from a path listing.
5. **"Plot runs per month by manufacturer and platform for the last 3 years."** — one
   aggregate call. Today there is no global/grouped run endpoint at all.

Two cross-cutting rules run through all of the above:

- **No "figure out your own SQL" escape hatch as the answer.** Each of these must be
  a first-class, correctly-scoped, indexed endpoint whose semantics (manual QC,
  deliverable, "cram", recency, run date basis, the search default vs the exact
  filters) are baked into the server and stated in the registry description — not left
  to a caller/LLM to reconstruct in ad-hoc SQL, where it silently gets the deliverable
  rule wrong, caps at a page, or picks the wrong timestamp.
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

The only authority for existing behaviour is **this repo's Go code** (`mlwh/registry.go`,
`mlwh/types.go`, `mlwh/manifest.go`, `mlwh/hierarchy.go`, `mlwh/search.go`,
`mlwh/people.go`, `mlwh/availability.go`, `mlwh/progress.go`, `mlwh/count.go`,
`mlwh/sync.go`, `mlwh/sync_platform_coverage.go`, `mlwh/cache.go`,
`mlwh/cache_schema/{sqlite,mysql}/*.sql`, `mlwh/remote.go`, `mlwh/server.go`,
`mlwh/openapi.go`, and the `cmd/mlwh_*.go` commands). The `.docs/realworld*` specs are
background only — verify against code. The **run `date_basis` semantics** are defined by
the downstream reference `mlwh://reference/sequencing-timestamps`
(`~/dj3-mlwh-mcp/src/index.ts`), validated against source below. **Current MLWH API
version: 1.7.0** (`mlwh/openapi.go` `APIVersion`); **current `CacheSchemaVersion`: 12**
(`mlwh/cache.go`).

Already merged and **reused as-is** (confirm in code before relying on it):

- **`Study`/`study_mirror`** carry `id_study_lims`, `name`, `accession_number`,
  `study_title`, `faculty_sponsor`, `data_access_group`, `programme`, `state`, etc.
  Indexed: `id_study_lims`, `uuid_study_lims`, `accession_number`, `name`,
  `faculty_sponsor`.
- **`Sample`/`sample_mirror`** carry `name`, `sanger_sample_id`, `supplier_name`,
  `accession_number`, `donor_id`, `common_name`, `taxon_id`, `description`. Indexed:
  those text columns individually (ci collation) — this is what makes literal-prefix
  and the exact filters fast without any new index.
- **`sample_search_token`** (derived word-token index; `(token, id_sample_tmp)`;
  **43.7 M rows**) backs `/search/sample` word-prefix matching. Retained for the opt-in
  `--words` mode (D3), no longer the default.
- **`library_samples`** carries `pipeline_id_lims` (**library_type**; 122 distinct
  values) linked to samples — the existing exact library-type path.
- **`seq_product_irods_locations_mirror`** already denormalises `id_iseq_product`,
  `irods_root_collection`, `irods_data_relative_path`, `irods_collection`,
  `irods_file_name`, **`id_sample_tmp`**, **`id_study_lims`**, `last_updated`,
  **`created`** (nullable), `platform`; indexed incl. `(id_study_lims, created)`,
  `(id_study_lims, id_iseq_product)`, `(id_sample_tmp)`.
- **`iseq_product_metrics_mirror`** mirrors `id_iseq_product`, `id_iseq_flowcell_tmp`,
  `id_run`, `position`, `tag_index`, `id_sample_tmp`, `id_study_lims`, **`qc`**,
  `qc_lib`, `qc_seq`.
- **`iseq_run_status_mirror` + `iseq_run_status_dict_mirror`** mirror the run-status
  timeline (`id_run`, `date`, `id_run_status_dict`, `iscurrent`; the dict maps to the
  status description). This is the run-date source for D5's shared platforms.
- **`StudyManifest`** (`/study/:id/manifest`, `with_irods`, `file_type`) +
  `CountStudyManifest`; **`/study|sample|run/:id/irods`** (+ counts, `file_type`);
  **`StudyOverview`/`StatusBreakdown`/`SampleProgress`/`RunOverview`/`RunStatus`**;
  people→studies (`/studies/faculty-sponsor/:name`, `/studies/user/:person`,
  `/resolve-person/:term`); the QC roll-up (`qc.go`: `qc` 1/0/NULL → pass/fail/pending,
  precedence fail > pending > pass).
- **`wa mlwh info <identifier>`** already IS the exact single-identifier lookup: it
  classifies/resolves sanger id, lims id, accession, supplier name, sample/study UUID,
  study lims id/accession, library pipeline/library ids and numeric run id (auto or
  `--type sample|study|run|library`), with NO 3-char minimum. D3 does not duplicate it.
- CLI already has `wa mlwh {info,search,irods,manifest,studies,sync,serve}`.

## Verified facts (checked against the live source `mlwarehouse` and our MySQL mirror, 2026-07-02)

These were confirmed directly, so the work is grounded, not guessed. **Cite these in
the spec; do not silently contradict them.**

- **The deliverable discriminator is `iseq_flowcell.entity_type`, NOT `is_spiked`.**
  There is no scalar `target` column in MLWH: `iseq_product_metrics` has
  `target_*`/`mean_bait_target_coverage` columns but every one is a bait/capture
  **coverage metric**, not a target-vs-control flag. The "target=1" a user means is the
  iRODS metadata AVU on the CRAM (the primary deliverable, excluding PhiX/controls).
  Confirmed source `iseq_flowcell.entity_type` distribution: `library_indexed`
  **9,030,465** + `library` **34,708** (the deliverables) vs `library_indexed_spike`
  **173,739** + `library_control` **3,445** (the spikes/controls to exclude).
  **`is_spiked` is the WRONG discriminator** — it is `1` for **7,320,389 of 9,242,357
  rows (79 %)**: it flags lanes/plexes that had PhiX spiked *in*, not products that
  *are* the spike, so filtering `is_spiked=0` would drop most real libraries.
  Element/Ultima use `{eseq,useq}_product_metrics.is_sequencing_control` (`=0` for
  deliverables; confirmed present, indexed). **The mirror does not currently mirror
  `iseq_flowcell` at all** (only `oseq_flowcell_mirror` for ONT and the derived
  `library_samples`), so it presently CANNOT exclude controls/spikes — D4 adds it.
- **`manual_qc` = `iseq_product_metrics.qc`** (`tinyint(1)`; **1 = pass, 0 = fail,
  NULL = pending/undecided**; confirmed distribution on a real run slice: 1 ≫ 0 > NULL).
  Already mirrored as `qc`. Per-platform: `pac_bio_product_metrics.qc`,
  `eseq_product_metrics.qc`, `useq_product_metrics.qc` (same 0/1/NULL); ONT
  (`oseq_flowcell`) has no product/qc.
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
- **Run `date_basis` per platform (from `mlwh://reference/sequencing-timestamps`,
  validated against source):** the authoritative "when did the run happen" date is
  completion-based and, for the three platforms that share `iseq_run_status`, comes from
  that status timeline, NOT the platform's own run table:
  - **Illumina & Element Aviti:** `iseq_run_status.date` at status **`run complete`**
    (Element shares `iseq_run_status`, joined via `eseq_product_metrics.id_run`;
    Illumina via `iseq_product_metrics.id_run`).
  - **Ultima:** `iseq_run_status.date` at status **`run archived`** — Ultima runs NEVER
    reach `run complete` (confirmed: joining `useq_run_metrics` → `iseq_run_status`
    shows **242 `run archived`** and **0 `run complete`**; `useq_run_metrics` has a
    `run_archived` column and no `run_complete`).
  - **PacBio:** the run (`pac_bio_run_name`) is dated by
    `pac_bio_run_well_metrics.run_complete` — a run-level value shared across the run's
    wells (the per-well `well_complete` differs per well). Example: `TRACTION-RUN-1000`
    has 8 wells A1–H1 all sharing `run_complete = 2023-12-20`, but `well_complete`
    spanning 2023-12-13→21. PacBio is not in `iseq_run_status`.
  - **ONT:** has **no run-metrics table and no true sequencing date**. `oseq_flowcell.run_id`
    is NULL for all ~8,730 rows, so the run identity is **`experiment_name`** (447 distinct,
    e.g. `ONTRUN-11`); its only date is `oseq_flowcell.last_updated`, which (like
    `recorded_at`) is a **warehouse-load timestamp, not a sequencing time** (all flowcell
    rows of a run share one identical `last_updated`) — treated as such in D5.
- **Measured manifest/aggregate performance (source-direct vs our mirror vs current
  HTTP endpoint):**
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
- **Measured search/filter reality (the SQSCP sample domain is the WHOLE
  `sample_mirror` = 10,352,242 rows; earlier "~1.9 M" was stale):**
  - **Literal-prefix is free and correct:** `hek_r%` over the four text fields
    (`name`, `supplier_name`, `common_name`, `donor_id`) = the **4** `Hek_R1..4` in
    **0.07 s**, an index range seek on the existing per-column btree index. No new
    structure needed. This is the fix for Q2.
  - **A general "contains" is not worth building:** naive `%substr%` over the four
    fields = **16–23 s**. MySQL 8.4 InnoDB `ngram` FULLTEXT is fast (~0.27 s) but
    **incorrect** — boolean/phrase mode **undercounts ~50 %** (false negatives a
    LIKE-verify cannot recover) and natural-language mode overcounts massively;
    building it on 10 M rows ran **>7.5 min**. A correct trigram token table would be
    **~200 M+ rows** (4–6× the existing 43.7 M `sample_search_token`) and as slow to
    build. **So NO substring / n-gram / FULLTEXT index is built.**
  - **Organism is low-cardinality → a whole-word filter, not a substring search:**
    `common_name` has **16,234** distinct values (dominated by "Mus Musculus" 235,447,
    "Homo sapiens" 1,194,161, empty 6.2 M). Matching the whole organism word/name (e.g.
    `musculus`, `mus musculus`) against that tiny vocabulary, then an exact indexed
    lookup by `common_name`, is page-fast and needs no big index. A mid-word fragment
    (e.g. `usculus`) is out of scope by design. This is the `--organism` filter (D3).
  - **`library_type` is a clean vocabulary:** `library_samples.pipeline_id_lims` has
    **122** distinct values — ideal for an exact filter flag.
  - **ONT is cheap to mirror for identity:** source `oseq_flowcell` is **~8,730 rows**;
    run identity is **`experiment_name`** (447 distinct — `run_id` is NULL for every row,
    and `flowcell_id`/`run_uuid` are ~24 % null); its only date is the warehouse-load
    `last_updated` (see the `date_basis` fact above).
  - **Escaping is fine:** `_`/`%` are already correctly escaped (`escapeLIKELiteral`,
    `ESCAPE '!'`) — there is no wildcard bug; today's `hek_r`→55 is the semantic
    word-prefix problem fixed above. Study/person search keeps its small-table `%term%`
    substring (fine there; not a model for large tables).

## Per-question verdict

| Q | Question | Verdict | What's needed |
| --- | --- | --- | --- |
| 1 | study cram TSV w/ chosen columns + manual_qc + deliverable, fast | **GAP + PERF** | D1: dedicated fast TSV subcommand + backing export path; D4 manual_qc/deliverable |
| 2 | "starts with hek_r" → 4 | **WRONG** | D3: literal-prefix as the DEFAULT search |
| 3 | constrain a search to an organism (the Mus musculus samples) | **GAP** | D3: `--organism` whole-word/full-name filter over the low-cardinality `common_name` vocabulary |
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
  covers Q1 (`supplier_name, study_accession_number, sanger_sample_id, manual_qc,
  irods_path`). Output is real TSV: a header row then tab-separated rows, deterministic
  order (settle: by `id_run, lane, tag_index, name`), stable across pages.
- **Filters (the shared filter family — see D3/D4).** Required study; `--file-type`
  (default `cram`, filename-suffix semantics as elsewhere); **`--deliverables-only` on
  by default** for the cram TSV (D4, `entity_type`-based), with a flag to include
  controls/sub-products; `--qc pass|fail|pending` (here at PRODUCT grain — each TSV row
  is a product, so it filters on that product's `qc`); and where useful `--library-type
  <val>` / `--organism <val>`. Filtering and the returned columns are independent (Q1
  wanted the `manual_qc` column, not necessarily the filter). This is the SAME filter
  family offered on `wa mlwh search` (D3), differing only in QC grain (see D3).
- **Speed: <1 s for a bounded page even for the largest studies** (7699-scale). The
  first page must be web-responsive; a `/count` counterpart and sizing headers are
  required. **No silent row cap**: a full-study export MUST be able to stream/emit
  *every* matching row (7699 → 100 k+ rows) via **keyset pagination** (not `LIMIT
  OFFSET`, which degrades on deep pages) or a streaming response. State explicitly in
  the CLI when output is a bounded page vs the complete set (`--all` = complete set).
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
    supplier/sanger id/accession/qc/deliverable-flag/irods path) populated during sync,
    so the TSV is one ordered range scan. Decide in the spec; whichever route, the
    target is <1 s per page at 7699 scale and correct totals.
- **Correctness.** `manual_qc` is the product's `qc` value rendered pass/fail/pending
  by `qc.go`. `irods_path` concatenation must be verified against real paths (e.g.
  `.../lane6/plex45/51945_6#45.cram`). The study 7556 deliverable-only cram count is the
  `entity_type`-derived count (≈886 — assert the actual figure and document any delta;
  see D4), controls dropped.

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
  study name, sample `name`, `supplier_name`, `id_run`, lane/tag, platform. It returns
  a **bounded, pageable page ordered `created DESC`** (settle a small default N), ties
  broken deterministically by `(id_run, id_product)` — NOT an unbounded "all rows tied
  at MAX(created)" set. Support a **faculty-sponsor-scoped** variant (join
  `study_mirror.faculty_sponsor`) so "latest data for the Anderson lab" does not
  require the caller to fan out over 91 studies and then fail to bridge the timestamp
  back to a row (the exact realworld trap). Decide the cheap cross-study path (the
  `(id_study_lims, created)` index is per-study; a small per-study MAX then merge may
  beat a global filesort — settle with EXPLAIN).
- Reconcile `StudyOverview.newest_data_added` with this: its membership basis is the
  raw `seq_product_irods_locations_mirror` scan on `(id_study_lims, created)`; document
  that basis so the "latest data" rows are **followable** to the max the overview
  reports (today it reports a max that `samples-with-data` windows could not reproduce).

### D3 — Correct search: literal-prefix default, opt-in word-prefix, and a shared exact filter family

Make search intent explicit and self-documenting so the MCP layer/agent chooses
correctly, and so none of it depends on the caller crafting `LIKE` patterns. The
measured evidence above drives this design.

- **Default = literal whole-value prefix.** `wa mlwh search hek_r` (no mode flag) must
  return **exactly** the samples where any of the four text fields (`name`,
  `supplier_name`, `common_name`, `donor_id`) literally starts with the term
  (`col LIKE 'hek!_r%' ESCAPE '!'`, an index range seek) — the **4** `Hek_R1..4` — in
  ~0.07 s. This REPLACES word-prefix AND as the default sample search.
- **Word-prefix is retained as an opt-in `--words` mode** (keep the `sample_search_token`
  table). Its one distinct value is separator-agnostic multi-word matching (query
  `10X Automation HEK` matching stored `10X_Automation_HEK`); it is NOT the default and
  its cross-field AND behaviour (today's `hek_r`→55) is no longer what a bare search does.
- **No general contains / no substring index.** Do NOT build an n-gram/trigram/substring
  token table or a FULLTEXT index (measured: naive is 16–23 s; ngram FULLTEXT is
  incorrect; a correct trigram table is ~200 M+ rows and slow to build). Mid-word
  substring matching is explicitly out of scope.
- **Every sample query carries a search term; the exact filters NARROW it.** There is no
  termless/filter-only route: a sample query is always `search <term> [filters…]`. "All
  Mus musculus samples" is `search --words musculus` (word-prefix finds the `musculus`
  word in `common_name`); the exact filters below refine a term search and gate the TSV.
- **`--organism` is a WORD-MEMBERSHIP filter over `common_name`.** A value matches iff its
  `common_name` contains the query as a whole word (all query words, for a multi-word
  query): `--organism musculus` matches every `common_name` containing the word `musculus`
  — INCLUDING subspecies like `Mus musculus castaneus`; `--organism mus` matches all
  genus-`Mus` names; `--organism "mus musculus"` requires both words. It is NOT mid-word
  substring (`usculus` matches nothing) and NOT exact-whole-value (subspecies are
  included). Resolve against the low-cardinality (~16 k) `common_name` vocabulary, then
  constrain to samples by the indexed `common_name`. Settle the exact mechanism (a small
  distinct-`common_name` helper table, or word tokens scoped to `common_name`) and prove
  it index-served.
- **Exact single-identifier lookups stay `wa mlwh info`'s job.** Do NOT add a `--exact`
  search mode and do NOT add a new `find` command; do NOT duplicate `info`. Short
  controlled tokens are reached via `info` (single identifier) or via the exact filters
  below (which are exempt from the 3-char minimum).
- **The shared exact filter family** — server-side params, AND-combined with the search
  term and with each other, each a fast indexed path (prove with EXPLAIN), each exempt
  from the 3-char free-text minimum (which stays only on the free-text search term and
  `--words`); filters apply to **sample** search (not study search):
  - `--library-type <val>` — exact `library_samples.pipeline_id_lims` (122-value vocabulary).
  - `--organism <val>` — the whole-word/full-name `common_name` filter above.
  - `--qc pass|fail|pending` — **grain-appropriate**: on sample SEARCH it matches the
    per-sample ROLL-UP verdict (`qc.go` fail>pending>pass; one bucket per sample; agrees
    with `SampleProgress.qc`/`StatusBreakdown`). On the D1 TSV (rows ARE products) it
    matches the raw PER-PRODUCT `qc`. State both grains explicitly.
  - `--deliverables-only` — the D4 deliverable filter (`entity_type`-based).
    **Pass-through for platforms with no source discriminator (PacBio, ONT):** it filters
    only Illumina/Element/Ultima products and leaves PacBio/ONT-only samples unaffected
    (neither positively kept nor dropped), so they never silently disappear (HARD REQ 5).
  Combining a filter with the term intersects the candidate `id_sample_tmp` set (via
  `library_samples` for library-type, the `common_name` index for organism, the
  product/flowcell join for qc/deliverable) — index it so the intersection is not a scan.
  The SAME family is offered on the D1 `tsv`.
- **Expose via CLI and endpoint/param**, with the registry description stating precisely
  what the default does, what `--words` does, and exactly what each filter matches and
  over which field (incl. the QC grain difference between search and tsv). Do NOT
  overload one endpoint so the caller must add `%`/anchors.

### D4 — manual_qc & deliverable semantics (feeds D1 and the D3 filter family; also the manifest)

- **Expose `manual_qc`** (the `qc` roll-up: pass/fail/pending; the raw 1/0/NULL may be
  offered additionally) wherever product/iRODS rows are listed: the D1 TSV,
  `StudyManifest` rows, optionally the iRODS listings. Reuse `qc.go`'s roll-up so it can
  never disagree with `SampleProgress.qc` / `StatusBreakdown`.
- **Implement the "deliverable" filter server-side using `entity_type` only** (confirmed
  today; see Verified facts). Mirror a new **`iseq_flowcell`** table carrying
  `id_iseq_flowcell_tmp` (PK — the join key from
  `iseq_product_metrics_mirror.id_iseq_flowcell_tmp`), `entity_type`, `pipeline_id_lims`
  (library_type), `id_sample_tmp`, `id_study_tmp`. A product is a **deliverable** iff its
  flowcell's `entity_type IN ('library','library_indexed')` (i.e. NOT
  `library_indexed_spike` / `library_control`); for Element/Ultima, a product is a
  deliverable iff `is_sequencing_control = 0` on `{eseq,useq}_product_metrics`. Do **NOT**
  use `iseq_flowcell.is_spiked` (it flags spiked lanes, not spike products — 79 % true).
  Do **NOT** read the iRODS `target=1` AVU and do **NOT** mirror
  `iseq_composition_tmp`/component structure this wave; "non-primary sub-product"
  exclusion is only to the extent `entity_type` expresses it (documented edge case). The
  filter must be a fast indexed column, proven with EXPLAIN. **PacBio and ONT have NO
  deliverable discriminator in the source** (PacBio has only a `qc` flag and `control_*`
  read metrics — not a per-product control classifier; ONT has no products at all), so
  `--deliverables-only` is **pass-through** for them: it never drops PacBio/ONT samples
  (HARD REQ 5). Document this.
- **Verify** against real studies: study 7556's deliverable-only cram count is the
  `entity_type`-derived count and known controls/spikes are dropped where present. 886 is
  the expected figure — assert the actual discriminator-derived count and document any
  delta from 886 (and from the true iRODS `target=1` AVU) rather than hard-pinning
  exactly 886.
- **Document the semantics** in registry text: `iseq_product_metrics.target_*` are
  coverage metrics; there is no scalar warehouse `target` column; the deliverable filter
  is an `entity_type`-based approximation of the iRODS `target=1` AVU; exactly what
  "deliverable-only" includes/excludes and its known edge cases.

### D5 — Global run aggregation (runs per month by manufacturer & platform)

- **A grouped-count endpoint** answering "runs per month by manufacturer and platform"
  in one call over a `since`/`until` window: rows of `{month, manufacturer, platform,
  count, date_basis, cache_synced_at}`, across all platforms (Illumina/Element/
  Ultima/PacBio/ONT). **Manufacturer** is derived from platform (Illumina→Illumina,
  Elembio→Element Biosciences, Ultimagen→Ultima Genomics, PacBio→PacBio,
  ONT→Oxford Nanopore); state the mapping.
- **`date_basis` per platform is the authoritative reference (validated above), and each
  response row states which basis it used** (HARD REQ 8):
  - Illumina & Element: `iseq_run_status` status **`run complete`** date (via each
    platform's product-metrics `id_run`).
  - Ultima: `iseq_run_status` status **`run archived`** date (never "completes").
  - PacBio: `pac_bio_run_well_metrics.run_complete` — the run-level value (NOT the
    per-well `well_complete`), because D5 counts runs (see run grain below).
  - **ONT: `oseq_flowcell.last_updated`, labelled `date_basis` = "warehouse load time —
    not a true sequencing date".** ONT IS included in the monthly buckets under this
    explicit label (so it is never silently dropped), but the label makes clear its
    month is a warehouse-load month, not a sequencing month.
- **Run grain — one counted "run" is one run identifier, never a sub-run unit:**
  Illumina/Element/Ultima = one `id_run`; PacBio = one `pac_bio_run_name` (a run has
  several wells — count it once, dated by the shared run-level `run_complete`; the 8-well
  `TRACTION-RUN-1000` is one Dec-2023 run, not 8); ONT = one `experiment_name` (a run has
  several flowcells — count once, dated by `last_updated`). Do NOT count wells/flowcells
  as runs. Expected magnitudes: PacBio ≈ 2,843 runs (from 12,499 well rows), ONT ≈ 447
  runs, Ultima `run archived` = 242.
- **Source/mirror notes:** the authoritative run-date source for Illumina/Element/Ultima
  is `iseq_run_status` (mirrored as `iseq_run_status_mirror` + dict). Ensure the sync
  includes Element/Ultima `id_run`s in that mirror (else fall back to the platform run
  mirrors' `run_complete`/`run_archived`). Mirror run dates are stored as **varchar**, so
  the monthly grouping needs a normalized, indexed date to stay index-served. Extend
  `oseq_flowcell_mirror` to carry ONT run identity (`experiment_name` — the usable one;
  `run_id` is NULL for every row) and `last_updated`.
- **A global run listing** for drill-down: one row per run — a **stable cross-platform
  run identifier that is a composite string `<platform>:<native_id>`** (e.g.
  `illumina:47409`, `pacbio:<run_name>`, `ont:<experiment_name>` — ONT `run_id` is NULL,
  so `experiment_name` is the identity), with `platform` and the native
  run id ALSO carried as separate fields; plus manufacturer and the run date(s) with the
  same per-platform basis (ONT's date carries the "warehouse load" caveat) — bounded,
  paged, with `/count`. The composite id is the paging cursor / drill-down key. This also
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
  [--deliverables-only] [--qc pass] [--library-type X] [--organism Y]
  [--limit/--offset|--all] [--server] [--json]`** emitting TSV to stdout (and `--json`
  for the structured form). This is the command a user runs to get the exact file from
  Q1 of the realworld problem set.
- **D2:** recency on `wa mlwh irods <scope> <id> [--sort created-desc] [--since/--until]`
  and a `wa mlwh latest <study|--faculty-sponsor NAME> [--file-type cram]` (or an
  `info`/manifest section) for "most recent data".
- **D3:** `wa mlwh search <term> [--words] [--library-type X] [--organism Y]
  [--qc pass|fail|pending] [--deliverables-only] [--type study|sample]`. The `<term>` is
  required; default matching is literal-prefix; `--words` selects the retained
  word-prefix mode; the exact filters are the shared family; exact single-identifier
  lookups remain `wa mlwh info`.
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
3. **Correctness baked in, not delegated to caller SQL.** manual_qc, deliverable,
   "cram" (filename suffix), recency basis (`created`), run `date_basis`, the search
   default (literal-prefix), the `--words` mode, and the exact filters
   (organism/library-type/qc/deliverable) are defined and enforced server-side and
   stated in the registry text. The generic dynamic-call path is a fallback, never the
   intended way to answer these five questions.
4. **Source-true semantics.** `manual_qc` = `iseq_product_metrics.qc` (1/0/NULL),
   rolled up as `qc.go` does. There is no scalar `target` column; deliverable-only is
   the `entity_type`-based (Illumina) / `is_sequencing_control`-based (Element/Ultima)
   filter, NOT `is_spiked`. Run `date_basis` follows the per-platform reference (Illumina/
   Element `run complete`, Ultima `run archived`, PacBio `run_complete`, ONT the
   `last_updated` warehouse-load fallback, labelled). iRODS `created` = data added;
   `last_changed` = sync key (not surfaced as "new data"); `cache_synced_at` /
   `/freshness` = freshness caveat.
5. **Platform-aware; never a false "no data".** Keep uniform multi-platform treatment.
   Where a platform lacks a capability (ONT has no product/qc/iRODS-cram and no true
   sequencing date), say so explicitly; never collapse to a bare zero or a silent drop.
   Run aggregation covers all platforms including ONT, whose bucket carries the explicit
   "warehouse load time — not a true sequencing date" `date_basis` label.
   `--deliverables-only` is pass-through on platforms with no deliverable discriminator
   (PacBio/ONT) rather than dropping their samples.
6. **Cache correctness.** New mirror tables/columns — the new `iseq_flowcell` mirror
   (`entity_type`, `pipeline_id_lims`, keys); any organism helper structure over
   `common_name`; the extended `oseq_flowcell_mirror` ONT run columns
   (`run_id`/`run_uuid`/`experiment_name`/`last_updated`); a normalized+indexed run-date
   for the monthly grouping; run-aggregation indexes; the `iseq_product_metrics_mirror`
   PK/`char(64)` change; any deliverable/export structure — must be added to BOTH
   `sqlite` and `mysql` schema dialects (kept in parity), to `mlwh/sync.go`'s source
   selection, AND to the cold-load sparse read-index set where the mirror is large, with
   a **`CacheSchemaVersion` bump (12 → 13; full resync acceptable)**.
7. **Tests.** TDD with behavioural tests; preserve all existing regressions. Add
   **real-MySQL integration tests** (`mlwh/cache_mysql_integration_test.go` pattern:
   throwaway DB, dropped on cleanup, skipped without creds) asserting the new paths
   execute on MySQL, are index-served (EXPLAIN), and return correct counts/rows:
   - `hek_r` default (literal-prefix) search = the **4** `Hek_R1..4`.
   - the `--organism musculus` filter matches by WORD-MEMBERSHIP — every `common_name`
     containing the word `musculus`, INCLUDING subspecies (e.g. `Mus musculus castaneus`),
     so the count exceeds the dominant `Mus Musculus` value's 235,447 alone; assert the
     actual word-membership count, and that the mid-word fragment `usculus` matches
     **nothing** (locks word-membership — not substring, not exact-whole-value).
   - study 7556 deliverable-only cram TSV = the `entity_type`-derived count (**≈886**;
     assert the actual figure, document any delta), controls/spikes dropped where present.
   - a `--qc` filter on sample search buckets by the per-sample roll-up, while `--qc` on
     the TSV filters per-product (the grain difference).
   - D5 run counts are at RUN grain: PacBio ≈ 2,843 distinct `pac_bio_run_name`s (not
     12,499 wells), ONT ≈ 447 distinct `experiment_name`s (not per-flowcell), Ultima
     `run archived` = 242; ONT buckets by the labelled warehouse-load date, and
     `--deliverables-only` leaves PacBio/ONT samples in (pass-through).

   Add a **source integration test** (`mlwh/sync_source_integration_test.go` pattern)
   covering the new source columns/tables (`iseq_flowcell.entity_type`/`pipeline_id_lims`,
   `{eseq,useq}_product_metrics.is_sequencing_control`, `iseq_product_metrics.qc`, the run
   dates incl. the `iseq_run_status` `run complete`/`run archived` basis and
   `oseq_flowcell.last_updated`) so the schema these tests assume stays true.
8. **Registry descriptions are the contract.** Each new/changed endpoint's
   `Description`/`Summary`/`Query` must precisely state the definition used
   (manual_qc, deliverable via `entity_type`, "cram" suffix, `created` recency, the
   per-platform run `date_basis` incl. the ONT warehouse-load caveat, the search default
   vs `--words` vs the exact filters and the fields each covers, and the freshness
   caveat), because the downstream MCP server surfaces this text verbatim.
9. **API version bump** (1.7.0 → 1.8.0) alongside the `CacheSchemaVersion` bump, per
   `openapi.go`'s documented lineage.

## Design decisions for the spec to settle (HOW, not WHETHER)

- The dedicated TSV subcommand's exact name/flags and column vocabulary + defaults;
  whether the backing path reuses/refactors `manifest.go` or adds an export
  structure; the deterministic row order; keyset vs streaming for the full export.
- The exact `iseq_flowcell` mirror shape and index (the deliverable filter's indexed
  column and how it joins to products); the surface form of `manual_qc`.
- The `--organism` filter's word-membership mechanism over `common_name` (whole-word incl. subspecies; not substring or exact-whole-value)
  (a small distinct-`common_name` helper table vs `common_name`-scoped word tokens) and
  its index; how each shared filter intersects the search candidate set; the flag names.
- The recency "latest" endpoint shape (per study, per sample, per faculty sponsor),
  the cheap cross-study path, and the small default page size.
- The run-aggregation endpoint shape; how the `iseq_run_status` `run complete`/`run
  archived` date (and the varchar→date normalization/index) is realised in the mirror
  for Illumina/Element/Ultima; the ONT run key; the composite run-identifier format.
- Index choices for each new path and the additive-vs-recreate migration detail.

## Pointers / prior art

- Endpoints + descriptions: `mlwh/registry.go`. Result types/json tags: `mlwh/types.go`.
- Manifest: `mlwh/manifest.go`. iRODS + list SQL: `mlwh/hierarchy.go`. Counts:
  `mlwh/count.go`. Availability/recency/overview: `mlwh/availability.go`. QC/breakdown/
  progress: `mlwh/progress.go`, `mlwh/qc.go`. Search (incl. the existing
  `sampleFullPrefix*` literal-prefix machinery and `sampleSearchCountCap`):
  `mlwh/search.go`. Exact single-identifier resolution (reused by `info`, not
  duplicated): `mlwh/resolver.go`, `mlwh/enrich.go`, `mlwh/hierarchy.go`
  (`FindSamplesBy*`). People: `mlwh/people.go`.
- Sync source selection + cold-load read-index sets + tokeniser: `mlwh/sync.go`,
  `mlwh/sync_platform_coverage.go`, `mlwh/cache.go`. Schema:
  `mlwh/cache_schema/{sqlite,mysql}/*.sql`, `mlwh/cache_schema.go`. Client + paging:
  `mlwh/remote.go`. CLI: `cmd/mlwh_manifest.go`, `cmd/mlwh_irods.go`,
  `cmd/mlwh_search.go`, `cmd/mlwh_info.go`, `cmd/mlwh_studies.go`, `cmd/mlwh.go`.
- Run `date_basis` authority: `mlwh://reference/sequencing-timestamps` in
  `~/dj3-mlwh-mcp/src/index.ts` (and its `AGENTS.md` § Platform-specific schema
  knowledge) — the per-platform reliable date field and join path; validated against
  source 2026-07-02.
- Integration-test patterns: `mlwh/cache_mysql_integration_test.go`,
  `mlwh/sync_source_integration_test.go`. Bugfix checklists: `.docs/bugfixes/`.
- Source schema (authoritative, for the source integration tests): DBIx::Class result
  classes at `wtsi-npg/ml_warehouse`
  (`lib/WTSI/DNAP/Warehouse/Schema/Result/{Sample,Study,IseqFlowcell,IseqProductMetric,SeqProductIrodsLocation,IseqRunStatus,OseqFlowcell,...}.pm`);
  SQLAlchemy mirror at `wtsi-npg/ml-warehouse-python` (no Element/Ultima tables — use
  Perl for those). The first/second waves (reused, not rebuilt): `.docs/realworld/`,
  `.docs/realworld2/` (background only — code is authority).

## Notes (settled decisions that govern the spec)

These resolve the important open choices; they are instructions, not questions. They
agree with the deliverables above (no overrides needed).

### Versions and migration
- Bump `APIVersion` 1.7.0 → **1.8.0** and `CacheSchemaVersion` 12 → **13** together. A
  full resync is acceptable; the recreate-tables migration creates the new
  `iseq_flowcell` mirror, any organism helper structure over `common_name`, the
  extended `oseq_flowcell_mirror` ONT run columns, the normalized+indexed run-date for
  the monthly grouping, the run-aggregation indexes, the `iseq_product_metrics_mirror`
  PK/`char(64)` change, and any export structure. Do NOT take the additive
  no-version-bump path for the key-type change.

### D1 TSV
- A **dedicated subcommand**, not a format flag on `manifest`. Default columns cover Q1
  (`supplier_name, study_accession_number, sanger_sample_id, manual_qc, irods_path`);
  accept `supplier_sample_name` as an alias for `supplier_name`. `--deliverables-only`
  defaults **on** for the cram TSV. `manual_qc` is the pass/fail/pending roll-up string
  (raw-value column may be offered additionally). Full-study export uses **keyset
  pagination**; the CLI prints the complete set with `--all` and a bounded page
  otherwise, and always states which it gave. It offers the shared filter family (with
  `--qc` at product grain).

### D2 recency
- The "latest data / most recently sequenced" endpoint(s) return a **bounded, pageable
  page** ordered `created DESC` (small default N), ties broken by `(id_run, id_product)`
  — NOT an unbounded "all rows tied at MAX(created)" set. Membership basis = the raw
  `seq_product_irods_locations_mirror` scan on `(id_study_lims, created)`; document it so
  the result reconciles with `StudyOverview.newest_data_added`. A faculty-sponsor-scoped
  variant uses the same shape.

### D3 search + filters
- **Default sample search = literal whole-value prefix** over `name`, `supplier_name`,
  `common_name`, `donor_id` (index range seek; fixes `hek_r` → the 4). **Word-prefix is
  retained as an opt-in `--words` mode** (keep `sample_search_token`), not the default.
  **No substring / n-gram / FULLTEXT / trigram index is built.** **Every sample query
  requires a `<term>`; there is no termless/filter-only route** — the exact filters
  narrow a term search. **Exact single-identifier lookups stay `wa mlwh info`'s job** —
  no `--exact` mode, no new `find` command.
- **Shared exact filter family** on both `search` and `tsv`, server-side, AND-combined,
  fast/indexed (EXPLAIN), exempt from the 3-char minimum: `--library-type` (exact
  `pipeline_id_lims`), `--organism` (**word-membership** over the ~16 k `common_name`
  vocabulary — matches whole words incl. subspecies; NOT mid-word substring and NOT
  exact-whole-value; `usculus` matches nothing), `--qc pass|fail|pending`
  (**grain-appropriate**: per-sample roll-up on search, per-product on the TSV),
  `--deliverables-only` (D4; **pass-through** for PacBio/ONT, which have no discriminator).
  Filters apply to sample search, not study search.

### D4 deliverable
- The deliverable filter is server-side, indexed, and defined by **`iseq_flowcell.entity_type`**
  (deliverable = `entity_type IN ('library','library_indexed')`; exclude
  `library_indexed_spike`/`library_control`) and, for Element/Ultima,
  `is_sequencing_control = 0`. **Do NOT use `is_spiked`** (79 % true; flags spiked lanes,
  not spike products). Mirror a new `iseq_flowcell` table (do NOT read the iRODS `target=1`
  AVU; do NOT mirror composition/component structure this wave). Documented as an
  approximation of the iRODS `target=1` AVU; verified so study 7556 ≈ 886 crams (assert
  the actual `entity_type`-derived count, document any delta). `iseq_product_metrics.target_*`
  are coverage metrics — not mirrored for this purpose.

### D5 run aggregation
- One grouped monthly-count endpoint (`{month, manufacturer, platform, count,
  date_basis, cache_synced_at}`) plus one global run listing. Manufacturer derived from
  platform (state the map). **One counted "run" = one run identifier, not a sub-run
  unit:** Illumina/Element/Ultima = `id_run`; PacBio = `pac_bio_run_name` (dated by the
  run-level `run_complete`, NOT per-well `well_complete`; ≈2,843 runs); ONT =
  `experiment_name` (dated by `last_updated`; ≈447 runs — `run_id` is NULL, unusable). Do
  not count wells/flowcells as runs. **`date_basis` per platform follows the authoritative
  reference:** Illumina & Element = `iseq_run_status` `run complete`; Ultima =
  `iseq_run_status` `run archived` (never completes); PacBio =
  `pac_bio_run_well_metrics.run_complete`; **ONT = `oseq_flowcell.last_updated` labelled
  "warehouse load time — not a true sequencing date"**, included in the buckets under
  that explicit label (never silently dropped). Each response row states its basis. The
  listing's stable run identifier is the composite `<platform>:<native_id>`
  (`ont:<experiment_name>`), with platform and native id also carried separately.

### Perf posture
- Every study-scoped answer stays a single indexed scan on a denormalised column (the
  proven mirror win); the manifest/TSV and the compound aggregates must be brought to
  <1 s at 7699 scale before this ships, verified by EXPLAIN and the integration tests.
