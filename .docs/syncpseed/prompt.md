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
