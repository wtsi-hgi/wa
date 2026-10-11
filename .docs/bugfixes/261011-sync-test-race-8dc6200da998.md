# Bugfix: sync retry test data race

- Branch: `syncrace-bd100063`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-syncrace`

## Items

- [ ] `TestClientSyncTwoTablesReconnectIndependently` (`mlwh/sync_test.go`)
  has a data race under `-race`: the test passes one `strings.Builder` as
  `syncRetryWriter` and appends to an unguarded `waits` slice in
  `syncRetrySleep`, while `Client.Sync` syncs tables in parallel, so
  `emitSyncRetry` (`mlwh/sync.go`) and the sleep hook run on several
  goroutines at once. Test-only: production writes to `os.Stderr`.
  - Source: phase 2 batch 3 review on branch `usage`
    (`.docs/usage/phase2.md`, items 2.5/2.6), reported as incidental.
  - Evidence: the reviewer's full `go test -race ./mlwh` run reported
    `WARNING: DATA RACE` in this test. A targeted rerun,
    `CGO_ENABLED=1 go test -race -tags netgo -count 50 ./mlwh -run
    TestClientSyncTwoTablesReconnectIndependently`, passed (exit 0), so
    the race is intermittent and needs a higher-rate reproduction before
    fixing.
  - Independent of the usage feature: neither file is changed on `usage`.
  - Other `syncRetryWriter: &stderr` tests in `mlwh/sync_test.go` may share
    the pattern; check them when fixing.
