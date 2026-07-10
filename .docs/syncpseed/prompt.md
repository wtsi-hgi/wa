# Faster Warm MLWH Sync

## Goal

Make `wa mlwh sync` cheap enough to run every 30 minutes when the live MLWH source has only a small number of changes since the previous sync.

Do not split tables onto different schedules. A single `wa mlwh sync` invocation must still cover every supported sync table.

Do not require or propose MLWH source database changes. The source database is outside our control.

Cold sync may remain expensive. The requested changes target the warm path: low source query load where the current query shape is wasteful, and low destination write volume where the source must still be read.

## Evidence Summary

The investigation used `.env.development.local` and disposable MySQL cache databases derived from `WA_MLWH_CACHE_PATH`.

A seeded-warm baseline was measured by creating a fresh cache schema, seeding `sync_state` to current live high-water values as if a sync had just completed, and running the unmodified `Client.Sync`.

Current seeded-warm baseline:

- Total sync: `7m0.254s`.
- `seq_product_irods_locations`: 95 rows, `3m59.964s`.
- `iseq_product_metrics`: 4 rows, `3m0.276s`.
- `seq_ops_tracking_per_sample`: 1,464,639 rows, `1m12.427s`.
- `iseq_run_status`: 704,236 rows, `44.278s`.
- `sample`: 1 row, `24.181s`.
- `iseq_flowcell`: 388 rows, `7.773s`.
- All other tables were under `3s`, most under `1s`.

The current total is dominated by `iseq_product_metrics` in the parallel pre-iRODS phase and then by the staged `seq_product_irods_locations` phase. Removing those two long source queries, plus the unnecessary run-status and tracking-table write work, should move seeded-warm sync from about 7 minutes to roughly under 1 minute on the measured development-scale data.

## Requested Changes

### 1. Change warm `seq_product_irods_locations` to start from changed iRODS rows

Current warm behaviour:

- The seeded-warm sync returned only 95 `seq_product_irods_locations` rows, but took `3m59.964s`.
- The slow work is source-side: the current query builds the large product recovery rowset, including Illumina `JSON_TABLE` composition recovery, before the high-water filter makes the result small.

Requested behaviour:

- For warm incremental sync, first materialize only changed `seq_product_irods_locations` rows using the existing high-water predicate.
- Join that small changed rowset into each recovery branch: Illumina composition recovery, Illumina legacy fallback, PacBio, Elembio, Ultimagen, and ONT.
- Keep cold sync separate if that is simpler.
- Preserve current row semantics: recovered sample/study metadata, sibling iRODS objects, multi-row expansion from one source row, stable source-row replacement, and platform coverage must match current behaviour.

Experimental result:

- A changed-first live source query returned the same 95-row result count in `10.64s`.
- This is the largest proven source-query win: about 4 minutes down to about 11 seconds for the measured warm window.

### 2. Change warm `iseq_product_metrics` to avoid broad composite recovery

Current warm behaviour:

- The seeded-warm sync wrote only 4 `iseq_product_metrics` rows, but took `3m0.276s`.
- The current composition-aware query unions direct rows with a broad composite recovery branch using `JSON_TABLE`.
- A direct changed-first CTE that still kept broad composite recovery was stopped after `3m29.69s`; that shape is not an improvement.

Requested behaviour:

- For warm incremental sync, replace the monolithic union query with a two-phase warm path:
  1. Fetch the changed `iseq_product_metrics` rows directly with their flowcell/study metadata and `iseq_composition_tmp`.
  2. In Go, identify changed rows whose composition needs composite recovery.
  3. Run composite recovery only for those changed product IDs.
- Keep the existing cold path and unsupported-`JSON_TABLE` fallback behaviour intact.
- Preserve existing ordering/resume semantics by `last_changed` and `id_iseq_pr_metrics_tmp`.
- Preserve current mirror row semantics for direct rows and recovered composite rows.

Experimental result:

- The direct changed-row fetch returned 1,647 live changed rows in `10.15s`.
- Of those rows, 508 were multi-component/composite candidates.
- Product-ID-scoped composite recovery for those 508 candidates returned 0 additional recoverable rows in `0.08s`.
- This replaces the measured `3m0.276s` warm table cost with about `10.23s` for the measured live warm window.

### 3. Retain a completed high-id cursor for `iseq_run_status`

Current warm behaviour:

- `iseq_run_status` has an ascending `id_run_status` but no time high-water.
- The sync pages by `id_run_status`, then clears `resume_cursor` after a successful completed run.
- The next warm sync starts from `id_run_status > 0` again.
- The seeded-warm sync reread and rewrote 704,236 rows in `44.278s`.

Requested behaviour:

- Store the last completed `id_run_status` watermark after successful sync.
- On the next warm sync, query only rows with `id_run_status > last_completed_id`.
- Keep freshness semantics clear even though this table does not use a timestamp high-water.
- Write only new status rows in warm sync.

Experimental result:

- Current live max `id_run_status` was 708,910.
- Querying `id_run_status > 708910` returned 0 rows in `0.04s`.
- This replaces the measured `44.278s` warm table cost with effectively zero work when no new status rows exist.

### 4. Diff `seq_ops_tracking_per_sample` instead of deleting and reinserting unchanged rows

Current warm behaviour:

- `seq_ops_tracking_per_sample` has no reliable change key and mutates in place, so the source table still has to be read as a full snapshot.
- The current sync deletes and reinserts the mirror every time.
- The seeded-warm sync wrote 1,464,639 rows and took `1m12.427s`.

Requested behaviour:

- Keep the full source snapshot read.
- Do not split this table onto a different schedule.
- Replace wholesale delete-and-reinsert with an atomic diff/apply against the current mirror.
- Use `id_sample_lims` as the row identity for the diff; the live source duplicate check found zero duplicate `id_sample_lims` values.
- Insert new rows, update rows whose values changed, delete rows missing from the new snapshot, and update sync state.
- If the snapshot is unchanged, write only sync state and zero mirror rows.
- Preserve atomicity: readers must see either the old complete snapshot or the new complete snapshot, never a partially applied table.

Experimental result:

- A disposable clone of the current tracking mirror was created only as probe setup.
- Against that cloned cache, the proposed no-op warm path measured:
  - source snapshot read: 1,464,639 rows in `3.616s`;
  - in-memory sort by `id_sample_lims`: `0.037s`;
  - ordered cache scan and exact diff: 1,464,639 source rows vs 1,464,639 cache rows, 0 inserts, 0 updates, 0 deletes, `6.158s`.
- The measured unchanged warm strategy is therefore about `9.81s` plus sync-state write, with zero mirror-row writes, instead of `1m12.427s` and 1.46M mirror-row writes.

## Explicit No-Change Decisions

Do not change the following tables as part of this feature. The investigation did not find a significant warm-speed improvement, or the current cost is already small enough for a 30-minute cadence.

- `sample`: no change. Seeded-warm sync returned 1 row in `24.181s`. Source query variants did not improve the warm query: current order `50.25s`, no order `50.19s`, id order `50.09s` in the later live probe. The cache-side `common_name` vocabulary read used by `common_name_word_mirror` rebuild took only `0.22s`, so skipping that rebuild is not the meaningful bottleneck.
- `study`: no change. Seeded-warm sync returned 1 row in `99.7ms`.
- `iseq_flowcell`: no change. Seeded-warm sync returned 388 rows in `7.773s`. Removing the current order changed source timing only from `7.34s` to `7.05s`, not a significant improvement.
- `iseq_run_status_dict`: no change. Full refresh returned 27 rows in `52.8ms`.
- `oseq_flowcell`: no change. Full refresh returned 8,730 rows in `583ms`.
- `study_users`: no change. Full refresh returned 45,604 rows in `2.743s`.
- `pac_bio_run_well_metrics`: no change. Full refresh returned 12,527 rows in `903ms`.
- `eseq_run`: no change. Full refresh returned 301 rows in `110ms`.
- `eseq_run_lane_metrics`: no change. Full refresh returned 298 rows in `110ms`.
- `useq_run_metrics`: no change. Full refresh returned 316 rows in `67ms`.
- `pac_bio_product_metrics`: no change. Seeded-warm sync returned 3 rows in `78ms`.
- `eseq_product_metrics`: no change. Seeded-warm sync returned 4 rows in `110ms`.
- `useq_product_metrics`: no change. Seeded-warm sync returned 96 rows in `110ms`.

## Expected Result

After these changes, a warm sync with very few source changes should:

- still sync every supported table in one command;
- avoid the current broad source recovery cost for warm `seq_product_irods_locations`;
- avoid the current broad composite recovery cost for warm `iseq_product_metrics`;
- avoid rereading and rewriting all historical `iseq_run_status` rows;
- avoid rewriting 1.46M unchanged tracking rows;
- keep the unchanged full-refresh tables on the same schedule because their measured costs are already small.

## Implementation Constraints Verified Against Live Source (2026-07-10)

This section records facts re-verified against the live source (`WA_MLWH_DSN`) and
current code (`mlwh/sync.go`, `mlwh/sync_platform_coverage.go`) after the changes in
PRs #29 and #30 landed. They are binding constraints, not new goals: the four
requested changes are correct, but these are the non-obvious details an
implementation must respect to stay behaviour-preserving. The original per-table
timings above come from an earlier investigation window and remain illustrative;
the numbers here are from a fresh re-check.

### General

- Warm scope only. Each change applies to BOTH warm variants of its table: the
  plain incremental query (no `resume_cursor`) and the from-cursor resume query
  (mid-warm-sync resume). The cold/ascending-id path and the unsupported-`JSON_TABLE`
  legacy fallback must be left intact. All four affected tables have distinct
  cold / incremental / from-cursor query builders today; only the incremental and
  from-cursor builders are in scope.
- Preserve the `AllSyncSourceQueries()` registry: any new or reshaped source SELECT
  the sync issues must be represented there so the source-schema integration test
  still covers it.

### 1. `seq_product_irods_locations` changed-first (proven)

- Equivalence was proven on the live source over a real warm window: the current
  query and a changed-first rewrite returned an identical 504-row result set
  (identical MD5 checksum of all projected columns), in `4m27s` vs `5.3s`.
- The rewrite that works: materialize changed `spi` rows first
  (`WHERE last_changed >= ?`, or the two-part `(last_changed > ?) OR (last_changed = ?
  AND id_seq_product_irods_locations_tmp > ?)` from-cursor predicate), derive the
  distinct changed `id_product` set, and push that set into EACH recovery branch
  (Illumina composition, PacBio, Elembio, Ultimagen, ONT/oseq) so `JSON_TABLE`
  expands only changed products. This is a semijoin reduction of the current
  `spi INNER JOIN (recovery UNION ALL) ON recovery.id_product = spi.id_product`, so
  it cannot change the inner-join result.
- Must preserve, unchanged: the exact projected column list and order consumed by
  `scanSeqProductIRODSLocationsSyncRow` (`id_seq_product_irods_locations_tmp`,
  `id_product`, `irods_root_collection`, `COALESCE(irods_data_relative_path,'')`,
  recovered `id_sample_tmp`, recovered `id_study_lims`, `last_changed`, `created`,
  `seq_platform_name`); the `ORDER BY spi.last_changed, spi.id_seq_product_irods_locations_tmp`
  (so all expansion rows of one source row stay contiguous for the source-row-boundary
  batch flush in `syncSeqProductIRODSLocationsTable`); the post-fetch
  `enrichSeqProductIRODSLocationsExportFields` step, which fills
  `id_run`/`position`/`tag_index`/`qc`/`is_deliverable`/`merged` from the LOCAL
  product-metrics mirrors and is independent of the source query.

### 2. `iseq_product_metrics` two-phase (feasible; two subtleties)

- Live re-check (1-day window): 3,285 changed rows, of which 272 were
  multi-component; scoped composite recovery over those 272 ran in `0.097s`. The
  Phase-1 direct fetch ran in `10.7s`. Confirms the two-phase shape.
- Correctness subtlety (must get right): multi-component ("merged") products have a
  NULL own `id_iseq_flowcell_tmp` — this was true for 100% (272/272) of the
  multi-component changed rows sampled. The current composite branch therefore
  recovers `id_sample_tmp`/`id_study_lims` from the COMPONENTS' flowcells (via the
  `JSON_TABLE` component join), NOT from the path product's own flowcell, and stores
  `COALESCE(path_ipm.id_iseq_flowcell_tmp, MIN(component ipm.id_iseq_flowcell_tmp))`.
  Consequently Phase 1 must NOT gate row inclusion on an INNER JOIN to the path
  product's own flowcell/study — that would silently drop every merged product
  before Phase 2 could classify it. Phase 1 should fetch every changed row plus its
  `iseq_composition_tmp` (LEFT JOIN own flowcell/study for the direct-row metadata),
  and Phase 2 should classify purely by component count, which is the same disjoint
  partition the current SQL uses (direct branch = `NOT EXISTS components[1]`;
  composite branch = multi-component).
- Behaviour to preserve for single-component (direct) rows: a single-component row is
  emitted only when its own flowcell exists AND that flowcell's study is `SQSCP`
  (the current direct branch INNER-JOINs `study ... AND id_lims = 'SQSCP'`); rows
  failing that are dropped (in the sampled window 49 single-component changed rows
  had a NULL own flowcell and are correctly dropped today, alongside any whose study
  is not `SQSCP`). Direct rows keep their own `id_run`/`position`/`tag_index`.
- Behaviour to preserve for composite rows: run the existing composite recovery
  (component `JSON_TABLE` expansion, `EXISTS` an Illumina `spi` row for the path
  product, `HAVING COUNT(*) > 1`, `position = 0`, `tag_index = 0`, `id_run` =
  common component run else 0, `MIN` sample/study, `qc`/`qc_lib`/`qc_seq` and
  `last_changed` from the path row) so a candidate that lacks an Illumina `spi` row
  or fails `HAVING` yields no mirror row, exactly as today.
- Performance subtlety (must get right): the composite recovery MUST be scoped by the
  explicit list of changed multi-component product IDs already fetched in Phase 1
  (an `IN (...)` of literal IDs, as Go would build). Re-deriving the candidate set
  with a correlated subquery (`... WHERE id_iseq_product IN (SELECT ... WHERE
  last_changed >= ? AND JSON_LENGTH(...) > 1)`) makes the optimizer expand
  `JSON_TABLE` far too broadly — the subquery form did not finish in 110s, versus
  `0.097s` for the explicit-ID form. Chunk the ID list to respect the sync statement
  parameter limit.
- Preserve ordering/resume semantics: final combined output ordered by
  `last_changed, id_iseq_pr_metrics_tmp`, and the from-cursor resume cursor
  (`last_changed \t id_iseq_pr_metrics_tmp`) unchanged.

### 3. `iseq_run_status` retained cursor (safe, localized)

- Confirmed current behaviour: `writeIseqRunStatusBatch` already stores
  `resume_cursor = id_run_status \t <maxID>` during paging, but
  `syncIseqRunStatusTable` calls `finalizeSyncState(..., time.Time{})` at the end,
  and `finalizeSyncState` writes `resume_cursor = NULL` — so the completed cursor is
  discarded and the next warm sync restarts from `id_run_status > 0`.
- `id_run_status` is the source PRIMARY KEY and monotonic (min 1, live max 709,055),
  so a stored high-id watermark is a sound resume key.
- The change is localized: the ONLY consumer of this cursor is
  `iseqRunStatusResumeID`. Freshness reads only `high_water` and `last_run` from
  `sync_state` and ignores `resume_cursor`, so retaining the cursor does not affect
  freshness. Keep `high_water` empty (zero) for this table as today; the fix is to
  stop clearing the completed cursor at finalize (persist the last paged `id_run_status`).

### 4. `seq_ops_tracking_per_sample` diff/apply (identity safe; mirror lacks a unique key)

- Source identity is safe: `mlwh_reporting.seq_ops_tracking_per_sample` has
  `id_sample_lims` unique and never NULL (1,463,775 rows, 1,463,775 distinct, 0
  NULL). The table lives in the `mlwh_reporting` schema (not `mlwarehouse`), as the
  current schema-qualified source query already reflects.
- Constraint to resolve in the spec: the cache mirror `seq_ops_tracking_per_sample_mirror`
  currently declares `id_sample_lims` as `NOT NULL` with only a NON-unique index —
  there is no PRIMARY KEY or UNIQUE constraint on it. An `id_sample_lims`-keyed
  upsert/delete therefore either (a) needs a new UNIQUE index added to the mirror
  schema (a cache-side schema change plus a `schema_version` bump — permitted, since
  only the source is off-limits), or (b) must diff in application code (load the
  current mirror keyed by `id_sample_lims`, compare against the new snapshot, then
  issue targeted inserts/updates/deletes). The earlier evidence ("ordered cache scan
  and exact diff") describes approach (b).
- Preserve atomicity by applying the whole diff (inserts + updates + deletes + sync
  state) inside one write transaction, so a concurrent reader on another connection
  sees either the entire old snapshot or the entire new one (the same guarantee the
  current single-transaction delete-and-reinsert gives). When the snapshot is
  unchanged, write only `sync_state` and zero mirror rows.

## Notes

Decisions resolved during requirements clarification. These are binding.

- `seq_ops_tracking_per_sample` warm diff is computed in application code with NO
  cache schema change: load the current mirror keyed by `id_sample_lims`, compare
  against the freshly read source snapshot, and apply the difference (insert new
  `id_sample_lims`, update rows whose non-key values changed, delete `id_sample_lims`
  absent from the new snapshot) together with the `sync_state` write inside one
  write transaction. Do NOT add a UNIQUE/PRIMARY KEY on the mirror's `id_sample_lims`
  and do NOT bump `CacheSchemaVersion` (a bump routes through `migrateCacheSchema`,
  which drops every mirror table and clears every `sync_state` row, forcing a full
  cold resync of all tables). Prefer a bounded-memory approach (ordered streams
  merged on `id_sample_lims` rather than two full in-memory maps where practical),
  consistent with the codebase's paged cold-load discipline. When the snapshot equals
  the mirror, write only `sync_state` and zero mirror rows.

- `iseq_run_status` retains the last completed `id_run_status` as its resume
  watermark: stop clearing `resume_cursor` at finalize so the next warm sync queries
  only `id_run_status > last_completed_id` and writes only new rows; keep `high_water`
  empty (zero) for this table. No upgrade migration or backfill is added. Existing
  caches (whose `resume_cursor` the old code cleared to NULL) simply perform one final
  full re-read of `iseq_run_status` on the first post-upgrade sync; thereafter the
  retained watermark makes every warm sync near-zero. That one-time re-read is
  expected and acceptable.

- The `iseq_product_metrics` warm two-phase path may buffer the full changed-row set
  in memory for a single run (all-or-nothing for that run) and need not checkpoint
  `resume_cursor` mid-run. Crash resumability is provided only by the existing
  from-cursor query resuming a previously interrupted run. The cold/ascending-id path
  stays streaming and unchanged. No explicit warm-window row cap or cold-fallback
  threshold is required; this is justified by the warm premise of few changes.

- `seq_product_irods_locations` warm sync stays a single self-contained SQL statement:
  a changed-`spi` CTE (or derived table) applying the high-water (incremental) or
  two-part (from-cursor) predicate, semi-joined into each recovery branch so
  `JSON_TABLE` expands only changed products, preserving the exact projected columns,
  the `ORDER BY spi.last_changed, spi.id_seq_product_irods_locations_tmp`, and the
  source-row-boundary batch flush. Register its incremental and from-cursor variants
  in `AllSyncSourceQueries` with fixed arg counts as today. `iseq_product_metrics`
  Phase 1 (direct changed-row fetch) is a single registered statement; Phase 3 scoped
  composite recovery is a Go-built, chunked, explicit `IN`-list query registered in
  `AllSyncSourceQueries` via a canonical single-ID representative form (arg count for
  one bound product ID) so the source-schema integration test validates its columns
  and schema.

- Warm-path correctness for `seq_product_irods_locations` and `iseq_product_metrics`
  is tested against the hermetic SQLite source: extend `rewriteJSONTableQueryForSQLite`
  to translate the reshaped statements (the `JSON_TABLE` composition fragments stay
  byte-identical, so the existing substitutions keep matching), execute the real warm
  queries on SQLite fixtures covering every recovery branch (Illumina composition,
  Illumina legacy fallback, PacBio, Elembio, Ultimagen, ONT/oseq) and merged /
  multi-component products, and assert the resulting warm mirror is byte-identical to
  a full cold / current-path sync of the same fixture (parity oracle).

- A zero-change warm sync performs zero mirror-row writes, advances `last_run`, and
  reports `SyncReport{Inserted:0, Updated:0}`; finalize still runs when `state.Exists`.
  `high_water` semantics are per-table and unchanged from today, so the blanket phrase
  "preserve high_water" does NOT apply uniformly: for `seq_product_irods_locations` and
  `iseq_product_metrics`, `high_water` is the max source `last_changed` seen, so with no
  changed rows it is preserved at its prior value; for `iseq_run_status`, `high_water`
  stays empty (zero) and it is instead the retained `id_run_status` resume cursor that
  is preserved (never NULL-ed); for `seq_ops_tracking_per_sample`, `high_water` is the
  wall-clock refresh time and therefore ADVANCES to the new refresh time on every run
  (the full snapshot is always re-read), even when the diff is empty. Assert via the
  recording write-observer that the transaction commits with zero mirror-row Execs.

- The `seq_ops_tracking_per_sample` diff has an acceptance test that the mirror side is
  streamed ordered by `id_sample_lims` (not fully materialized alongside the buffered
  source snapshot). A changed row is any differing non-key column compared as stored,
  with NULL-vs-NULL milestone datetimes treated as equal, so an unchanged snapshot
  yields zero updates. Include an explicit no-op test (unchanged snapshot ⇒ only
  `sync_state` written, zero mirror writes) and a mixed test asserting exact
  insert/update/delete counts, all inside one transaction. The existing recording
  observer captures only writes (`ExecContext`) and the read-only connection has no
  observer, so the streamed-ordered-read assertion requires extending the recording
  driver/observer to capture the mirror diff-read (`QueryContext`) on an observed
  connection — do not stub it.

- Because wall-clock cannot be asserted deterministically, warm-path efficiency is
  proven via the recording/counting test infrastructure on two axes: (i) the warm run
  issues the reshaped changed-first / from-cursor source queries and scopes
  `iseq_product_metrics` composite recovery to exactly the Go-classified
  multi-component candidate product IDs (and `iseq_run_status` to
  `id_run_status > retained watermark`); and (ii) destination mirror write counts are
  minimal or zero on a no-op window.
