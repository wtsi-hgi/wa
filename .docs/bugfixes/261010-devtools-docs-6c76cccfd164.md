# Bugfix checklist: developer docs and run-dev shutdown

- Branch: `devtools-docs-9abf0468` (worktree `../wa-devtools-docs`)
- Base: `origin/develop` at `29077be861f3db047bb12322327a1efbf565be54`
- Queue owner: llm-knowledge-base branch `feedback` delivery
  (PR wtsi-hgi/llm-knowledge-base#5); this checklist

Red scripts live in the orchestrating session's scratchpad, outside this
repository, at
`/tmp/claude-1000/-home-ubuntu-llm-knowledge-base/76e80516-b817-4e23-b793-f62b560cc0a0/scratchpad/wa-red/devtools-docs/`
(`$RED` below). Each takes the worktree path and writes temporary files under
`$TMPDIR`.

Prior items that must not regress: 260422-2 stops spawned frontend and
results-server processes as process groups during test teardown.
260522-1 makes `run-dev.sh` send SIGTERM to each child, escalate to SIGKILL,
and reap it. 260708-6 and 260709-1 cover the per-process binary and
the `.tmp/wa` symlink removed on cleanup. Their regressions are in
`cmd/run_dev_test.go`.

- [x] README.md:70,87 and DEVELOPING.md:197,337 document `wa results search --pipeline …`, which fails with 'unknown flag: --pipeline'; the real flags are --pipeline-name and --pipeline-identifier.
    - Source: llm-knowledge-base `feedback` delivery (PR wtsi-hgi/llm-knowledge-base#5)
    - Confirmed at 29077be. `wa results search --help` lists
      `--pipeline-identifier`, `--pipeline-name` and `--pipeline-version`, and
      has no `--pipeline` flag.
    - Red command: `$RED/red9.sh .` from the worktree root (exit 1). It
      builds `wa`, extracts every `wa … results search` line from `README.md`
      and `DEVELOPING.md`, and runs each with `--help` appended in an empty
      directory under `env -i`. Cobra parses flags before handling help, so
      an unknown flag still fails and no server is contacted. The same line
      with `--pipeline-name` exits 0, so the script turns green once the docs
      are fixed.

        ```text
        == README.md:70: wa results search --pipeline my-pipeline --user jdoe
        exit=1 Error: unknown flag: --pipeline
        == README.md:87: wa --env development results search --pipeline my-pipeline
        exit=1 Error: unknown flag: --pipeline
        == DEVELOPING.md:197: wa results search --pipeline nf-pipe
        exit=1 Error: unknown flag: --pipeline
        == DEVELOPING.md:337: ./wa --env development results search --pipeline nf-pipe
        exit=1 Error: unknown flag: --pipeline
        checked 4 documented lines; real flags:
              --pipeline-identifier string   Pipeline identifier filter
              --pipeline-name string         Pipeline name filter
              --pipeline-version string      Pipeline version filter
        ```

    - Fixed: the four examples in `README.md` and `DEVELOPING.md` now use
      `--pipeline-name`, and the verify-wa `results-cli.md` gotcha no longer
      describes the wrong docs. New `TestDocumentedCommandsParse`
      (`cmd/docs_commands_test.go`) parses every documented `wa` line with `--help`
      and reports unknown flags and unknown nested subcommands. Red command now
      exits 0.

- [x] run-dev.sh leaves `next dev` and its workers running (holding the frontend port) after the script receives SIGTERM, because it only signals pnpm.
    - Source: llm-knowledge-base `feedback` delivery (PR wtsi-hgi/llm-knowledge-base#5)
    - Confirmed at 29077be. `run-dev.sh` starts the frontend as
      `bash -lc '… exec pnpm dev …' &` and records only that PID.
      `cleanup` → `terminate_child_process` sends SIGTERM to that PID alone.
      pnpm exits, and its `next dev` child and `next-server` worker stay up.
      Ctrl-C in a terminal does not show the bug, because SIGINT reaches the
      whole foreground process group.
    - Red command: `$RED/red10.sh .` from the worktree root (exit 1, about 18
      s, the same result on two runs). It needs `frontend/node_modules`
      (`pnpm install --frozen-lockfile`). It starts
      `setsid ./run-dev.sh --mode test` with `.env.test` on ports 47791–47793
      (`WA_RED_PORT_BASE`), waits for `Development environment is ready.`,
      sends SIGTERM to the run-dev.sh PID only, and waits for it to exit.
      It then fails if the frontend port is still listening or any process
      in run-dev.sh's process group survives. It always kills that group
      and checks that the ports are free. Exit 2 means the result is
      inconclusive.

        ```text
        ready: run-dev.sh pid=2073551 pgid=2073551; frontend port 47791 listening: yes
        run-dev.sh exited with status 0 after SIGTERM
        FAIL: frontend port 47791 still LISTENING after run-dev.sh exited
        FAIL: processes left in run-dev.sh's group 2073551:
        2073941 2073551 node <worktree>/frontend/node_modules/.bin/../next/dist/bin/next dev --port 47791 --experimental-https --experimental-https-k
        2073962 2073551 next-server (v16.2.4)
        cleanup: group 2073551 gone, ports free
        ```

    - Fixed: `terminate_child_process` in `run-dev.sh` now sends SIGTERM to each
      child and its whole descendant tree (new `descendant_pids`, from one `ps`
      snapshot), then escalates and reaps as before; children stay in run-dev's
      process group so outer group kills still work. Regression test
      `TestRunDevScriptStopsFrontendDescendantsOnSIGTERM` (`cmd/run_dev_test.go`)
      uses a multi-level wrapper tree and fails against the old script and a
      direct-children-only mutant. verify-wa `SKILL.md` and `cleanup.sh` no longer
      describe the bug as current. Red command now exits 0.

- [x] `cmd/run_dev_test.go` (`TestRunDevAutoManagedMLWHBackendCanServeProdConfiguredCacheWithoutDSN`) fails whenever the repository is checked out under `/tmp`, because `mlwh_cache_path_looks_test` in `run-dev.sh` treats any cache path under `/tmp/*` as test-shaped.
    - Source: found by the serve-startup item 7 reviewer, who ran the suite from a scratch copy under `/tmp`; the same failure occurs on the unpatched base there.
    - Red command: run `go test ./cmd -run TestRunDevAutoManagedMLWHBackendCanServeProdConfiguredCacheWithoutDSN` from a detached copy of this branch under `/tmp`.
    - Fixed: the test was at fault, not the guard (which `.docs/mlwh/spec.md`
      requires). It now passes the repo-relative cache path
      `.tmp/run-dev-prod-mlwh-<port>.sqlite`, which run-dev resolves after
      `cd "$REPO_ROOT"`, so it is never test-shaped wherever the checkout lives.
      Passes from a copy under `/tmp` and from the real worktree.

- [x] `gofmt -l cmd/` lists `cmd/env.go`, so the file is not gofmt-formatted although `make lint-go` passes.
    - Source: found by the serve-startup item 7 implementor.
    - Red command: `[ -z "$(gofmt -l cmd/)" ]`
    - Exit status: 1 (prints `cmd/env.go`).
    - Fixed: `gofmt -w cmd/env.go` added the missing final newline. No other tracked
      Go file is unformatted. Red command now exits 0.

- [x] `make lint-go` does not check gofmt formatting: the repo has no golangci-lint config, golangci-lint v2's defaults enable no formatters, and the Makefile's fallback pin (`golangci-lint@v1.64.8`) would reject a v2 config.
    - Source: found while fixing the `cmd/env.go` item.
    - Red command: in a scratch copy, strip the final newline from a Go file in `cmd/` and run `make lint-go`; it should fail and currently reports `0 issues.`
    - Fixed: new `.golangci.yml` (`version: "2"`, the unchanged standard linter set,
      and the `gofmt` formatter); the `Makefile` fallback pin moves to
      `golangci-lint/v2@v2.12.2`, which CI uses via `make lint`. The red copy now
      fails with a gofmt finding, both with the local binary and the `go run`
      fallback.
