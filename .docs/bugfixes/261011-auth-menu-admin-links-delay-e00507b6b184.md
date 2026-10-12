# Bugfix: admin menu links appear late after login

- Branch: `menurace-39ecd8d1`
- Base: `origin/develop` at `f89df69d91e434b33aff2586f9aed30007c8d504`
  (#44 merged), rebased from `5e851cdad10a2cc9c77a8c73c08c4b263f18d34a`
  (#39 merged), itself rebased from the original base
  `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-menurace`

## Items

- [x] Right after login, opening the account menu shows only "Log out";
      the admin "Feedback" link appears about 230 ms later. An admin who opens
      the menu straight away does not see it.
    - Source: phase 4 user-path verification on branch `usage`
      (`.docs/usage/phase4.md`), reported as incidental.
    - Evidence: a Playwright drive reading the menu items immediately after
      login got `["Log out"]`, then `["Feedback","Usage","Log out"]` about
      230 ms later on branch `usage`. Feedback is affected as well as Usage,
      so the delay comes from how `app/(results)/layout.tsx` resolves the
      admin flag, not from the usage feature. The red run below used the
      original base `73ebdaa`, before #39, so it checked Feedback alone.
    - `drive-feedback.mjs` waits for the link, so the existing recipe does
      not catch it.
    - Red command (stack from `.agents/skills/verify-wa/scripts/launch.sh`,
      `WA_VERIFY_PORT_BASE=47790`):
      `node .agents/skills/verify-wa/scripts/drive-menu-after-login.mjs "$RUN"`.
      It holds the post-login RSC refresh back for 2 s, logs in as the owner,
      opens the account menu as soon as its button appears and reads the
      menu items. Exit 1 on `b7565f2` (base `73ebdaa`), four runs out of four (including one
      with `WA_VERIFY_REFRESH_DELAY_MS=0`). Output:
      `menu items right after login: ["Log out"]` then
      `FAIL account menu shows Feedback straight after login`.
    - Before screenshot: `$RUN/evidence/before-menu-after-login.png`
      (1280x900, gitignored; the menu shows only "Log out").
    - Cause: `app/(results)/layout.tsx` decided the admin flag on the server
      and passed it to `AuthMenu` as `showFeedbackLink`. `loginAction` and
      `/api/auth/refresh` returned only `authenticated` and `username`, so
      the menu switched to signed-in at once but gained the link only when
      `router.refresh()` re-rendered the layout.
    - Fix: `CurrentSession` carries a server-decided `isAdmin` boolean, set
      in `parseSession` and `loginAction` (`app/(results)/auth/actions.ts`)
      and in `app/api/auth/refresh/route.ts`. `components/auth-menu.tsx`
      shows Feedback and #39's Usage from `session.isAdmin` and drops the
      `showFeedbackLink` and `showUsageLink` props; the browser refresh also
      compares `isAdmin`.
    - Tests: `tests/auth-menu.test.ts` (admin and non-admin straight after
      login with a refresh that never answers, admin revoked on refresh,
      E5 tests moved from the prop to the session),
      `tests/auth-actions.test.ts`, `tests/integration/auth-api.test.ts`.
    - Drive: `drive-menu-after-login.mjs` now also holds back
      `/api/auth/refresh` and requires both Feedback and Usage.
    - Rebase onto `5e851cd`: #39's Usage tests moved from the
      `showUsageLink` prop to the session flag; `isFeedbackAdmin` became
      `isMLWHAdmin` in `actions.ts` and `route.ts`. On `5e851cd` product code
      the drive prints `["Log out"]` and exits 1; after the fix it prints
      `["Feedback","Usage","Log out"]` and exits 0 (including with
      `WA_VERIFY_REFRESH_DELAY_MS=0`), and `drive-feedback.mjs` is ALL PASS.
- [x] #44 made `launch.sh` write `$RUN/stack.env` with `printf '%q'` and
      made `drive-feedback.mjs` parse it through bash.
      `drive-menu-after-login.mjs` still split lines on `=`, so with a
      checkout path containing a space `REPO` and `STATE_HOME` kept their
      backslash escapes and the script could not resolve `@playwright/test`
      or the token file. The PR's docs also ran it as
      `node $SKILL/scripts/drive-menu-after-login.mjs "$RUN"`, unquoted.
    - Source: rebase onto f89df69 (#44).
    - Red command: a run dir whose `stack.env` was written as `launch.sh`
      writes it, with `REPO` a symlink to this worktree under
      `<scratch>/has space/` and `STATE_HOME` a dir beside it holding dummy
      tokens; URLs on port 1.
      `node .agents/skills/verify-wa/scripts/drive-menu-after-login.mjs <run>`
      exited 1 with `Error: Cannot find module '@playwright/test'`,
      require stack `<scratch>/has\ space/wa/frontend/package.json`.
    - Green: the same command gets past env parsing, `require` and the token
      read, then exits 1 because no stack is running:
      `FAIL browser drive - Error: page.goto: net::ERR_UNSAFE_PORT`.
      `drive-feedback.mjs` on the same run dir reaches its first `fetch`
      (`bad port`).
    - Files: `.agents/skills/verify-wa/scripts/stack-env.mjs` (new),
      `drive-feedback.mjs`, `drive-menu-after-login.mjs`, `launch.sh`
      (comment), `features/feedback-admin-page.md`,
      `features/usage-admin-page.md`.
    - Fix: moved #44's bash-sourcing parser (only `PATH` inherited) into
      `readStackEnv(run)` in `stack-env.mjs`; both drivers call it.
      The two doc invocations quote the script path.
