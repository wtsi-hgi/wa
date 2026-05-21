# Phase 3: go-authserver serving

Ref: [spec.md](spec.md) sections A2, A3, A4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.
Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

## Items

### Item 3.1: A3 - LDAP authentication callback

spec.md section: A3

Create `internal/authldap/ldap.go` and
`internal/authldap/ldap_test.go` with `Dialer`, `DialFunc`,
`UIDLookup`, and `CheckPassword`. Use `ldaps://<ldap_server>:636`,
format the bind DN with the username, perform UID lookup before dialing,
and return `(false, "")` for lookup or bind failures. Covering all 3
acceptance tests from A3.

- [ ] implemented
- [ ] reviewed

### Item 3.2: A4 - Owner session tracking and logout

spec.md section: A4

Implement WA owner-session tracking in `results/auth.go` with
`OwnerSessionConfig`, `OwnerSessionMiddleware`, an in-memory
`OwnerSessionStore`, `CurrentUserFromContext`, and the
`POST /rest/v1/auth/logout` route. Mark owner sessions only when the
server-starting user logs in with the server token, carry owner state
through JWT refresh, and clear owner markers on logout. Covering all 6
acceptance tests from A4.

- [ ] implemented
- [ ] reviewed

### Item 3.3: A2 - HTTPS-only serving

spec.md section: A2

Wire `wa results serve` in `cmd/results.go` to go-authserver using
`gas.New`, `EnableAuthWithServerToken`, and the appropriate HTTPS or ACME
start path. Add TLS, ACME, LDAP, bind address, and server-token flag/env
validation in `cmd/results_serve_test.go`, including strict ACME cache
permissions and legacy `--port` handling. Depends on items 3.1 and 3.2.
Covering all 5 acceptance tests from A2.

- [ ] implemented
- [ ] reviewed
