# MLWH Sync Admin Page

## What It Is

Admins read per-table MLWH sync health at frontend `/mlwh-sync` ("MLWH
sync"). Next.js calls `${WA_MLWH_BACKEND_URL}/freshness` without the admin
token. The page shows a banner (`role="alert"` "MLWH sync needs attention"
with counts, or `role="status"` "All N tables are in sync") and a table of
Table, Status, Last success, and Last error, worst status first. Each last
error is a `details` element that expands to the full text. The same data
backs `wa mlwh freshness [--check|--json]`.

## How To Reach It

- Account menu: log in (see the skill's Login), open the `<user> account`
  button, then `MLWH sync`. Admins see `Feedback`, `Usage`, `MLWH sync`,
  `Log out`.
- Direct URL: `$FRONTEND_URL/mlwh-sync`.
- API: `curl -s "$MLWH_URL/freshness"`.
- CLI: `WA_MLWH_SERVER_URL=$MLWH_URL "$RUN/wa" mlwh freshness --check`.

## How To Drive It

The fixture cache has five synced tables from May 2026, so a fresh stack
reports them `stale` and the rest `never_synced`. No sync can succeed on the
hermetic stack, but a failing one can be recorded for real. Save
`curl -s "$MLWH_URL/freshness"` as `$RUN/evidence/mlwh-sync-00-baseline.json`,
then fail a sync against the stack's cache, where `<cache>` is the
`--mlwh-cache` path in the `mlwh serve` arguments. Leave out `--env test`,
which refuses `WA_MLWH_DSN`:

```bash
env -u WA_ENV WA_MLWH_DSN='verify@tcp(127.0.0.1:1)/mlwarehouse?timeout=3s' \
  WA_MLWH_PASSWORD=dummy-secret WA_MLWH_CACHE_PATH=<cache> "$RUN/wa" mlwh sync
```

1. The sync exits 1. Every table then reports `failing` in `/freshness`,
   with `last_error` `connect to MLWH source: ... connection refused`, and
   the five fixture tables keep their May `last_success_at`. Nothing
   contains `dummy-secret`.
2. `"$RUN/wa" mlwh freshness --check` with `WA_MLWH_SERVER_URL=$MLWH_URL`
   exits 1, prints nothing on stdout, and prints on stderr one
   `17 tables failing (<tables>): connect to MLWH source: ...` line, since
   every table shares the error, plus
   `Error: mlwh sync status failing: 17 of 17 tables need attention`.
   Tables with their own error get one
   `<table>: failing, last success <time|never>: <error>` line each.
3. `node "$SKILL/scripts/drive-mlwh-sync.mjs" "$RUN"` (exit 0, `PASS`)
   opens the page from the menu and saves `mlwh-sync-01-page.png`,
   `mlwh-sync-02-study-error.png` (expanded) and `mlwh-sync-03-dark.png`.
   At 390px wide it checks that rows stack with no horizontal overflow and
   that the column and row headers keep their roles, printing a `narrow:`
   line, and saves `mlwh-sync-04-narrow.png` and
   `mlwh-sync-05-narrow-study.png`.

## Gotchas

- Non-OK states render one `role="status"` panel: "Log in to view MLWH sync
  status.", "You do not have access to MLWH sync status.", "Status
  unavailable: server too old." (a server older than MLWH API 1.12.0), "The
  MLWH server sent a sync status this page could not read." (a response that
  fails schema validation), or "The MLWH server could not be reached."
- This app's global `* { border-color }` rule is unlayered, so it beats
  Tailwind border-colour utilities; the page tints with `ring-*` instead.
  Check colours in screenshots, not class names.
- An `ok` state needs a successful sync, which the hermetic stack cannot
  run. The Go and Vitest suites cover it.
