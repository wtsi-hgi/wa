# Bugfix: mlwh serve closes stores before requests finish

- Branch: `serveshutdown-cf1cb518`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-serveshutdown`

## Items

- [x] `mlwhServeGasAuthServer.StartHTTP` in `cmd/mlwh.go` runs
      `httpServer.Shutdown` in a goroutine but returns as soon as `Serve`
      returns `http.ErrServerClosed`, without waiting for `Shutdown` to
      finish. `RunE`'s deferred closes of the feedback store and cache client
      (and, on branch `usage`, the usage store) then run while handlers are
      still active, so in-flight requests at shutdown hit closed databases.
      net/http documents that callers must wait for `Shutdown` to return.
    - Source: spec-aware PR review of branch `usage`, reported as
      incidental; the same `StartHTTP` code is on this base.
    - Evidence: code inspection of `cmd/mlwh.go` (`StartHTTP`, around lines
      129-153 on `usage`). Not yet reproduced; build a red test that holds a
      request open during shutdown and asserts it completes against an open
      store.
    - Impact: errors or lost writes for requests in flight during shutdown
      (for example `POST /feedback`).
    - Red command (exit 1, deterministic over 3 runs):
      `CGO_ENABLED=1 go test -tags netgo --count 1 -run TestMLWHServeStartHTTPWaitsForInFlightRequests ./cmd/`

        ```text
        * cmd/mlwh_serve_shutdown_test.go
        Line 111:
        Expected: false
        Actual:   true
        --- FAIL: TestMLWHServeStartHTTPWaitsForInFlightRequests (0.00s)
        ```

        Line 111 of the first red version (line 119 of the committed test)
        asserts `returnedEarly` is false: with a handler blocked in
        flight, `StartHTTP` returned within 500 ms of the context cancel,
        before the handler was released.

    - Files: `cmd/mlwh.go`, `cmd/mlwh_serve_shutdown_test.go`.
    - Approach: `StartHTTP` now waits for its shutdown goroutine before
      returning. The new helper `shutdownMLWHServeHTTPOnCancel` runs
      `Shutdown` with the existing 5 s timeout once the context is cancelled,
      and calls `Close` if the timeout expires. This matches the secured
      graceful path, which also returns nil after its timeout. The helper
      returns without shutting down if `Serve` ends first, so no goroutine
      leaks. The regression test holds a request in flight across the
      cancel and asserts that `StartHTTP` returns only after the handler
      finishes.
    - Checked: `cmd/mlwhdiff.go` `serve` has the same pattern in separate
      code. It is reported to the queue owner, not fixed here. The secured
      `mlwh serve` and `results serve` paths use go-authserver's graceful
      server, whose `Serve` waits for connections to drain, so they are not
      affected.

- [x] `wa mlwhdiff serve` (`cmd/mlwhdiff.go`, `RunE`, around lines 257-266)
      has the same shutdown-order flaw in its own code: it calls
      `httpServer.Shutdown(context.Background())` in a goroutine and returns on
      `http.ErrServerClosed` without waiting, so the deferred `store.Close()`
      and `provider.Close()` can run under live requests; its shutdown also has
      no timeout. Same root cause as the item above, so it shares this branch.
    - Source: bugfix of the item above on this branch, reported as
      incidental.
    - Evidence: code inspection only; needs a red test like
      `TestMLWHServeStartHTTPWaitsForInFlightRequests`.
    - Red command (exit 1, deterministic over 3 runs):
      `CGO_ENABLED=1 go test -tags netgo --count 1 -run TestMLWHDiffServeWaitsForInFlightRequests ./cmd/`

        ```text
        * cmd/mlwh_serve_shutdown_test.go
        Line 245:
        Expected: false
        Actual:   true
        --- FAIL: TestMLWHDiffServeWaitsForInFlightRequests (0.01s)
        ```

        Line 245 asserts `returnedEarly` is false: with a
        `GET /diff/sample/S1` handler blocked in flight, `ExecuteContext`
        returned within 500 ms of the context cancel. `GET /diff/study/all`
        does not show the bug, because its handler holds the store mutex
        that `Store.Close` also takes.

    - Files: `cmd/mlwh.go`, `cmd/mlwhdiff.go`,
      `cmd/mlwh_serve_shutdown_test.go`.
    - Approach: the serve-and-wait code from `StartHTTP` moved unchanged into
      `serveHTTPUntilCancelled` in `cmd/mlwh.go`, and the shutdown helper is
      now `shutdownHTTPOnCancel`. `mlwh serve` and `mlwhdiff serve` both call
      it, so `mlwhdiff serve` returns only after in-flight requests drain,
      with the same 5 s timeout and `Close` fallback. `mlwh serve` behaviour
      is unchanged. The regression test holds `GET /diff/sample/S1` in flight
      across the cancel and asserts that the command returns, and the
      provider closes, only after the handler finishes.

- [x] The `http.Server` in `wa mlwhdiff serve` (`cmd/mlwhdiff.go`, around
      line 254) sets no `ReadHeaderTimeout`, while `wa mlwh serve`'s
      `StartHTTP` uses 30 s, so slow-header clients can hold connections open.
      Same server setup as the item above, so it shares this branch.
    - Source: bugfix of the item above, reported as incidental.
    - Evidence: code inspection; `.golangci.yml` does not enable gosec, so
      the linter does not flag it.
    - Red command (exit 1, deterministic over 3 runs):
      `CGO_ENABLED=1 go test -tags netgo --count 1 -run 'TestMLWHDiffServeClosesSlowHeaderConnections|TestMLWHServeStartHTTPClosesSlowHeaderConnections' ./cmd/`

        ```text
        * cmd/mlwh_serve_shutdown_test.go
        Line 334:
        Expected: true
        Actual:   false
        --- FAIL: TestMLWHDiffServeClosesSlowHeaderConnections (2.01s)
        ```

        Line 334 asserts the server closed a connection that sent an
        incomplete request header. With the shared read-header timeout
        shortened to 150 ms for the test, `mlwhdiff serve` still held the
        connection open after 2 s. The `mlwh serve` check passes.

    - Files: `cmd/mlwh.go`, `cmd/mlwhdiff.go`,
      `cmd/mlwh_serve_shutdown_test.go`.
    - Approach: the new helper `newServeHTTPServer` in `cmd/mlwh.go` builds
      the plain-HTTP server for both `mlwh serve` and `mlwhdiff serve`. It
      sets `ReadHeaderTimeout` from `serveHTTPReadHeaderTimeout`, which
      defaults to the 30 s that `mlwh serve` already used, so `mlwh serve`
      behaviour is unchanged. The regression tests send an incomplete
      request header to each command's server, with the timeout shortened
      to 150 ms, and assert that the server closes the connection.

- [x] After rebasing onto `origin/develop` at
      `a6d0876c6fba678217e4d441c5d42f5342e1d888` (which adds #39's usage
      store), no test proves that `wa mlwh serve` keeps its usage store open
      until in-flight requests finish. `RunE` defers `usageStore.Close()`,
      and the usage middleware records each event after its handler returns,
      so a request still in flight when the store closes loses its usage row
      (`Record` after `Close` is a no-op).
    - Source: queue owner request after the rebase: confirm the shutdown fix
      covers the usage store close and add a cheap command-level test.
    - Red command (exit 1 on 3 of 3 runs at `a6d0876` with only the new test
      added, and 3 of 3 runs on this branch with `<-shutdownDone` removed
      from `serveHTTPUntilCancelled`):
      `CGO_ENABLED=1 go test -tags netgo --count 1 -run TestMLWHServeKeepsUsageStoreOpenForInFlightRequests ./cmd/`

        ```text
        Line 129:
        Expected: false
        Actual:   true
        --- FAIL: TestMLWHServeKeepsUsageStoreOpenForInFlightRequests (0.19s)
        ```

        Line 129 is the `returnedEarly` assertion in the test file as added
        alone at `a6d0876` (line 217 in the committed file). The command
        returned while the request was still held. With that
        assertion disabled at `a6d0876`, the row check failed too
        (`Expected: 1`, `Actual: 0`): the in-flight request's usage row was
        lost.

    - Files: `cmd/mlwh_serve_shutdown_test.go`.
    - Approach: test only; the existing `serveHTTPUntilCancelled` wait
      already covers the usage store, because `RunE` closes it in a deferred
      call that runs only after `startMLWHServeAuthServer` returns. The
      regression test runs the real command with `--usage-db`, holds a
      counted `GET /hold` in an engine-level middleware across the cancel,
      and asserts that the command returns only after the request finishes
      and that exactly one usage row, for `/hold`, is persisted.
