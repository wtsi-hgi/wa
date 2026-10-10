# Results Web UI

## What It Is

The Next.js app at `$FRONTEND_URL/`: a search builder over registered
pipeline result sets, a "Latest result sets" table, and a result detail page
(`/results/<id>`) with run details, metadata chips, and a file browser with
inline previews. Anonymous users see locked rows. Logging in unlocks rows
the user's groups can read.

## How To Reach It

- Dashboard and search: `$FRONTEND_URL/`. Filters live in the query string,
  for example `?user=alice`.
- Detail: click a project link in the table, or go to
  `$FRONTEND_URL/results/<id>` (id from `wa results search` or `get`).
- Login: the skill's Login recipe.

## How To Drive It

Use Playwright as described in the skill. Selectors are from
`frontend/e2e/`.

1. `goto($FRONTEND_URL)`, wait for text `Latest result sets`. Rows are
   `tbody tr[data-result-row="true"]`. The seed gives 10 on the first page.
   Screenshot.
2. Log in. Expect button `<user> account`.
3. Click `getByRole("link", { name: "nf-core/rnaseq" }).first()`. Expect URL
   `/results/<64-hex id>` and `heading level 1` named `nf-core/rnaseq`. The
   file browser shows `[data-directory-path]` entries (7 for rnaseq) and the
   header shows `137 files`. Screenshot.
4. Click `Back to dashboard`. Expect `/` again.
5. To prove a registration reaches the UI, register with the
   [results CLI](results-cli.md), then open that result's link by its
   workflow name. It shows its files, `Requester`, and `Study` and `Sample`
   chips.

Evidence: `$RUN/evidence/rw-*.png`.

## Gotchas

- Fixture output directories are under `.docs/results-web/fixtures/files/`
  in the checkout, so absolute paths in the UI name the checkout.
- `next dev` compiles routes on first hit. Allow 30 s for the first detail
  page.
- The search builder's add-filter flow (`add specific field to filter`,
  option, `<label> value` input, `Add`) is used in
  `frontend/e2e/results.spec.ts`, `addRequesterFilter`. Reuse it rather than
  typing into the generic box.
- The frontend binds all interfaces (`*:<port>`), not just loopback. Keep
  the port range private.
