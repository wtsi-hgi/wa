---
name: verify-wa
description: "Verify user-visible wa changes through the real app: launch an isolated stack, doctor it, drive and capture evidence for wa mlwh serve agent feedback, the /feedback admin page, the results web UI, the MLWH query API, and the results/mlwh CLIs. Use when proving a wa change works or reproducing a wa bug end to end."
---

# Verify wa

The stack is three processes started by the repo's own `run-dev.sh --mode
test`: the results API (HTTPS), a real `wa mlwh serve` with feedback on
(HTTP), and the Next.js dev frontend (HTTPS, self-signed). Test mode is
hermetic: it seeds a fixture MLWH cache and results fixtures, uses throwaway
DBs under `.tmp/`, and refuses real MLWH credentials. No MLWH database, LDAP,
or network access is needed.

Pick the features to drive from [features/README.md](features/README.md).

`$SKILL` below is the absolute path of this skill's directory,
`<checkout>/.agents/skills/verify-wa`. The scripts act on the checkout that
contains them, so use the copy in the worktree you are verifying. `$RUN` is
the run directory `launch.sh` prints. Shell variables do not persist between
tool calls, so set both at the top of every command.

## Launch

```bash
SKILL=<checkout>/.agents/skills/verify-wa
"$SKILL/scripts/launch.sh"          # last line: RUN=<checkout>/.tmp/verify-wa/<UTC stamp>
```

- Ports: `WA_VERIFY_PORT_BASE` (default 47690) +1 frontend, +2 results,
  +3 MLWH, +4 spare for standalone serves. The script refuses to start if any
  is listening; pick another base rather than stopping someone else's server.
  Run one stack per checkout: run-dev test mode shares `.tmp/state-test`.
  Separate worktrees can run side by side on different bases.
- It installs `frontend/node_modules` with `pnpm install --frozen-lockfile`
  when missing, builds `$RUN/wa`, and waits for run-dev's
  `Development environment is ready.` line (first run: about a minute).
- URLs, token dir, cert, PID, and PGID land in `$RUN/stack.env`; service logs
  in `$RUN/logs/`, run-dev output in `$RUN/run-dev.out`.
- Ready means `launch.sh` exited 0. Non-zero: read the tail it prints, run
  Cleanup, fix, relaunch.

## Doctor

```bash
"$SKILL/scripts/doctor.sh" "$RUN"   # every line "ok", exit 0
```

It checks run-dev is alive, the stack's revision is this checkout's HEAD,
results/MLWH/frontend health, feedback is on (unauthenticated
`GET /feedback` is 401, not 503), and both token files exist. Run it before
the first drive and after any failed drive.

## Drive

Each feature file gives its recipe. Shared facts:

- **Login** (results web and `/feedback`): the account menu's `Log in`
  button opens `form[aria-label="Log in"]`. Username is the OS user
  (`id -un`); password is the contents of
  `$STATE_HOME/.wa-results-server.token`. That is the results server's
  owner-token login. Test mode has no LDAP, so real LDAP accounts cannot be
  driven here. The OS user running Next.js is always a feedback admin.
- **Browser**: Playwright from `frontend/node_modules` (already a repo
  dependency). Load it with
  `createRequire("<checkout>/frontend/package.json")("@playwright/test")`, launch
  with `ignoreHTTPSErrors: true`. Set `WA_VERIFY_CHROMIUM` to an executable
  path if the bundled Chromium is missing.
- **Fixture data**: MLWH study `6568` (one sample, one library), results
  seeded from `.docs/results-web/fixtures/seed.json` (requesters `alice`,
  `carol`, `erin`, `grace`, `ivy`).

## Evidence

Evidence goes in `$RUN/evidence/` under the repo's gitignored `.tmp/`. Leave
it uncommitted. It survives Cleanup and `make test`'s `clean-test-tmp`.
Test-mode feedback and cache DBs are deleted on shutdown, so capture rows
through the admin API (the drive scripts do) before Cleanup.

## Cleanup

```bash
"$SKILL/scripts/cleanup.sh" "$RUN"  # exit 0 = ports free, process group gone
```

It sends TERM to the recorded run-dev PID, then TERM to its recorded process
group. run-dev.sh signals only pnpm, so `next dev` would survive without the
group kill. Evidence stays. Run it after every failed launch or drive too.

## Proof Standards

- Drive the real user path: HTTP routes the MCP server or browser uses, the
  account menu, real buttons. Internal setters, direct DB writes, and the
  Playwright suite's `seqmeta-stub.mjs` prove fixtures, not features.
- Capture the action and the resulting state, and check the side effect
  (stored row via admin API, file mode, token file) as well as the screen.
- Record feature, expected and observed outcome, `REVISION` from
  `stack.env` (suffixed `+uncommitted` when product files differ from HEAD),
  verdict, and evidence paths.
- Verdict is VERIFIED, NOT VERIFIED (product failure), or INCONCLUSIVE
  (environment or access). Inconclusive is not a pass.

## Other Harnesses

`make test-e2e` runs the repo's Playwright suite on its own throwaway ports,
with a stub instead of `wa mlwh serve`. Use it for regression coverage, not
as proof of MLWH or feedback behaviour.
