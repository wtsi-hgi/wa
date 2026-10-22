# Border colour utilities have no effect in the web UI

- Branch: bordercolor-4eabad42
- Base: origin/develop @ fa77143
- Queue owner: k8s deployment delivery (llm-knowledge-base session); this
  checklist owns this branch only.

- [x] Tailwind border colour classes such as `border-destructive`,
      `border-primary`, `hover:border-primary/40` and `border-border/70` do
      not change the rendered border colour: every border is drawn in
      `--border`.
    - Red command:
      `cd frontend && pnpm exec vitest run tests/globals-css-border-color.test.ts`
      -> in Chromium, a `border border-destructive` element's border colour is
      `--border`, not `--destructive`.
    - Cause: `frontend/app/globals.css` set `* { border-color: var(--border) }`
      outside any cascade layer. In Tailwind v4 the utilities live in
      `@layer utilities`, and unlayered styles beat every layer regardless of
      specificity, so the universal rule overrode all border colour utilities.
    - Fixed in `frontend/app/globals.css`: the rule moved into `@layer base`,
      as in shadcn's Tailwind v4 template. Elements with a bare `border` class
      still default to `--border`.
    - Visible effect: borders now render as authored. The many
      `border-border/70`-style borders are lighter (their opacity now
      applies), hover and focus border colours appear, the usage page's
      alert tiles and error box get destructive borders, the default `Badge`
      border is transparent, and the file-preview dialogs use their
      `border-white/15` edge.
    - Test in `frontend/tests/globals-css-border-color.test.ts`: compiles
      `globals.css` with Tailwind and renders it in Chromium; a bare `border`
      is `--border`, and `border-destructive` and `border-primary` render
      their own colours.
