# Usage Admin Page

## What It Is

The server admin reads MLWH API usage at frontend `/usage` ("MLWH usage"), a
dashboard: a filter bar, a KPI strip (`Requests`, `People`, `Sessions`,
`MCP requests`, `Faults`; `Errors` instead of `Faults` from a server
without severities), `Needs attention` (fault, then caller, rows merged
across routes by status and code, each a closed `details` with id
`err-<severity>-<status>-<code>`), `Activity` (an SVG chart stacked by
category with legend buttons and a "View as table" disclosure), `What
changed`, ranked bars for `People`, `Via what`, `Endpoints`, and `MCP
tools`, then closed panels `Expected outcomes`, `Older errors without
details` (only when there are some), and `Sessions and clients`.
Next.js reads the MLWH admin token from disk on the server side and calls
`${WA_MLWH_BACKEND_URL}/usage?window=<w>` plus any filter parameters
(`category`, `user`, `tool`, `agent`, `method`+`route`). The browser never
sees the token. The spec is `.docs/usage/spec.md` A4 (API) and E4 (page).

## How To Reach It

- Account menu: log in (see the skill's Login), open the `<user> account`
  button, then the `Usage` menu item. Admins see menu items `Feedback`,
  `Usage`, `MLWH sync`, `Log out`; the admin rule is the same as
  `/feedback`.
- Direct URL: `$FRONTEND_URL/usage`, with query `window=24h|7d|30d|all`
  (no or unknown `window` shows 7d) and filters such as
  `category=mcp`, `user=<name>`, `agent=claude-code`, or
  `method=GET&route=%2Fstudies`. An invalid filter is dropped whole.
- API, with `STATE_HOME` and `MLWH_URL` from `stack.env`:

    ```bash
    curl -s -H "Authorization: Bearer $(cat "$STATE_HOME/.wa-mlwh-server.token")" \
      "$MLWH_URL/usage?window=24h"
    ```

## How To Drive It

`node "$SKILL/scripts/drive-usage.mjs" "$RUN"` drives the page after the
traffic below: login through the account menu, `Last 24 hours`, KPIs,
Needs attention order, the CLI legend toggle and expected markers, no
sideways scroll at 1280 and 390 px, the `claude-code` agent filter (fewer
requests, a chip, the window link keeps it, the chip's remove link clears
it), a `#err-caller-400-bad_request-GET-studies` deep link opening its
`details`, screenshots in light and dark (`usagedash-<scheme>-page.png`,
`-attention.png`, `-activity.png`, `-expanded.png`,
`usagedash-light-filtered.png`), and axe on `main` with every disclosure
closed and then open. axe is the repo's transitive `axe-core` (from
`eslint-config-next`), injected from
`frontend/node_modules/.pnpm/axe-core@<version>/`; update the path in the
script if the lockfile moves it. It writes `usagedash-drive.json` and
exits 1 when a check fails or axe finds a serious or critical violation.
`node "$SKILL/scripts/drive-menu-after-login.mjs" "$RUN"` (exit 0, `PASS`)
checks that the `Feedback` and `Usage` items are in the menu opened the
moment login finishes; see [Feedback admin page](feedback-admin-page.md).

1. Record a baseline: `GET /usage?window=24h` with the token, saved as
   `$RUN/evidence/usage-00-baseline.json`. Test mode turns usage on with a
   throwaway DB, and the doctor's own `curl /freshness` is already counted
   as `other`.
2. Make CLI traffic:
   `"$RUN/wa" mlwh info 6568 --type study --server "$MLWH_URL"` sends 9
   requests with one `X-WA-Client-ID` and your username. For more people,
   send:

    ```bash
    curl -A wa-cli/v1.0.0 -H "X-WA-Client-User: carol" \
      -H "X-WA-Client-ID: run-carol" "$MLWH_URL/studies?limit=3"
    ```

3. Make errors of each severity. Expected outcomes and probes:
   `"$RUN/wa" mlwh info NOPE-XYZ --server "$MLWH_URL"` (exit 1, "no match")
   sends `/resolve/study/NOPE-XYZ` and `/resolve/sample-name/NOPE-XYZ` as
   probes (`X-WA-Lookup: probe`), then the final `/classify/NOPE-XYZ`;
   `wa mlwh info ERS10524782` adds two probe misses and succeeds. Then, with
   `curl -s -w ' %{http_code}\n'`: `"$MLWH_URL/classify/nonsense-xyz"`
   (404 `not_found`, expected), `"$MLWH_URL/studies?limit=-1&token=abc"`
   (400 `bad_request` from `other`, a caller error), the same URL with
   `-A wa-cli/v1.0.0` (a fault: wa's own CLI sent a bad request), and
   `"$MLWH_URL/no/such/route"` and `"$MLWH_URL/version"` (gin's plain 404,
   caller). The hermetic fixture's cache is synced, so 5xx faults cannot be
   produced live; the Go and Vitest tests cover them.
4. Make MCP traffic: for each tool `mlwh_classify_identifier`,
   `mlwh_list_studies`, and `mlwh_export` and each agent
   `claude-code/2.1.0`, `claude-code/2.1.5`, and `codex/0.9`, curl
   `"$MLWH_URL/classify/GUESSED-ID-42"` (expected) and
   `"$MLWH_URL/studies?limit=5"` with `User-Agent: mlwh-mcp-server/1.3.1`,
   `X-WA-MCP-Tool`, `X-WA-MCP-Agent`, and
   `X-WA-Client-ID: mcp-sess-<agent>`. Add one
   `"$MLWH_URL/studies?limit=-3"` from `codex/0.9` (an MCP caller error).
   The `verify-mlwh-mcp` skill in the llm-knowledge-base repo drives a real
   MCP server instead.
5. Check filters over the API (`usage-03-filters.txt`): with the token,
   `GET /usage?window=24h&category=mcp`, `&agent=claude-code` (both
   claude-code versions), `&agent=claude-code%2F2.1.0` (one),
   `&method=GET&route=%2Fstudies`, and `&user=%27%20OR%20%271%27%3D%271`
   (0 requests, 200) each echo `filter` and narrow `totals.requests`;
   `&category=bogus` and `&method=GET` answer 400
   `invalid usage filter: ...`.
6. Run `drive-usage.mjs`. Expect every check `ok: true` and every `axe`
   list empty or moderate only. Look at the screenshots: faults and caller
   rows with their action line, the chart with fault and caller markers,
   and the ranked bars with severity badges, in both themes.
7. Side effect: `GET /usage?window=24h` with the token, saved as
   `usage-02-after.json`, shows the same totals as the KPIs, a `filter` of
   `{}`, `bucket_unit` `hour` with 25 `buckets`, `previous_totals`, and
   `error_severity` on totals, days, buckets, categories, endpoints, MCP
   tools, and agents, where `fault`, `caller`, `expected`, and `probe` sum
   to the errors. Its `errors[]` groups match Needs attention and Expected
   outcomes, with `error_code` `null` only for rows recorded by a server
   older than the error details. In the raw rows, the CLI's probes have
   `lookup` `probe` and every other row NULL. To read raw rows, find the
   `--usage-db` path in the `mlwh serve` arguments
   (`.tmp/mlwh-usage-test.*.sqlite`) and open it read-only
   (`file:<path>?mode=ro` with SQLite URI mode). Do it before Cleanup,
   which deletes the file.

API checks, each with `curl -s -w '%{http_code}'`:

- No token or a wrong token: 401
  `{"code":"unauthorized","message":"admin token required"}`.
- Token and `window=1h`: 400 `invalid window "1h"`.
- Token and `category=bogus`: 400
  `invalid usage filter: unknown category "bogus"`.
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

- A fresh stack has one series period and records starting inside every
  window, so `What changed` says "Not enough history yet", KPI changes say
  "no earlier records", and the chart is clipped to the hours with records.
  The frontend unit tests cover multi-period changes and deltas.
- Hour labels on the chart are UTC; timestamps elsewhere are local.
- `next dev` compiles each route on first hit. The first `/usage` load can
  take several seconds.
