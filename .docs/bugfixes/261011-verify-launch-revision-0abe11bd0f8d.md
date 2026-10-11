# Bugfix: verify-wa launch writes unquoted REVISION

- Branch: `verifyrev-45b2c5f7`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-verifyrev`

## Items

- [x] `.agents/skills/verify-wa/scripts/launch.sh` writes
      `REVISION=<sha> +uncommitted` into `stack.env` without quotes when the
      worktree has uncommitted changes (line 37). Sourcing `stack.env` then runs
      `+uncommitted` as a command, so `doctor.sh` fails with
      `REVISION: unbound variable`.
    - Source: phase 4 user-path verification on branch `usage`
      (`.docs/usage/phase4.md`), reported as incidental.
    - Evidence: the verifier launched a stack from a dirty worktree and
      `doctor.sh` exited non-zero with `REVISION: unbound variable`;
      quoting the value in the generated `stack.env` let it run.
    - Independent of the usage feature: `launch.sh` is unchanged on `usage`.
    - Red command: a throwaway harness (`red.sh`, below; not committed as a
      script) copies the verify-wa scripts into a temporary git repository
      with a dirty product file, stubs `go`, `next` and `run-dev.sh`, runs the
      real `launch.sh`, then sources the resulting `stack.env` under `set -euo pipefail` and checks
      `REVISION`, `REPO`, `STATE_HOME`, `CERT` and `PGID`. Run with a plain
      directory name and with `wa repo` (a space in the path). Both exit 127:

        ```text
        === plainrepo
        .../plainrepo/.tmp/verify-wa/20261011T125021Z/stack.env: line 2: +uncommitted: command not found
        exit=127
        === wa repo
        .../wa repo/.tmp/verify-wa/20261011T125021Z/stack.env: line 1: repo: command not found
        exit=127
        ```

        ```bash
        #!/usr/bin/env bash
        # Red check for verify-wa launch.sh stack.env quoting.
        # Usage: red.sh <worktree> [repo-dir-name]   Runs the real launch.sh from a stubbed temp repo
        # (dirty tree; default dir name has a space) and sources stack.env under set -u.
        set -euo pipefail
        WT="$(cd "$1" && pwd -P)"
        T="$(mktemp -d "${TMPDIR:-/tmp}/verifyrev-red.XXXXXX")"
        trap 'rm -rf "$T"' EXIT
        R="$T/${2:-wa repo}"
        mkdir -p "$R/.agents/skills/verify-wa/scripts" "$R/frontend/node_modules/.bin" "$T/bin"
        cp "$WT"/.agents/skills/verify-wa/scripts/*.sh "$R/.agents/skills/verify-wa/scripts/"
        printf '#!/bin/sh\nexit 0\n' >"$R/frontend/node_modules/.bin/next"
        printf '#!/usr/bin/env bash\necho "Development environment is ready."\nsleep 30\n' >"$R/run-dev.sh"
        : >"$R/.env.test"
        printf 'x\n' >"$R/product.txt"
        chmod +x "$R/frontend/node_modules/.bin/next" "$R/run-dev.sh"
        printf '#!/bin/sh\nexit 0\n' >"$T/bin/go"; chmod +x "$T/bin/go"
        git -C "$R" init -q && git -C "$R" add -A && git -C "$R" -c user.email=a@b -c user.name=t commit -qm init
        printf 'dirty\n' >>"$R/product.txt"
        out="$(PATH="$T/bin:$PATH" WA_VERIFY_PORT_BASE=$((40000 + RANDOM % 20000)) timeout 30 "$R/.agents/skills/verify-wa/scripts/launch.sh")"
        RUN="${out#RUN=}"
        pkill -f "$R/run-dev.sh" 2>/dev/null || true
        echo "--- stack.env"; cat "$RUN/stack.env"
        echo "--- source under set -u"
        HEAD_SHA="$(git -C "$R" rev-parse HEAD)"
        bash -c 'set -euo pipefail; . "$1/stack.env"
          [[ "$REVISION" == "$2 +uncommitted" ]] || { echo "REVISION=[$REVISION]"; exit 1; }
          [[ "$REPO" == "$3" ]] || { echo "REPO=[$REPO]"; exit 1; }
          [[ "$STATE_HOME" == "$3/.tmp/state-test" && "$CERT" == "$3/.tmp/wa-dev-cert.pem" ]] || { echo "STATE_HOME/CERT wrong"; exit 1; }
          [[ -n "$PGID" ]] || exit 1
          echo OK' _ "$RUN" "$HEAD_SHA" "$R"
        ```

    - Fixed: `launch.sh` writes every `stack.env` value, including the
      later `PGID` line, with `printf 'KEY=%q\n'`, so `REVISION` with the
      `+uncommitted` suffix and checkout paths with spaces or shell
      metacharacters survive `. stack.env`. `drive-feedback.mjs` no longer
      splits lines itself; it has bash source the file
      (`set -a; . "$1"; exec env -0`, with only `PATH` inherited) and reads
      the NUL-separated values, so it sees what `doctor.sh`, `cleanup.sh` and
      `drive-feedback-modes.sh` see. Files: `launch.sh`, `drive-feedback.mjs`
      under `.agents/skills/verify-wa/scripts/`. The red command now prints
      `OK` and exits 0 for `plainrepo`, `wa repo` and a name containing `'`,
      `$` and a backtick. No committed regression test: no test harness
      covers the verify-wa scripts, and a Go test in `cmd/` would couple
      product tests to agent tooling.

- [x] verify-wa docs break or mislead with unusual checkout paths:
      `features/results-cli.md` sets `C="--server $RESULTS_URL --cert $CERT"`
      and uses `$C` unquoted, so a `CERT` path with a space splits; lines
      26-30 run `$RUN/wa results ...` unquoted, so a checkout path with a space
      splits too; and `SKILL.md` (around line 38) says the PID goes into
      `stack.env` when it is written to `$RUN/run-dev.pid`. Same quoting theme
      as the item above, so it shares this branch.
    - Source: bugfix of the item above, reported as incidental.
    - Evidence: reading the files on this base.
    - Red command: a throwaway harness (`red2.sh`, not committed) builds the
      same stubbed temporary repository as `red.sh` above under `wa repo`,
      runs the real `launch.sh`, and replaces `$RUN/wa`, `node` and
      `drive-feedback-modes.sh` with stubs that log their argv. It then runs,
      as written in the docs and after `. "$RUN/stack.env"`: the
      `results-cli.md` drive block (`<id>` replaced by `fakeid`); every inline
      command span in the other feature files starting with `node`, `$SKILL`,
      `$RUN/wa` or `WA_MLWH_SERVER_URL=` (spans with `<placeholders>`
      skipped); and every `$(cat ...)` span. It fails when a command exits
      non-zero, when no `--cert` argument equals `$CERT`, or when a logged
      path argument is not an existing path. Before the fix it exits 1:

        ```text
        === results-cli.md drive block
          ! bash: line 7: /tmp/verifyrev-red2.cZ4way/wa: No such file or directory
          exit=127
        === feedback-admin-page.md: node $SKILL/scripts/drive-feedback.mjs "$RUN"
          exit=0
        === mlwh-feedback-submit.md: $SKILL/scripts/drive-feedback-modes.sh "$RUN"
          exit=127
        === mlwh-query-api.md: WA_MLWH_SERVER_URL=$MLWH_URL $RUN/wa mlwh info 6568 --type study
          exit=127
        === mlwh-feedback-submit.md: echo $(cat $STATE_HOME/.wa-mlwh-server.token)
          ! cat: repo/.tmp/state-test/.wa-mlwh-server.token: No such file or directory
          exit=1
        === argv check
        no --cert arg equal to CERT
        split path args:
          /tmp/verifyrev-red2.cZ4way/wa
        repo/.agents/skills/verify-wa/scripts/drive-feedback.mjs
        RED
        ```

        The `node $SKILL/...` spans exit 0 only because the stub `node`
        accepts the split words; its argv log shows the script path split
        in two.

    - Review added one more case to this item: the `SKILL.md` launch block
      assigned `SKILL=<checkout>/.agents/skills/verify-wa` unquoted. `red2.sh`
      also runs each `SKILL.md` line matching `^"?SKILL=` with `<checkout>`
      filled in and requires `SKILL` to equal the path; before the fix that
      check fails with `SKILL: unbound variable`, exit 127. The harness also
      copies `scripts/*.mjs` so a correctly quoted `drive-feedback.mjs` path
      exists in the temp repo.
    - Fixed: the recipes quote every command path (`"$RUN/wa"`,
      `"$SKILL/scripts/..."`, `"$STATE_HOME/..."`, `SKILL="<checkout>/..."`),
      and `results-cli.md` passes the server flags as the array
      `C=(--server "$RESULTS_URL" --cert "$CERT")`, expanded `"${C[@]}"`.
      `SKILL.md` now lists what `launch.sh` writes to `stack.env` and says the
      run-dev PID is in `$RUN/run-dev.pid`. Files under
      `.agents/skills/verify-wa/`: `SKILL.md`, `features/results-cli.md`,
      `features/feedback-admin-page.md`, `features/mlwh-feedback-submit.md`,
      `features/mlwh-query-api.md`. `red2.sh` now ends `ALL OK` and exits 0
      for `wa repo` and `plainrepo`; `red.sh` still prints `OK`. No committed
      regression test, for the same reason as the item above.
