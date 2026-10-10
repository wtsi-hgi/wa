# Bugfix checklist: feedback client error text

- Branch: `feedback-errors-55019c28` (worktree `../wa-feedback-errors`)
- Base: `origin/develop` at `29077be861f3db047bb12322327a1efbf565be54`
- Queue owner: llm-knowledge-base branch `feedback` delivery
  (PR wtsi-hgi/llm-knowledge-base#5); this checklist

Red scripts live in the orchestrating session's scratchpad, outside this
repository, at
`/tmp/claude-1000/-home-ubuntu-llm-knowledge-base/76e80516-b817-4e23-b793-f62b560cc0a0/scratchpad/wa-red/feedback-errors/`
(`$RED` below). `red.sh <worktree>` injects `red_feedback_errors_test.go`
into `mlwh` with `go test -overlay`, so it leaves the worktree unchanged.
`TMPDIR` only sets where the overlay file goes. The fix's regression tests
belong in `mlwh/remote_feedback_test.go`.

Prior items checked: no earlier checklist covers `SubmitFeedback` error text.
260627-9 relies on `errors.Is` reaching `context.DeadlineExceeded` through
`ErrUpstreamImpaired` on Registry calls, which `SubmitFeedback` does not touch.

- [ ] Feedback client error text repeats 'feedback is disabled on this server' three times: submitFeedbackError in mlwh/remote_feedback.go wraps the decodeRemoteError result (which already contains the sentinel) with the sentinel again, so callers see e.g. `mlwh: feedback is disabled on this server: feedback is disabled on this server: mlwh: feedback is disabled on this server`.
    - Source: llm-knowledge-base `feedback` delivery (PR wtsi-hgi/llm-knowledge-base#5)
    - Confirmed at 29077be against the real `NewServer` with feedback off.
      `decodeRemoteError` maps `feedback_disabled` to `ErrFeedbackDisabled`
      and returns `<server message>: <sentinel>`, and `submitFeedbackError`
      prefixes the sentinel again. The other feedback statuses do not repeat
      their text, because `decodeRemoteError` maps their envelope codes to
      nil, not to a feedback sentinel. Their error text is the subject of
      the next item.
    - Red command: `$RED/red.sh .` from the worktree root
      (exit 1; the same run covers both items). The test asserts that `errors.Is(err, ErrFeedbackDisabled)`
      holds and that the text occurs exactly once.

        ```text
        === RUN   TestRedItem5FeedbackDisabledTextNotRepeated
            red_feedback_errors_test.go:64: err.Error() = "mlwh: feedback is disabled on this server: feedback is disabled on this server: mlwh: feedback is disabled on this server"
            red_feedback_errors_test.go:71: "feedback is disabled on this server" appears 3 times in "mlwh: feedback is disabled on this server: feedback is disabled on this server: mlwh: feedback is disabled on this server"; want 1
        --- FAIL: TestRedItem5FeedbackDisabledTextNotRepeated (0.00s)
        ```

- [ ] Client-side feedback errors (401/404 text bodies, 413 too large, 400 invalid, 500) include the misleading text 'mlwh: upstream database impaired' even though no database is involved.
    - Source: llm-knowledge-base `feedback` delivery (PR wtsi-hgi/llm-knowledge-base#5)
    - Confirmed at 29077be. 413, 400 and 500 come from the real `NewServer`
      with `WithFeedback`. The 500 comes from a closed feedback store. The 404
      is gin's text body for a missing route, and the 401 comes from a
      text-body stub. One correction to the wording: the 500 does involve a
      database, the SQLite feedback store. It is still not the upstream MLWH
      database that `ErrUpstreamImpaired` names.
    - Contract conflict: `ErrUpstreamImpaired` is part of the documented
      contract today. `.docs/feedback/spec.md` C1 says 400 `bad_request`,
      401, 413 and non-envelope bodies satisfy
      `errors.Is(err, ErrUpstreamImpaired)`. Acceptance tests C1.6 and C1.7
      require it, `SubmitFeedback`'s doc comment says so, and
      `mlwh/remote_feedback_test.go` asserts it for 404 text, 400 text, 400
      `bad_request`, 413, 401 and 500. A fix must amend spec C1, the doc
      comment and those assertions on purpose, recording the reason here.
      An `Error()` that hides a sentinel `errors.Is` still matches would
      only move the confusion.
    - What callers depend on: the only external caller is llm-knowledge-base
      `internal/mlwh/tools_feedback.go` `mapFeedbackError`. It checks the
      five feedback sentinels with `errors.Is` before anything else, maps the
      rest to `feedback_unavailable`, and never tests `ErrUpstreamImpaired`.
      It does put the full `err.Error()` in the tool result, so agents see
      this text. Its tests assert only the substring
      `feedback is disabled on this server`. In wa, nothing outside `mlwh`
      calls `SubmitFeedback`.
    - Red command: `$RED/red.sh .` from the worktree root
      (exit 1; the same run covers both items). Each case first requires its feedback sentinel to
      match (none for 500) and then fails on the text.

        ```text
        === RUN   TestRedItem6FeedbackErrorsDoNotClaimDatabaseImpaired/401_text_body
            red_feedback_errors_test.go:118: err.Error() = "mlwh: feedback request unauthorized: mlwh: upstream database impaired: remote SubmitFeedback returned 401 without a valid MLWH error envelope; response may not have come from the MLWH server; content-type text/plain; charset=utf-8"
        === RUN   TestRedItem6FeedbackErrorsDoNotClaimDatabaseImpaired/404_text_body_from_a_router_without_the_route
            red_feedback_errors_test.go:118: err.Error() = "mlwh: server does not support feedback: mlwh: upstream database impaired: remote SubmitFeedback returned 404 without a valid MLWH error envelope; response may not have come from the MLWH server; content-type text/plain"
        === RUN   TestRedItem6FeedbackErrorsDoNotClaimDatabaseImpaired/413_too_large_from_the_real_server
            red_feedback_errors_test.go:118: err.Error() = "mlwh: feedback too large: description exceeds 16384 bytes: mlwh: upstream database impaired"
        === RUN   TestRedItem6FeedbackErrorsDoNotClaimDatabaseImpaired/400_invalid_from_the_real_server
            red_feedback_errors_test.go:118: err.Error() = "mlwh: invalid feedback: invalid category \"bogus\": mlwh: upstream database impaired"
        === RUN   TestRedItem6FeedbackErrorsDoNotClaimDatabaseImpaired/500_from_the_real_server_with_a_closed_store
            red_feedback_errors_test.go:118: err.Error() = "could not store feedback: mlwh: upstream database impaired"
        --- FAIL: TestRedItem6FeedbackErrorsDoNotClaimDatabaseImpaired (0.03s)
            --- FAIL: .../401_text_body (0.00s)
            --- FAIL: .../404_text_body_from_a_router_without_the_route (0.00s)
            --- FAIL: .../413_too_large_from_the_real_server (0.02s)
            --- FAIL: .../400_invalid_from_the_real_server (0.01s)
            --- FAIL: .../500_from_the_real_server_with_a_closed_store (0.01s)
        FAIL	github.com/wtsi-hgi/wa/mlwh	0.040s
        ```
