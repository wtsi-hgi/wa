# Phase 5: CLI Integration

Ref: [spec.md](spec.md) sections E1, E2, E3, E5, E6

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

## Items

### Batch 1 (parallel)

#### Item 5.1: E1 - wa results register uses mlwh resolvers [parallel with 5.2, 5.3, 5.4, 5.5]

spec.md section: E1

Update `cmd/results.go` register to call `mlwh.ResolveSample`,
`ResolveStudy`, `ResolveRun`, `ResolveLibrary`. Rewrite
`--run/--study/--sample/--library` help text to enumerate input forms
and warn on first-call latency for `--library` (mention "first call"
and "wa mlwh sync"). Error messages name the dimension and offending
value. Covers all 6 acceptance tests from E1.

- [ ] implemented
- [ ] reviewed

#### Item 5.2: E2 - wa seqmeta serve flag rewiring [parallel with 5.1, 5.3, 5.4, 5.5]

spec.md section: E2

Edit `cmd/seqmeta.go`: drop `--token` and `--base-url`; keep `--db`;
add `--mlwh-cache` and `--mlwh-sync-interval` (default zero, opt-in);
remove `prefetchedProvider`. Reject DSNs with embedded passwords.
Covers all 4 acceptance tests from E2.

- [ ] implemented
- [ ] reviewed

#### Item 5.3: E3 - New wa mlwh sync command [parallel with 5.1, 5.2, 5.4, 5.5]

spec.md section: E3

Add `cmd/mlwh.go` registering `wa mlwh sync` with optional
`--tables`. One-shot `mlwh.Client.Sync` over `sample`, `study`,
`iseq_flowcell`. Exit non-zero with `WA_MLWH_DSN` named when the env
var is missing. Covers all 3 acceptance tests from E3.

- [ ] implemented
- [ ] reviewed

#### Item 5.4: E5 - wa results serve MLWH flag rewiring [parallel with 5.1, 5.2, 5.3, 5.5]

spec.md section: E5

Edit `cmd/results.go` `serve` to add `--mlwh-cache` and
`--mlwh-sync-interval`, with the same DSN-password rejection and
opt-in sync goroutine semantics as E2. Cancel the sync goroutine when
the server shuts down. Covers all 5 acceptance tests from E5.

- [ ] implemented
- [ ] reviewed

#### Item 5.5: E6 - Env scenario guards for WA*MLWH*\* [parallel with 5.1, 5.2, 5.3, 5.4]

spec.md section: E6

Extend `run-dev.sh` and the `cmd/root.go` mode validation so
`WA_MLWH_DSN`, `WA_MLWH_PASSWORD`, `WA_MLWH_CACHE_PATH`, and
`WA_MLWH_CACHE_PASSWORD` follow the same scenario guards as the
existing `WA_RESULTS_DB_*` vars. Add tests in
`cmd/run_dev_modes_test.go` using the existing
`runRunDevExpectingFailureForTest` pattern. Covers all 7 acceptance
tests from E6.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item.
Launch review subagents using the `go-reviewer` skill (review all
items in the batch together in a single review pass).
