# Phase 6: CLI

Ref: [spec.md](spec.md) sections F1, F2, F3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 6.1: F1 - diff subcommand

spec.md section: F1

Implement Cobra root command (`seqmeta`) with persistent flags
(`--token`, `--base-url`, `--db`) and `diff` subcommand in
`cmd/seqmeta/main.go`. `diff --study <id>` and
`diff --sample <id>` print JSON diff results to stdout. Mutually
exclusive flags; error if neither or both provided. Depends on
all prior phases (SAGAProvider, Store, Diff wrappers). Covering
all 4 acceptance tests from F1.

- [x] implemented
- [x] reviewed

### Item 6.2: F2 - validate subcommand

spec.md section: F2

Add `validate <identifier>` subcommand to Cobra CLI. Prints
`IdentifierResult` JSON to stdout. Non-zero exit on unknown
identifier or missing argument. Covering all 3 acceptance tests
from F2.

- [x] implemented
- [x] reviewed

### Item 6.3: F3 - serve subcommand

spec.md section: F3

Add `serve` subcommand to Cobra CLI. Starts REST API server on
`--port` (default 8080). Wires SAGAProvider, Store, and Server
together. Invalid port returns non-zero exit. Covering all 3
acceptance tests from F3.

- [x] implemented
- [x] reviewed
