# MLWH Usage Metrics Specification

## Overview

`wa mlwh serve` records one row per counted request in its own SQLite
database: caller category (CLI, MCP, web, results server, mlwhdiff server,
other API), route template, caller-declared IDs, socket IP, status, and
latency. A token-protected admin endpoint, `GET /usage`, computes aggregates
at query time. A Next.js `/usage` page shows them to allowlisted admins: totals
per category, unique sessions and users, top endpoints, MCP tools and agents,
unmatched paths, per-day counts, and a series of consecutive windows back to
the earliest record, so an admin can see that usage rose, fell, or stopped.

Today the server cannot tell callers apart, so callers identify themselves
through optional request headers (wire contract below). `mlwh.RemoteClient`
gains a configurable User-Agent and per-request identity headers. The CLI,
the web UI, and wa's own servers send them. The companion MCP spec
(`/home/ubuntu/llm-knowledge-base/.docs/usage/spec.md`, written separately)
consumes this contract through `RemoteConfig` and `WithClientIdentity`.

Usage tracking is off unless a usage DB path is configured. A usage write
never fails or noticeably slows the request being counted. Release: wa
`v0.11.0`, `APIVersion` `1.9.0` -> `1.10.0`.

## Architecture

### Files

```text
mlwh/usage.go                  categories, windows, caps, User-Agent parsing
mlwh/usage_store.go            SQLite store, async writer, retention
mlwh/usage_summary.go          UsageSummary types and Summary queries
mlwh/usage_server.go           counting middleware, GET /usage
mlwh/client_identity.go        identity headers, ClientIdentity, context
mlwh/remote.go                 RemoteConfig fields; headers in do()
mlwh/remote_feedback.go        headers in SubmitFeedback
mlwh/errors_http.go            usage_disabled code
mlwh/server.go                 WithUsage; RegisterRoutes wiring
mlwh/openapi.go                header parameters; APIVersion
cmd/mlwh.go                    --usage-db, --usage-retention-days, token
cmd/mlwh_identity.go           CLI and server RemoteConfig builders
cmd/mlwh_info.go, mlwh_latest.go, mlwh_search.go, mlwh_runs.go,
cmd/mlwh_export.go, mlwh_studies.go, mlwhdiff.go, results.go  call sites
run-dev.sh                     usage DB wiring; wa-run-dev User-Agent
frontend/proxy.ts                           anonymous usage cookie
frontend/lib/usage-identity.ts              web identity headers
frontend/lib/backend-client.ts              mlwhJson User-Agent
frontend/app/(results)/actions.ts           send identity headers
frontend/app/(results)/feedback/actions.ts  send identity headers
frontend/lib/contracts.ts                   Zod usage schemas
frontend/app/(results)/usage/actions.ts     Server Action
frontend/app/(results)/usage/page.tsx       admin page
frontend/components/usage-admin-view.tsx    view
frontend/lib/usage-messages.ts              reason messages
frontend/components/auth-menu.tsx           Usage link
frontend/app/(results)/layout.tsx           link visibility
```

Go tests sit beside each file (`mlwh/usage_test.go`,
`mlwh/usage_store_test.go`, `mlwh/usage_summary_test.go`,
`mlwh/usage_server_test.go`, `mlwh/client_identity_test.go`,
`mlwh/openapi_test.go`, `cmd/mlwh_test.go`, `cmd/mlwh_identity_test.go`,
`cmd/run_dev_test.go`). Frontend tests go in `frontend/tests/*.test.ts`.

### Wire contract: identification headers

Every header is optional. Servers older than API 1.10.0 ignore them.

| Header               | Sent by             | Value                                                                    | Cap (bytes) |
| -------------------- | ------------------- | ------------------------------------------------------------------------ | ----------- |
| `User-Agent`         | all wa clients      | `<product>/<version>` (products below)                                   | 512         |
| `X-WA-Client-ID`     | CLI, web, MCP       | CLI: per-process run ID. Web: hashed browser cookie. MCP: hashed session | 128         |
| `X-WA-Client-User`   | CLI, web, MCP stdio | self-declared username                                                   | 128         |
| `X-WA-MCP-Transport` | MCP                 | `stdio` or `http`                                                        | 32          |
| `X-WA-MCP-Agent`     | MCP                 | stdio: clientInfo `<name>/<version>`. HTTP: the agent's User-Agent       | 256         |
| `X-WA-MCP-Tool`      | MCP                 | MCP tool name                                                            | 128         |

User-Agent product token (text before the first `/` or space, exact,
case-sensitive) -> category:

| Product              | Category          | Sender                                                                                  |
| -------------------- | ----------------- | --------------------------------------------------------------------------------------- |
| `wa-cli`             | `cli`             | `wa mlwh info/latest/search/runs/export/studies/programmes/people`, `wa mlwhdiff diff`  |
| `mlwh-mcp-server`    | `mcp`             | companion MCP server                                                                    |
| `wa-web`             | `web`             | Next.js server (`mlwhJson`)                                                             |
| `wa-results-server`  | `results_server`  | `wa results serve`, which also carries `wa results register` traffic                    |
| `wa-mlwhdiff-server` | `mlwhdiff_server` | `wa mlwhdiff serve`                                                                     |
| `wa-run-dev`         | not counted       | `run-dev.sh` readiness `curl`                                                           |
| anything else, none  | `other`           | unidentified callers, including verify-wa `curl` and third-party `RemoteClient` users   |

The version is the text after the first `/` up to the next space, or `""`.
Internal servers send no `X-WA-Client-ID` or `X-WA-Client-User`; they do not
carry the originating end user.

### What is counted

| Request                                                      | Counted | `route`                       | `path`                |
| ------------------------------------------------------------ | ------- | ----------------------------- | --------------------- |
| Registry endpoints (plain router or `AuthRouter()`)          | yes     | Registry `Path`, e.g. `/classify/:id` | `""`          |
| `GET /openapi.json`                                          | yes     | `/openapi.json`               | `""`                  |
| `POST /feedback`                                             | yes     | `/feedback`                   | `""`                  |
| no matching route (gin NoRoute, 404, includes wrong method)  | yes     | `""`                          | URL path, no query    |
| `GET /health`; admin `GET/PATCH/DELETE /feedback`; `GET /usage` | no   | -                             | -                     |
| rejected by gas JWT middleware (401, secured mode)           | no      | -                             | -                     |
| User-Agent product `wa-run-dev`                              | no      | -                             | -                     |

- `route` is the template passed at registration, never the actual path, so
  secured-mode rows carry `/classify/:id`, not `/rest/v1/auth/classify/:id`.
- `path` is `c.Request.URL.Path` truncated to 256 bytes, stored only when
  `route` is `""`.
- A matched route's 404 (e.g. unknown sample) is counted under its template.
- Error means status >= 400.

### Admin endpoint

| Method | Path     | Router       | Auth         | Success            |
| ------ | -------- | ------------ | ------------ | ------------------ |
| GET    | `/usage` | plain router | admin Bearer | 200 `UsageSummary` |

Query: `window` = `24h`, `7d`, `30d`, or `all`; absent or empty means `7d`.

| Status | Code             | Message                                    |
| ------ | ---------------- | ------------------------------------------ |
| 503    | `usage_disabled` | `usage tracking is disabled on this server` |
| 401    | `unauthorized`   | `admin token required`                     |
| 400    | `bad_request`    | `invalid window "<value>"`                 |
| 500    | `internal_error` | `could not summarise usage`                |

Check order: disabled, admin auth, window, store. The route is always
registered and is not in OpenAPI or `Registry`. Add
`httpErrorCodeUsageDisabled = "usage_disabled"` to `mlwh/errors_http.go`; no
sentinel (no Go client calls this route).

### Admin token

The token from `.docs/feedback/spec.md` (secured: `--server-token` file;
plain: `.wa-mlwh-server.token`) is resolved when feedback or usage is
enabled, and the same bytes go to both. Neither enabled: no token file is
created or read.

### Store (`mlwh/usage_store.go`)

DSN via `sqliteWritableDSN(path)`. New files are created with
`createFeedbackStoreFile` (mode 0600; rows hold usernames and IPs).

```sql
CREATE TABLE IF NOT EXISTS usage_requests (
    id             INTEGER PRIMARY KEY,
    ts_ms          INTEGER NOT NULL,
    category       TEXT NOT NULL,
    method         TEXT NOT NULL,
    route          TEXT NOT NULL DEFAULT '',
    path           TEXT NOT NULL DEFAULT '',
    client_id      TEXT NOT NULL DEFAULT '',
    username       TEXT NOT NULL DEFAULT '',
    remote_addr    TEXT NOT NULL DEFAULT '',
    client_version TEXT NOT NULL DEFAULT '',
    user_agent     TEXT NOT NULL DEFAULT '',
    mcp_transport  TEXT NOT NULL DEFAULT '',
    mcp_agent      TEXT NOT NULL DEFAULT '',
    mcp_tool       TEXT NOT NULL DEFAULT '',
    status         INTEGER NOT NULL,
    latency_us     INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS usage_requests_ts ON usage_requests (ts_ms);
CREATE INDEX IF NOT EXISTS usage_requests_route_ts
    ON usage_requests (method, route, ts_ms);
```

`ts_ms` is Unix milliseconds UTC. `remote_addr` uses `feedbackRemoteHost`
(socket host, never `X-Forwarded-For`).

Writes are asynchronous. `Record` does a non-blocking send to a buffered
channel (capacity `usageQueueSize = 4096`). When full it drops the event,
increments an atomic counter, and logs `mlwh usage event dropped` at Warn at
most once per minute. One writer goroutine inserts up to 256 queued events
per transaction. A failed write logs `mlwh usage write failed` at Warn with
the error and drops that batch. The writer also prunes on open and every
hour.

### Summary semantics (`mlwh/usage_summary.go`)

`now` is the store clock at call time. For window length `W` (24h, 7d, 30d),
a row's period index is `k = floor((now_ms - ts_ms) / W_ms)`, with rows where
`ts_ms > now_ms` given `k = 0`. Period `k` covers `(now - (k+1)W, now - kW]`.

- The chosen window is period 0. For `all` it is every row.
- `series` lists periods `k = K-1 ... 0` (oldest first), where
  `K = floor((now_ms - earliest_ms) / W_ms) + 1`, or `[]` when the store is
  empty or the window is `all`. Zero-count periods are included.
- `days` lists every UTC date from the date of `now - W` (for `all`, the
  date of the earliest row) to the date of `now`, zero-filled. `all` on an
  empty store gives `[]`. Each entry counts only rows inside the chosen
  window, so the first entry of a 24h/7d/30d window omits that date's rows
  before `window_start`, and with no row after `now`, `days` requests sum to
  `totals.requests`.
- Sessions: distinct non-empty `client_id`. Clients by IP: distinct
  `remote_addr` among rows with empty `client_id`. Users: distinct non-empty
  `username`.
- Percentiles use nearest rank: sorted ascending, the p-th value is element
  `ceil(p/100 * n)` (1-based). Median is p50. Latency in ms is
  `latency_us / 1000`.
- Requests per session: per category, the p50 and max of per-session request
  counts; 0 when the category has no sessions.
- `error_rate` = errors / requests, 0 when requests is 0.
- Lists sort by requests descending, then by name/value ascending.
  `usageTopLimit = 25` caps `mcp_tools`, `mcp_agents`, `other_user_agents`,
  `unmatched`, `users`, and `clients`. `endpoints` is uncapped.
- Every `by_*` map is non-nil, omits zero counts, and is keyed by category
  value, tool, agent, or `"<METHOD> <route>"`.
- Aggregates are computed in SQL (GROUP BY, ORDER BY with LIMIT/OFFSET for
  ranks); `Summary` never loads every row into Go memory.
- `Summary` first flushes queued events, so counts include every request
  recorded before the call.

### Known limits (documented, not fixed)

- Usernames, client IDs, and User-Agents are self-declared and spoofable.
  Any caller can send `wa-run-dev` to avoid counting.
- CLI and MCP users cannot opt out of sending their username.
- Web counts are requests that reached the MLWH server, not page views. The
  per-browser `wa-seqmeta-cache` cookie cache and the server-wide `/studies`
  cache (`lib/studies-cache.ts`) serve some lookups with no server call.
- The `/studies` cache fetch carries no session or user.
- Clients by IP conflate callers behind one host. All MCP HTTP users share
  the MCP host's IP.
- Secured-mode requests rejected by gas JWT middleware are not counted.
- In `make dev`, a reused MLWH server ignores `--usage-db`, as with
  `--feedback-db`.
- Events dropped on a full queue are not stored; `dropped_events` counts
  them since server start.
- gin's trailing-slash redirect (`RedirectTrailingSlash`, on by default)
  answers e.g. `GET /studies/` with 301 (307 for other methods) before any
  route handler or `NoRoute` runs, so the redirect itself records no row,
  neither under the target route nor as unmatched. Only a client that
  follows it is counted, under the target route.

## A. Usage Model and Store

### A1: Categories, windows, and User-Agent parsing

As the wa server, I want one authoritative usage model, so that the
middleware, store, and page agree on categories and caps.

**Package:** `mlwh/`
**File:** `mlwh/usage.go`
**Test file:** `mlwh/usage_test.go`

```go
type UsageCategory string

const (
    UsageCategoryCLI            UsageCategory = "cli"
    UsageCategoryMCP            UsageCategory = "mcp"
    UsageCategoryWeb            UsageCategory = "web"
    UsageCategoryResultsServer  UsageCategory = "results_server"
    UsageCategoryMLWHDiffServer UsageCategory = "mlwhdiff_server"
    UsageCategoryOther          UsageCategory = "other"
)

// UsageCategories returns the six categories in the order above.
func UsageCategories() []UsageCategory

type UsageWindow string

const (
    UsageWindow24h UsageWindow = "24h"
    UsageWindow7d  UsageWindow = "7d"
    UsageWindow30d UsageWindow = "30d"
    UsageWindowAll UsageWindow = "all"
)

// UsageWindows returns the four windows in the order above.
func UsageWindows() []UsageWindow
// Valid reports whether w is one of the four windows.
func (w UsageWindow) Valid() bool
// Duration returns 24h, 168h, 720h, or 0 for all and unknown values.
func (w UsageWindow) Duration() time.Duration

const (
    UserAgentProductCLI            = "wa-cli"
    UserAgentProductMCP            = "mlwh-mcp-server"
    UserAgentProductWeb            = "wa-web"
    UserAgentProductResultsServer  = "wa-results-server"
    UserAgentProductMLWHDiffServer = "wa-mlwhdiff-server"
    UserAgentProductRunDev         = "wa-run-dev"
)

const (
    UsageMaxUserAgentBytes     = 512
    UsageMaxClientIDBytes      = 128
    UsageMaxUsernameBytes      = 128
    UsageMaxMCPTransportBytes  = 32
    UsageMaxMCPAgentBytes      = 256
    UsageMaxMCPToolBytes       = 128
    UsageMaxPathBytes          = 256
    UsageMaxClientVersionBytes = 64
)

// ParseUsageUserAgent returns the category and version for ua, and
// counted=false for product wa-run-dev. Version is truncated to
// UsageMaxClientVersionBytes.
func ParseUsageUserAgent(ua string) (category UsageCategory, version string, counted bool)

// UsageEvent is one counted request.
type UsageEvent struct {
    Time          time.Time
    Category      UsageCategory
    Method        string
    Route         string // registration template; "" when unmatched
    Path          string // URL path, only when Route == ""
    ClientID      string
    Username      string
    RemoteAddr    string
    ClientVersion string
    UserAgent     string
    MCPTransport  string
    MCPAgent      string
    MCPTool       string
    Status        int
    Latency       time.Duration
}

// truncateUTF8 returns s cut to at most max bytes, backing off to a rune
// boundary so the result stays valid UTF-8 when s was.
func truncateUTF8(s string, max int) string
```

**Acceptance tests:**

1. Given `UsageCategories()`, then it equals
   `[cli, mcp, web, results_server, mlwhdiff_server, other]`.
2. Given `UsageWindows()`, then `[24h, 7d, 30d, all]`, each `Valid()`. Given
   `"1h"`, `""`, and `"ALL"`, then `Valid()` is false and `Duration()` is 0.
   `Duration()` of the four is `24h`, `168h`, `720h`, `0`.
3. Given these User-Agents, then `ParseUsageUserAgent` returns:

| User-Agent                        | category          | version     | counted |
| --------------------------------- | ----------------- | ----------- | ------- |
| `wa-cli/v0.11.0`                  | `cli`             | `v0.11.0`   | true    |
| `mlwh-mcp-server/0.5.0`           | `mcp`             | `0.5.0`     | true    |
| `wa-web`                          | `web`             | `""`        | true    |
| `wa-results-server/v0.11.0`       | `results_server`  | `v0.11.0`   | true    |
| `wa-mlwhdiff-server/v0.11.0 (x)`  | `mlwhdiff_server` | `v0.11.0`   | true    |
| `curl/8.5.0`                      | `other`           | `8.5.0`     | true    |
| `Go-http-client/1.1`              | `other`           | `1.1`       | true    |
| `""`                              | `other`           | `""`        | true    |
| `WA-CLI/1`                        | `other`           | `1`         | true    |
| `wa-cli-extra/1`                  | `other`           | `1`         | true    |
| `wa-run-dev`                      | -                 | -           | false   |
| `wa-run-dev/1`                    | -                 | -           | false   |

4. Given a User-Agent `wa-cli/` plus 100 `x`, then version is 64 bytes.
5. Given `truncateUTF8("abc", 5)`, then `"abc"`. Given 127 `a` followed by
   `"\xc3\xa9"` (U+00E9, 2 bytes) and max 128, then 127 `a`.

### A2: SQLite usage store and async writer

As the server admin, I want requests stored durably without slowing them,
so that metrics survive restarts and callers never wait on SQLite.

**Package:** `mlwh/`
**File:** `mlwh/usage_store.go`
**Test file:** `mlwh/usage_store_test.go`

```go
type UsageStore struct { /* db; now func() time.Time; queue; retention; dropped */ }

// OpenUsageStore opens, creating if needed, the usage DB at path, prunes
// rows older than retentionDays, and starts the writer. retentionDays < 1
// is an error.
func OpenUsageStore(ctx context.Context, path string, retentionDays int) (*UsageStore, error)
// Record queues ev without blocking; it drops ev when the queue is full.
// Calling Record after Close is a no-op.
func (s *UsageStore) Record(ev UsageEvent)
// Flush blocks until every event queued before the call is written or
// dropped by a write failure, or ctx ends. After Close it returns nil at
// once.
func (s *UsageStore) Flush(ctx context.Context) error
// Prune deletes rows with ts_ms older than now - retentionDays*24h and
// returns the count deleted.
func (s *UsageStore) Prune(ctx context.Context) (int64, error)
// Dropped returns events dropped on a full queue since open.
func (s *UsageStore) Dropped() int64
// RetentionDays returns the configured retention.
func (s *UsageStore) RetentionDays() int
// Close flushes queued events, stops the writer, and closes the DB.
func (s *UsageStore) Close() error
```

An unexported
`openUsageStore(ctx, path, retentionDays, queueSize, now func() time.Time)`
backs `OpenUsageStore` with `usageQueueSize` and `time.Now`. `now` is fixed
at construction and drives the open-time prune, the hourly prune, `Prune`,
and `Summary`; nothing reassigns it later. Tests use `openUsageStore` for
small queues and fixed clocks; every test with a fixed `now` passes it here.
Insert truncates each string field to its cap (`Path` to
`UsageMaxPathBytes`) with `truncateUTF8`. Tests read rows back with a
test-file helper that queries `usage_requests`.

**Acceptance tests:**

1. Given a new path `<tmp>/u.sqlite`, when opened, then the file exists with
   mode 0600 and `usage_requests` is empty.
2. Given one full `UsageEvent` at 2026-10-10T12:00:00.123Z with latency
   1500 us, when `Record` then `Flush` run, then one row holds every field,
   `ts_ms == 1791633600123`, and `latency_us == 1500`.
3. Given `ClientID` of 200 bytes and `UserAgent` of 600 bytes, then the row
   holds the first 128 and 512 bytes.
4. Given 3 events recorded and the store closed without `Flush`, when
   reopened, then 3 rows exist (Close flushes).
5. Given `openUsageStore` with retention 30 and a clock fixed at
   2026-10-10T12:00:00Z, with rows at now-31d, now-30d+1s, and now-1h
   written, when `Prune` runs, then it returns 1 and two rows remain. Given
   those three rows written to a file by a store opened with retention 365
   and the same clock, when reopened via `openUsageStore` with retention 30
   and the same clock, then two rows remain (prune on open).
6. Given `retentionDays` 0 or -1, then `OpenUsageStore` returns an error
   containing `retention` and creates no writer.
7. Given `openUsageStore` with queue size 4 and a second connection holding
   `BEGIN EXCLUSIVE` on the file, when `Record` is called 100 times, then the
   100 calls return in under 100 ms in total and `Dropped() >= 50`. After the
   lock is released and `Flush` returns, rows + `Dropped()` == 100. The test
   releases the lock well within the DSN's 5 s `busy_timeout` (e.g. after
   500 ms): a batch that times out is a write failure, not counted in
   `Dropped()`.
8. Given a store whose `db` is closed under it, when 5 events are recorded
   and `Flush` runs, then `Flush` returns nil within 1 s, a Warn record
   `mlwh usage write failed` is logged, and `Record` does not panic.
9. Given a closed store, then `Record` returns without panic and `Close`
   called again returns nil.
10. Given a closed store, then `Flush(context.Background())` returns nil
    within 1 s and `Summary(ctx, UsageWindow7d)` returns a non-nil error.

### A3: Summary

As the server admin, I want aggregates computed for a chosen window, so that
I can see who uses the server and how.

**Package:** `mlwh/`
**File:** `mlwh/usage_summary.go`
**Test file:** `mlwh/usage_summary_test.go`

```go
// Summary flushes queued events, then aggregates window w at s.now().
// An invalid w returns an error wrapping ErrUsageWindowInvalid. After
// Close it returns a non-nil error.
func (s *UsageStore) Summary(ctx context.Context, w UsageWindow) (UsageSummary, error)

var ErrUsageWindowInvalid = errors.New("mlwh: invalid usage window")

type UsageSummary struct {
    Window          UsageWindow          `json:"window"`
    GeneratedAt     string               `json:"generated_at"`    // RFC3339 UTC
    WindowStart     string               `json:"window_start"`    // "" for all
    EarliestRecord  string               `json:"earliest_record"` // "" when empty
    RetentionDays   int                  `json:"retention_days"`
    DroppedEvents   int64                `json:"dropped_events"`
    Totals          UsageTotals          `json:"totals"`
    Categories      []UsageCategoryStats `json:"categories"` // all six, fixed order
    Endpoints       []UsageEndpointStats `json:"endpoints"`
    MCPTools        []UsageCount         `json:"mcp_tools"`
    MCPAgents       []UsageCount         `json:"mcp_agents"`
    OtherUserAgents []UsageCount         `json:"other_user_agents"`
    Unmatched       []UsageUnmatched     `json:"unmatched"`
    Users           []UsageActor         `json:"users"`
    Clients         []UsageActor         `json:"clients"`
    Days            []UsageDay           `json:"days"`
    Series          []UsagePeriod        `json:"series"`
}

type UsageTotals struct {
    Requests  int64   `json:"requests"`
    Errors    int64   `json:"errors"`
    ErrorRate float64 `json:"error_rate"`
    Sessions  int64   `json:"sessions"`
    IPClients int64   `json:"ip_clients"`
    Users     int64   `json:"users"`
}

type UsageCategoryStats struct {
    Category                 UsageCategory `json:"category"`
    Requests                 int64         `json:"requests"`
    Errors                   int64         `json:"errors"`
    ErrorRate                float64       `json:"error_rate"`
    Sessions                 int64         `json:"sessions"`
    IPClients                int64         `json:"ip_clients"`
    Users                    int64         `json:"users"`
    RequestsPerSessionMedian int64         `json:"requests_per_session_median"`
    RequestsPerSessionMax    int64         `json:"requests_per_session_max"`
}

type UsageEndpointStats struct {
    Method     string           `json:"method"`
    Route      string           `json:"route"`
    Requests   int64            `json:"requests"`
    Errors     int64            `json:"errors"`
    ErrorRate  float64          `json:"error_rate"`
    P50Ms      float64          `json:"p50_ms"`
    P95Ms      float64          `json:"p95_ms"`
    ByCategory map[string]int64 `json:"by_category"`
}

type UsageCount struct {
    Value    string `json:"value"`
    Requests int64  `json:"requests"`
}

type UsageUnmatched struct {
    Method     string           `json:"method"`
    Path       string           `json:"path"`
    Requests   int64            `json:"requests"`
    ByCategory map[string]int64 `json:"by_category"`
    ByTool     map[string]int64 `json:"by_tool"`
    ByAgent    map[string]int64 `json:"by_agent"`
}

// UsageActor is a user (Name = username, ByIP false) or a client (Name =
// client ID, or socket IP with ByIP true for rows without a client ID).
type UsageActor struct {
    Name       string           `json:"name"`
    ByIP       bool             `json:"by_ip"`
    Requests   int64            `json:"requests"`
    ByCategory map[string]int64 `json:"by_category"`
}

type UsageDay struct {
    Date       string           `json:"date"` // YYYY-MM-DD, UTC
    Requests   int64            `json:"requests"`
    ByCategory map[string]int64 `json:"by_category"`
}

type UsagePeriod struct {
    Start      string           `json:"start"` // exclusive, RFC3339 UTC
    End        string           `json:"end"`   // inclusive, RFC3339 UTC
    Requests   int64            `json:"requests"`
    ByCategory map[string]int64 `json:"by_category"`
    ByEndpoint map[string]int64 `json:"by_endpoint"` // "<METHOD> <route>"
}
```

Lists are `[]`, never nil, in JSON. `endpoints` and `by_endpoint` cover
matched routes only. `mcp_tools` and `mcp_agents` count rows with a
non-empty `mcp_tool` / `mcp_agent`; likewise `by_tool` / `by_agent` omit
rows with an empty `mcp_tool` / `mcp_agent`. `other_user_agents` counts category
`other` rows by raw `user_agent` (`""` included).

Fixture F (tests 1-6, 13-15): store from `openUsageStore` with the clock
fixed at `now` = 2026-10-10T12:00:00Z, retention 365. Rows
(`-` is empty; default method GET, IP 10.0.0.1):

| #   | ts         | category | route           | path     | client_id | username | IP       | user_agent                 | tool                | agent               | status | latency |
| --- | ---------- | -------- | --------------- | -------- | --------- | -------- | -------- | -------------------------- | ------------------- | ------------------- | ------ | ------- |
| 1   | now-1h     | cli      | `/studies`      | -        | run-a     | alice    | 10.0.0.1 | `wa-cli/v0.11.0`           | -                   | -                   | 200    | 10ms    |
| 2   | now-2h     | cli      | `/studies`      | -        | run-a     | alice    | 10.0.0.1 | `wa-cli/v0.11.0`           | -                   | -                   | 200    | 30ms    |
| 3   | now-3h     | cli      | `/classify/:id` | -        | run-b     | bob      | 10.0.0.1 | `wa-cli/v0.11.0`           | -                   | -                   | 404    | 5ms     |
| 4   | now-4h     | mcp      | `/studies`      | -        | sess-1    | -        | 10.0.0.2 | `mlwh-mcp-server/0.5.0`    | `mlwh_list_studies` | `claude-code/2.1.0` | 200    | 20ms    |
| 5   | now-5h     | other    | `/studies`      | -        | -         | -        | 10.0.0.9 | `curl/8.5.0`               | -                   | -                   | 500    | 40ms    |
| 6   | now-6h     | other    | -               | `/bogus` | -         | -        | 10.0.0.9 | `curl/8.5.0`               | -                   | -                   | 404    | 1ms     |
| 7   | now-30h    | cli      | `/studies`      | -        | run-a     | alice    | 10.0.0.1 | `wa-cli/v0.11.0`           | -                   | -                   | 200    | 10ms    |
| 8   | now-60h    | web      | `/studies`      | -        | web-1     | alice    | 10.0.0.3 | `wa-web`                   | -                   | -                   | 200    | 50ms    |
| 9   | now-228h   | cli      | `/studies`      | -        | run-c     | carol    | 10.0.0.1 | `wa-cli/v0.11.0`           | -                   | -                   | 200    | 10ms    |

Row 7 is 2026-10-09T06:00:00Z, before the 24h `window_start`. Row 9 is
2026-10-01T00:00:00Z.

**Acceptance tests:**

1. Given F and window `24h`, then:
    - `window_start` `2026-10-09T12:00:00Z`, `earliest_record`
      `2026-10-01T00:00:00Z`, `retention_days` 365, `generated_at`
      `2026-10-10T12:00:00Z`.
    - `totals` = requests 6, errors 3, error_rate 0.5, sessions 3,
      ip_clients 1, users 2.
    - `categories` in order, fields as in `UsageCategoryStats`: cli {3, 1,
      1/3 (`ShouldAlmostEqual`), sessions 2, ip 0, users 2, median 1, max
      2}; mcp {1, 0, 0, 1, 0, 0, 1, 1}; web, results_server,
      mlwhdiff_server all zero; other {2, 2, 1.0, 0, 1, 0, 0, 0}.
    - `endpoints` = [`GET /studies` {4, errors 1, 0.25, p50 20, p95 40,
      by_category {cli:2, mcp:1, other:1}}, `GET /classify/:id` {1, 1, 1.0,
      5, 5, {cli:1}}].
    - `mcp_tools` [{mlwh_list_studies, 1}], `mcp_agents`
      [{claude-code/2.1.0, 1}], `other_user_agents` [{curl/8.5.0, 2}].
    - `unmatched` [{GET, /bogus, 1, {other:1}, {}, {}}].
    - `users` [alice 2 {cli:2}, bob 1 {cli:1}].
    - `clients` [{10.0.0.9, by_ip, 2, {other:2}}, {run-a, 2, {cli:2}},
      {run-b, 1}, {sess-1, 1}] (ties by name).
    - `days` [{2026-10-09, 0, {}}, {2026-10-10, 6, {cli:3, mcp:1,
      other:2}}]; row 7 is on 2026-10-09 but outside the window.
2. Given F and `24h`, then `series` has 10 periods. The last is
   `(2026-10-09T12:00:00Z, 2026-10-10T12:00:00Z]` with requests 6,
   by_category {cli:3, mcp:1, other:2}, by_endpoint {"GET /studies":4,
   "GET /classify/:id":1}. Index 8 has requests 1 {cli:1}. Index 7 has
   requests 1 {web:1}. Index 0 starts `2026-09-30T12:00:00Z` with requests
   1 {cli:1}. The other 6 have requests 0 and empty maps.
3. Given F and `7d`, then totals requests 8, sessions 4, users 2; web
   {1, sessions 1, users 1}; `users` [alice 4 {cli:3, web:1}, bob 1];
   `days` has 8 entries from 2026-10-03 to 2026-10-10, with 2026-10-09
   {1, {cli:1}} and requests summing to 8; `series` has 2 periods with
   requests [1, 8].
4. Given F and `all`, then `window_start` `""`, totals requests 9, users 3,
   `series` `[]`, and `days` has 10 entries from 2026-10-01 (requests 1) to
   2026-10-10 (requests 6), with 2026-10-09 requests 1.
5. Given F and `30d`, then totals requests 9 and `series` has one period
   with requests 9.
6. Given F plus row 10 at now+1h (cli, `/studies`), when `24h` is
   summarised, then period 0 and totals include it (requests 7).
7. Given an empty store and `7d`, then totals all 0, `categories` six zero
   rows, every other list `[]` except `days` (8 zero entries), `series`
   `[]`, `earliest_record` `""`. With `all`, `days` is `[]`.
8. Given 30 distinct MCP tools with 1-30 requests, then `mcp_tools` has 25
   entries, the first with 30 requests.
9. Given one endpoint with latencies 1-100 ms, then p50 50 and p95 95.
   Given one row of 7 ms, then p50 7 and p95 7.
10. Given sessions with counts [1, 2, 3, 10] in cli, then median 2, max 10.
11. Given `Record` of 2 events with no `Flush`, when `Summary` runs, then
    totals requests 2.
12. Given window `"1h"`, then `errors.Is(err, ErrUsageWindowInvalid)`.
13. Given F plus an unmatched row at now-30m (category mcp, path `/bogus`,
    tool `mlwh_bogus`, agent `claude-code/2.1.0`), when `24h` is
    summarised, then the `/bogus` entry has requests 2, by_category
    {mcp:1, other:1}, by_tool {mlwh_bogus:1}, and by_agent
    {"claude-code/2.1.0":1}.
14. Given F with 3 drops recorded, then `dropped_events` 3.
15. Given F and `24h`, when JSON-marshalled, then every `by_*` value is an
    object (never `null`) and every list is an array.

## B. HTTP Middleware and Admin Endpoint

### B1: Counting middleware

As the server admin, I want every counted request recorded with its caller
identity, so that metrics reflect real use.

**Package:** `mlwh/`
**File:** `mlwh/usage_server.go`, `mlwh/server.go`
**Test file:** `mlwh/usage_server_test.go`

```go
// WithUsage enables usage tracking. store may be nil (disabled).
// adminToken is the admin Bearer token; ignored when store is nil.
func WithUsage(store *UsageStore, adminToken []byte) ServerOption
```

- `(s *Server) usageMiddleware(route string) gin.HandlerFunc` records start
  time, calls `c.Next()`, then builds a `UsageEvent` from
  `c.Request.Method`, `route`, `c.Writer.Status()`, elapsed time, the
  headers (each truncated to its cap), `ParseUsageUserAgent`, and
  `feedbackRemoteHost(c.Request.RemoteAddr)`, and calls `Record`. Uncounted
  User-Agents record nothing.
- When usage is enabled, `RegisterRoutes` prepends `usageMiddleware(path)`
  to Registry handlers (plain router or auth group), `/openapi.json`, and
  `POST /feedback`, and sets `router.NoRoute(s.usageMiddleware(""))`. With
  route `""`, `Path` = `truncateUTF8(c.Request.URL.Path, UsageMaxPathBytes)`.
  gin then writes its default `404 page not found` body.
- `/health`, feedback admin routes, and `GET /usage` get no middleware. In
  secured mode the gas auth group handlers run first, so a 401 from gin-jwt
  records nothing.
- Usage disabled: no middleware and no `NoRoute` change.

**Acceptance tests:**

Tests use `NewServer(fakeQueryer, WithUsage(store, T))`, a gin engine with
`RegisterRoutes(engine, nil)`, `httptest` requests (remote `192.0.2.1`), and
`store.Flush` before reading rows.

1. Given `GET /studies` with `User-Agent: wa-cli/v0.11.0`,
   `X-WA-Client-ID: run-a`, `X-WA-Client-User: alice`, then 200 and one row:
   category `cli`, method `GET`, route `/studies`, path `""`, client_id
   `run-a`, username `alice`, remote_addr `192.0.2.1`, client_version
   `v0.11.0`, user_agent `wa-cli/v0.11.0`, status 200, latency_us >= 0.
2. Given `GET /classify/ABC?x=1`, then route `/classify/:id` and path `""`.
3. Given `GET /no/such?id=secret`, then 404 with body `404 page not found`
   and one row: route `""`, path `/no/such`, status 404. Given a 300-byte
   unmatched path, then the stored path is 256 bytes.
4. Given `POST /studies`, then one row: method `POST`, route `""`, path
   `/studies`, status 404.
5. Given a fake queryer returning `ErrNotFound` for `GET /classify/X`, then
   the row has route `/classify/:id` and status 404.
6. Given `GET /health`, `GET /usage` with token `T`, and `GET /feedback`
   with token `T` (feedback enabled), then no rows.
7. Given `GET /openapi.json`, then one row with route `/openapi.json`. Given
   feedback enabled and a valid `POST /feedback`, then one row with method
   `POST`, route `/feedback`, status 201.
8. Given `User-Agent: wa-run-dev` or `wa-run-dev/1` on `GET /studies` and
   `GET /bogus`, then no rows. Given `curl/8.5.0`, then category `other`.
9. Given `X-WA-MCP-Transport: stdio`, `X-WA-MCP-Agent: claude-code/2.1.0`,
   `X-WA-MCP-Tool: mlwh_list_studies`, and
   `User-Agent: mlwh-mcp-server/0.5.0`, then category `mcp` and the three
   MCP fields stored.
10. Given `X-Forwarded-For: 203.0.113.9`, then remote_addr is `192.0.2.1`.
11. Given `X-WA-Client-ID` of 200 bytes, then 128 bytes are stored.
12. Given an engine plus group `/rest/v1/auth` passed as `auth`, then
    `GET /rest/v1/auth/studies` stores route `/studies`. Given that group
    with a middleware aborting 401 first, then no row.
13. Given usage disabled (`WithUsage(nil, nil)`), then `GET /bogus` is 404
    `404 page not found` and no store is touched.
14. Given a store whose `db` was closed, then 10 `GET /studies` requests
    each return the same status and body as with usage disabled.

### B2: GET /usage

As the server admin, I want the summary over HTTP behind the admin token,
so that the web page can show it.

**Package:** `mlwh/`
**File:** `mlwh/usage_server.go`
**Test file:** `mlwh/usage_server_test.go`

`registerUsageAdminRoute(router)` registers `GET /usage` with
`requireUsageAdmin` (503 when store nil, else 401 unless
`feedbackAdminTokenMatches`) then the handler. The handler maps
`ErrUsageWindowInvalid` to 400 and any other `Summary` error to 500.

**Acceptance tests:**

1. Given usage disabled, when `GET /usage` with or without a token, then 503
   `{"code":"usage_disabled","message":"usage tracking is disabled on this server"}`.
2. Given token `T`, when no `Authorization`, `Bearer wrong`, or `Basic T`,
   then 401 `{"code":"unauthorized","message":"admin token required"}`.
3. Given token `T` and two recorded `GET /studies` requests, when
   `GET /usage` with `Bearer T`, then 200, `window` `7d`, and
   `totals.requests` 2 (no explicit flush needed).
4. Given `?window=24h`, `?window=30d`, `?window=all`, then `window` echoes
   each. Given `?window=` (empty), then `7d`.
5. Given `?window=1h`, then 400 `bad_request` message
   `invalid window "1h"`.
6. Given a store closed before the request, then 500
   `{"code":"internal_error","message":"could not summarise usage"}`.
7. Given usage enabled with no feedback (`WithFeedback(nil, nil)`), then
   `GET /usage` with `T` is 200 and `GET /feedback` is 503
   `feedback_disabled`.

### B3: Route placement

As an operator, I want usage routes wired consistently, so that the API
stays predictable.

**Package:** `mlwh/`
**File:** `mlwh/server.go`
**Test file:** `mlwh/usage_server_test.go`

**Acceptance tests:**

1. Given `len(Registry)` before this feature, then it is unchanged and no
   entry has path `/usage`.
2. Given `OpenAPIDocument()`, then `paths` has no `/usage`.
3. Given a secured-style engine (group as `auth`), then `GET /usage` is on
   the root router and `GET /rest/v1/auth/usage` is unmatched.

## C. Remote Client and OpenAPI

### C1: RemoteClient identity headers

As a wa client (CLI, server, or MCP), I want to send identification headers
through the shared client, so that the server can categorise my calls.

**Package:** `mlwh/`
**File:** `mlwh/client_identity.go`, `mlwh/remote.go`,
`mlwh/remote_feedback.go`
**Test file:** `mlwh/client_identity_test.go`

```go
const (
    HeaderClientID     = "X-WA-Client-ID"
    HeaderClientUser   = "X-WA-Client-User"
    HeaderMCPTransport = "X-WA-MCP-Transport"
    HeaderMCPAgent     = "X-WA-MCP-Agent"
    HeaderMCPTool      = "X-WA-MCP-Tool"
)

// ClientIdentity is optional caller identification. Empty fields send no
// header.
type ClientIdentity struct {
    ClientID     string
    Username     string
    MCPTransport string
    MCPAgent     string
    MCPTool      string
}

// WithClientIdentity returns ctx carrying id. RemoteClient requests made
// with it send id's non-empty fields, each overriding the same field of
// RemoteConfig.Identity. A nested call replaces the outer identity.
func WithClientIdentity(ctx context.Context, id ClientIdentity) context.Context

// RemoteConfig gains:
//   UserAgent string         // sent on every request; "" keeps Go's default
//   Identity  ClientIdentity // sent on every request
```

- One unexported `(rc *RemoteClient) setIdentityHeaders(req *http.Request)`
  is called from `do` and `SubmitFeedback`.
- Each value is sanitised: every byte outside 0x20-0x7E becomes `?`, then it
  is cut to its cap from A1 (`UsageMaxUserAgentBytes` for User-Agent). A
  value empty after that sends no header. Sanitising keeps `net/http` from
  rejecting the request.

**Acceptance tests:**

1. Given
   `RemoteConfig{UserAgent: "wa-cli/v1", Identity: {ClientID: "run-a", Username: "alice"}}`
   and an `httptest` stub, when `AllStudies` and `SubmitFeedback` are
   called, then both requests carry `User-Agent: wa-cli/v1`,
   `X-WA-Client-ID: run-a`, `X-WA-Client-User: alice`, and no
   `X-WA-MCP-*` header.
2. Given an empty `RemoteConfig` identity and UserAgent, then the
   User-Agent starts with `Go-http-client/` and no `X-WA-*` header is sent.
3. Given config
   `{UserAgent: "mlwh-mcp-server/0.5.0", Identity: {MCPTransport: "stdio"}}`
   and ctx from `WithClientIdentity` with
   `{ClientID: "s1", MCPAgent: "claude-code/2.1.0", MCPTool: "mlwh_list_studies"}`,
   when `Call(ctx, "AllStudies", nil, nil)` runs, then all five headers
   arrive with those values.
4. Given two concurrent `AllStudies` calls on one client with tools `t1` and
   `t2` in their contexts, then each request carries its own tool.
5. Given config Username `alice` and ctx Username `bob`, then
   `X-WA-Client-User: bob`. Given ctx Username `""`, then `alice`.
6. Given ctx Username `"caf\xc3\xa9\n"`, then the header is `caf???` and the
   call succeeds. Given a 200-byte ClientID, then 128 bytes are sent.
7. Given a 600-byte UserAgent, then 512 bytes are sent.
8. Given the real `NewServer(q, WithUsage(store, T))` behind `httptest` and
   a client with UserAgent `wa-cli/v1` and ClientID `run-a`, when
   `AllStudies` runs, then the store holds one row with category `cli` and
   client_id `run-a` (round trip).

### C2: OpenAPI header parameters and API version

As an external implementor, I want the identification headers described in
`/openapi.json`, so that I can identify my client.

**Package:** `mlwh/`
**File:** `mlwh/openapi.go`, `MLWH_API_REFERENCE.md`
**Test file:** `mlwh/openapi_test.go`, `mlwh/docs_test.go`

- `APIVersion = "1.10.0"`; update its doc comment.
- `info.description` becomes exactly:

```text
Cache-backed REST API mirroring Multi-LIMS Warehouse (MLWH) study, sample, run, and library metadata. MLWH data is read-only; the only documented write endpoint is POST /feedback, which stores agent feedback in a separate database. Callers may identify themselves with the optional headers in components.parameters; when usage tracking is enabled the server records them with each request's route, status, and latency in a separate usage database. Unauthenticated by default; the network boundary is the access-control boundary.
```

- `components.parameters` gains six entries, keys `UserAgentHeader`,
  `ClientIDHeader`, `ClientUserHeader`, `MCPTransportHeader`,
  `MCPAgentHeader`, `MCPToolHeader`. Each is
  `{"name": <header>, "in": "header", "required": false, "description": <text>, "schema": {"type": "string", "maxLength": <cap>}}`
  with the header names and caps from the wire contract. The User-Agent
  description lists the product tokens; the `X-WA-Client-User` description
  says the value is self-declared.
- No operation references them, so Registry parameter coverage tests and
  MCP-derived tool schemas are unchanged.
- Update `openAPIB7InfoDescription`,
  `TestAPIVersionIsThePhase1DocumentationPatch`, and
  `TestEndpointReferenceCommittedAtFeedbackVersionB7` (rename to
  `...UsageVersionC2`) to the new values. Regenerate the reference with
  `WA_REFRESH_DOCS=1 go test ./mlwh -run TestWriteEndpointReference`; only
  its version line changes.

**Acceptance tests:**

1. Given `APIVersion`, then `"1.10.0"` and served `info.version` equals it.
2. Given served `info.description`, then it equals the text above.
3. Given `components.parameters`, then its keys are exactly the six names,
   and `ClientIDHeader` is
   `{name: "X-WA-Client-ID", in: "header", required: false, schema: {type: "string", maxLength: 128}}`
   plus a description. `UserAgentHeader.schema.maxLength` is 512.
4. Given every operation in `paths`, then none has a parameter with
   `in: "header"`.
5. Given `components.schemas`, then its key set equals the key set before
   this feature.
6. Given the committed `MLWH_API_REFERENCE.md`, then the no-drift test
   passes and it contains ``**API version:** `1.10.0` ``.

## D. Serve Command, Callers, and Dev Script

### D1: --usage-db flag and admin token

As an operator, I want usage enabled by one setting, so that existing
deployments are unaffected.

**Package:** `cmd/`
**File:** `cmd/mlwh.go`
**Test file:** `cmd/mlwh_test.go`

- Flags: `--usage-db`, default `firstEnv("WA_MLWH_USAGE_PATH")`, help
  `SQLite path for usage metrics; usage tracking is disabled when unset`.
  `--usage-retention-days`, int flag with default 365, help
  `days of usage rows to keep; overrides WA_MLWH_USAGE_RETENTION_DAYS`
  (cobra appends `(default 365)`).
- `Long` gains a paragraph naming `--usage-db`, `WA_MLWH_USAGE_PATH`,
  `--usage-retention-days`, `GET /usage`, and that the admin token is the
  same as feedback's.
- Replace the `Long` sentence "MLWH data is read-only; POST /feedback
  (below) is the only write." with "MLWH data is read-only; the only writes
  are POST /feedback and usage metrics (both below, both optional), each to
  its own SQLite file."
- Rename `mlwhServeFeedbackAdminToken` to `mlwhServeAdminToken`.
- `RunE` after `openMLWHServeFeedback`: `openMLWHServeUsage`, defer closing
  a non-nil store, then `adminToken := feedbackToken`; when the usage store
  is non-nil and `adminToken` is nil,
  `adminToken, err = mlwhServeAdminToken(serveConfig)`. Build
  `mlwh.NewServer(client, mlwh.WithFeedback(feedbackStore, feedbackToken), mlwh.WithUsage(usageStore, adminToken))`.

```go
// openMLWHServeUsage returns (nil, nil) when usageDB is blank. It rejects
// a MySQL-looking DSN, :memory:, and file: URIs with the same messages as
// feedback but naming --usage-db, rejects retentionDays < 1 with
// "--usage-retention-days must be at least 1", creates the parent
// directory, and opens the store.
func openMLWHServeUsage(ctx context.Context, usageDB string, retentionDays int) (*mlwh.UsageStore, error)
```

```go
// resolveMLWHServeUsageRetentionDays returns flagValue when flagChanged.
// Otherwise it returns the trimmed WA_MLWH_USAGE_RETENTION_DAYS parsed with
// strconv.Atoi, or flagValue when that is blank; a parse failure returns
// `invalid WA_MLWH_USAGE_RETENTION_DAYS "<v>"`.
func resolveMLWHServeUsageRetentionDays(flagValue int, flagChanged bool) (int, error)
```

This mirrors `resolveMLWHServeBindPort`: `RunE` calls it with
`cmd.Flags().Changed("usage-retention-days")` before
`openMLWHServeUsage`, whether or not usage is enabled, and returns its
error. An explicit `--usage-retention-days` ignores the env var, even an
invalid one. Range checking stays in `openMLWHServeUsage`.

**Acceptance tests:**

1. Given a blank `usageDB`, then `(nil, nil)` and no file is created.
2. Given `<tmp>/sub/u.sqlite` and retention 365, then `sub/` and the file
   exist, mode 0600, and the store reports `RetentionDays() == 365`.
3. Given `user@tcp(db:3306)/u`, `:memory:`, or `file:x.db`, then an error
   mentioning `--usage-db` and `SQLite file path`.
4. Given retention 0, then an error containing
   `--usage-retention-days must be at least 1`.
5. Given `WA_MLWH_USAGE_PATH=<tmp>/u.sqlite`, then the `--usage-db` default
   is that path. Given `WA_MLWH_USAGE_RETENTION_DAYS` `30`, ` 30 `, `""`,
   and unset, then `resolveMLWHServeUsageRetentionDays(365, false)` returns
   30, 30, 365, 365. Given env `30` and `(90, true)`, then 90. Given env
   `abc`, then `(365, false)` returns an error equal to
   `invalid WA_MLWH_USAGE_RETENTION_DAYS "abc"` and `(90, true)` returns 90
   with nil error.
6. Given the `mlwh serve` command, then `Long` contains `--usage-db`,
   `WA_MLWH_USAGE_PATH`, `--usage-retention-days`, `GET /usage`, and
   `the only writes are POST /feedback and usage metrics`, and does not
   contain `is the only write`. `Long` is `strings.Join(..., "\n")` of
   source lines, so that phrase must sit on one line of the slice.

Tests 7-9 drive the real command as feedback D1 tests 8-9 do (same env
reset plus `WA_MLWH_USAGE_PATH=""`, real listener, poll `/health`, cancel).

7. Given extra args `--usage-db <tmp>/u.sqlite` only, when `GET /studies`
   is sent with `User-Agent: wa-cli/x`, then `GET /usage?window=24h` with
   `Bearer <contents of <tmp>/.wa-mlwh-server.token>` returns 200 with
   `totals.requests` 1 and the `cli` category requests 1; without a token,
   401. After the command returns, reopening the DB shows one row.
8. Given `--usage-db` and `--feedback-db`, then the same token works for
   `GET /usage` and `GET /feedback`.
9. Given no extra args, then `GET /usage` is 503 `usage_disabled` and
   `<tmp>/.wa-mlwh-server.token` does not exist after the command returns.

### D2: CLI and internal-server identities

As the server admin, I want wa's own callers to identify themselves, so that
CLI and internal-server use is categorised.

**Package:** `cmd/`
**File:** `cmd/mlwh_identity.go` and the call sites listed in Files
**Test file:** `cmd/mlwh_identity_test.go`

```go
// waBuildVersion returns debug.ReadBuildInfo().Main.Version, or "devel"
// when build info is missing, empty, or "(devel)".
func waBuildVersion() string

// mlwhCLIRemoteConfig returns RemoteConfig{BaseURL, UserAgent:
// "wa-cli/"+waBuildVersion(), Identity: {ClientID: run ID, Username: local
// username}}. The run ID is 16 crypto/rand bytes as lowercase hex, made once
// per process (sync.OnceValue). The username is os/user.Current().Username,
// else $USER, else "".
func mlwhCLIRemoteConfig(baseURL string) mlwh.RemoteConfig

// mlwhServerRemoteConfig returns RemoteConfig{BaseURL, UserAgent:
// product+"/"+waBuildVersion()} with no identity.
func mlwhServerRemoteConfig(baseURL, product string) mlwh.RemoteConfig
```

- The seven CLI sites (`openMLWH{Info,Latest,Search,Runs,Export,Studies}...`
  in their `cmd/mlwh_*.go` files, and `wa mlwhdiff diff`) build their
  remote config with `mlwhCLIRemoteConfig`.
  `openMLWHStudiesConfiguredClient` also serves `wa mlwh programmes` and
  `wa mlwh people`.
- `openResultsServeMLWHClientWithConfig` (`cmd/results.go`) uses
  `mlwhServerRemoteConfig(cfg.ServerURL, mlwh.UserAgentProductResultsServer)`.
- `mlwhdiffMLWHConfig` gains `AsServer bool`. `openMLWHDiffClient` gains an
  `asServer bool` argument, set true by `newMLWHDiffServeCommand` and false
  by `newMLWHDiffDiffCommand`. `openMLWHDiffClientWithConfig` uses
  `mlwhServerRemoteConfig(..., mlwh.UserAgentProductMLWHDiffServer)` when
  `AsServer`, else `mlwhCLIRemoteConfig`.
- Existing cmd tests need no update: none compares a whole `RemoteConfig`
  or `mlwhdiffMLWHConfig` (`TestMLWHExportServerFlagUsesRemoteClientD1b`
  checks only `cfg.BaseURL`; the mlwhdiff E3 tests check only `ServerURL`
  and `CachePath`).

**Acceptance tests:**

1. Given `mlwhCLIRemoteConfig("http://x")` called twice, then both have
   BaseURL `http://x`, UserAgent matching `^wa-cli/\S+$`, the same ClientID
   matching `^[0-9a-f]{32}$`, and Username equal to
   `os/user.Current().Username`.
2. Given `waBuildVersion()` under `go test`, then it is non-empty and
   contains no space.
3. Given each of the eight `wa mlwh` commands (`info`, `latest`, `search`,
   `runs`, `export`, `studies`, `programmes`, `people`) run with
   `--server <url>` and its `openMLWH*RemoteClient` stub capturing the
   config, then each captured config equals `mlwhCLIRemoteConfig(<url>)`.
4. Given `openResultsServeMLWHClientWithConfig` with ServerURL at an
   `httptest` stub, when `AllStudies` runs, then the stub sees User-Agent
   `wa-results-server/<v>` and no `X-WA-Client-ID` or `X-WA-Client-User`.
5. Given `openMLWHDiffClientWithConfig` with `AsServer: true`, then the stub
   sees `wa-mlwhdiff-server/<v>` and no `X-WA-Client-*`. With
   `AsServer: false`, it sees `wa-cli/<v>` and an `X-WA-Client-ID`.
6. Given `wa mlwhdiff serve` and `wa mlwhdiff diff` with
   `openMLWHDiffClientFunc` stubbed to capture the config, then serve
   passes `AsServer: true` and diff `false`.

### D3: run-dev.sh wiring

As a developer, I want `make dev` to enable usage in test mode and keep its
own probes out of the counts, so that the page is exercisable locally.

**Package:** repository root
**File:** `run-dev.sh`
**Test file:** `cmd/run_dev_test.go`

- Mirror the feedback DB wiring with `USAGE_DB_PATH` /
  `USAGE_DB_EPHEMERAL`: test mode
  `mktemp "$TMP_DIR/mlwh-usage-test.XXXXXX.sqlite"`, removed with `-wal`
  and `-shm` on cleanup. Dev and prod use `${WA_MLWH_USAGE_PATH:-}` and
  create its parent directory. Only the auto-managed `mlwh serve` branch
  appends `--usage-db "$USAGE_DB_PATH"` when non-empty.
- `curl_probe` and the `mlwh_freshness_is_cold` `curl` add
  `-A wa-run-dev`, in every mode.

**Acceptance tests:**

1. Given `--mode test` with the fake toolchain, then the recorded
   `mlwh serve` invocation contains `--usage-db <path>` under
   `<repo>/.tmp/` with basename starting `mlwh-usage-test.`; after SIGINT
   the file and its `-wal`/`-shm` are gone.
2. Given `--mode dev` with a configured cache and no `WA_MLWH_USAGE_PATH`,
   then no `--usage-db`. With `WA_MLWH_USAGE_PATH=<tmp>/u/dev.sqlite`, then
   `--usage-db <tmp>/u/dev.sqlite`, `<tmp>/u` exists, and the file survives
   shutdown.
3. Given `WA_RUN_DEV_SEQMETA_CMD` set, then no `--usage-db`.
4. Given the fake `mlwh serve` stub extended to append each request's
   `user-agent` and URL to a log, when `--mode test` and a cold-cache dev
   run reach readiness, then every logged `/freshness` and `/studies`
   request has User-Agent `wa-run-dev`.

## E. Frontend

### E1: Web identity headers

As the server admin, I want web traffic to carry an anonymous session and
the logged-in user, so that web use is counted per browser.

**File:** `frontend/proxy.ts`, `frontend/lib/usage-identity.ts`,
`frontend/lib/backend-client.ts`, `frontend/app/(results)/actions.ts`,
`frontend/app/(results)/feedback/actions.ts`
**Test file:** `frontend/tests/usage-proxy.test.ts`,
`frontend/tests/usage-identity.test.ts`,
`frontend/tests/backend-client.test.ts`, `frontend/tests/actions.test.ts`,
`frontend/tests/feedback-actions.test.ts`

```ts
// lib/usage-identity.ts
export const usageSessionCookieName = "wa_usage_session";
export const usageSessionCookieOptions = {
    httpOnly: true,
    maxAge: 365 * 24 * 60 * 60,
    path: "/",
    sameSite: "lax",
    secure: true,
} as const;
export const mlwhWebUserAgent = "wa-web";
// usageClientId returns the first 32 hex chars of SHA-256(cookieValue).
export function usageClientId(cookieValue: string): string;
// mlwhIdentityHeaders returns x-wa-client-id from the usage cookie and
// x-wa-client-user from username, or from currentSession() when username
// is undefined. Values are sanitised as in C1. It never throws: a failing
// cookies() omits the ID and a failing currentSession() omits the user.
export async function mlwhIdentityHeaders(
    username?: string | null,
): Promise<Record<string, string>>;

// proxy.ts
export function proxy(request: NextRequest): NextResponse;
export const config = {
    matcher: ["/((?!api|_next/static|_next/image|favicon.ico).*)"],
};
```

- `proxy`: when the request has no `wa_usage_session` cookie, or its value
  is not a UUID, it sets a new `crypto.randomUUID()` on the forwarded
  request and on the response with `usageSessionCookieOptions`. The
  forwarded cookie header keeps every other cookie (e.g. `wa_results_jwt`):
  call `request.cookies.set(name, uuid)`, which rewrites the request's
  `cookie` header with all cookies, then
  `NextResponse.next({ request: { headers: request.headers } })`. Never
  build a cookie header holding only `wa_usage_session`. Otherwise it
  returns `NextResponse.next()` unchanged.
- `mlwhJson` always sends `user-agent: wa-web`, overriding any caller
  value. `lib/studies-cache.ts` is unchanged, so `/studies` carries only the
  User-Agent.
- In `app/(results)/actions.ts`, `validateIdentifier`, `enrichIdentifier`,
  `fetchStudySamples`, and `fetchStudyLibrarySamples` pass
  `{ headers: await mlwhIdentityHeaders() }`. `enrichIdentifiers` computes
  the headers once and reuses them for every value.
- `feedback/actions.ts` merges `await mlwhIdentityHeaders(session.username)`
  into `feedbackRequest` headers, reusing the gate's session:
  `authorizeFeedback` (today returning `{ token, args }`) also returns
  `username` from its `currentSession()` call, so `currentSession` runs once
  per action.
- Existing tests asserting exact `fetch` arguments break with the forced
  User-Agent; update them to expect `headers: { "user-agent": "wa-web", ... }`
  (or check headers via `new Headers(init.headers)`):
  `backend-client.test.ts` ("returns validated MLWH JSON ...", "preserves a
  path prefix in the configured MLWH backend URL", "passes request options
  through ...", "accepts an empty 204 ...", and the CA-certificate test's
  `https://mlwh.example/studies` assertion), and in `actions.test.ts` the A3,
  A4, and H2 `toHaveBeenCalledWith("https://mlwh:9000/...")` assertions.

**Acceptance tests:**

1. Given `proxy(new NextRequest("https://h/"))` with no cookie, then the
   response `set-cookie` contains `wa_usage_session=<uuid>`, `HttpOnly`,
   `Secure`, `SameSite=lax`, `Path=/`, and `Max-Age=31536000`, and
   `x-middleware-request-cookie` contains the same `wa_usage_session=<uuid>`.
2. Given a valid UUID cookie, then no `set-cookie`. Given cookie value `x`,
   then a new UUID is set.
3. Given request cookies `wa_results_jwt=J` and no usage cookie, then
   `x-middleware-request-cookie` contains both `wa_results_jwt=J` and
   `wa_usage_session=<uuid>`. Given `wa_results_jwt=J` and
   `wa_usage_session=x`, then it contains `wa_results_jwt=J` and
   `wa_usage_session=<new uuid>`, not `wa_usage_session=x`.
4. Given `config.matcher`, then it excludes `/api/...`, `/_next/static/...`,
   `/_next/image...`, and `/favicon.ico`, and matches `/` and `/usage`.
5. Given `usageClientId("abc")`, then `"ba7816bf8f01cfea414140de5dae2223"`.
6. Given `cookies()` mocked with `wa_usage_session=abc` and
   `currentSession` returning `alice`, then `mlwhIdentityHeaders()` returns
   `{"x-wa-client-id":"ba7816bf8f01cfea414140de5dae2223","x-wa-client-user":"alice"}`.
   Given an anonymous session, then only `x-wa-client-id`. Given
   `mlwhIdentityHeaders(null)`, then `currentSession` is not called and no
   user header is set.
7. Given `cookies()` throwing and `currentSession` rejecting, then
   `mlwhIdentityHeaders()` resolves `{}`. Given username `"zo\u00eb"`, then
   `x-wa-client-user` is `"zo??"` (each non-ASCII UTF-8 byte becomes `?`).
8. Given a two-argument `mlwhJson` call, then `fetch` receives header
   `user-agent: wa-web`. Given caller header `user-agent: x`, still
   `wa-web`.
9. Given `getStudies()` with an empty cache, then the fetch has
   `user-agent: wa-web` and no `x-wa-client-id` or `x-wa-client-user`.
10. Given cookie `abc` and session `alice`, when `fetchStudySamples("S1")`
    runs, then the fetch carries the User-Agent, client ID, and user above.
11. Given `enrichIdentifiers(["a", "b", "c"])` with an empty `MLWHCache`,
    then three fetches carry the identity headers and `currentSession` is
    called once.
12. Given admin `alice` and token `T`, when `listFeedbackAction` runs, then
    the fetch carries `authorization: Bearer T`, `user-agent: wa-web`, and
    `x-wa-client-user: alice`, and `currentSession` is called once.

### E2: Contracts

As the frontend, I want validated usage payloads, so that contract drift
fails fast.

**File:** `frontend/lib/contracts.ts`
**Test file:** `frontend/tests/contracts.test.ts`

```ts
export const usageWindowSchema = z.enum(["24h", "7d", "30d", "all"]);
export type UsageWindow = z.infer<typeof usageWindowSchema>;
export const usageCategorySchema = z.enum([
    "cli", "mcp", "web", "results_server", "mlwhdiff_server", "other",
]);
export type UsageCategory = z.infer<typeof usageCategorySchema>;
const countMap = z.record(z.string(), z.number().int().nonnegative());
const count = z.number().int().nonnegative();
export const usageSummarySchema = z.object({
    window: usageWindowSchema,
    generated_at: z.string(),
    window_start: z.string(),
    earliest_record: z.string(),
    retention_days: z.number().int().positive(),
    dropped_events: count,
    totals: z.object({
        requests: count, errors: count, error_rate: z.number(),
        sessions: count, ip_clients: count, users: count,
    }),
    categories: z.array(z.object({
        category: usageCategorySchema,
        requests: count, errors: count, error_rate: z.number(),
        sessions: count, ip_clients: count, users: count,
        requests_per_session_median: count,
        requests_per_session_max: count,
    })),
    endpoints: z.array(z.object({
        method: z.string(), route: z.string(),
        requests: count, errors: count, error_rate: z.number(),
        p50_ms: z.number(), p95_ms: z.number(), by_category: countMap,
    })),
    mcp_tools: z.array(z.object({ value: z.string(), requests: count })),
    mcp_agents: z.array(z.object({ value: z.string(), requests: count })),
    other_user_agents: z.array(z.object({ value: z.string(), requests: count })),
    unmatched: z.array(z.object({
        method: z.string(), path: z.string(), requests: count,
        by_category: countMap, by_tool: countMap, by_agent: countMap,
    })),
    users: z.array(z.object({
        name: z.string(), by_ip: z.boolean(), requests: count,
        by_category: countMap,
    })),
    clients: z.array(z.object({
        name: z.string(), by_ip: z.boolean(), requests: count,
        by_category: countMap,
    })),
    days: z.array(z.object({
        date: z.string(), requests: count, by_category: countMap,
    })),
    series: z.array(z.object({
        start: z.string(), end: z.string(), requests: count,
        by_category: countMap, by_endpoint: countMap,
    })),
});
export type UsageSummary = z.infer<typeof usageSummarySchema>;
```

**Acceptance tests:**

1. Given the JSON from A3 test 1 (copied as a fixture), then
   `usageSummarySchema.parse` succeeds.
2. Given that fixture with `window: "1h"`, a category `"bogus"`, or
   `series` missing, then `safeParse` fails.
3. Given `usageWindowSchema`, then the four values parse and `"1h"`, `""`,
   and `null` fail.

### E3: Server Action

As an allowlisted admin, I want a Server Action that fetches the summary
with the token server-side, so that the browser never sees it.

**File:** `frontend/app/(results)/usage/actions.ts` (`"use server"`)
**Test file:** `frontend/tests/usage-actions.test.ts`

```ts
export type UsageUnavailableReason =
    | "no_token"
    | "usage_disabled"
    | "token_rejected"
    | "unsupported"
    | "backend_error";
export type UsageSummaryState =
    | { status: "ok"; summary: UsageSummary }
    | { status: "unauthenticated" | "forbidden" | "invalid_input" }
    | { status: "unavailable"; reason: UsageUnavailableReason };
export async function getUsageSummaryAction(
    window: UsageWindow,
): Promise<UsageSummaryState>;
```

Checks in order, as in feedback E3: no session -> `unauthenticated`;
`!isFeedbackAdmin(username)` -> `forbidden`; `usageWindowSchema.safeParse`
fails -> `invalid_input`; `readMLWHServerToken()` null ->
`unavailable/no_token`. Extracting feedback's `authorizeFeedback` into a
shared module is allowed if feedback tests pass unchanged; its result
carries `username` (E1). Then
`mlwhJson("/usage?window=<w>", usageSummarySchema, {cache: "no-store", headers: {authorization: "Bearer <token>", ...identity}})`,
with identity from `mlwhIdentityHeaders(username)` using the gate's
session, so `currentSession` is called once.

Errors: 503 with code `usage_disabled` -> `usage_disabled`; 401 ->
`token_rejected`; 404, any body -> `unsupported` (wa older than 1.10.0);
anything else, including `BackendUnavailableError` and contract failures,
-> `backend_error`.

**Acceptance tests:**

1. Given no session, then `unauthenticated` and `fetch` not called.
2. Given session `bob` not allowlisted, then `forbidden`, no fetch.
3. Given admin `alice` and no token file, then `unavailable/no_token`.
4. Given admin, token `T`, and `"24h"`, then the fetch URL is
   `<base>/usage?window=24h` with `authorization: Bearer T`, and a valid
   summary body gives `ok` with the parsed summary.
5. Given `"1h"` or `null` cast past TS, then `invalid_input` and no fetch.
6. Given wa 503 `usage_disabled`, 401, 404 text `404 page not found`, 500,
   and a rejecting `fetch`, then `usage_disabled`, `token_rejected`,
   `unsupported`, `backend_error`, `backend_error`.
7. Given token contents `tok-7f3a9c`, then no returned state's
   `JSON.stringify` contains `tok-7f3a9c`.

### E4: Usage page and view

As an allowlisted admin, I want a `/usage` page with window presets, so that
I can see at a glance whether and how the server is used.

**File:** `frontend/app/(results)/usage/page.tsx`,
`frontend/components/usage-admin-view.tsx`, `frontend/lib/usage-messages.ts`
**Test file:** `frontend/tests/usage-page.test.ts`

- Page (Server Component, `dynamic = "force-dynamic"`, in `(results)`):
  reads `window` from `(await searchParams) ?? {}`; a valid
  `usageWindowSchema` value is used, anything else is `7d`. It calls
  `getUsageSummaryAction` and renders non-`ok` states in one
  `role="status"` panel under heading "MLWH usage":
    - `unauthenticated`: "Log in to view usage."
    - `forbidden`: "You do not have access to usage."
    - `invalid_input`: "Invalid usage request."
    - `unavailable`: "Usage is unavailable." plus
      `usageReasonMessages[reason]`: `no_token` "The MLWH server token is
      not readable by this server." `usage_disabled` "Usage tracking is
      disabled on the MLWH server." `token_rejected` "The MLWH server
      rejected the admin token." `unsupported` "wa mlwh serve is too old for
      usage metrics (needs MLWH API 1.10.0)." `backend_error` "The MLWH
      server could not be reached."
- `UsageAdminView` (`summary`, `window` props; Server Component or client,
  implementor's choice):
    - Heading "MLWH usage". A `nav` labelled "Window" with links "Last 24
      hours" `/usage?window=24h`, "Last 7 days" `/usage`, "Last 30 days"
      `/usage?window=30d`, "All time" `/usage?window=all`; the current one
      has `aria-current="page"`.
    - Note: "Usernames are self-declared by callers. Web counts are requests
      that reached the MLWH server; lookups served from browser or Next.js
      caches are not counted." When `dropped_events > 0`, also
      "<n> requests were not recorded because the usage queue was full."
    - Stat tiles: "Requests", "Errors" (`<n> (<rate>)`), "Sessions",
      "Clients by IP", "Users (self-declared)".
    - Category labels: `cli` "CLI", `mcp` "MCP", `web` "Web",
      `results_server` "Results server", `mlwhdiff_server` "mlwhdiff
      server", `other` "Other API". A categories cell lists non-zero
      `by_category` entries in category order as `"<label> <n>"` joined by
      ", ".
    - Rates render as percent with one decimal (`0.25` -> `25.0%`);
      latencies as ms with one decimal (`20` -> `20.0`).
    - Sections, each an `h2` then a table, or "None." when empty:
        - "By category": Category, Requests, Errors, Error rate, Sessions,
          Clients by IP, Users, Requests/session (median), Requests/session
          (max). All six rows always.
        - "Trend" (omitted for `all`): heading text
          "Trend (24-hour periods)", "(7-day periods)", or "(30-day
          periods)". Rows: "All requests", the six categories, then every
          endpoint key present in any period's `by_endpoint`, ordered by
          series total descending then key. Columns: label, "Latest"
          (period 0), "Previous" (period 1, `-` with one period), and a bar
          strip `role="img"` with `aria-label`
          `"<label>: latest <x>, previous <y>, <n> periods"` holding one bar
          per period, height proportional to the row's max, each with
          `title="<start> to <end>: <count>"`. An empty `series` shows
          "No usage recorded yet."
        - "Endpoints": Endpoint (`<METHOD> <route>`), Requests, Errors,
          Error rate, Median latency (ms), p95 latency (ms), Categories.
        - "MCP tools", "MCP agents", "Other API user agents": Value,
          Requests (`""` renders as "(none)").
        - "Unmatched paths": Method, Path, Requests, Categories, MCP tools,
          MCP agents (maps rendered as `"<key> <n>"` joined by ", ").
        - "Top users (self-declared)": User, Requests, Categories.
        - "Top clients": Client (`<ip> (by IP)` when `by_ip`), Requests,
          Categories.
        - "Requests per day (UTC)": Date, Requests, Categories.
    - Semantic tokens, mobile-first, tables scroll horizontally on narrow
      screens.

**Acceptance tests:**

1. Given `searchParams` `Promise.resolve({})` and an admin, then
   `getUsageSummaryAction` is called with `"7d"`. Given `{window: "30d"}`,
   then `"30d"`. Given `{window: "bogus"}` or `{window: ["24h", "7d"]}`,
   then `"7d"`.
2. Given each non-`ok` state and each `UsageUnavailableReason`, then the
   markup contains the exact message(s) above.
3. Given the E2 fixture (A3 test 1, window `24h`), then the markup contains
   "Last 24 hours" with `aria-current="page"`, stat "Requests" 6, the "By
   category" row "CLI" with 3 and "33.3%", the "Endpoints" row
   `GET /studies` with "25.0%", "20.0", "40.0", and "CLI 2, MCP 1, Other
   API 1", and "Trend (24-hour periods)".
4. Given that fixture, then the Trend row "All requests" has Latest 6,
   Previous 1, and 10 bars; its `aria-label` is
   `"All requests: latest 6, previous 1, 10 periods"`.
5. Given that fixture, then "Top clients" shows `10.0.0.9 (by IP)` and
   "Unmatched paths" shows `/bogus` with "Other API 1".
6. Given a fixture with window `all` and `series: []`, then no "Trend"
   heading. Given window `7d` and `series: []`, then "No usage recorded
   yet.".
7. Given `dropped_events: 3`, then "3 requests were not recorded because
   the usage queue was full." Given 0, then no such text.
8. Given `mcp_tools: []`, then the "MCP tools" section shows "None.".
9. Given the note text, then "Usernames are self-declared by callers."
   appears on every `ok` render.

### E5: Auth menu link

As an allowlisted admin, I want a Usage link in the account menu, so that I
can find the page.

**File:** `frontend/components/auth-menu.tsx`,
`frontend/app/(results)/layout.tsx`
**Test file:** `frontend/tests/auth-menu.test.ts`

`AuthMenu` gains `showUsageLink?: boolean` (default false). The layout
passes the same value as `showFeedbackLink`. When true and authenticated,
the dropdown shows `menuitem` link "Usage" to `/usage` (lucide
`ChartColumn` icon) after "Feedback" and before "Log out".

**Acceptance tests:**

1. Given an authenticated session and `showUsageLink`, when the menu opens,
   then a link "Usage" with `href="/usage"` is present.
2. Given no prop, or an anonymous session with the prop, then no "Usage"
   link.
3. Given the layout set up as in feedback E5 tests 4-6 with authenticated
   `svc` (OS user), then "Usage" and "Feedback" links are present; with
   `bob`, neither.

## F. Docs and Release

### F1: Operator documentation

As an operator, I want usage tracking documented, so that I can enable it
and tell users what is recorded.

**File:** `README.md`, `.docs/mcp/security-posture.md`, `.env.development`,
`.env.production`, `.agents/skills/verify-wa/features/usage-admin-page.md`,
`.agents/skills/verify-wa/features/README.md`,
`.agents/skills/verify-wa/SKILL.md`,
`.agents/skills/verify-wa/scripts/doctor.sh`
**Test file:** `mlwh/docs_test.go`

- README `mlwh serve` section: `--usage-db` / `WA_MLWH_USAGE_PATH`,
  `--usage-retention-days` / `WA_MLWH_USAGE_RETENTION_DAYS`, the shared
  admin token, the `/usage` page and `WA_FEEDBACK_ADMINS`, the header table
  (`X-WA-Client-ID`, `X-WA-Client-User`, `X-WA-MCP-*`, User-Agent
  products with their senders as in the wire contract, including
  `wa mlwh programmes` and `wa mlwh people`), that usernames are
  self-declared and CLI/MCP users cannot opt out, the `wa_usage_session`
  cookie (anonymous, used only for usage counting), that cache hits are
  not counted, that `wa-run-dev` traffic is excluded, and the Known limits.
- Security posture gains a "Usage metrics" section: what each row stores
  (including socket IP and self-declared username), that the usage DB is a
  separate file written by the server for every counted request, and the
  cookie. Its statement about the server's only write gains the usage DB.
  The "No route checks a credential except the feedback admin routes"
  sentence (unauthenticated plain HTTP section) also names `GET /usage`,
  which needs the admin Bearer token whenever usage is on.
- Commented `WA_MLWH_USAGE_PATH=` (dev: `.tmp/mlwh-usage.sqlite`) and
  `WA_MLWH_USAGE_RETENTION_DAYS=` in both root env files.
- verify-wa: a `usage-admin-page.md` recipe in the format of
  `feedback-admin-page.md` (reach `/usage` via the account menu, switch
  windows, expected sections, gotchas), a row in `features/README.md`, the
  `/usage` page in the SKILL description, and `doctor.sh` checking that
  unauthenticated `GET /usage` is 401.

**Acceptance tests:**

1. Given `README.md`, then it contains `--usage-db`, `WA_MLWH_USAGE_PATH`,
   `--usage-retention-days`, `WA_MLWH_USAGE_RETENTION_DAYS`,
   `X-WA-Client-ID`, `X-WA-Client-User`, `wa_usage_session`,
   `self-declared`, and `wa-run-dev`.
2. Given `.docs/mcp/security-posture.md`, then it contains
   `Usage metrics`, `X-WA-Client-User`, and `wa_usage_session`.
3. Given `.agents/skills/verify-wa/features/README.md`, then it links
   `usage-admin-page.md`.

### F2: Release v0.11.0

After all stories pass, merge and tag `v0.11.0`. The MCP repo then bumps
`github.com/wtsi-hgi/wa` to `v0.11.0` and implements its companion spec.
Release step, no tests.

## Implementation Order

1. **Model and store (A1, A2).** Sequential foundation.
2. **Summary (A3).** After 1.
3. **Middleware and admin endpoint (B1-B3).** After 1; B2 needs 2. B1-B3
   share files; do them in order.
4. **RemoteClient identity (C1).** Parallel with 1-3 except test 8, which
   needs B1. **OpenAPI (C2)** after C1 (it uses the cap constants from A1).
5. **Serve wiring (D1)** after 3; **identities (D2)** after C1; **run-dev
   (D3)** after D1.
6. **Frontend.** E1 and E2 in parallel (independent of Go), then E3, then
   E4 and E5 in parallel.
7. **Docs (F1).** After 5 and 6.
8. **Verification.** `golangci-lint run --fix`,
   `CGO_ENABLED=1 go test -tags netgo --count 1 ./...`, and
   `cd frontend && pnpm lint && pnpm test`. Then release (F2).

## Appendix: Key Decisions

- **Headers, not body fields or query params.** Every Registry route is a
  GET with typed query parameters; headers add identity without touching
  any endpoint contract, so old servers ignore them and old clients count
  as "other API".
- **Template from registration, not `c.FullPath()`.** gin groups copy their
  handler chain at creation, so a later `router.Use` misses the auth group.
  Prepending the middleware per route at registration covers both modes,
  and the captured template has no `/rest/v1/auth` prefix, so plain and
  secured rows aggregate together. `NoRoute` covers unmatched requests.
- **Async single writer.** A buffered channel plus one goroutine keeps
  SQLite off the request path and avoids writer contention. Dropping on a
  full queue, counted in `dropped_events`, is the price of never blocking.
  `Summary` flushes first so the page and tests see every recorded request.
- **One row per request, aggregates in SQL.** Matches the settled storage
  decision; indexes on `ts_ms` and `(method, route, ts_ms)` keep window
  queries bounded, and retention bounds the table.
- **Error = status >= 400.** Client errors (bad parameters, unknown IDs)
  are what LLM callers most often hit, so they count. Unmatched paths are
  listed separately as well.
- **Nearest-rank percentiles.** Deterministic integer ranks keep tests
  exact.
- **Client-side sanitising.** `net/http` and Node `fetch` reject some header
  bytes; replacing them keeps a strange username from failing the MLWH
  call.
- **Headers in `components.parameters` only.** Referencing them from every
  operation would change Registry parameter coverage tests and the MCP
  tools' generated schemas for no gain; the MCP server sets them through
  `RemoteConfig` and context, not tool inputs.
- **Usage admin route outside OpenAPI**, like feedback's admin routes.
- **Testing.** GoConvey with `t.TempDir()` SQLite and `httptest`, D1 tests
  7-9 against a real listener; Vitest with `fetch`, `next/headers`,
  `node:os`, and `currentSession` mocked. Follow **go-implementor** /
  **go-reviewer** and **nextjs-fastapi-implementor** /
  **nextjs-fastapi-reviewer**; the Next.js app has no FastAPI backend, so
  wa Go is the backend.
