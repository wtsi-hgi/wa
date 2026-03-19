# Phase 2: Core Endpoints

Ref: [spec.md](spec.md) sections C1, C2, C3, C4, C5

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Batch 1 (parallel)

#### Item 2.1: C1 - Health check [parallel with 2.2, 2.3, 2.4, 2.5]

spec.md section: C1

Implement `Ping(ctx context.Context) error` on `Client` in
`saga/core.go`. Calls `GET /` and returns nil on 200. Test with
mock server in `saga/core_test.go` covering all 2 acceptance tests
from C1.

- [x] implemented
- [x] reviewed

#### Item 2.2: C2 - Version [parallel with 2.1, 2.3, 2.4, 2.5]

spec.md section: C2

Implement `Version(ctx) (*VersionInfo, error)` on `Client` in
`saga/core.go`. Parses `GET /version` response including nullable
`rev` field. Test in `saga/core_test.go` covering all 2 acceptance
tests from C2.

- [x] implemented
- [x] reviewed

#### Item 2.3: C3 - Current user [parallel with 2.1, 2.2, 2.4, 2.5]

spec.md section: C3

Implement `AuthMe(ctx) (*UserInfo, error)` on `Client` in
`saga/core.go`. Parses `GET /auth/me`. Test in `saga/core_test.go`
covering the 1 acceptance test from C3.

- [x] implemented
- [x] reviewed

#### Item 2.4: C4 - Generate token [parallel with 2.1, 2.2, 2.3, 2.5]

spec.md section: C4

Implement `GenerateToken(ctx) (*TokenResponse, error)` on `Client`
in `saga/core.go`. Calls `POST /auth/token`. Test in
`saga/core_test.go` covering the 1 acceptance test from C4.

- [x] implemented
- [x] reviewed

#### Item 2.5: C5 - List users [parallel with 2.1, 2.2, 2.3, 2.4]

spec.md section: C5

Implement `ListUsers(ctx) ([]User, error)` on `Client` in
`saga/core.go`. Parses `GET /users/` array response. Test in
`saga/core_test.go` covering the 1 acceptance test from C5.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
