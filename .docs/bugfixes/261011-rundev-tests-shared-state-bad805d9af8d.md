# Bugfix: run-dev tests clobber a live verify-wa stack

- Branch: `rundevstate-64ed3f75`
- Base: `origin/develop` at `5e851cd0f63f9f1a3ccbbfc7d7edc0e0ed9a7a83`
  (rebased from `73ebdaa721d5cfe478e22e9ee504061ad30b1492`)
- Queue owner: delivery checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md` on branch
  `sqlitedsn-723fb158` (originally recorded from branch `usage`)
- Worktree: `../wa-rundevstate`

## Items

- [x] Running `go test -run TestRunDev ./cmd/` in a checkout that also has
      a verify-wa stack running breaks that stack: the tests deleted
      `.tmp/state-test/.wa-results-server.token` and rewrote
      `.tmp/wa-dev-key.pem`. The tests share the checkout's `.tmp` state
      directory with real run-dev stacks, and verify-wa's one-stack-per-checkout
      rule does not warn about this.
    - Source: spec-aware PR review of branch `usage`, reported as
      incidental.
    - Evidence: observed on branch `usage` while a verify-wa stack ran in
      the same checkout; not yet reproduced on this base.
    - Fix direction: give the tests an isolated state and key directory, or
      document the conflict in verify-wa.
    - Red command (from the checkout root; plants sentinels in the shared
      `.tmp`, runs one run-dev test, then checks the sentinels):

        ```bash
        mkdir -p .tmp/state-test
        printf 'sentinel-token\n' > .tmp/state-test/.wa-results-server.token
        printf 'sentinel-key\n' > .tmp/wa-dev-key.pem
        printf 'sentinel-cert\n' > .tmp/wa-dev-cert.pem
        ln -sfn /nonexistent/sentinel-wa .tmp/wa
        CGO_ENABLED=1 go test -tags netgo -count 1 -run TestRunDevUsageDB ./cmd/
        grep -qx sentinel-token .tmp/state-test/.wa-results-server.token &&
          grep -qx sentinel-key .tmp/wa-dev-key.pem &&
          grep -qx sentinel-cert .tmp/wa-dev-cert.pem &&
          [ "$(readlink .tmp/wa)" = /nonexistent/sentinel-wa ]
        ```

    - Red result on `5e851cd`: `go test` passed (exit 0) but every sentinel
      was clobbered (check exit 1):

        ```text
        CLOBBERED: .tmp/state-test/.wa-results-server.token
        CLOBBERED: .tmp/wa-dev-key.pem (-----BEGIN PRIVATE KEY-----...)
        CLOBBERED: .tmp/wa-dev-cert.pem
        CLOBBERED: .tmp/wa -> (symlink removed)
        ```

    - Fixed. `run-dev.sh` now reads its scratch directory from
      `WA_RUN_DEV_TMP_DIR` (default `$REPO_ROOT/.tmp`, so real users see no
      change); the dev TLS cert/key, `state-test`, built binaries, `wa`
      link, `wa.lock`, and ephemeral DBs follow it, and the test-mode cache
      guard accepts paths under it. The run-dev Go tests give each test its
      own `t.TempDir()` for it, unset the `.env.test`
      `WA_RESULTS_SERVER_CERT`/`KEY` paths that point at `.tmp`, and delete
      the per-test token instead of the checkout's.
    - Regression test: `TestRunDevTmpDirOverride` checks every artefact
      lands in the per-test directory; it fails on the old `run-dev.sh`.
      The red command passes with `TestRunDevUsageDB` and with the full
      `TestRunDev` suite.
    - Files: `run-dev.sh`, `cmd/run_dev_test.go`,
      `cmd/run_dev_modes_test.go`, `cmd/run_dev_tmp_dir_test.go`,
      `DEVELOPING.md`.

- [x] The run-dev tests don't set `WA_RUN_DEV_LOG_DIR`, so they replace a
      `make dev` session's `logs/results.log`, `logs/mlwh.log` and
      `logs/frontend.log` in the checkout, and one test asserts those files
      exist (around `cmd/run_dev_test.go` lines 2551-2553). Same shared-state
      theme as the item above, so it shares this branch.
    - Source: bugfix of the item above, reported as incidental.
    - Evidence: code reading of the tests and `run-dev.sh`'s log directory
      default.
    - Red command (from the checkout root):

        ```bash
        mkdir -p logs
        for f in results frontend mlwh; do
          printf 'sentinel-%s\n' "$f" > "logs/$f.log"
        done
        CGO_ENABLED=1 go test -tags netgo -count 1 -run TestRunDevUsageDB ./cmd/
        for f in results frontend mlwh; do
          grep -qx "sentinel-$f" "logs/$f.log" || echo "CLOBBERED: logs/$f.log"
        done
        ```

    - Red result on `29de3f2`: `go test` passed (exit 0) but all three
      sentinels were replaced (check exit 1):

        ```text
        CLOBBERED: logs/results.log ()
        CLOBBERED: logs/frontend.log ( [dev] )
        CLOBBERED: logs/mlwh.log ()
        ```

    - Fixed. `runDevEnvForTest`, which builds every run-dev invocation's
      environment in the cmd tests, now replaces any inherited
      `WA_RUN_DEV_LOG_DIR` with a `logs` directory inside the per-test
      `WA_RUN_DEV_TMP_DIR`; a test's own override still wins.
      `TestRunDevScript` checks the three logs there. `run-dev.sh` is
      unchanged.
    - Regression test: `TestRunDevLogDirOverride` checks run-dev reports
      and writes its logs in the per-test directory; it fails without the
      env change. The red command passes with `TestRunDevUsageDB` and the
      full `TestRunDev` suite.
    - Files: `cmd/run_dev_test.go`, `cmd/run_dev_tmp_dir_test.go`,
      `DEVELOPING.md`.

- [x] `make test`'s Playwright step runs a real `run-dev.sh --mode test` with
      the default `.tmp`, and `frontend/e2e/results-auth-helpers.ts` reads
      `.tmp/state-test`; afterwards `clean-test-tmp` deletes `.tmp/wa`. So
      `make test` can still break a verify-wa stack in the same checkout.
    - Source: bugfix of the item above, reported as incidental.
    - Evidence: code reading of the Makefile `test-e2e` target and the e2e
      helpers.
    - Red command (from the checkout root, with frontend dependencies
      installed): plant the same `.tmp` sentinels as the first item plus
      sentinel `logs/{results,frontend,mlwh}.log`, run the `test-e2e` recipe
      limited to one spec, run `make clean-test-tmp`, then check the
      sentinels:

        ```bash
        set -a; . ./.env.test; set +a
        (cd frontend && pnpm exec playwright test e2e/result-id-copy-chip.spec.ts)
        make clean-test-tmp
        ```

    - Red result on `8bfc014`: the spec passed (exit 0) and
      `clean-test-tmp` exited 0, but every sentinel was replaced or removed
      (check exit 1):

        ```text
        CLOBBERED: .tmp/state-test/.wa-results-server.token
        CLOBBERED: .tmp/wa-dev-key.pem (-----BEGIN PRIVATE KEY-----)
        CLOBBERED: .tmp/wa-dev-cert.pem
        CLOBBERED: .tmp/wa -> ''
        CLOBBERED: logs/results.log
        CLOBBERED: logs/frontend.log
        CLOBBERED: logs/mlwh.log
        ```

    - The e2e run-dev also replaces a `make dev` session's `logs/`, the same
      clobbering as the previous item, so this fix covers its log directory
      too.
    - Fixed. The e2e run now owns `.tmp/e2e/` (set once in
      `frontend/e2e/run-dir.ts`): `playwright.config.ts` passes
      `WA_RUN_DEV_TMP_DIR`, `WA_RUN_DEV_LOG_DIR` and the cert/key paths to its
      `run-dev.sh` through `webServer.env`, the ports cache moved there, and
      the e2e helpers and the timezone repro spec read the binary, token,
      cert and results DB from it. `clean-test-tmp` now removes only
      `.tmp/e2e`. `.env.test` keeps its `.tmp/wa-dev-*.pem` values because
      `wa --env test` (see `260522-1.md`) and verify-wa's `launch.sh` rely on
      them; the Playwright run overrides them.
    - Regression test: `frontend/tests/e2e-run-dir.test.ts` checks the
      Playwright run-dev env points inside a dedicated `.tmp` subdirectory and
      that `make clean-test-tmp` leaves a stack's shared `.tmp` files and
      `logs/` alone; both tests fail without the fix. The red command passes
      for the single spec and for the full `make test-e2e` (107 passed, 2
      existing `test.fixme` skips).
    - Files: `frontend/e2e/run-dir.ts`, `frontend/playwright.config.ts`,
      `frontend/e2e/results-auth-helpers.ts`,
      `frontend/e2e/result-date-timezone-repro.spec.ts`,
      `frontend/tests/e2e-run-dir.test.ts`, `Makefile`, `.env.test`,
      `DEVELOPING.md`, `.agents/skills/verify-wa/SKILL.md`.

- [x] Dev-mode run-dev tests that don't set `XDG_STATE_HOME` inherit the
      caller's, so a real results server started by one could write its token
      into the user's real state directory. Unconfirmed: most of those tests
      use a fake server. Confirm before fixing; record the evidence either way.
    - Source: bugfix of the item above, reported as incidental.
    - Confirmed. Red command (from the checkout root; `$STATE` is any
      scratch directory standing in for the caller's state directory):

        ```bash
        printf 'sentinel-token\n' > "$STATE/.wa-results-server.token"
        XDG_STATE_HOME="$STATE" CGO_ENABLED=1 go test -tags netgo -count 1 \
          -run '^TestRunDevScript$' ./cmd/
        grep -qx sentinel-token "$STATE/.wa-results-server.token"
        ```

    - Red result on `1f40eb0`: `go test` passed (exit 0, 46.7s) but the
      caller's sentinel token was deleted (`grep` exit 2, "No such file or
      directory"). The full `-run TestRunDev` suite does the same;
      `TestRunDevUsageDB` and `TestRunDevFeedbackDB` leave it alone.
    - Cause: only `--mode test` points `XDG_STATE_HOME` at
      `$TMP_DIR/state-test`. In dev and prod mode the real `wa results serve`
      inherited the caller's value, used its `.wa-results-server.token`, and
      go-authserver's `Server.Stop()` deleted it on shutdown. With
      `XDG_STATE_HOME` unset it fell back to `$HOME` and deleted
      `$HOME/.wa-results-server.token`. Eleven dev/prod-mode Convey blocks
      across `TestRunDevScript`, `TestRunDevScriptF1DevCertificates`,
      `TestRunDevScriptDevCertificateSubjectAltNames` and
      `TestRunDevScriptReportsResultsBindAddress` did this.
    - Fixed. `runDevEnvForTest` replaces any inherited `XDG_STATE_HOME` with
      a `state` directory inside the per-test `WA_RUN_DEV_TMP_DIR`, which
      also stops the `$HOME` fallback; a test's own value still wins. The
      failing-MLWH dev-mode scenario moved into
      `runRunDevDevModeWithFailingMLWHForTest` so the regression test can
      reuse it; the original subtest keeps all its assertions. `run-dev.sh`
      is unchanged.
    - Regression test: `TestRunDevStateHomeOverride` plants a token in the
      test process's `XDG_STATE_HOME`, runs that scenario, and checks the
      token survives; it fails without the fix. The red command passes for
      `^TestRunDevScript$` and the full `TestRunDev` suite.
    - Files: `cmd/run_dev_test.go`, `cmd/run_dev_tmp_dir_test.go`.
