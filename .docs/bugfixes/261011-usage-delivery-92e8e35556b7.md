# Delivery queue: usage metrics

- Branch: `usage`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, this checklist
- Worktree: `.` (main clone)
- Origin work: `.docs/usage/phase1.md` to `.docs/usage/phase6.md`

## Deliver queue

| Branch                   | Worktree              | PR   | Target    | Depends on | Head      | Status     | Waits on                  | Next action                     |
| ------------------------ | --------------------- | ---- | --------- | ---------- | --------- | ---------- | ------------------------- | ------------------------------- |
| `usage`                  | `.`                   | none | `develop` | none       | `9f3d79b` | PR reviews | spec-aware review pass 2  | converge reviews, push, open PR |
| `syncrace-bd100063`      | `../wa-syncrace`      | none | `develop` | none       | `18cf0b6` | recorded   | owner completion boundary | reproduce and fix via bugfix    |
| `resultswal-867e6b0f`    | `../wa-resultswal`    | none | `develop` | none       | `1c8afaa` | recorded   | owner completion boundary | reproduce and fix via bugfix    |
| `frontendtsc-69d13e03`   | `../wa-frontendtsc`   | none | `develop` | none       | `87fd1d6` | recorded   | owner completion boundary | reproduce and fix via bugfix    |
| `verifyrev-45b2c5f7`     | `../wa-verifyrev`     | none | `develop` | none       | `2b7eae0` | recorded   | owner completion boundary | reproduce and fix via bugfix    |
| `menurace-39ecd8d1`      | `../wa-menurace`      | none | `develop` | none       | `b7565f2` | recorded   | owner completion boundary | reproduce and fix via bugfix    |
| `serveshutdown-cf1cb518` | `../wa-serveshutdown` | none | `develop` | none       | `a5e6c44` | recorded   | owner completion boundary | reproduce and fix via bugfix    |
| `rundevstate-64ed3f75`   | `../wa-rundevstate`   | none | `develop` | none       | `3281ef9` | recorded   | owner completion boundary | reproduce and fix via bugfix    |
| `pnpmfilter-90a8f24a`    | `../wa-pnpmfilter`    | none | `develop` | none       | `c469daa` | recorded   | owner completion boundary | reproduce and fix via bugfix    |

## Incidental handoffs

- Phase 2 batch 3 review: data race in
  `TestClientSyncTwoTablesReconnectIndependently`.
    - Branch: `syncrace-bd100063`
    - Checklist: `.docs/bugfixes/261011-sync-test-race-8dc6200da998.md`
    - Entry commit: `18cf0b660783df7043ba4cd3ff40aa77ab7e0549`
- Phase 3 item 3.3: `run-dev.sh` and `make clean-test-tmp` leave the
  ephemeral results DB's `-wal`/`-shm` files behind.
    - Branch: `resultswal-867e6b0f`
    - Checklist: `.docs/bugfixes/261011-results-db-wal-leak-56bf2e73f24d.md`
    - Entry commit: `46270cb99761ad9c332fca516310a5bd2d561ae2`
- Phase 4 items 4.1/4.2: 12 pre-existing `tsc --noEmit` errors in frontend
  test and e2e files, with no typecheck gate.
    - Branch: `frontendtsc-69d13e03`
    - Checklist: `.docs/bugfixes/261011-frontend-tsc-errors-67a818f2fe4e.md`
    - Entry commit: `87fd1d69e7586bfc0918a075544ef29ac4f5cb3b`
- Phase 4 verification: run-dev test MLWH cache also leaks `-wal`/`-shm`
  and `make clean-test-tmp` misses `mlwh-test.*`; appended to the WAL branch.
    - Branch: `resultswal-867e6b0f`
    - Checklist: `.docs/bugfixes/261011-results-db-wal-leak-56bf2e73f24d.md`
    - Entry commit: `1c8afaaeee7323a8e409d084ffe90ffc7d313a26`
- Phase 4 verification: verify-wa `launch.sh` writes an unquoted
  `REVISION` when the worktree is dirty.
    - Branch: `verifyrev-45b2c5f7`
    - Checklist: `.docs/bugfixes/261011-verify-launch-revision-0abe11bd0f8d.md`
    - Entry commit: `2b7eae0d7fe72a4f30b860fdd918193cfd7c6fcc`
- Phase 4 verification: admin account-menu links appear about 230 ms after
  login.
    - Branch: `menurace-39ecd8d1`
    - Checklist: `.docs/bugfixes/261011-auth-menu-admin-links-delay-e00507b6b184.md`
    - Entry commit: `b7565f2847702177885dbc5b851d8f83542ea634`
- Spec-aware PR review pass 1: `wa mlwh serve` closes its stores before
  in-flight requests finish (`StartHTTP` does not wait for `Shutdown`).
    - Branch: `serveshutdown-cf1cb518`
    - Checklist: `.docs/bugfixes/261011-serve-shutdown-order-7b5b276cd3b9.md`
    - Entry commit: `a5e6c446eb6d4f5751edbf9cd5a6de41d8df1d20`
- Spec-aware PR review pass 1: `TestRunDev*` tests clobber a running
  verify-wa stack's `.tmp` state in the same checkout.
    - Branch: `rundevstate-64ed3f75`
    - Checklist: `.docs/bugfixes/261011-rundev-tests-shared-state-bad805d9af8d.md`
    - Entry commit: `3281ef92624abd9f218c9a3f0395d46fc91adbeb`
- Spec-aware PR review pass 7: `pnpm test -- <files>` ignores the file
  filter and runs the whole suite plus the scaffold build.
    - Branch: `pnpmfilter-90a8f24a`
    - Checklist: `.docs/bugfixes/261011-pnpm-test-filter-110fa8768b88.md`
    - Entry commit: `c469daa456fdb4fc8d4130986eb9b44fa63d70d3`
