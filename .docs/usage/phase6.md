# Phase 6: Verification and release

Ref: [spec.md](spec.md) sections F2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating Go-side
fixes with the `go-implementor` and `go-reviewer` skills, and frontend
fixes with the `nextjs-fastapi-implementor` and `nextjs-fastapi-reviewer`
skills.

Bound shell commands that could hang with `timeout`. Do not run
long-lived servers.

The companion MCP spec
(`/home/ubuntu/llm-knowledge-base/.docs/usage/spec.md`) depends on this
release, so this is the final phase.

## Items

### Item 6.1: F2 - Release v0.11.0

spec.md section: F2

First run the full verification from spec.md Implementation Order step 8
and fix any failure: `golangci-lint run --fix`,
`CGO_ENABLED=1 go test -tags netgo --count 1 ./...`, and
`cd frontend && pnpm lint && pnpm test`, each bounded with `timeout`.
After the orchestrator's spec-aware and spec-free PR reviews converge,
push the feature branch and open a PR against `develop`, and run the
`pr-resolver` skill on it until its review comments are resolved and CI
passes. Do not merge, push to `main`, or tag: the user merges and tags
`v0.11.0` after the PR lands. The MCP repo then bumps
`github.com/wtsi-hgi/wa` to `v0.11.0`. F2 has no acceptance tests; review
confirms that verification passed and the PR is open against `develop`
with comments resolved and CI green.

- [ ] implemented
- [ ] reviewed
