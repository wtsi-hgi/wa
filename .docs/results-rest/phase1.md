# Phase 1: Types, Keys, and Scanner

Ref: [spec.md](spec.md) sections A1, A2, A3, B1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 1.1: A1 - CompositeKeyID

spec.md section: A1

Implement `CompositeKeyID(pipelineIdentifier, runKey) string`
in `results/types.go`. Returns lowercase hex SHA256 of the two
inputs joined by a null byte separator. Covers all 3 acceptance
tests from A1.

- [ ] implemented
- [ ] reviewed

### Item 1.2: A2 - BuildRunKey

spec.md section: A2

Implement `BuildRunKey(runID, additionalUnique string) string`
in `results/types.go`. Produces a URL-query-encoded string with
keys sorted alphabetically, omitting empty values. Covers all
5 acceptance tests from A2.

- [ ] implemented
- [ ] reviewed

### Batch 1 (parallel, after item 1.2 is reviewed)

#### Item 1.3: A3 - DetectPipeline [parallel with 1.4]

spec.md section: A3

Implement `DetectPipeline(workflowPath string) (identifier,
name, version string, err error)` in `results/types.go`.
Auto-detects pipeline identity from a workflow file path using
git metadata or file content hashing. Covers all 5 acceptance
tests from A3.

- [ ] implemented
- [ ] reviewed

#### Item 1.4: B1 - ScanDirectory [parallel with 1.3]

spec.md section: B1

Implement `ScanDirectory(dir string, includeHidden bool)
([]FileEntry, int, error)` in `results/scanner.go`. Recursively
lists files with mtime/size, follows symlinks, detects cycles,
and optionally excludes hidden files. Also define the `FileEntry`
struct in `results/types.go`. Covers all 7 acceptance tests
from B1.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).
