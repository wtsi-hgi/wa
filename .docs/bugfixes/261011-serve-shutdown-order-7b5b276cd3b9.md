# Bugfix: mlwh serve closes stores before requests finish

- Branch: `serveshutdown-cf1cb518`
- Base: `origin/develop` at `73ebdaa721d5cfe478e22e9ee504061ad30b1492`
- Queue owner: branch `usage`, checklist
  `.docs/bugfixes/261011-usage-delivery-92e8e35556b7.md`
- Worktree: `../wa-serveshutdown`

## Items

- [ ] `mlwhServeGasAuthServer.StartHTTP` in `cmd/mlwh.go` runs
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
