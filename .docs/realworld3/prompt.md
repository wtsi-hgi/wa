# Feature: a fast, column-selectable, multi-format `wa mlwh export` over any entity→children (replacing `irods`), iRODS recency, correct literal-prefix search + a shared exact filter family (organism / library-type / QC / deliverable), global run aggregation, `programme` as a first-class grouping dimension + a study→users inverse, and merged multi-lane CRAM attribution (MLWH realworld3)

## Summary

A third wave of real user questions must become **fast (web-responsive, ideally <1s,
and never a silent truncation), cheap (one call, bounded response), and CORRECT
without asking the caller to hand-write SQL**. These extend the "realworld" (API
1.6.0) and "realworld2" (API 1.7.0) work already merged. The questions, taken from
real agent transcripts that currently fail, are slow, or give wrong answers:

1. **"Write a TSV of the iRODS cram files for study X with columns
   `[supplier_sample_name, study_accession_number, sanger_sample_id, manual_qc,
   irods_path]`, primary/target deliverables only."** — today there is no
   column-selectable, format-selectable listing path; the closest (`wa mlwh manifest
   --with-irods`) has no `manual_qc`, no deliverable filter, no column/format control,
   misses merged multi-lane CRAMs (see Q7), and is **~3 s for a large study** (below).
   This is the flagship, and it must be served by a **new, dedicated, very fast,
   GENERIC `wa mlwh export` subcommand** (D1) — not a one-off "study cram TSV" command.
2. **"Give me all samples that start with `hek_r`."** — should return the **4**
   samples whose supplier name literally starts with `Hek_R` (`Hek_R1`..`Hek_R4`).
   Today `/search/sample/hek_r` returns **55**, because sample search is a
   word-prefix AND across four fields. The fix is to make **literal whole-value prefix
   the DEFAULT** sample search.
3. **"The Mus musculus samples" (constrain a search to an organism).** — the mouse
   samples a user reaches for with the organism word `musculus` (or `mus`, or
   `mus musculus`). There is no clean, composable way to constrain a search to an
   organism, nor to use organism as a filter on the export. This is an **organism lookup**,
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
6. **"Break down PacBio sequencing by programme for the last year; which 5 studies?
   Then a per-study table with programme, faculty sponsor, and the study owners /
   managers / followers."** — three walls: (a) there is no platform-scoped, date-scoped,
   grouped sequencing aggregate, and `programme` is not an aggregation/grouping dimension
   (no studies-by-programme route, no grouped count, and `StudyOverview` omits
   `programme`); (b) the study↔person graph only runs person→studies
   (`/studies/user/:person`), so a study's owners/managers/followers cannot be listed
   FROM the study; the data is already mirrored (`study_users_mirror`, indexed) but has
   no study→users route. D7 makes `programme` first-class and adds the inverse
   study→users listing.
7. **"List every sample in study 7568 with sample name, EGA id, and an iRODS CRAM
   path."** — nearly one call via `manifest --with-irods --file-type cram`, EXCEPT a
   subset of samples come back with an empty `irods_path` because their CRAM is a
   **merged multi-lane composite object** whose composite `id_iseq_product` matches no
   single-lane manifest row. For study 7568 that is **48 of 732** samples (arithmetic
   below), silently dropped, recoverable today only by a per-sample backfill. D8 makes
   merged/composite CRAMs first-class: attributed to their sample in listings/exports,
   and made explicit (not silently empty) in the manifest.

Two cross-cutting rules run through all of the above:

- **No "figure out your own SQL" escape hatch as the answer.** Each of these must be
  a first-class, correctly-scoped, indexed endpoint whose semantics (manual QC,
  deliverable, "cram", recency, run date basis, the search default vs the exact
  filters, programme grouping, study-role direction, merged-product attribution) are
  baked into the server and stated in the registry description — not left to a
  caller/LLM to reconstruct in ad-hoc SQL, where it silently gets the deliverable rule
  wrong, caps at a page, picks the wrong timestamp, or drops merged CRAMs.
- **Correct the mirror where it is wrong or slow.** Comparing our cache to a
  source-direct implementation surfaced concrete schema/query defects (missing
  discriminators, a mis-typed join key, an un-surfaced timestamp, big-study
  slowness, identity derived through a join that misses composite products). Fix them
  here.

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
  `study_title`, `faculty_sponsor`, `data_access_group`, **`programme`**, `state`, etc.
  Indexed: `id_study_lims`, `uuid_study_lims`, `accession_number`, `name`,
  `faculty_sponsor`. (`programme` is present on the mirror and the `Study` type today
  but is NOT indexed and NOT a grouping/filter dimension yet — D7.)
- **`Sample`/`sample_mirror`** carry `name`, `sanger_sample_id`, `supplier_name`,
  `accession_number`, `donor_id`, `common_name`, `taxon_id`, `description`. Indexed:
  those text columns individually (ci collation) — this is what makes literal-prefix
  and the exact filters fast without any new index.
- **`sample_search_token`** (derived word-token index; `(token, id_sample_tmp)`;
  **43.7 M rows**) backs `/search/sample` word-prefix matching. Retained for the opt-in
  `--words` mode (D3), no longer the default.
- **`library_samples`** carries `pipeline_id_lims` (**library_type**; 122 distinct
  values) linked to samples — the existing exact library-type path.
- **`study_users_mirror`** ALREADY mirrors the study↔person role membership
  (`id_study_users_tmp` PK, `id_study_tmp`, `role`, `login`, `email`, `name`,
  `last_updated`), **indexed on `id_study_tmp` AND `role`** (and `login`/`email`/`name`).
  It backs the existing person→studies endpoints (`/studies/user/:person`,
  `/resolve-person/:term`) via `people.go`. The known stored `role` vocabulary is
  `owner`, `manager`, `data_access_contact`, `follower`, `slf_manager`, `lab_manager`,
  `administrator` (the person→studies default role set is `owner, manager,
  data_access_contact`). **There is no study→users route today** — D7 adds it; NO new
  mirror table is needed, only the endpoint (a single indexed `id_study_tmp` lookup).
- **`seq_product_irods_locations_mirror`** already denormalises `id_iseq_product`,
  `irods_root_collection`, `irods_data_relative_path`, `irods_collection`,
  `irods_file_name`, **`id_sample_tmp`**, **`id_study_lims`**, `last_updated`,
  **`created`** (nullable), `platform`; indexed incl. `(id_study_lims, created)`,
  `(id_study_lims, id_iseq_product)`, `(id_study_lims, id_sample_tmp)`,
  `(id_sample_tmp, id_iseq_product)`. **Crucially it carries `id_sample_tmp` and
  `id_study_lims` on every iRODS row — including merged/composite CRAMs** — so a sample
  or study is matched to its CRAMs by these denormalised keys, NOT by the single-lane
  product id (this is the lever for D8).
- **`iseq_product_metrics_mirror`** mirrors `id_iseq_product`, `id_iseq_flowcell_tmp`,
  `id_run`, `position`, `tag_index`, `id_sample_tmp`, `id_study_lims`, **`qc`**,
  `qc_lib`, `qc_seq`. **`IRODSPath.id_run` / `name` / `id_sample_tmp` are currently
  DERIVED by a LEFT JOIN `seq_product_irods_locations_mirror.id_iseq_product →
  iseq_product_metrics_mirror`** (see `hierarchy.go` `queryIRODSPaths` /
  `queryIRODSPathsWithSample` and the `IRODSPath` doc comment), and this mirror holds
  per-single-lane-product rows — the direct cause of the Q7 gap (D8).
- **`iseq_run_status_mirror` + `iseq_run_status_dict_mirror`** mirror the run-status
  timeline (`id_run`, `date`, `id_run_status_dict`, `iscurrent`; the dict maps to the
  status description). This is the run-date source for D5's shared platforms.
- **`StudyManifest`** (`/study/:id/manifest`, `with_irods`, `file_type`) +
  `CountStudyManifest`; **`/study|sample|run/:id/irods`** (+ counts, `file_type`);
  **`StudyOverview`/`StatusBreakdown`/`SampleProgress`/`RunOverview`/`RunStatus`**;
  people→studies (`/studies/faculty-sponsor/:name`, `/studies/user/:person`,
  `/resolve-person/:term`); the QC roll-up (`qc.go`: `qc` 1/0/NULL → pass/fail/pending,
  precedence fail > pending > pass). **`StudyOverview` carries `name`,
  `accession_number`, `faculty_sponsor`, `data_access_group` but NOT `programme`** — D7
  adds it.
- **The full entity→children list surface** already exists as fixed-shape endpoints and
  typed client methods (this is what D1's generic `export` unifies): study→
  {`samples`, `libraries`, `runs`, `irods`, `manifest`, `samples-with-data`,
  `samples-without-data`}; run→{`samples`, `irods`}; sample→{`lanes`, `irods`,
  `studies`}; library(`pipeline+study`|`id`|`lims-id`|`type`)→`samples`; and the
  people→studies lists. Each has a `/count`.
- **`wa mlwh info <identifier>`** already IS the exact single-identifier lookup: it
  classifies/resolves sanger id, lims id, accession, supplier name, sample/study UUID,
  study lims id/accession, library pipeline/library ids and numeric run id (auto or
  `--type sample|study|run|library`), with NO 3-char minimum. D3 does not duplicate it.
- CLI already has `wa mlwh {info,search,irods,manifest,studies,people,sync,serve}`.
  **D1 REPLACES `wa mlwh irods` with the generic `wa mlwh export`** (the iRODS listing
  becomes `wa mlwh export irods <scope> <id>`); do not keep two commands for the same job.

## Verified facts (checked against the live source `mlwarehouse` and our MySQL mirror, 2026-07-02 / 2026-07-06)

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
  and **NO `id_run`, sample-name, study-name, or file-type column** (it does carry the
  denormalised `id_sample_tmp` and `id_study_lims`); its unique key is
  `(irods_root_collection, id_product)`, so **`id_product` is not unique** — one
  product can have several iRODS rows. Its `id_product varchar(64)` joins to
  `iseq_product_metrics.id_iseq_product char(64)` (the SHA256 product id).
- **Merged multi-lane / composite CRAMs break the manifest's iRODS attribution (Q7).**
  The archived CRAM for a sample sequenced across several lanes is ONE composite iRODS
  object with its OWN composite `id_iseq_product` hash (e.g. study 7568's
  `/seq/illumina/runs/49/49348/lane1-2/plex1/49348_1-2#1.cram`). That composite hash
  matches no single-lane `iseq_product_metrics_mirror` row, so:
  - `StudyManifest --with-irods` (product-grained, one row per single-lane product,
    LEFT JOIN to iRODS on the single-lane `id_iseq_product`) leaves those products'
    `irods_path` empty. For **study 7568**: `manifest count = 780` products,
    `irods cram count = 732` objects, `samples = 732`; the arithmetic closes as
    **780 = 684 single-lane + 96 empty (48 samples × 2 lanes)** and
    **732 = 684 single-lane CRAMs + 48 merged CRAMs**, so **48 samples** get an empty
    `irods_path` even though every one has a CRAM.
  - The study- and run-scoped iRODS lists DO include the merged object via the mirror's
    denormalised `id_study_lims` (study count = 732 includes it), but its derived
    `id_run`, `id_sample_tmp` and `name` come back `0`/`0`/`""` because those are taken
    from the failing single-lane product-metrics LEFT JOIN — so it can't be tied back
    to its sample from a study/run listing.
  - `IRODSPathsForRun` is worse: it is product-metrics-driven (joins the run's
    `iseq_product_metrics_mirror` rows to iRODS by `id_iseq_product`), so it CANNOT see
    the composite at all — `mlwh_irods_paths_for_run(49348)` returns 33 single-lane/
    control objects, none of them the study's merged CRAMs.
  - Only the sample route finds it: `IRODSPathsForSample` filters the iRODS mirror by the
    resolved sample's `id_sample_tmp` (index `(id_sample_tmp, id_iseq_product)`), which
    the merged row carries — so **the mirror already links merged CRAMs to their sample
    by `id_sample_tmp`/`id_study_lims`; only the output identity is mis-sourced from the
    single-lane join.** This is the lever for D8.
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
- **`study_users_mirror` is present and indexed for the inverse direction (Q6).** It
  has an index on `id_study_tmp` (and on `role`), so a study→users listing is a single
  indexed lookup joined to `study_mirror` on `id_study_tmp` — no new mirror table, no
  version-forcing schema change for this part alone. `faculty_sponsor` is a free-text PI
  name on `Study` and is explicitly NOT a `study_users` role (keep that distinction).
- **`programme` is populated but inert (Q6).** `study_mirror.programme` and
  `Study.programme` exist and are returned by the study detail/resolve/list routes, but
  `programme` is not indexed, is absent from `StudyOverview`, and there is no
  studies-by-programme route nor any grouped aggregate over it. `programme` is
  low-cardinality (a controlled set such as `Human Genetics`, `Cancer Genetics and
  Genomics`, `Other`, …); an indexed exact filter/group-by is cheap (D7).
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
| 1 | study cram TSV w/ chosen columns + manual_qc + deliverable, fast, incl. merged CRAMs | **GAP + PERF** | D1: generic `export` subcommand + backing export path; D4 manual_qc/deliverable; D8 merged CRAMs |
| 2 | "starts with hek_r" → 4 | **WRONG** | D3: literal-prefix as the DEFAULT search |
| 3 | constrain a search to an organism (the Mus musculus samples) | **GAP** | D3: `--organism` whole-word/full-name filter over the low-cardinality `common_name` vocabulary |
| 4 | most-recent sample / latest iRODS for study/lab | **GAP** | D2: expose `created`, recency sort, latest endpoints |
| 5 | runs/month by manufacturer & platform | **GAP** | D5: global run aggregation |
| 6 | sequencing by programme + date; studies-by-programme; per-study owners/managers/followers | **GAP + DIRECTION** | D7: `programme` as an indexed filter/group-by + in overview + a grouped sequencing aggregate; the study→users inverse (data already mirrored) |
| 7 | list samples of a study with an iRODS CRAM path, incl. merged multi-lane samples | **BUG (silent drop)** | D8: attribute merged/composite CRAMs to their sample; per-sample CRAM export; make the manifest gap explicit |
| — | big-study aggregates <1 s; per_platform=null bug | **PERF/BUG** | D6 |

## Deliverables (all firm)

### D1 — Fast, generic, column-selectable, multi-format `wa mlwh export` (the flagship; REPLACES `wa mlwh irods`)

Build **one dedicated, purpose-built subcommand** whose single job is: *emit a
caller-shaped table of a parent entity's children — chosen columns, chosen output
format, filtered, ordered, complete, and very fast.* This is NOT a study-only "cram
TSV" and NOT a `--format` flag bolted onto `manifest`; it is a first-class generic
listing command that **supersedes and replaces `wa mlwh irods`** and unifies the
fixed-shape "list the X of a Y" endpoints under column + format control.

- **Name.** Recommended **`wa mlwh export`** (chosen over `tsv`/`files` because it emits
  more than TSV and more than files; over `list` because it also does complete
  extraction and column/format control). The spec may settle a different verb, but it
  MUST be a single generic command, and `wa mlwh irods` must be removed in its favour.
- **Grammar — any entity→children relationship.** Recommended
  `wa mlwh export <children> <parent-kind> <parent-id> [flags]`, generalising the old
  `wa mlwh irods <scope> <id>` (where `irods` was hard-wired) so the child kind is now a
  parameter. Settle the exact positional/flag shape in the spec, but the command MUST
  cover at least these relationships, each backed by the existing (or D7/D8) endpoint and
  each gaining column selection + format + the applicable filters:
  - **`irods` (a.k.a. `files`) of `study` | `sample` | `run`** — the CRAM/data-object
    listing (the flagship; the replacement for `wa mlwh irods`). Merged/composite CRAMs
    are included and attributed per D8.
  - **`samples` of `study` | `run` | `library`**; **`runs` of `study` | `sample`**
    (the "list runs with the columns I want for a sample" case);
    **`libraries` of `study`**; **`lanes` of `sample`**; **`studies` of `sample` |
    `faculty-sponsor` | `user` | `programme`** (programme per D7).
  - **`users` of `study`** (D7 inverse: owner/manager/follower rows).
  - **`sample-crams` of `study`** (D8: one merged-aware CRAM row per sample).
- **Output formats.** `--format tsv|csv|json` (default `tsv`). TSV/CSV = a header row
  then delimited rows; JSON = an array of row objects (keyed by the selected columns).
  Deterministic, stable-across-pages row order (settle per relationship, e.g. iRODS/
  product listings by `id_run, lane, tag_index, name`). `--json` remains a shorthand for
  `--format json`. The MCP layer consumes the structured (JSON) form; a human/pipe
  consumes TSV/CSV.
- **Column selection.** A `--columns` (ordered, comma-separated) selector over a
  documented per-relationship vocabulary. For the flagship iRODS/files-of-study
  relationship, at minimum: `supplier_name` (accept the alias `supplier_sample_name`),
  `sanger_sample_id`, `name` (sample name), `study_accession_number`, `id_study_lims`,
  `manual_qc`, `id_run`, `lane` (position), `tag_index`, `platform`, `created`,
  `irods_path` (the full path = `CONCAT(irods_root_collection, '/',
  irods_data_relative_path)`). Default column set for that relationship covers Q1
  (`supplier_name, study_accession_number, sanger_sample_id, manual_qc, irods_path`).
  Other relationships expose their own child+ancestor field vocabulary (e.g. `runs of
  sample` → `id_run, platform, manufacturer, run_date, date_basis`; `users of study` →
  `role, name, login, email`). Unknown columns are an actionable error listing the valid
  set. Selecting a column never changes filtering (see below).
- **Filters (the shared filter family — see D3/D4).** Applicable per relationship;
  `--file-type` (default `cram` for the iRODS/files/sample-crams relationships,
  filename-suffix semantics as elsewhere); **`--deliverables-only` on by default** for
  the cram file listings (D4, `entity_type`-based), with a flag to include controls/
  sub-products; `--qc pass|fail|pending` (at PRODUCT grain on the file/product listings —
  each row is a product, so it filters on that product's `qc`); `--library-type <val>`
  / `--organism <val>` where the child is or joins samples. Filtering and the returned
  columns are independent (Q1 wanted the `manual_qc` column, not necessarily the filter).
  This is the SAME filter family offered on `wa mlwh search` (D3), differing only in QC
  grain (see D3).
- **Speed: <1 s for a bounded page even for the largest studies** (7699-scale). The
  first page must be web-responsive; a `/count` counterpart and sizing headers are
  required for every relationship. **No silent row cap**: a full listing MUST be able to
  stream/emit *every* matching row (7699 → 100 k+ rows) via **keyset pagination** (not
  `LIMIT OFFSET`, which degrades on deep pages) or a streaming response. State explicitly
  in the CLI when output is a bounded page vs the complete set (`--all` = complete set).
- **Fix the perf** (for the flagship study→iRODS/files scan). Rebuild the backing
  query/schema so the export is a near-single-index-ordered scan:
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
    so the export is one ordered range scan. Decide in the spec; whichever route, the
    target is <1 s per page at 7699 scale and correct totals.
- **Correctness.** `manual_qc` is the product's `qc` value rendered pass/fail/pending
  by `qc.go` (including for merged/composite products — D8 requires the backing data to
  carry a qc for the composite, not a blank). `irods_path` concatenation must be verified
  against real paths (e.g. `.../lane6/plex45/51945_6#45.cram` and the merged
  `.../lane1-2/plex1/49348_1-2#1.cram`). The study 7556 deliverable-only cram count is the
  `entity_type`-derived count (≈886 — assert the actual figure and document any delta;
  see D4), controls dropped. Merged/composite CRAMs are included and attributed to their
  sample (D8): the study 7568 flagship export returns **732** attributed cram rows, not
  684 with 48 blanks.

### D2 — iRODS recency ("latest data" / "most recently sequenced")

- **Add `created` to `IRODSPath`** (RFC3339 UTC; the column is already mirrored). Keep
  the "added to iRODS" wording discipline (it is `created`, never `last_changed`). It is
  a selectable `export` column.
- **Recency ordering + window** on the iRODS list endpoints (and thus the `export irods`
  relationship): an `order_by=created_desc` (and the default stays as-is) plus
  `since`/`until` (half-open `[since, until)` over `created`, matching `SamplesWithData`).
  The study-scoped case is already index-served by `(id_study_lims, created)`; add
  sample-/run-scoped recency support and index as needed.
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
  word in `common_name`); the exact filters below refine a term search and gate the export.
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
    with `SampleProgress.qc`/`StatusBreakdown`). On the D1 export (rows ARE products) it
    matches the raw PER-PRODUCT `qc`. State both grains explicitly.
  - `--deliverables-only` — the D4 deliverable filter (`entity_type`-based).
    **Pass-through for platforms with no source discriminator (PacBio, ONT):** it filters
    only Illumina/Element/Ultima products and leaves PacBio/ONT-only samples unaffected
    (neither positively kept nor dropped), so they never silently disappear (HARD REQ 5).
  Combining a filter with the term intersects the candidate `id_sample_tmp` set (via
  `library_samples` for library-type, the `common_name` index for organism, the
  product/flowcell join for qc/deliverable) — index it so the intersection is not a scan.
  The SAME family is offered on the D1 `export`.
- **Expose via CLI and endpoint/param**, with the registry description stating precisely
  what the default does, what `--words` does, and exactly what each filter matches and
  over which field (incl. the QC grain difference between search and export). Do NOT
  overload one endpoint so the caller must add `%`/anchors.

### D4 — manual_qc & deliverable semantics (feeds D1 and the D3 filter family; also the manifest)

- **Expose `manual_qc`** (the `qc` roll-up: pass/fail/pending; the raw 1/0/NULL may be
  offered additionally) wherever product/iRODS rows are listed: the D1 export,
  `StudyManifest` rows, optionally the iRODS listings. Reuse `qc.go`'s roll-up so it can
  never disagree with `SampleProgress.qc` / `StatusBreakdown`. For merged/composite
  products, `manual_qc` must resolve to the composite's own `qc` (D8), not a blank.
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
  ONT→Oxford Nanopore); state the mapping. **This endpoint's grouping is generalised in
  D7** to also group by a study attribute (`programme`, `faculty_sponsor`) so Q6's
  "PacBio sequencing by programme" is one call.
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
  removes the "there is no all-runs endpoint" gap. This listing is one of the `export`
  relationships (`export runs …`).
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

### D7 — `programme` as a first-class dimension, and the study→users inverse (Q6)

Make `programme` a groupable/filterable dimension and let a study's role members be
listed FROM the study. The data for both is already mirrored; this is mostly endpoints,
one index, and one additive `StudyOverview` field.

- **`programme` in `StudyOverview`.** Add `programme` to the `StudyOverview` response
  (alongside `name`, `accession_number`, `faculty_sponsor`, `data_access_group`) so a
  per-study pass can group/label by programme in ONE call, not a second
  `study_detail`/`resolve` per study (the Q6 two-calls-per-study trap over 8,223 studies).
- **`programme` as an indexed exact filter and grouping key.** Index
  `study_mirror.programme`. Add:
  - **A studies-by-programme listing** (`/studies/programme/:name`, with `/count`) — an
    exact, indexed "studies in programme X" route (NOT the substring `search/study`
    conflation of name/title/programme/sponsor), so Q6 turn 2 ("which 5 studies") is a
    clean filter, and so `export studies programme "Human Genetics"` works. Support the
    same date/platform narrowing the aggregate uses where useful.
  - **A programme enumeration** (`/programmes`) returning the distinct `programme`
    vocabulary (with study counts) so a caller can discover the controlled values instead
    of guessing them.
- **A grouped sequencing aggregate (generalise D5's grouping).** One aggregate endpoint
  answers "PacBio sequencing by programme for the last year" directly:
  `group_by ∈ {programme, faculty_sponsor, platform, manufacturer, month}` (combinable),
  an optional `platform` filter, a `since`/`until` window, and an explicit **`unit`** —
  because "sequencing" is ambiguous, the endpoint states and lets the caller choose the
  counted unit: **`runs`** (run grain + `date_basis` per D5), or **`samples`/`products`**
  (data grain, each product mapping to exactly ONE study→programme, windowed by iRODS
  `created`). Every response row states `unit` and `date_basis`/date field used, plus
  `cache_synced_at`. When grouping runs by a study attribute, define and STATE the
  attribution rule for a run spanning multiple studies/programmes (count distinct per
  group; a multi-study run appears in each) — do not leave it implicit. Prove
  index-served (EXPLAIN); do not fan out over studies server-side.
- **The study→users inverse (`/study/:id/users?role=…`).** Add the missing direction:
  given a study, list its role members — `role`, `name`, `login`, `email` — one indexed
  `study_users_mirror.id_study_tmp` lookup joined to `study_mirror`. `role` is an
  optional comma-separated filter over the stored vocabulary (`owner`, `manager`,
  `data_access_contact`, `follower`, `slf_manager`, `lab_manager`, `administrator`);
  default to returning all roles present (state the default). This closes Q6 turn 3
  (owners/managers/followers per study in one call) and is exposed as the
  `export users study <id>` relationship. Keep the documented distinction: the
  `faculty_sponsor` PI name is a `Study` field, NOT a `study_users` role.
- **No new mirror table is required for users** (`study_users_mirror` exists and is
  indexed on `id_study_tmp`/`role`); `programme` needs only an index and the additive
  overview field and the two programme routes. Confirm the sync already populates
  `study_users_mirror` fully; if not, extend it.

### D8 — Merged multi-lane / composite CRAM attribution (Q7)

Make merged/composite CRAMs first-class rather than silently dropped, using the lever
that the iRODS mirror already denormalises `id_sample_tmp`/`id_study_lims` on every
row (including the composite object) — so attribution needs the sample↔iRODS link, NOT
the single-lane product-metrics join and NOT composition/component mirroring.

- **Attribute iRODS objects to their sample from the mirror's own denormalised keys.**
  In the study- and sample-scoped iRODS listings (and thus the D1 `export irods/files`
  relationship), source `id_sample_tmp` — and the sample `name`/`supplier_name` (via a
  `sample_mirror` join on that `id_sample_tmp`) — from the iRODS mirror row itself, NOT
  from the `id_iseq_product → iseq_product_metrics` LEFT JOIN. Then a merged/composite
  CRAM is correctly tied to its sample. `id_run` legitimately spans lanes for a merged
  object, so it may be `0`/multi-valued — represent that honestly (e.g. a `merged: true`
  flag and/or the set of contributing runs) rather than a misleading single `id_run`.
- **A per-sample CRAM export: `export sample-crams study <id>`** (backing endpoint e.g.
  `GET /study/:id/sample-crams`) — one row per sample: `sample_name`, `ega_id`
  (the sample accession), `irods_cram_path`, resolving each sample's cram through the
  sample↔iRODS-mirror linkage so merged CRAMs are included and each sample's multi-lane
  rows collapse to one CRAM. This is the clean single-call answer to Q7's exact request
  and must return **all 732** samples of study 7568 with a path, none blank.
- **Make the manifest gap explicit (don't silently emit an empty path).** For the
  product-grained `StudyManifest --with-irods`, when a product's CRAM cannot be matched
  through the single-lane join because it is part of a merged/composite object, either
  (preferred) resolve it via the sample↔iRODS linkage so the row carries the merged
  path, or at minimum add a per-row `irods_unmatched: true` with a `reason`
  (`merged_multilane`) AND an envelope counter `products_without_irods`, so the caller
  sees the shortfall without reconstructing it from count discrepancies. Update the
  manifest registry `Description` to name merged multi-lane CRAMs as the common cause of
  an empty `irods_path` (today it reads like missing data).
- **`manual_qc`/deliverable for composite products.** Ensure the backing data can
  render `manual_qc` and the deliverable flag for a composite product (its own
  `iseq_product_metrics.qc` / flowcell `entity_type`), so a merged-CRAM export row is not
  silently blank in those columns. If the current mirror only holds single-lane product
  rows, extend the sync to include the composite product rows needed to carry these
  columns (do NOT mirror full composition/component structure — only what attributes and
  qc-labels the composite object).
- **Verify** with study 7568 (the realworld case): 780 manifest products, 732 cram
  objects, 732 samples; 48 samples on the multi-lane run 49348 whose CRAM is the merged
  `lane1-2` object. Assert the per-sample cram export = 732 rows all populated, and that
  the study `export irods` cram listing attributes all 732 (no `name:""`/`id_sample_tmp:0`
  for the merged rows).

## CLI exposure (REQUIRED — part of every deliverable)

Every new capability must be reachable from `wa mlwh`, in both local-cache and
`--server` modes, with graceful degradation (not-found/empty/not-tracked render
cleanly, exit 0), matching existing `wa mlwh` behaviour:

- **D1:** the new generic **`wa mlwh export <children> <parent-kind> <parent-id>
  [--columns …] [--format tsv|csv|json] [--file-type cram] [--deliverables-only]
  [--qc pass] [--library-type X] [--organism Y] [--sort created-desc]
  [--since/--until] [--limit/--offset|--all] [--server] [--json]`**, emitting the chosen
  format to stdout. This **replaces `wa mlwh irods`** (`wa mlwh export irods study 5901
  --file-type cram` is the old command) and is the command a user runs to get the exact
  file from Q1, or "the runs (with these columns) for a sample" (`wa mlwh export runs
  sample DN1234`), etc.
- **D2:** recency via `wa mlwh export irods <scope> <id> --sort created-desc
  [--since/--until]` and a `wa mlwh latest <study|--faculty-sponsor NAME>
  [--file-type cram]` (or an `info`/manifest section) for "most recent data".
- **D3:** `wa mlwh search <term> [--words] [--library-type X] [--organism Y]
  [--qc pass|fail|pending] [--deliverables-only] [--type study|sample]`. The `<term>` is
  required; default matching is literal-prefix; `--words` selects the retained
  word-prefix mode; the exact filters are the shared family; exact single-identifier
  lookups remain `wa mlwh info`.
- **D5:** `wa mlwh runs --monthly [--since/--until] [--platform …]` (grouped counts)
  and the `wa mlwh export runs …` listing.
- **D4:** `manual_qc` column/section wherever product rows render (`info <study>`,
  `manifest`, `export`); a `--deliverables-only` toggle where iRODS/product rows list.
- **D7:** `wa mlwh studies --programme "Human Genetics"` (and `wa mlwh export studies
  programme "Human Genetics"`); a programme listing (`wa mlwh programmes` or a
  `studies` sub-view); `wa mlwh runs --monthly --group-by programme --platform PacBio
  --since … --until …` (the generalised grouped aggregate); `programme` shown in
  `info <study>`/overview; and `wa mlwh export users study 7568 --role
  owner,manager,follower` (the study→users inverse).
- **D8:** `wa mlwh export sample-crams study 7568` (one merged-aware CRAM per sample);
  the manifest's unmatched flag/counter surfaced in `wa mlwh manifest`; merged CRAMs
  attributed in `wa mlwh export irods study <id>`.

## HARD REQUIREMENTS

1. **Web-responsive (<1 s) for bounded pages, including the largest studies**, and one
   cheap call for every count/overview/aggregate. Prove index-served paths with
   EXPLAIN. No per-row correlated subqueries or full scans of the large mirrors.
2. **No silent truncation.** Any "give me all …" (a full study export, a full path list,
   a full sample-crams list) must return the complete set via keyset paging/streaming, or
   clearly report that output is a bounded page plus a total and next cursor. (The
   comparison implementation's silent 1000-row cap is exactly the failure mode to avoid.)
3. **Correctness baked in, not delegated to caller SQL.** manual_qc, deliverable,
   "cram" (filename suffix), recency basis (`created`), run `date_basis`, the search
   default (literal-prefix), the `--words` mode, the exact filters
   (organism/library-type/qc/deliverable), the `programme` grouping/attribution, and the
   study→users direction are defined and enforced server-side and stated in the registry
   text. The generic dynamic-call path is a fallback, never the intended way to answer
   these seven questions.
4. **Source-true semantics.** `manual_qc` = `iseq_product_metrics.qc` (1/0/NULL),
   rolled up as `qc.go` does. There is no scalar `target` column; deliverable-only is
   the `entity_type`-based (Illumina) / `is_sequencing_control`-based (Element/Ultima)
   filter, NOT `is_spiked`. Run `date_basis` follows the per-platform reference (Illumina/
   Element `run complete`, Ultima `run archived`, PacBio `run_complete`, ONT the
   `last_updated` warehouse-load fallback, labelled). iRODS `created` = data added;
   `last_changed` = sync key (not surfaced as "new data"); `cache_synced_at` /
   `/freshness` = freshness caveat. `faculty_sponsor` is a `Study` field, NOT a
   `study_users` role; owner/manager/follower ARE `study_users` roles.
5. **Platform-aware; never a false "no data".** Keep uniform multi-platform treatment.
   Where a platform lacks a capability (ONT has no product/qc/iRODS-cram and no true
   sequencing date), say so explicitly; never collapse to a bare zero or a silent drop.
   Run aggregation covers all platforms including ONT, whose bucket carries the explicit
   "warehouse load time — not a true sequencing date" `date_basis` label.
   `--deliverables-only` is pass-through on platforms with no deliverable discriminator
   (PacBio/ONT) rather than dropping their samples.
6. **No silently dropped rows in listings/exports (Q7).** A merged/composite CRAM must be
   attributed to its sample and appear in the study/sample iRODS listings, the D1 export,
   and the per-sample cram export — never with `name:""`/`id_sample_tmp:0`, never omitted.
   The product-grained manifest either resolves merged CRAMs or makes the shortfall
   explicit (`products_without_irods` + per-row flag) and documents the cause. The study
   7568 cram export/sample-crams return all 732 samples with a path.
7. **Cache correctness.** New mirror tables/columns — the new `iseq_flowcell` mirror
   (`entity_type`, `pipeline_id_lims`, keys); any organism helper structure over
   `common_name`; the extended `oseq_flowcell_mirror` ONT run columns
   (`run_id`/`run_uuid`/`experiment_name`/`last_updated`); a normalized+indexed run-date
   for the monthly grouping; run-aggregation indexes; the `study_mirror.programme` index;
   the `iseq_product_metrics_mirror` PK/`char(64)` change; any composite-product rows
   needed for merged-CRAM qc/deliverable columns; any deliverable/export structure — must
   be added to BOTH `sqlite` and `mysql` schema dialects (kept in parity), to
   `mlwh/sync.go`'s source selection, AND to the cold-load sparse read-index set where the
   mirror is large, with a **`CacheSchemaVersion` bump (12 → 13; full resync acceptable)**.
   (`study_users_mirror` already exists — reuse it; do not recreate it.)
8. **Tests.** TDD with behavioural tests; preserve all existing regressions. Add
   **real-MySQL integration tests** (`mlwh/cache_mysql_integration_test.go` pattern:
   throwaway DB, dropped on cleanup, skipped without creds) asserting the new paths
   execute on MySQL, are index-served (EXPLAIN), and return correct counts/rows:
   - `hek_r` default (literal-prefix) search = the **4** `Hek_R1..4`.
   - the `--organism musculus` filter matches by WORD-MEMBERSHIP — every `common_name`
     containing the word `musculus`, INCLUDING subspecies (e.g. `Mus musculus castaneus`),
     so the count exceeds the dominant `Mus Musculus` value's 235,447 alone; assert the
     actual word-membership count, and that the mid-word fragment `usculus` matches
     **nothing** (locks word-membership — not substring, not exact-whole-value).
   - study 7556 deliverable-only cram export = the `entity_type`-derived count (**≈886**;
     assert the actual figure, document any delta), controls/spikes dropped where present.
   - a `--qc` filter on sample search buckets by the per-sample roll-up, while `--qc` on
     the export filters per-product (the grain difference).
   - D5 run counts are at RUN grain: PacBio ≈ 2,843 distinct `pac_bio_run_name`s (not
     12,499 wells), ONT ≈ 447 distinct `experiment_name`s (not per-flowcell), Ultima
     `run archived` = 242; ONT buckets by the labelled warehouse-load date, and
     `--deliverables-only` leaves PacBio/ONT samples in (pass-through).
   - **D7:** the study→users listing for a study returns its `study_users` rows with the
     right roles (owner/manager/follower present), from one `id_study_tmp` lookup;
     `/studies/programme/:name` returns exactly the studies with that `programme` (and
     agrees with its `/count`); the grouped aggregate for `platform=PacBio,
     group_by=programme, since/until` returns per-programme counts stating `unit` and
     `date_basis`.
   - **D8:** study 7568 — the per-sample cram export returns **732** rows all with a
     populated `irods_cram_path` (the 48 run-49348 merged-CRAM samples included, each
     collapsed to one), and the study `export irods` cram listing attributes all merged
     rows to a sample (no `id_sample_tmp:0`/`name:""`); the manifest surfaces
     `products_without_irods` = 48 (or resolves them).

   Add a **source integration test** (`mlwh/sync_source_integration_test.go` pattern)
   covering the new source columns/tables (`iseq_flowcell.entity_type`/`pipeline_id_lims`,
   `{eseq,useq}_product_metrics.is_sequencing_control`, `iseq_product_metrics.qc`, the run
   dates incl. the `iseq_run_status` `run complete`/`run archived` basis and
   `oseq_flowcell.last_updated`, `study_users` role rows, `study.programme`, and a
   merged/composite iRODS object's `id_sample_tmp`/`id_study_lims`) so the schema these
   tests assume stays true.
9. **Registry descriptions are the contract.** Each new/changed endpoint's
   `Description`/`Summary`/`Query` must precisely state the definition used
   (manual_qc, deliverable via `entity_type`, "cram" suffix, `created` recency, the
   per-platform run `date_basis` incl. the ONT warehouse-load caveat, the search default
   vs `--words` vs the exact filters and the fields each covers, the `programme`
   grouping/attribution unit, the study→users direction and role vocabulary, the
   merged-CRAM attribution, and the freshness caveat), because the downstream MCP server
   surfaces this text verbatim.
10. **API version bump** (1.7.0 → 1.8.0) alongside the `CacheSchemaVersion` bump, per
    `openapi.go`'s documented lineage.

## Design decisions for the spec to settle (HOW, not WHETHER)

- The generic `export` subcommand's exact name/positional-vs-flag grammar, the child/
  parent relationship vocabulary and per-relationship column vocabularies + defaults, the
  format set (tsv/csv/json) and JSON row shape, the deterministic row order per
  relationship, and keyset vs streaming for the full export; whether the flagship backing
  path reuses/refactors `manifest.go`/`hierarchy.go` or adds an export structure.
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
- **D7:** the studies-by-programme + programme-enumeration route shapes and the
  `study_mirror.programme` index; the grouped aggregate's `group_by`/`unit`/`platform`/
  window param surface and the multi-study run-attribution rule; the study→users route
  shape and its default role behaviour.
- **D8:** the exact mechanism that attributes merged/composite CRAMs to samples (source
  `id_sample_tmp`/`id_study_lims` from the iRODS mirror row vs the product join), how
  `id_run`/`merged` is represented for a composite object, the `sample-crams` endpoint
  shape and per-sample de-duplication, and whether the manifest resolves merged CRAMs or
  only flags+counts them; what minimal composite-product rows (if any) the mirror needs
  to carry `manual_qc`/deliverable for composites without mirroring full composition.
- Index choices for each new path and the additive-vs-recreate migration detail.

## Pointers / prior art

- Endpoints + descriptions: `mlwh/registry.go`. Result types/json tags: `mlwh/types.go`
  (`IRODSPath`, `ManifestRow`, `StudyManifest`, `Study`, `Sample`, `Lane`, `PersonStudy`,
  `PersonCandidate`; `Run`/`Library` in `mlwh/mlwh.go`). Manifest: `mlwh/manifest.go`.
  iRODS + list SQL (incl. `IRODSPathsFor{Study,Sample,Run}`, `queryIRODSPaths` vs
  `queryIRODSPathsWithSample`, and all the entity→children list methods the `export`
  command unifies): `mlwh/hierarchy.go`. Counts: `mlwh/count.go`. Availability/recency/
  overview: `mlwh/availability.go`. QC/breakdown/progress: `mlwh/progress.go`,
  `mlwh/qc.go`. Search (incl. the existing `sampleFullPrefix*` literal-prefix machinery
  and `sampleSearchCountCap`): `mlwh/search.go`. People / study_users (person→studies
  today; the study→users inverse and the resolve-person directory live here):
  `mlwh/people.go`. Exact single-identifier resolution (reused by `info`, not
  duplicated): `mlwh/resolver.go`, `mlwh/enrich.go`, `mlwh/hierarchy.go`
  (`FindSamplesBy*`).
- Sync source selection + cold-load read-index sets + tokeniser: `mlwh/sync.go`,
  `mlwh/sync_platform_coverage.go`, `mlwh/cache.go`. Schema:
  `mlwh/cache_schema/{sqlite,mysql}/*.sql` (note `study_users_mirror.sql` and
  `seq_product_irods_locations_mirror.sql` already exist), `mlwh/cache_schema.go`.
  Client + paging: `mlwh/remote.go`. CLI: `cmd/mlwh_irods.go` (the command being replaced
  by `export`), `cmd/mlwh_manifest.go`, `cmd/mlwh_search.go`, `cmd/mlwh_info.go`,
  `cmd/mlwh_studies.go`, `cmd/mlwh.go` (subcommand wiring).
- Run `date_basis` authority: `mlwh://reference/sequencing-timestamps` in
  `~/dj3-mlwh-mcp/src/index.ts` (and its `AGENTS.md` § Platform-specific schema
  knowledge) — the per-platform reliable date field and join path; validated against
  source 2026-07-02. That reference also confirms `seq_product_irods_locations` has no
  `id_run` (the Q7/D8 root) and provides the platform product-metrics join pattern.
- Integration-test patterns: `mlwh/cache_mysql_integration_test.go`,
  `mlwh/sync_source_integration_test.go`. Bugfix checklists: `.docs/bugfixes/`.
- Source schema (authoritative, for the source integration tests): DBIx::Class result
  classes at `wtsi-npg/ml_warehouse`
  (`lib/WTSI/DNAP/Warehouse/Schema/Result/{Sample,Study,StudyUser,IseqFlowcell,IseqProductMetric,SeqProductIrodsLocation,IseqRunStatus,OseqFlowcell,...}.pm`);
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
  the monthly grouping, the run-aggregation indexes, the `study_mirror.programme` index,
  the `iseq_product_metrics_mirror` PK/`char(64)` change, any composite-product rows for
  merged-CRAM columns, and any export structure. Do NOT take the additive
  no-version-bump path for the key-type change. `study_users_mirror` already exists —
  the study→users work adds only an endpoint, not a table.

### D1 export
- A **single generic subcommand** (recommended `wa mlwh export`), NOT a study-only "tsv"
  and NOT a format flag on `manifest`; it **replaces `wa mlwh irods`**. It works over any
  parent→children relationship (irods/files, samples, runs, libraries, lanes, studies,
  users, sample-crams), with `--columns` selection, `--format tsv|csv|json` (default
  tsv; `--json` = `--format json`), the shared filter family, deterministic paging, and
  `--all` for the complete set (keyset). Default columns for the flagship iRODS/files-of-
  study relationship cover Q1 (`supplier_name, study_accession_number, sanger_sample_id,
  manual_qc, irods_path`); accept `supplier_sample_name` as an alias for `supplier_name`.
  `--deliverables-only` defaults **on** for the cram file listings. `manual_qc` is the
  pass/fail/pending roll-up string (raw-value column may be offered additionally). The CLI
  always states whether it gave a bounded page or the complete set.

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
- **Shared exact filter family** on both `search` and `export`, server-side, AND-combined,
  fast/indexed (EXPLAIN), exempt from the 3-char minimum: `--library-type` (exact
  `pipeline_id_lims`), `--organism` (**word-membership** over the ~16 k `common_name`
  vocabulary — matches whole words incl. subspecies; NOT mid-word substring and NOT
  exact-whole-value; `usculus` matches nothing), `--qc pass|fail|pending`
  (**grain-appropriate**: per-sample roll-up on search, per-product on the export),
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
  (`ont:<experiment_name>`), with platform and native id also carried separately. The
  grouping is generalised in D7 (group by `programme`/`faculty_sponsor`, with a `unit`).

### D7 programme + study→users
- `programme` becomes an indexed exact filter/group-by and gains a studies-by-programme
  route (+`/count`), a `/programmes` enumeration, and a slot in `StudyOverview`. The
  grouped sequencing aggregate generalises D5's grouping to include study attributes
  (`programme`, `faculty_sponsor`) with a stated, caller-chosen `unit` (runs at run grain
  + `date_basis`, or samples/products at data grain windowed by iRODS `created`); a run
  spanning multiple studies is counted once per group it touches (state it). The
  study→users inverse (`/study/:id/users?role=…`) is a single indexed
  `study_users_mirror.id_study_tmp` lookup returning `role/name/login/email`, with the
  role vocabulary documented; `faculty_sponsor` stays a `Study` field, not a role. No new
  mirror table; `study_users_mirror` is reused.

### D8 merged CRAM attribution
- Attribute merged/composite CRAMs to their sample using the iRODS mirror's own
  denormalised `id_sample_tmp`/`id_study_lims` (already populated on composite rows and
  indexed), NOT the single-lane `id_iseq_product → iseq_product_metrics` join. Represent a
  composite object's run honestly (`merged: true` and/or the contributing run set) rather
  than a misleading single `id_run`. Add a per-sample `sample-crams` export (one CRAM per
  sample, merged-aware, de-duplicated) that returns all 732 samples of study 7568 with a
  path. The product-grained manifest resolves merged CRAMs or, at minimum, flags them
  (`irods_unmatched`/`reason=merged_multilane`) with an envelope `products_without_irods`
  counter, and its description names merged multi-lane CRAMs as the cause. Do NOT mirror
  full composition/component structure — carry only what attributes the composite object
  and (if needed) its `manual_qc`/deliverable columns.

### Perf posture
- Every study-scoped answer stays a single indexed scan on a denormalised column (the
  proven mirror win); the export and the compound aggregates must be brought to
  <1 s at 7699 scale before this ships, verified by EXPLAIN and the integration tests.
