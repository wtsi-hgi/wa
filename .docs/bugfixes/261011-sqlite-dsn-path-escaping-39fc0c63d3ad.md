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
