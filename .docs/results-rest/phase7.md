# Phase 7: Integration Tests

Ref: [spec.md](spec.md) sections I1, I2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 7.1: I1 - MySQL Store Integration

spec.md section: I1

Implement MySQL Store integration tests in
`cmd/results_test.go`. Gated by `WA_RESULTS_TEST_MYSQL_DSN`
environment variable. When set, connect to MySQL, DROP
results tables, run full Store CRUD lifecycle (Upsert, Get,
Search, GetFiles, ReplaceOutputFiles, Delete) against real
MySQL. Verify ON DELETE CASCADE, LIKE search, RFC 3339
timestamp round-trip, PRAGMA silent ignore, and idempotent
schema creation. Depends on Phase 6 (all results code must
exist). Covers all 13 acceptance tests from I1.

- [ ] implemented
- [ ] reviewed

### Item 7.2: I2 - End-to-End CLI Integration

spec.md section: I2

Implement end-to-end CLI integration tests in
`cmd/results_test.go`. Start server via
`NewRootCommand().SetArgs(...)` in a goroutine, exercise
all CLI commands (register, search, get, rescan, delete)
through Cobra command paths against the running server.
SQLite variant always runs; MySQL variant runs when
`WA_RESULTS_TEST_MYSQL_DSN` is set (server started with
`--db <dsn>` to verify DSN-to-driver selection). Depends
on item 7.1. Covers all 7 acceptance tests from I2.

- [ ] implemented
- [ ] reviewed
