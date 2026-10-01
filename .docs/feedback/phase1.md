# Phase 1: Model and store

Ref: [spec.md](spec.md) sections A1, A2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.
Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

## Items

### Item 1.1: A1 - Categories and validation

spec.md section: A1

Create `mlwh/feedback.go` with `FeedbackCategory`, its five constants,
`FeedbackCategories`, `Description`, `Valid`, the six cap constants,
`FeedbackSubmission` with `Validate`, `FeedbackReceipt`, `FeedbackReport`,
and `FeedbackFilter`, exactly as in spec.md Architecture "Go types".
`Description` returns the text from the category table in spec.md "Wire
contract". Add the sentinels `ErrFeedbackDisabled`, `ErrFeedbackInvalid`,
`ErrFeedbackTooLarge`, `ErrFeedbackUnsupported`, and
`ErrFeedbackUnauthorized` to `mlwh/errors_http.go` now, since `Validate`
wraps two of them. Caps count bytes. `Validate` checks caps first, then
category, then description. Tests go in `mlwh/feedback_test.go`. Covering
all 12 acceptance tests from A1.

- [ ] implemented
- [ ] reviewed

### Item 1.2: A2 - SQLite feedback store

spec.md section: A2

Create `mlwh/feedback_store.go` with `FeedbackStore`, `OpenFeedbackStore`,
`Close`, `Add`, `List`, `SetAcknowledged`, and `Delete`, using the
`modernc.org/sqlite` DSN and schema from spec.md Architecture "Store". Keep
the unexported `now func() time.Time` for tests. `List` orders by `id DESC`
and never returns nil `Items`. `SetAcknowledged` and `Delete` of a missing
id return an error wrapping `ErrNotFound`. Tests go in
`mlwh/feedback_store_test.go` using `t.TempDir()`. Depends on item 1.1.
Covering all 11 acceptance tests from A2.

- [ ] implemented
- [ ] reviewed
