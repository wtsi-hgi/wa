# Phase 5: CLI auth

Ref: [spec.md](spec.md) sections D1, D2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.
Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

## Items

### Item 5.1: D1 - Authenticated CLI client

spec.md section: D1

Update `cmd/results.go` with `resultsAuthClient`,
`newResultsAuthClient`, `resultsServerTokenBasename`, and
`resultsJWTBasename`. Use go-authserver `ClientCLI`, keep `--server` as
the HTTPS WA server URL, pass `host[:port]` to gas auth, reject paths in
the auth URL, pass the backend CA/cert path from `--cert`, and surface token
permission errors without prompting. Cover all 5 acceptance tests from D1.

- [ ] implemented
- [ ] reviewed

### Item 5.2: D2 - CLI endpoint permissions

spec.md section: D2

Route CLI commands in `cmd/results.go` according to auth requirements:
public search uses `/rest/v1/results`, while get, file listing/downloads,
register, delete, and rescan use authenticated requests and
`/rest/v1/auth/...` endpoints. Preserve locked 403 error messaging for
inaccessible reads. Depends on item 5.1. Cover all 4 acceptance tests from
D2.

- [ ] implemented
- [ ] reviewed
