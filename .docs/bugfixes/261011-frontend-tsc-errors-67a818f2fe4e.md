# Bugfix: frontend test files fail typecheck

- Branch: `frontendtsc-69d13e03`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-frontendtsc`

## Items

- [x] `tsc --noEmit -p .` in `frontend/` reports 12 type errors in test and
      e2e files, and nothing catches them because `frontend/package.json` has
      no typecheck script and neither `pnpm lint`, `pnpm test` nor CI runs
      `tsc` over tests (`next build` checks only app code).
    - Source: phase 4 items 4.1/4.2 on branch `usage`
      (`.docs/usage/phase4.md`), reported as incidental.
    - Evidence: `./node_modules/.bin/tsc --noEmit -p .` from `frontend/` on
      this base exits 2 with:

        ```text
        e2e/file-browser-layout.spec.ts(1353,50): error TS2345: Argument of type 'Element | null' is not assignable to parameter of type 'Element'.
        e2e/file-browser-layout.spec.ts(2889,35): error TS2345: Argument of type 'Element | null' is not assignable to parameter of type 'Element | undefined'.
        e2e/file-browser-layout.spec.ts(2896,50): error TS2345: Argument of type 'Element | null | undefined' is not assignable to parameter of type 'Element | undefined'.
        e2e/real-data-layout-repro.spec.ts(219,43): error TS2677: A type predicate's type must be assignable to its parameter's type.
        tests/dashboard.test.ts(999,57): error TS2304: Cannot find name 'FileEntry'.
        tests/file-browser.test.ts(1446,21): error TS2345: Argument of type 'Element | null | undefined' is not assignable to parameter of type 'Element | null'.
        tests/file-browser.test.ts(3762,21): error TS2345: Argument of type 'Element | null | undefined' is not assignable to parameter of type 'Element | null'.
        tests/file-browser.test.ts(4246,21): error TS2345: Argument of type 'Element | null | undefined' is not assignable to parameter of type 'Element | null'.
        tests/integration/auth-api.test.ts(146,25): error TS2339: Property 'status' does not exist on type 'NextResponse<unknown> | "timed out"'.
        tests/integration/auth-api.test.ts(147,31): error TS2339: Property 'json' does not exist on type 'NextResponse<unknown> | "timed out"'.
        tests/integration/auth-api.test.ts(151,32): error TS2345: Argument of type 'NextResponse<unknown> | "timed out"' is not assignable to parameter of type 'Response'.
        tests/integration/auth-api.test.ts(152,32): error TS2345: Argument of type 'NextResponse<unknown> | "timed out"' is not assignable to parameter of type 'Response'.
        ```

    - Independent of the usage feature: none of these files change on `usage`.
    - Decide with the fix whether to add a typecheck script and gate (for
      example in `pnpm lint` or `make lint-frontend`) so they cannot return.
    - Red command: `./node_modules/.bin/tsc --noEmit -p .` from `frontend/`,
      exit 2 with the 12 errors above; exit 0 after the fix.
    - Fix: narrowed types instead of casting. `textStyle` (e2e layout spec)
      and the `click` helper (`tests/file-browser.test.ts`) accept
      `Element | null | undefined` and keep their `instanceof HTMLElement`
      guards; `lineRect` is computed inside the `instanceof` check; the
      invalid `HTMLElement` predicate over `HTMLDivElement`s is dropped;
      `dashboard.test.ts` imports `FileEntry`; `auth-api.test.ts` throws when
      the logout POST times out, which narrows `response` and still fails the
      test.
    - Gate: `frontend/package.json` gains `typecheck` (`tsc --noEmit -p .`),
      and `lint` now ends with `pnpm typecheck`, so `make lint-frontend` and
      CI's `make lint` fail on type errors in tests and e2e specs. Proved by
      reintroducing an error: `pnpm lint` and `make lint-frontend` exit 2.
    - Files: `frontend/package.json`,
      `frontend/e2e/file-browser-layout.spec.ts`,
      `frontend/e2e/real-data-layout-repro.spec.ts`,
      `frontend/tests/dashboard.test.ts`, `frontend/tests/file-browser.test.ts`,
      `frontend/tests/integration/auth-api.test.ts`.
