# Phase 4: Identifier Validation

Ref: [spec.md](spec.md) sections D1, D2, D3, D4, D5

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 4.1: D1 - Validate study identifiers

spec.md section: D1

Implement `Validate` function in `seqmeta/validate.go`. Define
`IdentifierType` constants and `IdentifierResult` type. Check
study ID via `GetStudy` first, then study accession via
`AllStudies`. Depends on Phase 1 (SAGAProvider). Covering all
2 acceptance tests from D1.

- [ ] implemented
- [ ] reviewed

### Batch 2 (parallel, after item 4.1 is reviewed)

#### Item 4.2: D2 - Validate sample identifiers [parallel with 4.3, 4.4, 4.5]

spec.md section: D2

Extend `Validate` to check sample fields using a single
`AllSamples` call: SangerID, IDSampleLims, AccessionNumber,
IDRun (string to int), LibraryType. Study checks take priority.
Covering all 6 acceptance tests from D2.

- [ ] implemented
- [ ] reviewed

#### Item 4.3: D3 - Validate project name [parallel with 4.2, 4.4, 4.5]

spec.md section: D3

Extend `Validate` to check project names via `ListProjects` as
the final fallback after study and sample checks fail. Covering
the 1 acceptance test from D3.

- [ ] implemented
- [ ] reviewed

#### Item 4.4: D4 - Unknown identifier [parallel with 4.2, 4.3, 4.5]

spec.md section: D4

Ensure `Validate` returns `ErrUnknownIdentifier` when no SAGA
entity matches, including for empty string input. Covering all
2 acceptance tests from D4.

- [ ] implemented
- [ ] reviewed

#### Item 4.5: D5 - Upstream errors propagate [parallel with 4.2, 4.3, 4.4]

spec.md section: D5

Ensure non-`ErrNotFound` saga errors (connection refused, 502,
unauthorized, timeout) propagate immediately from `Validate`
instead of being swallowed. Covering all 4 acceptance tests
from D5.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
