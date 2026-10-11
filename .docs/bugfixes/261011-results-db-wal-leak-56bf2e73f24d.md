# Bugfix: run-dev results DB leaves WAL files

- Branch: `resultswal-867e6b0f`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-resultswal`

## Items

- [x] `run-dev.sh` test mode removes only the ephemeral results DB file
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
    - Red command (run-dev.sh), exit 1:

        ```bash
        CGO_ENABLED=1 go test -tags netgo --count 1 ./cmd/ \
          -run 'TestRunDevTestModeRemovesResultsDBSidecars'
        ```

        ```text
        * cmd/run_dev_test.go
        Line 1090:
        Expected: false
        Actual:   true
        --- FAIL: TestRunDevTestModeRemovesResultsDBSidecars (1.29s)
        ```

        Line 1090 is the post-SIGINT absence check; the `-wal` file remained.

    - Red command (Makefile):

        ```bash
        touch .tmp/results-dev.REDX.sqlite{,-wal,-shm} &&
          make clean-test-tmp && ls .tmp | grep REDX
        ```

        It printed `results-dev.REDX.sqlite-shm` and
        `results-dev.REDX.sqlite-wal`.

    - Fixed: `run-dev.sh` cleanup now runs
      `rm -f "$DB_PATH" "$DB_PATH-wal" "$DB_PATH-shm"`, and `clean-test-tmp`
      in `Makefile` also removes `.tmp/results-dev.*.sqlite-wal` and
      `.tmp/results-dev.*.sqlite-shm`, mirroring the feedback DB lines.
      `TestRunDevTestModeRemovesResultsDBSidecars` in `cmd/run_dev_test.go`
      encoded the repro; item 2 renamed it
      `TestRunDevTestModeRemovesEphemeralSQLiteSidecars`. Review: PASS.

- [x] `run-dev.sh` test mode removes only the ephemeral MLWH cache file
      (`rm -f "$MLWH_CACHE_PATH"`) and not its `-wal`/`-shm` files, and
      `make clean-test-tmp` never removes `.tmp/mlwh-test.*.sqlite*`. Same
      ephemeral-SQLite cleanup gap as the item above, so it shares this branch.
    - Source: phase 4 user-path verification on branch `usage`
      (`.docs/usage/phase4.md`), reported as incidental.
    - Evidence: `ls .tmp | grep -c '^mlwh-test\..*sqlite-\(wal\|shm\)'` in the
      main clone printed `152` (exit 0), for example
      `mlwh-test.0hMpbs.sqlite`, `mlwh-test.0hMpbs.sqlite-shm` and
      `mlwh-test.0hMpbs.sqlite-wal`.
    - Red command (run-dev.sh), exit 1. Item 1's test is extended to cover
      the MLWH test cache and renamed
      `TestRunDevTestModeRemovesEphemeralSQLiteSidecars`:

        ```bash
        CGO_ENABLED=1 go test -tags netgo --count 1 ./cmd/ \
          -run 'TestRunDevTestModeRemovesEphemeralSQLiteSidecars'
        ```

        ```text
        * cmd/run_dev_test.go
        Line 1096:
        Expected [.tmp/mlwh-test.qi06EZ.sqlite-wal .tmp/mlwh-test.qi06EZ.sqlite-shm] to be empty (but it wasn't)!
        --- FAIL: TestRunDevTestModeRemovesEphemeralSQLiteSidecars (1.28s)
        ```

        The results DB check on line 1095 passed.

    - Red command (Makefile):

        ```bash
        touch .tmp/mlwh-test.REDX.sqlite{,-wal,-shm} &&
          make clean-test-tmp && ls .tmp | grep REDX
        ```

        It printed `mlwh-test.REDX.sqlite`, `mlwh-test.REDX.sqlite-shm` and
        `mlwh-test.REDX.sqlite-wal`.

    - Fixed: `run-dev.sh` cleanup now runs
      `rm -f "$MLWH_CACHE_PATH" "$MLWH_CACHE_PATH-wal" "$MLWH_CACHE_PATH-shm"`
      inside the existing `MLWH_CACHE_EPHEMERAL` guard, and `clean-test-tmp`
      in `Makefile` now removes `.tmp/mlwh-test.*.sqlite` and its `-wal` and
      `-shm` files. The glob cannot match persistent caches such as
      `.tmp/mlwh-dev.sqlite`. `TestRunDevTestModeRemovesEphemeralSQLiteSidecars`
      in `cmd/run_dev_test.go` covers both ephemeral DBs in one run. Review:
      PASS.
