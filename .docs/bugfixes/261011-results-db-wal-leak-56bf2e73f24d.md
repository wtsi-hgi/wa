# Bugfix: run-dev results DB leaves WAL files

- Branch: `resultswal-867e6b0f`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-resultswal`

## Items

- [ ] `run-dev.sh` test mode removes only the ephemeral results DB file
  (`rm -f "$DB_PATH"` in cleanup) and not its `-wal`/`-shm` files,
  and the Makefile `clean-test-tmp` target removes only
  `.tmp/results-dev.*.sqlite`. The feedback and usage ephemeral DBs
  already remove all three files.
  - Source: phase 3 item 3.3 on branch `usage`
    (`.docs/usage/phase3.md`), reported as incidental.
  - Evidence: `ls .tmp | grep -c 'results-dev.*sqlite-\(wal\|shm\)'` in
    the main clone printed `104` (exit 0), with dates from 2026-06-28 to
    2026-10-11 and no matching `.sqlite` files left beside them.
  - Independent of the usage feature: the results DB lines are unchanged on
    `usage`.
- [ ] `run-dev.sh` test mode removes only the ephemeral MLWH cache file
  (`rm -f "$MLWH_CACHE_PATH"`) and not its `-wal`/`-shm` files, and
  `make clean-test-tmp` never removes `.tmp/mlwh-test.*.sqlite*`. Same
  ephemeral-SQLite cleanup gap as the item above, so it shares this branch.
  - Source: phase 4 user-path verification on branch `usage`
    (`.docs/usage/phase4.md`), reported as incidental.
  - Evidence: `ls .tmp | grep -c '^mlwh-test\..*sqlite-\(wal\|shm\)'` in the
    main clone printed `152` (exit 0), for example
    `mlwh-test.0hMpbs.sqlite`, `mlwh-test.0hMpbs.sqlite-shm` and
    `mlwh-test.0hMpbs.sqlite-wal`.
