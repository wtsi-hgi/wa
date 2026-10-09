# MLWH Agent Feedback Specification

## Overview

LLM agents using the MLWH MCP server (repo `wtsi-hgi/llm-knowledge-base`)
will report problems with a user request: they could not work out an answer,
made a mistake, found no endpoint that could answer, saw the user was unhappy,
or something else went wrong. This spec covers the wa side. `wa mlwh serve`
accepts reports on `POST /feedback`, stores them in a separate SQLite
database, and exposes token-protected admin endpoints. A new `/feedback` page
in the Next.js frontend lets allowlisted LDAP users list, acknowledge, and
delete reports.

This spec owns the wire contract. The companion MCP spec
(`/home/ubuntu/llm-knowledge-base/.docs/feedback/spec.md`) consumes it through
`RemoteClient.SubmitFeedback` and must match it exactly.

Feedback is off unless a feedback DB path is configured. Existing deployments
are unaffected apart from the API version bump. Release: wa `v0.10.0`,
`APIVersion` `1.8.1` -> `1.9.0`.

## Architecture

### Files

```text
mlwh/feedback.go               types, categories, caps, validation
mlwh/feedback_store.go         SQLite store
mlwh/feedback_server.go        handlers, admin auth, route registration
mlwh/remote_feedback.go        RemoteClient.SubmitFeedback
mlwh/errors_http.go            new error codes and sentinels
mlwh/server.go                 WithFeedback option; RegisterRoutes wiring
mlwh/openapi.go                hand-documented POST /feedback; APIVersion
cmd/mlwh.go                    --feedback-db flag, store, admin token
run-dev.sh                     feedback DB wiring
frontend/lib/contracts.ts      Zod feedback schemas
frontend/lib/backend-client.ts mlwhJson options
frontend/lib/feedback-admin.ts token path, token read, allowlist
frontend/app/(results)/feedback/actions.ts  Server Actions
frontend/app/(results)/feedback/page.tsx    admin page
frontend/components/feedback-admin-view.tsx client view
frontend/components/auth-menu.tsx           Feedback link
frontend/app/(results)/layout.tsx           computes link visibility
```

Go tests sit beside each file (`mlwh/feedback_test.go`,
`mlwh/feedback_store_test.go`, `mlwh/feedback_server_test.go`,
`mlwh/remote_feedback_test.go`, `cmd/mlwh_test.go`,
`cmd/run_dev_test.go`). Frontend tests go in `frontend/tests/*.test.ts`.

### Wire contract

All bodies are JSON with snake_case fields. Errors use the existing
`{"code","message"}` envelope (`httpErrorEnvelope`).

| Method | Path            | Router          | Auth         | Success                    |
| ------ | --------------- | --------------- | ------------ | -------------------------- |
| POST   | `/feedback`     | Registry router | as Registry  | 201 `FeedbackReceipt`      |
| GET    | `/feedback`     | plain router    | admin Bearer | 200 `Page[FeedbackReport]` |
| PATCH  | `/feedback/:id` | plain router    | admin Bearer | 200 `FeedbackReport`       |
| DELETE | `/feedback/:id` | plain router    | admin Bearer | 204, empty body            |

- "Registry router" means the plain router in plain mode and `AuthRouter()`
  in secured mode. In secured mode the submit path is therefore
  `/rest/v1/auth/feedback` (gas `EndPointAuth` prefix), like every Registry
  endpoint, and requires a gas JWT.
- Admin routes always sit on the plain router at root paths, in both modes.
  They check the admin token themselves, not a gas JWT.
- None of these routes is a `Registry` entry. `mlwh_call_endpoint` cannot
  reach them and the Registry/Queryer/OpenAPI coverage tests stay unchanged.
  `TestOpenAPIErrorEnvelopeC2`, which pins every documented error code, does
  change (B7).

Error responses:

| Status | Code                | When                                                                               |
| ------ | ------------------- | ---------------------------------------------------------------------------------- |
| 400    | `bad_request`       | invalid JSON, bad enum, blank description, bad query/id                            |
| 401    | `unauthorized`      | admin request without the correct Bearer token                                     |
| 404    | `not_found`         | admin PATCH/DELETE of an unknown id                                                |
| 413    | `payload_too_large` | submit body over 65536 bytes or any field over its cap; PATCH body over 1024 bytes |
| 500    | `internal_error`    | store read or write failed                                                         |
| 503    | `feedback_disabled` | no feedback DB configured (all four routes)                                        |

Check order on every route: disabled (503), then admin auth (401, admin
routes only), then input (413 before 400), then store.

Submission caps (bytes of the UTF-8 string, `len(s)` in Go):

| Field                                                                                                     | Cap                | Required                                 |
| --------------------------------------------------------------------------------------------------------- | ------------------ | ---------------------------------------- |
| whole request body                                                                                        | 65536              | -                                        |
| `category`                                                                                                | enum               | yes                                      |
| `description`                                                                                             | 16384              | yes, non-blank after `strings.TrimSpace` |
| `user_request`                                                                                            | 16384              | no                                       |
| `tools_tried`                                                                                             | 50 items, each 128 | no                                       |
| `mcp_server_version`, `wa_api_version`, `transport`, `client_name`, `client_version`, `client_user_agent` | 256 each           | no                                       |

Unknown JSON fields are ignored, so newer clients can talk to this server.
The OpenAPI `FeedbackSubmission` schema says so with
`additionalProperties: true` (B7). Values are stored as received, with no
trimming.

Category enum, in order:

| Value              | Description (`FeedbackCategory.Description()`)         |
| ------------------ | ------------------------------------------------------ |
| `could_not_answer` | the agent could not work out how to answer the request |
| `agent_mistake`    | the agent made a mistake while answering               |
| `no_endpoint`      | no endpoint could possibly answer the question         |
| `user_unhappy`     | the user was unhappy with the answer                   |
| `other`            | any other problem with the request                     |

Example submit request and response:

```json
{
    "category": "no_endpoint",
    "description": "No endpoint lists sample consent.",
    "user_request": "Which samples have withdrawn consent?",
    "tools_tried": ["mlwh_search_samples", "mlwh_call_endpoint"],
    "mcp_server_version": "0.4.0",
    "wa_api_version": "1.9.0",
    "transport": "stdio",
    "client_name": "claude-code",
    "client_version": "2.1.0"
}
```

```json
{ "id": 7, "created_at": "2026-10-01T12:00:00Z" }
```

### Go types (`mlwh/feedback.go`)

```go
type FeedbackCategory string

const (
    FeedbackCategoryCouldNotAnswer FeedbackCategory = "could_not_answer"
    FeedbackCategoryAgentMistake   FeedbackCategory = "agent_mistake"
    FeedbackCategoryNoEndpoint     FeedbackCategory = "no_endpoint"
    FeedbackCategoryUserUnhappy    FeedbackCategory = "user_unhappy"
    FeedbackCategoryOther          FeedbackCategory = "other"
)

// FeedbackCategories returns the five categories in the order above.
func FeedbackCategories() []FeedbackCategory
// Description returns the table description; "" for unknown values.
func (c FeedbackCategory) Description() string
// Valid reports whether c is one of the five categories.
func (c FeedbackCategory) Valid() bool

const (
    FeedbackMaxBodyBytes        = 65536
    FeedbackMaxDescriptionBytes = 16384
    FeedbackMaxUserRequestBytes = 16384
    FeedbackMaxToolsTried       = 50
    FeedbackMaxToolNameBytes    = 128
    FeedbackMaxMetaBytes        = 256
)

type FeedbackSubmission struct {
    Category         FeedbackCategory `json:"category" doc:"problem category"`
    Description      string   `json:"description" doc:"the agent's own description of the problem"`
    UserRequest      string   `json:"user_request,omitempty" doc:"the user's original request, verbatim"`
    ToolsTried       []string `json:"tools_tried,omitempty" doc:"MCP tools the agent tried"`
    MCPServerVersion string   `json:"mcp_server_version,omitempty" doc:"reporting MCP server build version"`
    WAAPIVersion     string   `json:"wa_api_version,omitempty" doc:"MLWH API version the MCP server targets"`
    Transport        string   `json:"transport,omitempty" doc:"MCP transport: stdio or http"`
    ClientName       string   `json:"client_name,omitempty" doc:"agent application name from the MCP handshake"`
    ClientVersion    string   `json:"client_version,omitempty" doc:"agent application version from the MCP handshake"`
    ClientUserAgent  string   `json:"client_user_agent,omitempty" doc:"HTTP User-Agent of the agent application (MCP HTTP mode)"`
}

// Validate returns nil, or an error wrapping ErrFeedbackTooLarge (any cap)
// or ErrFeedbackInvalid (enum, blank description). Caps are checked first,
// then category, then description. The message names the offending field,
// e.g. "description exceeds 16384 bytes", "invalid category \"bogus\"",
// "description is required".
func (s FeedbackSubmission) Validate() error

type FeedbackReceipt struct {
    ID        int64  `json:"id" doc:"feedback report id"`
    CreatedAt string `json:"created_at" doc:"time wa received the report (UTC RFC3339)"`
}

// FeedbackReport is a stored report. Every field is always present in JSON;
// absent optional strings are "" and ToolsTried is [] (never null).
type FeedbackReport struct {
    ID               int64            `json:"id"`
    CreatedAt        string           `json:"created_at"`
    Category         FeedbackCategory `json:"category"`
    Description      string           `json:"description"`
    UserRequest      string           `json:"user_request"`
    ToolsTried       []string         `json:"tools_tried"`
    MCPServerVersion string           `json:"mcp_server_version"`
    WAAPIVersion     string           `json:"wa_api_version"`
    Transport        string           `json:"transport"`
    ClientName       string           `json:"client_name"`
    ClientVersion    string           `json:"client_version"`
    ClientUserAgent  string           `json:"client_user_agent"`
    RemoteAddr       string           `json:"remote_addr"`
    Acknowledged     bool             `json:"acknowledged"`
    AcknowledgedAt   string           `json:"acknowledged_at"`
}

type FeedbackFilter struct {
    Acknowledged *bool            // nil: both states
    Category     FeedbackCategory // "": all categories
}
```

`RemoteAddr` is the host part of `c.Request.RemoteAddr` (via
`net.SplitHostPort`; the raw value if splitting fails). It is never taken
from `X-Forwarded-For` or `c.ClientIP()`. For MCP traffic it is the MCP
server host, not the end user's machine. No other reporter identity is
stored.

### Errors (`mlwh/errors_http.go`)

New sentinels: `ErrFeedbackDisabled`, `ErrFeedbackInvalid`,
`ErrFeedbackTooLarge`, `ErrFeedbackUnsupported`, `ErrFeedbackUnauthorized`.
New code constants: `httpErrorCodeBadRequest = "bad_request"`,
`httpErrorCodeUnauthorized = "unauthorized"`,
`httpErrorCodePayloadTooLarge = "payload_too_large"`,
`httpErrorCodeInternal = "internal_error"`,
`httpErrorCodeFeedbackDisabled = "feedback_disabled"`.
`sentinelForHTTPErrorCode("feedback_disabled")` returns
`ErrFeedbackDisabled`. Existing mappings are unchanged.

### Store (`mlwh/feedback_store.go`)

SQLite via the existing `modernc.org/sqlite` driver, DSN
`file:<path>?mode=rwc&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)`.
It is independent of the cache and of sync.

```sql
CREATE TABLE IF NOT EXISTS feedback (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at         TEXT NOT NULL,
    category           TEXT NOT NULL,
    description        TEXT NOT NULL,
    user_request       TEXT NOT NULL DEFAULT '',
    tools_tried        TEXT NOT NULL DEFAULT '[]',
    mcp_server_version TEXT NOT NULL DEFAULT '',
    wa_api_version     TEXT NOT NULL DEFAULT '',
    transport          TEXT NOT NULL DEFAULT '',
    client_name        TEXT NOT NULL DEFAULT '',
    client_version     TEXT NOT NULL DEFAULT '',
    client_user_agent  TEXT NOT NULL DEFAULT '',
    remote_addr        TEXT NOT NULL DEFAULT '',
    acknowledged_at    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS feedback_ack_id ON feedback (acknowledged_at, id);
```

`tools_tried` holds a JSON array. `AUTOINCREMENT` stops ids being reused
after a delete. Timestamps are `time.Now().UTC().Format(time.RFC3339)`. The
store has an unexported `now func() time.Time` that tests override.
Retention is indefinite.

### Admin token

When feedback is enabled, `wa mlwh serve` creates or reuses a token file and
requires `Authorization: Bearer <token>` on admin routes. The comparison uses
`crypto/subtle.ConstantTimeCompare` on whitespace-trimmed bytes.

- Secured mode: the `--server-token` file that gas
  `EnableAuthWithServerToken` already creates. Resolve it with
  `resultsServeServerTokenPath(serveConfig.serverToken)` and read it with
  `resultsServeServerToken` after gas is enabled.
- Plain mode: basename `.wa-mlwh-server.token` (const
  `mlwhServeDefaultServerTokenBasename`), resolved the same way under
  `gas.TokenDir()`. It is created with `gas.GenerateToken` at mode 0600 and
  reused if present.
- Feedback disabled: no token file is created or read.

The token is the whole credential, with no JWT step, so it also works in
plain mode. A future starter-user CLI such as `wa mlwh feedback list` can read
the same file; this spec does not build one.

### Known limits (documented, not fixed)

- The MCP server never sets `RemoteConfig.Token`, so it can only submit to a
  plain-mode wa. `RemoteClient` appends each path to its base URL verbatim.
  Secured wa serves TLS only (gas `Start` -> `ListenAndServeTLS`) and mounts
  submit and Registry routes under `/rest/v1/auth`. gin's
  `HandleMethodNotAllowed` is off (`gin.New()`), so root `POST /feedback` is
  404 even though admin `GET /feedback` sits there. Without a token, the MCP
  side gets:

| MCP base URL                     | `SubmitFeedback`                                               | Registry calls               |
| -------------------------------- | -------------------------------------------------------------- | ---------------------------- |
| `http://host:port`               | 400 plain text from Go's TLS listener -> `ErrUpstreamImpaired` | `ErrUpstreamImpaired`        |
| `https://...`, untrusted cert    | transport failure -> `ErrUpstreamImpaired`                     | same                         |
| `https://host:port`              | 404 -> `ErrFeedbackUnsupported`                                | 404 -> `ErrUpstreamImpaired` |
| `https://host:port/rest/v1/auth` | gin-jwt 401 -> `ErrFeedbackUnauthorized`                       | 401 -> `ErrUpstreamImpaired` |

- Registry calls never get a wa envelope in these cases (gin-jwt's `code` is
  a number), so `decodeRemoteError` yields `ErrUpstreamImpaired`.
- Next.js finds a non-default secured-mode token path only via
  `WA_MLWH_SERVER_TOKEN`. A path given only as the `--server-token` flag is
  invisible to it.
- `wa mlwh serve` already reads `WA_MLWH_SERVER_TOKEN` as its
  `--server-token` default (`cmd/mlwh.go`), and any non-empty value makes it
  secured, so it then also needs `--cert` and `--key`. Set it only in secured
  mode, to the same value in the environment of both processes. Leave it
  unset in plain mode, where both sides use `.wa-mlwh-server.token`.
- Next.js calls admin routes at `${WA_MLWH_BACKEND_URL}/feedback`, so that
  URL must be the server root.
- The existing frontend MLWH reads (`mlwhJson` in `lib/studies-cache.ts` and
  `app/(results)/actions.ts`) send no JWT, so they already fail against a
  secured wa. With `WA_MLWH_BACKEND_URL` at the server root they get 404
  (Registry routes are under `/rest/v1/auth`), and only the feedback admin
  page works. This spec does not change that.
- `mlwhJson` has no HTTPS agent or CA option. Against a secured wa whose
  certificate is signed by a private CA, start Next.js with
  `NODE_EXTRA_CA_CERTS=<ca.pem>` so the admin page can connect.
- No rate limiting, no push notifications.

## A. Feedback Model and Store

### A1: Categories and validation

As the wa server, I want one authoritative feedback model, so that the MCP
side, the store, and the page agree on every field.

**Package:** `mlwh/`
**File:** `mlwh/feedback.go`
**Test file:** `mlwh/feedback_test.go`

Types, constants, and signatures as in Architecture.

**Acceptance tests:**

1. Given `FeedbackCategories()`, then it equals
   `[could_not_answer, agent_mistake, no_endpoint, user_unhappy, other]`. Each `Valid()` is true
   and each `Description()` equals the table text.
2. Given `FeedbackCategory("bogus")` and `FeedbackCategory("Other")`, then
   `Valid()` is false and `Description()` is `""`.
3. Given `{Category: "other", Description: "x"}`, when validated, then the
   error is nil.
4. Given category `"bogus"`, then `errors.Is(err, ErrFeedbackInvalid)` and the
   message contains `invalid category "bogus"`.
5. Given description `"  \n"` or `""`, then `ErrFeedbackInvalid` with
   `description is required`.
6. Given a description of exactly 16384 bytes, then nil. Given 16385 bytes,
   then `ErrFeedbackTooLarge` with `description exceeds 16384 bytes`.
7. Given `user_request` of 16385 bytes, then `ErrFeedbackTooLarge` naming
   `user_request`.
8. Given 50 tools of 128 bytes each, then nil. Given 51 tools, then
   `ErrFeedbackTooLarge` naming `tools_tried`. Given one tool of 129 bytes,
   then `ErrFeedbackTooLarge` naming `tools_tried`.
9. Given each of the six metadata fields set to 257 bytes in turn, then
   `ErrFeedbackTooLarge` naming that JSON field. At 256 bytes, nil.
10. Given category `"bogus"` and a 16385-byte description, then
    `ErrFeedbackTooLarge` (caps first).
11. Given a description of 4096 copies of "e" with an acute accent (2 bytes
    each, 8192 bytes), then nil, because caps count bytes, not runes.
12. Given the zero `FeedbackSubmission{}`, then `ErrFeedbackInvalid` with
    message containing `invalid category ""` (category before description).

### A2: SQLite feedback store

As the server admin, I want reports persisted durably and separately from
the cache, so that they survive restarts and cache rebuilds.

**Package:** `mlwh/`
**File:** `mlwh/feedback_store.go`
**Test file:** `mlwh/feedback_store_test.go`

```go
type FeedbackStore struct { /* db *sql.DB; now func() time.Time */ }

func OpenFeedbackStore(ctx context.Context, path string) (*FeedbackStore, error)
func (s *FeedbackStore) Close() error
func (s *FeedbackStore) Add(ctx context.Context, sub FeedbackSubmission, remoteAddr string) (FeedbackReport, error)
func (s *FeedbackStore) List(ctx context.Context, filter FeedbackFilter, limit, offset int) (Page[FeedbackReport], error)
func (s *FeedbackStore) SetAcknowledged(ctx context.Context, id int64, acknowledged bool) (FeedbackReport, error)
func (s *FeedbackStore) Delete(ctx context.Context, id int64) error
```

- `Add` does not validate (the handler does). Nil `ToolsTried` is stored as
  `[]`.
- `List` orders by `id DESC`. `Total` counts the filtered rows.
  `NextOffset` is `offset+len(Items)` when that is less than `Total`, else
  `-1`. `Items` is `[]`, never nil.
- `SetAcknowledged(true)` sets `acknowledged_at` to now only if it is empty,
  so the original time is kept. `false` sets it to `""`.
- `SetAcknowledged` and `Delete` of a missing id return an error wrapping
  `ErrNotFound`.

**Acceptance tests:**

1. Given a new path in `t.TempDir()`, when opened, then the file exists and
   `List(ctx, FeedbackFilter{}, 50, 0)` returns
   `{Items: [], Total: 0, NextOffset: -1}`.
2. Given `now` fixed at 2026-10-01T12:00:00Z, when `Add` is called with a
   full submission and `"10.0.0.5"`, then the report has `ID == 1`,
   `CreatedAt == "2026-10-01T12:00:00Z"`, every submitted field equal, and
   `RemoteAddr == "10.0.0.5"`, `Acknowledged == false`,
   `AcknowledgedAt == ""`.
3. Given a submission with nil `ToolsTried`, when added and listed, then
   `ToolsTried` is a non-nil empty slice and JSON-marshals as `[]`.
4. Given 3 reports added, when the store is closed and reopened, then `List`
   returns ids `[3, 2, 1]`.
5. Given 5 reports, when `List(..., 2, 2)`, then ids `[3, 2]`, `Total == 5`,
   `NextOffset == 4`. When `List(..., 2, 4)`, then ids `[1]`,
   `NextOffset == -1`.
6. Given ids 1 and 2 with id 1 acknowledged, when listed with
   `Acknowledged: &false`, then `[2]`. With `&true`, then `[1]`. With nil,
   then `[2, 1]`.
7. Given categories `other` (id 1) and `no_endpoint` (id 2), when listed with
   `Category: "no_endpoint"`, then `[2]` with `Total == 1`.
8. Given `now` at T1, when `SetAcknowledged(1, true)` runs, then
   `Acknowledged == true` and `AcknowledgedAt == T1`. Given `now` advanced
   to T2 and a second `SetAcknowledged(1, true)`, then `AcknowledgedAt` stays
   T1. After `SetAcknowledged(1, false)`, `Acknowledged == false` and
   `AcknowledgedAt == ""`.
9. Given id 99 absent, then `SetAcknowledged` and `Delete` return
   `errors.Is(err, ErrNotFound)`.
10. Given ids 1 and 2, when `Delete(1)` runs and a new report is added, then
    `List` returns `[3, 2]`. When `Delete(3)` then removes the highest id and
    another report is added, then `List` returns `[4, 2]`. Ids are never
    reused.
11. Given a closed store, when `Add` is called, then it returns a non-nil
    error.

## B. HTTP Endpoints

### B1: Submit feedback

As an MCP server, I want to POST a report and get its id, so that I can
confirm submission to the agent.

**Package:** `mlwh/`
**File:** `mlwh/feedback_server.go`, `mlwh/server.go`
**Test file:** `mlwh/feedback_server_test.go`

```go
// WithFeedback enables feedback. store may be nil (disabled). adminToken is
// the admin Bearer token; ignored when store is nil.
func WithFeedback(store *FeedbackStore, adminToken []byte) ServerOption
```

Handler steps:

1. Store nil -> 503 `feedback_disabled`, message
   `feedback is disabled on this server`.
2. `Content-Length` over `FeedbackMaxBodyBytes` -> 413. Read through
   `http.MaxBytesReader(..., FeedbackMaxBodyBytes)`. A `*http.MaxBytesError`
   -> 413 `request body exceeds 65536 bytes`.
3. Decode one JSON object with `json.NewDecoder`, then decode again and
   require `io.EOF`, as `decodeJSONBody` in `results/server.go` does.
   Empty body, malformed JSON, a non-object, or trailing data after the
   object (another value or garbage; trailing whitespace is fine) -> 400
   `invalid JSON body`. A `*http.MaxBytesError` from either decode is still 413. A literal `null` decodes without error to a zero submission, so it
   is not special-cased and fails step 4 with `invalid category ""`.
4. `Validate()`: `ErrFeedbackTooLarge` -> 413, `ErrFeedbackInvalid` -> 400.
   The envelope message is the validation message.
5. `store.Add` failure -> 500 `internal_error`, message
   `could not store feedback`.
6. Log
   `slog.Default().Info("mlwh feedback received", "id", id, "category", category, "remote_addr", addr)`.
7. 201 with `FeedbackReceipt`.

**Acceptance tests:**

1. Given a plain-mode server with a temp store, when POSTing the
   Architecture example, then 201, a body of exactly
   `{"id":1,"created_at":"<RFC3339 UTC>"}`, and the stored report equals the
   submission with `RemoteAddr` equal to the request's remote host
   (`192.0.2.1` for an `httptest` request).
2. Given header `X-Forwarded-For: 203.0.113.9`, when POSTing, then the stored
   `RemoteAddr` is still the connection host, not `203.0.113.9`.
3. Given a captured default slog handler, when one report is accepted, then
   one log record has message `mlwh feedback received` and attributes
   `id=1`, `category=no_endpoint`, `remote_addr=192.0.2.1`.
4. Given body `{"category":"bogus","description":"x"}`, then 400
   `{"code":"bad_request"}`, message containing `invalid category`, and no
   row stored.
5. Given a blank description, then 400 `bad_request`.
6. Given bodies `""`, `not json`, `[1]`, the Architecture example followed
   by `{}`, and the example followed by `x`, then each gets 400
   `bad_request` with message `invalid JSON body` and no row is stored.
   Given the example followed by `"\n"`, then 201.
7. Given a 70000-byte body (valid JSON with a long `user_request`), then 413
   `payload_too_large` and no row stored.
8. Given a 16385-byte description in a body under 65536 bytes, then 413
   `payload_too_large` with message naming `description`.
9. Given an extra field `"future_field":"x"`, then 201.
10. Given a store closed before the request, then 500 `internal_error`.
11. Given two sequential valid POSTs, then ids 1 and 2.
12. Given body `null`, then 400 `bad_request` with message
    `invalid category ""` and no row stored.

### B2: Admin authentication

As the server admin, I want admin routes usable only with the starter-user
token, so that other users cannot read or change reports.

**Package:** `mlwh/`
**File:** `mlwh/feedback_server.go`
**Test file:** `mlwh/feedback_server_test.go`

Middleware on the three admin routes: store nil -> 503 `feedback_disabled`.
Otherwise `Authorization` must be `Bearer ` plus a value equal to the token
(constant-time, trimmed). Else 401 `unauthorized`, message
`admin token required`.

**Acceptance tests:**

1. Given token `T`, when `GET /feedback` has no `Authorization`, then 401
   `{"code":"unauthorized","message":"admin token required"}`.
2. Given `Authorization: Bearer wrong`, `Basic T`, or `Bearer ` (empty),
   then 401 for each of GET, PATCH, and DELETE.
3. Given `Authorization: Bearer T`, then `GET /feedback` returns 200.
4. Given a server with feedback disabled, when `GET /feedback` is called
   with or without a token, then 503 `feedback_disabled`.
5. Given token `T`, when `POST /feedback` is sent without `Authorization` in
   plain mode, then 201, because submit does not use the admin token.
6. Given token `T`, when `GET /studies` is sent without `Authorization` in
   plain mode, then its status is unchanged from current behaviour (not
   401).

### B3: List feedback

As the server admin, I want paginated, filterable listing, newest first, so
that I can triage reports.

**Package:** `mlwh/`
**File:** `mlwh/feedback_server.go`
**Test file:** `mlwh/feedback_server_test.go`

`GET /feedback` query parameters:

- `limit`: default 50, must be 0-500. `0` is allowed, following the existing
  list endpoints (`mlwhPaginationFromQuery` and
  `mlwhSearchPaginationFromQuery` in `mlwh/server.go` reject only negative
  limits). It returns no items with the filtered `total`.
- `offset`: default 0, must be >= 0.
- `acknowledged`: `true` or `false`; absent means both.
- `category`: one of the enum values.

An invalid value gets 400 `bad_request` naming the parameter. The body is
`Page[FeedbackReport]` JSON: `{"items":[...],"total":N,"next_offset":M}`.

**Acceptance tests:**

1. Given 3 reports, when GET with a valid token, then 200, `items` ids
   `[3,2,1]`, `total` 3, `next_offset` -1. Each item has all 15 JSON keys
   from `FeedbackReport`.
2. Given 60 reports, when GET with no params, then 50 items, `total` 60,
   `next_offset` 50.
3. Given `?limit=501`, `?limit=-1`, `?limit=x`, `?offset=-1`,
   `?acknowledged=yes`, or `?category=bogus`, then 400 `bad_request` for
   each.
4. Given id 1 acknowledged and id 2 not, when `?acknowledged=false`, then
   items `[2]`, `total` 1.
5. Given `?category=user_unhappy&acknowledged=true` with one matching
   report, then exactly that report.
6. Given an empty store, then `{"items":[],"total":0,"next_offset":-1}`.
7. Given 3 reports, when `?limit=0`, then 200
   `{"items":[],"total":3,"next_offset":0}`.

### B4: Acknowledge and un-acknowledge

As the server admin, I want to mark reports handled or unhandled, so that I
can track what still needs attention.

**Package:** `mlwh/`
**File:** `mlwh/feedback_server.go`
**Test file:** `mlwh/feedback_server_test.go`

`PATCH /feedback/:id` with body `{"acknowledged": true|false}`. The body is
read first, through `http.MaxBytesReader(..., feedbackMaxPatchBodyBytes)`
(unexported const `1024` in `mlwh/feedback_server.go`). `Content-Length` over
the cap, or a `*http.MaxBytesError` from decoding -> 413 `payload_too_large`,
message `request body exceeds 1024 bytes`. `:id` must be
a positive base-10 int64, else 400 `invalid feedback id`. A missing or
non-boolean `acknowledged` gets 400 `acknowledged must be a boolean`. A
missing id (`ErrNotFound` from the store) gets 404 with the JSON envelope
`{"code":"not_found","message":"feedback <id> not found"}`; the frontend
relies on that code to tell a missing report from a missing route (E3).
Returns 200 with the updated report.

**Acceptance tests:**

1. Given report 1, when PATCH `{"acknowledged":true}`, then 200 with
   `acknowledged` true and a non-empty RFC3339 `acknowledged_at`.
2. Given an acknowledged report 1, when PATCH `{"acknowledged":false}`, then
   200 with `acknowledged` false and `acknowledged_at` `""`.
3. Given PATCH `/feedback/99`, then 404, `Content-Type` `application/json`,
   body `{"code":"not_found","message":"feedback 99 not found"}`.
4. Given PATCH `/feedback/abc`, `/feedback/0`, or `/feedback/-1`, then 400
   `bad_request`.
5. Given bodies `{}`, `{"acknowledged":"yes"}`, or `not json`, then 400
   `bad_request`.
6. Given report 1 and PATCH `/feedback/1` with a 2000-byte body
   `{"acknowledged":true,"pad":"<padding>"}`, then 413
   `{"code":"payload_too_large"}` with message
   `request body exceeds 1024 bytes`, and report 1 is still unacknowledged.

### B5: Delete feedback

As the server admin, I want to delete reports, so that I can remove noise.

**Package:** `mlwh/`
**File:** `mlwh/feedback_server.go`
**Test file:** `mlwh/feedback_server_test.go`

`DELETE /feedback/:id`: same id rules as B4. 204 with empty body. Missing id
gets the same 404 `not_found` envelope as B4.

**Acceptance tests:**

1. Given reports 1 and 2, when DELETE `/feedback/1`, then 204 with a
   zero-length body, and a later GET lists only `[2]`.
2. Given DELETE `/feedback/1` repeated, then the second returns 404 with
   body `{"code":"not_found","message":"feedback 1 not found"}`.
3. Given DELETE `/feedback/abc`, then 400 `bad_request`.

### B6: Route placement and disabled mode

As an operator, I want feedback routes wired consistently in both serve
modes, so that the security posture is predictable.

**Package:** `mlwh/`
**File:** `mlwh/server.go`, `mlwh/feedback_server.go`
**Test file:** `mlwh/feedback_server_test.go`

`RegisterRoutes(router, auth)` always registers the four routes, enabled or
not. Submit goes on `auth` when non-nil, else on `router`. Admin routes go
on `router`.

**Acceptance tests:**

1. Given `NewServer(q)` with no `WithFeedback`, when `POST /feedback` is
   sent in plain mode, then 503
   `{"code":"feedback_disabled","message":"feedback is disabled on this server"}`.
2. Given `WithFeedback(nil, nil)`, then the same as test 1, and PATCH and
   DELETE `/feedback/1` also return 503.
3. Given a gin engine and a separate group `/rest/v1/auth` passed as `auth`,
   with feedback enabled, then `POST /rest/v1/auth/feedback` returns 201,
   `POST /feedback` returns 404, and `GET /feedback` with the admin token
   returns 200.
4. Given `len(Registry)` before this feature, then it is unchanged and no
   `Registry` entry has path `/feedback`.

### B7: OpenAPI document and API version

As an external implementor, I want `POST /feedback` described in
`/openapi.json`, so that I can see the contract without reading Go.

**Package:** `mlwh/`
**File:** `mlwh/openapi.go`, `MLWH_API_REFERENCE.md`
**Test file:** `mlwh/openapi_test.go`, `mlwh/docs_test.go`

Set `APIVersion = "1.9.0"` and update its doc comment. In
`OpenAPIDocument`, replace `info.description` (now "Cache-backed, read-only
REST API ...") with exactly:

```text
Cache-backed REST API mirroring Multi-LIMS Warehouse (MLWH) study, sample, run, and library metadata. MLWH data is read-only; the only documented write endpoint is POST /feedback, which stores agent feedback in a separate database. Unauthenticated by default; the network boundary is the access-control boundary.
```

No existing test pins the old wording, and `MLWH_API_REFERENCE.md` does
not include it. Add
`addOpenAPIFeedbackPath(paths, collector)`, called from `OpenAPIDocument`
next to `addOpenAPIHealthPath`. It documents `post` on `/feedback` with:

- `summary` `Submit agent feedback`.
- A description stating it is not a Registry endpoint and is behind
  `AuthRouter()` in secured mode.
- A required `application/json` request body referencing
  `#/components/schemas/FeedbackSubmission`, registered via
  `collector.schemaForType`. Its `category` property gains `enum` from
  `FeedbackCategories()`. `registerStruct` emits
  `additionalProperties: false` for every struct, so after registration set
  `additionalProperties` to `true` on this component only, matching the
  ignore-unknown-fields rule. Other components are unchanged.
- `201` referencing `FeedbackReceipt`, and `400`, `413`, `500`, `503` error
  responses built with `openAPIErrorResponseObject`, with codes
  `bad_request`, `payload_too_large`, `internal_error`, `feedback_disabled`.
  `openAPIErrorResponses()` (the six Registry codes) is unchanged.

`TestOpenAPIErrorEnvelopeC2` (`mlwh/openapi_test.go`) asserts with
`ShouldResemble` the code -> status map collected from every operation, so it
must change: add `"payload_too_large": "413"`, `"internal_error": "500"`,
and `"feedback_disabled": "503"` to its expected map, and change "six stable
codes" in its Convey description to "nine stable codes".

Admin routes are not documented. Regenerate `MLWH_API_REFERENCE.md` with
`WA_REFRESH_DOCS=1 go test ./mlwh -run TestWriteEndpointReference`. Only its
version line changes.

**Acceptance tests:**

1. Given `APIVersion`, then it equals `"1.9.0"` and the served
   `info.version` equals it. Update
   `TestAPIVersionIsThePhase1DocumentationPatch` to the new value.
2. Given the document, then `paths["/feedback"]["post"]` exists with
   `requestBody.required == true`, a schema `$ref` of
   `#/components/schemas/FeedbackSubmission`, response keys exactly
   `{201, 400, 413, 500, 503}`, and no `x-queryer-method`.
3. Given component `FeedbackSubmission`, then its properties are exactly the
   10 JSON names, `required` is `[category, description]`,
   `category.enum` equals the five values in order, and
   `additionalProperties` is `true`. Component `FeedbackReceipt` still has
   `additionalProperties` `false`.
4. Given component `FeedbackReceipt`, then properties `id` (integer) and
   `created_at` (string) are both required.
5. Given the document, then `paths["/feedback"]` has no `get`, `patch`, or
   `delete`, and `/feedback/{id}` is absent.
6. Given the committed `MLWH_API_REFERENCE.md`, then the existing no-drift
   test passes and it contains ``**API version:** `1.9.0` ``.
7. Given the document, then `TestOpenAPIErrorEnvelopeC2` passes with
   exactly nine codes: the existing six plus `payload_too_large` -> `413`,
   `internal_error` -> `500`, and `feedback_disabled` -> `503`.
8. Given the served `/openapi.json`, then `info.description` equals the
   text above, contains `POST /feedback`, and does not contain
   `read-only REST API`.

## C. Remote Client

### C1: RemoteClient.SubmitFeedback

As the MCP server, I want a typed client method, so that I can submit
feedback without hand-building HTTP.

**Package:** `mlwh/`
**File:** `mlwh/remote_feedback.go`
**Test file:** `mlwh/remote_feedback_test.go`

```go
// SubmitFeedback POSTs submission to <BaseURL>/feedback and returns the
// receipt. It is not a Queryer method and has no Registry entry.
func (rc *RemoteClient) SubmitFeedback(ctx context.Context, submission FeedbackSubmission) (FeedbackReceipt, error)
```

- Body: `json.Marshal(submission)`, header
  `Content-Type: application/json`, and `Authorization: Bearer <token>` when
  `RemoteConfig.Token` is set. Uses `rc.httpClient`, so timeout, CA, and
  proxy behaviour match other calls. No client-side validation.
- Any 2xx: decode `FeedbackReceipt`. A decode failure wraps
  `ErrUpstreamImpaired`.
- Non-2xx: read the body once (`io.ReadAll` of
  `io.LimitReader(response.Body, FeedbackMaxBodyBytes)`). Decode it into
  `httpErrorEnvelope` with `json.NewDecoder(bytes.NewReader(raw)).Decode`,
  as `decodeRemoteError` does. Then set
  `response.Body = io.NopCloser(bytes.NewReader(raw))` and build
  `base := decodeRemoteError(response, submitFeedbackEndpoint, proxyURL)`, which keeps
  the envelope message and the proxy hints. `submitFeedbackEndpoint` is an
  unexported
  `Endpoint{Method: "SubmitFeedback", NewResult: func() any { return &FeedbackReceipt{} }}`, not added to `Registry`. `NewResult` must be
  set: on a `cache_never_synced` envelope `decodeRemoteError` calls
  `endpointResultIsSlice(entry)`, which calls `entry.NewResult()` and would
  panic on nil. The result is a non-slice, so no `ErrNotFound` is joined.
  A body that does not decode gives a `base` wrapping `ErrUpstreamImpaired`
  with message
  `remote SubmitFeedback returned <status> without a valid MLWH error envelope; ...`. Return `fmt.Errorf("%w: %w", sentinel, base)`, where
  sentinel is picked by status:

| Status                                    | Sentinel                  |
| ----------------------------------------- | ------------------------- |
| 400, envelope decoded, code `bad_request` | `ErrFeedbackInvalid`      |
| 400 otherwise                             | none, return `base`       |
| 401                                       | `ErrFeedbackUnauthorized` |
| 404                                       | `ErrFeedbackUnsupported`  |
| 413                                       | `ErrFeedbackTooLarge`     |
| 503 with code `feedback_disabled`         | `ErrFeedbackDisabled`     |
| anything else                             | none, return `base`       |

- Only the wa handler sends 400 with a `bad_request` envelope. A 400
  without one is not about the input: Go's TLS listener answers a plain
  `http://` request with a 400, no headers, and text body
  `Client sent an HTTP request to an HTTPS server.` (Known limits).
- 401, 404, and 413 map by status alone. wa's own 413s all carry the
  envelope; gas, gin, and `net/http` send none on this route (gin's 413 is
  only from `c.Bind`, which the handler does not use). A 413 from a reverse
  proxy body limit still means the body is too large.
- 404 means no `POST <BaseURL>/feedback` route: a wa older than API 1.9.0,
  or a secured wa addressed at its root URL (Known limits). Any 404 body
  maps to `ErrFeedbackUnsupported`.
- `base` wraps `ErrUpstreamImpaired` whenever `sentinelForHTTPErrorCode`
  returns nil, which includes `bad_request`, `unauthorized`, and
  `payload_too_large`, and whenever the body is not an envelope. So 400, 401,
  and 413 errors satisfy both their feedback sentinel and
  `errors.Is(err, ErrUpstreamImpaired)`. Callers must check the feedback
  sentinels before `ErrUpstreamImpaired`.
- A transport failure gets `ErrUpstreamImpaired` with message
  `SubmitFeedback request failed: ...`, as in `do`.
- A nil client returns `ErrUpstreamImpaired`.

**Acceptance tests:**

1. Given a stub returning 201 `{"id":7,"created_at":"2026-10-01T12:00:00Z"}`,
   when called, then the receipt is `{7, "2026-10-01T12:00:00Z"}`, the stub
   saw `POST /feedback` with `Content-Type: application/json`, and the
   decoded body equals the submission.
2. Given base URL `http://host/rest/v1/auth` and `Token: "jwt"`, then the
   request path is `/rest/v1/auth/feedback` with header
   `Authorization: Bearer jwt`. Given no token, there is no `Authorization`
   header.
3. Given 503
   `{"code":"feedback_disabled","message":"feedback is disabled on this server"}`, then `errors.Is(err, ErrFeedbackDisabled)` and
   `err.Error()` contains `feedback is disabled on this server`.
4. Given a 404 with text body `404 page not found`, then
   `errors.Is(err, ErrFeedbackUnsupported)`.
5. Given 400 `{"code":"bad_request","message":"invalid category \"x\""}`,
   then `ErrFeedbackInvalid` and the message is kept.
6. Given 400 with text body
   `Client sent an HTTP request to an HTTPS server.\n`, or 400 `{"message":"x"}` (no code), then
   `errors.Is(err, ErrFeedbackInvalid)` is false and
   `errors.Is(err, ErrUpstreamImpaired)` is true. The text-body error
   contains `without a valid MLWH error envelope`.
7. Given 413 `payload_too_large`, then `ErrFeedbackTooLarge`. Given 401 with
   any body, then `ErrFeedbackUnauthorized`. For the 413, for a 401 with
   gin-jwt body `{"code":401,"message":"x"}`, and for test 5,
   `errors.Is(err, ErrUpstreamImpaired)` is also true.
8. Given 500 `internal_error` or 503 `cache_never_synced`, then the call
   does not panic, no feedback sentinel matches, and the error is non-nil
   with the envelope message kept. For `cache_never_synced`,
   `errors.Is(err, ErrCacheNeverSynced)` is true and
   `errors.Is(err, ErrNotFound)` is false.
9. Given a closed listener, then `errors.Is(err, ErrUpstreamImpaired)`.
10. Given 201 with body `not json`, then `ErrUpstreamImpaired`.
11. Given the real `NewServer(..., WithFeedback(store, token))` behind
    `httptest`, when `SubmitFeedback` is called, then it returns id 1 and
    the store holds the report (round trip).

## D. Serve Command and Dev Script

### D1: --feedback-db flag and admin token

As an operator, I want feedback enabled by one setting, so that existing
deployments are unaffected.

**Package:** `cmd/`
**File:** `cmd/mlwh.go`
**Test file:** `cmd/mlwh_test.go`

- Flag `--feedback-db`, default `firstEnv("WA_MLWH_FEEDBACK_PATH")`, help
  `SQLite path for agent feedback; feedback is disabled when unset`. Add a
  `Long` help paragraph naming `--feedback-db`, `WA_MLWH_FEEDBACK_PATH`, and
  `.wa-mlwh-server.token`.
- Move `server := mlwh.NewServer(client)` from before the secured branch to
  after it. In secured mode the token file may not exist until
  `EnableAuthWithServerToken` creates it. The `RunE` order becomes:
    1. `authServer := mlwhServeNewAuthServer(...)`, as now.
    2. Secured only: `EnableAuthWithServerToken`, then
       `configureMLWHServeRouter`.
    3. `openMLWHServeFeedback` (below), then defer closing a non-nil store.
    4. `server := mlwh.NewServer(client, mlwh.WithFeedback(store, token))`.
    5. `server.RegisterRoutes(authServer.Router(), authServer.AuthRouter())`
       when secured, else `server.RegisterRoutes(authServer.Router(), nil)`.
    6. `startMLWHServeAuthServer`.

```go
// openMLWHServeFeedback returns (nil, nil, nil) when feedbackDB is blank.
// It rejects a MySQL-looking DSN, creates the parent directory
// (ensureMLWHSyncCacheDirectory), opens the store, and resolves the admin
// token as in Architecture.
func openMLWHServeFeedback(ctx context.Context, feedbackDB string, config mlwhServeConfig) (*mlwh.FeedbackStore, []byte, error)
```

- The deferred close runs after `startMLWHServeAuthServer` returns, so the
  store outlives the server.

**Acceptance tests:**

1. Given `XDG_STATE_HOME` set to a temp dir and a blank `feedbackDB`, then
   it returns nil store, nil token, nil error, and no
   `.wa-mlwh-server.token` exists.
2. Given plain config and `feedbackDB` `<tmp>/sub/fb.sqlite`, then `sub/` and
   the DB file are created, and
   `$XDG_STATE_HOME/.wa-mlwh-server.token` exists with mode 0600. The
   returned token equals the file contents.
3. Given that token file already exists, when called again, then the same
   token is returned and the file is unchanged.
4. Given secured config with `serverToken` `.my.token` and an existing
   0600 file `$XDG_STATE_HOME/.my.token` holding 43 bytes (43 `a`, no
   newline), then that file's token is returned, the file is unchanged, and
   `.wa-mlwh-server.token` is not created. The fixture must be at least 43
   bytes and mode exactly 0600: go-authserver v1.6.0 `GetStoredToken`
   returns `(nil, nil)` below `tokenLength` (43) and an error for other
   modes, and `resultsServeServerToken` then overwrites the file.
5. Given `feedbackDB` `user@tcp(db:3306)/fb`, then the error mentions
   `SQLite file path` and no token file is created.
6. Given `WA_MLWH_FEEDBACK_PATH=<tmp>/fb.sqlite` and no flag, when the serve
   command resolves flags, then the flag value equals that path. Given
   `--feedback-db <other>`, then the flag wins.
7. Given the `mlwh serve` command from `NewRootCommand()`, then its `Long`
   contains `--feedback-db`, `WA_MLWH_FEEDBACK_PATH`, and
   `.wa-mlwh-server.token`.

Tests 8 and 9 drive the real command end to end, with no fake auth server.
Each sets `XDG_STATE_HOME=<tmp>`, sets `WA_MLWH_SERVER_TOKEN`,
`WA_MLWH_SERVER_CERT`, `WA_MLWH_SERVER_KEY`, and `WA_MLWH_FEEDBACK_PATH` to
`""`, prepares a synced cache with `prepareMLWHServeCacheForTest(t, true)`,
picks a free port by listening on `127.0.0.1:0` and closing, and runs
`NewRootCommand().ExecuteContext(ctx)` in a goroutine with args
`mlwh serve --url 127.0.0.1:<port> --mlwh-cache <cache>` plus the extra args
below. It polls `GET /health` until 200 (5 s limit), then makes the requests
over real HTTP. Finally it cancels `ctx` and the command returns nil within
10 s.

8. Given extra args `--feedback-db <tmp>/fb.sqlite`, when the Architecture
   example is POSTed to `/feedback`, then 201 with body matching
   `{"id":1,"created_at":"<RFC3339 UTC>"}`. `GET /feedback` with
   `Authorization: Bearer <contents of <tmp>/.wa-mlwh-server.token>` returns
   200 with one item whose `id` is 1 and `description` is
   `No endpoint lists sample consent.`. `GET /feedback` without
   `Authorization` returns 401 `unauthorized`. After the command returns,
   `<tmp>/fb.sqlite-wal` does not exist (SQLite removes the WAL when the last
   connection closes, so this proves the store was closed), and reopening
   with `OpenFeedbackStore` lists id 1. If the `-wal` absence check proves
   flaky, drop it and rely on the reopen: `OpenFeedbackStore` succeeds and
   `List` returns exactly id 1. Keep the reopen assertion either way.
9. Given no extra args, when the Architecture example is POSTed to
   `/feedback`, then 503 `feedback_disabled`, and after the command returns
   `<tmp>/.wa-mlwh-server.token` does not exist.

### D2: run-dev.sh wiring

As a developer, I want `make dev` to enable feedback in test mode and on
request elsewhere, so that the admin page is exercisable locally.

**Package:** repository root
**File:** `run-dev.sh`
**Test file:** `cmd/run_dev_test.go`

- Test mode:
  `FEEDBACK_DB_PATH="$(mktemp "$TMP_DIR/mlwh-feedback-test.XXXXXX.sqlite")"`, ephemeral. Cleanup
  removes it and its `-wal` and `-shm` files.
- Dev and prod: `FEEDBACK_DB_PATH="${WA_MLWH_FEEDBACK_PATH:-}"`, not
  ephemeral. Create the parent directory when set.
- Only the auto-managed `mlwh serve` branch appends
  `--feedback-db "$FEEDBACK_DB_PATH"`, and only when it is non-empty. The
  remote and `WA_RUN_DEV_SEQMETA_CMD` branches are unchanged.
- In dev mode that branch reuses an MLWH server already healthy on the port
  and starts nothing, so `--feedback-db` is not applied. Feedback then
  depends on how that server was started. Leave this behaviour unchanged.

**Acceptance tests:**

1. Given `--mode test` with the fake toolchain, then the recorded
   `mlwh serve` invocation contains `--feedback-db <path>`, with `<path>`
   under `<repo>/.tmp/` and containing `mlwh-feedback-test.`. After SIGINT
   the file is gone.
2. Given `--mode dev` with a configured cache and no
   `WA_MLWH_FEEDBACK_PATH`, then the invocation has no `--feedback-db`.
3. Given `--mode dev` with `WA_MLWH_FEEDBACK_PATH=<tmp>/fb/dev.sqlite`, then
   the invocation contains `--feedback-db <tmp>/fb/dev.sqlite`, `<tmp>/fb`
   exists, and the path survives shutdown.
4. Given `WA_RUN_DEV_SEQMETA_CMD` set, then no `--feedback-db` is passed.

## E. Frontend Admin Page

### E1: Contracts and MLWH client options

As the frontend, I want validated feedback payloads, so that contract drift
fails fast.

**File:** `frontend/lib/contracts.ts`, `frontend/lib/backend-client.ts`
**Test file:** `frontend/tests/contracts.test.ts`,
`frontend/tests/backend-client.test.ts`

```ts
export const feedbackCategorySchema = z.enum([
    "could_not_answer",
    "agent_mistake",
    "no_endpoint",
    "user_unhappy",
    "other",
]);
export type FeedbackCategory = z.infer<typeof feedbackCategorySchema>;
export const feedbackReportSchema = z.object({
    id: z.number().int().positive(),
    created_at: z.string(),
    category: feedbackCategorySchema,
    description: z.string(),
    user_request: z.string(),
    tools_tried: z.array(z.string()),
    mcp_server_version: z.string(),
    wa_api_version: z.string(),
    transport: z.string(),
    client_name: z.string(),
    client_version: z.string(),
    client_user_agent: z.string(),
    remote_addr: z.string(),
    acknowledged: z.boolean(),
    acknowledged_at: z.string(),
});
export type FeedbackReport = z.infer<typeof feedbackReportSchema>;
export const feedbackPageSchema = z.object({
    items: z.array(feedbackReportSchema),
    total: z.number().int().nonnegative(),
    next_offset: z.number().int(),
});
export type FeedbackPage = z.infer<typeof feedbackPageSchema>;
export const feedbackDeleteResponseSchema = z.literal("");

// Server Action inputs. Server Actions are public POST endpoints, so
// arguments are untrusted at runtime whatever their TS types say.
export const feedbackIdSchema = z
    .number()
    .int()
    .positive()
    .max(Number.MAX_SAFE_INTEGER);
export const feedbackListInputSchema = z.object({
    show: z.enum(["unacknowledged", "all"]),
    category: feedbackCategorySchema.nullable(),
    offset: z.number().int().nonnegative().max(Number.MAX_SAFE_INTEGER),
});
export type FeedbackListInput = z.infer<typeof feedbackListInputSchema>;
```

`mlwhJson(path, schema, options?: BackendFetchOptions)` passes `options` to
`buildFetchInit` (no HTTPS agent), as `resultsJson` does.

**Acceptance tests:**

1. Given a full report object with all 15 fields, then
   `feedbackReportSchema.parse` succeeds.
2. Given the same object with `category: "bogus"`, or with `tools_tried`
   missing, then `safeParse` fails.
3. Given `{"items":[],"total":0,"next_offset":-1}`, then
   `feedbackPageSchema` parses it.
4. Given
   `mlwhJson("/feedback", schema, {method: "PATCH", headers: {authorization: "Bearer T"}, body: "{}"})`, then `fetch` is called with
   `<WA_MLWH_BACKEND_URL>/feedback`, method `PATCH`, and header
   `authorization: Bearer T`.
5. Given existing two-argument `mlwhJson` calls, then the existing tests pass
   unchanged.
6. Given `feedbackIdSchema`, then `1` and `Number.MAX_SAFE_INTEGER` parse,
   and `"../x"`, `"3"`, `0`, `-1`, `1.5`, `NaN`, `Infinity`, and
   `Number.MAX_SAFE_INTEGER + 1` fail.
7. Given `feedbackListInputSchema`, then
   `{show:"all",category:null,offset:0}` parses, and each of `show:"x"`,
   `category:"bogus"`, `category:"../x"`, `offset:-1`, `offset:1.5`,
   `offset:"0"`, a missing key, and `null` fails.

### E2: Token and allowlist helpers

As the Next.js server, I want to find the MLWH server token and decide who
is an admin, so that only allowlisted users reach admin data.

**File:** `frontend/lib/feedback-admin.ts`
**Test file:** `frontend/tests/feedback-admin.test.ts`

```ts
export const defaultMLWHServerTokenBasename = ".wa-mlwh-server.token";
export const feedbackPageSize = 50;
export function mlwhServerTokenPath(env?: NodeJS.ProcessEnv): string;
export function readMLWHServerToken(env?: NodeJS.ProcessEnv): string | null;
export function feedbackAdmins(
    env?: NodeJS.ProcessEnv,
    osUsername?: string,
): Set<string>;
export function isFeedbackAdmin(
    username: string | null,
    env?: NodeJS.ProcessEnv,
    osUsername?: string,
): boolean;
```

- `env` defaults to `process.env`. `osUsername` defaults to
  `userInfo().username`, called only when `osUsername` is undefined. Import
  `homedir` and `userInfo` as named imports from `node:os` so tests can mock
  them.
- `userInfo()` throws when the uid has no passwd entry (e.g. some
  containers). The layout (E5) calls `isFeedbackAdmin` on every results
  page, so catch that error: there is then no OS-user default, and
  `WA_FEEDBACK_ADMINS` still applies. Neither `feedbackAdmins` nor
  `isFeedbackAdmin` throws.
- Token path mirrors gas `tokenStoragePath`: trimmed `WA_MLWH_SERVER_TOKEN`.
  Absolute -> as-is. Any relative value, with or without a separator ->
  joined under `XDG_STATE_HOME`, falling back to `homedir()`. Empty -> the
  default basename under that dir. `wa mlwh serve` accepts only a basename
  or absolute path (`validateResultsServeServerToken`), so a value with a
  separator never reaches a running server. Next.js still resolves it the
  gas way for consistency.
- `readMLWHServerToken` returns the trimmed contents. It returns `null` when
  the file is absent, unreadable, or empty.
- Admins: `WA_FEEDBACK_ADMINS` split on `,`, entries trimmed, empties
  dropped, plus `osUsername`. Matching is exact and case-sensitive.
  `isFeedbackAdmin(null)` is false.

**Acceptance tests:**

1. Given `{XDG_STATE_HOME: "/s"}`, then the path is
   `/s/.wa-mlwh-server.token`. Given also `WA_MLWH_SERVER_TOKEN: ".t"`, then
   `/s/.t`. Given `WA_MLWH_SERVER_TOKEN: "/abs/tok"`, then `/abs/tok`. Given
   `WA_MLWH_SERVER_TOKEN: "a/b"`, then `/s/a/b`.
2. Given no `XDG_STATE_HOME`, then the path is under `os.homedir()`.
3. Given a temp file containing `"tok\n"`, then `readMLWHServerToken` returns
   `"tok"`. Given a missing file or an empty file, then `null`.
4. Given `WA_FEEDBACK_ADMINS: " alice, ,bob "` and `osUsername` `"svc"`, then
   the admins are `{alice, bob, svc}`. `isFeedbackAdmin("bob")` is true,
   `isFeedbackAdmin("Bob")` false, and `isFeedbackAdmin(null)` false.
5. Given `WA_FEEDBACK_ADMINS` unset and `osUsername` `"svc"`, then only
   `"svc"` is an admin.
6. Given `node:os` mocked so `userInfo()` throws
   `Error("ENOENT: no such file or directory, uv_os_get_passwd")`, no
   `osUsername` argument, and `WA_FEEDBACK_ADMINS: "alice"`, then
   `feedbackAdmins` returns `{alice}`, `isFeedbackAdmin("alice")` is true,
   and `isFeedbackAdmin("svc")` is false, with no throw. Given
   `WA_FEEDBACK_ADMINS` unset as well, then `feedbackAdmins` is empty.

### E3: Server Actions

As an allowlisted admin, I want list and mutation actions that call wa with
the token server-side, so that the browser never sees it.

**File:** `frontend/app/(results)/feedback/actions.ts` (`"use server"`)
**Test file:** `frontend/tests/feedback-actions.test.ts`

```ts
// FeedbackListInput comes from lib/contracts.ts (E1).
export type FeedbackUnavailableReason =
    | "no_token"
    | "feedback_disabled"
    | "token_rejected"
    | "unsupported"
    | "backend_error";
export type FeedbackListState =
    | { status: "ok"; page: FeedbackPage }
    | { status: "unauthenticated" | "forbidden" | "invalid_input" }
    | { status: "unavailable"; reason: FeedbackUnavailableReason };
export type FeedbackMutationState =
    | { status: "ok" }
    | {
          status:
              | "unauthenticated"
              | "forbidden"
              | "invalid_input"
              | "not_found";
      }
    | { status: "unavailable"; reason: FeedbackUnavailableReason };

export async function listFeedbackAction(
    input: FeedbackListInput,
): Promise<FeedbackListState>;
export async function setFeedbackAcknowledgedAction(
    id: number,
    acknowledged: boolean,
): Promise<FeedbackMutationState>;
export async function deleteFeedbackAction(
    id: number,
): Promise<FeedbackMutationState>;
```

Every action checks, in order:

1. `currentSession()` not authenticated -> `unauthenticated`.
2. `!isFeedbackAdmin(username)` -> `forbidden`.
3. Arguments fail `safeParse` -> `invalid_input`: `input` against
   `feedbackListInputSchema`, `id` against `feedbackIdSchema`, and
   `acknowledged` against `z.boolean()`. Only parsed values build the URL
   and body.
4. `readMLWHServerToken()` null -> `unavailable/no_token`.

It then calls `mlwhJson` with header `authorization: Bearer <token>` and
`cache: "no-store"`. No step before this calls `fetch`.

- List path: `/feedback?limit=50&offset=<offset>`, plus
  `&acknowledged=false` when `show` is `unacknowledged`, plus
  `&category=<c>` when set.
- PATCH `/feedback/<id>` with JSON body `{"acknowledged":<bool>}` and
  `content-type: application/json`, validated by `feedbackReportSchema`.
- DELETE `/feedback/<id>`, validated by `feedbackDeleteResponseSchema`.
- Errors (`BackendRequestError` status and body):
    - 503 with body code `feedback_disabled` -> `feedback_disabled`.
    - 401 -> `token_rejected`.
    - 404 on list, any body -> `unsupported`. A wa older than API 1.9.0 has
      no `GET /feedback`, and gin answers with text `404 page not found`;
      a 1.9.0+ wa never sends 404 on list.
    - 404 on mutations whose body is the wa envelope with code `not_found`
      (B4, B5) -> `not_found`. Any other 404 body -> `unsupported`.
    - `BackendUnavailableError` or anything else -> `backend_error`.

**Acceptance tests:**

1. Given no session, then `listFeedbackAction` returns `unauthenticated` and
   `fetch` is not called.
2. Given session `bob` not in the allowlist, then `forbidden` and no fetch.
3. Given admin `alice` and no token file, then
   `{status:"unavailable",reason:"no_token"}`.
4. Given admin and token `T`, when listing
   `{show:"unacknowledged",category:"no_endpoint",offset:50}`, then fetch
   URL is
   `<base>/feedback?limit=50&offset=50&acknowledged=false&category=no_endpoint`
   with `authorization: Bearer T`, and the result is `ok` with the parsed
   page.
5. Given `{show:"all",category:null,offset:0}`, then the URL is
   `<base>/feedback?limit=50&offset=0`.
6. Given wa returns 503 `feedback_disabled`, then
   `unavailable/feedback_disabled`. Given 401, then
   `unavailable/token_rejected`.
7. Given `setFeedbackAcknowledgedAction(3, true)`, then fetch is PATCH
   `<base>/feedback/3` with body `{"acknowledged":true}`, and the result is
   `ok`. Given wa 404 with JSON body
   `{"code":"not_found","message":"x"}`, then `not_found`.
8. Given `deleteFeedbackAction(3)` and wa 204, then DELETE `<base>/feedback/3`
   and `ok`.
9. Given a non-admin, then both mutations return `forbidden` with no fetch.
10. Given no session, then `setFeedbackAcknowledgedAction(3, true)` returns
    `unauthenticated` and `fetch` is not called.
11. Given admin and token `T`, when `deleteFeedbackAction(3)` gets wa 503
    `{"code":"feedback_disabled","message":"feedback is disabled on this server"}`, then `{status:"unavailable",reason:"feedback_disabled"}`.
12. Given admin and token `T`, when `fetch` rejects (`backendJson` turns
    this into `BackendRequestError(503, null)`, not
    `BackendUnavailableError`), then `listFeedbackAction` returns
    `{status:"unavailable",reason:"backend_error"}`. Given wa returns 500,
    then the same.
13. Given admin, token `T`, and `WA_MLWH_BACKEND_URL` unset (`mlwhJson`
    throws `BackendUnavailableError`), then `listFeedbackAction` returns
    `{status:"unavailable",reason:"backend_error"}` and `fetch` is not
    called.
14. Given admin and token `T`, when `listFeedbackAction` gets wa 404 with
    `content-type: text/plain` body `404 page not found`, then
    `{status:"unavailable",reason:"unsupported"}`. When
    `setFeedbackAcknowledgedAction(3, true)` or `deleteFeedbackAction(3)`
    gets that same 404, then also `unavailable/unsupported`, not
    `not_found`.
15. Given admin and token `T`, when `setFeedbackAcknowledgedAction` is
    called with id `"../x"`, `-1`, `0`, `1.5`, or
    `Number.MAX_SAFE_INTEGER + 1` (cast past TS), or with acknowledged
    `"yes"`, then each returns `{status:"invalid_input"}` and `fetch` is not
    called. The same ids passed to `deleteFeedbackAction` give the same.
16. Given admin and token `T`, when `listFeedbackAction` is called with
    `category:"bogus"`, `category:"../x"`, `show:"x"`, `offset:-1`,
    `offset:1.5`, or `null` as the whole input, then each returns
    `{status:"invalid_input"}` and `fetch` is not called.
17. Given no session and id `"../x"`, then `deleteFeedbackAction` returns
    `unauthenticated` (auth checks run before input validation).
18. Given admin and token file contents `tok-7f3a9c`, when each action runs
    against wa responses 200/204 (ok), 401, 404 text, 404 `not_found`,
    500, 503 `feedback_disabled`, and a rejecting `fetch`, then for every
    returned state `JSON.stringify(state)` does not contain `tok-7f3a9c`.

### E4: Feedback page and view

As an allowlisted admin, I want a `/feedback` page showing unacknowledged
reports newest first, with filters and actions, so that I can triage
quickly.

**File:** `frontend/app/(results)/feedback/page.tsx`,
`frontend/components/feedback-admin-view.tsx`
**Test file:** `frontend/tests/feedback-page.test.ts`

- The page (Server Component, in the `(results)` group so it reuses the
  layout) takes
  `{ searchParams?: Promise<Record<string, string | string[] | undefined>> }`, as `app/(results)/page.tsx` does, and reads
  `(await searchParams) ?? {}`:
    - `show=all` -> `all`, anything else -> `unacknowledged`.
    - `category` -> the value if it is in the enum, else null.
    - `offset` -> a non-negative integer, else 0.
- It calls `listFeedbackAction` and renders by status:
    - `unauthenticated`: "Log in to view feedback."
    - `forbidden`: "You do not have access to feedback."
    - `invalid_input`: "Invalid feedback request." The page sanitises its
      params, so this only guards the type.
    - `unavailable`: "Feedback is unavailable." plus a reason line.
      `no_token`: "The MLWH server token is not readable by this server."
      `feedback_disabled`: "Feedback collection is disabled on the MLWH
      server." `token_rejected`: "The MLWH server rejected the admin token."
      `unsupported`: "wa mlwh serve is too old for feedback (needs MLWH API
      1.9.0)." `backend_error`: "The MLWH server could not be reached."
    - `ok`:
      `<FeedbackAdminView page={...} show={...} category={...} offset={...} />`.
- `FeedbackAdminView` (`'use client'`):
    - Heading "Agent feedback" and "`<total>` reports".
    - A checkbox "Show acknowledged" and a native `<select>` labelled
      "Category" (no shadcn Select exists here). Options: "All categories"
      (value `""`) plus the five labels "Could not answer", "Agent mistake",
      "No endpoint", "User unhappy", "Other" with the enum values. Changing
      either calls `router.push("/feedback?" + params)`, params in order
      `show`, `category`, built from the props with `offset` removed. Omit
      `show` when unacknowledged and `category` when null. With no params,
      push `/feedback`, not `/feedback?`.
    - One `article` per report showing:
        - category label badge, `#<id>`, and `LocalTimestamp` of `created_at`
        - `description` (`whitespace-pre-wrap`)
        - `user_request` under "User request" when non-empty
        - `tools_tried` joined by ", " under "Tools tried" when non-empty
        - a metadata line with client name/version or user agent, MCP server
          version, wa API version, transport, and `remote_addr`
        - "Acknowledged" badge when acknowledged
    - Button "Acknowledge" or "Unacknowledge" calls
      `setFeedbackAcknowledgedAction`, then `router.refresh()`.
    - Button "Delete" calls
      `window.confirm("Delete feedback #<id>? This cannot be undone.")`. Only on true does it call `deleteFeedbackAction`,
      then `router.refresh()`.
    - A non-`ok` mutation result shows a Sonner error toast.
    - "Previous" link when `offset > 0`, omitted when `offset` is 0. "Next"
      link when `next_offset !== -1`. Both hrefs are `/feedback?` plus params
      in order `show`, `category`, `offset`, with `show` and `category` kept
      from the props under the omission rules above. `offset` is
      `max(0, offset - 50)` for Previous and `next_offset` for Next, omitted
      when 0. With no params the href is `/feedback`.
    - Empty list: "No feedback to show."
    - shadcn `Button`/`Badge`, semantic tokens, mobile-first layout.

**Acceptance tests:**

1. Given `searchParams` `Promise.resolve({})` and an admin, then
   `listFeedbackAction` is called with
   `{show:"unacknowledged",category:null,offset:0}`. Given
   `Promise.resolve({show:"all",category:"bogus",offset:"-3"})`, then it is
   called with
   `{show:"all",category:null,offset:0}`.
2. Given each non-`ok` state (`unauthenticated`, `forbidden`,
   `invalid_input`, and `unavailable` with each
   `FeedbackUnavailableReason`: `no_token`, `feedback_disabled`,
   `token_rejected`, `unsupported`, `backend_error`), then the page
   markup contains the exact message(s) listed above for that state.
3. Given a page of 2 reports (one acknowledged), then the view renders 2
   `article` elements, the first with "Unacknowledge" and an "Acknowledged"
   badge, the second with "Acknowledge", plus "2 reports".
4. Given a report with empty `user_request` and `tools_tried`, then neither
   "User request" nor "Tools tried" appears.
5. Given a click on "Acknowledge" for id 2, then
   `setFeedbackAcknowledgedAction(2, true)` is called, then
   `router.refresh`.
6. Given `window.confirm` returning false, when "Delete" is clicked, then
   `deleteFeedbackAction` is not called. Returning true, it is called with
   the id and `router.refresh` follows.
7. Given props `show` `unacknowledged` and category `other`, when "Show
   acknowledged" is ticked, then `router.push` receives
   `/feedback?show=all&category=other`.
8. Given `offset` 0 and `next_offset` 50, then "Next" links to
   `/feedback?offset=50` (keeping other params) and "Previous" is absent.
9. Given an empty page, then "No feedback to show." appears.
10. Given props `show` `all`, category null, and offset 50, when "No
    endpoint" is selected in "Category", then `router.push` receives
    `/feedback?show=all&category=no_endpoint`. Given props `show` `all`,
    category `no_endpoint`, and offset 50, when "All categories" is
    selected, then `router.push` receives `/feedback?show=all`.
11. Given `setFeedbackAcknowledgedAction` mocked to resolve
    `{status:"not_found"}` and `sonner` mocked, when "Acknowledge" is
    clicked, then `toast.error` is called once.
12. Given `show` `all`, category `other`, offset 120, and `next_offset` -1,
    then "Previous" links to `/feedback?show=all&category=other&offset=70`
    and "Next" is absent. Given offset 30, then "Previous" links to
    `/feedback?show=all&category=other`. Given `show` `unacknowledged`,
    category null, and offset 50, then "Previous" links to `/feedback`.
13. Given props `show` `all`, category null, and offset 50, when "Show
    acknowledged" is unticked, then `router.push` receives exactly
    `/feedback`.

### E5: Auth menu link

As an allowlisted admin, I want a Feedback link in the account menu, so that
I can find the page. Other users never see it.

**File:** `frontend/components/auth-menu.tsx`,
`frontend/app/(results)/layout.tsx`
**Test file:** `frontend/tests/auth-menu.test.ts` (it already mocks
`currentSession` for layout tests)

`AuthMenu` gains prop `showFeedbackLink?: boolean` (default false). The
layout passes `session.authenticated && isFeedbackAdmin(session.username)`.
When true and authenticated, the dropdown shows a `menuitem` link
"Feedback" to `/feedback` above "Log out".

The prop is computed server-side, so it only appears after login if the
layout re-renders. It does: `AuthMenu.handleLoginSubmit` already calls
`router.refresh()` after a successful `loginAction`, and logout reloads the
document. No login change is needed. No existing test asserts the refresh,
so test 7 pins it.

**Acceptance tests:**

1. Given an authenticated session and `showFeedbackLink`, when the menu is
   opened, then a link named "Feedback" with `href="/feedback"` is present.
2. Given an authenticated session without the prop, then no "Feedback" link.
3. Given an anonymous session with `showFeedbackLink`, then no "Feedback"
   link.

Tests 4-6 call `await ResultsLayout({children})` with `currentSession`
mocked, `node:os` mocked so `userInfo()` returns username `svc`, and `fetch`
stubbed to return the same session. They render the result with `render()`
inside `AppProviders`, open the account menu, and look for `menuitem`
"Feedback".

4. Given authenticated `bob` and `WA_FEEDBACK_ADMINS` unset, then no
   "Feedback" link.
5. Given authenticated `svc` and `WA_FEEDBACK_ADMINS` unset, then the
   "Feedback" link is present.
6. Given authenticated `alice` and `WA_FEEDBACK_ADMINS=alice`, then the
   "Feedback" link is present.
7. Given an anonymous `AuthMenu`, `loginAction` resolving
   `{authenticated:true,username:"alice"}`, and `fetch` stubbed so
   `/api/auth/refresh` returns 200 `{authenticated:true,username:"alice"}`,
   when the login form is submitted, then `fetch` is called with
   `/api/auth/refresh` (await it with `waitFor`) and `router.refresh` is
   called exactly once. The stub matters: the login makes the session
   authenticated, which runs AuthMenu's session-refresh effect, and that
   effect calls `router.refresh()` again if the browser refresh yields an
   anonymous session. Given `loginAction` rejecting, then `router.refresh`
   is not called.
8. Given `node:os` mocked so `userInfo()` throws, authenticated `alice`,
   and `WA_FEEDBACK_ADMINS=alice`, set up as tests 4-6, then
   `ResultsLayout` resolves without throwing and the "Feedback" link is
   present. Given authenticated `bob` instead, then the layout still
   renders and there is no "Feedback" link.

## F. Docs and Release

### F1: Operator documentation

As an operator, I want feedback setup documented, so that I can enable and
read it.

**File:** `README.md`, `.docs/mcp/security-posture.md`, `.env.development`,
`.env.production`, `frontend/.env.example`
**Test file:** `mlwh/docs_test.go`, `frontend/tests/scaffold.test.ts`

- README `mlwh serve` section covers:
    - `--feedback-db` / `WA_MLWH_FEEDBACK_PATH`
    - the token file location in both modes
    - `/feedback` page requirements: same OS user and token dir, LDAP login,
      `WA_FEEDBACK_ADMINS`
    - the known limits above, including that `WA_MLWH_SERVER_TOKEN` is for
      secured mode only (same value for both processes, unset in plain mode)
      and that a private CA needs `NODE_EXTRA_CA_CERTS` for Next.js
    - that `make dev` in dev mode reuses an MLWH server already running on
      its port without applying `--feedback-db` (D2)
- Security posture gains a short section. `POST /feedback` is the only write
  endpoint and is unauthenticated in plain mode. Admin routes need the
  starter-user token. The stored remote address is the client host.
- Security posture line 26 ("The server is a read-only, internal backend
  ...") is reworded, not just supplemented: the server serves MLWH data
  read-only, and its only write is the optional `POST /feedback`, which
  writes to the separate feedback DB. This matches the OpenAPI
  `info.description` rewording (B7).
- Commented `WA_MLWH_FEEDBACK_PATH=` in both root env files.
  `WA_FEEDBACK_ADMINS=` and `WA_MLWH_SERVER_TOKEN=` in
  `frontend/.env.example`, the latter commented as secured mode only.

**Acceptance tests:**

1. Given `README.md`, then it contains `--feedback-db`,
   `WA_MLWH_FEEDBACK_PATH`, `.wa-mlwh-server.token`, `WA_FEEDBACK_ADMINS`,
   and `NODE_EXTRA_CA_CERTS`.
2. Given `frontend/.env.example`, then it contains `WA_FEEDBACK_ADMINS=`.
3. Given `.docs/mcp/security-posture.md`, then it contains `POST /feedback`
   and does not contain `The server is a read-only, internal backend`.

### F2: Release v0.10.0

As the MCP maintainer, I want a tagged wa release, so that the MCP repo can
depend on `SubmitFeedback`.

After all stories pass, merge and tag `v0.10.0`. The MCP repo then bumps
`github.com/wtsi-hgi/wa` to `v0.10.0`. This is a release step with no tests.

## Implementation Order

1. **Model and store (A1, A2).** Sequential foundation.
2. **Endpoints (B1-B6) and errors.** Sequential after 1. B2-B5 share a file;
   do them in order.
3. **OpenAPI and version (B7)** and **RemoteClient (C1)**. Parallel after 2.
4. **Serve wiring (D1)**, then **run-dev (D2)**. Sequential after 2.
5. **Frontend (E1-E5).** E1 and E2 in parallel, then E3, then E4 and E5 in
   parallel. Independent of 3-4 except for the contract.
6. **Docs (F1).** After 4 and 5.
7. **Verification.** `golangci-lint run --fix`,
   `CGO_ENABLED=1 go test -tags netgo --count 1 ./...`, and
   `cd frontend && pnpm lint && pnpm test`. Then release (F2).

## Appendix: Key Decisions

- **Plain routes, not Registry.** This keeps feedback out of
  `mlwh_call_endpoint` and leaves the Registry/Queryer/OpenAPI coverage tests
  untouched. `POST /feedback` is documented by hand, so only the documented
  error-code test grows (B7).
- **Submit follows the Registry posture. Admin routes use their own token.**
  In secured mode, submit needs a gas JWT like any read. Admin routes need
  the starter-user token in both modes, with no JWT step, so plain mode and
  a future CLI work.
- **Always register routes.** A disabled server answers 503
  `feedback_disabled` rather than 404, so the MCP side can tell "disabled"
  from "no feedback route at this URL" (404).
- **Status-based client sentinels, except 400.** 401/404 can come from gas
  or gin without the wa envelope, and any 413 means too large, so
  `SubmitFeedback` maps those by status and keeps the decoded message. A 400
  needs a `bad_request` envelope: Go's TLS listener sends a plain-text 400
  to `http://` clients, and telling the agent to fix its input would be
  wrong.
- **Byte caps.** They are deterministic in Go and bound storage. All cap
  violations are 413. Semantic errors are 400.
- **Remote address from the socket.** Forwarded headers are spoofable and gin
  trusts all proxies by default.
- **Frontend Zod contract mirrors Go JSON exactly.** Every field is always
  present, so the schemas need no optionals.
- **Testing.** GoConvey for Go, with hermetic `t.TempDir()` SQLite and
  `httptest`, plus D1 tests 8-9 against a real listener. Secured mode is
  deliberately left out of those end-to-end tests because it would need
  certs; D1 test 4 covers its token resolution. Vitest for
  frontend, with `fetch`, `next/headers`, `node:os`, and `currentSession`
  mocked. Follow **go-implementor** / **go-reviewer** and
  **nextjs-fastapi-implementor** / **nextjs-fastapi-reviewer**. The Next.js
  app has no FastAPI backend, so the backend parts of those conventions do
  not apply; wa Go is the backend.
