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

- [ ] `wa mlwhdiff serve` (`cmd/mlwhdiff.go`, `RunE`, around lines 257-266)
      has the same shutdown-order flaw in its own code: it calls
      `httpServer.Shutdown(context.Background())` in a goroutine and returns on
      `http.ErrServerClosed` without waiting, so the deferred `store.Close()`
      and `provider.Close()` can run under live requests; its shutdown also has
      no timeout. Same root cause as the item above, so it shares this branch.
    - Source: bugfix of the item above on this branch, reported as
      incidental.
    - Evidence: code inspection only; needs a red test like
      `TestMLWHServeStartHTTPWaitsForInFlightRequests`.
