# Bugfix: admin menu links appear late after login

- Branch: `menurace-39ecd8d1`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
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
      admin flag, not from the usage feature. Reproduce on this base with the
      Feedback link alone.
    - `drive-feedback.mjs` waits for the link, so the existing recipe does
      not catch it.
    - Red command (stack from `.agents/skills/verify-wa/scripts/launch.sh`,
      `WA_VERIFY_PORT_BASE=47790`):
      `node .agents/skills/verify-wa/scripts/drive-menu-after-login.mjs "$RUN"`.
      It holds the post-login RSC refresh back for 2 s, logs in as the owner,
      opens the account menu as soon as its button appears and reads the
      menu items. Exit 1 on `b7565f2`, four runs out of four (including one
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
      shows Feedback from `session.isAdmin` and drops the prop; the browser
      refresh also compares `isAdmin`. Any other admin-only menu link, such
      as Usage from #39, reads the same `session.isAdmin`.
    - Tests: `tests/auth-menu.test.ts` (admin and non-admin straight after
      login with a refresh that never answers, admin revoked on refresh,
      E5 tests moved from the prop to the session),
      `tests/auth-actions.test.ts`, `tests/integration/auth-api.test.ts`.
    - Drive: `drive-menu-after-login.mjs` now also holds back
      `/api/auth/refresh`; it prints `["Feedback","Log out"]` and exits 0
      after the fix, and `drive-feedback.mjs` is ALL PASS.
