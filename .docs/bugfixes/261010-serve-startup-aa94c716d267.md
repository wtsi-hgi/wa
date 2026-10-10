# Bugfix checklist: wa mlwh serve startup failures

- Branch: `serve-startup-d4629ccd` (worktree `../wa-serve-startup`)
- Base: `origin/develop` at `29077be861f3db047bb12322327a1efbf565be54`
- Queue owner: llm-knowledge-base branch `feedback` delivery
  (PR wtsi-hgi/llm-knowledge-base#5); this checklist

Red scripts live in the orchestrating session's scratchpad, outside this
repository, at
`/tmp/claude-1000/-home-ubuntu-llm-knowledge-base/76e80516-b817-4e23-b793-f62b560cc0a0/scratchpad/wa-red/serve-startup/`
(`$RED` below). Each takes the worktree path, leaves the worktree unchanged,
and writes temporary files under `$TMPDIR`. In excerpts, `<tmp>` replaces
long temporary paths.

Prior items checked: 260627-1 and 260708-6 change `wa mlwh serve` startup but
not error reporting. `wa mlwh sync` also sets `SilenceErrors: true`
(`cmd/mlwh.go:473`). It prints its own errors through
`reportMLWHSyncCommandError`, so a fix in `main.go` must not make sync print
them twice.

- [x] `wa mlwh serve` startup errors exit 1 with nothing on stderr (cmd/mlwh.go:205 sets SilenceErrors: true and main.go does not print the error), e.g. `wa mlwh serve` with no cache path, or with `--feedback-db :memory:`, though README promises a rejection message.
    - Source: llm-knowledge-base `feedback` delivery (PR wtsi-hgi/llm-knowledge-base#5)
    - Confirmed at 29077be. `main.go` exits 1 without printing the error that
      `run` returns. The other cobra commands print because they leave
      `SilenceErrors` false. Run with the same cache path and no
      `--feedback-db`, the server stays up until `timeout` kills it (exit
      124), so the `:memory:` case's exit 1 does come from the feedback check
      in `openMLWHServeFeedback`.
    - Red command: `$RED/red7.sh .` from the worktree root (exit 1). It
      builds `wa`, runs it in an empty directory with `env -i PATH HOME`, so
      no `.env` files or `WA_*` variables apply, and requires a non-zero exit
      with the expected message on stderr.

        ```text
        == no cache path: wa mlwh serve
        exit=1 stdout_bytes=0 stderr_bytes=0
        stderr:
        FAIL: want non-zero exit and stderr containing: WA_MLWH_CACHE_PATH must be set
        == --feedback-db :memory:: wa mlwh serve --mlwh-cache <tmp>/cache.sqlite --feedback-db :memory:
        exit=1 stdout_bytes=0 stderr_bytes=0
        stderr:
        FAIL: want non-zero exit and stderr containing: :memory:
        ```
  - Fixed: the serve command in `cmd/mlwh.go` no longer sets `SilenceErrors`, so
    cobra prints its errors once. `run` in `main.go` takes a stderr writer and
    prints errors raised before any command runs (env and scenario checks) once.
    Regression tests: `TestMLWHStartupErrorsPrintedOnceOnStderr`
    (`cmd/mlwh_test.go`) and `TestRunPrintsStartupErrorsOnce` (`main_test.go`).
    Red command now exits 0.

- [ ] `wa mlwh serve --feedback-db <path>` fails to start (silently) when the XDG_STATE_HOME directory for the feedback token does not exist, instead of creating it.
    - Source: llm-knowledge-base `feedback` delivery (PR wtsi-hgi/llm-knowledge-base#5)
    - Confirmed at 29077be. In plain mode, `mlwhServeFeedbackAdminToken`
      resolves `$XDG_STATE_HOME/.wa-mlwh-server.token` through
      `gas.TokenDir`, and `writeResultsServeServerToken` (`cmd/results.go`)
      calls `os.OpenFile` without creating the parent directory. The startup
      is silent because of the previous item. `wa results serve` uses the
      same token helpers, so a fix in the shared helper would also change it.
    - Red command: `$RED/red8.sh .` from the worktree root (exit 1). It
      injects `red_serve_state_dir_test.go` into `cmd` with
      `go test -overlay`. The test sets `XDG_STATE_HOME` to a missing nested
      directory, starts `mlwh serve --feedback-db` in-process, and requires
      `/health`, a non-empty token file, and `GET /feedback` with that token
      to return 200.

        ```text
        === RUN   TestRedItem8FeedbackDBStartsWhenStateDirIsMissing
            red_serve_state_dir_test.go:42: mlwh serve with --feedback-db and missing XDG_STATE_HOME <tmp>/001/missing/state did not start: command exited early: feedback admin token: open <tmp>/001/missing/state/.wa-mlwh-server.token: no such file or directory
        --- FAIL: TestRedItem8FeedbackDBStartsWhenStateDirIsMissing (0.18s)
        FAIL
        FAIL	github.com/wtsi-hgi/wa/cmd	0.183s
        ```
- [ ] `wa mlwhdiff bogus` and `wa results bogus` exit 0 with nothing on stderr, while `wa bogus` and `wa mlwh bogus` report an unknown command.
  - Source: found by the item 7 reviewer.
  - Red command (from an empty directory, binary built from this branch):
    `env -i HOME=$HOME PATH=$PATH wa mlwhdiff bogus; [ $? -ne 0 ]` and the
    same for `wa results bogus`.
  - Exit status: 1 (both commands exit 0 with empty stderr).
- [ ] `wa mlwh serve extra-arg` and `wa mlwh sync extra` accept unexpected positional arguments instead of rejecting them.
  - Source: found by the item 7 reviewer.
  - Red command:
    `env -i HOME=$HOME PATH=$PATH wa mlwh serve extra-arg 2>&1 | grep -q 'accepts 0 arg'`
  - Exit status: 1 (output is
    `Error: WA_MLWH_CACHE_PATH must be set or --mlwh-cache provided`; the
    extra argument is ignored).
- [ ] `wa mlwh sync` prints its errors without the `Error: ` prefix every other command uses.
  - Source: found by the item 7 reviewer.
  - Red command:
    `env -i HOME=$HOME PATH=$PATH wa mlwh sync 2>&1 | grep -q '^Error: WA_MLWH_DSN must be set'`
  - Exit status: 1 (output is `WA_MLWH_DSN must be set`).
