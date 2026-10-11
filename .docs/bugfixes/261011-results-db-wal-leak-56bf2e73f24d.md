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
