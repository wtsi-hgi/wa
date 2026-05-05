# Phase 3: Seqmeta Validation

Ref: [spec.md](spec.md) sections D1

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Item 3.1: D1 - SeqmetaValidator

spec.md section: D1

Implement `NewSeqmetaValidator(baseURL string,
timeout time.Duration) *SeqmetaValidator` and
`(v *SeqmetaValidator) ValidateMetadata(ctx context.Context,
metadata map[string]string) error` in `results/validate.go`.
Also define `SeqmetaFieldTypes` map and `SeqmetaValidator`
struct in `results/types.go`. Validates seqmeta\_\* metadata
fields against a remote seqmeta service via HTTP GET. Returns
appropriate sentinel errors for unknown suffixes, type
mismatches, and unreachable servers. Covers all 7 acceptance
tests from D1.

- [x] implemented
- [x] reviewed
