# Phase 5: Frontend admin page

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

## Items

### Batch 1 (parallel)

#### Item 5.1: E1 - Contracts and MLWH client options [parallel with 5.2]

spec.md section: E1

Add the feedback Zod schemas and types from spec.md E1 to
`frontend/lib/contracts.ts`, including `feedbackIdSchema` and
`feedbackListInputSchema`. Give `mlwhJson` in
`frontend/lib/backend-client.ts` an optional `BackendFetchOptions` argument
passed to `buildFetchInit`, as `resultsJson` does. Tests go in
`frontend/tests/contracts.test.ts` and
`frontend/tests/backend-client.test.ts`. Implement all 7 acceptance tests
from E1.

- [x] implemented
- [x] reviewed

#### Item 5.2: E2 - Token and allowlist helpers [parallel with 5.1]

spec.md section: E2

Create `frontend/lib/feedback-admin.ts` with
`defaultMLWHServerTokenBasename`, `feedbackPageSize`, `mlwhServerTokenPath`,
`readMLWHServerToken`, `feedbackAdmins`, and `isFeedbackAdmin`. Import
`homedir` and `userInfo` as named imports from `node:os`, and catch a
throwing `userInfo()` so neither admin helper throws. Tests go in
`frontend/tests/feedback-admin.test.ts`. Implement all 6 acceptance tests
from E2.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `nextjs-fastapi-reviewer` skill
(review all items in the batch together in a single review
pass).

### Item 5.3: E3 - Server Actions

spec.md section: E3

Create `frontend/app/(results)/feedback/actions.ts` (`"use server"`) with
`listFeedbackAction`, `setFeedbackAcknowledgedAction`, and
`deleteFeedbackAction` plus the state types from spec.md E3. Each action
checks session, allowlist, `safeParse` of its arguments, then the token,
before any `fetch`. Map backend errors to the unavailable reasons and
`not_found` as listed, and never put the token in a returned state. Tests
go in `frontend/tests/feedback-actions.test.ts`. Depends on items 5.1 and
5.2. Implement all 18 acceptance tests from E3.

- [x] implemented
- [x] reviewed

### Batch 2 (parallel, after item 5.3 is reviewed)

#### Item 5.4: E4 - Feedback page and view [parallel with 5.5]

spec.md section: E4

Create the Server Component `frontend/app/(results)/feedback/page.tsx`,
which sanitises `searchParams` and renders each state's exact message, and
the client component `frontend/components/feedback-admin-view.tsx` with the
filters, report articles, acknowledge and delete buttons, Sonner error
toasts, and Previous/Next links built by the param rules in spec.md E4.
Tests go in `frontend/tests/feedback-page.test.ts`. Implement all 13
acceptance tests from E4.

- [x] implemented
- [x] reviewed

#### Item 5.5: E5 - Auth menu link [parallel with 5.4]

spec.md section: E5

Add the `showFeedbackLink` prop to `frontend/components/auth-menu.tsx` and
compute it in `frontend/app/(results)/layout.tsx` as
`session.authenticated && isFeedbackAdmin(session.username)`. The
"Feedback" `menuitem` links to `/feedback` above "Log out". Tests go in
`frontend/tests/auth-menu.test.ts`, including test 7 pinning the existing
`router.refresh()` after login. Implement all 8 acceptance tests from E5.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `nextjs-fastapi-reviewer` skill
(review all items in the batch together in a single review
pass).
