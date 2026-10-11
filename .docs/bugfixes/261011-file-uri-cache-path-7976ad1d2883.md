# Bugfix: file: MLWH cache paths open the wrong file

- Branch: `filecache-00de9195`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-filecache`
- Priority: low; not yet confirmed through the CLI.

## Items

- [ ] The SQLite DSN builders in `mlwh/cache.go` (`sqliteWritableDSN`,
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
    rebase after it merges.
