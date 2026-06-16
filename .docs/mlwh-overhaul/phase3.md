# Phase 3: mlwh serve command

Ref: [spec.md](spec.md) sections E4

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.

Depends on Phase 2 (`mlwh.Queryer`, registry, gin server).

## Items

### Item 3.1: E4 - wa mlwh serve command

spec.md section: E4

Add `newMLWHServeCommand` to `cmd/mlwh.go`: `OpenCacheOnly` a local
`Client`, build `mlwh.NewServer(client)`, wire gas (auth off by default;
secured when token/cert configured, mirroring `wa results serve`
security flags `--cert`/`--key`/`--server-token`/`--url`/`--port` plus
`--mlwh-cache`), and `Start`. It must never trigger sync and must not add
`--mlwh-sync-interval`. Tests in `cmd/mlwh_test.go` cover all 5 acceptance
tests from E4.

- [ ] implemented
- [ ] reviewed
