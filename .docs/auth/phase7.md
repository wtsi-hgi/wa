# Phase 7: Frontend UI

Ref: [spec.md](spec.md) sections E3, E4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
frontend subagents with the `nextjs-fastapi-implementor` and
`nextjs-fastapi-reviewer` skills. Additional required skill:
`frontend-design`.

Run frontend checks from `frontend/` with bounded commands, including
`timeout 120s pnpm test` for Vitest coverage,
`timeout 120s pnpm lint` for Prettier and ESLint, and
`timeout 180s pnpm exec playwright test` for browser flows when e2e tests
are added or changed. Do not run long-lived servers.

## Items

### Batch 1 (parallel)

#### Item 7.1: E3 - Compact login/logout tool [parallel with 7.2]

spec.md section: E3

Create `frontend/components/auth-menu.tsx` and integrate it in
`frontend/app/(results)/layout.tsx`. Use shadcn/ui `Button`,
`DropdownMenu`, `Avatar`, `Badge`, `Tooltip`, and `Alert` where
appropriate, with lucide `LogIn`, `LogOut`, `User`, and `LockKeyhole`
icons. Implement anonymous login, signed-in account menu, accessible
logout, and failed-login focus/error behavior. Covering all 4 acceptance
tests from E3.

- [ ] implemented
- [ ] reviewed

#### Item 7.2: E4 - Locked rows and direct locked page [parallel with 7.1]

spec.md section: E4

Update `frontend/components/results-columns.tsx`,
`frontend/components/results-table.tsx`,
`frontend/app/(results)/results/[id]/page-content.tsx`, and
`frontend/lib/contracts.ts` for access-aware rows and locked detail
rendering. Locked rows must be disabled, visually muted, show a lock icon
with tooltip, and remove detail links; locked detail responses render only
the locked state and dashboard return link. Covering all 5 acceptance
tests from E4.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `nextjs-fastapi-reviewer` skill
(review all items in the batch together in a single review pass).
