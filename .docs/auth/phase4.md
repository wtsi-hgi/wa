# Phase 4: Protected API policy

Ref: [spec.md](spec.md) sections B3, C1, C2, C3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.
Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

## Items

### Item 4.1: B3 - Annotate public results

spec.md section: B3

Add `AnnotateAccess(results []ResultSet, user *CurrentUser)` in
`results/server.go` and apply it to public and authenticated list, search,
and stats/latest routes. Public routes annotate with nil user; auth routes
use the current go-authserver user. Preserve query matching and aggregate
counts while populating nested `ResultSet.Access` values. Covering all 5
acceptance tests from B3.

- [ ] implemented
- [ ] reviewed

### Item 4.2: C1 - Protect detail and file APIs

spec.md section: C1

Add public and authenticated detail, file list, and file content policy in
`results/server.go` and `results/server_file.go`. Public routes must load
the result then return stable locked JSON through `WriteLocked` instead of
serving detail or bytes; auth routes must use `RequireResultAccess`.
Preserve 404 behavior and existing successful file headers for authorized
users. Covering all 8 acceptance tests from C1.

- [ ] implemented
- [ ] reviewed

### Item 4.3: C2 - Registration authorization

spec.md section: C2

Update registration in `results/server.go` and CLI register behavior in
`cmd/results.go` so only `/rest/v1/auth/results` accepts registration.
Server-owner-token sessions may set requester and operator, while LDAP
sessions preserve requester but force operator to the authenticated
username before validation and upsert. Covering all 5 acceptance tests
from C2.

- [ ] implemented
- [ ] reviewed

### Item 4.4: C3 - Server-owner-only mutations

spec.md section: C3

Apply `RequireServerOwner` to delete, rescan, and any non-registration
mutation introduced by this feature in `results/server.go` and
`cmd/results.go`. LDAP users, including the server-starting username when
logged in by LDAP password, must receive locked 403 responses and leave
data unchanged; owner-token sessions retain delete/rescan behavior.
Covering all 5 acceptance tests from C3.

- [ ] implemented
- [ ] reviewed
