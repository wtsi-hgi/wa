# Phase 7: Verification and release

Ref: [spec.md](spec.md) sections F2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating Go-side
fixes with the `go-implementor` and `go-reviewer` skills, and frontend
fixes with the `nextjs-fastapi-implementor` and `nextjs-fastapi-reviewer`
skills.

Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

The companion MCP spec
(`/home/ubuntu/llm-knowledge-base/.docs/feedback/spec.md`) depends on this
release, so this is the final phase.

## Items

### Item 7.1: F2 - Release v0.10.0

spec.md section: F2

First run the full verification from spec.md Implementation Order step 7
and fix any failure: `golangci-lint run --fix`,
`CGO_ENABLED=1 go test -tags netgo --count 1 ./...`, and
`cd frontend && pnpm lint && pnpm test`, each bounded with `timeout`. Then
merge the branch and tag `v0.10.0`. Merging to `main`, pushing, and
tagging need the user's explicit approval; never push to `main` directly.
The MCP repo then bumps `github.com/wtsi-hgi/wa` to `v0.10.0`. F2 has no
acceptance tests; review confirms that verification passed and the tag
points at the merged commit.

- [ ] implemented
- [ ] reviewed
