# Delivery queue: usage metrics

- Branch: `usage`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, this checklist
- Worktree: `.` (main clone)
- Origin work: `.docs/usage/phase1.md` to `.docs/usage/phase6.md`

## Deliver queue

| Branch                   | Worktree            | PR                                            | Target    | Depends on        | Head      | Status                              | Waits on    | Next action               |
| ------------------------ | ------------------- | --------------------------------------------- | --------- | ----------------- | --------- | ----------------------------------- | ----------- | ------------------------- |
| `usage`                  | removed after merge | [#39](https://github.com/wtsi-hgi/wa/pull/39) | `develop` | none              | `879dcf8` | merged as `5e851cd`                 | none        | done; user tags `v0.11.0` |
| `syncrace-bd100063`      | removed after merge | [#47](https://github.com/wtsi-hgi/wa/pull/47) | `develop` | none              | `42619e6` | merged as `1757d9a`                 | none        | done                      |
| `resultswal-867e6b0f`    | removed after merge | [#42](https://github.com/wtsi-hgi/wa/pull/42) | `develop` | #39 (merged)      | `a5f3e9d` | merged as `d35401d`                 | none        | done                      |
| `frontendtsc-69d13e03`   | removed after merge | [#43](https://github.com/wtsi-hgi/wa/pull/43) | `develop` | #39 (merged)      | `47cdfd7` | merged as `407579c`                 | none        | done                      |
| `verifyrev-45b2c5f7`     | removed after merge | [#44](https://github.com/wtsi-hgi/wa/pull/44) | `develop` | #39 (merged)      | `2bb7e0c` | merged as `f89df69`                 | none        | done                      |
| `menurace-39ecd8d1`      | removed after merge | [#45](https://github.com/wtsi-hgi/wa/pull/45) | `develop` | #43, #44 (merged) | `007c0d6` | merged as `e8bbf25`                 | none        | done                      |
| `serveshutdown-cf1cb518` | removed after merge | [#41](https://github.com/wtsi-hgi/wa/pull/41) | `develop` | #39, #40 (merged) | `6a4d1ab` | merged as `fcefbc8`                 | none        | done                      |
| `rundevstate-64ed3f75`   | removed after merge | [#46](https://github.com/wtsi-hgi/wa/pull/46) | `develop` | none              | `5d80ca2` | merged as `a2b63f3`                 | none        | done                      |
| `pnpmfilter-90a8f24a`    | removed after merge | [#48](https://github.com/wtsi-hgi/wa/pull/48) | `develop` | #43 (merged)      | `1298557` | merged as `b691615`                 | none        | done                      |
| `filecache-00de9195`     | `../wa-filecache`   | [#49](https://github.com/wtsi-hgi/wa/pull/49) | `develop` | #40 (merged)      | rebased   | rebased on `b691615`; last in queue | CI, Copilot | ask user to merge #49     |
| `sqlitedsn-723fb158`     | removed after merge | [#40](https://github.com/wtsi-hgi/wa/pull/40) | `develop` | #39 (merged)      | `8727521` | merged as `a6d0876`                 | none        | done                      |

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
- Spec-aware PR review pass 9: `sqliteWritableDSN` does not escape the path,
  so `#` or `?` in a DB file name sends data to a different, 0644 file
  (privacy; high priority).
    - Branch: `sqlitedsn-723fb158`
    - Checklist: `.docs/bugfixes/261011-sqlite-dsn-path-escaping-39fc0c63d3ad.md`
    - Entry commit: `a1d73c49b1ec721bbe0ee84b2b4140993882efa9`
- PR #40 bugfix: `file:` MLWH cache paths are opened as literal file names.
    - Branch: `filecache-00de9195`
    - Checklist: `.docs/bugfixes/261011-file-uri-cache-path-*.md`
    - Entry commit: `c0ed799222bf529d5619ac3d91aad162d76dba55`

## Deferred follow-ups

- `mlwh.OpenCache`: a `file:` URI repeating `mode` with a non-memory last
  value (for example `file:x?mode=memory&mode=rwc`) fails to open because the
  writable DSN keeps both `mode` params. Obscure spelling; recorded in
  `.docs/bugfixes/261011-file-uri-cache-path-7976ad1d2883.md` on PR #49.
