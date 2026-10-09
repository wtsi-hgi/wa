# Bugfix checklist: Prettier failure on feedback spec

- Branch: `feedback` (PR #34)
- Base: `origin/develop` at `33d14f275af6ffb31f547115bee4793317725505`
- Queue owner: caller (pr-resolver batch on `feedback`); this checklist

- [x] CI Lint job failed on PR head df0222a at `make lint-prettier-root` (Makefile:146): `frontend/node_modules/.bin/prettier --check .` at the repo root warns on `.docs/feedback/spec.md` ("Code style issues found").
    - Source: CI Lint job, PR #34 head df0222a
    - Red command: `make lint-prettier-root` (exit 2)

        ```text
        Checking formatting...
        [warn] .docs/feedback/spec.md
        [warn] Code style issues found in the above file. Run Prettier with --write to fix.
        make: *** [Makefile:146: lint-prettier-root] Error 1
        ```

    - Files touched: `.docs/feedback/spec.md`.
    - Approach: ran `prettier --write` on that file only. Beforehand, moved each
      inline code span that wrapped across lines onto one line, because Prettier
      otherwise strips the indentation from the continuation line inside the
      span. The change is formatting-only: table padding, JSON/TS fenced-block
      layout, nested list indentation (`tabWidth: 4`), and line breaks. Rendered
      micromark HTML, excluding fenced blocks, is identical before and after.
      `make lint` passes (lint-go, lint-frontend, lint-prettier-root).
