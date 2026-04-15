# Phase 6: Results CLI and Server Command

Ref: [spec.md](spec.md) sections G1, G2, G3, G4, G5, H1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 6.1: H1 - wa serve

spec.md section: H1

Implement the `serve` subcommand in `cmd/serve.go`. Opens
database (SQLite for file paths, MySQL for DSNs containing @),
creates results.NewStore, optionally creates
results.NewSeqmetaValidator, creates results.NewServer, and
listens on the given port. Flags: --port (int, default 8080),
--db (string, default "results.db"), --seqmeta-url (string),
--seqmeta-timeout (duration, default 30s). Depends on Phase 4
(Server) and Phase 5 (root command). Covers all 3 acceptance
tests from H1.

- [ ] implemented
- [ ] reviewed

### Batch 1 (parallel, after item 6.1 is reviewed)

#### Item 6.2: G1 - wa results register [parallel with 6.3, 6.4, 6.5, 6.6]

spec.md section: G1

Implement the `results register` subcommand in
`cmd/results.go`. Scans output directory, detects pipeline
via DetectPipeline, builds run key via BuildRunKey, constructs
Registration, POSTs to server. Supports --server, --user,
--operator, --command, --nextflow-workflow, --runid,
--additional-unique, --input-file (repeatable), --meta
(repeatable key=value), --include-hidden, --json (stdin),
and positional output directory. Covers all 4 acceptance
tests from G1.

- [ ] implemented
- [ ] reviewed

#### Item 6.3: G2 - wa results search [parallel with 6.2, 6.4, 6.5, 6.6]

spec.md section: G2

Implement the `results search` subcommand in
`cmd/results.go`. Supports --server, --user, --operator,
--pipeline-name, --meta key=value, --output-dir-prefix.
Prints JSON array to stdout. Covers all 2 acceptance tests
from G2.

- [ ] implemented
- [ ] reviewed

#### Item 6.4: G3 - wa results get [parallel with 6.2, 6.3, 6.5, 6.6]

spec.md section: G3

Implement the `results get` subcommand in `cmd/results.go`.
Takes positional ID argument, optional --files flag to include
file list. Prints JSON to stdout. Covers all 3 acceptance
tests from G3.

- [ ] implemented
- [ ] reviewed

#### Item 6.5: G4 - wa results delete [parallel with 6.2, 6.3, 6.4, 6.6]

spec.md section: G4

Implement the `results delete` subcommand in
`cmd/results.go`. Takes positional ID argument, sends DELETE
to server. Covers all 2 acceptance tests from G4.

- [ ] implemented
- [ ] reviewed

#### Item 6.6: G5 - wa results rescan [parallel with 6.2, 6.3, 6.4, 6.5]

spec.md section: G5

Implement the `results rescan` subcommand in
`cmd/results.go`. Takes positional ID and directory arguments,
optional --include-hidden flag. Scans directory locally, PUTs
file list to server. Covers all 2 acceptance tests from G5.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
