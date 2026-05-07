# Phase 6: Frontend Cleanup

Ref: [spec.md](spec.md) sections F1, F2, F3, F4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Batch 1 (parallel)

#### Item 6.1: F1 - Project schemas removed [parallel with 6.2, 6.3, 6.4]

spec.md section: F1

Edit `frontend/lib/contracts.ts` to drop `projectSchema`,
`projectUserSchema`, `Project`, `ProjectUser`, and the `project` and
`users` fields on `enrichmentGraphSchema`. Update
`frontend/tests/contracts.test.ts`. `pnpm tsc --noEmit` must pass.
Covers all 3 acceptance tests from F1.

- [x] implemented
- [x] reviewed

#### Item 6.2: F2 - Seqmeta badge no longer renders project rows [parallel with 6.1, 6.3, 6.4]

spec.md section: F2

Edit `frontend/components/seqmeta-badge.tsx` to drop "Project" and
"Project users" rows and any "Saga"/"via Saga" string. Update
`frontend/tests/seqmeta-badge.test.ts`. Covers all 2 acceptance tests
from F2.

- [x] implemented
- [x] reviewed

#### Item 6.3: F3 - iRODS file contract tightened [parallel with 6.1, 6.2, 6.4]

spec.md section: F3

In `frontend/lib/contracts.ts`, narrow the iRODS schema to
`{id_product, collection, data_object, irods_path}`. Update
`frontend/components/result-detail-files.tsx` and
`frontend/tests/result-detail-files.test.tsx` so no Checksum/Size/AVU
DOM nodes remain. Covers all 3 acceptance tests from F3.

- [x] implemented
- [x] reviewed

#### Item 6.4: F4 - Hierarchical-search docs warn about first-call latency [parallel with 6.1, 6.2, 6.3]

spec.md section: F4

Update the `library=` filter help text in
`frontend/components/filter-builder.tsx` (or the equivalent
hierarchical-search docs surface) and the JSDoc on the
search-expansion helper so each contains "first call" and
"wa mlwh sync"; remove any "Saga"/"via Saga" string. Update
`frontend/tests/filter-builder.test.tsx`. Covers all 3 acceptance
tests from F4.

- [x] implemented
- [x] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).
