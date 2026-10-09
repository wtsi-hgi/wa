# Phase 6: Docs

Ref: [spec.md](spec.md) sections F1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating Go-side
docs and tests with the `go-implementor` and `go-reviewer` skills, and
frontend parts with the `nextjs-fastapi-implementor` and
`nextjs-fastapi-reviewer` skills.

Bound shell commands that could hang with `timeout`. Run frontend checks
from `frontend/` with bounded commands such as `timeout 120s pnpm test`.
Do not run long-lived servers.

## Items

### Item 6.1: F1 - Operator documentation

spec.md section: F1

Use the Go skills for `README.md`, `.docs/mcp/security-posture.md`,
`.env.development`, `.env.production`, and `mlwh/docs_test.go`. Use the
Next.js skills for `frontend/.env.example` and
`frontend/tests/scaffold.test.ts`. Cover every README topic listed in
spec.md F1, reword security posture line 26 rather than supplementing it,
and add the commented env entries. Depends on phases 4 and 5. Covering all
3 acceptance tests from F1.

- [x] implemented
- [x] reviewed
