# Usage Admin Page

## What It Is

The server admin reads MLWH API usage at frontend `/usage` ("MLWH usage").
It shows totals (Requests, Errors, Sessions, Clients by IP, Users
(self-declared)), then sections `By category`, `Trend (24-hour periods)`
(or `7-day`, `30-day`; hidden for All time), `Endpoints`, `Errors`,
`MCP tools`, `MCP agents`, `Other API user agents`, `Unmatched paths`,
`Top users (self-declared)`, `Top clients`, and `Requests per day (UTC)`.
Next.js reads the MLWH admin token from disk on the server side and calls
`${WA_MLWH_BACKEND_URL}/usage?window=<w>`. The browser never sees the token.

## How To Reach It

- Account menu: log in (see the skill's Login), open the `<user> account`
  button, then the `Usage` menu item. Admins see menu items `Feedback`,
  `Usage`, `MLWH sync`, `Log out`; the admin rule is the same as
  `/feedback`.
- Direct URL: `$FRONTEND_URL/usage`, with query `window=24h|7d|30d|all`.
  No or unknown `window` shows 7d.
- API, with `STATE_HOME` and `MLWH_URL` from `stack.env`:

    ```bash
    curl -s -H "Authorization: Bearer $(cat "$STATE_HOME/.wa-mlwh-server.token")" \
      "$MLWH_URL/usage?window=24h"
    ```

## How To Drive It

No helper script for the page itself. Use Playwright as described in the
skill. `node "$SKILL/scripts/drive-menu-after-login.mjs" "$RUN"` (exit 0,
`PASS`) checks that the `Feedback` and `Usage` items are in the menu opened
the moment login finishes; see
[Feedback admin page](feedback-admin-page.md).

1. Record a baseline: `GET /usage?window=24h` with the token, saved as
   `$RUN/evidence/usage-00-baseline.json`. Test mode turns usage on with a
   throwaway DB, and the doctor's own `curl /freshness` is already counted
   as `other`.
2. Make CLI traffic:
   `"$RUN/wa" mlwh info 6568 --type study --server "$MLWH_URL"` sends 9
   requests with one `X-WA-Client-ID` and your username. `wa mlwh studies`
   needs `--programme`, `--user`, or `--faculty-sponsor`.
3. Make web traffic: log in, then open the `nf-core/rnaseq` result detail
   (see [Results web UI](results-web.md)). It calls `/enrich/6568` (200)
   and `/enrich/study-alpha` (404). The dashboard makes no MLWH calls, and
   anonymous visitors make none with this fixture.
4. Open the account menu and wait for menuitem `Usage`, then click it.
   Expect heading `MLWH usage`, a `Generated` timestamp, and the `Window`
   nav with `Last 7 days` marked `aria-current="page"`. Expect
   `By category` rows for `CLI`, `Web`, and `Other API` matching steps 1-3.
   Evidence: `usage-01-page.png` (full page).
5. Click each of `Last 24 hours`, `Last 30 days`, and `All time`. Expect
   the URL `window` to change, the clicked link to take `aria-current`, and
   the Trend section to disappear for All time. Evidence: one screenshot
   per window.
6. Errors: with `MLWH_URL` from `stack.env`, make one error of each kind
   with `curl -s -w ' %{http_code}\n'`: `"$MLWH_URL/classify/nonsense-xyz"`
   (404 `not_found`), `"$MLWH_URL/studies?limit=-1&token=abc"` (400
   `bad_request`), `"$MLWH_URL/no/such/route"` (gin's plain 404), and
   `"$MLWH_URL/classify/GUESSED-ID-42"` with headers
   `User-Agent: mlwh-mcp-server/1.3.1`,
   `X-WA-MCP-Tool: mlwh_classify_identifier`, and
   `X-WA-MCP-Agent: claude-code/2.1.0`. Open `/usage?window=24h`. Expect
   region `Errors` to start with a `What to do` group listing each code
   present once (`dt` code, `dd` action), then one `group` per endpoint
   (`h3` `GET /classify/:id`, `GET /studies`, `GET (no matching route)`,
   plus `GET /resolve/...` groups if step 2 used an unknown identifier),
   each holding `article`s named `<status> <code>` (`404 not_found`,
   `400 bad_request`, `404 no_route`) with a one-line meaning and a
   `What to do about <code>` link to the guide entry, but no action text of
   their own. Click `Messages and examples (...)` in `404 not_found`:
   expect `Top messages` `mlwh: identifier not found` and an example row
   `/classify/GUESSED-ID-42` whose Caller is
   `MCP · mlwh_classify_identifier · claude-code/2.1.0`. The
   `400 bad_request` example's Request is
   `/studies?limit=-1&token=[redacted]`. The Endpoints row `GET /studies`
   has its Errors cell link, named `1 error on GET /studies, see details`,
   pointing to `#usage-errors-GET-studies`, and `MCP tools` has an Errors
   column. Evidence: `usage-03-errors.png` (the region) and one expanded
   entry. Timeouts (`Causes: timeout`) cannot be produced on the hermetic
   stack; the Go tests cover them.
7. Side effect: `GET /usage?window=24h` with the token shows the same
   totals as the page, in `usage-02-after.json`, and its `errors[]` groups
   match the Errors section, with `error_code` `null` only for rows
   recorded by a server older than the error details. To read raw rows,
   find the `--usage-db` path in the `mlwh serve` arguments
   (`.tmp/mlwh-usage-test.*.sqlite`) and open it read-only
   (`file:<path>?mode=ro` with SQLite URI mode). Do it before Cleanup,
   which deletes the file.

API checks, each with `curl -s -w '%{http_code}'`:

- No token or a wrong token: 401
  `{"code":"unauthorized","message":"admin token required"}`.
- Token and `window=1h`: 400 `invalid window "1h"`.
- Server without `--usage-db`: 503 `usage_disabled`, with or without a
  token.

## Gotchas

- Non-OK states render one `role="status"` panel instead of the page. "Log
  in to view usage." means no session. "You do not have access to usage."
  means not an admin. "Usage is unavailable." comes with a reason: token
  unreadable, "Usage tracking is disabled on the MLWH server.", token
  rejected, server older than MLWH API 1.10.0, or unreachable.
- Only the OS user can log in, and it is always an admin. To show another
  admin or a non-admin, launch with `WA_FEEDBACK_ADMINS=carol` and set a
  `wa_results_jwt` cookie holding an RS512 JWT signed with
  `.tmp/wa-dev-key.pem`, claims `{Username, UID: "", orig_iat, exp}`.
  carol can read `nf-core/sarek`; bob can read `nf-core/rnaseq`.
- Disabled state: while the stack runs, copy its MLWH cache (the
  `--mlwh-cache` path in the `mlwh serve` arguments) into `$RUN` with
  SQLite's backup API; a never-synced cache fails readiness. Cleanup, then
  relaunch with the copy, where `<mlwh>` is the old stack's MLWH port
  (base+3) and `<copy>` and `<tmp file>` are double-quoted paths. run-dev
  `eval`s the command, so `printf '%q '` escapes each argument:

    ```bash
    WA_RUN_DEV_SEQMETA_CMD="$(printf '%q ' "$RUN/wa" mlwh serve --port <mlwh> \
      --url 127.0.0.1:<mlwh> --mlwh-cache <copy> --feedback-db <tmp file>)" \
      "$SKILL/scripts/launch.sh"
    ```

    run-dev passes no `--usage-db` to that command, so `/usage` shows "Usage
    is unavailable." and the doctor's usage check fails by design.

- A fresh stack has one Trend period, so multi-bar strips and the Previous
  column cannot be shown live. The frontend unit tests cover them.
- At narrow widths (390px) the full page is about 4000px tall. Take region
  screenshots per section.
- `next dev` compiles each route on first hit. The first `/usage` load can
  take several seconds.
