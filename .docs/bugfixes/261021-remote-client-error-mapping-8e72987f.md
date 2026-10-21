# MLWH remote client reports bad input as an impaired upstream

- Branch: errmap-8e72987f
- Base: origin/develop @ fa77143
- Queue owner: k8s deployment delivery (llm-knowledge-base session); this
  checklist owns this branch only.

- [x] `RemoteClient` (CLI `--server` mode, and the MCP server through the Go
      client) maps any error envelope code it has no sentinel for to
      `ErrUpstreamImpaired`. A 400 `bad_request` such as `limit must not
      exceed 1000` therefore reaches users as
      `limit must not exceed 1000: mlwh: upstream database impaired`.
    - Red commands:
      `go test ./mlwh -run 'TestRemoteClient(Maps(BadRequest|UnknownClientErrors|UnknownServerErrors)|KeepsMappedCodes)FromRealServer' -count=1`
      and
      `go test ./cmd -run TestMLWHSearchServerBadRequestShowsServerMessage -count=1`
      -> the 400 and every unknown 4xx wrapped `ErrUpstreamImpaired`, and
      `wa mlwh search --limit 1001 --server ...` printed the impaired text.
    - Cause: `decodeRemoteError` (`mlwh/remote.go`) used
      `ErrUpstreamImpaired` as the fallback for every status, and
      `sentinelForHTTPErrorCode` has no entry for `bad_request`,
      `unauthorized`, `payload_too_large` or any future code.
    - Fixed in `mlwh/remote.go` and `mlwh/errors_http.go`: a 4xx whose code
      has no sentinel, or whose body is not an envelope, is now a
      `*RemoteRequestError` keeping the status, code and message. It matches
      the new `ErrRequestRejected`, and also the new `ErrBadRequest` when the
      status is 400 or the code is `bad_request`. Its text is
      `mlwh: bad request: <message>` or
      `mlwh: request rejected (<status> <code>): <message>`.
    - Unchanged: `not_found`, `ambiguous`, `unsupported_identifier`,
      `cache_never_synced`, `upstream_impaired` and `feedback_disabled` keep
      their sentinels and `<message>: <sentinel>` text, and `SubmitFeedback`
      keeps its own mapping.
    - Decision: a 5xx with an unknown code (for example 500
      `internal_error`), and a non-envelope 5xx, still wrap
      `ErrUpstreamImpaired`. The server failed to serve a request it
      accepted, so retrying later is the right remedy, and that is what
      callers matching `ErrUpstreamImpaired` already advise. A new sentinel
      would break their `errors.Is` checks and change nothing for the user.
    - Tests: `mlwh/remote_errors_test.go` drives the real MLWH routes for the
      400, injects 401 `unauthorized`, 413 `payload_too_large`, 418 with an
      unknown code, 500 `internal_error` and every mapped code through the
      server's own error writer, and checks a plain-text 404 from a gin router
      without the MLWH routes. `cmd/mlwh_test.go` checks the CLI message. The
      `mlwh/remote_test.go` cases that asserted a 400 matched
      `ErrUpstreamImpaired` now assert `ErrBadRequest` and not
      `ErrUpstreamImpaired`.
    - Follow-up outside this repo: the MLWH MCP server's error hints
      (`internal/mlwh/errmap.go` in llm-knowledge-base) rely on a 400
      matching `ErrUpstreamImpaired` for the "fix the input" hint. When it
      bumps wa, it needs hints for `ErrBadRequest` and `ErrRequestRejected`.
