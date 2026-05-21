# Phase 8: Dev/docs

Ref: [spec.md](spec.md) sections F1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating Go and
script work with the `go-implementor` and `go-reviewer` skills, and
frontend-facing changes with the `nextjs-fastapi-implementor` and
`nextjs-fastapi-reviewer` skills. Additional required skill:
`frontend-design`.

This phase may run alongside phase 6 after phase 3 has stabilized server
flags and token/TLS behavior. Bound shell commands that could hang with
`timeout`. Run frontend checks from `frontend/` with bounded commands,
including `timeout 120s pnpm test`, `timeout 120s pnpm lint`, and focused
Playwright checks with `timeout 180s pnpm exec playwright test` when e2e
coverage changes. Do not run long-lived servers.

## Items

### Item 8.1: F1 - Dev stack certificates

spec.md section: F1

Update `run-dev.sh`, `.env.development`, `.env.test`, `README.md`,
`cmd/run_dev_test.go`, and `frontend/tests/next-config.test.ts` so the dev
stack creates or reuses self-signed certificates under `.tmp/`, starts the
backend with explicit TLS and LDAP/test-auth configuration, exports
`WA_RESULTS_BACKEND_URL` and `WA_RESULTS_BACKEND_CA_CERT` for Next.js, and
assembles `next dev` with experimental HTTPS key/cert flags. Ensure fake
auth is test-only and development without LDAP flags still fails. Covering
all 4 acceptance tests from F1.

- [x] implemented
- [x] reviewed
