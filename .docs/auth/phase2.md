# Phase 2: Gin migration

Ref: [spec.md](spec.md) sections A1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.
Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

## Items

### Item 2.1: A1 - Gin route migration

spec.md section: A1

Convert the results API from Chi/plain HTTP to go-authserver/Gin route
registration in `results/server.go` and `results/server_file.go`. Replace
Chi URL params with `gin.Context.Param`, use Gin JSON responses, add
`NewServer(...opts)` and `RegisterRoutes(router *gin.Engine,
auth *gin.RouterGroup)`, and migrate `results/server_test.go` plus
`results/server_file_test.go` to Gin/httptest with faked auth. Preserve
existing route behavior while moving paths under `/rest/v1` and
`/rest/v1/auth`. Covering all 4 acceptance tests from A1.

- [ ] implemented
- [ ] reviewed
