# Bugfix: admin menu links appear late after login

- Branch: `menurace-39ecd8d1`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-menurace`

## Items

- [ ] Right after login, opening the account menu shows only "Log out";
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
