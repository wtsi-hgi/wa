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
- [x] PR #49 makes `file:` URI MLWH cache paths pass through to SQLite
      (`mlwh/cache.go`). But `--mlwh-cache file::memory:` (and any `file:`
      URI with `mode=memory`) is not treated like `:memory:`: the read and
      write handles likely open separate in-memory databases and the sync
      lock becomes a disk file `:memory:.sync-lock`. Also trim surrounding
      whitespace in `mlwh.OpenCache`'s path if trivial and consistent with
      the CLI (which already trims).
    - Source: incidental from the item above, routed back by the queue
      owner.
    - Confirmed on `da94daa`. Red command: a script that builds `wa`, serves
      each of `:memory:`, `file::memory:`, `file:x?mode=memory` and
      `file::memory:?cache=shared` with `timeout 6 wa mlwh serve
      --mlwh-cache <p>`, and expects `GET /studies` to return the same
      `cache_never_synced` error as `:memory:`. Exit 1:

        ```text
        :memory: exit=124 body={"code":"cache_never_synced",...}
        file::memory: exit=124 body={"code":"upstream_impaired","message":"mlwh: upstream database impaired: query study cache: SQL logic error: no such table: study_mirror (1)"}
        FAIL file::memory:
        file:x?mode=memory exit=1 body=<none> Error: open mlwh cache: mlwh: ping sqlite read-only cache: unable to open database file: out of memory (14)
        FAIL file:x?mode=memory
        file::memory:?cache=shared exit=124 body={"code":"cache_never_synced",...}
        ```

      `serve` never takes the sync lock, so the `:memory:.sync-lock` part is
      covered by a unit test on `sqliteLockDSN` rather than the CLI.
    - Decision: treat in-memory `file:` URIs exactly as `:memory:` (one
      shared handle, no sync lock) instead of rejecting them. `cache.go`
      accepts `:memory:` as a cache, the `--mlwh-cache` CLI accepts both
      `:memory:` and `file:` URIs, and SQLite defines these URIs as the same
      in-memory database, so rejecting only the URI spelling would be the
      more surprising choice.
    - Fixed. Files: `mlwh/cache.go`, `mlwh/cache_test.go`.
    - Approach: a new `isSQLiteMemoryPath` replaces the `== ":memory:"`
      checks in `openSQLiteCache`, `sqliteLockDSN` and `sqliteReadOnlyDSN`,
      so an in-memory path gets one shared handle and no sync lock. It
      matches `:memory:`, a `file:` URI whose percent-decoded path (before
      `?` or `#`) is `:memory:`, and a `file:` URI whose last
      percent-decoded `mode` param is `memory` or last `vfs` param is
      `memdb`. Keys and values are case-sensitive, as in SQLite.
      `OpenCache` now trims whitespace from `cfg.Path`, as the CLI does.
    - Regression tests: `TestOpenCacheSQLiteMemoryURI` (each in-memory
      spelling shares data between the write and read handles and leaves the
      working directory empty), `TestOpenCacheTrimsPathWhitespace`, and
      `?vfs=unix`, `?MODE=memory` and `?VFS=memdb` disk cases in
      `TestOpenCacheSQLiteFileURI`, which must keep a `.sync-lock` file.
      Mutants of each predicate rule fail these tests.
    - The red command exits 0 after the fix; every spelling returns
      `cache_never_synced` and no stray files appear.
    - Incidental, not fixed here: `file:x?mode=memory&mode=rwc` is a disk
      file to SQLite (last `mode` wins), but `OpenCache` fails with
      "unable to open database file: out of memory (14)" because the
      writable DSN keeps both user `mode` params.
