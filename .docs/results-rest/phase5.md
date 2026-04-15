# Phase 5: Unified wa Binary

Ref: [spec.md](spec.md) sections F1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 5.1: F1 - Root command and migration

spec.md section: F1

Implement `NewRootCommand() *cobra.Command` in `cmd/root.go`.
Create Cobra root command `wa` with three subcommand trees:
saga (cmd/saga.go), seqmeta (cmd/seqmeta.go), results
(cmd/results.go stub, including serve stub).

Migration tasks:
- Move main.go inspector logic to cmd/saga.go as `inspect`
  subcommand under `saga`, preserving all flags.
- Move cmd/seqmeta/main.go to cmd/seqmeta.go, preserving all
  existing flags and behaviour.
- Rewrite root main.go as thin entry point calling
  NewRootCommand().Execute().
- Move cmd/seqmeta/main_test.go to cmd/seqmeta_test.go.
- Move main_test.go to cmd/saga_test.go.
- Remove cmd/seqmeta/ directory.

Covers all 5 acceptance tests from F1.

- [ ] implemented
- [ ] reviewed
