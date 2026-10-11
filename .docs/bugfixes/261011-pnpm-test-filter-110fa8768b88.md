# Bugfix: pnpm test ignores file arguments

- Branch: `pnpmfilter-90a8f24a`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-pnpmfilter`

## Items

- [ ] `pnpm test -- <files>` in `frontend/` ignores the file filter. The
  `test` script in `frontend/package.json` is
  `vitest run --exclude tests/scaffold.test.ts && vitest run --config
  vitest.custom.config.ts tests/scaffold.test.ts ...`, so extra arguments
  reach only the second command. A targeted run silently runs the whole
  suite plus the scaffold build, which can clash with another build in the
  same tree.
  - Source: spec-aware PR review pass 7 of branch `usage`, reported as
    incidental.
  - Evidence: `frontend/package.json` `test` script on this base (last
    changed in af87dac); a reviewer's targeted `pnpm test -- <file>` ran
    all 55 files.
  - Workaround until fixed: `node_modules/.bin/vitest run <files>`.
