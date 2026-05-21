# Phase 6: Next auth proxy

Ref: [spec.md](spec.md) sections E1, E2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
frontend subagents with the `nextjs-fastapi-implementor` and
`nextjs-fastapi-reviewer` skills. Additional required skill:
`frontend-design`.

Run frontend checks from `frontend/` with bounded commands, including
`timeout 120s pnpm test` for Vitest coverage and
`timeout 120s pnpm lint` for Prettier and ESLint. Run focused tests when
available before broader checks. Do not run long-lived servers.

## Items

### Item 6.1: E1 - Server-side auth handlers

spec.md section: E1

Add server-side login, logout, refresh, and current-session handling in
`frontend/app/(results)/auth/actions.ts`,
`frontend/app/api/auth/login/route.ts`,
`frontend/app/api/auth/logout/route.ts`, and
`frontend/app/api/auth/refresh/route.ts`. Proxy credentials and JWTs only
from server-side code, store `wa_results_jwt` as an HTTP-only Secure
SameSite=Lax cookie, expire it on logout, and tolerate backend logout
404/501 by still clearing the cookie. Covering all 6 acceptance tests from
E1.

- [ ] implemented
- [ ] reviewed

### Item 6.2: E2 - Secure backend client

spec.md section: E2

Update `frontend/lib/backend-client.ts`,
`frontend/app/(results)/actions.ts`, and
`frontend/app/api/file/route.ts` so frontend server code requires an HTTPS
`WA_RESULTS_BACKEND_URL`, trusts `WA_RESULTS_BACKEND_CA_CERT` only for WA
backend requests, selects auth or public endpoints based on the
HTTP-only cookie, and preserves locked 403 JSON responses. Depends on item
6.1 for cookie/session conventions. Covering all 10 acceptance tests from
E2.

- [ ] implemented
- [ ] reviewed
