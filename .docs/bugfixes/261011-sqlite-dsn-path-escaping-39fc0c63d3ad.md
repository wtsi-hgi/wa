# Bugfix: SQLite DSN does not escape file paths

- Branch: `sqlitedsn-723fb158`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-sqlitedsn`
- Priority: high (privacy). Reported to the user on discovery.

## Items

- [ ] `sqliteWritableDSN` in `mlwh/cache.go` builds
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
