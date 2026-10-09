# Phase 4: Serve wiring and dev script

Ref: [spec.md](spec.md) sections D1, D2

## Instructions

Use the `orchestrator` skill to complete this phase, coordinating
subagents with the `go-implementor` and `go-reviewer` skills.
Bound shell commands that could hang with `timeout`. Do not run
long-lived servers outside the bounded end-to-end tests in D1.

## Items

### Item 4.1: D1 - --feedback-db flag and admin token

spec.md section: D1

In `cmd/mlwh.go`, add the `--feedback-db` flag defaulting to
`firstEnv("WA_MLWH_FEEDBACK_PATH")`, the `Long` help paragraph, the
`mlwhServeDefaultServerTokenBasename` const, and
`openMLWHServeFeedback(ctx, feedbackDB, config)`. Move `mlwh.NewServer` after
the secured branch and follow the six-step `RunE` order in spec.md D1, with
the store closed after `startMLWHServeAuthServer` returns.

The existing serve tests in `cmd/mlwh_test.go` must clear
`WA_MLWH_FEEDBACK_PATH`, for example with
`t.Setenv("WA_MLWH_FEEDBACK_PATH", "")` in their shared setup such as
`installFakeMLWHServeAuthServer`. Otherwise a developer's exported value
could make them open a store or write a token file under the real
`XDG_STATE_HOME`.

Tests 8 and 9 run the real command end to end on a free local port, with
the 5 s health and 10 s shutdown limits from the spec. Covering all 9
acceptance tests from D1.

- [x] implemented
- [x] reviewed

### Item 4.2: D2 - run-dev.sh wiring

spec.md section: D2

Update `run-dev.sh` so test mode uses an ephemeral
`mlwh-feedback-test.XXXXXX.sqlite` under `$TMP_DIR` (cleanup removes it and
its `-wal` and `-shm` files), and dev and prod use
`WA_MLWH_FEEDBACK_PATH`, creating its parent directory when set. Only the
auto-managed `mlwh serve` branch appends `--feedback-db`, and only when the
path is non-empty. Tests go in `cmd/run_dev_test.go`. Depends on item 4.1.
Covering all 4 acceptance tests from D2.

- [x] implemented
- [x] reviewed
