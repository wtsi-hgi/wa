# Phase 3: Serve command, callers, and dev script

Ref: [spec.md](spec.md) sections D1, D2, D3

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.
Bound shell commands that could hang with `timeout`. Do not run
long-lived servers outside the bounded end-to-end tests in D1 and D3.

`cmd/` `TestRunDev*` tests are load-dependent and can flake when the whole
suite runs. If one fails, re-run the changed package in isolation before
treating it as a real failure.

## Items

### Batch 1 (parallel)

#### Item 3.1: D1 - --usage-db flag and admin token [parallel with 3.2]

spec.md section: D1

In `cmd/mlwh.go`, add the `--usage-db` flag defaulting to
`firstEnv("WA_MLWH_USAGE_PATH")` and the int `--usage-retention-days` flag
(default 365), the `Long` paragraph and replaced sentence from spec.md D1,
`openMLWHServeUsage`, and `resolveMLWHServeUsageRetentionDays`, which
mirrors `resolveMLWHServeBindPort`. Rename `mlwhServeFeedbackAdminToken`
to `mlwhServeAdminToken`. In `RunE`, resolve retention, open usage after
`openMLWHServeFeedback`, defer closing a non-nil store, resolve the shared
admin token only when usage is on and feedback did not already resolve
it, and pass `mlwh.WithUsage(usageStore, adminToken)` to `mlwh.NewServer`.

Existing serve tests must clear `WA_MLWH_USAGE_PATH` and
`WA_MLWH_USAGE_RETENTION_DAYS` in their shared env reset, so a developer's
exported value cannot open a real store. Tests 7 to 9 drive the real
command on a free local port as feedback D1 tests 8 and 9 do. Tests go in
`cmd/mlwh_test.go`. Depends on phase 2. Covering all 9 acceptance tests
from D1.

- [ ] implemented
- [ ] reviewed

#### Item 3.2: D2 - CLI and internal-server identities [parallel with 3.1]

spec.md section: D2

Create `cmd/mlwh_identity.go` with `waBuildVersion`,
`mlwhCLIRemoteConfig` (per-process run ID from 16 `crypto/rand` bytes via
`sync.OnceValue`, username from `os/user` then `$USER`), and
`mlwhServerRemoteConfig`. Switch the seven CLI call sites in
`cmd/mlwh_info.go`, `cmd/mlwh_latest.go`, `cmd/mlwh_search.go`,
`cmd/mlwh_runs.go`, `cmd/mlwh_export.go`, `cmd/mlwh_studies.go`, and
`wa mlwhdiff diff` to `mlwhCLIRemoteConfig`.
`openResultsServeMLWHClientWithConfig` in `cmd/results.go` uses
`mlwhServerRemoteConfig` with `mlwh.UserAgentProductResultsServer`. Add
`AsServer` to `mlwhdiffMLWHConfig` and an `asServer` argument to
`openMLWHDiffClient`, true from `newMLWHDiffServeCommand` and false from
`newMLWHDiffDiffCommand`; when `AsServer` is set,
`openMLWHDiffClientWithConfig` uses `mlwhServerRemoteConfig` with
`mlwh.UserAgentProductMLWHDiffServer`. Tests go in
`cmd/mlwh_identity_test.go`. Depends on item 2.4. Covering all 6
acceptance tests from D2.

- [ ] implemented
- [ ] reviewed

For parallel batch items, use separate subagents per item under the
`subagents` skill's shared concurrency limits.
Launch review subagents using the `go-reviewer` skill
(review all items in the batch together in a single review
pass).

### Item 3.3: D3 - run-dev.sh wiring

spec.md section: D3

In `run-dev.sh`, mirror the feedback DB wiring with `USAGE_DB_PATH` and
`USAGE_DB_EPHEMERAL`: test mode uses an ephemeral
`mlwh-usage-test.XXXXXX.sqlite` under `$TMP_DIR`, removed with its `-wal`
and `-shm` files on cleanup; dev and prod use `${WA_MLWH_USAGE_PATH:-}` and
create its parent directory. Only the auto-managed `mlwh serve` branch
appends `--usage-db`, and only when the path is non-empty. Add
`-A wa-run-dev` to `curl_probe` and to the `mlwh_freshness_is_cold`
`curl` in every mode. Tests go in `cmd/run_dev_test.go`, extending the
fake `mlwh serve` stub to log each request's User-Agent and URL for test
4. Depends on item 3.1. Covering all 4 acceptance tests from D3.

- [ ] implemented
- [ ] reviewed
