# Phase 3: iseq_product_metrics two-phase warm path (B1-B2)

Ref: [spec.md](spec.md) sections B1, B2 (E1/E2 lockstep updates landed
here)

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills. Those skills
reference `go-conventions` and `testing-principles`; ensure subagents
follow both. Tests are GoConvey acceptance tests asserting user-visible
boundaries: byte-identical warm-vs-cold mirror dumps, the
composite-recovery `IN`-list args, `SyncReport`, and recorded mirror write
counts.

This is the most complex reshape and REUSES the Phase 2 parity harness
(`assertWarmMirrorMatchesCold`, `recordingSource`, the SQLite rewriter);
it depends on Phase 2. Replace the warm monolithic union for
`iseq_product_metrics` (when `JSON_TABLE` is supported) with the in-Go
two-phase path in `mlwh/sync.go`:
- Phase 1 (one registered SELECT): fetch every changed row with its
  own-flowcell metadata and `iseq_composition_tmp`, LEFT JOIN own
  `iseq_flowcell` and its `SQSCP` `study` (so merged products are NOT
  dropped). Predicate incremental `ipm.last_changed >= ?` (1 arg) or
  from-cursor two-part (3 args); order `ipm.last_changed,
  ipm.id_iseq_pr_metrics_tmp`.
- Phase 2 (Go): classify by component count parsed from
  `iseq_composition_tmp` (`COALESCE` empty to `{"components":[]}`): <= 1 is
  a direct candidate, > 1 composite. Emit a direct row only when its own
  flowcell exists AND that flowcell's study is `SQSCP`; direct rows keep
  their own `id_run`/`position`/`tag_index`.
- Phase 3 (Go-built, chunked): run
  `iseqProductMetricsCompositeSourceSelect` scoped by explicit
  `path_ipm.id_iseq_product IN (<literal changed multi-component ids>)`,
  chunked to a package-level `var` defaulting to `syncStatementRowLimit(1)`
  (shrinkable in tests), preserving the existing composite semantics
  (component `JSON_TABLE` expansion, Illumina `spi` `EXISTS`, `HAVING
  COUNT(*) > 1`, `position`/`tag_index` 0, common-run-else-0 `id_run`,
  `MIN` sample/study, `COALESCE` path/component flowcell, path-row
  `qc`/`qc_lib`/`qc_seq`/`last_changed`).
- Combine direct + composite rows, order `last_changed,
  id_iseq_pr_metrics_tmp`, write via the existing batch writer
  (`iseqProductMetricsMirrorRowArgs`). The warm run may buffer the full
  changed-row set (all-or-nothing per run); no mid-run cursor checkpoint.

The cold / ascending-id union query is UNCHANGED; on
`isUnsupportedCompositionQueryError` the warm path still falls back to the
existing legacy direct-only query.

CAUTIONS (all in spec):
- Scan struct: Phase 1 MUST scan into a NULLABLE fetch struct (spec name
  `iseqProductMetricsChangedRow`: `id_iseq_flowcell_tmp` `sql.NullInt64`,
  `id_sample_tmp` `sql.NullInt64`, `id_study_lims` `sql.NullString`, plus
  the raw `iseq_composition_tmp` and the direct columns), NOT
  `iseqProductMetricsSyncRow` - whose non-nullable `IDSampleTmp` /
  `IDStudyLims` cannot scan a merged product's NULL own-flowcell metadata
  (the merged-product trap this feature exists to avoid).
  `iseqProductMetricsSyncRow` is reused ONLY as the combined write-output
  row.
- Test routing: drive the warm two-phase tests through the parity oracle
  (`assertWarmMirrorMatchesCold`) and `recordingSource` wrapping the REAL
  SQLite `JSON_TABLE` source, NOT the planned-row mock
  `openSyncTestSourceDB` - it routes by table-name string match and cannot
  route the composite-recovery query (its FROM alias is `path_ipm`).
- Scoping: Phase 3 must issue literal `IN`-list queries whose bound args
  union to exactly the Go-classified multi-component ids, chunked to the
  parameter limit; NEVER a correlated subquery that re-derives candidates
  (which makes `JSON_TABLE` expand far too broadly).

Land in THIS phase (so no phase is left red) the E1
`AllSyncSourceQueries()` updates - `iseq_product_metrics incremental`
ArgCount 2->1, `... from cursor` 6->3, new `iseq_product_metrics composite
recovery` (ArgCount 1, canonical single-bound-id form) - and the
`TestRealworldA3...` revision (spec E2, see item 3.1). Preserve verbatim:
the final combined output ordering `last_changed, id_iseq_pr_metrics_tmp`;
the from-cursor cursor encoding; the
`iseqProductMetricsCompositeSourceSelect` `JSON_TABLE` fragments
byte-identical (SQLite rewriter matching).

Do NOT run the live sync or any db/network command; bound every shell
command with `timeout`. Feature-wide: no source schema change, no
`CacheSchemaVersion` bump.

## Items

### Item 3.1: B1 - two-phase warm path + registry/test updates

spec.md section: B1 (with E1 ipm registry + E2 TestRealworldA3 landed
here)

Implement the three-phase warm path above in `mlwh/sync.go` and prove it
cold-identical. In the SAME change (to keep the suite green) update
`AllSyncSourceQueries()`: `iseq_product_metrics incremental` becomes the
Phase-1 direct fetch (ArgCount 1), `... from cursor` (ArgCount 3), and add
`iseq_product_metrics composite recovery` (ArgCount 1, the scoped recovery
in canonical single-bound-id form); cold and legacy entries unchanged.
Revise
`TestRealworldA3IseqProductMetricsSourceQueryIncludesCompositeProducts`
(spec E2): assert the composite fragments (`JSON_TABLE(path_ipm...`, the
composite Illumina `spi` `EXISTS` check, `CASE WHEN
MIN(component.component_run) ...`) on the NEW composite-recovery entry
(ArgCount 1); assert `incremental` (1) and `from cursor` (3) are Phase-1
direct-only (no composite fragments, no single-component `NOT EXISTS (...
JSON_TABLE(COALESCE(ipm...` filter, but keep `study.id_lims = 'SQSCP'`
from the own-flowcell LEFT JOIN); leave cold (ArgCount 2) unchanged.

Covering all 3 acceptance tests from B1 (a mixed fixture - direct SQSCP,
direct NULL-flowcell, direct non-SQSCP, merged multi-component with an
Illumina `spi` row, a multi-component candidate with no `spi` row, and one
failing `HAVING` - warm-synced incremental via
`assertWarmMirrorMatchesCold` is byte-identical to the cold union mirror;
the same forced from-cursor is byte-identical; the merged product's mirror
row has `position=0`/`tag_index=0` with recovered
`id_sample_tmp`/`id_study_lims` from its components, and the NULL-flowcell
and non-SQSCP direct rows are absent, matching cold). Test file:
`mlwh/sync_real_schema_test.go` (use the real `JSON_TABLE` source, not the
planned-row mock - see the test-routing CAUTION). Depends on Phase 2
harness.

- [ ] implemented
- [ ] reviewed

### Item 3.2: B2 - composite-recovery scoping efficiency and no-op

spec.md section: B2

Add the test helper `withSyncCompositeRecoveryChunkSizeForTest(t, size)`
(overrides the Phase-3 chunk-size `var`, restores on cleanup) and prove
the scoping and no-op behaviour over the item-3.1 implementation. Covering
all 4 acceptance tests from B2 (with N changed rows of which K are
multi-component within one chunk, the union of bound args across the
issued composite-recovery queries equals exactly those K `id_iseq_product`
values, each query a literal `IN`-list, none the broad
correlated-subquery form; with K exceeding a forced-small chunk size, more
than one query is issued, each a literal `IN`-list of at most `size` args,
their union exactly the K candidates; zero multi-component changed rows
issues NO composite-recovery query at all; a zero-row warm window on
`openRecordingSQLiteSyncTestCache` gives `SyncReport{0,0}`, no
`iseq_product_metrics_mirror` write, exactly one `sync_state` upsert,
`high_water` preserved, `last_run` advanced). Test files:
`mlwh/sync_test.go`, `mlwh/sync_real_schema_test.go`. Depends on 3.1.

- [ ] implemented
- [ ] reviewed

For these sequential items, a single review pass after each item is
acceptable; the reviewer must confirm Phase 1 scans the NULLABLE fetch
struct (not `iseqProductMetricsSyncRow`), Phase 3 issues only literal
chunked `IN`-list recovery queries (never a correlated subquery), the warm
mirror is byte-identical to cold, and the E1 arg counts match the registry
(incremental 1, from-cursor 3, composite recovery 1).

## Ordering and dependency notes

- Depends on Phase 2 (the parity harness and SQLite rewriter). Do not
  start until Phase 2 is reviewed.
- 3.1 and 3.2 are sequential: both edit the warm `iseq_product_metrics`
  path in `sync.go`; 3.2 asserts scoping/no-op over 3.1's implementation.
- The E1 `iseq_product_metrics` registry updates and the
  `TestRealworldA3...` revision land in 3.1 (with the SELECT reshape) so
  no phase is left red; Phase 4 only ADDS the registry coverage test, it
  does not re-edit these entries.
- `high_water` for `iseq_product_metrics` is the max source
  `last_changed`, so a zero-change window PRESERVES it (spec Appendix).
- Cold-path, QC-mapping, and mirror-write batch tests are untouched (spec
  E2, verified).
