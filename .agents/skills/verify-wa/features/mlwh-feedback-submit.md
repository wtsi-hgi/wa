# MLWH Feedback Submit

## What It Is

An LLM agent using the MLWH MCP server (separate repo,
`wtsi-hgi/llm-knowledge-base`) reports a problem with a user request. The MCP
server sends it, without credentials, as JSON to `POST /feedback` on a
plain-mode `wa mlwh serve`. wa stores it in a separate SQLite feedback DB and
answers `201 {"id","created_at"}`. With no feedback DB configured, every
feedback route answers `503 {"code":"feedback_disabled"}`.

## How To Reach It

- Feedback on: `wa mlwh serve --feedback-db <sqlite path>`, or
  `WA_MLWH_FEEDBACK_PATH=<path>` with no flag. run-dev.sh test mode passes the
  flag with a throwaway DB. dev/prod pass it only when `WA_MLWH_FEEDBACK_PATH`
  is set.
- Feedback off: neither flag nor env var.
- Secured mode (`--cert/--key/--server-token`) moves submit to
  `POST /rest/v1/auth/feedback` behind a JWT. The MCP server cannot use that
  route, so this map does not drive it.
- Wire contract and caps: `.docs/feedback/spec.md`, `mlwh/feedback.go`.

## How To Drive It

1. Feedback on, through the run-dev stack (`--feedback-db` path):
   `node $SKILL/scripts/drive-feedback.mjs "$RUN"`. Steps 1-2 of its output
   cover this feature:
    - `POST $MLWH_URL/feedback` with an MCP-shaped body (category,
      description, user_request, tools_tried, version and client fields)
      returns 201 and an integer id. Evidence: `01-post-feedback.json`.
    - Side effect: unauthenticated `GET /feedback` is 401. With
      `Authorization: Bearer $(cat $STATE_HOME/.wa-mlwh-server.token)` it
      lists the row, every field round-tripped and `acknowledged:false`.
      Evidence: `02-admin-list-after-post.json`. The mlwh log
      (`$RUN/logs/mlwh.log`) shows `mlwh feedback received id=N`.
2. Feedback off, env-var on, and rejected paths, on a fresh never-synced
   cache on `SPARE_PORT`: `$SKILL/scripts/drive-feedback-modes.sh "$RUN"`.
   Expected `PASS` lines:
    - off: POST and admin GET both 503 `feedback_disabled`; no
      `.wa-mlwh-server.token` created; serve still starts and `/freshness` is 200.
    - `WA_MLWH_FEEDBACK_PATH` only: POST 201; DB and token file mode 0600;
      admin list 200; invalid category 400 `bad_request`.
    - `--feedback-db :memory:`: exits 1.

Both exit 0 only when every check passes. Evidence for step 2: `10-*`,
`11-*`, `12-*` in `$RUN/evidence/`.

## Gotchas

- `wa mlwh serve` prints nothing when it fails to start: config errors (no
  cache path, `:memory:`, a MySQL DSN as `--feedback-db`) and a missing
  `XDG_STATE_HOME` directory all give exit 1 with empty stderr. That is a
  known product bug, not a harness fault. If a serve dies silently, check
  those first.
- With feedback on, the admin token is created in `$XDG_STATE_HOME` (else
  `$HOME`) and the directory must already exist. A standalone serve without
  `XDG_STATE_HOME` uses `~/.wa-mlwh-server.token`. Point `XDG_STATE_HOME`
  at a scratch dir rather than touching that file.
- Serve needs no MLWH credentials or sync to start. On an empty cache data
  routes answer 503 `cache_never_synced`, while feedback works normally.
- `remote_addr` is the TCP peer. Forwarded headers are ignored by design.
- Test-mode feedback DB is deleted on shutdown. Capture rows before Cleanup.
