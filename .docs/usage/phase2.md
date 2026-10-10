# Phase 2: Summary, middleware, and remote client

Ref: [spec.md](spec.md) sections A3, B1, B2, B3, C1, C2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.
Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

This phase covers Implementation Order steps 2 to 4. Items 2.2, 2.3, and
2.5 share `mlwh/usage_server.go`, `mlwh/usage_server_test.go`, and
`mlwh/server.go`, so they run in that order. Items 2.4 and 2.6 touch
other files and run beside them.

## Items

### Batch 1 (parallel)

#### Item 2.1: A3 - Summary [parallel with 2.2]

spec.md section: A3

Create `mlwh/usage_summary.go` with `ErrUsageWindowInvalid`,
`UsageStore.Summary`, and the `UsageSummary`, `UsageTotals`,
`UsageCategoryStats`, `UsageEndpointStats`, `UsageCount`,
`UsageUnmatched`, `UsageActor`, `UsageDay`, and `UsagePeriod` types from
spec.md A3. Follow spec.md Architecture "Summary semantics" for periods,
`series`, `days`, nearest-rank percentiles, sort order, the
`usageTopLimit = 25` cap, and non-nil lists and maps. Compute aggregates
in SQL; `Summary` flushes first and never loads every row into Go memory.
Tests go in `mlwh/usage_summary_test.go`, building fixture F with
`openUsageStore` and a fixed clock. Also add the `Summary` assertion of
A2 test 10 to `mlwh/usage_store_test.go`. Depends on phase 1. Covering
all 15 acceptance tests from A3.

- [ ] implemented
- [ ] reviewed

#### Item 2.2: B1 - Counting middleware [parallel with 2.1]

spec.md section: B1

Add `WithUsage(store, adminToken)` to `mlwh/server.go` and
`(s *Server) usageMiddleware(route string)` in `mlwh/usage_server.go`.
When usage is enabled, `RegisterRoutes` prepends `usageMiddleware(path)`
to Registry handlers (plain router or auth group), `/openapi.json`, and
`POST /feedback`, and sets `router.NoRoute(s.usageMiddleware(""))`.
`/health` and the feedback admin routes get no middleware. Remote address
comes from `feedbackRemoteHost`, never forwarded headers. Usage disabled
means no middleware and no `NoRoute` change. Tests go in
`mlwh/usage_server_test.go`.

Test 6's `GET /usage` request needs the route from B2; write that part of
test 6 in item 2.3 and the `/health` and `/feedback` parts here. Depends on
phase 1. Covering all 14 acceptance tests from B1.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item under the
`subagents` skill's shared concurrency limits.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).

### Batch 2 (parallel, after batch 1 is reviewed)

#### Item 2.3: B2 - GET /usage [parallel with 2.4]

spec.md section: B2

Add `registerUsageAdminRoute(router)` and `requireUsageAdmin` in
`mlwh/usage_server.go`: 503 `usage_disabled` when the store is nil, else
401 `unauthorized` unless `feedbackAdminTokenMatches`, then the handler.
Absent or empty `window` means `7d`; `ErrUsageWindowInvalid` maps to 400
`invalid window "<value>"` and any other `Summary` error to 500
`could not summarise usage`. Add
`httpErrorCodeUsageDisabled = "usage_disabled"` to `mlwh/errors_http.go`
with no sentinel. Also add the `GET /usage` part of B1 test 6. Depends on
items 2.1 and 2.2. Covering all 7 acceptance tests from B2.

- [ ] implemented
- [ ] reviewed

#### Item 2.4: C1 - RemoteClient identity headers [parallel with 2.3]

spec.md section: C1

Create `mlwh/client_identity.go` with the five `Header*` constants,
`ClientIdentity`, and `WithClientIdentity`. Add `UserAgent` and `Identity`
to `RemoteConfig` in `mlwh/remote.go`, and one unexported
`(rc *RemoteClient) setIdentityHeaders(req)` called from `do` and from
`SubmitFeedback` in `mlwh/remote_feedback.go`. Context fields override
config fields one by one. Each value has every byte outside 0x20-0x7E
replaced with `?`, is cut to its A1 cap, and sends no header when empty.
Tests go in `mlwh/client_identity_test.go`; test 8 is a round trip through
the real `NewServer(q, WithUsage(store, T))`. Depends on items 1.1 and 2.2.
Covering all 8 acceptance tests from C1.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item under the
`subagents` skill's shared concurrency limits.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).

### Batch 3 (parallel, after batch 2 is reviewed)

#### Item 2.5: B3 - Route placement [parallel with 2.6]

spec.md section: B3

Wire `registerUsageAdminRoute` into `RegisterRoutes` in `mlwh/server.go`
on the root router, always registered, with no `Registry` entry and no
OpenAPI path. Tests go in `mlwh/usage_server_test.go`. Depends on item 2.3.
Covering all 3 acceptance tests from B3.

- [ ] implemented
- [ ] reviewed

#### Item 2.6: C2 - OpenAPI header parameters and API version [parallel with 2.5]

spec.md section: C2

In `mlwh/openapi.go`, set `APIVersion = "1.10.0"` and update its doc
comment, replace `info.description` with the exact text in spec.md C2, and
add the six `components.parameters` header entries using the A1 caps as
`maxLength`. No operation references them. Update
`openAPIB7InfoDescription` and `TestAPIVersionIsThePhase1DocumentationPatch`
in `mlwh/openapi_test.go`, and rename
`TestEndpointReferenceCommittedAtFeedbackVersionB7` in `mlwh/docs_test.go`
to `TestEndpointReferenceCommittedAtUsageVersionC2`. Regenerate
`MLWH_API_REFERENCE.md` with
`WA_REFRESH_DOCS=1 go test ./mlwh -run TestWriteEndpointReference`; only
its version line changes. Depends on item 2.4. Covering all 6 acceptance
tests from C2.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item under the
`subagents` skill's shared concurrency limits.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
