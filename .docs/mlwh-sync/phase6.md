# Phase 6: Performance

Ref: [spec.md](spec.md) sections E1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 6.1: E1 - 2x per-table cold-sync budget

spec.md section: E1

Add an integration test in `mlwh/integration_test.go` gated by
`MLWH_SYNC_PERF_TEST=1` plus `WA_MLWH_DSN` and
`WA_MLWH_PASSWORD`. The test:

- times the unbuffered streaming of each of the five source
  queries via `*sql.DB.QueryContext` against the live MLWH
  replica (counting rows only) to produce `streamDuration[t]`;
- times `Client.Sync` end-to-end against a fresh empty SQLite
  cache file under `t.TempDir()` to produce `syncDuration[t]`
  per-table from the returned `SyncReport`;
- asserts `syncDuration[t] <= 2 * streamDuration[t]` for every
  table.

Skips cleanly with `MLWH_SYNC_PERF_TEST not set` when the gate is
unset and `WA_MLWH_DSN not set` when the DSN is missing. Covers
all 3 acceptance tests from E1.

- [ ] implemented
- [ ] reviewed
