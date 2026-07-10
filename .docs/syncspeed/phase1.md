# Phase 1: Parallel table reshapes - watermark + tracking diff (C, D)

Ref: [spec.md](spec.md) sections C1, C2, D1, D2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those
skills reference `go-conventions` and `testing-principles`; ensure
subagents follow both. This feature is test-driven: every spec acceptance
test is a GoConvey test that asserts user-visible boundaries (mirror
contents, `SyncReport`, recorded source-query shape, recorded mirror
write/commit counts), never private helper internals.

The two items reshape independent tables and MAY run in parallel (spec
Implementation Order: "Within Phase 1 the two tables are independent and
may proceed in parallel"). Both edit `mlwh/sync_platform_coverage.go` but
in disjoint functions - C in `syncIseqRunStatusTable` /
`iseqRunStatusResumeID`, D in `syncSeqOpsTrackingPerSampleTable` and the
code replacing `writeSeqOpsTrackingPerSampleFullRefresh` - so give each
item its own subagent and reconcile the shared file at review.

Neither item depends on the Phase 2 parity harness. C uses the EXISTING
recording source `recordingOrderSource` (it already captures the paging
cursor) plus `openRecordingSQLiteSyncTestCache`. D uses
`openRecordingSQLiteSyncTestCache` with its recording driver/observer
EXTENDED to capture the diff-read (D2.2). Do NOT add `recordingSource` /
`assertWarmMirrorMatchesCold` here - those are the Phase 2 harness.

Feature-wide constraints (hold in every phase): no MLWH source schema
change; do NOT bump `CacheSchemaVersion` (it routes through
`migrateCacheSchema`, which drops all mirrors and forces a full cold
resync); do NOT add a UNIQUE/PRIMARY KEY on the tracking mirror's
`id_sample_lims`. Do NOT run the live sync or any db/network command; rely
on the hermetic GoConvey suite over the ephemeral SQLite cache. Bound
every shell command with `timeout`.

Critical cautions for item 1.2 (D), stated in full in the item:
1. Compute the WHOLE diff plan while streaming the mirror read, then apply
   inserts/updates/deletes AFTER the read cursor is drained and closed - a
   MySQL INSERT issued while a streaming SELECT is still open on the same
   connection/tx fails (documented in `sync.go` around lines 738-742).
2. Scope the D2.2 query-recording to the diff-read connection (or a
   per-connection "record queries" flag), NOT a global write-observer, so
   it does not capture unrelated reads such as `countExistingKeys`'
   `SELECT COUNT(*)` and break exact-count assertions.
3. Wire `syncTrackingMirrorResidencyHook` to the TRUE mirror-side
   working-set size (1 per merge step), so a full-materialization diff
   would report `N`, not `1`; go-reviewer must confirm this at review.

## Items

### Batch 1 (parallel)

The two tables are independent; use one subagent per item.

#### Item 1.1: C - iseq_run_status retained watermark [parallel with 1.2]

spec.md section: C1, C2

Reshape `syncIseqRunStatusTable` finalize in
`mlwh/sync_platform_coverage.go` to STOP clearing `resume_cursor`. On
successful completion persist `resume_cursor = id_run_status \t <last
completed id>`, where the id is the max `id_run_status` paged this run, or
the id already stored when no new rows were read; `high_water` stays empty
(zero). `iseqRunStatusResumeID` (the only reader) already returns the cold
initial id (0) for a NULL cursor, so an upgraded cache does one full
re-read then persists the watermark. No migration or backfill.

Covering all 2 acceptance tests from C1 (a warm sync after inserting rows
M+1..M+P pages from cursor M not 0, `Inserted == P`, `Updated == 0`,
mirror count `M+P`, stored cursor decodes to `M+P`; a following zero-row
warm sync records no `iseq_run_status_mirror` write, `SyncReport` is
`{0,0}`, the stored cursor still decodes to `M+P` and is never NULL, and
`high_water` stays empty) and all 2 from C2 (a NULL-cursor upgraded cache
whose mirror already holds `1..M` does one full re-read from cursor 0 with
`Inserted == 0` / `Updated == M` then stores cursor `M`; the next warm
sync pages from `M` and reads zero rows). Tests in `mlwh/sync_a5_test.go`
assert cursor progression via the existing recording source and no-op
writes via `openRecordingSQLiteSyncTestCache`. C revises no existing test
(spec E2, verified).

- [x] implemented
- [x] reviewed

#### Item 1.2: D - seq_ops_tracking_per_sample diff/apply [parallel with 1.1]

spec.md section: D1, D2

Replace the wholesale delete-and-reinsert
(`writeSeqOpsTrackingPerSampleFullRefresh`) in
`mlwh/sync_platform_coverage.go` with an atomic in-application diff/apply.
Keep the full buffered source snapshot read; sort it byte-wise in Go
(`slices.Sort` on `id_sample_lims`). Stream the current mirror in the SAME
byte-wise order - `ORDER BY id_sample_lims COLLATE utf8mb4_bin` (MySQL) /
`ORDER BY id_sample_lims COLLATE BINARY` (SQLite), which overrides the
column's case-insensitive declared collation so Go byte order and the
mirror stream agree - and merge-compare to compute inserts (in snapshot,
not mirror), updates (in both, a non-key column differs as stored, with
NULL-vs-NULL milestone datetimes equal), and deletes (in mirror, not
snapshot). Row identity is `id_sample_lims` (unique, never NULL in
source). Apply all inserts + updates + deletes + the `sync_state` write in
ONE write transaction. `high_water` ADVANCES to the refresh time on every
run (the snapshot is always re-read), even when the diff is empty;
`SyncReport.Inserted` counts new ids, `Updated` counts changed ids
(deletions are observable via mirror absence / row-count delta).

Add the production seam `syncTrackingMirrorResidencyHook`
(`func(residentMirrorRows int)`, nil in production) and call it as the
diff advances with the count of mirror rows held resident in the
mirror-side working set (1 per merge step). Add the test helper
`withSyncTrackingResidencyHookForTest`. EXTEND the recording
driver/observer (`sqliteSyncSQLObserver` / `recordingSQLiteConn`) so the
mirror diff-read `QueryContext` is recorded on the observed connection
(D2.2).

CAUTIONS (all in spec):
- Full-drain: compute the entire diff plan (bounded by change count) while
  streaming the mirror, then issue every INSERT/UPDATE/DELETE only AFTER
  the diff-read cursor is drained and closed. A MySQL INSERT run while a
  streaming SELECT is open on the same connection/tx fails (see `sync.go`
  ~738-742). The compute-then-apply ordering makes this explicit.
- Observer scope: record the diff-read on the diff-read connection (or a
  per-connection "record queries" flag), NOT a global write-observer that
  would also capture `countExistingKeys`' `SELECT COUNT(*)` and break the
  exact-count assertions. No stub - the recorded SELECT must be the real
  streamed diff-read.
- Residency: the hook must report the TRUE resident mirror-row count so a
  streamed one-row merge records peak `1` while a full-materialization
  diff would record `N`; go-reviewer confirms this.
- Do NOT add a UNIQUE/PRIMARY KEY on `id_sample_lims`; do NOT bump
  `CacheSchemaVersion`.

Covering all 4 acceptance tests from D1 (mixed insert/update/delete over
`{A,B,C,D}` vs `{A,B',E}` yields `Inserted == 1`, `Updated == 1`, mirror
becomes `{A,B,E}`, surviving columns equal the snapshot; the mix runs in
exactly one write transaction, `BeginCount()` / `CommitCount()` each 1;
NULL-vs-NULL milestone equality yields no update; case-variant ids
`{A,a,B,b}` equal on both sides give `{0,0}` with no mirror
INSERT/UPDATE/DELETE, proving both sides use byte-wise order) and all 3
from D2 (equal mirror/snapshot writes only one `sync_state` upsert in one
committed tx, `{0,0}`, mirror unchanged, `high_water`/`last_run` advance;
the observer records exactly one SELECT against
`seq_ops_tracking_per_sample_mirror` whose text contains `ORDER BY
id_sample_lims COLLATE BINARY` and no unordered full-table SELECT; with
`N = 10000` equal rows the residency hook's recorded peak is exactly 1,
not `N`). Tests in `mlwh/sync_a5_test.go` on
`openRecordingSQLiteSyncTestCache`.

Also revise the two existing tracking tests (spec E2) so the package
compiles and their intent is preserved:
- `TestClientSyncSeqOpsTrackingPerSampleSwapIsAtomic`: retarget at the new
  diff/apply entry point, preserving the concurrent-reader atomicity
  intent (a reader never sees a partial table).
- `TestClientSyncSeqOpsTrackingPerSampleFullRefreshReplacesSnapshot`:
  content still passes under diff/apply (disjoint old/new sets), but
  rename and recomment it to diff/apply semantics (no wholesale
  full-refresh claim).

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item. Launch review
subagents using the `go-reviewer` skill (review both items in the batch
together in a single review pass).

## Ordering and dependency notes

- C and D are independent tables and run in parallel; both edit
  `sync_platform_coverage.go` in disjoint functions, so reconcile that
  file when merging the two subagents' work.
- Phase 1 does NOT depend on the Phase 2 parity harness. C reuses the
  existing recording source and recording cache; D extends the existing
  recording driver/observer (D2.2).
- C revises no existing test (spec E2, verified). D revises exactly two
  existing tracking tests (see item 1.2).
- `high_water` behaviour differs by table (spec Appendix): C keeps
  `high_water` empty and preserves the retained cursor (never NULL); D
  advances `high_water` to the refresh time every run.
