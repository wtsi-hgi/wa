# Bugfix: sync retry test data race

- Branch: `syncrace-bd100063`
- Base: `origin/develop` at `d35401d11b5c142fcfd74080c8db175ed8342ec2`,
  rebased from `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-syncrace`

## Items

- [x] `TestClientSyncTwoTablesReconnectIndependently` (`mlwh/sync_test.go`)
      has a data race under `-race`: the test passes one `strings.Builder` as
      `syncRetryWriter` and appends to an unguarded `waits` slice in
      `syncRetrySleep`, while `Client.Sync` syncs tables in parallel, so
      `emitSyncRetry` (`mlwh/sync.go`) and the sleep hook run on several
      goroutines at once. Test-only: production writes to `os.Stderr`.
    - Source: phase 2 batch 3 review on branch `usage`
      (`.docs/usage/phase2.md`, items 2.5/2.6), reported as incidental.
    - Evidence: the reviewer's full `go test -race ./mlwh` run reported
      `WARNING: DATA RACE` in this test. A targeted 50-run rerun of only
      this test with `-race -count 50` passed (exit 0), so the race is
      intermittent and needs a higher-rate reproduction before fixing.
    - Independent of the usage feature: neither file is changed on `usage`.
    - Other `syncRetryWriter: &stderr` tests in `mlwh/sync_test.go` may share
      the pattern; check them when fixing.
    - Red command, run from `mlwh/` (exit 1 when red):
      `CGO_ENABLED=1 go test -tags netgo -race -count 50 -run '^TestClientSyncTwoTablesReconnectIndependently$' .`
    - Rate on base `d35401d` (4-CPU host): `-count 50` failed 4/4 processes
      (one each at `-cpu` 1, 2, 4 and 8, about 30 s each); `-count 10` failed
      4/5; `-count 1` failed 1/20.
    - Each failing process reports five races, all on test-owned state.
      Excerpt: `WARNING: DATA RACE` / `Read at ... by goroutine 381:` /
      `strings.(*Builder).Write()` / `fmt.Fprintf()` /
      `mlwh.(*Client).emitSyncRetry()` (`mlwh/sync.go:2867`) /
      `mlwh.(*Client).syncTable()` / `syncTablesInParallel.func1()`; and
      `TestClientSyncTwoTablesReconnectIndependently.func1.3()`
      (`mlwh/sync_test.go:4515`, the `waits` append) /
      `mlwh.(*Client).sleepSyncRetry()`.
    - No production race: production `emitSyncRetry` makes one `Fprintf`
      to `os.Stderr`, and the default sleep uses a per-call timer.
    - The other `syncRetryWriter: &stderr` tests (B5.1, B5.2, B5.3 and the
      reconnect-budget test) sync only `sample` through
      `syncSelectedTablesForTest`, which calls `syncTable` sequentially, so
      their hooks run on one goroutine.
    - Fixed in `mlwh/sync_test.go` (test-only; production unchanged): a
      mutex-guarded `lockedBufferForTest` replaces the shared
      `strings.Builder`, a mutex guards `waits`, and the sleep hook holds
      each retry until both tables are retrying (10 s timeout fails the
      test instead of hanging), so the retries always overlap.
    - After: the red command exits 0 with no `DATA RACE` at `-cpu` 1, 2, 4
      and 8. With the barrier, removing the `waits` guard fails 10/10 at
      `-count 1` and removing the writer guard fails 14/20 (the `fmt`
      printer pool sometimes orders the two writes for the race detector).

- [ ] `emitSyncReport` (`mlwh/sync.go`, around line 2633, called around line 2462) writes to `syncReportWriter` from each table's sync goroutine, but
      `SetSyncReportWriter` (`mlwh/cache.go`, around line 268) doesn't say the
      writer must be safe for concurrent use. Production passes
      `cmd.OutOrStdout()` (normally `os.Stdout`, which is safe), but a caller
      passing a `bytes.Buffer` would race. Same concurrent-writer flaw as the
      item above, so it shares this branch.
    - Source: bugfix of the item above, reported as incidental.
    - Evidence: code reading; no test hits it today.
    - Red harness: a throwaway copy of `TestClientSyncFiveTableParallelReports`
      that calls `client.SetSyncReportWriter(&out)` with a plain
      `strings.Builder` before `client.Sync`, built with
      `CGO_ENABLED=1 go test -tags netgo -race -c ./mlwh` and run with
      `-test.run '^TestScratchSyncReportWriterRace$' -test.count 1`.
    - Rate on `f8e8fc5`: 20/20 processes failed (exit 1) with
      `WARNING: DATA RACE` / `strings.(*Builder).WriteString()` /
      `io.WriteString()` / `mlwh.(*Client).emitSyncReport()`
      (`mlwh/sync.go:2638`) / `syncTablesInParallel.func1()`
      (`mlwh/sync.go:2462`), against a previous write from another table's
      goroutine on the same path.
