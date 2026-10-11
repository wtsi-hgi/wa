# Bugfix: file: MLWH cache paths open the wrong file

- Branch: `filecache-00de9195`
- Base: `origin/develop` at `d35401d11b5c142fcfd74080c8db175ed8342ec2`
  (rebased from `73ebdaa721d5cfe478e22e9ee504061ad30b1492`).
- Dependency: `sqlitedsn-723fb158` (PR #40) merged into `origin/develop`
  as `a6d0876`; the dependency hold is released. Publication still follows
  the queue owner's push authority.
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-filecache`
- Priority: low; confirmed through the CLI.

## Items

- [x] The SQLite DSN builders in `mlwh/cache.go` (`sqliteWritableDSN`,
      `sqliteReadOnlyDSN`, `sqliteLockDSN`) do not pass `file:` inputs
      through, although `looksLikeMySQLDSN` and `cmd/mlwh.go` accept a
      `file:` MLWH cache path as SQLite. A probe on branch
      `sqlitedsn-723fb158` opened `file:x.db` as a file literally named
      `file:x.db`; the code on this base behaves the same, so
      `--mlwh-cache file:/abs/x.db` would open the wrong file.
    - Source: bugfix of `sqlitedsn-723fb158`
      (`.docs/bugfixes/261011-sqlite-dsn-path-escaping-39fc0c63d3ad.md`),
      reported as incidental.
    - Next step: confirm through `wa mlwh serve --mlwh-cache file:...`; then
      either pass `file:` through or reject it like `--feedback-db` does.
    - Depends on `sqlitedsn-723fb158` if the fix touches the same builders;
      rebase after it merges. Released: rebased onto `d35401d` after #40
      merged.
    - Confirmed on `d35401d`. Red command: a script that builds `wa`, runs
      `wa mlwh serve --mlwh-cache file:x.db` (bounded by `timeout 5`) in an
      empty directory and checks that `x.db`, not `file:x.db`, exists; then
      creates a cache with a plain absolute path and serves it again as
      `--mlwh-cache file:<abs>/x.db`, expecting it to stay up (exit 124 from
      `timeout`). Exit 1:

        ```text
        relative: exit=124 files=file:x.db
        FAIL relative: expected x.db, not file:x.db
        absolute: exit=1 output: Error: open mlwh cache: mlwh: ping sqlite cache: unable to open database file: out of memory (14)
        FAIL absolute: serve did not stay up
        ```

    - Decision: pass `file:` URIs through, as `cmd/results.go`
      (`resultsSQLiteDSN`) and `mlwhdiff/store.go` (`storeDSN`) do, rather
      than rejecting them. `looksLikeMySQLDSN` and
      `ensureMLWHSyncCacheDirectory` already treat `file:` as a SQLite cache,
      and the `--mlwh-cache` help and README promise only "a path or MySQL
      DSN", so accepting `file:` keeps current documented behaviour.

    - Fixed. Files: `mlwh/cache.go`, `mlwh/cache_test.go`,
      `mlwh/feedback_store.go`, `mlwh/feedback_store_test.go`,
      `mlwh/usage_store.go`, `mlwh/usage_store_test.go`.
    - Approach: the three builders pass `file:` URIs through, drop any
      `#fragment`, keep the user's query, and merge per-handle params with
      `mergeSQLiteURIParams`. Params match by key, and `_pragma` params by
      case-insensitive pragma name after percent-decoding. The user's params
      win over the writable defaults (`mode=rwc`, `journal_mode(WAL)`,
      `busy_timeout(5000)`). The read handle forces `mode=ro`. The lock
      handle opens `<path>.sync-lock` and forces `mode=rwc` and
      `busy_timeout(0)`. Merging is needed because modernc.org/sqlite
      v1.48.0 orders duplicate `busy_timeout` pragmas unstably and rejects a
      repeated `mode` that widens access. Plain paths and `:memory:` are
      unchanged. The feedback and usage stores now call
      `sqliteWritablePathDSN`, so they keep opening the exact 0600 file they
      create; the CLI still rejects `file:` for `--feedback-db` and
      `--usage-db`.
    - Regression tests: `TestOpenCacheSQLiteFileURI` (relative, absolute,
      fragments, user params on an existing cache, unrelated pragma on a new
      cache, percent-encoded pragma keys and values), plus `file:` cases in
      `TestFeedbackStoreA2` and
      `TestUsageStoreSQLitePathWithURISpecialCharacters`. Mutants of each
      merge rule fail these tests.
    - The red command exits 0 after the fix:

        ```text
        relative: exit=124 files=x.db
        absolute: exit=124 output:
        ```

    - Incidental, reported to the queue owner and not fixed here:
      `--mlwh-cache file::memory:` is not treated as `:memory:`, so the read
      and write handles probably open separate in-memory databases and the
      lock opens a disk file named `:memory:.sync-lock`; `mlwh.OpenCache`
      does not trim whitespace from library callers' paths (the CLI does).
