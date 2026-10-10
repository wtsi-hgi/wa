# Phase 1: Model and store

Ref: [spec.md](spec.md) sections A1, A2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.
Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

## Items

### Item 1.1: A1 - Categories, windows, and User-Agent parsing

spec.md section: A1

Create `mlwh/usage.go` with `UsageCategory` and its six constants,
`UsageCategories`, `UsageWindow` with its four constants, `UsageWindows`,
`Valid`, and `Duration`, the six `UserAgentProduct*` constants, the eight
`UsageMax*Bytes` caps, `ParseUsageUserAgent`, `UsageEvent`, and the
unexported `truncateUTF8`, exactly as in spec.md A1. The product token is
the text before the first `/` or space, matched exactly and
case-sensitively. Tests go in `mlwh/usage_test.go`. Covering all 5
acceptance tests from A1.

- [ ] implemented
- [ ] reviewed

### Item 1.2: A2 - SQLite usage store and async writer

spec.md section: A2

Create `mlwh/usage_store.go` with `UsageStore`, `OpenUsageStore`, `Record`,
`Flush`, `Prune`, `Dropped`, `RetentionDays`, and `Close`, backed by the
unexported `openUsageStore(ctx, path, retentionDays, queueSize, now)`. Use
`sqliteWritableDSN`, `createFeedbackStoreFile`, and the schema from spec.md
Architecture "Store". `Record` never blocks: a full queue
(`usageQueueSize = 4096`) drops the event, counts it, and logs
`mlwh usage event dropped` at most once per minute. One writer goroutine
inserts up to 256 events per transaction, logs `mlwh usage write failed`
on error, and prunes on open and hourly. Insert truncates each string field
to its cap with `truncateUTF8`. Tests go in `mlwh/usage_store_test.go`
using `t.TempDir()`.

Test 10's `Summary` assertion needs A3, which phase 2 adds; write that
assertion in item 2.1 and the `Flush` half here. Depends on item 1.1.
Covering all 10 acceptance tests from A2.

- [ ] implemented
- [ ] reviewed
