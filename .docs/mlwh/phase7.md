# Phase 7: Final Sweep

Ref: [spec.md](spec.md) sections E4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 7.1: E4 - Deletion sweep

spec.md section: E4

Delete `saga/`, `cmd/saga.go`, `cmd/saga_test.go`, the `.docs/saga/`,
`.docs/results-seqmeta/`, and `.docs/seqmeta/` directories, every
`SAGA_*` env var across `.env.*` files, `Makefile`, `run-dev.sh`,
`README.md`, `DEVELOPING.md`, and `.github/`. Strip residual
`Project`/`HopProject`/`IdentifierProjectName` symbols from
`seqmeta/`. Strip `projectSchema`/`projectUserSchema`/`HopProject`/
`via Saga`/`SAGA` references from the frontend (excluding
`node_modules`, `pnpm-lock`, `e2e` snapshots, and generated build
output). Adjust `cmd/results_register_test.go` accordingly. Covers
all 8 acceptance tests from E4. Run only after every prior phase is
reviewed and green.

- [ ] implemented
- [ ] reviewed
