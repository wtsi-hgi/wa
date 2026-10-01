# Feedback: wa mlwh serve side

Add a "provide feedback" tool to the mlwh mcp, so llm agents using it can report when they had problems with a user request (it couldn't figure something out/ it make a mistake/ no endpoint could possibly answer the question/ user was unhappy).

Have wa mlwh serve (~/wa) collect that feedback and make it readable for the server admin.

This spec (/home/ubuntu/wa/.docs/feedback/spec.md) covers the wa mlwh serve side in this repo (/home/ubuntu/wa): accepting, storing, and exposing feedback to the server admin. A companion spec at /home/ubuntu/llm-knowledge-base/.docs/feedback/spec.md covers the MCP tool (repo /home/ubuntu/llm-knowledge-base). Both specs must agree on the wire contract between the MCP tool and the wa endpoint.

## Notes

### Existing system (findings)

- The MCP server (github.com/wtsi-hgi/llm-knowledge-base) imports `github.com/wtsi-hgi/wa/mlwh` as a tagged module (currently v0.9.1, no replace directive) and talks to wa through `wa.RemoteClient`. Named MCP tools call typed client methods (e.g. `mlwh_freshness` calls `client.Freshness`); `mlwh_call_endpoint` is a generic fallback whose method enum is built from `wa.Registry`, with parity tests. This feature therefore needs a new typed `RemoteClient` method in wa, a wa release tag, and a `go.mod` bump in the MCP repo, as earlier cross-repo features did (see llm-knowledge-base `.docs/realworld3/spec.md` "Authority and dependency").
- `wa.APIVersion` is currently "1.8.1" (`wa/mlwh/openapi.go`); adding the feedback endpoint bumps it (minor bump).
- `wa mlwh serve` opens its cache read-only, so feedback needs its own store.
- Serve mode is unauthenticated by default; secured mode (`--cert`, `--key`, `--server-token` together) puts endpoints behind `AuthRouter()` with server-token login only. There is no separate admin concept; "server admin" means whoever runs the process / holds the server token.
- The MCP server never sets `RemoteConfig.Token`, so it currently only works against an unsecured wa server. MCP HTTP mode is unauthenticated; stdio runs as the local OS user.
- The wa Next.js frontend (`wa/frontend`) has only the `(results)` app and reads MLWH data via `WA_MLWH_BACKEND_URL`; it has no admin pages yet.
- The MCP server publishes an `mlwh://workflow` resource guiding agents on tool choice (`internal/mlwh/workflow.go`).

### Decisions

- wa stores feedback in a separate SQLite database whose path is set by a new `wa mlwh serve` flag / env var (e.g. `--feedback-db` / `WA_MLWH_FEEDBACK_PATH`), independent of the read-only cache and of sync.
- The server admin reads feedback through a new page in the wa Next.js frontend.
- Feedback category is an enum of five values: the agent could not figure out how to answer; the agent made a mistake; no endpoint could possibly answer the question; the user was unhappy; other.
- Each report carries the agent's own description of the problem, and may optionally carry the user's original request verbatim.
- The MCP server automatically attaches its own version, the wa API version, and its transport (stdio or http). The agent may list the tools it tried in its own field; the MCP server does not record a call trace.
- wa records the request's remote address with each report. No other reporter identity is recorded.
- The MCP side exposes a new named tool (e.g. `mlwh_provide_feedback`) with typed inputs, which calls a new typed `RemoteClient` method (e.g. `SubmitFeedback`) that POSTs to wa.
- On wa the feedback POST is a plain, hand-registered route (like `/health` and `/openapi.json`), NOT a `wa.Registry` entry, so it is not callable via `mlwh_call_endpoint` and does not disturb Registry/OpenAPI parity tests. It is documented in `/openapi.json` by hand.
- In secured mode the feedback POST sits behind `AuthRouter()` like the other endpoints; in plain mode it is open like the other endpoints.
- The endpoint enforces per-field and total request body size caps (e.g. 64 KiB total) and rejects oversized bodies with 413. No rate limiting.
- Feedback is retained indefinitely; the admin can acknowledge (mark handled) and delete items.
- If wa is unreachable or too old to have the feedback endpoint, the MCP tool returns a structured tool error; there is no local fallback.
- The tool description and the `mlwh://workflow` resource tell agents to call the feedback tool on their own initiative whenever one of the categories applies, without asking the user first.
- When `wa mlwh serve` starts without a feedback DB path, feedback is disabled: the POST returns 503 with error code `feedback_disabled` (the MCP tool reports a structured tool error); existing deployments are unaffected.
- The MCP server also attaches, as optional fields, the connecting agent application's name and version taken from the MCP initialize handshake (client info). This identifies software, not a person.
- Admin endpoints on wa (list, acknowledge/un-acknowledge, delete feedback) are protected by a starter-user-only token, reusing the pattern `wa results serve` already uses (`wa/cmd/results.go` `resultsServeServerToken`/`writeResultsServeServerToken`: random token via `gas.GenerateToken`, stored mode 0600 in `gas.TokenDir()`, reused if present). When the feedback store is enabled, `wa mlwh serve` creates or reuses such a token file: in secured mode the existing `--server-token` file; in plain mode a default basename (e.g. `.wa-mlwh-server.token`) in the token dir. Admin requests must present that token (Bearer); the token is compared directly (constant time), with no gas JWT login step, so it works in plain mode too. The read-only Registry endpoints and the feedback POST are unaffected by this token.
- The Next.js server must run as the same OS user, on the same machine (or sharing the token dir), as `wa mlwh serve` (as `make dev` / `run-dev.sh` already do); it reads the token file server-side (as `run-dev.sh` `readServerToken` already does for results) and attaches it to admin calls. The browser never sees the token.
- A future starter-user-only CLI (e.g. `wa mlwh feedback list`) can read the same token file; it is out of scope now but the design must not preclude it.
- The Next.js feedback page requires LDAP login (existing `wa_results_jwt` session) AND the logged-in username must be in an allowlist (env var, e.g. `WA_FEEDBACK_ADMINS`). The OS user who started `wa mlwh serve` is always in the allowlist by default; since the Next.js process runs as that same OS user, Next.js uses its own OS username for this default. OS usernames match LDAP usernames at this site.
- Client info source: in stdio mode the MCP server takes the agent app's name/version from the initialize handshake (`req.Session.InitializeParams().ClientInfo`). In HTTP mode the server is stateless (`internal/core/http.go`, `Stateless: true`) so handshake ClientInfo is nil; there it records the incoming HTTP request's `User-Agent` header instead (`req.Extra.Header`). HTTP mode stays stateless.
- The remote address wa records is the MCP server host's address (the MCP server is wa's HTTP client), not the end user's machine; specs should say so.
- Admin routes are registered on the plain router with their own Bearer-token check (not on `AuthRouter()`, which requires a gas JWT). In secured mode the token file is the `--server-token` file gas already creates/reuses; in plain mode reuse `resultsServeServerToken`/`resultsServeServerTokenPath`-style logic with default basename `.wa-mlwh-server.token`. Admin endpoints return 503 `feedback_disabled` when no store is configured.
- Next.js resolves the mlwh token path the way gas does: `WA_MLWH_SERVER_TOKEN` absolute path as-is, a basename under `XDG_STATE_HOME` falling back to home dir, default basename `.wa-mlwh-server.token`. The feedback page shows an "unavailable" state when the token file is absent (e.g. remote MLWH via `WA_MLWH_SERVER_URL`) or wa reports `feedback_disabled`. Frontend uses Server Actions and Zod contracts (`lib/contracts.ts`) per project conventions, and the existing `currentSession()` for the LDAP username.
- `make dev` / `run-dev.sh`: in test mode feedback is always enabled with a temporary DB; in dev/prod modes it is enabled only when `WA_MLWH_FEEDBACK_PATH` is set (passed through to `wa mlwh serve`).
- The admin page lives at `/feedback`, linked from the auth menu only for allowlisted users. By default it shows unacknowledged items only, newest first, with a toggle to show all and a category filter. Delete asks for confirmation.
- On success the MCP tool returns the new report's id and received time. wa logs each received report.
- The tool description / `mlwh://workflow` resource tell agents to submit at most one report per distinct problem, and after submitting, to tell the user briefly that they reported it to the server admin.
- Defaults for the wire contract and implementation (spec author may refine): `POST /feedback` (submit; 201 `{id, created_at}`); admin `GET /feedback` (paginated with the existing `Page` limit/offset envelope, filters), `PATCH /feedback/{id}` (set/clear acknowledged), `DELETE /feedback/{id}`. JSON snake_case fields; category enum strings snake_case (e.g. `could_not_answer`, `agent_mistake`, `no_endpoint`, `user_unhappy`, `other`). wa assigns an integer id and UTC RFC3339 timestamp. 400 with the existing `{code,message}` error envelope for bad enum / missing description; 413 over cap; per-field caps within 64 KiB total. Feedback store uses the existing `modernc.org/sqlite` dependency. `RemoteClient` needs a new JSON-body POST path for `SubmitFeedback` reusing existing error-envelope decoding; no `RemoteClient` methods for admin endpoints. The MCP server cannot reach a secured wa (never sets `RemoteConfig.Token`), same as all existing tools; specs state this rather than fix it. Next.js only learns a non-default secured-mode token path via `WA_MLWH_SERVER_TOKEN`; document that. `/feedback` page sits in the `(results)` route group reusing layout, auth menu and `currentSession()`. No push notifications to the admin.
- Release order: wa ships first (APIVersion 1.8.1 -> 1.9.0, tag v0.10.0); the MCP repo then bumps its `github.com/wtsi-hgi/wa` dependency in go.mod.
