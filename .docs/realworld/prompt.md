# Feature: answer sequencing-data availability, recency & sample progress, cheaply

> This prompt is the feature description for the **`wa` MLWH REST API** (the
> `wa mlwh serve` command, code under `mlwh/`). It will be moved into the `wa`
> repo and fed to that repo's spec-writer workflow. All paths below are relative
> to the `wa` repo root. The requester maintains a downstream consumer — the
> **MLWH MCP server** (`mlwh-mcp-server`), a thin read-only bridge that turns
> these endpoints into agent tools — which can only ever be as good as the
> endpoints this API offers.
>
> **Scope rule for this spec: everything described here is in scope to build.**
> The "Design decisions" section settles *how* each item is implemented, never
> *whether*. There are no optional items.

## Summary

Make the most common class of user question about a study answerable **cheaply —
one request, small response**:

- **"How many samples in study X have sequencing data available, and how many do
  not?"**
- **"Is there any *new* sequencing data available for study X this week?"** —
  i.e. data **added to iRODS** within a recent window.
- "Which samples have data / which are still missing it?"
- "What's in study X?" / "How much data is there?"
- **"What's happening with my sample?"** — where it is in the sequencing pipeline
  right now, and how long it has spent in each phase.

Today the API cannot answer any of these without abuse. Most needed facts are
already cached; the gaps are the iRODS-creation timestamp, the per-sample
ops-tracking milestones (`mlwh_reporting.seq_ops_tracking_per_sample`), the
per-platform sequencing-status tables, and — critically — a sample↔iRODS linkage that
spans **all sequencing platforms** (Illumina, PacBio, ONT, Elembio, Ultimagen), not
just Illumina. This feature adds the small, indexed aggregate + recency +
pipeline-progress + budget-safety surface, **platform-aware throughout** (Platform
coverage §), to close that.

## Why this is needed (the motivating incident — read this)

An agent asked "how many of study 7607's 428 samples have sequencing data?" had
only bad options:

1. `GET /study/:id/samples/count` → `428`: counts samples, not samples with
   *data*.
2. `GET /study/:id/irods` → the per-study iRODS list. **Huge** (735 rows /
   ~170 KB here; far larger in production) — it blew the downstream MCP client's
   token budget and was spilled to a file — **and** each `IRODSPath` row carries
   **no sample identity** (`id_product`/`collection`/`data_object`/`irods_path`),
   so it cannot be aggregated back to "distinct samples with data" anyway.
3. `GET /study/:id/detail` → also huge (~600 KB) and carries **no** iRODS/lane
   info per sample despite its name.

The only thing that worked was enumerating the 428 sample names and calling
`GET /sample/:id/irods` **428 times** — N round-trips for one aggregate, and not
viable through MCP at all. And there is currently **no way whatsoever** to ask the
recency question ("new this week"). The data is cached; the API just never exposes
the aggregates.

## Platform coverage — every platform, one uniform mechanism

These questions must work for **non-Illumina users**, or their first complaint is
"nothing works". The obstacle (verified): `seq_product_irods_locations` has **no
sample column** — its only sample link is `id_product` → a *per-platform*
`*_product_metrics` table — so the single Illumina join the cache uses today silently
drops every other platform (PacBio sample `DTOL10298321`: **2 iRODS objects, 0
Illumina products → a false "no data"**).

**Do every platform the same way.** Each sequencing platform plugs into one generic
model via its own tables; a capability is present exactly where that platform's
schema provides it — no platform is special-cased or deferred. The iRODS sample/study
linkage in the cache becomes a **UNION across all platforms' product-metrics tables**
(carrying a `platform` column = `seq_product_irods_locations.seq_platform_name`); QC
and sequencing status/dates come from each platform's own metrics/run tables; the ops
milestone timeline (`seq_ops_tracking_per_sample`) is already platform-agnostic.

Per-platform table map (confirmed in the source; the implementer confirms the exact
sample-link column per platform):

| Platform | product-metrics (→ iRODS `id_product`) | sample/study link | QC | status + dates |
|---|---|---|---|---|
| Illumina | `iseq_product_metrics` (`id_iseq_product`) | `iseq_flowcell` | `qc`/`qc_seq`/`qc_lib` | `iseq_run_status`, `iseq_run_lane_metrics` |
| PacBio | `pac_bio_product_metrics` (`id_pac_bio_product`) | `pac_bio_run` | `qc` | `pac_bio_run_well_metrics` (`run_start`/`run_complete`/`well_complete`/`qc_seq_date`, `run_status`/`well_status`) |
| Elembio | `eseq_product_metrics` (`id_eseq_product`) | `eseq_flowcell` | `qc`/`qc_seq`/`qc_lib` | `eseq_run`, `eseq_run_lane_metrics` |
| Ultimagen | `useq_product_metrics` (`id_useq_product`) | `useq_wafer` (carries `id_sample_tmp`/`id_study_tmp`; join `useq_product_metrics.id_useq_wafer_tmp`) | `qc`/`qc_seq`/`qc_lib` | `useq_run_metrics` |
| **ONT** | **— none —** | `oseq_flowcell` | **—** | **—** |

So Illumina/PacBio/Elembio/Ultimagen link **identically** (each product-metrics table
carries the iRODS `id_product` + QC). **ONT is the only platform with no
product-metrics / iRODS / QC / run-status** — just `oseq_flowcell` identity+metadata
(8,719 flowcells / 7,322 samples / 78 studies). Through the *same* code paths ONT
resolves identity, study, and the milestone timeline, and returns an explicit "not
tracked for ONT" on availability/recency/QC/status. Verified iRODS presence today:
Illumina ✓, PacBio ✓, Ultimagen ✓, Elembio ✗ (0 rows, wired the same), ONT ✗.

**Universal guardrail (HARD REQUIREMENT 11): never a bare "no data".** Every response
carries `platform`; every negative is platform-qualified; any capability a platform's
schema lacks yields a clear "not supported/tracked for &lt;platform&gt;" — derived
from the sample's detected platform(s) — never silence, never a false negative.

## Three timestamps — do not conflate them

The recency question hinges on picking the right time. There are three, and only
one is the answer:

1. **When the data was added to iRODS** — the iRODS-location **creation** time,
   the source column **`seq_product_irods_locations.created`** (`datetime`,
   `DEFAULT CURRENT_TIMESTAMP`, set once at insert — verified against the live
   warehouse). This is the *only* thing "any new data this week?" is about.
2. **`last_updated`** — the MLWH row's last-*changed* time (the source column
   **`seq_product_irods_locations.last_changed`**, `datetime ... on update
   CURRENT_TIMESTAMP`, which the mirror stores as `last_updated`). This is what the
   cache syncs on (it is the `sync_state.high_water`; `mlwh/freshness.go:54`
   documents `HighWater` as "latest synced last_updated"). It is a **proxy that
   conflates newly-added data with later-modified data** (QC edits, re-loads,
   collection moves all bump it), so it is the **wrong** signal for "new" and must
   not be presented as such.
3. **`last_run`** — when **`wa` last synced** its cache from MLWH
   (`sync_state.last_run`, surfaced by `GET /freshness`). Users do **not** care
   about this as the answer; it only **bounds how complete** a recent-window
   answer can be (data added to iRODS after the last sync is not in the cache
   yet). It is the **freshness caveat**, never the answer.

Consequence: `seq_product_irods_locations.created` is **not currently mirrored** —
the sync source queries select only `spi.last_changed` (`mlwh/sync.go` ~572–592)
and the mirror carries only `last_updated`
(`mlwh/cache_schema/sqlite/seq_product_irods_locations_mirror.sql:9`). Answering the
recency question correctly therefore **requires a cache schema change**: carry
`created` into the mirror (see deliverable R).

## Background: what exists today (this code is authoritative — read it)

- **The endpoint registry.** `mlwh/registry.go` — the `Registry` slice
  (`Endpoint`: `Method`, `Verb`, `Path`, `PathParams`, `Paginated`, `NewResult`,
  `Summary`, `Description`, `QueryParams`) generates `/openapi.json` and the
  endpoint reference, so it cannot drift. Mirror these entries:
  `CountSamplesForStudy` (~463–471), `SamplesForStudy` (~138–148),
  `IRODSPathsForStudy` (~258–268), `StudyDetail` (~380–388),
  `LanesForSample` (~234–244), plus `fetchAllPaginationParams()` and
  `newSliceResult`/`newResult`.
- **The count template.** `mlwh/count.go` — `CountSamplesForStudy` (~70–117) and
  the reusable `queryCount` helper (~120–133). Its SQL
  (`countSamplesForStudyCacheSQL`, ~42) joins `library_samples` → `sample_mirror`
  on `id_study_lims`, `COUNT(DISTINCT id_sample_tmp)`, and handles
  synced-with-rows / synced-empty / never-synced (`ErrCacheNeverSynced`).
- **The iRODS query + its dropped/absent columns.** `mlwh/hierarchy.go` —
  `IRODSPathsForStudy` (~1215–1248), `IRODSPathsForSample` (~1179–1212),
  `queryIRODSPaths` (~1298–1325). The study query already reads
  `seq_product_irods_locations_mirror WHERE id_study_lims = ?`; that table carries
  `id_sample_tmp` and `last_updated` but the `SELECT` and the `IRODSPath` struct
  (`mlwh/types.go` ~101–106) project neither.
- **The lanes query.** `mlwh/hierarchy.go` `LanesForSample` (~1124–1176) over
  `iseq_product_metrics_mirror` (also carrying `id_sample_tmp`, `id_study_lims`,
  `id_run`, `position`, `tag_index`, `last_updated`).
- **The cache schema (what's mirrored).**
  `mlwh/cache_schema/{sqlite,mysql}/seq_product_irods_locations_mirror.sql`:
  columns `id_iseq_product`, `irods_root_collection`,
  `irods_data_relative_path`, `irods_collection`, `irods_file_name`,
  **`id_sample_tmp`**, **`id_study_lims`**, **`last_updated`**; indexes
  `seq_product_irods_locations_mirror_id_sample_tmp_idx` and
  `spi_mirror_study_lims_sample_tmp_idx (id_study_lims, id_sample_tmp)`. Note there
  is **no creation-time column** and **no `(id_study_lims, last_updated)` /
  `(id_study_lims, created)` index** yet. `study_mirror` (key `id_study_lims`,
  has `last_updated` + index), `sample_mirror` (key `name`, has `last_updated` +
  `sample_mirror_last_updated_idx`), and `library_samples` complete the graph.
- **Incremental sync + freshness.** `sync_state` (`table_name`, `high_water`,
  `last_run`, `resume_cursor`, `indexes_dropped`) drives incremental sync keyed on
  `last_changed`; `mlwh/freshness.go` (`Freshness`, `TableFreshness{HighWater,
  LastRun, EverSynced}`, ~50–93) reports per-table `high_water` and `last_run`.
- **The iRODS sync source queries.** `mlwh/sync.go` ~560–592 holds the
  `seq_product_irods_locations` source SELECTs (initial / resume / incremental, in
  two join variants). They select
  `spi.id_seq_product_irods_locations_tmp, spi.id_product,
  spi.irods_root_collection, spi.irods_data_relative_path, ifc.id_sample_tmp,
  study.id_study_lims, spi.last_changed` — keying the incremental window on
  `spi.last_changed` and storing it as the mirror's `last_updated` (the row struct
  ~2542, batch insert ~2586, column list ~137). **`spi.created` is not selected**;
  the verified source columns are `created` (set at insert) and `last_changed`
  (bumped on update). Deliverable (R) adds `spi.created` to every one of these
  variants. The incremental window stays keyed on `last_changed`; `created` rides
  along (a new row has `created == last_changed`, so it is captured the first time
  it crosses the high-water mark).
- **The per-sample tracking table (verified in the source).**
  `mlwh_reporting.seq_ops_tracking_per_sample` — a **BASE TABLE** in the
  `mlwh_reporting` schema (readable by the same read-only user; all rows
  `id_lims = 'SQSCP'`), one row per tracked sample (PK `id_sample_lims_composite`;
  lookup keys `id_sample_lims`, `sanger_sample_id`, `sanger_sample_name`,
  `study_id`). It carries the pipeline milestones as named `datetime` columns, in
  order: `manifest_created` → `manifest_uploaded` → `labware_received` →
  `order_made` → `working_dilution` → `library_start` → `library_complete` →
  `sequencing_run_start` → `sequencing_qc_complete`, plus context columns
  (`programme`, `faculty_sponsor`, `data_access_group`, `library_type`,
  `project_name`, `platform`, …). This is the source the requester's own tracking
  tool [`wtsi-hgi/gst`](https://github.com/wtsi-hgi/gst) (`db/query.sql`) reads;
  it computes phase durations directly, e.g. `LibraryTime =
  DATEDIFF(library_complete, library_start)`, `SequencingTime =
  DATEDIFF(sequencing_qc_complete, sequencing_run_start)`. Verified for study 7607,
  e.g. sample 7607STDY16897354: manifest 2026-05-29 → labware 2026-06-02 → order
  2026-06-19 → library_start/complete 2026-06-19 → sequencing_run_start 2026-06-25 →
  `sequencing_qc_complete` NULL (**currently in the sequencing phase**).
- **The tracking table is a rolling ~2-year window — so it cannot be the only
  source.** Its global earliest `manifest_created` is **2024-06-28**, exactly two
  years before "today" (verified), and ~55% of rows (805k/1.46M) even have a NULL
  `manifest_created`. Concretely, long-running study 7607 has only **11** rows here
  (its newest 2026 cohort) versus **428** samples — the older cohorts have aged out.
  This boundary **moves**, so a sample reportable today silently ages out tomorrow:
  a "works for this sample, not that one, and it changed" experience. The feature
  must therefore **never** dead-end on "not tracked" (see deliverable P0). It also
  has **no `last_changed`/`updated` column** and rows mutate in place as milestones
  fill, so it cannot sync on the usual `last_changed` watermark — full-table refresh
  (≈1.46M rows, modest) or a `GREATEST(milestone columns)` pseudo-watermark; settle
  in Design decisions. It is **not mirrored today**.
- **Product-metrics + iRODS give every sample a baseline.** Independent of the
  tracking window, a sample's *sequenced? / QC / delivered-when* is derivable from
  its platform's product-metrics + the iRODS mirror. For **Illumina** this is already
  mirrored at no extra cost (verified: study 7607 has **469** sequenced in
  `iseq_product_metrics` and **all 428** delivered in iRODS — covering exactly the
  samples the tracking window drops); for the **other platforms** it comes from the
  per-platform tables mirrored for Platform coverage (§). So a coarse but
  **always-available** status (registered → sequenced[+QC] → delivered[+date]) is
  derivable for *any* sample on *any* platform, with the wet-lab→sequencing milestone
  timeline layered on when the sample is in the tracking window.
- **The within-sequencing run-status history (now first pass — verified).**
  `iseq_run_status` (`id_run_status` PK, `id_run`, `date` datetime,
  `id_run_status_dict`, `iscurrent`) records every NPG lifecycle transition for a
  run; `iseq_run_status_dict` (`id_run_status_dict`, `description`, `temporal_index`)
  is the ~29-row phase vocabulary (run pending → in progress → complete → mirrored →
  analysis → secondary analysis → analysis complete → archival → run archived →
  qc review pending → qc in progress → qc complete, plus on hold / cancelled /
  quarantined / stopped early). A sample reaches it via the **already-cached**
  `iseq_product_metrics_mirror` (`id_sample_tmp` → `id_run`). Verified end-to-end:
  run 52553 (study 7607) → pending (6h) → in progress (34h) → complete → mirrored →
  analysis → secondary analysis (6h) → analysis complete → **qc review pending
  (current)**. **Sync nuance:** it has **no `last_changed`** and `iscurrent` flips
  1→0 in place, so sync by the `id_run_status` PK ascending-id mode (cf.
  `seqProductIRODSLocationsIDMode`, `mlwh/sync.go:55`) and **derive "current" from
  the latest `date` per `id_run`**; mirror the tiny dict wholesale. Not mirrored
  today.
- **Handler wiring & invariants.** `mlwh/server.go` — one `case` per
  `Registry.Method` (~373–396); `RegisterRoutes` (~79–96). Every query bakes in
  `id_lims = 'SQSCP'`; keep it.
- **The add-a-query recipe** (`mlwh/registry.go` package docstring, ~26–30):
  (1) schema columns/indices in **both** dialects; (2) one `Client` method;
  (3) one `Queryer` member (`mlwh/queryer.go` ~31); (4) one `Registry` entry; plus
  a `server.go` handler case.
- **Generated docs.** After changing the `Registry`, run
  `WA_REFRESH_DOCS=1 go test ./mlwh -run TestWriteEndpointReference` (writes
  `.docs/mcp/api-reference.md`); drift guards
  (`TestEndpointReferenceAndOpenAPICoverSamePathsG1`) fail CI otherwise. Update
  `.docs/mcp/glossary.md` for new terms ("sequencing data available", "added to
  iRODS").
- **Hermetic tests.** GoConvey over an ephemeral SQLite cache
  (`openSQLiteSyncTestCache`), seeded via helpers in `mlwh/count_test.go` /
  `mlwh/hierarchy_test.go` (`seedHierarchyStudy`, `seedSampleMirrorSearchRow`,
  `seedLibrarySample`, `seedSyncStateRun`, the iRODS/product-metrics seeders).
  Never a live warehouse. Existing count tests cross-check the count against the
  length of the matching list — do the same.

## What the feature must deliver

### Availability

All availability/recency deliverables run on the **platform-spanning** iRODS linkage
(Platform-coverage §): the cache's `seq_product_irods_locations` sample/study link is
a union over every platform's `*_product_metrics`, carrying `platform`. Counts/lists
therefore span all platforms that deliver to iRODS, each row/aggregate platform-aware;
a sample whose platform has no iRODS representation (ONT) is reported "not tracked for
&lt;platform&gt;", never a false zero.

- **(S) A study sequencing-availability summary** — one GET, small fixed-size
  response, e.g. `GET /study/:id/sequencing-summary →
  { samples_total, samples_with_data, samples_without_data, data_objects, runs,
    newest_data_added, added_last_7_days, cache_synced_at }`. It directly answers
  "how many have data / how many don't / how much / anything new", and carries the
  freshness caveat (see F). The exact field set is settled in Design decisions, but
  it includes at least the sample-with/without-data counts, a "how much" figure,
  and the recency fields.
- **(C) A bare count** of samples-with-data, e.g.
  `GET /study/:id/samples-with-data/count → Count`, built on `queryCount` over
  `library_samples → sample_mirror → seq_product_irods_locations_mirror` scoped by
  `id_study_lims`.
- **(E) Enumerate which samples have / lack data.** Provide **both**:
  - list endpoints `GET /study/:id/samples-with-data` and
    `.../samples-without-data` returning `Sample`s, paginated like the other study
    fan-outs; and
  - **sample identity on the per-study iRODS rows** — add the sample's
    `id_sample_tmp` and Sanger `name` to the `IRODSPath` rows returned by
    `/study/:id/irods` (additive fields), so that list is aggregatable by sample
    standalone.

### Recency ("new data this week")

- **(R) Mirror the iRODS-location creation timestamp.** Carry the verified source
  column **`seq_product_irods_locations.created`** into the mirror: add a
  creation-time column to `seq_product_irods_locations_mirror` in **both** dialects
  (sqlite + mysql) plus a supporting index `(id_study_lims, <created column>)`; add
  `spi.created` to **all** the source SELECT variants in `mlwh/sync.go` ~560–592;
  and extend the sync row struct (~2542) and batch insert (~2586) to scan/store it.
  Keep the incremental window keyed on `last_changed` (no high-water change). Do this
  in the **same** iRODS-sync rework that makes the sample/study linkage span all
  platforms (Platform-coverage §) — one change to the iRODS mirror, adding both
  `created` and `platform`. It is what makes "added to iRODS since X" answerable
  precisely rather than via the `last_updated` proxy. Note re-syncing to backfill.
- **(T) Date-windowed availability**, filtering on the creation timestamp from (R):
  - a count, e.g. `GET /study/:id/samples-with-data/count?since=<RFC3339>` (and/or
    a dedicated "new since" count), returning distinct samples whose data was added
    to iRODS in the window; and
  - a list of the new data / newly-covered samples in the window.
  The window is expressed as explicit `since` (and optional `until`) RFC3339
  parameters — the API stays date-explicit; callers translate "this week" into a
  date. Both are single indexed range queries over the new column/index.

### Overviews that displace the giant aggregates

- **(O1) A cheap study overview** — small fixed-size superset of (S) answering
  "what's in study X?": sample / library / run counts, samples-with-data &
  data-object counts, the library types present, and the sequencing date range —
  all cheap aggregates over indexed columns. (May be the same endpoint as (S);
  settle in Design decisions.)
- **(O2) A cheap run overview** — the run-level analogue (how many samples /
  studies / data objects on a run) so "what's on this run / how much" needs
  neither `/run/:id/detail` nor per-sample calls.

### Budget-safety surface completion

- **(N) A `/count` counterpart for every paginated list endpoint** so any list can
  be sized before transfer: `/study/:id/irods/count`, `/sample/:id/irods/count`,
  `/study/:id/runs/count`, `/study/:id/libraries/count`, `/sample/:id/lanes/count`,
  `/run/:id/samples/count`, and the `library*/samples` + `find/sample/*` lists.
  Each is the same `queryCount` + four-step recipe.
- **(M) Sizing metadata on list responses** — return the total matching count and
  the next offset alongside each page (an envelope such as
  `{items, total, next_offset}`, or response headers; settle the exact shape),
  so one page reveals how much remains.
- **(L) Bounded / lean detail aggregates** — give `/study/:id/detail` and
  `/run/:id/detail` pagination of their nested collections, a `fields`/`lean`
  projection that drops heavy nested objects, and **de-duplication** of repeated
  nested entities (return each study/library once in a lookup table instead of
  re-embedding it under every sample). (See `StudyDetail`/`RunDetail` in
  `mlwh/types.go` and their builders in `mlwh/hierarchy.go`.)

### Freshness, woven through

- **(F) Every availability/recency response must let the caller honestly caveat
  recency** by surfacing the relevant table's `last_run` (when `wa` last synced the
  iRODS data) — e.g. a `cache_synced_at` field on the summary/overview and on the
  windowed responses — kept **clearly distinct** from any data-added timestamp.
  Reuse `mlwh/freshness.go`. A recent-window answer is only complete up to
  `last_run`.

### Sample progress / pipeline status ("what's happening with my sample?")

Three layers, so **every** sample on **every** platform gets a coherent answer and
there is no "works for this sample, not that one" cliff: a **baseline** from data
already mirrored (P0); the **milestone timeline** from
`mlwh_reporting.seq_ops_tracking_per_sample` (P1–P2); and the **within-sequencing
status** detail per platform (P5). All layers are platform-aware (Platform-coverage §)
— each capability follows the sample's platform schema, with an explicit "not
tracked for &lt;platform&gt;" where a platform lacks it (e.g. ONT status/QC), never a
false "no progress".

- **(P0) Always-available baseline status — every sample, every platform.** Derive a
  coarse phase that **always resolves**: *registered* (linked, no products) →
  *sequenced* (has products; report QC as pass/fail/pending) → *delivered* (has iRODS
  data; report the earliest `created` from R). Source it per platform via the
  Platform-coverage union — the product-metrics + iRODS mirrors for Illumina/PacBio/
  Elembio/Ultimagen, and `oseq_flowcell` (registered/flowcell-assigned only; iRODS/QC
  "not tracked") for ONT. For Illumina this needs no new tables (verified: study 7607
  has 469 sequenced + all 428 delivered in the cache vs 11 in the tracking table);
  the other platforms use the linkage tables mirrored for Platform coverage.
- **(P1) Mirror the tracking table** for the detailed milestone timeline. Add a
  `seq_ops_tracking_per_sample_mirror` in **both** dialects carrying the milestone
  `datetime` columns (`manifest_created`, `manifest_uploaded`, `labware_received`,
  `order_made`, `working_dilution`, `library_start`, `library_complete`,
  `sequencing_run_start`, `sequencing_qc_complete`) plus lookup/context columns
  (`id_sample_lims`, `sanger_sample_id`, `sanger_sample_name`, `study_id`,
  `programme`, `faculty_sponsor`, `library_type`, `platform`, …), indexed by
  `id_sample_lims`, `sanger_sample_name`, `study_id`. Extend `cache_schema.go`, the
  schema SQL, and `mlwh/sync.go`. No `last_changed` → sync by full refresh (or a
  `GREATEST(milestones)` pseudo-watermark); settle in Design decisions.
- **(P2) Sample progress endpoint** — e.g. `GET /sample/:id/progress` (by Sanger
  name) — **always returns the P0 baseline**; **when the sample is in the tracking
  window** layers on the ordered milestone timeline (each milestone with its
  `reached_at` and the duration to the next; the **current phase** = the span after
  the latest reached milestone whose successor is still NULL — open duration, return
  `reached_at` for the caller to compute elapsed); **and for each of the sample's
  runs** layers on the within-sequencing run-status event timeline from P5. A sample
  outside the tracking window is **not** an error: it returns the baseline (plus any
  run-status detail) and a flag like `detailed_timeline: false` (and why) — phrased
  as *less detail*, never *unavailable*.
- **(P3) Study rollup** — `GET /study/:id/status-breakdown`: counts of **all** the
  study's samples by their P0 baseline phase (registered / sequenced / delivered),
  with the count that additionally have a detailed timeline shown alongside (e.g.
  "428 samples: 428 delivered; 11 with detailed timeline"). One small aggregate; no
  per-sample fan-out; nothing silently dropped.
- **(P4) One continuous journey.** The P0 *delivered* phase is the same "added to
  iRODS" milestone as deliverable R, so baseline and detailed timeline (submission →
  … → qc complete → delivered) compose into one model rather than two vocabularies.
- **(P5) Within-sequencing status detail layer — per platform.** The detailed
  sequencing breakdown comes from each platform's own status tables (Platform-coverage
  §):
  - **Illumina:** mirror `iseq_run_status` + `iseq_run_status_dict` (sync
    `iseq_run_status` by the `id_run_status` PK ascending-id mode — no `last_changed`;
    mirror the tiny dict wholesale), indexed by `id_run` and `(id_run, date)`. Add
    `GET /run/:id/status` returning the ordered event timeline (each: `description`,
    `temporal_index`, `entered_at` = `date`, duration to next; current = latest
    `date`, **derived**, not the source `iscurrent`; recurrences / on-hold /
    cancelled / stopped-early faithful, not forced monotonic).
  - **PacBio / Elembio / Ultimagen:** the equivalent status + dates from their
    own metrics tables (PacBio `pac_bio_run_well_metrics`: `run_start`/`run_complete`/
    `well_complete`/`qc_seq_date` + `run_status`/`well_status`; Elembio
    `eseq_run`/`eseq_run_lane_metrics`; Ultimagen `useq_run_metrics`).
  - **ONT:** no status/run tables → "not tracked for ONT" (still resolves identity +
    milestones).
  P2 composes whichever applies for the sample's platform, as the detailed breakdown
  *within* the sequencing phase, covering sequenced samples even outside the tracking
  window.
- **(P6) Freshness on every progress response** — surface the relevant tables'
  `last_run` (tracking table for the milestone timeline; `iseq_run_status` for the
  run detail; product-metrics / iRODS for the baseline) so "current phase / time so
  far" is explicitly **as-of last sync** (reuse `mlwh/freshness.go`), distinct from
  the event datetimes.

## HARD REQUIREMENTS

1. **One request, small response** for every count/summary/overview question;
   response size independent of study/run size. No client should ever page the full
   iRODS list or call a per-sample endpoint N times to answer availability or
   recency.
2. **Single indexed query per aggregate.** Counts/summaries are SQL
   (`COUNT(DISTINCT ...)`, range scans on the new creation-time index), never an
   in-process scan of a fetched list. Add only the indices the new queries need.
3. **Correct recency signal.** "New / added to iRODS since X" filters on the
   mirrored **creation** timestamp from (R), never on `last_updated`. Never present
   `last_updated` or `last_run` as "when data was added".
4. **Reuse existing infrastructure & invariants.** `queryCount`, the four-step
   recipe, one-`case`-per-`Method` handlers, `id_lims = 'SQSCP'` in every query,
   and the never-synced / empty / unknown-study behaviour consistent with
   `CountSamplesForStudy`.
5. **Self-describing metadata.** Each new endpoint gets a clear `Summary` and
   `Description` (the downstream MCP surfaces `Description` verbatim as the agent's
   tool help): state the precise definition of "available", the recency semantics
   and window parameters, the study-scoping rule, and the freshness caveat.
6. **Regenerate generated docs; keep drift guards green.** Add `Registry` entries,
   refresh `.docs/mcp/api-reference.md`, update the glossary; OpenAPI must cover the
   new paths.
7. **Hermetic GoConvey tests.** Seed a study with samples — some with iRODS rows
   and some without, across ≥2 runs/tags, with iRODS rows of **differing creation
   times** (inside and outside the window), and at least one sample shared with
   another study (to exercise scoping). Assert the counts / summary / overview /
   windowed results, cross-check count against list length, and cover
   never-synced / empty-study and the freshness fields. Test both schema dialects'
   new column/index. For progress, seed a study where some samples are in
   `seq_ops_tracking_per_sample_mirror` (milestones filled to different points: one
   in library prep, one sequencing, one qc-complete) and some are **absent** but have
   product-metrics/iRODS rows; assert that absent samples still return the P0
   baseline (sequenced/QC/delivered), that tracked samples additionally return the
   ordered timeline + per-phase durations + current phase, and that the study rollup
   counts **all** samples by baseline phase plus the detailed-timeline subset. For
   the run-status layer, seed `iseq_run_status` rows across several phases (incl. a
   recurrence and a current phase) and assert the ordered run timeline, durations,
   and current-phase derivation (from latest `date`, not source `iscurrent`). For
   platform coverage, seed at least one **PacBio** sample (iRODS via
   `pac_bio_product_metrics`) and one **ONT** sample (`oseq_flowcell`, no iRODS) and
   assert: PacBio availability/QC/status resolve and carry `platform`; ONT resolves
   identity + study but returns "not tracked for ONT" on iRODS/QC/status — never a
   false zero.
8. **Every sample resolves; tiers are detail-level, not pass/fail.** P2 always
   returns the P0 baseline; the detailed milestone timeline and the run-status detail
   are layered on **when available** and their absence is reported as *less detail*
   (`detailed_timeline: false` + reason), never as an error or "no progress". Current
   milestone phase = the latest reached milestone whose successor is NULL; durations
   = consecutive deltas. The study rollup counts all samples, never silently dropping
   any.
9. **Run-status current is derived; sequence is faithful.** For the P5 run-status
   layer, compute "current" from the latest `date` per run (never the source
   `iscurrent`, which mutates in place with no sync trigger); present recurrences,
   on-hold, cancelled, and stopped-early faithfully, not forced into monotonic
   progress.
10. **Progress responses stay small and are aggregates where they must be.** A
   per-sample timeline is a fixed handful of milestones plus a bounded run-status
   sequence; the study rollup (P3) must be a single grouped query, never N
   per-sample lookups.
11. **Uniform per-platform; never a false "no data".** All platforms go through one
   generic mechanism (Platform-coverage §): the iRODS sample/study linkage is a
   **union across every platform's `*_product_metrics`** table, carrying `platform`;
   QC and status/dates come from each platform's own tables; capability is present
   wherever the schema provides it. No platform is special-cased or deferred — ONT
   simply has no product-metrics, so the same code paths yield "not tracked for ONT"
   on iRODS/QC/status while still resolving identity/study/milestones. Every response
   carries `platform`; every negative is platform-qualified; any missing capability
   yields a clear "not supported/tracked for &lt;platform&gt;", derived from the
   sample's detected platform(s) — never a bare "no data" or silence.

## Design decisions for the spec to settle (HOW, not WHETHER)

Each item below **will be built**; settle only the implementation:

- **Definition of "sequencing data available".** Use: ≥1 row in
  `seq_product_irods_locations_mirror` for the study (real data files in iRODS).
  Decide whether the summary *also* reports "sequenced but not yet in iRODS"
  (samples with `iseq_product_metrics_mirror` rows but no iRODS rows) as a separate
  figure. State the choice in every `Description`.
- **Study scoping of shared samples.** Scope "data for *this* study" by
  `seq_product_irods_locations_mirror.id_study_lims = :id` (as `/study/:id/irods`
  already does), not "data the sample has anywhere". This is the source of a real
  discrepancy seen in the incident (735 study-scoped objects vs 647 summed across
  un-scoped per-sample lists). Pick this rule and state it.
- **The mirror column for (R)** — the source column is `created` (settled); choose
  the mirror column name (e.g. `created` vs `irods_created`), its stored format
  (TEXT RFC3339, as `last_updated` is stored), and the exact index shape
  `(id_study_lims, <created column>)`.
- **Window semantics & parameters** — `since`/`until` (RFC3339), half-open vs
  closed intervals; and the precise meaning of "added" given that a creation
  timestamp records first registration.
- **Endpoint shapes & names** — one combined `sequencing-summary`/`overview`
  endpoint vs separate; the `samples-with-data[/count]`, run-overview, and
  `/count` counterpart paths; the summary/overview response structs. Keep
  consistent with the existing `/study/:id/...`, `Count`, and `*Detail`
  conventions.
- **Sizing-metadata shape (M)** — envelope vs headers, and whether it is always on
  or opt-in, reconciled with the current bare-slice contract.
- **Lean/de-dup detail shape (L)** — projection mechanism and the lookup-table
  layout for de-duplicated nested entities.
- **Progress endpoint shape & sync strategy** — the endpoint name
  (`/sample/:id/progress` vs `/status`); the tracking-table sync strategy
  (full-table refresh vs a `GREATEST(milestone columns)` pseudo-watermark, given no
  `last_changed`); how the open/current phase's elapsed time is represented (return
  `reached_at` for the caller to compute now − reached_at, vs compute against
  `last_run`); the unified shape carrying the P0 baseline, the optional milestone
  timeline, and the per-run run-status detail (and how `detailed_timeline: false` +
  reason is expressed); the canonical phase names exposed (mapping milestone columns
  + run-status phases, and how the P0 registered/sequenced/delivered ladder aligns).
- **Run-status layer (P5) shaping** — how the per-run `iseq_run_status` timeline is
  nested under P2 vs only on `GET /run/:id/status`; the `iseq_run_status` index
  shape; and how recurrences / on-hold / cancelled are represented.
- **Canonical phase-entry field name** — the milestone timeline currently uses
  `reached_at` and the run-status timeline `entered_at` for the same notion (when a
  phase/milestone began). Settle one canonical name across both layers, or keep the
  two deliberately distinct (milestone *reached* vs status *entered*) and say so;
  don't leave it accidental.

## Out of scope

- **Mirroring beyond what these deliverables require.** New mirrored source data is
  limited to: the iRODS-location `created` **column** (R); the
  `mlwh_reporting.seq_ops_tracking_per_sample` **table** (P1); the per-platform
  linkage/QC/status tables needed for uniform platform coverage (Platform-coverage §)
  — `iseq_run_status` + `iseq_run_status_dict`, `pac_bio_product_metrics` +
  `pac_bio_run` + `pac_bio_run_well_metrics`, `eseq_product_metrics` +
  `eseq_flowcell` + `eseq_run`/`eseq_run_lane_metrics`, `useq_product_metrics` +
  `useq_run_metrics`, and `oseq_flowcell`. Everything else reuses already-mirrored
  data; do not mirror tables/columns no deliverable here needs.
- Authentication / TLS changes (keep the current posture); mutating endpoints.
- Fuzzy relative-time parsing in the API ("this week"): the API takes explicit
  dates; callers compute the window.
- The downstream MCP server's tool surface (a separate, dependent spec).
- **No platform is deferred.** All sequencing platforms are handled by the one
  uniform mechanism (Platform-coverage §); capability simply follows each platform's
  schema, with an explicit "not supported/tracked for &lt;platform&gt;" where a table
  is absent (e.g. ONT iRODS/QC/status). The only things truly out of scope are
  *cosmetic* per-platform enrichment beyond what the questions need — e.g.
  instrument-model / pipeline labels that gst's `COALESCE` adds — and gst's
  HGI-sponsor / 2-year filters, which are gst-specific and not wanted here. (These
  are excluded, not "optional deliverables" — consistent with the scope rule.)

## Pointers / prior art (in order of authority)

1. **This repo's code**: `mlwh/count.go` (`CountSamplesForStudy`, `queryCount`);
   `mlwh/hierarchy.go` (`IRODSPathsForStudy`, `LanesForSample`, `queryIRODSPaths`,
   the detail builders); `mlwh/freshness.go` (the `last_run`/`high_water` caveat
   source); `mlwh/cache_schema/{sqlite,mysql}/seq_product_irods_locations_mirror.sql`
   + `iseq_product_metrics_mirror.sql` + `sync_state.sql` (the linkage, the
   `last_updated` signal, and where the new creation column/index go);
   `mlwh/sync.go` (~560–592, the `seq_product_irods_locations` source SELECTs to
   extend with `spi.created`; row struct ~2542, batch insert ~2586); the source
   table `mlwh_reporting.seq_ops_tracking_per_sample` (the milestone columns for the
   detailed timeline; full-refresh sync, no `last_changed`); the already-mirrored
   `iseq_product_metrics_mirror` (sequenced + `qc` flags) and
   `seq_product_irods_locations_mirror` (delivered + `created`) that power the P0
   always-available baseline; the source tables `iseq_run_status` /
   `iseq_run_status_dict` (the P5 run-status layer; sync via the
   `seqProductIRODSLocationsIDMode` ascending-id precedent at `mlwh/sync.go:55`);
   the per-platform linkage tables for uniform coverage (PacBio `pac_bio_product_metrics`
   / `pac_bio_run` / `pac_bio_run_well_metrics`; Elembio `eseq_product_metrics` /
   `eseq_flowcell` / `eseq_run` / `eseq_run_lane_metrics`; Ultimagen
   `useq_product_metrics` / `useq_wafer` (sample link) / `useq_run_metrics`; ONT
   `oseq_flowcell`), each
   `*_product_metrics` carrying an `id_*_product` matching
   `seq_product_irods_locations.id_product`; `mlwh/cache_schema.go` (the mirrored-table
   list to extend with the tracking-table, run-status, and per-platform mirrors);
   `mlwh/registry.go` (entry pattern + recipe); `mlwh/queryer.go`; `mlwh/server.go`;
   `mlwh/types.go` (`Count`, `IRODSPath`, `Sample`, `Lane`, `StudyDetail`,
   `RunDetail`).
2. **Generated docs + tests**: `.docs/mcp/api-reference.md`, `.docs/mcp/glossary.md`,
   the `WA_REFRESH_DOCS=1 go test ./mlwh -run TestWriteEndpointReference` flow, and
   the GoConvey hermetic-cache helpers in `mlwh/count_test.go` /
   `mlwh/hierarchy_test.go` / `mlwh/freshness_test.go`.
3. **The downstream consumer** (why descriptions matter): the MLWH MCP server turns
   each `Registry` entry into an agent tool and shows the `Description` as its help.
4. **Prior art for the progress feature**: [`wtsi-hgi/gst`](https://github.com/wtsi-hgi/gst)
   `db/query.sql` + `db/model.go` — reads `seq_ops_tracking_per_sample`, computes
   `LibraryTime`/`SequencingTime` from milestone deltas, and shows the per-platform
   join pattern (its `COALESCE` over `iseq_*` / `pac_bio_*` / `oseq_*`) that this
   feature generalises into the uniform linkage. Take the milestone model and the
   per-platform joins from it; ignore only its *cosmetic* enrichment labels (RunID /
   instrument / pipeline) and its HGI faculty-sponsor / 2-year filters (gst-specific,
   not wanted here).
