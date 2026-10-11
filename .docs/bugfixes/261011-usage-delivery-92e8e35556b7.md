# Delivery queue: usage metrics

- Branch: `usage`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, this checklist
- Worktree: `.` (main clone)
- Origin work: `.docs/usage/phase1.md` to `.docs/usage/phase6.md`

## Deliver queue

| Branch | Worktree | PR | Target | Depends on | Head | Status | Waits on | Next action |
| ------ | -------- | -- | ------ | ---------- | ---- | ------ | -------- | ----------- |
| `usage` | `.` | none | `develop` | none | in progress | implementing phases | phases 3-6 | finish phases, PR reviews |
| `syncrace-bd100063` | `../wa-syncrace` | none | `develop` | none | `18cf0b6` | recorded | owner completion boundary | reproduce and fix via bugfix |

## Incidental handoffs

- Phase 2 batch 3 review: data race in
  `TestClientSyncTwoTablesReconnectIndependently`.
  - Branch: `syncrace-bd100063`
  - Checklist: `.docs/bugfixes/261011-sync-test-race-8dc6200da998.md`
  - Entry commit: `18cf0b660783df7043ba4cd3ff40aa77ab7e0549`
