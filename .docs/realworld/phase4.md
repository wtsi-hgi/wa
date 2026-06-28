# Phase 4: Progress (F1-F4)

Ref: [spec.md](spec.md) sections F1, F2, F3, F4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both. Assert the closed enums verbatim (the 9 milestone
names, the 3 baseline phases, the `with_data`/`sequenced_no_data`/
`registered` ladder) and treat the within-sequencing status vocabulary
as an open dict/source pass-through (test as pass-through, never a frozen
list). The QC roll-up is fail > pending > pass on the overall `qc`
(1->pass, 0->fail, NULL->pending); ONT availability/QC/status are
`not_tracked`, never a false zero.

All new code lives in `mlwh/progress.go`; tests in
`mlwh/progress_test.go`, hermetic over `openSQLiteSyncTestCache` using
the HARD REQ 7 scenario seed (tracking-mirror samples filled to
different milestones, samples absent from it, `iseq_run_status` rows
with a recurrence and a derived-current, a PacBio sample, an ONT sample,
and a multi-platform sample). This phase reuses the availability/
delivered SQL from Phase 2 for the delivered phase and the ladder.

## Items

### Batch 1 (parallel)

F1 (baseline) and F2 (run-status timeline) are independent foundations:
F1 derives the coarse phase; F2 produces the normalized
`RunStatusTimeline`. Both are consumed by F3.

#### Item 4.1: F1 - always-available baseline (P0) [parallel with 4.2]

spec.md section: F1

Derive `baseline_phase` (`registered` -> `sequenced` [+QC roll-up] ->
`delivered` [+`delivered_at` = earliest iRODS `created`]) via the
platform-coverage union (product-metrics + iRODS mirrors for Illumina/
PacBio/Elembio/Ultimagen; `oseq_flowcell` for ONT with iRODS/QC reported
`not_tracked`). A multi-platform sample's baseline is the most-advanced
phase across its platforms; QC is the per-sample roll-up (fail >
pending > pass) on the overall `qc`. Covering all 6 acceptance tests
from F1 (registered, no qc set; Illumina NULL qc -> sequenced/pending;
delivered/pass/earliest delivered_at; any-fail -> fail; ONT
registered/not_tracked/`["ONT"]`; multi-platform most-advanced
delivered). Depends on Phases 1 and 2 (reuses the delivered/availability
SQL).

- [x] implemented
- [x] reviewed

#### Item 4.2: F2 - within-sequencing run-status timeline (P5) [parallel with 4.1]

spec.md section: F2

Implement `RunStatus(ctx, idRun string) (RunStatusTimeline, error)` ->
one normalized `RunStatusTimeline` of `{phase, entered_at, duration}`
events. `:id` = Illumina NPG `id_run`. Illumina: join
`iseq_run_status_mirror` to `iseq_run_status_dict_mirror` for `phase`
(= `description`), ordered by `date`, `entered_at` = `date`, `current` =
phase of the latest `date` (DERIVED, never source `iscurrent`),
recurrences/on-hold/cancelled preserved (not forced monotonic). Produce
the same normalized type for PacBio/Elembio/Ultimagen from their own
status/dates (used by F3's per-run embedding); ONT -> empty events +
`not_tracked`. The Description must state the open status vocabulary,
the derived `current`, and the `entered_at` vs `reached_at` distinction.
Covering all 5 acceptance tests from F2 (ordered events with deltas and
empty last duration and derived current; current is latest-date not
`iscurrent`; recurrence + on-hold preserved in order; unknown dict
description passes through; invalid run ErrNotFound). Depends on Phase 1
(`iseq_run_status`/dict mirrors + per-platform status mirrors).

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

### Batch 2 (parallel, after batch 1 is reviewed)

F3 embeds F2's `RunStatusTimeline` (and uses F1's baseline); F4 reuses
F1's baseline derivation for the ladder. Both depend on Batch 1 but not
on each other.

#### Item 4.3: F3 - unified sample progress endpoint (P2/P4/P6) [parallel with 4.4]

spec.md section: F3

Implement `SampleProgress(ctx, sangerName string) (SampleProgress, error)`
(by Sanger name) -> `SampleProgress`. Always return the P0 baseline
(4.1). When the sample is in `seq_ops_tracking_per_sample_mirror`, add
the ordered `milestones` (each `reached_at` + `duration_to_next`),
`current_milestone` (latest reached milestone whose successor is NULL),
and `detailed_timeline=true`; otherwise `detailed_timeline=false` with a
non-empty `timeline_reason` (never an error). Embed the F2
`RunStatusTimeline` (same type, no drift) for each of the sample's runs.
`cache_synced_at` = oldest `last_run` across feeding tables; the
open/current phase returns the timestamp (no server "now" subtraction).
The Description pins the 9 milestone names, 3 baseline phases, the QC
mapping/roll-up (overall `qc` authoritative), and the `reached_at` vs
`entered_at` distinction. Covering all 6 acceptance tests from F3
(tracked sample milestones in canonical order with deltas and
current_milestone; absent-from-tracking still returns baseline with a
reason; embedded run equals `RunStatus(thatRun)`; ONT resolves with
`["ONT"]`/not_tracked/empty runs; library_complete current with delta;
unknown name ErrNotFound, never-synced
ErrCacheNeverSynced+ErrNotFound). Depends on 4.1 (baseline) and 4.2
(`RunStatusTimeline`).

- [x] implemented
- [x] reviewed

#### Item 4.4: F4 - study status-breakdown rollup (P3) [parallel with 4.3]

spec.md section: F4

Implement `StatusBreakdown(ctx, studyLimsID string) (StatusBreakdown, error)`
-> `StatusBreakdown`. `distinct` is the distinct-sample partition
(most-advanced phase, sums to `samples_total`); `per_platform` is the
per-platform partition (each platform's buckets sum to its sample count;
grand total may exceed `samples_total`). `with_detailed_timeline` =
count of the study's samples also in the tracking mirror. Use one small
grouped query per partition (never N per-sample lookups). Samples with no
product-metrics (incl. ONT) are `registered`, never folded into
without-data. The Description pins the ladder enum, the two denominators,
and the freshness caveat. Covering all 4 acceptance tests from F4
(distinct ladder {3,1,1} summing to 5 and with_detailed_timeline=2;
multi-platform counted under both platforms in per_platform but once in
distinct; ONT counted in registered with distinct still summing to
total; never-synced/unknown/empty cascade). Depends on 4.1 (baseline
derivation) and Phase 2 (availability ladder SQL).

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all items
in the batch together in a single review pass).

## Ordering and dependency notes

- This phase depends on Phase 1 (the new mirrors) and Phase 2 (the
  availability/delivered SQL reused for the delivered phase and the
  ladder) being reviewed.
- Batch 1 (F1, F2) is parallel and foundational; Batch 2 (F3, F4) is
  parallel and runs after Batch 1 is reviewed (F3 embeds F2's type; F4
  reuses F1's baseline). F3 and F4 do not depend on each other.
- `RunStatusTimeline` is shared between F2's `GET /run/:id/status` and
  F3's per-run embedding -- reviewers must confirm the single shared type
  is used in both (no drift).
- Per-endpoint Registry/handler/RemoteClient wiring is done incrementally
  here (per the spec's G note); Phase 5 only does the final doc
  regeneration and drift-guard verification.
