# Bugfix: SQLite DSN does not escape file paths

- Branch: `sqlitedsn-723fb158`
- Base: `origin/develop` at `5e851cdad10a2cc9c77a8c73c08c4b263f18d34a`
  (rebased from `73ebdaa721d5cfe478e22e9ee504061ad30b1492`)
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-sqlitedsn`
- Priority: high (privacy). Reported to the user on discovery.

## Items

- [x] `sqliteWritableDSN` in `mlwh/cache.go` builds
      `file:<path>?mode=rwc&...` without URI-escaping the path, so a `#` or
      `?` in the file name changes which file SQLite opens. The pre-created
      0600 file is left empty while the real database and its `-wal`/`-shm`
      files are created at a truncated path with the default mode (0644 under
      a 022 umask). The feedback DB and MLWH cache use the same helper; on
      branch `usage` the usage DB does too.
    - Source: spec-aware PR review pass 9 of branch `usage`, reported as
      incidental.
    - Evidence: on branch `usage` a reviewer opened a usage store at
      `<tmp>/u#1.sqlite`: `u#1.sqlite` was created empty with mode 0600, and
      the data went to `<tmp>/u`, `u-wal` and `u-shm` with mode 0644.
    - Impact: databases holding usernames, IPs and agent feedback can become
      world-readable, and data lands at an unexpected path.
    - Sibling builders with the same flaw: `sqliteLockDSN` and
      `sqliteReadOnlyDSN` in `mlwh/cache.go`, and `resultsSQLiteDSN` in
      `cmd/results.go` (the `wa results` database). A `%23` in a name is
      also percent-decoded, so `f%231.sqlite` opens `f#1.sqlite`.
    - Red command (throwaway harnesses, not committed, that open the
      feedback store, MLWH cache and results DB at `f#1.sqlite`, `f?1.sqlite`
      and `f%231.sqlite` in a temp dir and assert the data lands in that
      exact file):
      `CGO_ENABLED=1 go test -tags netgo --count 1 -run 'TestRed.*DSNPathEscaping' ./mlwh/ ./cmd/`
      exited 1:

        ```text
        feedback "f#1.sqlite": files=[f f#1.sqlite] size=0 err=<nil>
        cache "f#1.sqlite": files=[f] size=-1 err=stat <tmp>/f#1.sqlite: no such file or directory
        feedback "f?1.sqlite": files=[f f?1.sqlite] size=0 err=<nil>
        cache "f?1.sqlite": files=[f] size=-1 err=stat <tmp>/f?1.sqlite: no such file or directory
        feedback "f%231.sqlite": files=[f#1.sqlite f%231.sqlite] size=0 err=<nil>
        cache "f%231.sqlite": files=[f#1.sqlite] size=-1 err=stat <tmp>/f%231.sqlite: no such file or directory
        FAIL	github.com/wtsi-hgi/wa/mlwh
        results "r#1.db": files=[r] err=stat <tmp>/r#1.db: no such file or directory
        results "r?1.db": files=[r] err=stat <tmp>/r?1.db: no such file or directory
        results "r%231.db": files=[r#1.db] err=stat <tmp>/r%231.db: no such file or directory
        FAIL	github.com/wtsi-hgi/wa/cmd
        ```

    - Further sibling found during the fix: `mlwhdiff.OpenStore` in
      `mlwhdiff/store.go` passes the raw path to `sql.Open("sqlite", path)`,
      and the driver cuts a non-`file:` name at the first `?`. Red command
      (throwaway harness opening the store at `d?1.db`, `d#1.db` and
      `d%231.db`):
      `CGO_ENABLED=1 go test -tags netgo --count 1 -run TestRedMLWHDiffStorePath ./mlwhdiff/`
      exited 1:

        ```text
        mlwhdiff "d?1.db": files=[d] err=stat <tmp>/d?1.db: no such file or directory
        FAIL	github.com/wtsi-hgi/wa/mlwhdiff
        ```

    - Fixed in `d6201ce` and `f53eae8`. New `internal/sqliteuri.File` escapes
      the path with `url.URL.EscapedPath` (so `?`, `#` and `%` are literal)
      and adds an empty authority to paths starting with `//`.
      `sqliteWritableDSN`, `sqliteLockDSN`, `sqliteReadOnlyDSN`,
      `resultsSQLiteDSN` and `mlwhdiff.OpenStore` (via `storeDSN`, `mode=rwc`)
      use it. `:memory:` and `file:` inputs to the results DB and mlwhdiff
      store pass through as before.
    - Files: `internal/sqliteuri/sqliteuri.go`,
      `internal/sqliteuri/sqliteuri_test.go`, `mlwh/cache.go`,
      `mlwh/cache_test.go`, `mlwh/feedback_store_test.go`, `cmd/results.go`,
      `cmd/results_serve_test.go`, `mlwhdiff/store.go`,
      `mlwhdiff/store_test.go`.
    - Regression tests open each store at `#`, `?` and `%23` names and assert
      the data, and only the expected `-wal`/`-shm`/`-journal`/`.sync-lock`
      siblings, land at that exact path; the feedback DB stays 0600. Reviewer
      PASS: red commands pass, 7 revert mutants all caught,
      `golangci-lint run` 0 issues, `go test -tags netgo --count 1 ./...` ok.

- [x] After the rebase onto `5e851cd` (#39), the usage DB opened by
      `OpenUsageStore` in `mlwh/usage_store.go` uses `sqliteWritableDSN`, but
      no test opens it at a `#`, `?` or `%23` name, so a regression there
      would go unnoticed.
    - Source: PR #40 rebase check requested by the queue owner.
    - Red command (throwaway harness, not committed, that opens the usage
      store at `u#1.sqlite`, `u?1.sqlite` and `u%231.sqlite` in a temp dir,
      records one event, closes it, and asserts the data and only its
      `-wal`/`-shm` siblings land at that exact path), run in a scratch copy
      of base `5e851cd`:
      `CGO_ENABLED=1 go test -tags netgo --count 1 -run TestRedUsageStoreDSNPathEscaping ./mlwh/`
      exited 1:

        ```text
        usage "u#1.sqlite": files=[u u#1.sqlite] size=0
        usage "u?1.sqlite": files=[u u?1.sqlite] size=0
        usage "u%231.sqlite": files=[u#1.sqlite u%231.sqlite] size=0
        usage DB data did not land at the exact path
        FAIL	github.com/wtsi-hgi/wa/mlwh
        ```

    - The same harness passes on this branch at `8138d8e`
      (`files=[u#1.sqlite] size=20480` and likewise for the other names), so
      the existing fix already covers the usage DB; the missing piece is a
      committed regression test.
    - Fixed by adding regression test
      `TestUsageStoreSQLitePathWithURISpecialCharacters`; no production code
      change.
    - Files: `mlwh/usage_store_test.go`.
    - Approach: for each of `u#1.sqlite`, `u?1.sqlite` and `u%231.sqlite`, the
      test opens the usage store in a temp dir with `openTestUsageStore`,
      records one event, and flushes it. It then asserts that the file at
      that exact path is non-empty with mode 0600 (pre-created by
      `createStoreFile`), that its `-wal` has no group/other bits, and that
      the dir holds only `name`, `name-wal` and `name-shm`.
    - Mutant evidence: in a scratch copy of this branch with
      `sqliteWritableDSN` reverted to the unescaped
      `"file:" + path + "?mode=rwc&..."` form,
      `CGO_ENABLED=1 go test -tags netgo --count 1 ./mlwh -run TestUsageStoreSQLitePathWithURISpecialCharacters`
      exited 1 for all three names:

        ```text
        <tmp>/mlwh/usage_store_test.go
        Line 461:
        Expected '0' to be greater than '0' (but it wasn't)!
        --- FAIL: TestUsageStoreSQLitePathWithURISpecialCharacters (0.08s)
        FAIL	github.com/wtsi-hgi/wa/mlwh
        ```

## Incidental handoffs

- `file:` MLWH cache paths are opened as literal file names.
    - Branch: `filecache-00de9195`
    - Checklist: `.docs/bugfixes/261011-file-uri-cache-path-7976ad1d2883.md`
    - Entry commit: `c0ed799222bf529d5619ac3d91aad162d76dba55`
