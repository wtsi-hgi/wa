# Production guard rejects the real MLWH source

- Branch: prodguard-880d6cbc
- Base: origin/develop @ 4b41795
- Queue owner: prodguard-880d6cbc (this checklist); delivery queue also tracks
  wa image branch, llm-knowledge-base image branch, and hgi-apps-helm values.

- [x] `WA_ENV=production` (Go `validateProductionScenarioEnvironment` in
      `cmd/root.go`, and `run-dev.sh --mode prod` guards) rejects the real
      read-only upstream MLWH source DSN
      `mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse` and its password, so wa cannot
      run `mlwh sync` / `make prod` in production. That source is shared by all
      environments. Keep the localhost/127.0.0.1/\_test DSN checks and the
      test-shaped cache-path checks.
    - Red command (exit 1; `wa mlwh freshness` was used for the first
      reproduction but is not a subcommand, and the guard ran before dispatch):
      `env -i PATH=/usr/bin:/bin HOME=<scratch> WA_ENV=production WA_MLWH_DSN='mlwh_humgen@tcp(mlwh-db-ro:3435)/mlwarehouse' WA_MLWH_PASSWORD=[REDACTED-known-dev-literal] WA_MLWH_CACHE_PATH=<non-/tmp dir>/cache.sqlite ./wa mlwh programmes`
      -> `Error: WA_MLWH_PASSWORD matches a development or test literal and is not permitted when WA_ENV=production`
    - Fixed in `cmd/root.go` and `run-dev.sh`: dropped the exact source-DSN
      match and the password-literal checks; the localhost/127.0.0.1/\_test
      DSN and test-shaped cache-path checks remain. Tests: `main_test.go`
      (production accepts the shared DSN with a fake password; rejects a
      localhost DSN and a `/tmp` cache path) and `cmd/run_dev_modes_test.go`
      (`--mode prod` accepts the shared DSN). `.docs/mlwh/spec.md`
      acceptance test 6 updated to match.
- [x] Remove the real MLWH password literal from the tree (user request: the
      repository is public). Tests that need a production-shaped password use
      an obviously fake value. History is not rewritten.
    - Red command: `git grep -n <literal>` finds it in `cmd/root.go`,
      `run-dev.sh`, `cmd/run_dev_modes_test.go` and `main_test.go`.
    - Fixed by removing the literal constants and the guards that compared
      against them; `git grep` for the literal now finds nothing.
