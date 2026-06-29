# Study Metadata, Manifests, Run iRODS, QC, People-to-Studies Specification

## Overview

The `wa mlwh serve` REST API (package `mlwh/`) and the `wa mlwh` CLI cannot
today answer cheaply a second wave of real user questions: the data access
groups for a study, the iRODS cram path for a run, a study's per-sample data
manifest, how many of a study's samples have passed manual QC, the study id
behind an ambiguous name, and which studies belong to a named sponsor or a
person by login/email. This feature adds a small, indexed, platform-aware
surface so each question is one request (counts/breakdowns) or one bounded,
pageable list (manifests, run iRODS, studies-by-person), and exposes every
result through the CLI.

It reuses the realworld1 machinery wholesale: the four-step add-a-query recipe
(schema column/index in both dialects, one `Client` method, one `Queryer`
member, one `Registry` entry), `queryCount`, the never-synced / empty-study
cascade (`countSamplesForEmptyStudy`), the `id_lims = 'SQSCP'` invariant, the
paginated list-sizing headers (`X-Total-Count` / `X-Next-Offset` + typed
`Page[T]`), the validate-before-query 400 pattern (`mlwhQueryRFC3339`), the
per-sample QC roll-up (`rollUpSampleQC`, fail > pending > pass, `not_tracked`
when no products), the study membership join (`studyDataMembershipJoin`) and
predicates (`studyScopedIRODSExists` / `studyScopedProductMetricsExists`), and
the freshness `cache_synced_at` discipline.

Five deliverables: **D1** run-scoped iRODS + a filename-suffix file-type filter
(also on study/sample iRODS) and `id_run` + `platform` on each iRODS row; **D2**
a paginated study data manifest (one row per product) with an optional
iRODS-path column; **D3** study-level QC counts folded into `StatusBreakdown`;
**D4** two people-to-studies endpoints (faculty sponsor; `study_users` role
membership) plus a resolve-person directory, backed by a new
`study_users_mirror`; **D5** cheap study-metadata exposure (data access groups
on the overview; disambiguation fields on search rows).

## Architecture

### Packages and files

- `mlwh/` -- all production code (reuse existing files; new files noted below).
- `mlwh/cache_schema/{sqlite,mysql}/*.sql` -- schema in both dialects, kept in
  parity (the cross-dialect shape test compares them).
- `cmd/*.go` -- CLI wiring only (no business logic).
- `.docs/mcp/api-reference.md`, `.docs/mcp/glossary.md` -- regenerated/updated.

### New / changed source files

| File                              | Purpose                                                                                                   |
| --------------------------------- | --------------------------------------------------------------------------------------------------------- |
| `mlwh/hierarchy.go`               | `id_run`+`platform` on `IRODSPath` rows; file-type filter on study/sample/run iRODS; `IRODSPathsForRun`   |
| `mlwh/manifest.go` (new)          | study data manifest list + count (one row per product, optional iRODS path)                               |
| `mlwh/progress.go`                | extend `StatusBreakdown` with study-level `qc_pass`/`qc_fail`/`qc_pending` over sequenced samples         |
| `mlwh/people.go` (new)            | studies-by-faculty-sponsor, studies-by-user (role-filtered), resolve-person directory                     |
| `mlwh/availability.go`            | surface `data_access_group`/`faculty_sponsor`/`name`/`accession_number` on `StudyOverview`                |
| `mlwh/types.go`                   | new result structs + `IRODSPath`/`StatusBreakdown`/`StudyOverview` field additions (additive)             |
| `mlwh/count.go`                   | `/count` SQL + methods for the new lists                                                                   |
| `mlwh/registry.go`                | new `Endpoint` entries; extend recency/QC Descriptions                                                     |
| `mlwh/queryer.go`                 | new `Queryer` members                                                                                     |
| `mlwh/server.go`                  | one handler `case` per new Method; file-type query parsing; role query parsing                            |
| `mlwh/remote.go`                  | new `RemoteClient` methods (+ `Page[T]` variants for new paginated lists)                                 |
| `mlwh/sync.go`                    | `study_users` constant; `studyUsersMirrorColumns`; new index sets; sparse-read decisions; iRODS unchanged |
| `mlwh/sync_platform_coverage.go`  | `studyUsersWholesaleSpec` + dispatch + `wholesaleMirrorTables` entry                                      |
| `mlwh/cache_schema.go`            | register `study_users_mirror` in the order/recreate/drop/sync-state lists                                 |
| `mlwh/cache.go`                   | bump `CacheSchemaVersion` 10 -> 11                                                                        |
| `mlwh/openapi.go`                 | bump `APIVersion` 1.6.0 -> 1.7.0                                                                           |
| `mlwh/freshness.go`               | add `study_users` to `freshnessSyncTables`                                                                |
| `cmd/mlwh_info.go`                | study QC counts + study metadata on `info <study>`; run-scoped iRODS on `info <run>`                      |
| `cmd/mlwh_irods.go` (new)         | `wa mlwh irods <study\|run\|sample> [--file-type] [paging]`                                               |
| `cmd/mlwh_manifest.go` (new)      | `wa mlwh manifest <study> [--with-irods --file-type] [paging]`                                            |
| `cmd/mlwh_studies.go` (new)       | `wa mlwh studies --faculty-sponsor <n> / --user <p> [--role ...]`; `wa mlwh people <term>`                |

### Existing infrastructure (authoritative; reuse, do not duplicate)

- Registry `Endpoint{Method,Verb,Path,PathParams,Query,Paginated,NewResult,
  Summary,Description,QueryParams}`; `newResult[T]`, `newSliceResult[T]`,
  `fetchAllPaginationParams()`, `searchPaginationParams()`. OpenAPI + endpoint
  reference derive from the Registry; no manual OpenAPI edits beyond
  `APIVersion`.
- `queryCount(ctx, sql, action, args...) (int, error)` (`count.go`).
- Never-synced cascade: `requireAnySyncState`, `requiredSyncStateSummary`,
  `cacheStudyExists`, `neverSyncedReadErr` (= `ErrNotFound` +
  `ErrCacheNeverSynced`), `countSamplesForEmptyStudy`.
- Handler switch `mlwhEndpointHandler` (one `case` per Method); `mlwhPathParam`,
  `mlwhIDAndPagination`, `mlwhPaginationFromQuery`, `mlwhQueryInt`,
  `mlwhQueryRFC3339`, `writeMLWHResult`, `writeMLWHBadRequest`,
  `writeMLWHPaginatedResult`, `writeListSizingHeaders`, `countValue`.
- RemoteClient `remoteCall[T]` / `remoteCallPage[T]` / `remotePagination` /
  `remoteAddedWindow`; dynamic `Call` derived from the Registry.
- QC roll-up: `sampleProductMetricsQCUnion()`, `rollUpSampleQC(productCount,
  pending, minQC)`, constants `qcPass`/`qcFail`/`qcPending` (`qc.go`),
  `qcNotTracked` (`progress.go`).
- Study membership: `studyDataMembershipJoin`, `studyScopedIRODSExists(window)`,
  `studyScopedProductMetricsExists()`, `platformsForStudySamplesSQL`,
  `platformCanonicalOrder`, `oldestFeedingLastRun`,
  `statusBreakdownFeedingTables`.
- iRODS reads: `irodsPathsForStudyCacheSQL` (already `LEFT JOIN sample_mirror`,
  selects `id_sample_tmp`,`name`), `irodsPathsForSampleCacheSQL`,
  `queryIRODSPaths` / `queryIRODSPathsWithSample`.
- Sync:
  `wholesaleMirrorSpec{syncTable,mirrorTable,mirrorColumns,sourceQuery,scan}`,
  `wholesaleMirrorTables()`, `wholesaleMirrorSpecFor`,
  `syncWholesaleMirrorTable` (the `oseq_flowcell_mirror` pattern);
  `syncMirrorIndexSet` / `syncIndexSpec`; `studyMirrorColumns` includes
  `id_study_tmp` (the `study_users` -> `study_mirror` link key);
  `studySourceColumnSpecs` alias mechanism.
- `CacheSchemaVersion` (`cache.go`); migration via
  `cacheMigrationRecreateTables` / `cacheMigrationDropTables` /
  `cacheMigrationSyncStateTables` (full resync).
- Resolver helpers `resolveStudyFromCache`, `resolveSampleFromCache`; sentinels
  `ErrNotFound`, `ErrAmbiguous`, `ErrUnsupportedIdentifier`,
  `ErrCacheNeverSynced`, `ErrUpstreamImpaired`; `httpStatusAndErrorCode`
  (404/400/409 mapping).

### Source-schema facts (verified against the live `mlwarehouse`; FIRM)

- `study` has `data_access_group`, `faculty_sponsor`, `name`,
  `accession_number`, `id_study_tmp`. (Mirrored; `study_mirror` carries all of
  them.)
- `study_users` exists: `id_study_users_tmp`, `id_study_tmp`, `last_updated`,
  `role`, `login`, `email`, `name`. Roles observed: `follower`, `manager`,
  `owner`, `data_access_contact`, `slf_manager`, `lab_manager`, `administrator`.
  Linked to a study via `id_study_tmp`. Distinct from `study.faculty_sponsor`.
  Worked example "Carl Anderson": `faculty_sponsor LIKE '%Carl Anderson%'` -> 91
  studies (verified live); via `study_users` owner of 59, data_access_contact of
  5, follower of 5.
- `iseq_product_metrics.qc` is the manual-QC verdict (1=pass, 0=fail,
  NULL=pending); already mirrored to `iseq_product_metrics_mirror.qc`
  (nullable).
- `seq_product_irods_locations` has NO `id_run` and NO file-type column.
  Run-scope is obtained by joining `id_iseq_product` ->
  `iseq_product_metrics_mirror.id_run`; file type is a filename-suffix match on
  `irods_file_name`. The iRODS mirror already stores `created` (nullable) and
  `platform` (the source `seq_platform_name`, e.g. "illumina").

### New / changed result types (additive to `mlwh/types.go`)

```go
// IRODSPath gains IDRun and Platform (D1). IDRun is the Illumina NPG run id,
// derived by LEFT JOIN id_iseq_product -> iseq_product_metrics_mirror.id_run; it
// is 0 when not derivable (non-Illumina / unmatched), matching the existing
// RunOverview.IDRun / RunStatusTimeline.IDRun "0 for non-Illumina" convention.
// Platform is the iRODS row's mirrored platform string (the source
// seq_platform_name, e.g. "illumina"), so a 0 id_run reads as ONT / non-Illumina
// rather than ambiguous. Both fields are additive; existing fields unchanged.
type IRODSPath struct {
    IDProduct   string `json:"id_product" doc:"product identifier of the iRODS data object"`
    Collection  string `json:"collection" doc:"iRODS collection containing the data object"`
    DataObject  string `json:"data_object" doc:"iRODS data object name"`
    IRODSPath   string `json:"irods_path" doc:"full iRODS path of the data object"`
    IDSampleTmp int64  `json:"id_sample_tmp" doc:"internal MLWH surrogate key of the sample the data object belongs to"`
    Name        string `json:"name" doc:"Sanger sample name of the sample the data object belongs to; empty when absent from the sample mirror"`
    IDRun       int    `json:"id_run" doc:"Illumina NPG run id of the data object; 0 when not derivable (non-Illumina or unmatched)"`
    Platform    string `json:"platform" doc:"platform string the iRODS row was synced with (source seq_platform_name); disambiguates a 0 id_run as ONT/non-Illumina"`
}

// ManifestRow is one row of a study's data manifest: one sequencing product
// (run x position x tag) joined to its sample's identity, plus the study-level
// metadata carried once in the envelope (not per row). When the file-type / iRODS
// path is requested, IRODSPath is the data object for that product matching the
// suffix filter (empty string when the product has no matching iRODS object).
type ManifestRow struct {
    Name            string `json:"name" doc:"Sanger sample name"`
    SupplierName    string `json:"supplier_name" doc:"supplier-given sample name"`
    AccessionNumber string `json:"accession_number" doc:"sample public archive accession number"`
    SangerSampleID  string `json:"sanger_sample_id" doc:"Sanger sample id"`
    IDRun           int    `json:"id_run" doc:"Illumina NPG run id of the product"`
    Position        int    `json:"lane" doc:"lane position of the product"`
    TagIndex        int    `json:"tag_index" doc:"multiplexing tag index of the product"`
    IRODSPath       string `json:"irods_path,omitempty" doc:"iRODS path of the product's data object matching the file-type filter; present only when with_irods is set"`
}

// StudyManifest is the manifest envelope: the study-level metadata once, plus the
// page of product rows. The page is bounded/pageable; the study fields answer Q3's
// "study details" without repeating per row (D2/D5).
type StudyManifest struct {
    IDStudyLims     string        `json:"id_study_lims" doc:"LIMS study id"`
    Name            string        `json:"name" doc:"study name"`
    AccessionNumber string        `json:"accession_number" doc:"study accession number"`
    FacultySponsor  string        `json:"faculty_sponsor" doc:"study faculty sponsor"`
    DataAccessGroup string        `json:"data_access_group" doc:"study data access group"`
    Rows            []ManifestRow `json:"rows" doc:"page of per-product manifest rows"`
    CacheSyncedAt   string        `json:"cache_synced_at" doc:"oldest last_run across feeding tables (UTC RFC3339)"`
}

// PersonStudy is one studies-by-person result row: the study plus the matched
// role (D4). Role is empty for the faculty-sponsor endpoint (sponsor is not a
// study_users role) and is the study_users role for the user endpoint.
type PersonStudy struct {
    Study Study  `json:"study" doc:"the study the person is associated with"`
    Role  string `json:"role,omitempty" doc:"study_users role matched (empty for the faculty-sponsor endpoint)"`
}

// PersonCandidate is one distinct candidate person from the resolve-person
// directory (D4): a canonical stored form plus how many studies it covers, so a
// caller can disambiguate a partial/spoken name before running a studies query.
// Source is "faculty_sponsor" (Name carries the free-text sponsor; Login/Email
// empty) or "study_users" (Name/Login/Email/Role carry the stored study_users
// identity).
type PersonCandidate struct {
    Source     string `json:"source" doc:"faculty_sponsor or study_users"`
    Name       string `json:"name" doc:"canonical stored full name"`
    Login      string `json:"login,omitempty" doc:"Sanger username (study_users only)"`
    Email      string `json:"email,omitempty" doc:"email (study_users only)"`
    Role       string `json:"role,omitempty" doc:"study_users role (study_users only)"`
    StudyCount int    `json:"study_count" doc:"distinct studies for this candidate"`
}
```

`StudyOverview` gains four metadata fields (D5, additive):

```go
    Name            string `json:"name" doc:"study name"`
    AccessionNumber string `json:"accession_number" doc:"study accession number"`
    FacultySponsor  string `json:"faculty_sponsor" doc:"study faculty sponsor"`
    DataAccessGroup string `json:"data_access_group" doc:"study data access group governing data access"`
```

`StatusBreakdown` gains a QC sub-struct (D3, additive):

```go
// StudyQCBreakdown is the QC split of a study's SEQUENCED (distinct) samples
// (D3): qc_pass/qc_fail/qc_pending partition the sequenced samples using the same
// per-sample roll-up progress.go applies (fail > pending > pass), so the three
// sum to "sequenced" (= samples_total - the registered bucket). not_tracked
// samples (no products, incl. ONT) are NOT sequenced and are excluded here.
type StudyQCBreakdown struct {
    QCPass    int `json:"qc_pass" doc:"distinct sequenced samples whose roll-up QC is pass"`
    QCFail    int `json:"qc_fail" doc:"distinct sequenced samples whose roll-up QC is fail"`
    QCPending int `json:"qc_pending" doc:"distinct sequenced samples whose roll-up QC is pending"`
}
// added to StatusBreakdown:
    QC StudyQCBreakdown `json:"qc" doc:"QC split of the sequenced (distinct) samples; sums to samples_total - registered"`
```

### Definitions to state in Descriptions (the MCP contract; HARD REQ 8)

- **received / sequenced / not-sequenced (D3):** received = `samples_total`;
  sequenced = the distinct samples with >=1 product-metrics row in the study
  (any platform) = `samples_total - distinct.registered`; not-sequenced =
  `distinct.registered` (linked samples with no products, incl. ONT). The QC
  split is over the sequenced samples only.
- **passed manual QC:** the per-sample roll-up of `iseq_product_metrics.qc`
  (1=pass, 0=fail, NULL=pending) across the study's products, fail > pending >
  pass, identical to `SampleProgress.qc`. `qc_pass` counts sequenced samples
  whose roll-up is pass.
- **faculty_sponsor vs study_users (D4):** a person-NAME query maps to
  `study.faculty_sponsor` (the named PI/sponsor, free-text); a login/email/"my
  studies" query maps to `study_users` (role membership). They return different
  sets. The user endpoint matches case-insensitively across `name`, `login` AND
  `email` (substring). Default roles: `owner`, `manager`, `data_access_contact`;
  `follower`/`slf_manager`/`lab_manager`/`administrator` are excluded unless
  `role=` widens. Each user-endpoint row surfaces the matched `role`.
- **file-type filter (D1, open suffix):** any token; strip a single leading `.`;
  match `irods_file_name LIKE '%.<token>'` case-insensitively. A
  valid-but-unmatched suffix yields an EMPTY result (not an error). 400 only
  when the value is empty/whitespace or contains `%`, `_`, or `/`. It is a
  FILENAME-SUFFIX filter, not a real file-type column. `/count` honours the same
  filter, so an empty result is distinguishable from "no data".
- **run-scope for iRODS (D1):** the run's `iseq_product_metrics_mirror` rows
  joined to the iRODS mirror by the shared `id_iseq_product`; `:id` is the
  Illumina NPG `id_run`.
- **`id_run` on iRODS rows (D1):** 0 = not derivable (non-Illumina/unmatched);
  `platform` disambiguates.
- **freshness (HARD REQ 5):** iRODS `created` = data added; `last_changed` =
  sync key (not surfaced); `cache_synced_at` / `/freshness` = freshness caveat.
  Manifest and iRODS results are complete only up to the last sync.

### Error handling

Reuse the sentinels and cascade. New count/aggregate endpoints return the zero
value + `neverSyncedReadErr()` on a never-synced cache, `ErrNotFound` for an
unknown study/run, and a zero-figure result for a synced-but-empty study (as
`CountSamplesForStudy`). The people endpoints (no parent identifier to "not
find") return an EMPTY list + `neverSyncedReadErr()` on a never-synced cache and
an empty list (no error) for a synced cache with no matches. A 400-class
file-type/role validation happens in the handler before the queryer (the
`Client` re-validates defensively and returns `ErrUnsupportedIdentifier` so a
direct caller is not silently wrong).

## A. Schema, sync, and migration foundation (HARD REQ 2)

### A1: study_users_mirror table, both dialects

As an implementor, I want a `study_users_mirror` table in both dialects so the
role-membership data has a home.

Add `cache_schema/{sqlite,mysql}/study_users_mirror.sql` (mirror the
`oseq_flowcell_mirror.sql` style; mysql uses the collation token for text):

- columns: `id_study_users_tmp` (INTEGER PK), `id_study_tmp` (INTEGER NOT NULL),
  `role` (TEXT/VARCHAR NOT NULL), `login` (TEXT/VARCHAR NOT NULL), `email`
  (TEXT/VARCHAR NOT NULL), `name` (TEXT/VARCHAR NOT NULL), `last_updated`
  (TEXT/VARCHAR NOT NULL). Text columns `COLLATE NOCASE` (sqlite) /
  `{{MYSQL_TEXT_COLLATION}}` (mysql) on `login`,`email`,`name` so
  case-insensitive lookup is index-friendly.
- indexes (both dialects): `study_users_mirror_id_study_tmp_idx (id_study_tmp)`,
  `study_users_mirror_login_idx (login)`, `study_users_mirror_email_idx
  (email)`, `study_users_mirror_name_idx (name)`, `study_users_mirror_role_idx
  (role)`.

Register `study_users_mirror` in `schemaStatementOrder`,
`cacheMigrationRecreateTables`, and `cacheMigrationDropTables`
(`cache_schema.go`); add the sync table name to `cacheMigrationSyncStateTables`.

**Package:** `mlwh/`
**Files:** `cache_schema/{sqlite,mysql}/study_users_mirror.sql`,
`cache_schema.go`
**Test file:** `mlwh/cache_schema_test.go`

**Acceptance tests:**

1. Given the sqlite schema, when parsed into a `schemaShape`, then
   `study_users_mirror` has columns `id_study_users_tmp`, `id_study_tmp`,
   `role`, `login`, `email`, `name`, `last_updated` and the five declared
   indexes.
2. Given the mysql schema, when parsed, then the same columns and indexes exist
   and the two dialects compare equal (the existing cross-dialect shape test
   passes).
3. Given an opened ephemeral sqlite cache, when a row is inserted with the
   `study_users_mirror` column list, then it reads back unchanged.

### A2: faculty_sponsor index + iRODS id_iseq_product index, both dialects

As an implementor, I want indexes backing the new query paths so they are
index-served (HARD REQ 1).

- Add to `study_mirror.sql` (both dialects): `study_mirror_faculty_sponsor_idx
  (faculty_sponsor)`. (Backs D4 faculty-sponsor lookup. Substring `LIKE
  '%term%'` cannot use it for a leading wildcard, but it backs the
  resolve-person `GROUP BY faculty_sponsor` distinct enumeration and any
  anchored/equality probe; declare it as the contract.)
- Add to `seq_product_irods_locations_mirror.sql` (both dialects):
  `spi_mirror_iseq_product_idx (id_iseq_product)`. (Backs D1 run-scoped iRODS:
  filter `iseq_product_metrics_mirror` by `id_run` over its `(id_run, position,
  tag_index)` index, then join the iRODS mirror on `id_iseq_product` -- without
  this index that join is a full scan of the ~9M-row iRODS mirror. Also backs
  the D2 manifest's per-product iRODS LEFT JOIN.)

**Package:** `mlwh/`
**Files:** `cache_schema/{sqlite,mysql}/study_mirror.sql`,
`cache_schema/{sqlite,mysql}/seq_product_irods_locations_mirror.sql`
**Test file:** `mlwh/cache_schema_test.go`

**Acceptance tests:**

1. Given both schemas, when parsed, then `study_mirror` has an index on
   `(faculty_sponsor)` and `seq_product_irods_locations_mirror` has an index on
   `(id_iseq_product)`, in both dialects, and the dialects compare equal.
2. Given the existing cross-dialect shape test, when run, then it still passes
   with the new indexes present in both dialects.

### A3: study_users wholesale sync

As an implementor, I want `study_users` synced into `study_users_mirror` so the
role data stays fresh.

- Add `syncTableStudyUsers = "study_users"` (`sync.go`); add it to
  `supportedSyncTables` (`sync.go`) and `freshnessSyncTables` (`freshness.go`)
  in the same position.
- Add `studyUsersWholesaleSpec()` returning a `wholesaleMirrorSpec` (the
  `oseqFlowcellWholesaleSpec` pattern):
  - `mirrorColumns`: `id_study_users_tmp, id_study_tmp, role, login, email,
    name, last_updated`.
  - `sourceQuery`: `SELECT su.id_study_users_tmp, su.id_study_tmp, su.role,
    su.login, su.email, su.name, su.last_updated FROM study_users su INNER JOIN
    study ON study.id_study_tmp = su.id_study_tmp AND study.id_lims = 'SQSCP'
    ORDER BY su.id_study_users_tmp` (the INNER JOIN to `study` mirrors the oseq
    pattern: only rows whose study is an SQSCP study are mirrored, so the link
    to `study_mirror.id_study_tmp` always resolves; NULL `login`/`email`/`name`
    are COALESCEd to `''` in the scan).
  - `scan`: scan the 7 columns (nullable text via `sql.NullString` -> `''`),
    skip a row whose `id_study_tmp` is 0.
- Add `syncTableStudyUsers` to `wholesaleMirrorTables()` and a `case` to
  `wholesaleMirrorSpecFor` and the `syncTableData` wholesale `case` group.

**Package:** `mlwh/`
**Files:** `mlwh/sync.go`, `mlwh/sync_platform_coverage.go`, `mlwh/freshness.go`
**Test file:** `mlwh/sync_test.go`, `mlwh/freshness_test.go`

**Acceptance tests:**

1. Given a mocked source `study_users` returning rows (incl. one with a NULL
   `email`) for an SQSCP study, when the `study_users` table syncs, then
   `study_users_mirror` contains the rows with `email` stored as `''` and the
   correct `id_study_tmp` / `role` / `login` / `name`.
2. Given a `study_users` source row whose study is not SQSCP, when syncing, then
   it is NOT mirrored (the INNER JOIN to `study` drops it).
3. Given a never-synced cache, when `Freshness` runs, then it returns an entry
   for `study_users` (the new `freshnessSyncTables` total), `ever_synced=false`,
   empty timestamps, and no error.
4. Given a `study_users` `sync_state` row, when `Freshness` runs, then that
   table is reported with its `last_run`.

### A4: CacheSchemaVersion bump (full resync)

As an implementor, I want the schema change to take effect via the existing
recreate-tables migration (full resync), per the settled Notes.

Bump `CacheSchemaVersion` from 10 to 11 (`cache.go`). The new
`study_users_mirror` table and all new indexes are created by the
recreate-tables migration (which performs a full resync); do NOT take the
additive `IF NOT EXISTS` no-version-bump path. `study_users_mirror` is in
`cacheMigrationRecreateTables`/`...DropTables` (A1) and `study_users` is in
`cacheMigrationSyncStateTables` (A1), so the migration recreates the table
cleanly and the next sync repopulates it.

**Package:** `mlwh/`
**Files:** `mlwh/cache.go`
**Test file:** `mlwh/cache_test.go` (the existing migration tests)

**Acceptance tests:**

1. Given `CacheSchemaVersion`, when read, then it equals 11.
2. Given a v10 cache opened by `OpenCache`, when the migration runs, then it
   recreates the tables (including `study_users_mirror`), clears the sync-state
   rows for the recreated tables, and stamps `schema_version` to 11 (the
   existing recreate-migration test, extended to cover `study_users_mirror`).

### A5: sparse cold-load read-index decisions

As an implementor, I want the new query paths to stay index-served after a cold
load on the large mirrors (the documented trap), without over-indexing the small
ones.

- `seq_product_irods_locations_mirror` is in the sparse cold-load read-index set
  (`seqProductIRODSLocationsMirrorReadIndexes`). Add
  `spi_mirror_iseq_product_idx (id_iseq_product)` (A2) to BOTH the mirror's full
  secondary-index set AND `seqProductIRODSLocationsMirrorReadIndexes`, so the D1
  run-scoped iRODS join and the D2 manifest iRODS LEFT JOIN are index-served
  immediately after a cold load (not only after the full index rebuild).
- `study_mirror` and `study_users_mirror` are small reference tables and are NOT
  in the sparse cold-load read-index machinery (`study_mirror` already is not).
  Their indexes (A1, A2) are created with the table; no sparse read-index
  variant is added.

**Package:** `mlwh/`
**Files:** `mlwh/sync.go`
**Test file:** `mlwh/sync_test.go`, `mlwh/cache_test.go`

**Acceptance tests:**

1. Given `seqProductIRODSLocationsMirrorReadIndexes`, when inspected, then it
   includes `spi_mirror_iseq_product_idx (id_iseq_product)`.
2. Given a sqlite cache whose iRODS mirror was cold-loaded (sparse read indexes
   installed, full set not yet rebuilt), when the post-cold-load index shape is
   checked, then it is accepted (the existing `sqliteLargeCacheReadIndexShape` /
   `...SparseReadIndexColumns` accept-list is updated to the new shape and still
   passes).

## B. D1 -- Run-scoped iRODS, id_run/platform, and the file-type filter

### B1: id_run and platform on every iRODS row

As a caller, I want each iRODS row to carry its run and platform so a row's
provenance is visible and a manifest can show the run (Q2/Q4).

Extend the study/sample/run iRODS read SQL to LEFT JOIN
`iseq_product_metrics_mirror` on `id_iseq_product` and select `MIN(id_run)` (or
`MAX`; deterministic) as `id_run`, plus the iRODS mirror's `platform`. `id_run`
is 0 when the LEFT JOIN finds no match (non-Illumina / unmatched). `platform` is
the iRODS row's mirrored `platform` string. Add `IDRun`/`Platform` to the scan
in `queryIRODSPaths` / `queryIRODSPathsWithSample`; update the matching `/count`
DISTINCT projections only if the added columns change the distinct grain (they
should not -- `id_iseq_product` already keys a row; keep the count SQL grain
unchanged so count == len(list)).

**Package:** `mlwh/`
**Files:** `mlwh/hierarchy.go` (SQL + scan), `mlwh/types.go`
**Test file:** `mlwh/hierarchy_test.go`

**Method signatures:** unchanged (`IRODSPathsForStudy`, `IRODSPathsForSample`).

**Acceptance tests:**

1. Given a study `S1` with an Illumina iRODS row whose `id_iseq_product` matches
   an `iseq_product_metrics_mirror` row on run 52553 (platform "illumina"), when
   `IRODSPathsForStudy("S1", all)` is fetched, then that row has `id_run=52553`
   and `platform="illumina"`.
2. Given an iRODS row (platform "ont") whose `id_iseq_product` matches NO
   `iseq_product_metrics_mirror` row, when the study iRODS list is fetched, then
   that row has `id_run=0` and `platform="ont"`.
3. Given `CountIRODSPathsForStudy("S1")` and `len(IRODSPathsForStudy("S1",
   all))`, when both are taken, then they are equal (the id_run/platform
   additions did not change the row grain).

### B2: file-type filter on study/sample/run iRODS lists and counts

As a caller, I want to list only the cram (or any suffix) data objects for a
study/sample/run in one call (Q2), with the count honouring the same filter.

Add a `fileType string` parameter (after `studyLimsID`/`sangerName`/`idRun`,
before `limit, offset`) to the iRODS list and count methods. When `fileType` is
empty, behaviour is unchanged (all rows). When set, normalise it (strip one
leading `.`, lowercase) and append `AND LOWER(irods_file_name) LIKE '%.' ||
LOWER(?)` (sqlite) / the mysql equivalent to the WHERE clause, binding the
normalised token. A normalised token containing `%`, `_`, or `/`, or empty after
trimming, is rejected with `ErrUnsupportedIdentifier` (the handler returns 400
first; the Client re-validates). A valid-but-unmatched suffix yields an empty
list (no error) on a synced cache.

The HTTP layer: `GET /study/:id/irods`, `/sample/:id/irods`, `/run/:id/irods`
(and their `/count`) accept a `file_type` query param. A present-but-invalid
`file_type` aborts with the bad_request 400 envelope before the queryer (new
helper `mlwhFileTypeFromQuery` mirroring `mlwhQueryRFC3339`: empty -> ("",
true); invalid chars -> 400; else (normalised, true)).

**Package:** `mlwh/`
**Files:** `mlwh/hierarchy.go`, `mlwh/count.go`, `mlwh/server.go`,
`mlwh/registry.go` (add `Query: ["file_type"]` + a `fileTypeQueryParam`),
`mlwh/remote.go`
**Test file:** `mlwh/hierarchy_test.go`, `mlwh/server_test.go`,
`mlwh/count_test.go`

**Method signatures (file-type-aware variants; keep the existing bare methods
delegating with `fileType=""`):**

```go
IRODSPathsForStudyByFileType(ctx, studyLimsID, fileType string, limit, offset int) ([]IRODSPath, error)
IRODSPathsForSampleByFileType(ctx, sangerName, fileType string, limit, offset int) ([]IRODSPath, error)
CountIRODSPathsForStudyByFileType(ctx, studyLimsID, fileType string) (Count, error)
CountIRODSPathsForSampleByFileType(ctx, sangerName, fileType string) (Count, error)
```

(The server dispatches the bare vs file-type path by whether `file_type` is set,
via an optional-capability interface like `samplesWithDataSinceQueryer`, so the
existing `IRODSPathsForStudy`/`...Sample` methods and Registry entries keep the
same `Method` name and the file-type param is an additive query param on the
same endpoint.)

**Acceptance tests:**

1. Given `S1` with iRODS objects `a.cram`, `b.cram`, `c.bai` (3 objects), when
   the study iRODS list is fetched with `file_type=cram`, then it returns
   exactly the two `.cram` objects, and the matching count is 2.
2. Given `file_type=.CRAM` (leading dot, mixed case), when fetched, then it
   matches the same two `.cram` objects (leading dot stripped,
   case-insensitive).
3. Given `file_type=bam` (valid but unmatched) on a synced `S1`, when fetched,
   then the list is empty and the count is 0, with NO error
   (empty-result-not-error).
4. Given `file_type=` (empty) or `file_type=%25` (a `%`) or `file_type=a/b` or
   `file_type=a_b`, when requested over HTTP, then the handler returns 400
   bad_request and the queryer is not reached.
5. Given a sample with a `.cram` and a `.bai` object, when
   `/sample/:id/irods?file_type=cram` is fetched, then only the `.cram` object
   is returned and its count is 1.
6. Given `EXPLAIN` (MySQL) of the study iRODS file-type query, when run, then it
   is index-served via the iRODS mirror's study-scoped index (not a full scan);
   see I1.

### B3: run-scoped iRODS list + count

As a caller, I want the iRODS data objects for a run (Q2), filterable by file
type, paginated like the other iRODS lists.

`GET /run/:id/irods` (+ `GET /run/:id/irods/count`) -> `[]IRODSPath` / `Count`.
`:id` is the Illumina NPG `id_run` (resolved via `ResolveRun`; a
non-Illumina/invalid run yields the existing not-found / unsupported-identifier
error, a numeric run absent from the synced cache yields `ErrNotFound`). The
list joins the run's `iseq_product_metrics_mirror` rows (filtered by `id_run`)
to the iRODS mirror on `id_iseq_product`, returning `IRODSPath` rows (each with
`id_run` = the run, `platform`). The `file_type` filter from B2 applies.
Paginated with `X-Total-Count` / `X-Next-Offset`; the count is the same join
with no LIMIT.

**Package:** `mlwh/`
**Files:** `mlwh/hierarchy.go`, `mlwh/count.go`, `mlwh/registry.go`,
`mlwh/queryer.go`, `mlwh/server.go`, `mlwh/remote.go`, `mlwh/types.go`
**Test file:** `mlwh/hierarchy_test.go`, `mlwh/count_test.go`,
`mlwh/server_test.go`, `mlwh/remote_test.go`

**Method signatures:**

```go
IRODSPathsForRun(ctx, idRun, fileType string, limit, offset int) ([]IRODSPath, error)
CountIRODSPathsForRun(ctx, idRun, fileType string) (Count, error)
```

**Acceptance tests:**

1. Given run 52553 with 6 iRODS data objects across its products (4 `.cram`, 2
   `.bai`), when `IRODSPathsForRun("52553", "", all)` is fetched, then 6 rows
   are returned, each with `id_run=52553`.
2. Given the same run, when fetched with `file_type=cram`, then 4 rows are
   returned and `CountIRODSPathsForRun("52553", "cram")` is 4.
3. Given a request `limit=2&offset=0` with 6 matching rows, when the list
   endpoint responds, then the body is a 2-element array, `X-Total-Count: 6`,
   `X-Next-Offset: 2`.
4. Given a non-numeric run id, when called, then `ErrUnsupportedIdentifier`;
   given a numeric run absent from a synced cache, then `ErrNotFound`; given a
   never-synced cache, then an error satisfying both `ErrCacheNeverSynced` and
   `ErrNotFound`.
5. Given `CountIRODSPathsForRun("52553", "")` and `len(IRODSPathsForRun("52553",
   "", all))`, when both taken, then equal.

## C. D2 -- Study data manifest

### C1: study manifest list (one row per product) + optional iRODS path

As a user, I want one bounded, pageable table of my study's products with sample
name / supplier_name / accession / sanger_sample_id / run (and optionally the
cram iRODS path), so Q3/Q4 are one server-side join, not N calls.

`GET /study/:id/manifest` -> `StudyManifest`. The envelope carries the study
`name`/`accession_number`/`faculty_sponsor`/`data_access_group` ONCE (from
`study_mirror`), `cache_synced_at`, and a page of `ManifestRow`. The row grain
is one row per sequencing product (`iseq_product_metrics_mirror` row: distinct
`id_run, position, tag_index`) joined to its sample's identity in
`sample_mirror`, scoped by the product-metrics `id_study_lims`. Ordered by
`(id_run, position, tag_index, name)` for determinism. Paginated by
`limit/offset` (fetch-all default); `X-Total-Count` / `X-Next-Offset` set, sized
by C2's count.

When `with_irods=true`, each row also carries `irods_path`: the product's iRODS
data object via LEFT JOIN `seq_product_irods_locations_mirror` on
`id_iseq_product` (and `id_study_lims`), restricted by the D1 `file_type` filter
(default cram when `with_irods` is set without `file_type`? NO -- require an
explicit `file_type` only to filter; with `with_irods` and no `file_type` the
path is any one object for the product). A product with no matching iRODS object
has `irods_path=""`. The join is set-at-once (LEFT JOIN + GROUP BY product),
never a per-row correlated subquery (the per-platform-breakdown perf class);
proven by EXPLAIN (I1).

The never-synced / unknown-study / synced-empty cascade matches
`CountSamplesForStudy`: never-synced -> zero value + `neverSyncedReadErr()`;
unknown study -> `ErrNotFound`; synced study with no products -> an envelope
with the study metadata, an empty `rows`, and `cache_synced_at`.

`Description` states: row grain (one per product run x lane x tag); the study
metadata is in the envelope, not per row; `with_irods` + `file_type` add the
cram (or suffix) path via the run-scope/suffix semantics of D1;
bounded-by-default and pageable; the freshness caveat.

**Package:** `mlwh/`
**File:** `mlwh/manifest.go`
**Test file:** `mlwh/manifest_test.go`

**Method signature:**
`StudyManifest(ctx, studyLimsID, fileType string, withIRODS bool, limit, offset
int) (StudyManifest, error)`

**Acceptance tests:**

1. Given study `S1` (study
   `name`/`accession`/`faculty_sponsor`/`data_access_group` set) with 3 Illumina
   products across 2 samples (run/lane/tag distinct), when `StudyManifest("S1",
   "", false, all)` is called, then the envelope carries the study metadata
   once, `rows` has 3 entries each with the correct `name`, `supplier_name`,
   `accession_number`, `sanger_sample_id`, `id_run`, `lane`, `tag_index`, and no
   `irods_path`, and `cache_synced_at` is populated.
2. Given the same study with `.cram` iRODS objects for 2 of the 3 products, when
   `StudyManifest("S1", "cram", true, all)` is called, then each of the 2
   covered rows carries its `.cram` `irods_path` and the uncovered row carries
   `irods_path=""`, and the row count is still 3 (the manifest is
   product-grained, not iRODS-grained).
3. Given `limit=2&offset=0` with 3 products, when the manifest endpoint
   responds, then `rows` has 2 entries, `X-Total-Count: 3`, `X-Next-Offset: 2`.
4. Given a never-synced cache, then an error satisfying both
   `ErrCacheNeverSynced` and `ErrNotFound`; given an unknown study, then
   `ErrNotFound`; given a synced study with no products, then an envelope with
   the study metadata, empty `rows`, and `cache_synced_at` populated.
5. Given `EXPLAIN` (MySQL) of the manifest query (with and without
   `with_irods`), when run, then it is index-served (the product-metrics study
   index and the iRODS `id_iseq_product` index), with no full scan of either
   9M-row mirror; see I1.

### C2: study manifest count

As a caller, I want to size the manifest before transfer.

`GET /study/:id/manifest/count` -> `Count` = the distinct `(id_run, position,
tag_index)` products in the study (the manifest row grain), via `COUNT(*)` over
the same `SELECT DISTINCT` with no LIMIT, so count == len(rows-all). The
`file_type` / `with_irods` params do NOT change the count (the manifest is
product-grained; a product with no matching iRODS object is still a row).
Cascade matches `CountSamplesForStudy`.

**Package:** `mlwh/`
**File:** `mlwh/count.go`
**Test file:** `mlwh/count_test.go`

**Method signature:** `CountStudyManifest(ctx, studyLimsID string) (Count,
error)`

**Acceptance tests:**

1. Given `S1` with 3 distinct products, when `CountStudyManifest("S1")` is
   called, then `Count{3}`, equal to
   `len(StudyManifest("S1","",false,all).Rows)`.
2. Given a never-synced cache, then both sentinels; an unknown study ->
   `ErrNotFound`; a synced study with no products -> `Count{0}` no error.

## D. D3 -- Study QC counts on StatusBreakdown

### D1q: study-level qc_pass / qc_fail / qc_pending over sequenced samples

As a study owner, I want received / sequenced / passed-manual-QC answerable in
one cheap call, by extending `StatusBreakdown` (no new endpoint, per the settled
Notes).

Extend `StatusBreakdown` to fill `QC StudyQCBreakdown` (the existing `distinct`,
`per_platform`, `with_detailed_timeline`, `cache_synced_at` are unchanged, so
received = `samples_total`, sequenced = `samples_total - distinct.registered`,
not-sequenced = `distinct.registered`). Add ONE grouped query that, over the
study's distinct sequenced samples, computes each sample's QC roll-up and
buckets it:

- Inner per-sample roll-up: for each distinct `id_sample_tmp` linked to the
  study with >=1 study-scoped product-metrics row (any platform via
  `studyScopedProductMetricsExists` membership), aggregate the overall `qc`
  across that sample's products IN THIS STUDY using the same logic as
  `rollUpSampleQC`: `MIN(qc)=0` -> fail; else any `qc IS NULL` -> pending; else
  pass. Implement as a single grouped query: a UNION ALL of each platform's
  product-metrics mirror scoped by `id_study_lims` and `id_sample_tmp`, grouped
  by sample, with conditional aggregation, then an outer count by bucket.
  (Study-scoped, unlike the sample-wide `sampleQCRollupSQL`, so the study counts
  reflect only this study's products -- but the roll-up rule is identical, so a
  sample's study QC verdict cannot disagree with `SampleProgress.qc` for a
  single-study sample.)
- `qc_pass + qc_fail + qc_pending == sequenced` (= `samples_total -
  distinct.registered`). not_tracked / registered-only / ONT samples have no
  products and are excluded from the QC split (they are the not-sequenced
  bucket), never a false zero.

Reuse `statusBreakdownFeedingTables` (already includes the product-metrics
mirrors). Per-platform QC counts are DEFERRED (not added now); the response
shape leaves room (a future `per_platform[i].qc`) without breaking.

`Description`: state that received = `samples_total`, sequenced = `samples_total
- registered`, not-sequenced = `registered`; that `qc` is the QC split of the
SEQUENCED distinct samples using the same per-sample roll-up as
`/sample/:id/progress` (fail > pending > pass over `iseq_product_metrics.qc`
1/0/NULL), summing to sequenced; that not_tracked/ONT samples are not-sequenced
and excluded from the QC split; and the freshness caveat.

**Package:** `mlwh/`
**File:** `mlwh/progress.go`
**Test file:** `mlwh/progress_test.go`

**Method signature:** unchanged (`StatusBreakdown(ctx, studyLimsID string)
(StatusBreakdown, error)`).

**Acceptance tests:**

1. Given study `S1` with: sample A delivered with all products `qc=1`; sample B
   sequenced with one product `qc=1` and one `qc=0`; sample C sequenced with one
   product `qc` NULL; sample D registered-only (no products); sample E ONT (no
   products), when `StatusBreakdown("S1")` is called, then
   `distinct.registered=2` (D, E), sequenced = `samples_total-2 = 3`, and `qc ==
   {qc_pass:1, qc_fail:1, qc_pending:1}` summing to 3.
2. Given the same study, when the QC sub-struct is summed, then
   `qc_pass+qc_fail+qc_pending == samples_total - distinct.registered` (the
   sequenced count).
3. Given a sample with two products in `S1`, one `qc=0` and one `qc=1`, when
   counted, then it lands in `qc_fail` (fail > pass), matching
   `SampleProgress.qc` for that sample.
4. Given an ONT-only sample linked to `S1`, when counted, then it is in
   `distinct.registered` and is NOT in any QC bucket (never a false
   `qc_pending`).
5. Given a never-synced cache, then both sentinels; an unknown study ->
   `ErrNotFound`; a synced study with no samples -> all-zero ladders AND `qc ==
   {0,0,0}` with `cache_synced_at` populated.
6. Given the existing `StatusBreakdown` regression tests (distinct /
   per_platform / with_detailed_timeline), when run, then they still pass
   unchanged (the QC field is additive).

## E. D4 -- People to studies

### E1: studies by faculty sponsor

As a user, I want the studies of a named PI/sponsor, with the total visible
(Q7).

`GET /studies/faculty-sponsor/:name` (+ `GET
/studies/faculty-sponsor/:name/count`) -> `[]PersonStudy` / `Count`. Matches
`study_mirror.faculty_sponsor` containing the
term, case-insensitive substring (`LOWER(faculty_sponsor) LIKE '%'||LOWER(?)||'%'`),
filtered to `id_lims='SQSCP'`. Each row's `Study` is the full study; `Role` is
empty (sponsor is not a `study_users` role). Paginated
(`searchPaginationParams`: default 100, max 1000) with `X-Total-Count` /
`X-Next-Offset`; the count is the same match with no LIMIT. A whitespace-only
`:name` (after trim) -> `ErrUnsupportedIdentifier` (400). Ordered by
`id_study_lims`.

`Description`: state this matches the named PI/SPONSOR (`study.faculty_sponsor`,
free-text), case-insensitive substring; that it is DISTINCT from
`/studies/user/:person` (role membership); and the freshness caveat. The
never-synced cache returns an empty list + `neverSyncedReadErr()`; a synced
cache with no match returns an empty list (no error).

**Package:** `mlwh/`
**File:** `mlwh/people.go`
**Test file:** `mlwh/people_test.go`

**Method signatures:**
```go
StudiesForFacultySponsor(ctx, name string, limit, offset int) ([]PersonStudy, error)
CountStudiesForFacultySponsor(ctx, name string) (Count, error)
```

**Acceptance tests:**

1. Given studies with `faculty_sponsor` "Carl Anderson" (2), "carl anderson"
   (1), "Jane Doe" (1), when `StudiesForFacultySponsor("carl", all)` is called,
   then it returns the 3 Carl studies (case-insensitive substring), each
   `PersonStudy` with the full `Study` and empty `Role`;
   `CountStudiesForFacultySponsor("carl")` is 3.
2. Given `limit=2&offset=0` with 3 matches, when the list endpoint responds,
   then 2 rows, `X-Total-Count: 3`, `X-Next-Offset: 2`.
3. Given a whitespace-only name over HTTP, then 400 bad_request.
4. Given a never-synced cache, then an empty list + an error satisfying both
   `ErrCacheNeverSynced` and `ErrNotFound`; a synced cache with no match ->
   empty list, no error.

### E2: studies by user (role-filtered)

As a user, I want "my studies" by my login/email/name with the right roles (Q7).

`GET /studies/user/:person` (+ `GET /studies/user/:person/count`) ->
`[]PersonStudy` / `Count`. Matches `study_users_mirror` where `person` is a
case-insensitive substring of `name` OR `login` OR `email`, joined to
`study_mirror` on `id_study_tmp` (filtered `study_mirror.id_lims='SQSCP'`).
DEFAULT role filter: `role IN ('owner','manager','data_access_contact')`. An
optional `role=` query param (comma-separated) overrides the default set; each
value is matched exactly (case-insensitive). Each row's `Role` is the matched
`study_users` role; the SAME study may appear under multiple roles --
de-duplicate to one row per `(id_study_lims, role)` and order by
`(id_study_lims, role)`. Paginated (`searchPaginationParams`) with sizing
headers; the count is the distinct `(id_study_lims, role)` matches with no
LIMIT. A whitespace-only `:person` -> 400.

`Description`: state this matches `study_users` ROLE MEMBERSHIP (distinct from
the faculty_sponsor endpoint); that the match is case-insensitive substring
across `name`, `login` AND `email` (so an email/login or a name both resolve,
and a caller given only an email does not get a false empty); the DEFAULT role
set (`owner`, `manager`, `data_access_contact`) and that `role=` widens it (e.g.
to include `follower`); that each row surfaces the matched `role`; and the
freshness caveat. Cascade as E1 (empty list + neverSyncedReadErr on
never-synced; empty list no-error on synced-no-match).

**Package:** `mlwh/`
**File:** `mlwh/people.go`
**Test file:** `mlwh/people_test.go`

**Method signatures:**
```go
StudiesForUser(ctx, person, role string, limit, offset int) ([]PersonStudy, error)
CountStudiesForUser(ctx, person, role string) (Count, error)
```
(`role` is the raw comma-separated override, "" for the default set; the handler
passes the `role` query param through.)

**Acceptance tests:**

1. Given `study_users_mirror` rows linking person login "ca3" / email
   "ca3@sanger.ac.uk" / name "Carl Anderson": owner of studies X,Y; manager of
   Z; follower of W, and a `study_mirror` for X,Y,Z,W, when
   `StudiesForUser("ca3", "", all)` is called, then it returns X,Y,Z (owner /
   manager are in the default set) but NOT W (follower excluded), each
   `PersonStudy` with the matched `Role`; `CountStudiesForUser("ca3", "")` is 3.
2. Given the same data, when `StudiesForUser("ca3@sanger.ac.uk", "", all)` is
   called (by email) and `StudiesForUser("anderson", "", all)` (by name
   substring), then both return the same 3 studies (match across
   login/email/name).
3. Given `role=follower`, when `StudiesForUser("ca3", "follower", all)` is
   called, then it returns W (the override replaces the default set);
   `CountStudiesForUser("ca3", "follower")` is 1.
4. Given a person who is BOTH owner and data_access_contact of study X, when
   listed with the default roles, then X appears twice -- once with
   `role=owner`, once with `role=data_access_contact` -- and the count is 2
   (distinct `(study, role)`).
5. Given a whitespace-only person over HTTP, then 400 bad_request.
6. Given a never-synced cache, then empty list + both sentinels; a synced cache
   with no match -> empty list, no error.

### E3: resolve-person directory

As a caller, I want to translate a partial/spoken name into the exact stored
form(s) and disambiguate among several people before running a studies query
(Q7).

`GET /resolve-person/:term` (+ `GET /resolve-person/:term/count`) ->
`[]PersonCandidate` / `Count`. Given a partial term (case-insensitive), returns
the DISTINCT candidate people, both:

- from `study_mirror`: distinct `faculty_sponsor` values containing the term
  (`Source="faculty_sponsor"`, `Name`=the sponsor text, `Login`/`Email`/`Role`
  empty), with `StudyCount` = distinct SQSCP studies for that sponsor; and
- from `study_users_mirror`: distinct `(name, login, email, role)` where the
  term is a substring of `name`/`login`/`email` (`Source="study_users"`), with
  `StudyCount` = distinct studies for that `(login, role)`.

Bounded/pageable (`searchPaginationParams`, default 100, max 1000) with sizing
headers; ordered by `(source, name, login, role)` for determinism. The count is
the distinct candidate count (both sources). A whitespace-only `:term` -> 400.

`Description`: state the stored forms (faculty_sponsor is free-text full names;
study_users identifies a person by `name` AND `login` (Sanger username) AND
`email`); the match-across-name/login/email behaviour; and the routing guidance:
"if a narrow term yields nothing or is ambiguous, enumerate candidates here
rather than dead-ending, then use /studies/faculty-sponsor or /studies/user with
the chosen stored form". Cascade as E1/E2.

**Package:** `mlwh/`
**File:** `mlwh/people.go`
**Test file:** `mlwh/people_test.go`

**Method signatures:**
```go
ResolvePerson(ctx, term string, limit, offset int) ([]PersonCandidate, error)
CountResolvePerson(ctx, term string) (Count, error)
```

**Acceptance tests:**

1. Given `study_mirror` with `faculty_sponsor` "Carl Anderson" on 91 studies
   (and a `study_users` row name "Carl Anderson"/login "ca3"/email
   "ca3@sanger.ac.uk"/role owner on 59 of them), when `ResolvePerson("carl",
   all)` is called, then it returns a `faculty_sponsor` candidate (`Name="Carl
   Anderson"`, `StudyCount=91`) AND a `study_users` candidate (`Name="Carl
   Anderson"`, `Login="ca3"`, `Email="ca3@sanger.ac.uk"`, `Role="owner"`,
   `StudyCount=59`).
2. Given `ResolvePerson("ca3", all)` (by login fragment), then the `study_users`
   candidate is returned (match across login), enabling translation from a login
   to the stored name.
3. Given two distinct sponsors "Carl Anderson" and "Carla Anders", when
   `ResolvePerson("carl", all)` is called, then both faculty_sponsor candidates
   appear (disambiguation), and `CountResolvePerson("carl")` counts every
   distinct candidate.
4. Given a whitespace-only term over HTTP, then 400 bad_request.
5. Given a never-synced cache, then empty list + both sentinels; a synced cache
   with no match -> empty list, no error.

## F. D5 -- Cheap study-metadata exposure

### F1: study metadata on the overview + disambiguation on search

As a caller, I want the data access groups (and sponsor/name/accession) from a
cheap study call (Q1) and enough on search rows to disambiguate a study name
(Q5).

- Surface `name`, `accession_number`, `faculty_sponsor`, `data_access_group` on
  `StudyOverview` (read from `study_mirror` in the existing `StudyOverview`
  path: add a single-row select of those columns, or reuse
  `resolveStudyFromCache` before building the overview). They are populated on
  the empty-study path too (the study exists; only its samples are zero).
  `Description` adds that the overview now carries the study's
  `data_access_group` (and name / accession / faculty_sponsor) so "data access
  groups for study X" is one small call without the giant `/study/:id/detail`.
- `SearchStudies` already returns full `Study` rows (which carry
  `id_study_lims`, `name`, `faculty_sponsor`). No SQL change is required;
  CONFIRM via a test that a search row carries `id_study_lims`, `name`, and
  `faculty_sponsor` so an ambiguous name (Q5) is disambiguable, and add the
  disambiguation note to the `SearchStudies` `Description`.

**Package:** `mlwh/`
**Files:** `mlwh/availability.go`, `mlwh/types.go`, `mlwh/registry.go`
**Test file:** `mlwh/availability_test.go`, `mlwh/search_test.go`

**Acceptance tests:**

1. Given study `S1` with `name`, `accession_number`, `faculty_sponsor`,
   `data_access_group` set and 5 linked samples, when `StudyOverview("S1")` is
   called, then those four fields are populated alongside the existing counts.
2. Given a synced study `S1` that exists but has zero linked samples, when
   `StudyOverview("S1")` is called, then the four metadata fields are still
   populated (read from `study_mirror`), the counts are 0, and `cache_synced_at`
   is populated.
3. Given two studies named "Malaria" with different `id_study_lims` and
   `faculty_sponsor`, when `SearchStudies("malaria", all)` is called, then each
   result row carries its distinct `id_study_lims`, `name`, and
   `faculty_sponsor` (Q5 disambiguation).

## G. Registry, remote, server wiring

### G1: Registry entries + handler cases + remote methods + Queryer members

As an implementor, I want every new endpoint wired through the four-step recipe
so local and remote surfaces stay aligned and self-describing.

For each new endpoint add: the `Queryer` member (`queryer.go`), the `Client`
method (its file), the `RemoteClient` method (+ `Page[T]` variant for new
paginated lists: `IRODSPathsForRunPage`, `StudiesForFacultySponsorPage`,
`StudiesForUserPage`, `ResolvePersonPage`; the manifest is an envelope, not a
bare slice, so it uses the plain `remoteCall`), the `Registry` `Endpoint`
(Summary + verbatim-for-MCP Description with the definitions above), and the
`server.go` handler `case`. New paginated entries declare `QueryParams`
(`fetchAllPaginationParams()` for run iRODS / manifest;
`searchPaginationParams()` for the people lists; plus the `file_type` /
`with_irods` / `role` query params). Bump `APIVersion` to 1.7.0.

New endpoint paths (settled):

| Method                          | Verb | Path                                      |
| ------------------------------- | ---- | ----------------------------------------- |
| `IRODSPathsForRun`              | GET  | `/run/:id/irods`                          |
| `CountIRODSPathsForRun`         | GET  | `/run/:id/irods/count`                    |
| `StudyManifest`                 | GET  | `/study/:id/manifest`                     |
| `CountStudyManifest`            | GET  | `/study/:id/manifest/count`               |
| `StudiesForFacultySponsor`      | GET  | `/studies/faculty-sponsor/:name`          |
| `CountStudiesForFacultySponsor` | GET  | `/studies/faculty-sponsor/:name/count`    |
| `StudiesForUser`                | GET  | `/studies/user/:person`                   |
| `CountStudiesForUser`           | GET  | `/studies/user/:person/count`             |
| `ResolvePerson`                 | GET  | `/resolve-person/:term`                   |
| `CountResolvePerson`            | GET  | `/resolve-person/:term/count`             |

`file_type` is added as a `Query` param to the existing
`IRODSPathsForStudy`/`IRODSPathsForSample` (and their `/count`) entries plus the
new `IRODSPathsForRun`; `with_irods`+`file_type` to `StudyManifest`; `role` to
`StudiesForUser`. The existing `IRODSPathsForStudy`/`IRODSPathsForSample` keep
their `Method` names (file-type is an additive query param on the same
endpoint).

The closed-set regression test `newAvailabilityRecencyProgressMethods()`
(`registry_test.go`) is EXTENDED with the new Method names so the "new methods
exist + are documented + paginated entries declare limit/offset" guard covers
them (preserve the existing list; add the new entries).

**Package:** `mlwh/`
**Files:** `mlwh/registry.go`, `mlwh/queryer.go`, `mlwh/server.go`,
`mlwh/remote.go`, `mlwh/openapi.go`
**Test file:** `mlwh/registry_test.go`, `mlwh/server_test.go`,
`mlwh/remote_test.go`

**Acceptance tests:**

1. Given the Registry, when iterated, then every new Method has a non-empty
   `Summary` and `Description`, every new paginated entry declares integer
   limit/offset `QueryParams`, and `TestRegistryCoversQueryer` passes (every
   Registry Method has a `Queryer` member and vice versa).
2. Given each new endpoint, when its `server.go` handler runs against a seeded
   cache, then it returns the same typed value as the `Client` method (the
   switch has a case for every new Method; no panic), and a paginated list sets
   `X-Total-Count` / `X-Next-Offset`.
3. Given each new endpoint, when called via `RemoteClient` against a test
   server, then it round-trips to the same typed result as the local `Client`;
   the new `Page[T]` variants return `Total` / `NextOffset` matching the
   headers.
4. Given `APIVersion`, when read, then it equals "1.7.0".
5. Given `TestRegistryRecencyDescriptionsCiteCreationTimestampG1`, when run,
   then it still passes (any new Description mentioning the iRODS created column
   also carries the never-last_updated/last_run clause).

### G2: regenerate docs; drift guards green; glossary updated

As a maintainer, I want the generated docs refreshed and the glossary extended
so the MCP surface and drift guards stay correct.

Run `WA_REFRESH_DOCS=1 go test ./mlwh -run TestWriteEndpointReference` to
rewrite `.docs/mcp/api-reference.md`. Add glossary entries
(`.docs/mcp/glossary.md`) for "data manifest", "file-type filter (filename
suffix)", "faculty sponsor", "study_users / role membership", "manual QC", and
"data access group".

**Package:** `mlwh/`
**Files:** `.docs/mcp/api-reference.md`, `.docs/mcp/glossary.md`
**Test file:** `mlwh/docs_test.go`

**Acceptance tests:**

1. Given the regenerated reference and the OpenAPI document, when
   `TestEndpointReferenceAndOpenAPICoverSamePathsG1` runs, then both cover the
   same set of Registry paths (no drift).
2. Given the committed reference, when compared to `EndpointReference()`
   (`TestEndpointReferenceMatchesCommittedDocumentG1`), then they match.
3. Given the glossary, when inspected, then it defines "data manifest" and
   "file-type filter".

## H. CLI exposure (REQUIRED; HARD REQ 6)

Every new endpoint's results are reachable from `wa mlwh`, in BOTH local-cache
and `--server` modes, with graceful degradation (not-found/empty/not-tracked
render cleanly, exit 0), mirroring `wa mlwh info`/`search`. Each command opens
its client via the existing `openMLWHInfoConfiguredClient` shape (server URL ->
RemoteClient, else local cache via `resolveMLWHInfoLocalConfig`), supports
`--json`, and follows the `mlwhInfoClient`/`mlwhSearchClient` interface-subset
pattern.

### H1: extend `wa mlwh info`

As a user, I want study QC counts and data-access groups on `info <study>`, and
run-scoped iRODS on `info <run>`.

- `info <study>`: the existing study section already prints the overview and
  status breakdown; surface the new fields: print the overview's
  `data_access_group` / `faculty_sponsor` / `name` / `accession_number` (D5),
  and the breakdown's `qc` (qc_pass/qc_fail/qc_pending) and derived
  received/sequenced/not-sequenced (D3). Add the fields to `mlwhInfoClient` only
  if new methods are needed (the existing `StudyOverview`/`StatusBreakdown`
  already carry them, so no interface change beyond the new struct fields).
- `info <run>`: add a run-scoped iRODS section (D1), summarised/limited
  (`infoMaxRelated`), via a new `mlwhInfoClient` method `IRODSPathsForRun(ctx,
  idRun, "", limit, offset)`. Render path + `id_run` + `platform`; an empty
  result prints "none" (exit 0).

**Package:** `cmd/`
**Files:** `cmd/mlwh_info.go`
**Test file:** `cmd/mlwh_info_test.go`

**Acceptance tests:**

1. Given a fake client returning a `StatusBreakdown` with `qc={pass:40012,
   fail:200,pending:583}` and `distinct.registered=4482` over
   `samples_total=45277`, when `wa mlwh info 7699 --type study` runs, then the
   output shows received 45277, sequenced 40795, not-sequenced 4482, and qc_pass
   40012 / qc_fail 200 / qc_pending 583.
2. Given a study overview carrying `data_access_group="grp-1"`, when `info
   <study>` runs, then the output shows the data access group.
3. Given a run resolving to 52553 with 6 iRODS objects, when `info 52553 --type
   run` runs, then a run iRODS section lists the (capped) paths with
   `id_run`/`platform`; given a run with no iRODS objects, the section renders
   "none" and the command exits 0.
4. Given a never-synced cache in `--server` mode, when `info <study>` runs, then
   it degrades gracefully (neutral message, no `wa mlwh sync` hint, exit 0 on
   the resolve-not-found path as today).

### H2: `wa mlwh irods` subcommand

As a user, I want iRODS paths for a study/run/sample filtered by file type from
the CLI (D1), since this is not "info about one identifier".

`wa mlwh irods <study|run|sample> <identifier> [--file-type cram]
[--limit N --offset M] [--server URL] [--json]`. The first positional selects
the scope (or auto-detect by resolving). Dispatches to
`IRODSPathsForStudyByFileType` / `IRODSPathsForRun` /
`IRODSPathsForSampleByFileType`. Tabular text output (path, id_run, platform);
`--json` emits the `[]IRODSPath`. An empty/unmatched result prints a "no
matching iRODS paths" line and exits 0; a never-synced cache degrades like
`info`.

**Package:** `cmd/`
**Files:** `cmd/mlwh_irods.go`, wired in `cmd/mlwh.go`
**Test file:** `cmd/mlwh_irods_test.go`

**Acceptance tests:**

1. Given a fake client returning 2 `.cram` paths for study `S1`, when `wa mlwh
   irods study S1 --file-type cram` runs, then both paths print with their
   `id_run`/`platform`, exit 0.
2. Given `wa mlwh irods run 52553 --file-type bam` returning an empty list, when
   run, then a "no matching iRODS paths" message prints and the command exits 0.
3. Given `--json`, when run, then a single JSON array of `IRODSPath` is emitted.
4. Given an invalid file type (`--file-type a/b`) the client/handler returns a
   bad-request-class error; the CLI prints a clear message and exits non-zero
   (input error, not a degradation).

### H3: `wa mlwh manifest` subcommand

As a user, I want the study manifest from the CLI (D2/Q3/Q4).

`wa mlwh manifest <study> [--with-irods --file-type cram] [--limit N --offset M]
[--server URL] [--json]`. Dispatches to `StudyManifest`. Tabular text: a header
line with the study metadata (name / accession / faculty_sponsor /
data_access_group) once, then one line per row (`name`, `supplier_name`,
`accession_number`, `sanger_sample_id`, `id_run`, `lane`, `tag_index`, and
`irods_path` when `--with-irods`). Honours paging; prints `X-Total-Count`-style
total if available. Empty `rows` prints the header + "no products"; exit 0.

**Package:** `cmd/`
**Files:** `cmd/mlwh_manifest.go`, wired in `cmd/mlwh.go`
**Test file:** `cmd/mlwh_manifest_test.go`

**Acceptance tests:**

1. Given a fake client returning a `StudyManifest` with study metadata and 3
   rows, when `wa mlwh manifest S1` runs, then the study metadata prints once
   and 3 row lines print with the per-row fields, exit 0.
2. Given `--with-irods --file-type cram` and rows carrying `irods_path`, when
   run, then each row line includes its `irods_path` (empty rendered as a
   placeholder like `-`).
3. Given `--json`, when run, then a single `StudyManifest` JSON object is
   emitted.
4. Given a synced study with no products, when run, then the header prints with
   "no products" and the command exits 0; a never-synced cache degrades
   gracefully.

### H4: `wa mlwh studies` and `wa mlwh people` subcommands

As a user, I want people-to-studies and person resolution from the CLI (D4/Q7).

- `wa mlwh studies --faculty-sponsor "<name>"` -> `StudiesForFacultySponsor`;
  `wa mlwh studies --user <login> [--role owner,manager]` -> `StudiesForUser`.
  Exactly one of `--faculty-sponsor` / `--user` is required (error if
  both/neither). Text output: one line per study (`id_study_lims`, `name`,
  `faculty_sponsor`, and `role` for the user mode); print the total count.
  `--json` emits `[]PersonStudy`.
- `wa mlwh people <term>` -> `ResolvePerson`: one line per candidate (`source`,
  `name`, `login`, `email`, `role`, `study_count`); `--json` emits
  `[]PersonCandidate`. Empty results print "no matches"; exit 0.

Both follow the `--server`/local + graceful-degradation pattern.

**Package:** `cmd/`
**Files:** `cmd/mlwh_studies.go`, wired in `cmd/mlwh.go`
**Test file:** `cmd/mlwh_studies_test.go`

**Acceptance tests:**

1. Given a fake client where `StudiesForFacultySponsor("carl")` returns 3
   studies, when `wa mlwh studies --faculty-sponsor carl` runs, then 3 study
   lines print with the total (3) and `faculty_sponsor`, exit 0.
2. Given `StudiesForUser("ca3", "owner,manager")` returns X,Y,Z, when `wa mlwh
   studies --user ca3 --role owner,manager` runs, then 3 lines print each with
   its `role`, exit 0.
3. Given neither/both of `--faculty-sponsor`/`--user`, when run, then a clear
   usage error and non-zero exit.
4. Given `ResolvePerson("carl")` returns a faculty_sponsor candidate and a
   study_users candidate, when `wa mlwh people carl` runs, then both candidate
   lines print with their `source` / stored form / `study_count`, exit 0.
5. Given a never-synced cache via `--server`, when any of these runs, then it
   degrades gracefully (neutral "cache not available" message, no sync hint,
   exit 0).

## I. Real-MySQL and source integration tests (HARD REQ 7)

### I1: real-MySQL integration test of the new query paths (EXPLAIN-proven)

As a maintainer, I want the new query paths to execute and be index-served on
MySQL, not only SQLite, following `cache_mysql_integration_test.go`.

Add tests that (skipping when `WA_MLWH_CACHE_PATH` is absent;
`realMySQLCacheDSNOrSkip` + `createThrowawayMySQLCacheDBOrSkip` with `t.Cleanup`
dropping the throwaway DB on success and failure) build the schema in a
throwaway MySQL DB, seed a scenario (the J1 seed), and assert:

- `IRODSPathsForRun` / `CountIRODSPathsForRun` (with and without `file_type`)
  return the correct rows/counts on MySQL.
- `StudyManifest` / `CountStudyManifest` (with and without
  `with_irods`+`file_type`) return the correct rows/counts on MySQL.
- `StatusBreakdown.QC` returns the correct qc_pass/qc_fail/qc_pending on MySQL.
- `StudiesForFacultySponsor`, `StudiesForUser`, `ResolvePerson` return the
  correct rows/counts on MySQL.
- Via `EXPLAIN` (the `explainRunsForStudy` / `mysqlExplainRow` helper pattern):
  the run-scoped iRODS query, the manifest query, and the file-type-filtered
  study iRODS query are index-served -- the chosen `key` is a real index and the
  scan `type` is not `ALL` (no full scan of the 9M-row iRODS or product-metrics
  mirrors); the `/studies/user` query uses a `study_users_mirror` lookup index.

**Package:** `mlwh/`
**File:** `mlwh/cache_mysql_integration_test.go`
**Test file:** same

**Acceptance tests:**

1. Given a throwaway MySQL cache seeded with the J1 scenario, when each new
   query path runs on MySQL, then it returns the same counts/rows the SQLite
   tests assert.
2. Given `EXPLAIN` of the run-scoped iRODS, manifest, and file-type-filtered
   study iRODS queries on MySQL, when inspected, then each `key` is a non-empty
   real index and `LOWER(type) != "all"` (index-served, no full scan).
3. Given `EXPLAIN` of `/studies/user` on MySQL, when inspected, then it is
   served by a `study_users_mirror` index (login/email/name or id_study_tmp),
   not a full scan.

### I2: source integration test for the new source columns/tables

As a maintainer, I want the schema the other tests assume to stay true against
the real source, following `sync_source_integration_test.go`.

Extend the source-schema test (skipping when `WA_MLWH_DSN` is absent;
`openRealMLWHSourceOrSkip`) so `AllSyncSourceQueries()` includes the new
`study_users` wholesale source SELECT, and `supportedSyncTables` /
`wholesaleMirrorTables()` include `study_users`, so the generic
`prepareAndCloseSourceQuery` validator PREPAREs the `study_users` query against
the real source (proving `study_users` exists with `id_study_users_tmp`,
`id_study_tmp`, `role`, `login`, `email`, `name`, `last_updated`). Add a probe
SELECT asserting `study.faculty_sponsor`, `study.data_access_group`, and
`iseq_product_metrics.qc` exist (a `PrepareContext` of a SELECT naming those
columns).

**Package:** `mlwh/`
**File:** `mlwh/sync_source_integration_test.go`
**Test file:** same

**Acceptance tests:**

1. Given a live source connection, when every sync source query (including the
   new `study_users` query) is PREPAREd, then each validates (no missing
   table/column), and `study_users` is covered by the supported-tables check.
2. Given a probe SELECT of `study.faculty_sponsor`, `study.data_access_group`,
   `iseq_product_metrics.qc`, when PREPAREd against the real source, then it
   succeeds (the columns exist).

## J. Testing strategy and the shared scenario seed

### J1: hermetic GoConvey scenario seed

Seed an ephemeral SQLite cache (`openSQLiteSyncTestCache`) via the existing
helpers, extended for the new columns/tables:

- `seedStudyMirrorRow` (extend or add a variant) to set `faculty_sponsor`,
  `data_access_group`, `name`, `accession_number` on study `S1`.
- `seedHierarchySample` / `seedLibrarySample` /
  `seedIseqProductMetricsMirrorRow` (and `...WithQC` from `progress_test.go`)
  for samples A-E with the QC mix of D1q.1 (all-pass, mixed-pass/fail,
  NULL-pending, registered-only, ONT via `seedOseqFlowcellMirrorRow`).
- `seedIRODSLocationMirrorRowWithCreatedPlatform` for `.cram`/`.bai` objects
  whose `id_iseq_product` matches the product-metrics rows (so the run-scope
  join, id_run/platform, file-type filter, and manifest iRODS column all
  resolve), incl. one Illumina (id_run derivable) and one non-Illumina (id_run
  0).
- a new `seedStudyUsersMirrorRow(t, db, idStudyUsersTmp, idStudyTmp int64, role,
  login, email, name string)` and study rows with `id_study_tmp` matching, for
  the D4 person scenarios (owner/manager/data_access_contact/follower; a person
  who is both owner and data_access_contact of one study; an email/login/name
  variant).
- `seedSyncStateRun` (MySQL-compatible) / `seedSyncState` for the feeding tables
  and `study_users`.

Reuse the count<->list cross-check (`count == len(list-all)`) for every new
count (run iRODS, manifest, faculty-sponsor, user, resolve-person). Never put
`So()` in loops > 20 iterations; count and assert the final count.

### J2: regression preservation

All existing tests must still pass. Specifically preserve: the `StatusBreakdown`
distinct/per_platform/with_detailed_timeline tests (D3 only adds `qc`); the
`IRODSPathsForStudy`/`...Sample` tests (B1 only adds `id_run`/`platform`,
unchanged grain; B2's file-type is an additive param defaulting to all rows);
the `StudyOverview` tests (D5 adds metadata fields); the cross-dialect schema
shape test (A1/A2 add to both dialects); the count<->list cross-checks; the
recency-Description guard; `TestRegistryCoversQueryer`,
`TestRegistryNewEndpointsAreFullyDocumentedG1` (extended set), and the
docs/OpenAPI drift guards (after regeneration).

## Implementation Order

Each phase builds on tested foundations from prior phases.

1. **Phase 1 -- Schema + sync foundation (A1-A5).** `study_users_mirror` table +
   indexes (both dialects); `faculty_sponsor` + iRODS `id_iseq_product` indexes
   (both dialects); `study_users` wholesale sync + freshness; sparse-read
   decision; `CacheSchemaVersion` 10 -> 11 (full resync). Sequential (everything
   downstream reads these tables/indexes).
2. **Phase 2 -- D1 iRODS (B1-B3).** `id_run`/`platform` on iRODS rows; file-type
   filter on study/sample/run iRODS lists + counts; `/run/:id/irods` (+count).
   Depends on Phase 1 (the iRODS `id_iseq_product` index).
3. **Phase 3 -- D2 manifest (C1-C2).** Study manifest list + count, with the
   optional iRODS-path column reusing D1's run-scope/file-type linkage. Depends
   on Phase 1 and Phase 2 (the iRODS join/index).
4. **Phase 4 -- D3 QC counts (D1q) + D5 metadata (F1).** Extend
   `StatusBreakdown` with the QC split; surface study metadata on
   `StudyOverview` and confirm search disambiguation. Depends on Phase 1;
   independent of Phases 2-3 (parallel after Phase 1).
5. **Phase 5 -- D4 people (E1-E3).** Faculty-sponsor, user (role-filtered), and
   resolve-person endpoints + counts. Depends on Phase 1 (`study_users_mirror`,
   `faculty_sponsor` index).
6. **Phase 6 -- Wiring + CLI + docs (G1-G2, H1-H4).** Registry/handler/remote
   wiring is done incrementally per endpoint within Phases 2-5; this phase
   finishes the `APIVersion` bump, the CLI subcommands/extensions, doc
   regeneration, glossary, and drift-guard verification.
7. **Phase 7 -- Integration tests (I1-I2).** Real-MySQL EXPLAIN-proven tests and
   the source-schema test for the new columns/tables. Depends on all prior
   phases.

## Appendix: Key Decisions

- **Why extend `StatusBreakdown`, not a new `/study/:id/qc-breakdown`.** Settled
  in the Notes: a single call already returns received / sequenced /
  not-sequenced via the existing `distinct` ladder; adding `qc` there keeps it
  one call and reuses the feeding-tables/freshness machinery. Per-platform QC is
  deferred; the additive sub-struct leaves room for it.
- **Why the QC roll-up is study-scoped but rule-identical.** The study counts
  must reflect this study's products, but the fail > pending > pass rule is the
  SAME as `rollUpSampleQC`, so a single-study sample's study QC verdict cannot
  disagree with `SampleProgress.qc`. Tested by D1q.3.
- **Why `id_run` is a read-side LEFT JOIN, not a synced column.** The iRODS
  source has no `id_run`; the Notes settle `0` = not derivable via LEFT JOIN
  `id_iseq_product -> iseq_product_metrics_mirror.id_run`, matching the existing
  "0 for non-Illumina" convention. No sync change; `platform` is already
  mirrored.
- **Why a filename-suffix file-type filter with empty-result-not-error.**
  Settled: no file-type column exists; an open suffix match (`LIKE '%.<token>'`)
  is the only truthful option. 400 is reserved for empty/whitespace or `% _ /`
  (SQL-wildcard / path) values; a valid-but-unmatched suffix is genuinely "no
  such files", an empty result, and `/count` honours the same filter so it is
  distinguishable from "no data".
- **Why two people endpoints + a directory, not one mode param.** Settled:
  separate endpoints carry their own Description so the
  faculty_sponsor-vs-study_users routing is self-documenting and the MCP layer
  cannot mis-set a mode. The directory lets a caller translate a partial/spoken
  name into the exact stored form before querying.
- **Why default roles owner/manager/data_access_contact.** Settled: these are
  the substantive "responsible-for" roles; `follower` and the operational roles
  are noisy and excluded unless `role=` widens. Each row surfaces the matched
  role so a caller sees why a study matched.
- **Why `study_users` is a wholesale mirror.** It is a small-to-medium reference
  table linked to studies by `id_study_tmp`; the `oseq_flowcell_mirror`
  wholesale pattern (INNER JOIN to `study` for SQSCP scoping, rebuilt each sync)
  is the simplest parity-clean fit and avoids cold-load sparse-read complexity.
- **Why the `id_iseq_product` iRODS index joins the sparse read set, others
  don't.** The iRODS mirror is ~9M rows and is cold-loaded; the
  run-scope/manifest joins on `id_iseq_product` must be index-served immediately
  after cold load (the documented per-platform-breakdown perf trap), so it joins
  `seqProductIRODSLocationsMirrorReadIndexes`.
  `study_mirror`/`study_users_mirror` are small reference tables, not in the
  sparse machinery.
- **Why headers, not an envelope, for the new lists.** Carries the realworld1
  decision forward: bare-slice bodies + `X-Total-Count`/`X-Next-Offset` + typed
  `Page[T]` variants, unchanged OpenAPI/MCP surface. The manifest is the one
  envelope (it must carry study metadata once), so it is a plain `remoteCall`
  value, paginated by its `rows` with sizing headers.

### Testing strategy (summary)

- Hermetic GoConvey over the ephemeral SQLite cache, seeded via the J1 scenario;
  count<->list cross-check for every new count; `So()` never in loops > 20.
- Real-MySQL integration tests (I1) assert the new paths execute on MySQL,
  return correct counts/rows, and are index-served via EXPLAIN.
- Source integration test (I2) keeps the assumed source schema (`study_users`,
  `study.faculty_sponsor`/`data_access_group`, `iseq_product_metrics.qc`) true.

### Implementor / reviewer references

- Follow **go-conventions** (copyright header, modern Go, GoConvey mechanics,
  the four-step add-a-query recipe, the `id_lims = 'SQSCP'` invariant) and
  **testing-principles** (behaviour-focused; supported boundaries: HTTP
  contract,
  typed `Client`/`RemoteClient` results, persisted mirror state, generated
  docs).
- Every spec acceptance test MUST have a corresponding GoConvey test -- no
  stubs, no hardcoded results, no swallowed failures, no build-tag exclusions.
