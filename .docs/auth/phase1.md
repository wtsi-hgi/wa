# Phase 1: Schema and access core

Ref: [spec.md](spec.md) sections B1, B2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.
Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

## Items

### Item 1.1: B2 - Evaluate access

spec.md section: B2

Implement `results/auth.go` with `AccessForResult`,
`RequireResultAccess`, and `RequireServerOwner`, plus focused tests in
`results/auth_test.go`. Cover requester, operator, Unix group ID, nil
user, group lookup error, and server-owner behavior. Covering all 7
acceptance tests from B2.

- [x] implemented
- [x] reviewed

### Item 1.2: B1 - Persist output directory GID

spec.md section: B1

Add nullable `output_directory_gid` storage to `results/store.go` and
`results/types.go`, including idempotent migration in `NewStore`,
server-side `OutputDirectoryGID(path)` capture during registration, and
JSON output. Ignore client-supplied GID values and reject registrations
when the output directory cannot be statted. Depends on item 1.1 for the
legacy NULL-GID access case. Covering all 5 acceptance tests from B1.

- [x] implemented
- [x] reviewed
