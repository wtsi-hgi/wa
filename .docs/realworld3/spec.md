# Fast Generic `wa mlwh export`, iRODS Recency, Correct Search + Shared Filters, Global Run Aggregation, Programme + Study->Users, and Merged-CRAM Attribution Specification

## Overview

Third wave of real-user MLWH questions that today fail, are slow, or wrong. It
adds a fast, column-selectable, multi-format `wa mlwh export` over any
entity->children relationship (replacing `wa mlwh irods`); surfaces iRODS
`created` for recency; makes literal-prefix the default sample search with an
opt-in `--words` mode and a shared exact filter family (organism / library-type
/ qc / deliverable); adds global run aggregation (runs per month by manufacturer
and platform) with a per-platform `date_basis`; makes `programme` a first-class
indexed filter/group-by and adds the study->users inverse; and makes merged
multi-lane / composite CRAMs first-class rather than silently dropped.

Two cross-cutting rules: (1) every answer is a first-class, correctly-scoped,
indexed endpoint whose semantics are baked in server-side and stated verbatim in
the registry `Description` (the MCP layer surfaces it) - never delegated to
caller-authored SQL; (2) correct the mirror where this wave found it wrong or
slow (a mis-typed join key, un-surfaced timestamp, big-study slowness, identity
derived through a join that misses composite products, `programme` inert).

Authority: this repo's Go code is authority for existing behaviour; `prompt.md`
is authority for what to build. This spec corrects two stale prompt claims
against the code (see "Code-authority corrections"). Bump `APIVersion` 1.7.0 ->
1.8.0 and `CacheSchemaVersion` 12 -> 13 together (full resync acceptable).

## Architecture

### Packages and files

- **`mlwh/`** - all query logic, types, schema, sync, registry, server, remote.
    - New: `mlwh/export.go` (+`_test.go`) - the generic export projection layer
      (relationship registry, column vocabularies, TSV/CSV/JSON rendering, keyset
      paging over the typed backing methods).
    - New: `mlwh/runs_agg.go` (+`_test.go`) - D5/D7 monthly grouped counts, global
      run listing, grouped sequencing aggregate.
    - Changed: `hierarchy.go` (iRODS list SQL: `created`, `merged`, per-product
      `qc`/deliverable, recency order/window, run-scoped composite visibility,
      export columns); `availability.go` (latest-data endpoints, overview
      `programme`, overview/status-breakdown perf); `progress.go` +
      `types.go`/`registry.go` (StatusBreakdown `per_platform` `[]`); `manifest.go`
      (`products_without_irods` counter, per-row `irods_unmatched`); `search.go`
      (literal-prefix default, `--words` opt-in, organism + shared filter family);
      `people.go` (study->users inverse, programme routes); `count.go` (new
      `/count` siblings); `sync.go`/`sync_platform_coverage.go` (new source
      selection, composite product rows, denormalised export columns, ONT run
      identity, run-date normalisation, organism vocabulary); `cache.go`
      (`CacheSchemaVersion=13`, sparse cold-load read-index set); `openapi.go`
      (`APIVersion="1.8.0"`); `registry.go`, `server.go`, `remote.go`, `docs.go`.
    - Schema: `mlwh/cache_schema/{sqlite,mysql}/*.sql` (new
      `iseq_flowcell_mirror.sql`, `common_name_word_mirror.sql`; changed
      `iseq_product_metrics_mirror.sql`, `seq_product_irods_locations_mirror.sql`,
      `oseq_flowcell_mirror.sql`, `iseq_run_status_mirror.sql`, `study_mirror.sql`),
      kept in dialect parity.
- **`cmd/`** - CLI. New: `cmd/mlwh_export.go` (+`_test.go`) replacing
  `cmd/mlwh_irods.go`; `cmd/mlwh_runs.go` (+`_test.go`); `cmd/mlwh_latest.go`
  (+`_test.go`). Changed: `cmd/mlwh.go` (wiring: drop `irods`, add `export`,
  `runs`, `latest`, `programmes`), `cmd/mlwh_studies.go` (`--programme`,
  `programmes`), `cmd/mlwh_manifest.go` (surface `products_without_irods`).

### Versions and migration (HARD REQ 7, 10)

- `APIVersion` 1.7.0 -> **1.8.0**; `CacheSchemaVersion` 12 -> **13**, bumped
  together per `openapi.go`'s documented lineage.
- Migration is the existing recreate-tables path (full resync acceptable). It
  creates the new mirrors/columns/indexes below. Do NOT take an additive
  no-version-bump path for the `id_iseq_product` key-type change.
- `study_users_mirror` already exists and is reused as-is (D7 adds only an
  endpoint, not a table).

### Code-authority corrections (verified against the Go code; the spec matches reality)

Two `prompt.md` claims are STALE; the code is authority. State these in
background and scope the work to reality:

1. **`iseq_product_metrics_mirror` already HAS a PRIMARY KEY.** Both
   `cache_schema/mysql/iseq_product_metrics_mirror.sql` and the SQLite dialect
   declare `id_iseq_product ... NOT NULL PRIMARY KEY`. The prompt's "no PRIMARY
   KEY (only a secondary KEY on id_iseq_product)" is wrong. The genuinely-needed
   change (A2) is ONLY the column TYPE: MySQL `VARCHAR(255)` -> `CHAR(64)` to
   match source `char(64)` and make the iRODS join a fixed-width key lookup
   (SQLite keeps `TEXT`). There is additionally a redundant secondary index
   `ipm_mirror_iseq_product_idx` on the same single column as the PK; drop it
   (it duplicates the PK). Do NOT "add a PRIMARY KEY".
2. **The study-scoped iRODS listing already attributes sample identity for
   merged CRAMs.** `hierarchy.go` `irodsPathsForStudyCacheSQLPrefix` (scanned by
   `queryIRODSPathsWithSample`) selects `spi.id_sample_tmp` and
   `COALESCE(sample_mirror.name, '')` via `LEFT JOIN sample_mirror ON
sample_mirror.id_sample_tmp = spi.id_sample_tmp` - i.e. from the iRODS
   mirror's own denormalised `id_sample_tmp`, NOT the product-metrics join. So a
   merged/composite CRAM in a STUDY listing ALREADY has the correct
   `id_sample_tmp` and a non-empty `name`. The ONLY mis-sourced field is
   `id_run` = `COALESCE(MIN(ipm.id_run), 0)`, which is `0` for a composite
   because the single-lane `id_iseq_product -> iseq_product_metrics_mirror` LEFT
   JOIN misses. The sample-scoped listing (`irodsPathsForSampleCacheSQLPrefix`)
   filters by `spi.id_sample_tmp`, so it also FINDS the merged CRAM (with
   `id_run` 0) but emits no sample-name column. The run-scoped listing
   (`irodsPathsForRunCacheSQLPrefix`) uses an INNER JOIN to
   `iseq_product_metrics_mirror` on `id_iseq_product` filtered by `ipm.id_run`,
   so it CANNOT see a composite object at all and emits no sample identity.
   Therefore D8's genuine fixes are: represent `id_run` honestly for a composite
   (a `merged` flag / `0`, not a misleading single run); make composites visible
   and attributed in the RUN-scoped listing; carry each object's own
   `manual_qc`/deliverable (incl. composites); the per-sample `sample-crams`
   export; and the manifest gap counter. Do NOT prescribe "fix study-scoped
   id_sample_tmp/name attribution" - it already works.

### Source-schema facts (verified against live `mlwarehouse` + mirror; FIRM - cite, do not contradict)

- **Deliverable discriminator is `iseq_flowcell.entity_type`, NOT `is_spiked`.**
  Deliverables = `entity_type IN ('library','library_indexed')`; controls/spikes
  = `library_indexed_spike` / `library_control`. `is_spiked` is `1` for ~79% of
  rows (spiked lanes, not spike products) and is WRONG. Element/Ultima use
  `{eseq,useq}_product_metrics.is_sequencing_control` (`=0` for deliverables).
  There is NO scalar `target` column; `iseq_product_metrics.target_*` are
  bait/capture coverage metrics, not a target flag. The mirror does not yet
  mirror `iseq_flowcell` (A1 adds it). PacBio/ONT have no deliverable
  discriminator -> `--deliverables-only` is pass-through for them.
- **`manual_qc` = `iseq_product_metrics.qc`** (`tinyint`: `1`=pass, `0`=fail,
  `NULL`=pending); already mirrored as `qc`; rolled up by `qc.go` (fail >
  pending > pass). Per-platform `qc` on `pac_bio_/eseq_/useq_product_metrics`;
  ONT has no product/qc.
- **`seq_product_irods_locations`** has `created` + `last_changed` (`datetime`),
  denormalised `id_sample_tmp` + `id_study_lims` on every row (incl. composite),
  and NO `id_run`/sample-name/file-type column. `id_product varchar(64)` joins
  `iseq_product_metrics.id_iseq_product char(64)`. Unique key
  `(irods_root_collection, id_product)`, so `id_product` is not row-unique.
- **Merged multi-lane / composite CRAM (Q7).** A sample sequenced across lanes
  has ONE composite iRODS object with its own composite `id_iseq_product` hash
  (e.g. study 7568 `/seq/illumina/runs/49/49348/lane1-2/plex1/49348_1-2#1.cram`),
  matching no single-lane `iseq_product_metrics_mirror` row. Study 7568: 780
  manifest products, 732 cram objects, 732 samples; 48 samples on multi-lane run
  49348 whose CRAM is the merged `lane1-2` object.
- **Run `date_basis` (authoritative, per `mlwh://reference/sequencing-timestamps`,
  validated against source):** Illumina & Element = `iseq_run_status.date` at
  status `run complete`; Ultima = `iseq_run_status.date` at status `run archived`
  (Ultima never "completes"); PacBio = `pac_bio_run_well_metrics.run_complete`
  (run-level, shared across wells; NOT per-well `well_complete`); ONT = NO true
  sequencing date - `oseq_flowcell.run_id` is NULL for all ~8,730 rows, run
  identity is `experiment_name` (447 distinct), only date is
  `oseq_flowcell.last_updated` (warehouse-load time).
- **Search/filter reality (sample domain = whole `sample_mirror`, 10,352,242
  rows):** literal `col LIKE 'hek!_r%' ESCAPE '!'` over the four indexed text
  fields = the 4 `Hek_R1..4` in ~0.07s (index range seek); naive `%substr%` =
  16-23s; ngram FULLTEXT incorrect; a correct trigram table ~200M+ rows -> NO
  substring/ngram/FULLTEXT index is built. `common_name` = 16,234 distinct
  (organism -> whole-word filter, not substring). `library_samples.pipeline_id_lims`
  = 122 distinct (exact filter). `_`/`%` already correctly escaped.
- **Big-study perf today:** cram manifest at study 7699 (~52k cram rows /
  ~102,763 product x iRODS rows) = 3.04s via the HTTP endpoint (raw mirror join
  15.8s vs source-direct 1.7s); `/overview` and `/status-breakdown` ~3s. The
  denormalised iRODS aggregate (COUNT + MIN/MAX(created) on `(id_study_lims,
created)`) is already 0.085s - the slowness is the varchar-collated
  `id_iseq_product` join and the sample-membership/per-platform/qc arms.

### New / changed result types (additive to `mlwh/types.go`)

```go
// IRODSPath gains recency, product coordinates, QC, deliverable, merged flag,
// and the sample/study identity columns the export projects. Existing fields
// unchanged. Created is RFC3339 UTC (the iRODS "data added" time, never
// last_changed), empty when the mirror created column is NULL. Merged is true
// for a composite multi-lane object (whose IDRun is then 0, not a misleading
// single run). ManualQC is the qc.go roll-up string (pass|fail|pending) from the
// row's per-platform denormalised qc (A4), empty only when the object has no
// product-metrics (ONT). Deliverable is a tri-state (*bool): true/false from the
// platform discriminator, nil for PacBio/ONT (no discriminator; pass-through).
type IRODSPath struct {
    // ... existing: IDProduct, Collection, DataObject, IRODSPath, IDSampleTmp,
    //     Name, IDRun, Platform ...
    Created              string `json:"created" doc:"iRODS created time (data added), UTC RFC3339; empty if unknown"`
    Position             int    `json:"lane" doc:"lane position of the product; 0 for a merged composite"`
    TagIndex             int    `json:"tag_index" doc:"multiplexing tag index; 0 for a merged composite"`
    ManualQC             string `json:"manual_qc" doc:"per-product QC roll-up pass|fail|pending; empty when no product-metrics (e.g. ONT)"`
    Deliverable          *bool  `json:"deliverable" doc:"true=deliverable, false=control/spike, null=no source discriminator (PacBio/ONT pass-through)"`
    Merged               bool   `json:"merged" doc:"true for a merged multi-lane composite object (IDRun/lane/tag then 0)"`
    SupplierName         string `json:"supplier_name" doc:"supplier-given sample name"`
    SangerSampleID       string `json:"sanger_sample_id" doc:"Sanger sample id"`
    AccessionNumber      string `json:"accession_number" doc:"sample public archive accession number"`
    StudyAccessionNumber string `json:"study_accession_number" doc:"study public archive accession number"`
    IDStudyLims          string `json:"id_study_lims" doc:"LIMS study id of the data object"`
}

// RecentDataRow is one row of a "latest data" listing (D2), ordered created DESC.
type RecentDataRow struct {
    Created      string `json:"created" doc:"iRODS created time, UTC RFC3339"`
    IRODSPath    string `json:"irods_path" doc:"full iRODS path"`
    IDStudyLims  string `json:"id_study_lims" doc:"LIMS study id"`
    StudyName    string `json:"study_name" doc:"study name"`
    Name         string `json:"name" doc:"Sanger sample name"`
    SupplierName string `json:"supplier_name" doc:"supplier-given sample name"`
    IDRun        int    `json:"id_run" doc:"Illumina NPG run id; 0 for merged/non-Illumina"`
    Position     int    `json:"lane" doc:"lane position; 0 for merged"`
    TagIndex     int    `json:"tag_index" doc:"tag index; 0 for merged"`
    Platform     string `json:"platform" doc:"platform string"`
    Merged       bool   `json:"merged" doc:"true for a merged composite object"`
}

// MonthlyRunCount is one grouped monthly run count (D5). Manufacturer is derived
// from platform. DateBasis states which per-platform completion date the month
// bucket used (incl. the ONT warehouse-load caveat).
type MonthlyRunCount struct {
    Month        string `json:"month" doc:"YYYY-MM bucket"`
    Manufacturer string `json:"manufacturer" doc:"manufacturer derived from platform"`
    Platform     string `json:"platform" doc:"platform"`
    Count        int    `json:"count" doc:"distinct runs at run grain in the bucket"`
    DateBasis    string `json:"date_basis" doc:"the completion date field/status used for this platform"`
    CacheSyncedAt string `json:"cache_synced_at" doc:"oldest last_run across feeding tables, UTC RFC3339"`
}

// RunListingRow is one global-run-listing row (D5). ID is the stable cross-
// platform composite "<platform>:<native_id>" (e.g. illumina:47409,
// pacbio:<run_name>, ont:<experiment_name>) and is the keyset/drill-down cursor.
type RunListingRow struct {
    ID           string `json:"id" doc:"composite <platform>:<native_id> run identifier (keyset cursor)"`
    Platform     string `json:"platform" doc:"platform"`
    NativeID     string `json:"native_id" doc:"platform-native run id (id_run / pac_bio_run_name / experiment_name)"`
    Manufacturer string `json:"manufacturer" doc:"manufacturer derived from platform"`
    RunDate      string `json:"run_date" doc:"completion date per date_basis, UTC RFC3339; empty if unknown"`
    DateBasis    string `json:"date_basis" doc:"the completion date field/status used"`
    CacheSyncedAt string `json:"cache_synced_at" doc:"oldest last_run across feeding tables, UTC RFC3339"`
}

// SequencingAggregateRow is one row of the generalised grouped aggregate (D7).
// Group carries only the requested group_by keys (month/platform/manufacturer/
// programme/faculty_sponsor). Unit is the counted unit (runs|samples|products).
type SequencingAggregateRow struct {
    Group         map[string]string `json:"group" doc:"the requested group_by key values"`
    Unit          string            `json:"unit" doc:"counted unit: runs|samples|products"`
    Count         int               `json:"count" doc:"count of the unit in this group"`
    DateBasis     string            `json:"date_basis" doc:"date field/basis used (per platform for runs; iRODS created for samples/products)"`
    CacheSyncedAt string            `json:"cache_synced_at" doc:"oldest last_run across feeding tables, UTC RFC3339"`
}

// Programme is one row of the /programmes enumeration (D7).
type Programme struct {
    Name       string `json:"name" doc:"distinct programme value"`
    StudyCount int    `json:"study_count" doc:"distinct SQSCP studies in this programme"`
}

// StudyUser is one study->users inverse row (D7): a role member of a study.
type StudyUser struct {
    Role  string `json:"role" doc:"study_users role"`
    Name  string `json:"name" doc:"person full name"`
    Login string `json:"login" doc:"Sanger username"`
    Email string `json:"email" doc:"email"`
}

// SampleCram is one per-sample merged-aware CRAM row (D8): one CRAM per sample.
type SampleCram struct {
    Name          string `json:"name" doc:"Sanger sample name"`
    EGAID         string `json:"ega_id" doc:"sample accession number (EGA/ENA id)"`
    IRODSCramPath string `json:"irods_cram_path" doc:"the sample's CRAM path (merged-aware), empty only if the sample truly has none"`
    Merged        bool   `json:"merged" doc:"true when this CRAM is a merged multi-lane composite object"`
}
```

Additive changes to existing types:

- `StudyOverview` += `Programme string` (D7).
- `ManifestRow` += `ManualQC string` (`json:"manual_qc"`, the pass|fail|pending
  roll-up per row, B1/D4), `IRODSUnmatched bool`
  (`json:"irods_unmatched,omitempty"`) and `Reason string`
  (`json:"reason,omitempty"`, currently only `merged_multilane`) (D8);
  `StudyManifest` += `ProductsWithoutIRODS int`
  (`json:"products_without_irods"`) (D8).

### Definitions to state in every new/changed Description (MCP contract; HARD REQ 9)

`manual_qc` = `iseq_product_metrics.qc` rolled up by `qc.go` (fail>pending>pass);
`deliverable` = `iseq_flowcell.entity_type IN ('library','library_indexed')`
(Element/Ultima `is_sequencing_control=0`), an approximation of the iRODS
`target=1` AVU, NOT `is_spiked`, pass-through for PacBio/ONT; "cram" = filename
suffix on `irods_file_name`; recency = iRODS `created` ("data added", never
`last_changed`); run `date_basis` per platform (Illumina/Element `run complete`,
Ultima `run archived`, PacBio `run_complete`, ONT `last_updated` labelled
"warehouse load time - not a true sequencing date"); search default =
literal whole-value prefix vs `--words` word-prefix vs the exact filters and the
field each covers (incl. the QC grain difference: per-sample roll-up on search,
per-product on export); `programme` grouping/attribution unit; the study->users
direction and role vocabulary; merged-CRAM attribution; `cache_synced_at` /
`/freshness` freshness caveat. `faculty_sponsor` is a `Study` field, NOT a
`study_users` role.

### Error handling / graceful degradation (HARD REQ 5)

- Reuse the existing cascade: unknown parent -> `ErrNotFound`; never-synced
  cache -> `ErrCacheNeverSynced` (+ empty result shape); synced-but-empty ->
  empty list / 0, exit 0. Invalid file-type (`normaliseFileType`) -> 400 /
  `ErrUnsupportedIdentifier`. `until` without `since` -> the existing
  `errUntilRequiresSince`.
- CLI renders not-found/empty/never-synced/not-tracked cleanly with exit 0, in
  both local-cache and `--server` modes, matching existing `wa mlwh` behaviour.
- Platform-aware: ONT/PacBio never silently drop under `--deliverables-only`
  (pass-through); ONT is included in run buckets under the explicit
  warehouse-load `date_basis` label, never a bare zero or silent drop.

---

## A. Schema, sync, and migration foundation (HARD REQ 7, 10)

### A1: deliverable-source schema (`iseq_flowcell_mirror` + eseq/useq `is_sequencing_control`) + sync (D4)

The deliverable filter's per-platform sources, so controls/spikes can be
excluded on EVERY platform that has a discriminator.

- **Illumina:** new mirror `iseq_flowcell_mirror` (both dialects, in parity):
  columns `id_iseq_flowcell_tmp` (PK, the join key from
  `iseq_product_metrics_mirror.id_iseq_flowcell_tmp`), `entity_type`,
  `pipeline_id_lims` (library_type), `id_sample_tmp`, `id_study_tmp`. Index
  `(entity_type)` and keep the PK for the product join. Sync selects
  `iseq_flowcell` wholesale in `sync.go` (new `syncTableIseqFlowcell` source arm;
  the constant already exists and is referenced by resolver sync-state cascades).
- **Element / Ultima:** add `is_sequencing_control INT` (nullable; source
  `{eseq,useq}_product_metrics.is_sequencing_control`, `0`=deliverable) to
  `eseq_product_metrics_mirror` AND `useq_product_metrics_mirror` (both dialects,
  in parity). These mirrors currently have NO such column and Element/Ultima
  products do NOT join `iseq_flowcell` (Element joins its own
  `id_eseq_flowcell_tmp`, Ultima `id_useq_wafer_tmp`), so the discriminator MUST
  live on their own product mirror. Add it to `sync.go` source selection for both
  arms. No new index needed (it is read alongside `qc` when the product row is
  resolved by its PK during A4 population).
- **PacBio / ONT:** NO deliverable discriminator in source (PacBio has only a
  per-product `qc`; ONT has no products) - handled as pass-through by A4/B2, not
  a schema change.

**Package:** `mlwh/`
**Files:** `cache_schema/{sqlite,mysql}/iseq_flowcell_mirror.sql`,
`cache_schema/{sqlite,mysql}/eseq_product_metrics_mirror.sql`,
`cache_schema/{sqlite,mysql}/useq_product_metrics_mirror.sql`, `sync.go`,
`sync_platform_coverage.go`, `cache_schema.go`
**Test file:** `mlwh/cache_schema_test.go`, `mlwh/sync_test.go`

**Acceptance tests:**

1. Given the schema is created, when both dialects are loaded, then
   `iseq_flowcell_mirror` exists with a PK on `id_iseq_flowcell_tmp` and an index
   on `entity_type`, byte-identical structure across dialects (parity test).
2. Given source `iseq_flowcell` rows with `entity_type` in
   `{library, library_indexed, library_indexed_spike, library_control}`, when
   sync runs, then every row is mirrored with its `entity_type`,
   `pipeline_id_lims`, `id_sample_tmp`, `id_study_tmp`.
3. Given the schema, when both dialects load, then `eseq_product_metrics_mirror`
   AND `useq_product_metrics_mirror` each have an `is_sequencing_control` column
   in parity; and given source Element/Ultima product rows with
   `is_sequencing_control` in `{0, 1}`, when sync runs, then each mirrored row
   carries its `is_sequencing_control` value.

### A2: `iseq_product_metrics_mirror.id_iseq_product` -> `char(64)`; drop redundant index (D1/D6)

CORRECTION (see Code-authority corrections #1): the PRIMARY KEY already exists;
only the column type changes. MySQL: `id_iseq_product CHAR(64) NOT NULL PRIMARY
KEY` (was `VARCHAR(255)`); SQLite keeps `TEXT ... PRIMARY KEY`. Align
`seq_product_irods_locations_mirror.id_iseq_product` to `CHAR(64)` (MySQL) too,
so the join is fixed-width both sides. Drop `ipm_mirror_iseq_product_idx` (it
duplicates the PK). This is a single correctness/perf fix shared by D1/D6/D8.

**Package:** `mlwh/`
**Files:** `cache_schema/{sqlite,mysql}/iseq_product_metrics_mirror.sql`,
`cache_schema/{sqlite,mysql}/seq_product_irods_locations_mirror.sql`
**Test file:** `mlwh/cache_schema_test.go`, `mlwh/cache_mysql_integration_test.go`

**Acceptance tests:**

1. Given the MySQL schema is created, when the `iseq_product_metrics_mirror`
   table is described, then `id_iseq_product` is `char(64)`, is the PRIMARY KEY,
   and no separate `ipm_mirror_iseq_product_idx` index exists.
2. Given both mirrors are populated on MySQL, when `EXPLAIN` runs the
   iRODS<->product join on `id_iseq_product`, then it uses the primary key (no
   full scan, no implicit collation conversion on the join column).

### A3: composite (merged) product rows in `iseq_product_metrics_mirror` (D8)

So a composite object carries its own `id_run`/`qc`/`id_iseq_flowcell_tmp` for
attribution, run-scoped visibility, and manual_qc/deliverable. Extend `sync.go`
source selection to ALSO mirror composite/merged `iseq_product_metrics` rows
(those whose `id_iseq_product` matches a composite iRODS object), not only
single-lane rows. Do NOT mirror `iseq_composition`/component structure - only the
composite product row itself. A composite spanning several lanes of one run
carries that run's `id_run`; one spanning runs carries `id_run` 0 (represented
`merged` downstream).

**Package:** `mlwh/`
**Files:** `sync.go`, `sync_platform_coverage.go`
**Test file:** `mlwh/sync_test.go`, `mlwh/sync_source_integration_test.go`

**Acceptance tests:**

1. Given source has the study-7568 composite product for
   `49348_1-2#1.cram`, when sync runs, then `iseq_product_metrics_mirror`
   contains that composite `id_iseq_product` with its `qc` and
   `id_iseq_flowcell_tmp` populated.
2. Given a composite product whose CRAM is one merged object, when the export
   backing query joins iRODS->product on `id_iseq_product`, then the composite
   object matches its product row (no empty `manual_qc`).

### A4: denormalised export columns on `seq_product_irods_locations_mirror` + covering index (D1/D8)

Make the flagship study->iRODS export a single index-ordered range scan (route
(b) of the prompt's perf options, realised by extending the existing iRODS
mirror rather than a second copy of 7.3M rows). Add columns populated at sync:
`id_run` (0 for a multi-run composite), `position`, `tag_index`, `qc` (raw
1/0/NULL), `is_deliverable` (tinyint, NULLABLE - see tri-state below), `merged`
(tinyint). `created` is already mirrored.

**Per-platform population (every platform's iRODS rows carry `qc` and
`is_deliverable`; HARD REQ 4/5).** The iRODS mirror already carries `platform`
per row; at sync, resolve each iRODS row's product-metrics row by matching its
`id_iseq_product` (= source `id_product`) against the platform-appropriate
product mirror by that mirror's PK, and populate `qc`, `id_run`/`position`/
`tag_index` and `is_deliverable` from it:

- **Illumina (+ merged composite):** join `iseq_product_metrics_mirror`
  (`id_iseq_product`) for `qc`, `id_run`, `position`, `tag_index`; then
  `iseq_flowcell_mirror` (via `id_iseq_flowcell_tmp`) for `entity_type` ->
  `is_deliverable = 1` iff `entity_type IN ('library','library_indexed')` else 0.
- **Element:** join `eseq_product_metrics_mirror` (`id_eseq_product`) for `qc`
  and `id_run`; `is_deliverable = 1` iff `is_sequencing_control = 0` else 0
  (A1); `position`/`tag_index` = 0 (no lane/tag concept).
- **Ultima:** join `useq_product_metrics_mirror` (`id_useq_product`) for `qc`
  and `id_run`; `is_deliverable = 1` iff `is_sequencing_control = 0` else 0 (A1);
  `position`/`tag_index` = 0.
- **PacBio:** join `pac_bio_product_metrics_mirror` (`id_pac_bio_product`) for
  `qc`; NO deliverable discriminator -> `is_deliverable = NULL`; `id_run`/
  `position`/`tag_index` = 0.
- **ONT:** no product/qc -> `qc = NULL`, `is_deliverable = NULL`.

**`is_deliverable` is a tri-state:** `1` = known deliverable, `0` = known
control/spike, `NULL` = no source discriminator (PacBio/ONT). This makes
`--deliverables-only` pass-through mean "not dropped" (B2): it excludes ONLY
`is_deliverable = 0`, retaining `1` AND `NULL`.

**Indexes.** Add covering index `(id_study_lims, id_run, position, tag_index,
id_seq_product_irods_locations_tmp)` (the export's keyset order); keep
`(id_study_lims, created)` (study recency); ADD `(id_sample_tmp, created)` and
`(id_run, created)` for the D2 sample-/run-scoped `created_desc` recency paths
(E1). The export reads this one table scoped by `id_study_lims`, joining
`sample_mirror` by the indexed `id_sample_tmp` only for human-readable identity
columns.

**Package:** `mlwh/`
**Files:** `cache_schema/{sqlite,mysql}/seq_product_irods_locations_mirror.sql`,
`sync.go`
**Test file:** `mlwh/cache_schema_test.go`, `mlwh/sync_test.go`,
`mlwh/cache_mysql_integration_test.go`

**Acceptance tests:**

1. Given the schema, when both dialects load, then the iRODS mirror has
   `id_run, position, tag_index, qc, is_deliverable, merged` columns, the
   covering index, and the `(id_sample_tmp, created)` and `(id_run, created)`
   indexes, in dialect parity.
2. Given study 7568 synced on MySQL, when `EXPLAIN` runs the export scan scoped
   by `id_study_lims` ordered by the covering index, then it is an index range
   scan (no full scan of the 7.3M-row mirror, no filesort).
3. Given the merged `lane1-2` object row, when read, then its `merged=1`,
   `id_run=0` (spanning lanes), and its `id_sample_tmp` equals the sample's.
4. Given an Element iRODS cram row (platform Elembio) and an Ultima one, when
   read, then each carries a populated `qc` and an `is_deliverable` of `1`/`0`
   from `is_sequencing_control`; given a PacBio cram row, then `qc` is populated
   and `is_deliverable IS NULL`; given an ONT row, then `qc IS NULL` and
   `is_deliverable IS NULL`.

### A5: `common_name_word_mirror` organism vocabulary (D3)

Word-membership organism filter over the low-cardinality (16,234) `common_name`
vocabulary without a big index. New helper (both dialects): rows `(word,
common_name)` - one per distinct word per distinct `common_name`, built at sync
by tokenising the distinct `common_name` values (same tokeniser as
`sample_search_token`, lowercased `[a-z0-9]` runs). Index `(word)`. ~40k rows.

**Package:** `mlwh/`
**Files:** `cache_schema/{sqlite,mysql}/common_name_word_mirror.sql`, `sync.go`
**Test file:** `mlwh/cache_schema_test.go`, `mlwh/sync_test.go`

**Acceptance tests:**

1. Given `common_name` values `{"Mus Musculus", "Mus musculus castaneus", "Homo
sapiens"}`, when the vocabulary is built, then word `musculus` maps to both
   `Mus Musculus` and `Mus musculus castaneus` (not `Homo sapiens`), and no row
   has word `usculus`.

### A6: ONT run identity + normalised run dates + run-aggregation indexes (D5)

- Extend `oseq_flowcell_mirror` with `experiment_name`, `run_id` (NULL for all
  rows - carried but unused as identity), `run_uuid`, `last_updated`. Index
  `(experiment_name)` and `(last_updated)`.
- Mirror run dates are stored as varchar; add a normalised, indexed
  `run_complete_date` (YYYY-MM-DD..) derived at sync so monthly grouping stays
  index-served: `iseq_run_status_mirror` (a normalised `date` already exists as
  varchar - add an indexed normalised column + status filtering support for
  `run complete`/`run archived`), `pac_bio_run_well_metrics_mirror.run_complete`,
  `oseq_flowcell_mirror.last_updated`. Add an indexed `(month)`-friendly derived
  column or a `(normalised_date)` index per source.
- Ensure sync includes Element and Ultima `id_run`s in `iseq_run_status_mirror`
  (else fall back to the platform run mirrors' `run_complete`/`run_archived`).

**Package:** `mlwh/`
**Files:** `cache_schema/{sqlite,mysql}/oseq_flowcell_mirror.sql`,
`iseq_run_status_mirror.sql`, `pac_bio_run_well_metrics_mirror.sql`,
`useq_run_metrics_mirror.sql`, `sync.go`
**Test file:** `mlwh/cache_schema_test.go`, `mlwh/sync_test.go`

**Acceptance tests:**

1. Given ONT source rows for `experiment_name=ONTRUN-11` sharing one
   `last_updated`, when synced, then `oseq_flowcell_mirror` carries
   `experiment_name` and `last_updated`; `run_id` is NULL.
2. Given `EXPLAIN` on the monthly run count grouped by month, then each
   platform's date grouping uses a normalised-date index (no full scan).

### A7: `study_mirror.programme` index (D7)

Add `study_mirror_programme_idx ON study_mirror(programme)` (both dialects) so
the exact filter/group-by is index-served. No new column (programme exists).

**Package:** `mlwh/`
**Files:** `cache_schema/{sqlite,mysql}/study_mirror.sql`
**Test file:** `mlwh/cache_schema_test.go`

**Acceptance tests:**

1. Given the schema, when both dialects load, then `study_mirror` has an index
   on `programme`, in parity.

### A8: version bump, migration, cold-load read-index set

`CacheSchemaVersion=13`, `APIVersion="1.8.0"`. Migration recreates tables (full
resync). Add the large new/changed mirrors' read-critical indexes to the sparse
cold-load read-index set in `cache.go`/`sync.go` (the iRODS covering index, the
`iseq_flowcell` PK, run-date indexes) so cold reads are index-served.

**Package:** `mlwh/`
**Files:** `cache.go`, `openapi.go`, `sync.go`
**Test file:** `mlwh/cache_test.go`, `mlwh/openapi_test.go`

**Acceptance tests:**

1. Given a v12 cache, when opened by v13 code, then it triggers the
   recreate-tables migration and reports `12 -> 13`.
2. Given the OpenAPI doc is generated, then `info.version == "1.8.0"`.

### A9: source integration test for new source columns/tables (HARD REQ 8)

Extend `mlwh/sync_source_integration_test.go` (throwaway/skip-without-creds
pattern) to assert the source schema these features assume stays true:
`iseq_flowcell.entity_type`/`pipeline_id_lims`;
`{eseq,useq}_product_metrics.is_sequencing_control` (now MIRRORED by A1 and
CONSUMED by A4/B2 for the Element/Ultima deliverable flag, not merely asserted);
`{iseq,eseq,useq,pac_bio}_product_metrics.qc`; run dates incl. `iseq_run_status`
`run complete`/`run archived` and `oseq_flowcell.last_updated`; `study_users`
role rows; `study.programme`; and a merged/composite iRODS object's
`id_sample_tmp`/`id_study_lims`.

**Acceptance tests:**

1. Given source creds, when the source integration test runs, then each asserted
   column/table exists with the expected type and the merged-object linkage is
   present; without creds it skips.

---

## B. D4 - manual_qc & deliverable semantics

### B1: `manual_qc` roll-up surface (reuse `qc.go`)

Expose `manual_qc` (pass|fail|pending via `qc.go`) wherever product/iRODS rows
list: the export (IRODSPath.ManualQC / SampleCram context), StudyManifest rows
(additive `manual_qc` per row), optionally the iRODS listings. `manual_qc`
renders from the row's denormalised `qc` (A4), which is populated PER PLATFORM
(Illumina `iseq_product_metrics.qc`; Element/Ultima `{eseq,useq}_product_metrics.qc`;
PacBio `pac_bio_product_metrics.qc`), so a non-Illumina product/iRODS row is
NOT blank. Reuse `qc.go` so it never disagrees with `SampleProgress.qc` /
`StatusBreakdown`. For a composite product, resolve to the composite's own `qc`
(via A3), never blank. ONT has no product/qc, so `manual_qc` is empty
(not_tracked) there - the one legitimate blank.

**Package:** `mlwh/`
**Files:** `qc.go` (reuse), `manifest.go`, `hierarchy.go`
**Test file:** `mlwh/manifest_test.go`, `mlwh/hierarchy_test.go`

**Acceptance tests:**

1. Given a product with `qc=1`, when its export/manifest row renders, then
   `manual_qc == "pass"`; `qc=0` -> `"fail"`; `qc=NULL` -> `"pending"`.
2. Given a merged composite product with `qc=1`, when its export row renders,
   then `manual_qc == "pass"` (not empty).
3. Given an Element and an Ultima product row (each with `qc` set), when their
   export/iRODS rows render, then `manual_qc` is the non-empty rolled-up
   pass/fail/pending (not blank); given an ONT row, `manual_qc` is empty.

### B2: `deliverable` filter via `entity_type`; pass-through PacBio/ONT

Server-side, indexed, resolved from the row's denormalised `is_deliverable`
tri-state (A4): `1` = deliverable (Illumina flowcell `entity_type IN
('library','library_indexed')`; Element/Ultima `is_sequencing_control=0`), `0` =
control/spike, `NULL` = no source discriminator (PacBio/ONT). **`--deliverables-only`
excludes ONLY `is_deliverable = 0`** (`... AND (is_deliverable = 1 OR
is_deliverable IS NULL)`), so it drops Illumina/Element/Ultima controls/spikes
but is PASS-THROUGH for PacBio/ONT - their rows (NULL) are RETAINED, never a
false "no data" (HARD REQ 5). On the sample-scoped search variant the same
tri-state is applied via the product/flowcell join; PacBio/ONT-only samples are
never dropped. Prove index-served (EXPLAIN).

**Package:** `mlwh/`
**Files:** `hierarchy.go`, `search.go`, `count.go`
**Test file:** `mlwh/cache_mysql_integration_test.go`, `mlwh/hierarchy_test.go`

**Acceptance tests:**

1. Given study 7556 with mixed `entity_type`, when the cram export runs with
   `--deliverables-only`, then the count equals the `entity_type`-derived
   deliverable count (expected ~886; assert the actual figure and document any
   delta from 886 and from the iRODS `target=1` AVU), controls/spikes dropped
   where present.
2. Given a study with only PacBio and ONT samples, when `--deliverables-only` is
   set, then none of its samples/rows are dropped (`is_deliverable IS NULL`
   retained) and the count is unchanged vs no filter.
3. Given `EXPLAIN` on the deliverable-filtered scan, then it is index-served (the
   Illumina flowcell join uses the `iseq_flowcell_mirror` PK and `entity_type`
   index; the export path reads the denormalised `is_deliverable`), no full scan.
4. Given an Element (or Ultima) study with both `is_sequencing_control=1` and
   `=0` products, when the cram export runs with `--deliverables-only`, then the
   `is_sequencing_control=1` products are EXCLUDED and the `=0` deliverables
   retained; AND each retained non-Illumina (Element/Ultima and/or PacBio) row's
   `manual_qc` is a non-empty pass/fail/pending value.

---

## C. D3 - search default, `--words`, and the shared exact filter family

The literal-prefix machinery already exists in `search.go`
(`sampleFullPrefixFields`, `sampleFullPrefixPageSQL/CountSQL`); today's default
merges it with the word-token path (so `hek_r` -> 55+). D3 makes literal-prefix
the SOLE default and moves word-prefix to opt-in `--words`. `searchTermMinLength=3`
and `sampleSearchCountCap=10000` are unchanged.

### C1: default sample search = literal whole-value prefix

`SearchSamples(term)` with no mode returns exactly the SQSCP samples where any of
`name, supplier_name, common_name, donor_id` starts with the term
(`col LIKE 'term%' ESCAPE '!'`, index range seek) - the 4 `Hek_R1..4` for
`hek_r` in ~0.07s. This is the ONLY default; the word-token union is removed from
the default path.

**Package:** `mlwh/`
**File:** `search.go`
**Test file:** `mlwh/search_test.go`, `mlwh/search_mysql_test.go`

```go
func (c *Client) SearchSamples(ctx context.Context, term string, opts SampleSearchOptions, limit, offset int) ([]Sample, error)
func (c *Client) CountSampleSearch(ctx context.Context, term string, opts SampleSearchOptions) (int, error)

// SampleSearchOptions carries the mode + shared filter family (all optional).
type SampleSearchOptions struct {
    Words           bool   // opt-in word-prefix mode (C2)
    Organism        string // C3 word-membership over common_name
    LibraryType     string // C4 exact pipeline_id_lims
    QC              string // C4 pass|fail|pending (per-sample roll-up on search)
    DeliverablesOnly bool  // C4 entity_type deliverable (pass-through PacBio/ONT)
}
```

**Acceptance tests:**

1. Given the sample mirror with `Hek_R1..Hek_R4` (supplier_name) plus many
   word-`hek` samples, when `SearchSamples("hek_r", {})` runs, then it returns
   exactly the 4 `Hek_R1..4` (not 55).
2. Given a term shorter than 3 chars with no filter, then the 3-char minimum
   applies (existing behaviour).
3. Given `EXPLAIN` on the default MySQL search, then each field predicate is an
   index range seek (no full table scan).

### C2: `--words` opt-in word-prefix mode

Retain `sample_search_token`. `--words` selects separator-agnostic multi-word
matching (query `10X Automation HEK` matches stored `10X_Automation_HEK`; `mus`
and `musculus` match `Mus Musculus`). It is NOT the default; its cross-field AND
(today's `hek_r`->55) is only reached via `--words`.

**Package:** `mlwh/`
**File:** `search.go`
**Test file:** `mlwh/search_test.go`

**Acceptance tests:**

1. Given `SearchSamples("musculus", {Words:true})`, then it returns samples with
   the word `musculus` in a searched field (matching `Mus Musculus`).
2. Given `SearchSamples("hek_r", {Words:true})`, then it returns the broader
   word-prefix set (>= the 4 literal-prefix matches), distinct from the default.

### C3: `--organism` word-membership filter over `common_name`

A `common_name` matches iff it contains the query as a whole word (all query
words for a multi-word query), resolved via `common_name_word_mirror` (A5), then
samples constrained by the indexed `sample_mirror.common_name`. Includes
subspecies (`Mus musculus castaneus`), excludes mid-word fragments (`usculus`)
and is not exact-whole-value. Exempt from the 3-char minimum.

**Package:** `mlwh/`
**File:** `search.go`
**Test file:** `mlwh/search_mysql_test.go`

**Acceptance tests:**

1. Given the mirror, when `SearchSamples(term, {Organism:"musculus"})` runs
   (with a term that does not itself narrow organism), then it matches every
   sample whose `common_name` contains the word `musculus` INCLUDING subspecies,
   so the count exceeds `Mus Musculus`'s 235,447 alone (assert the actual
   word-membership count).
2. Given `--organism usculus`, then it matches nothing (word-membership, not
   substring).
3. Given `--organism "mus musculus"`, then it requires both words (matches
   `Mus Musculus` and `Mus musculus castaneus`).
4. Given `EXPLAIN`, then the organism resolution uses the `(word)` index and the
   sample constraint uses the `common_name` index (no full scan).

### C4: shared exact filter family (search + export), AND-combined, indexed

`--library-type` (exact `library_samples.pipeline_id_lims`), `--organism` (C3),
`--qc pass|fail|pending`, `--deliverables-only` (B2). All AND-combine with the
term and each other, each index-served (EXPLAIN), each exempt from the 3-char
minimum (which stays on the free-text term / `--words`). Filters apply to SAMPLE
search only (not study search). **QC grain (state explicitly):** on sample
SEARCH `--qc` matches the per-sample ROLL-UP verdict (`qc.go` fail>pending>pass;
one bucket per sample; agrees with `SampleProgress.qc`); on the D1 EXPORT (rows
are products) `--qc` matches the raw PER-PRODUCT `qc`. Intersection is by
candidate `id_sample_tmp` set (library_samples for library-type, common_name
index for organism, product/flowcell join for qc/deliverable) - indexed, not a
scan.

**Package:** `mlwh/`
**Files:** `search.go`, `export.go`, `count.go`
**Test file:** `mlwh/search_mysql_test.go`, `mlwh/export_test.go`

**Acceptance tests:**

1. Given a sample search with `--qc pass`, then results are samples whose roll-up
   verdict is pass (fail>pending>pass precedence); a sample with any fail
   product is excluded.
2. Given the SAME `--qc pass` on the export of a study's products, then rows are
   the products whose raw `qc=1` (per-product grain) - a different, product-grained
   result from the search roll-up (assert the grain difference explicitly).
3. Given `--library-type <val> --organism musculus` combined, then results are
   the intersection; `EXPLAIN` shows indexed intersection (no full scan).
4. Given the SAME family applied on the EXPORT surface - e.g. `export irods
study <id> --organism <val>` (a sample-joining relationship) - then only the
   rows whose sample matches the organism are returned (a strict subset of the
   unfiltered export, matching the sample set `--organism <val>` selects), and
   `EXPLAIN` shows the filter is index-served (no full scan) - locking that the
   shared filter family applies identically on `export`, not only on `search`.

---

## D. D1 - fast, generic, column-selectable, multi-format `export` (flagship; replaces `wa mlwh irods`)

### D1a: export backing projection layer

One backing layer projecting a parent's children with chosen columns, format,
filters, order, and completeness. Relationships (each backed by the existing or
D7/D8 endpoint/method):

- `irods` (alias `files`) of `study | sample | run`
- `samples` of `study | run | library`
- `runs` of `study | sample`
- `libraries` of `study`
- `lanes` of `sample`
- `studies` of `sample | faculty-sponsor | user | programme`
- `users` of `study` (D7)
- `sample-crams` of `study` (D8)

**Formats:** `--format tsv|csv|json` (default `tsv`; `--json` = `--format json`).
TSV/CSV = header row + delimited rows; JSON = array of objects keyed by the
selected columns.

**Column selection:** `--columns` ordered, comma-separated, over a documented
per-relationship vocabulary; unknown column = actionable error listing the valid
set; selecting a column never changes filtering. Flagship `irods`-of-`study`
vocabulary: `supplier_name` (alias `supplier_sample_name`), `sanger_sample_id`,
`name`, `study_accession_number`, `id_study_lims`, `manual_qc`, `id_run`, `lane`
(=position), `tag_index`, `platform`, `created`, `merged`, `deliverable`,
`irods_path` (= `CONCAT(irods_root_collection,'/',irods_data_relative_path)`).
Default for that relationship (Q1): `supplier_name, study_accession_number,
sanger_sample_id, manual_qc, irods_path`. Other relationships expose their own
vocabulary (e.g. `runs of sample` -> `id_run, platform, manufacturer, run_date,
date_basis`; `users of study` -> `role, name, login, email`).

**Filters (shared family):** `--file-type` (default `cram` for
irods/files/sample-crams; suffix semantics); `--deliverables-only` ON BY DEFAULT
for the cram file listings (a flag includes controls/sub-products);
`--qc pass|fail|pending` (per-product on the export); `--library-type`,
`--organism` where the child is/joins samples. Filtering and columns independent.

**Order + completeness (no silent truncation, HARD REQ 2):** deterministic per
relationship. For iRODS relationships the canonical order is
`(id_run, position, tag_index, id_seq_product_irods_locations_tmp)`, index-served
by the A4 covering index; a merged object (id_run/lane/tag 0) sorts first with
`merged=true`. `--all` streams the COMPLETE set via KEYSET pagination on that
tuple (NOT LIMIT/OFFSET); a bounded page reports total + next cursor. The CLI
always states whether it emitted a bounded page or the complete set.

**Speed:** <1s per bounded page at study-7699 scale via the A4 single
index-ordered scan (sample_mirror joined by indexed `id_sample_tmp` only for
identity columns). Prove index-served with EXPLAIN; no correlated subqueries, no
full scans of the 9M/7.3M mirrors.

**Correctness:** `manual_qc` via `qc.go` (composite-aware, B1); `irods_path`
verified against real paths (`.../lane6/plex45/51945_6#45.cram` and merged
`.../lane1-2/plex1/49348_1-2#1.cram`); merged CRAMs included and attributed (D8)

- study 7568 flagship cram export returns 732 attributed rows (no `name:""` /
  `id_sample_tmp:0`).

**Package:** `mlwh/`
**File:** `mlwh/export.go`
**Test file:** `mlwh/export_test.go`, `mlwh/cache_mysql_integration_test.go`

```go
// Export projects a relationship into ordered rows for the chosen columns.
func (c *Client) Export(ctx context.Context, rel ExportRelationship, parentID string,
    opts ExportOptions) (ExportResult, error)

type ExportRelationship struct{ Children, ParentKind string } // e.g. {"irods","study"}
type ExportOptions struct {
    Columns          []string
    FileType         string
    DeliverablesOnly *bool  // nil => relationship default (on for cram listings)
    QC               string
    LibraryType      string
    Organism         string
    Sort             string // "" | "created-desc"
    Since, Until     string
    Limit, Offset    int
    All              bool
    Cursor           string // keyset cursor for --all continuation
}
type ExportResult struct {
    Columns    []string
    Rows       [][]string // string-rendered cells in Columns order
    Total      int        // -1 when streaming --all
    NextCursor string     // "" when complete
    Complete   bool       // true when this result is the full set
}
```

**Acceptance tests:**

1. Given study 7556 synced, when `Export({irods,study}, "7556", {Columns:
["supplier_sample_name","study_accession_number","sanger_sample_id",
"manual_qc","irods_path"], FileType:"cram"})` runs (deliverables-only default
   on), then columns are `[supplier_name, study_accession_number,
sanger_sample_id, manual_qc, irods_path]` (alias resolved), every
   `irods_path` ends `.cram`, and the row count equals the deliverable cram count
   (~886; assert actual).
2. Given an unknown column, then the error lists the valid vocabulary and no rows
   are emitted.
3. Given `--format json`, then output is a JSON array of objects keyed by the
   selected columns; `--format csv` uses comma delimiters with a header row;
   default `tsv` uses tabs.
4. Given `--all` on study 7699 (100k+ rows), when streamed, then every matching
   row is emitted via keyset paging (no LIMIT/OFFSET), heap growth stays bounded
   (memory-bounded test, < 20 MiB), and no row is silently capped.
5. Given a bounded page (no `--all`), then `Total >= len(Rows)` and `NextCursor`
   is non-empty when more rows remain.
6. Given `EXPLAIN` on the study cram export at 7699 scale on MySQL, then it is an
   index range scan on the A4 covering index and completes < 1s per page.

### D1b: `wa mlwh export` CLI (replaces `wa mlwh irods`)

Grammar: `wa mlwh export <children> <parent-kind> <parent-id> [flags]`. Remove
`wa mlwh irods`; `wa mlwh export irods study 5901 --file-type cram` is the old
command. Flags: `--columns`, `--format tsv|csv|json`, `--json`, `--file-type`
(default cram for cram listings), `--deliverables-only` / `--include-controls`,
`--qc`, `--library-type`, `--organism`, `--sort created-desc`, `--since/--until`,
`--limit/--offset`, `--all`, `--server`. Both local-cache and `--server` modes;
graceful degradation exit 0.

**Package:** `cmd/`
**File:** `cmd/mlwh_export.go` (remove `cmd/mlwh_irods.go`)
**Test file:** `cmd/mlwh_export_test.go`

**Acceptance tests:**

1. Given `wa mlwh export irods study 5901 --file-type cram`, then it prints the
   cram TSV (header + rows) and exits 0 (behavioural equivalent of the removed
   `wa mlwh irods study 5901 --file-type cram`).
2. Given `wa mlwh export runs sample DN1234 --columns id_run,platform,run_date`,
   then it prints those columns for the sample's runs.
3. Given `wa mlwh export ...` without `--all`, then the CLI prints a line stating
   the output is a bounded page plus the total and next cursor; with `--all` it
   states the complete set was emitted.
4. Given an unknown parent id, then it renders a clean not-found message and
   exits 0.

### D1c: per-relationship `/count` and sizing

Every relationship has a `/count` counterpart and sets X-Total-Count /
X-Next-Offset (reuse the existing `Page[T]`/paging machinery in `remote.go`).
Counts match `len(list-all)` per relationship.

**Package:** `mlwh/`
**Files:** `count.go`, `server.go`, `remote.go`
**Test file:** `mlwh/count_test.go`, `mlwh/remote_test.go`

**Acceptance tests:**

1. Given any relationship, when the count and the full list are fetched, then
   count == len(rows).

---

## E. D2 - iRODS recency ("latest data" / "most recently sequenced")

### E1: `created` on `IRODSPath` + recency order/window

`created` (RFC3339 UTC, "data added", never `last_changed`) is added to
`IRODSPath` and is a selectable export column. ALL THREE iRODS list scopes
(study, sample, run) - and thus the `export irods` relationship - gain
`order_by=created_desc` (default order unchanged) and `since`/`until` (half-open
`[since, until)` over `created`, matching `SamplesWithData`). Study-scoped is
served by `(id_study_lims, created)`; sample-scoped by the new
`(id_sample_tmp, created)` index and run-scoped by the new `(id_run, created)`
index (both added in A4, both on the iRODS mirror, so the run-scoped recency
path scans the iRODS mirror by its denormalised `id_run` rather than the
product-metrics INNER JOIN).

**Package:** `mlwh/`
**Files:** `hierarchy.go`, `availability.go`
**Test file:** `mlwh/hierarchy_test.go`, `mlwh/cache_mysql_integration_test.go`

**Acceptance tests:**

1. Given a study's iRODS rows with distinct `created`, when listed with
   `order_by=created_desc`, then rows are newest-first; every `IRODSPath.Created`
   is populated RFC3339 UTC.
2. Given a SAMPLE's iRODS rows with distinct `created`, when listed with
   `order_by=created_desc` and a `[since, until)` window, then only rows in the
   window are returned, newest-first.
3. Given a RUN's iRODS rows with distinct `created`, when listed with
   `order_by=created_desc` and a `[since, until)` window, then only rows in the
   window are returned, newest-first.
4. Given `since`/`until` on any scope, then only rows with `since <= created <
until` are returned; `until` without `since` errors (existing
   `errUntilRequiresSince`).
5. Given `EXPLAIN` on MySQL for the sample-scoped and the run-scoped
   `created_desc` paths, then each uses its recency index (`(id_sample_tmp,
created)` / `(id_run, created)`) - an index range scan, no full scan, no
   filesort.

### E2: "latest data" endpoints (study + faculty-sponsor)

`GET /study/:id/latest-data` and `GET /latest-data/faculty-sponsor/:name` return
a BOUNDED, pageable page ordered `created DESC` (small default N, e.g. 10), ties
broken by `(id_run, id_product)` - NOT an unbounded "all rows tied at
MAX(created)" set. Rows are `RecentDataRow`. Membership basis = the raw
`seq_product_irods_locations_mirror` scan on `(id_study_lims, created)`;
DOCUMENT this so results reconcile with `StudyOverview.newest_data_added`. The
faculty-sponsor variant joins `study_mirror.faculty_sponsor`; compute each
study's top-N via the `(id_study_lims, created)` index and merge (avoids a global
filesort over 91 studies). CLI: `wa mlwh latest <study-id | --faculty-sponsor
NAME> [--file-type cram]`.

**Package:** `mlwh/`, `cmd/`
**Files:** `availability.go`, `count.go`, `cmd/mlwh_latest.go`
**Test file:** `mlwh/availability_test.go`, `cmd/mlwh_latest_test.go`

**Acceptance tests:**

1. Given a study, when `/study/:id/latest-data` is called, then it returns <= N
   rows ordered `created DESC`, the first row's `created` equals the study's
   `StudyOverview.newest_data_added`, and each row carries `irods_path`,
   `study_name`, sample `name`, `supplier_name`, `id_run`, lane/tag, `platform`.
2. Given the Anderson lab spanning multiple studies, when
   `/latest-data/faculty-sponsor/Anderson` is called, then it returns the top-N
   newest rows across those studies in one call (not one-per-study fan-out).
3. Given `wa mlwh latest 5901 --file-type cram`, then it prints the newest cram
   rows for the study and exits 0.

---

## F. D5 - global run aggregation

### F1: monthly grouped run counts

`GET /runs/monthly?since&until&platform` -> `[]MonthlyRunCount` `{month,
manufacturer, platform, count, date_basis, cache_synced_at}` across all
platforms. Manufacturer map (state it): Illumina->Illumina,
Elembio->Element Biosciences, Ultimagen->Ultima Genomics, PacBio->PacBio,
ONT->Oxford Nanopore. Run grain: Illumina/Element/Ultima = distinct `id_run`;
PacBio = distinct `pac_bio_run_name`; ONT = distinct `experiment_name` - never
wells/flowcells. `date_basis` per platform (section source facts); each response
row states its basis. ONT is included under the labelled warehouse-load basis
(never dropped). Index-served (EXPLAIN, A6 normalised dates). CLI:
`wa mlwh runs --monthly [--since/--until] [--platform ...]`.

**Package:** `mlwh/`, `cmd/`
**File:** `mlwh/runs_agg.go`, `cmd/mlwh_runs.go`
**Test file:** `mlwh/runs_agg_test.go`, `cmd/mlwh_runs_test.go`,
`mlwh/cache_mysql_integration_test.go`

**Acceptance tests:**

1. Given the mirror, when `/runs/monthly` runs over the full window, then PacBio
   count = distinct `pac_bio_run_name` (~2,843, NOT 12,499 wells), ONT = distinct
   `experiment_name` (~447), Ultima `run archived` = 242; each row states its
   `date_basis`; the 8-well `TRACTION-RUN-1000` counts once in Dec-2023.
2. Given ONT rows, then their bucket's `date_basis` == "warehouse load time -
   not a true sequencing date" and they are present (not dropped).
3. Given `EXPLAIN`, then the monthly grouping is index-served (no full scan).

### F2: global run listing + composite id

`GET /runs?platform&since&until` -> `[]RunListingRow`, one row per run, `id` =
composite `<platform>:<native_id>` (`ont:<experiment_name>`), with `platform`
and `native_id` also separate, `manufacturer`, `run_date` + `date_basis`
(ONT carries the warehouse-load caveat). Bounded, paged, `/count`; the composite
id is the keyset cursor. This parentless GLOBAL/all-runs listing is exposed as
`wa mlwh runs` (the D5 listing), removing the "no all-runs endpoint" gap. It is
DISTINCT from the D1a `export` `runs` relationship, which is strictly
parent-scoped (`runs of study | sample`); the global listing is NOT an
`export runs ...` form (the export grammar requires a parent). CLI: `wa mlwh
runs [--platform] [--since/--until]` (the flat listing; `--monthly` selects F1's
grouped counts instead).

**Package:** `mlwh/`
**File:** `mlwh/runs_agg.go`, `count.go`
**Test file:** `mlwh/runs_agg_test.go`

**Acceptance tests:**

1. Given a run per platform, then each `id` is `<platform>:<native_id>`,
   `native_id` matches the platform's run identity, and `run_date`/`date_basis`
   follow the per-platform basis.
2. Given `--all`, then keyset paging on the composite id emits every run
   (no silent cap); count == len(list-all).

---

## G. D7 - `programme` as a first-class dimension, and the study->users inverse

### G1: programme in overview + studies-by-programme + enumeration

- Add `Programme` to `StudyOverview` (additive) so a per-study pass groups by
  programme in one call.
- `GET /studies/programme/:name` (+ `/count`) - exact, indexed (A7) "studies in
  programme X" (NOT the substring `search/study`). Supports the same
  date/platform narrowing where useful; backs `export studies programme "X"`.
- `GET /programmes` -> `[]Programme` (distinct programme + study counts) so a
  caller discovers the controlled vocabulary. CLI: `wa mlwh studies --programme
"Human Genetics"`, `wa mlwh programmes`.

**Package:** `mlwh/`, `cmd/`
**Files:** `availability.go`, `people.go`, `count.go`, `cmd/mlwh_studies.go`
**Test file:** `mlwh/availability_test.go`, `mlwh/people_test.go`,
`cmd/mlwh_studies_test.go`

**Acceptance tests:**

1. Given a study, then `StudyOverview.programme` equals `study_mirror.programme`.
2. Given `/studies/programme/"Human Genetics"`, then it returns exactly the
   studies with that programme (agreeing with `/count`), index-served (EXPLAIN).
3. Given `/programmes`, then it lists distinct programme values with study
   counts.

### G2: grouped sequencing aggregate (generalises F1)

`GET /sequencing/aggregate?group_by=&unit=&platform=&since=&until=` ->
`[]SequencingAggregateRow`. `group_by` in `{month, platform, manufacturer,
programme, faculty_sponsor}` (combinable). `unit` (explicit, caller-chosen):
`runs` (run grain + per-platform `date_basis` per D5) or `samples`/`products`
(data grain, each product mapping to exactly ONE study->programme, windowed by
iRODS `created`). Each row states `unit`, `date_basis`/date field, and
`cache_synced_at`. A run spanning multiple studies/programmes is counted ONCE per
group it touches (state the rule). Index-served (EXPLAIN); no server-side
fan-out over studies. CLI: `wa mlwh runs --monthly --group-by programme --platform
PacBio --since ... --until ...`.

**Package:** `mlwh/`, `cmd/`
**File:** `mlwh/runs_agg.go`, `cmd/mlwh_runs.go`
**Test file:** `mlwh/runs_agg_test.go`, `mlwh/cache_mysql_integration_test.go`

**Acceptance tests:**

1. Given `platform=PacBio, group_by=programme, unit=runs, since/until` over the
   last year, then it returns per-programme run counts, each row stating
   `unit=runs` and PacBio's `date_basis`, in one call.
2. Given a run spanning two studies in different programmes with
   `group_by=programme`, then it is counted once in each programme's group.
3. Given `EXPLAIN`, then the grouping is index-served (no per-study fan-out).
4. Given `group_by=programme, unit=samples` (optionally `platform=PacBio`) over a
   `[since, until)` window, then it returns per-programme DISTINCT-SAMPLE counts
   windowed by iRODS `created` (NOT a run `date_basis`), each row stating
   `unit=samples` and the `created`-based date field (distinct from the runs
   path's per-platform `date_basis`); a sample counts in exactly one programme
   (its study's programme, single attribution per product->study->programme);
   assert a concrete per-programme figure that differs from the `unit=runs`
   result for the same grouping (so a runs-only stub cannot pass).
5. Given `group_by=programme, unit=products` over the same window, then it
   returns per-programme DISTINCT-PRODUCT counts (each product attributed to
   exactly ONE study->programme, counted once) windowed by iRODS `created`; where
   a sample has multiple products the `products` count exceeds the `samples`
   count for that programme (locking products vs samples as distinct grains).

### G3: study->users inverse

`GET /study/:id/users?role=` -> `[]StudyUser` `{role, name, login, email}` from a
single indexed `study_users_mirror.id_study_tmp` lookup joined to `study_mirror`.
`role` is an optional comma-separated filter over the stored vocabulary (`owner`,
`manager`, `data_access_contact`, `follower`, `slf_manager`, `lab_manager`,
`administrator`); DEFAULT (no `role`) returns ALL roles present (state this; it
differs from the person->studies default of owner/manager/data_access_contact).
Reuse `study_users_mirror` (no new table). `faculty_sponsor` is a `Study` field,
NOT a role. Backs `export users study <id> --role owner,manager,follower`.

**Package:** `mlwh/`, `cmd/`
**Files:** `people.go`, `count.go`, `mlwh/export.go`
**Test file:** `mlwh/people_test.go`, `cmd/mlwh_export_test.go`

**Acceptance tests:**

1. Given a study with owner/manager/follower rows, when `/study/:id/users` (no
   role) is called, then it returns all of them from one `id_study_tmp` lookup;
   `EXPLAIN` uses `study_users_mirror_id_study_tmp_idx`.
2. Given `?role=owner,manager`, then only those roles are returned.
3. Given `wa mlwh export users study 7568 --role owner,manager,follower`, then it
   prints `role,name,login,email` rows for those roles.

---

## H. D8 - merged multi-lane / composite CRAM attribution (Q7)

### H1: honest `id_run` / `merged` for composite objects in listings/export

Per Code-authority correction #2, of the three iRODS list scopes only the
STUDY-scoped SQL emits a sample-name column today (from `sample_mirror` joined
on `spi.id_sample_tmp`), so a composite object already has correct
`id_sample_tmp` + non-empty `name` THERE; the SAMPLE-scoped SQL already FINDS the
composite (it filters by `spi.id_sample_tmp`) but emits NO name column today; the
RUN-scoped SQL cannot see it at all (H2). D8 fixes the remaining mis-source:
represent a composite object's run HONESTLY - `merged=true` and `id_run=0` (a
composite spanning lanes/runs has no single run) - rather than the current
misleading `0` with no flag; `merged` comes from the A4 denormalised column. The
export (which projects sample identity for all scopes via the `sample_mirror`
join on the denormalised `id_sample_tmp`) surfaces `name`/`supplier_name`,
`merged`, and `id_run=0` for composites, so no export row is `name:""` /
`id_sample_tmp:0`.

**Package:** `mlwh/`
**Files:** `hierarchy.go`, `types.go`
**Test file:** `mlwh/hierarchy_test.go`

**Acceptance tests:**

1. Given study 7568's merged `lane1-2` object, when listed via the STUDY iRODS
   scope (and via the `export irods` relationship for any scope), then it carries
   the correct non-empty `name`/`id_sample_tmp` and now `merged=true`,
   `id_run=0`.
2. Given a single-lane object, then `merged=false` and `id_run` is its run.

### H2: run-scoped composite visibility

The run-scoped iRODS listing currently INNER-JOINs product-metrics on
`id_iseq_product` and cannot see composites (Code-authority correction #2). With
A3 (composite product rows now mirrored carrying the run's `id_run` for a
single-run multi-lane composite), the run-scoped listing sees and attributes the
composite. Represent it with `merged=true`; where the composite genuinely spans
runs and thus has no single run, it is surfaced via the study/sample listings
and `sample-crams`, not fabricated onto a run.

**Package:** `mlwh/`
**File:** `hierarchy.go`
**Test file:** `mlwh/hierarchy_test.go`, `mlwh/cache_mysql_integration_test.go`

**Acceptance tests:**

1. Given run 49348 with a single-run multi-lane composite, when
   `IRODSPathsForRun(49348)` runs, then the composite object appears (attributed,
   `merged=true`) alongside the single-lane objects (no longer invisible).

### H3: per-sample `sample-crams` export

`GET /study/:id/sample-crams` -> `[]SampleCram` `{name, ega_id, irods_cram_path,
merged}`, one row per sample, resolving each sample's cram through the
sample<->iRODS-mirror linkage (`id_sample_tmp`), so merged CRAMs are included and
a sample's multi-lane rows collapse to one CRAM (de-duplicated: prefer the
merged composite object when present, else the single-lane cram). Bounded/paged +
`/count`. Backs `wa mlwh export sample-crams study 7568`.

**Package:** `mlwh/`, `cmd/`
**Files:** `hierarchy.go`, `count.go`, `mlwh/export.go`, `cmd/mlwh_export.go`
**Test file:** `mlwh/cache_mysql_integration_test.go`, `cmd/mlwh_export_test.go`

**Acceptance tests:**

1. Given study 7568, when `sample-crams` runs, then it returns all 732 samples,
   each with a populated `irods_cram_path` (the 48 run-49348 merged-CRAM samples
   included, each collapsed to one), none blank; the 48 carry `merged=true`.
2. Given a sample with multiple single-lane crams and no merge, then it
   collapses to one row (de-duplicated deterministically).
3. Given the study `export irods` cram listing for 7568, then all merged rows are
   attributed to a sample (no `id_sample_tmp:0` / `name:""`), total 732.

### H4: manifest gap made explicit

For the product-grained `StudyManifest --with-irods`, when a single-lane
product's CRAM cannot be matched (its cram was merged into a composite object),
set per-row `irods_unmatched=true` with `reason="merged_multilane"` and add an
envelope `products_without_irods` counter, so the shortfall is visible without
reconstructing it from count discrepancies. Do NOT resolve the composite path
onto each single-lane row (that would duplicate one merged path across a sample's
lane rows); direct callers to `sample-crams` / `export irods` for the merged-aware
view. Update the manifest registry `Description` to name merged multi-lane CRAMs
as the common cause of an empty `irods_path`.

**Package:** `mlwh/`, `cmd/`
**Files:** `manifest.go`, `types.go`, `registry.go`, `cmd/mlwh_manifest.go`
**Test file:** `mlwh/manifest_test.go`, `cmd/mlwh_manifest_test.go`

**Acceptance tests:**

1. Given study 7568 `manifest --with-irods --file-type cram`, then
   `products_without_irods` equals the number of product rows with empty
   `irods_path` (the merged-multilane single-lane rows), each such row has
   `irods_unmatched=true, reason="merged_multilane"`, and
   `(products) - (products with a path) == products_without_irods` (arithmetic
   closes; assert the actual figure and document its relation to the 48 merged
   samples / 96 single-lane rows).
2. Given a study with no merged CRAMs, then `products_without_irods == 0` and no
   row has `irods_unmatched`.
3. Given `wa mlwh manifest 7568 --with-irods --file-type cram`, then the CLI
   surfaces `products_without_irods` and exits 0.

---

## I. D6 - mirror correctness / perf fixes

### I1: `StatusBreakdown.per_platform` `[]` for empty studies

Return `[]` (never `null`) for studies with no products (observed on 5990, 8338),
so the array schema holds and downstream validation does not break.

**Package:** `mlwh/`
**File:** `progress.go`
**Test file:** `mlwh/progress_test.go`

**Acceptance tests:**

1. Given a study with no products, when `StatusBreakdown` is called, then
   `per_platform == []` (empty array), not null, and JSON serialises `[]`.

### I2: `StudyOverview` / `StatusBreakdown` < 1s at big-study scale

Bring both under 1s at study-7699 scale (both ~3s today). The pure iRODS
aggregate is already 0.085s; profile and fix the sample-membership / per-platform
/ QC-rollup arms with the same denormalisation/index discipline (EXPLAIN), reusing
the A2 char(64) join and A4 columns; do not regress the existing per-platform
breakdown.

**Package:** `mlwh/`
**Files:** `availability.go`, `progress.go`
**Test file:** `mlwh/cache_mysql_integration_test.go`

**Acceptance tests:**

1. Given study 7699 synced on MySQL, when `StudyOverview` and `StatusBreakdown`
   run, then `EXPLAIN` shows index-served arms (no full scans / correlated
   subqueries) and each completes < 1s.

---

## J. Registry, remote, server, docs wiring (HARD REQ 9)

Add registry entries + handler cases + remote client methods + Queryer members
for every new endpoint (`/study/:id/latest-data`,
`/latest-data/faculty-sponsor/:name`, `/runs/monthly`, `/runs`,
`/sequencing/aggregate`, `/studies/programme/:name`, `/programmes`,
`/study/:id/users`, `/study/:id/sample-crams`, and their `/count` siblings), plus
the changed `manual_qc`/`created`/filter params on the iRODS/search/manifest
entries. Every `Description`/`Summary`/`Query` states the exact definition used
(see "Definitions to state"). Update the SearchSamples / CountSampleSearch
Descriptions to state the NEW default (literal whole-value prefix over the four
fields) vs the `--words` mode vs the exact filters and the QC grain difference.
Regenerate docs; drift/parity guards green.

**Package:** `mlwh/`
**Files:** `registry.go`, `server.go`, `remote.go`, `queryer.go`, `docs.go`
**Test file:** `mlwh/registry_test.go`, `mlwh/server_test.go`,
`mlwh/remote_test.go`, `mlwh/docs_test.go`, `mlwh/parity_test.go`,
`mlwh/openapi_test.go`

**Acceptance tests:**

1. Given the registry, then every new endpoint has a Description stating its
   definition (manual_qc / deliverable via entity_type / cram suffix / created /
   per-platform date_basis incl. ONT caveat / search default vs --words vs
   filters / programme unit / study->users direction + role vocab / merged-CRAM
   attribution / freshness), and the search entry no longer claims word-prefix as
   the default.
2. Given each new endpoint, then a remote client method and a server handler case
   exist and round-trip (remote == local for the same args), and the docs/parity
   drift guards pass.

---

## K. CLI exposure (REQUIRED - part of every deliverable; HARD REQ)

All reachable from `wa mlwh` in local-cache and `--server` modes, graceful
degradation exit 0:

- **D1:** `wa mlwh export <children> <parent-kind> <parent-id> [--columns]
[--format tsv|csv|json] [--file-type cram] [--deliverables-only] [--qc]
[--library-type] [--organism] [--sort created-desc] [--since/--until]
[--limit/--offset|--all] [--server] [--json]` (replaces `wa mlwh irods`).
- **D2:** `wa mlwh export irods <scope> <id> --sort created-desc [--since/--until]`
  and `wa mlwh latest <study | --faculty-sponsor NAME> [--file-type cram]`.
- **D3:** `wa mlwh search <term> [--words] [--library-type] [--organism] [--qc
pass|fail|pending] [--deliverables-only] [--type study|sample]` (term required;
  default literal-prefix; exact single-identifier lookups stay `wa mlwh info`).
- **D5:** `wa mlwh runs --monthly [--since/--until] [--platform ...]` (grouped
  counts); `wa mlwh runs [--platform] [--since/--until]` (the flat global
  all-runs listing, F2); and the parent-scoped `wa mlwh export runs <study|sample>
<id>` (the D1a relationship - NOT a parentless form).
- **D7:** `wa mlwh studies --programme "Human Genetics"`; `wa mlwh programmes`;
  `wa mlwh runs --monthly --group-by programme --platform PacBio --since --until`;
  `programme` shown in `info <study>`/overview; `wa mlwh export users study 7568
--role owner,manager,follower`.
- **D8:** `wa mlwh export sample-crams study 7568`; `products_without_irods`
  surfaced in `wa mlwh manifest`; merged CRAMs attributed in `wa mlwh export
irods study <id>`.
- **D4:** `manual_qc` column/section wherever product rows render (`info
<study>`, `manifest`, `export`); `--deliverables-only` where iRODS/product rows
  list.

**Package:** `cmd/`
**Files:** `cmd/mlwh.go`, `cmd/mlwh_export.go`, `cmd/mlwh_runs.go`,
`cmd/mlwh_latest.go`, `cmd/mlwh_studies.go`, `cmd/mlwh_manifest.go`,
`cmd/mlwh_info.go`, `cmd/mlwh_search.go`
**Test file:** the matching `_test.go` files

**Acceptance tests:**

1. Given `wa mlwh --help`, then `export`, `runs`, `latest`, `programmes` are
   listed and `irods` is NOT.
2. Given each command above with a never-synced cache, then it renders a clean
   message and exits 0 (both local and `--server`).
3. Given `wa mlwh info <study>` for a study with sequenced products, then the
   output renders both `programme` (D7) and a `manual_qc` value/section (D4);
   given a study whose products include a non-Illumina (Element/Ultima/PacBio)
   platform, `manual_qc` renders there too (not blank).

---

## Implementation Order

Phases build on tested foundations; within a phase, stories may be parallel.

1. **Foundation (A1-A9).** Schema (new mirrors/columns/indexes, both dialects),
   sync source selection (iseq_flowcell, composite product rows, denormalised
   iRODS export columns, ONT run identity, normalised run dates, organism
   vocabulary), `char(64)` fix, `CacheSchemaVersion=13` / `APIVersion=1.8.0`,
   cold-load read-index set, source integration test. Everything else depends on
   this.
2. **D4 semantics (B1-B2).** manual_qc roll-up surface; deliverable filter via
   entity_type (pass-through PacBio/ONT). Feeds C4 and D1.
3. **D3 search + filters (C1-C4).** Literal-prefix default; `--words`; organism;
   shared filter family (search grain). Depends on A5, B2.
4. **D1 export (D1a-D1c).** Backing projection + keyset + formats/columns; CLI
   replacing `irods`; per-relationship counts. Depends on A4, B, C.
5. **D2 recency (E1-E2).** `created` + order/window; latest-data endpoints + CLI.
   Depends on A4/A6.
6. **D5 run aggregation (F1-F2).** Monthly counts + global run listing. Depends
   on A6.
7. **D7 programme + users (G1-G3).** Overview programme + studies-by-programme +
   enumeration; grouped aggregate (generalises F1); study->users inverse. Depends
   on A7, F1.
8. **D8 merged CRAM (H1-H4).** Honest id_run/merged; run-scoped visibility;
   sample-crams; manifest counter. Depends on A3/A4.
9. **D6 fixes (I1-I2).** per_platform `[]`; overview/status-breakdown < 1s.
   Parallel with 4-8 after phase 1.
10. **Wiring + CLI (J, K).** Registry/remote/server/docs; full CLI exposure;
    regenerate docs, parity/drift guards green.

---

## Appendix: Key Decisions

- **Code is authority; two prompt claims corrected.** (1) The
  `iseq_product_metrics_mirror` PRIMARY KEY already exists - A2 only changes the
  column type to `char(64)` and drops the redundant duplicate index. (2) The
  study-scoped iRODS listing already attributes `id_sample_tmp`/`name` for merged
  CRAMs (via the mirror's denormalised `id_sample_tmp` + a `sample_mirror` join),
  so D8 fixes only the honest `id_run`/`merged` representation, run-scoped
  visibility, composite qc/deliverable, the `sample-crams` export, and the
  manifest counter - it does NOT re-fix attribution that works.
- **Export backing = extend the iRODS mirror (route b-lite).** Denormalise
  `id_run/position/tag_index/qc/is_deliverable/merged` onto
  `seq_product_irods_locations_mirror` at sync (from the char(64) join to the
  composite-aware product mirror + iseq_flowcell mirror) + a covering
  `(id_study_lims, id_run, position, tag_index, id_seq_product_irods_locations_tmp)`
  index. Chosen over (a) a runtime multi-join (measured 15.8s at 7699) and over a
  separate projection table (a second copy of 7.3M rows). Gives one index-ordered
  range scan: < 1s per page AND keyset completeness in one structure, and resolves
  D8 attribution/qc for composites in the same place.
- **Keyset, not LIMIT/OFFSET, for `--all`** (HARD REQ 2): the canonical iRODS
  order tuple is the covering-index prefix + the mirror's row surrogate as the
  unique tiebreaker/cursor; run listing cursor = the composite
  `<platform>:<native_id>`. The CLI always states bounded-page vs complete-set.
- **QC grain is explicit and different by surface:** per-sample roll-up on
  search (agrees with SampleProgress/StatusBreakdown), per-product on export.
- **Deliverable = entity_type only; pass-through for PacBio/ONT.** No `is_spiked`,
  no iRODS AVU read, no composition/component mirroring this wave.
- **Organism = word-membership over a 16k `common_name` vocabulary** (a small
  `common_name`-word helper), including subspecies, excluding mid-word fragments;
  no substring/ngram/FULLTEXT index is built.
- **Run grain = one run identifier** (id_run / pac_bio_run_name /
  experiment_name), never a well/flowcell; ONT always included under the labelled
  warehouse-load `date_basis`.
- **Manifest surfaces the merged gap (flag + counter), does not duplicate a
  composite path across a sample's single-lane rows;** merged-aware paths come
  from `sample-crams` / `export irods`.
- **Testing (HARD REQ 8):** TDD, behaviour-focused (per testing-principles);
  preserve all existing regressions. Real-MySQL integration tests
  (`cache_mysql_integration_test.go` pattern: throwaway DB dropped on cleanup,
  skipped without creds) assert the new paths execute on MySQL, are index-served
  (EXPLAIN), and return the correct counts/rows (hek_r->4; organism musculus
  word-membership incl. subspecies, usculus->0; 7556 deliverable ~886 assert
  actual; QC grain difference; PacBio ~2,843 / ONT ~447 / Ultima 242; study->users
  roles; programme filter + grouped aggregate; 7568 sample-crams=732 all
  populated + manifest `products_without_irods`). Add the source integration test
  (A9). Memory-bounded test for `--all` streaming (< 20 MiB heap growth). See the
  go-implementor and go-reviewer skills; every acceptance test above maps to a
  GoConvey test - no stubs, no hardcoded results, no build-tag exclusions.
