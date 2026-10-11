# Bugfix: verify-wa launch writes unquoted REVISION

- Branch: `verifyrev-45b2c5f7`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-verifyrev`

## Items

- [ ] `.agents/skills/verify-wa/scripts/launch.sh` writes
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
