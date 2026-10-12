# Bugfix: pnpm test ignores file arguments

- Branch: `pnpmfilter-90a8f24a`
- Base: `origin/develop` at `407579c2dc3e65e76c8e6711b6dad35b1e71b94f`
  (rebased from `73ebdaa`)
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-pnpmfilter`

## Items

- [x] `pnpm test -- <files>` in `frontend/` ignores the file filter. The
      `test` script in `frontend/package.json` is
      `vitest run --exclude tests/scaffold.test.ts && vitest run --config vitest.custom.config.ts tests/scaffold.test.ts ...`,
      so extra arguments reach only the second command. A targeted run
      silently runs the whole suite plus the scaffold build, which can clash
      with another build in the same tree.
    - Source: spec-aware PR review pass 7 of branch `usage`, reported as
      incidental.
    - Evidence: `frontend/package.json` `test` script on this base (last
      changed in af87dac); a reviewer's targeted `pnpm test -- <file>` ran
      all 55 files.
    - Workaround until fixed: `node_modules/.bin/vitest run <files>`.
    - Red command (from `frontend/`): `pnpm test -- tests/health.test.ts`,
      then require exactly one vitest run whose summary is
      `Test Files  1 passed (1)` and no `tests/scaffold.test.ts` run. Exit 0
      after 71s, but red:

        ```text
        $ vitest run --exclude tests/scaffold.test.ts && vitest run --config vitest.custom.config.ts tests/scaffold.test.ts --poolOptions.forks.singleFork -- tests/health.test.ts
         Test Files  55 passed (55)
              Tests  737 passed (737)
         ✓ tests/scaffold.test.ts (7 tests) 20254ms
         Test Files  1 passed (1)
              Tests  7 passed (7)
        ```

        pnpm (v11 locally, and v10 as used in CI) forwards the literal `--`,
        and vitest treats arguments after `--` as non-filters, so even the
        second command ignored the file.

    - Fixed: the `test` script now runs `node scripts/run-tests.mts`, which
      drops a forwarded `--`, sends file filters to the main run, sends a
      `scaffold` filter only to the single-fork `vitest.custom.config.ts`
      run, and passes options to every run. With no arguments it runs the
      same two vitest commands in the same order as before, stopping at the
      first failure with vitest's exit code. `tsconfig.json` includes
      `**/*.mts` so `tsc` checks the script.
    - Files: `frontend/package.json`, `frontend/scripts/run-tests.mts`,
      `frontend/tsconfig.json`, `frontend/tests/run-tests.test.ts`.
    - Regression test: `frontend/tests/run-tests.test.ts` covers the
      dispatch, including `["--", "tests/health.test.ts"]`.
    - Green: the red command now runs one vitest run,
      `Test Files  1 passed (1)`, in about 5s, with no scaffold run.
      `pnpm test -- tests/scaffold.test.ts` runs only the scaffold run, and a
      nonexistent filter exits 1.

- [x] `pnpm test` and `make test` fail with a cryptic
      `ERR_UNKNOWN_FILE_EXTENSION` on Node.js 22.0 to 22.17. The
      `frontend/package.json` `test` script is `node scripts/run-tests.mts`,
      which needs Node's default TypeScript type stripping, unflagged only
      from Node.js 22.18.0. The `DEVELOPING.md` Prerequisites table said
      Node.js `22+`, and `frontend/package.json` had no `engines` field.
    - Source: Copilot review on PR #48 head `f60f738`, thread
      `PRRT_kwDORrF2pM6rSDIN`.
    - Red command (from the repo root):

        ```bash
        node -e 'const p=require("./frontend/package.json");const d=require("fs").readFileSync("DEVELOPING.md","utf8");if(p.engines?.node!==">=22.18"||!/\| \*\*Node\.js\*\* \| 22\.18\+/.test(d)){console.error("engines.node=",p.engines?.node,"; DEVELOPING.md Node row:",d.split("\n").find(l=>l.includes("Node.js")));process.exit(1)}'
        ```

        Exit 1 before the fix:

        ```text
        engines.node= undefined ; DEVELOPING.md Node row: | **Node.js** | 22+       | Frontend dev server and build                                |
        ```

    - Fixed: `frontend/package.json` declares `"engines": { "node": ">=22.18" }`,
      so pnpm warns `Unsupported engine` on older Node.js, and the
      `DEVELOPING.md` Prerequisites table lists Node.js `22.18+`.
    - Files: `frontend/package.json`, `DEVELOPING.md`.
    - Green: the red command exits 0 with no output.
