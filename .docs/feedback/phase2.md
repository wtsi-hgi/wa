# Phase 2: Endpoints and errors

Ref: [spec.md](spec.md) sections B1, B2, B3, B4, B5, B6

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.
Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

Items 2.1 to 2.6 share `mlwh/feedback_server.go` and
`mlwh/feedback_server_test.go`, so do them in order. Each item registers
the routes its own tests need; item 2.6 settles their final placement.

## Items

### Item 2.1: B1 - Submit feedback

spec.md section: B1

Add `WithFeedback(store, adminToken)` to `mlwh/server.go` and the submit
handler in `mlwh/feedback_server.go`, following the seven handler steps in
spec.md B1. Add the new error code constants (`bad_request`, `unauthorized`,
`payload_too_large`, `internal_error`, `feedback_disabled`) to
`mlwh/errors_http.go` and map `feedback_disabled` to `ErrFeedbackDisabled`
in `sentinelForHTTPErrorCode`, leaving existing mappings unchanged.
`RemoteAddr` is the host from `net.SplitHostPort(c.Request.RemoteAddr)`,
or the raw value if splitting fails. It is never taken from forwarded
headers. Test 3 replaces `slog.Default()` with a capturing
handler; restore the previous default with `t.Cleanup`. Depends on phase 1.
Covering all 12 acceptance tests from B1.

- [ ] implemented
- [ ] reviewed

### Item 2.2: B2 - Admin authentication

spec.md section: B2

Add the admin middleware in `mlwh/feedback_server.go` for the three admin
routes: 503 `feedback_disabled` when the store is nil, else a `Bearer`
token compared with `crypto/subtle.ConstantTimeCompare` on trimmed bytes,
else 401 `unauthorized` with message `admin token required`. Depends on
item 2.1. Covering all 6 acceptance tests from B2.

- [ ] implemented
- [ ] reviewed

### Item 2.3: B3 - List feedback

spec.md section: B3

Add the `GET /feedback` handler with `limit` (default 50, 0-500),
`offset`, `acknowledged`, and `category` query parsing, returning
`Page[FeedbackReport]`. Invalid values get 400 naming the parameter.
Depends on item 2.2. Covering all 7 acceptance tests from B3.

- [ ] implemented
- [ ] reviewed

### Item 2.4: B4 - Acknowledge and un-acknowledge

spec.md section: B4

Add the `PATCH /feedback/:id` handler with the unexported
`feedbackMaxPatchBodyBytes = 1024` cap, read before id parsing, positive
int64 id validation, boolean `acknowledged` validation, and the 404
`not_found` envelope `feedback <id> not found`. Depends on item 2.3.
Covering all 6 acceptance tests from B4.

- [ ] implemented
- [ ] reviewed

### Item 2.5: B5 - Delete feedback

spec.md section: B5

Add the `DELETE /feedback/:id` handler with the same id rules and 404
envelope as item 2.4, returning 204 with an empty body. Depends on item
2.4. Covering all 3 acceptance tests from B5.

- [ ] implemented
- [ ] reviewed

### Item 2.6: B6 - Route placement and disabled mode

spec.md section: B6

Wire the four routes in `RegisterRoutes(router, auth)` in `mlwh/server.go`:
always registered, submit on `auth` when non-nil else `router`, admin
routes on `router`. No `Registry` entry is added. Depends on item 2.5.
Covering all 4 acceptance tests from B6.

- [ ] implemented
- [ ] reviewed
