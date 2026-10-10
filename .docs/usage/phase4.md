# Phase 4: Frontend

Ref: [spec.md](spec.md) sections E1, E2, E3, E4, E5

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `nextjs-fastapi-implementor` and
`nextjs-fastapi-reviewer` skills. The Next.js app has no FastAPI backend;
wa Go is the backend, so the FastAPI parts of those conventions do not
apply.

Bound shell commands that could hang with `timeout`. Run frontend checks
from `frontend/` with bounded commands such as `timeout 120s pnpm test` and
`timeout 120s pnpm lint`. Do not run long-lived servers.

This phase does not depend on the Go phases: frontend tests mock `fetch`,
and the E2 fixture is copied from spec.md A3 test 1.

## Items

### Batch 1 (parallel)

#### Item 4.1: E1 - Web identity headers [parallel with 4.2]

spec.md section: E1

Create `frontend/lib/usage-identity.ts` with `usageSessionCookieName`,
`usageSessionCookieOptions`, `mlwhWebUserAgent`, `usageClientId`, and
`mlwhIdentityHeaders`, which never throws. Create `frontend/proxy.ts` with
`proxy` and `config`; it sets a new UUID cookie through
`request.cookies.set` so every other cookie stays in the forwarded header.
Make `mlwhJson` in `frontend/lib/backend-client.ts` always send
`user-agent: wa-web`. Pass identity headers from the four actions in
`frontend/app/(results)/actions.ts`, computing them once in
`enrichIdentifiers`. In `frontend/app/(results)/feedback/actions.ts`,
`authorizeFeedback` also returns `username` so `currentSession` runs once
per action. Update the existing `backend-client.test.ts` and
`actions.test.ts` assertions listed in spec.md E1. Tests go in the five
test files named in spec.md E1. Covering all 12 acceptance tests from E1.

- [ ] implemented
- [ ] reviewed

#### Item 4.2: E2 - Contracts [parallel with 4.1]

spec.md section: E2

Add `usageWindowSchema`, `usageCategorySchema`, `usageSummarySchema`, and
their types to `frontend/lib/contracts.ts`, exactly as in spec.md E2.
Tests go in `frontend/tests/contracts.test.ts`, with the A3 test 1 JSON as
a fixture. Covering all 3 acceptance tests from E2.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item under the
`subagents` skill's shared concurrency limits.
Launch review subagents using the `nextjs-fastapi-reviewer` skill
(review all items in the batch together in a single review
pass).

### Item 4.3: E3 - Server Action

spec.md section: E3

Create `frontend/app/(results)/usage/actions.ts` (`"use server"`) with
`UsageUnavailableReason`, `UsageSummaryState`, and
`getUsageSummaryAction`. Check session, allowlist, window, then token
before any `fetch`, and pass identity from
`mlwhIdentityHeaders(username)` using the gate's session. Map 503
`usage_disabled`, 401, 404, and every other failure to the reasons in
spec.md E3, and never put the token in a returned state. Extracting
`authorizeFeedback` into a shared module is allowed only if feedback tests
pass unchanged. Tests go in `frontend/tests/usage-actions.test.ts`.
Depends on items 4.1 and 4.2. Covering all 7 acceptance tests from E3.

- [ ] implemented
- [ ] reviewed

### Batch 2 (parallel, after item 4.3 is reviewed)

#### Item 4.4: E4 - Usage page and view [parallel with 4.5]

spec.md section: E4

Create the Server Component `frontend/app/(results)/usage/page.tsx`
(`dynamic = "force-dynamic"`), which defaults any invalid `window` to
`7d` and renders each non-`ok` state's exact message,
`frontend/lib/usage-messages.ts` with `usageReasonMessages`, and
`frontend/components/usage-admin-view.tsx` with the window `nav`, note,
stat tiles, and sections listed in spec.md E4, including the Trend bar
strips. Use semantic tokens and horizontally scrolling tables on narrow
screens. Tests go in `frontend/tests/usage-page.test.ts`. Depends on item
4.3. Covering all 9 acceptance tests from E4.

- [ ] implemented
- [ ] reviewed

#### Item 4.5: E5 - Auth menu link [parallel with 4.4]

spec.md section: E5

Add `showUsageLink?: boolean` to `frontend/components/auth-menu.tsx` and
pass the same value as `showFeedbackLink` from
`frontend/app/(results)/layout.tsx`. When true and authenticated, the
dropdown shows a "Usage" `menuitem` link to `/usage` with the lucide
`ChartColumn` icon, after "Feedback" and before "Log out". Tests go in
`frontend/tests/auth-menu.test.ts`. Covering all 3 acceptance tests from
E5.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item under the
`subagents` skill's shared concurrency limits.
Launch review subagents using the `nextjs-fastapi-reviewer` skill
(review all items in the batch together in a single review
pass).
